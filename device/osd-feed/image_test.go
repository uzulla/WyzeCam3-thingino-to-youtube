package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a PNG with straight alpha: left half opaque red, right half half-transparent blue
func writeTestPNG(t *testing.T, w, h int) string {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{B: 200, A: 128})
			}
		}
	}
	p := filepath.Join(t.TempDir(), "t.png")
	f, _ := os.Create(p)
	png.Encode(f, img)
	f.Close()
	return p
}

func px(out []byte, w, x, y int) [4]byte {
	o := (y*w + x) * 4
	return [4]byte{out[o], out[o+1], out[o+2], out[o+3]}
}

func TestConvertPNGSameSizeIsBGRAStraightAlpha(t *testing.T) {
	p := writeTestPNG(t, 4, 2)
	out, err := convertPNG(p, 4, 2, "contain")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4*2*4 {
		t.Fatalf("size %d", len(out))
	}
	if got := px(out, 4, 0, 0); got != [4]byte{0, 0, 255, 255} { // B G R A
		t.Errorf("opaque red -> %v", got)
	}
	got := px(out, 4, 3, 1)
	if got[3] != 128 || got[0] < 195 || got[0] > 200 || got[1] != 0 || got[2] != 0 {
		t.Errorf("half-transparent blue must come back straight (B~200, A=128), got %v", got)
	}
}

func TestConvertPNGContainShrinksAndCentres(t *testing.T) {
	p := writeTestPNG(t, 8, 4)                 // 2:1
	out, err := convertPNG(p, 4, 4, "contain") // fits 4x2, centred vertically at rows 1..2
	if err != nil {
		t.Fatal(err)
	}
	if px(out, 4, 0, 0)[3] != 0 || px(out, 4, 0, 3)[3] != 0 {
		t.Error("padding rows must be transparent")
	}
	if got := px(out, 4, 0, 1); got != [4]byte{0, 0, 255, 255} {
		t.Errorf("scaled red at (0,1) -> %v", got)
	}
	if got := px(out, 4, 3, 2); got[3] != 128 {
		t.Errorf("scaled blue alpha at (3,2) -> %v", got)
	}
}

func TestConvertPNGNoneCropsAndSmallIsCentred(t *testing.T) {
	p := writeTestPNG(t, 8, 8)
	out, err := convertPNG(p, 4, 4, "none") // centre 4x4 of an 8x8: columns 2..5 -> red,red,blue,blue
	if err != nil {
		t.Fatal(err)
	}
	if px(out, 4, 1, 0)[2] != 255 || px(out, 4, 2, 0)[0] == 0 {
		t.Errorf("crop must keep the centre: %v %v", px(out, 4, 1, 0), px(out, 4, 2, 0))
	}
	p2 := writeTestPNG(t, 2, 2)
	out, _ = convertPNG(p2, 6, 6, "contain") // never enlarged: 2x2 at (2,2)
	if px(out, 6, 0, 0)[3] != 0 || px(out, 6, 2, 2)[3] != 255 {
		t.Errorf("small image must be centred, not enlarged: %v %v", px(out, 6, 0, 0), px(out, 6, 2, 2))
	}
}

func TestConvertPNGErrors(t *testing.T) {
	if _, err := convertPNG(filepath.Join(t.TempDir(), "missing.png"), 4, 4, "contain"); err == nil {
		t.Error("missing file must fail")
	}
	p := filepath.Join(t.TempDir(), "bad.png")
	os.WriteFile(p, []byte("not a png"), 0o644)
	if _, err := convertPNG(p, 4, 4, "contain"); err == nil {
		t.Error("garbage must fail")
	}
}

func TestImageSlotRenderTracksTheFile(t *testing.T) {
	p := writeTestPNG(t, 4, 4)
	s := newImageSlot("imagefile", SlotConfig{PNG: p}, SlotGeometry{Path: "/tmp/x", Width: 4, Height: 4})
	pix, changed, err := s.render()
	if err != nil || !changed || len(pix) != 64 {
		t.Fatalf("first render: %v changed=%v len=%d", err, changed, len(pix))
	}
	if _, changed, _ = s.render(); changed {
		t.Error("unchanged file must not report a change")
	}
	os.Remove(p)
	pix, changed, err = s.render()
	if err == nil || pix != nil || !changed {
		t.Errorf("removed file: err=%v pix=%v changed=%v", err, pix != nil, changed)
	}
	// geometry change forces a new conversion
	writeTestPNG2 := writeTestPNG(t, 4, 4)
	s.PNG = writeTestPNG2
	s.render()
	if !s.follow(SlotGeometry{Path: "/tmp/x", Width: 8, Height: 8}, SlotConfig{PNG: writeTestPNG2}) {
		t.Fatal("new size must count as a change")
	}
	pix, _, err = s.render()
	if err != nil || len(pix) != 8*8*4 {
		t.Errorf("after resize: %v len=%d", err, len(pix))
	}
}

func TestImageSlotTouchedPNGIsNotReconverted(t *testing.T) {
	p := writeTestPNG(t, 4, 4)
	s := newImageSlot("imagefile", SlotConfig{PNG: p}, SlotGeometry{Path: "/tmp/x", Width: 4, Height: 4})
	if _, changed, err := s.render(); err != nil || !changed {
		t.Fatalf("first: %v %v", err, changed)
	}
	// same bytes, new mtime
	data, _ := os.ReadFile(p)
	os.WriteFile(p, data, 0o644)
	future := s.pngMod.Add(2 * 1e9)
	os.Chtimes(p, future, future)
	if _, changed, _ := s.render(); changed {
		t.Error("a touched but identical PNG must not count as a change")
	}
}

