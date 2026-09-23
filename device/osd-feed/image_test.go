package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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
