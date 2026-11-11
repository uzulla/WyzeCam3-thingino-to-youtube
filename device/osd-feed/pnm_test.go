package main

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"testing"
)

func pgm(w, h int, f func(x, y int) byte) []byte {
	b := []byte(fmt.Sprintf("P5 %d %d 255\n", w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			b = append(b, f(x, y))
		}
	}
	return b
}

func ppm(w, h int, f func(x, y int) [3]byte) []byte {
	b := []byte(fmt.Sprintf("P6\n%d %d\n255\n", w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := f(x, y)
			b = append(b, c[0], c[1], c[2])
		}
	}
	return b
}

func TestPGMAndPPMBecomeOpaqueBGRA(t *testing.T) {
	out, err := convertPNGBytes(pgm(4, 2, func(x, y int) byte { return byte(x * 60) }), "t.pgm", 4, 2, "contain")
	if err != nil {
		t.Fatal(err)
	}
	if got := px(out, 4, 3, 1); got != [4]byte{180, 180, 180, 255} {
		t.Errorf("gray 180 -> %v", got)
	}
	out, err = convertPNGBytes(ppm(4, 2, func(x, y int) [3]byte { return [3]byte{255, 0, 0} }), "t.ppm", 4, 2, "contain")
	if err != nil {
		t.Fatal(err)
	}
	if got := px(out, 4, 0, 0); got != [4]byte{0, 0, 255, 255} { // B G R A
		t.Errorf("red -> %v", got)
	}
	// a comment and odd whitespace in the header are fine (netpbm allows them)
	data := append([]byte("P5\n# from a camera\n4 2\r\n255\t"), pgm(4, 2, func(x, y int) byte { return 7 })[len("P5 4 2 255\n"):]...)
	if out, err := convertPNGBytes(data, "t.pgm", 4, 2, "contain"); err != nil || px(out, 4, 1, 1)[0] != 7 {
		t.Errorf("header with comment: %v %v", err, out != nil)
	}
	// larger than the box: shrunk like a PNG (contain keeps the ratio)
	out, err = convertPNGBytes(pgm(16, 8, func(x, y int) byte { return 200 }), "t.pgm", 8, 8, "contain")
	if err != nil || len(out) != 8*8*4 {
		t.Fatalf("shrink: %v len %d", err, len(out))
	}
	if px(out, 8, 0, 0)[3] != 0 || px(out, 8, 0, 3)[3] != 255 { // 8x4 centred: rows 2..5 filled
		t.Errorf("contain must centre the 8x4 picture: top %v row3 %v", px(out, 8, 0, 0), px(out, 8, 0, 3))
	}
}

func TestPNMRejects(t *testing.T) {
	for name, data := range map[string][]byte{
		"short pixels":  []byte("P5 4 2 255\n1234"),
		"too many":      []byte("P5 2 1 255\n123"),
		"16 bit":        []byte("P5 2 1 65535\n\x00\x01\x00\x02"),
		"no dims":       []byte("P6\n"),
		"zero size":     []byte("P5 0 0 255\n"),
		"huge":          []byte("P5 100000 100000 255\n"),
		"ascii variant": []byte("P2 2 1 255\n1 2\n"),
	} {
		if _, err := convertPNGBytes(data, name, 4, 4, "contain"); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
}

func TestBGRAPassesThroughAtTheBoxSize(t *testing.T) {
	raw := make([]byte, 4*2*4)
	for i := range raw {
		raw[i] = byte(i)
	}
	out, err := convertPNGBytes(raw, "t.bgra", 4, 2, "contain")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(raw) || out[5] != 5 {
		t.Errorf("BGRA must come through untouched: %v", out[:8])
	}
	// the wrong size cannot be a headerless picture for this box
	_, err = convertPNGBytes(raw, "t.bgra", 8, 2, "contain")
	if err == nil || !strings.Contains(err.Error(), "BGRA must be exactly 64 bytes") {
		t.Errorf("wrong size: %v", err)
	}
	// a BGRA frame whose first pixels look like a magic is still BGRA
	for name, head := range map[string][]byte{"pnm-like": []byte("P5 "), "jpeg-like": []byte("\xff\xd8\xff"), "png-like": []byte("\x89PNG")} {
		raw2 := append(append([]byte{}, head...), raw[len(head):]...)
		out, err := convertPNGBytes(raw2, "t.bgra", 4, 2, "contain")
		if err != nil || len(out) != len(raw2) || out[0] != head[0] {
			t.Errorf("%s: BGRA starting with a magic must pass through: %v", name, err)
		}
	}
	// a JPEG that happens to be w*h*4 bytes is still a JPEG (decoded, not passed through)
	j := testJPEG(t, 2, 2, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	for len(j)%64 != 0 {
		j = append(j, 0) // trailing bytes after EOI are ignored by the decoder
	}
	bw, bh := 16, len(j)/64 // bw*bh*4 == len(j)
	out, err = convertPNGBytes(j, "t.jpg", bw, bh, "contain")
	if err != nil || px(out, bw, bw/2, bh/2)[0] < 240 {
		t.Errorf("a %d-byte JPEG for a %dx%d box must be decoded as a JPEG, got %v", len(j), bw, bh, err)
	}
}

// over HTTP, like the remote camera sends it
func TestURLPicturePGM(t *testing.T) {
	ps := newPictureServer(t, pgm(8, 4, func(x, y int) byte { return 99 }))
	p := newURLPicture(SlotConfig{URL: ps.URL + "/image?w=8&h=4&fmt=pgm"}, 8, 4)
	p.fetchOnce(context.Background())
	var seq uint64
	pix, changed, err := p.latest(&seq)
	if err != nil || !changed || len(pix) != 8*4*4 || pix[0] != 99 || pix[3] != 255 {
		t.Fatalf("pgm over http: err %v changed %v len %d %v", err, changed, len(pix), pix[:4])
	}
	raw := make([]byte, 8*4*4)
	ps.body.Store(&raw)
	p.fetchOnce(context.Background())
	if pix, changed, err := p.latest(&seq); err != nil || !changed || len(pix) != 8*4*4 || pix[3] != 0 {
		t.Fatalf("bgra over http: err %v changed %v", err, changed)
	}
}