func TestImageSlotHoldsOurFile(t *testing.T) {
	dir := t.TempDir()
	s := &ImageSlot{Path: filepath.Join(dir, "img")}
	if s.holdsOurFile() {
		t.Error("nothing written yet")
	}
	writeSlotBytes(s.Path, []byte("abcd"))
	s.wrote()
	if !s.holdsOurFile() {
		t.Error("just written")
	}
	os.WriteFile(s.Path, []byte("abce"), 0o644) // same size, new inode/mtime
	if s.holdsOurFile() {
		t.Error("replaced by someone else must be noticed")
	}
	os.Remove(s.Path)
	if s.holdsOurFile() {
		t.Error("removed must be noticed")
	}
}

func TestConvertPNGRefusesHugeSource(t *testing.T) {
	// header claims 5000x5000: DecodeConfig must stop us before decoding
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	data := buf.Bytes()
	// patch the IHDR width/height (bytes 16..23, big endian) and fix its CRC
	copy(data[16:20], []byte{0, 0, 0x13, 0x88})
	copy(data[20:24], []byte{0, 0, 0x13, 0x88})
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err := convertPNGBytes(data, "huge.png", 4, 4, "contain"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("huge header must be refused before decoding, got %v", err)
	}
}

func TestImageSlotBrokenPNGIsNotDecodedEveryCall(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.png")
	os.WriteFile(p, []byte("not a png at all"), 0o644)
	s := newImageSlot("imagefile", SlotConfig{PNG: p}, SlotGeometry{Path: "/tmp/x", Width: 4, Height: 4})
	_, _, err1 := s.render()
	if err1 == nil {
		t.Fatal("garbage must fail")
	}
	// second call: same file -> same error, without touching pngData (no re-decode)
	before := s.pngData
	_, changed, err2 := s.render()
	if err2 == nil || changed || !bytes.Equal(before, s.pngData) {
		t.Errorf("cached failure expected, got err=%v changed=%v", err2, changed)
	}
	// fixing the file recovers
	good := writeTestPNG(t, 4, 4)
	data, _ := os.ReadFile(good)
	os.WriteFile(p, data, 0o644)
	future := s.pngMod.Add(2 * 1e9)
	os.Chtimes(p, future, future)
	if pix, changed, err := s.render(); err != nil || !changed || len(pix) != 64 {
		t.Errorf("after fixing: %v %v %d", err, changed, len(pix))
	}
}

func TestConvertPNGRefusesExtremeShapesAnd16Bit(t *testing.T) {
	// a 1 x 1,800,000 strip fits the pixel budget but not the per-side limit
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	data := buf.Bytes()
	copy(data[16:20], []byte{0, 0, 0, 1})
	binary.BigEndian.PutUint32(data[20:24], 1800000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err := convertPNGBytes(data, "strip.png", 4, 4, "contain"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("extreme strip must be refused, got %v", err)
	}
	// 16-bit: 1000x1000 NRGBA64 = 8 MB decoded, over the byte budget; 8-bit of the same size is fine
	img16 := image.NewNRGBA64(image.Rect(0, 0, 1, 1))
	buf.Reset()
	png.Encode(&buf, img16)
	d16 := buf.Bytes()
	binary.BigEndian.PutUint32(d16[16:20], 1000)
	binary.BigEndian.PutUint32(d16[20:24], 1000)
	binary.BigEndian.PutUint32(d16[29:33], crc32.ChecksumIEEE(d16[12:29]))
	if _, err := convertPNGBytes(d16, "big16.png", 4, 4, "contain"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("16-bit 1000x1000 must be refused by bytes, got %v", err)
	}
	// a 3000x2 source (wide) shrinks without overflow and never enlarges
	wide := image.NewNRGBA(image.Rect(0, 0, 3000, 2))
	for x := 0; x < 3000; x++ {
		wide.SetNRGBA(x, 0, color.NRGBA{R: 255, A: 255})
		wide.SetNRGBA(x, 1, color.NRGBA{R: 255, A: 255})
	}
	buf.Reset()
	png.Encode(&buf, wide)
	out, err := convertPNGBytes(buf.Bytes(), "wide.png", 100, 50, "contain")
	if err != nil || len(out) != 100*50*4 {
		t.Fatalf("wide: %v %d", err, len(out))
	}
	if px(out, 100, 50, 24)[3] != 255 || px(out, 100, 50, 0)[3] != 0 { // 1 row, centred: (50-1)/2 = 24
		t.Errorf("wide strip must land as one row in the middle: %v %v", px(out, 100, 50, 24), px(out, 100, 50, 0))
	}
}

func TestImageSlotRestoredSameContentShowsAgain(t *testing.T) {
	p := writeTestPNG(t, 4, 4)
	s := newImageSlot("imagefile", SlotConfig{PNG: p}, SlotGeometry{Path: "/tmp/x", Width: 4, Height: 4})
	if pix, _, err := s.render(); err != nil || pix == nil {
		t.Fatal("first render")
	}
	data, _ := os.ReadFile(p)
	os.Remove(p)
	if pix, _, err := s.render(); err == nil || pix != nil {
		t.Fatalf("removed: %v %v", err, pix != nil)
	}
	os.WriteFile(p, data, 0o644) // the very same bytes come back
	pix, changed, err := s.render()
	if err != nil || pix == nil || !changed {
		t.Errorf("restored file must be converted and shown again: err=%v pix=%v changed=%v", err, pix != nil, changed)
	}
}
