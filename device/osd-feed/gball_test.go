package main

import (
	"bytes"
	"os"
	"testing"
)

func snap(ok bool, lat, lon any) map[string]any {
	return map[string]any{"rb": map[string]any{"ok": ok, "g_y": lat, "g_x": lon}}
}

// ballAt - the centre of the red ball's pixels in a w x h BGRA canvas
func ballAt(t *testing.T, pix []byte, w, h int) (int, int, bool) {
	t.Helper()
	sx, sy, n := 0, 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := px(pix, w, x, y)
			if p[3] == 255 && p[2] > 200 && p[1] < 100 && p[0] < 100 { // opaque red
				sx, sy, n = sx+x, sy+y, n+1
			}
		}
	}
	if n == 0 {
		return 0, 0, false
	}
	return sx / n, sy / n, true
}

func TestGBallDirections(t *testing.T) {
	const S = 160
	c := S / 2
	for name, tc := range map[string]struct {
		lat, lon float64
		inv      [2]bool
		dx, dy   int // expected sign of the ball's offset from the centre
	}{
		"braking goes up":        {0, -1, [2]bool{}, 0, -1},
		"acceleration goes down": {0, 0.5, [2]bool{}, 0, 1},
		"right turn goes left":   {1, 0, [2]bool{}, -1, 0},
		"left turn goes right":   {-1, 0, [2]bool{}, 1, 0},
		"invert lateral":         {1, 0, [2]bool{true, false}, 1, 0},
		"invert longitudinal":    {0, -1, [2]bool{false, true}, 0, 1},
	} {
		zero := 0
		g := newGBall(GBallConfig{Source: "rb", Trail: &zero, InvertLateral: tc.inv[0], InvertLongitudinal: tc.inv[1]}, S)
		pix := make([]byte, S*S*4)
		g.draw(pix, S, S, g.place(snap(true, tc.lat, tc.lon)))
		x, y, ok := ballAt(t, pix, S, S)
		if !ok {
			t.Fatalf("%s: no ball", name)
		}
		sign := func(v int) int {
			switch {
			case v > 3:
				return 1
			case v < -3:
				return -1
			}
			return 0
		}
		if sign(x-c) != tc.dx || sign(y-c) != tc.dy {
			t.Errorf("%s: ball at %d,%d (centre %d)", name, x, y, c)
		}
	}
}

func TestGBallClampsToTheRim(t *testing.T) {
	const S = 160
	zero := 0
	g := newGBall(GBallConfig{Source: "rb", Trail: &zero}, S)
	pix := make([]byte, S*S*4)
	g.draw(pix, S, S, g.place(snap(true, 0.0, -5.0))) // 5 G braking, max_g 1.5
	_, y, ok := ballAt(t, pix, S, S)
	if !ok || y < g.ballRadius() || y > S/2 {
		t.Errorf("ball must sit inside the top of the rim, y=%d ok=%v", y, ok)
	}
}

func TestGBallHiddenWithoutData(t *testing.T) {
	const S = 64
	g := newGBall(GBallConfig{Source: "rb"}, S)
	for name, data := range map[string]map[string]any{
		"not ok":       snap(false, 0.5, 0.5),
		"no source":    {},
		"missing keys": {"rb": map[string]any{"ok": true}},
		"not numbers":  snap(true, "0.5", nil),
	} {
		pix := make([]byte, S*S*4)
		g.draw(pix, S, S, g.place(data))
		for i := range pix {
			if pix[i] != 0 {
				t.Errorf("%s: something was drawn", name)
				break
			}
		}
	}
}

func TestGBallTrailIsForgottenWhenDataStops(t *testing.T) {
	const S = 64
	g := newGBall(GBallConfig{Source: "rb"}, S)
	pix := make([]byte, S*S*4)
	for i := 0; i < 20; i++ {
		g.draw(pix, S, S, g.place(snap(true, 0.1*float64(i%5), 0.0)))
	}
	if len(g.hist) != 8 {
		t.Fatalf("trail keeps the last 8, has %d", len(g.hist))
	}
	g.draw(pix, S, S, g.place(snap(false, 0.0, 0.0)))
	if len(g.hist) != 0 {
		t.Error("trail must be dropped while hidden")
	}
}

func TestImageSlotPNGAndGBall(t *testing.T) {
	p := writeTestPNG(t, 8, 4) // left half opaque red, right half translucent blue
	zero := 0
	cfg := SlotConfig{PNG: p, PNGBox: &Box{X: 0, Y: 70, Width: 8, Height: 4},
		GBall: &GBallConfig{Source: "rb", Size: 64, X: 0, Y: 0, Trail: &zero}}
	s := newImageSlot("imagefile", cfg, SlotGeometry{Path: "/tmp/x", Width: 64, Height: 74})
	pix, changed, err := s.render(snap(false, 0, 0))
	if err != nil || !changed || len(pix) != 64*74*4 {
		t.Fatalf("first: %v changed=%v len=%d", err, changed, len(pix))
	}
	if p := px(pix, 64, 0, 70); p != [4]byte{0, 0, 255, 255} {
		t.Errorf("PNG must be in its box at 0,70: %v", p)
	}
	if p := px(pix, 64, 32, 32); p[3] == 0 {
		t.Error("rings must be drawn without data")
	}
	if _, _, ok := ballAt(t, pix[:64*64*4], 64, 64); ok {
		t.Error("no ball without data")
	}
	if _, changed, _ = s.render(snap(false, 0, 0)); changed {
		t.Error("same picture must not count as a change")
	}
	pix, changed, _ = s.render(snap(true, 0.0, 0.0))
	if _, _, ok := ballAt(t, pix[:64*64*4], 64, 64); !ok || !changed {
		t.Errorf("ball must show with data (changed=%v)", changed)
	}
	// the PNG gone: the G-ball stays, the PNG's box goes blank
	os.Remove(p)
	pix, _, err = s.render(snap(true, 0.0, 0.0))
	if err == nil || pix == nil {
		t.Fatalf("missing PNG: err=%v pix=%v", err, pix != nil)
	}
	if p := px(pix, 64, 0, 70); p[3] != 0 {
		t.Errorf("PNG box must be empty now: %v", p)
	}
}

func TestImageSlotGBallOnly(t *testing.T) {
	cfg := SlotConfig{GBall: &GBallConfig{Source: "rb"}}
	s := newImageSlot("imagefile", cfg, SlotGeometry{Path: "/tmp/x", Width: 100, Height: 80})
	pix, _, err := s.render(snap(true, 0.0, 0.0))
	if err != nil || len(pix) != 100*80*4 {
		t.Fatalf("%v len=%d", err, len(pix))
	}
	if s.widgets[0].(*gball).size != 80 {
		t.Errorf("default size must fit the slot: %d", s.widgets[0].(*gball).size)
	}
	// a resize makes a new G-ball of the new size
	if !s.follow(SlotGeometry{Path: "/tmp/x", Width: 120, Height: 120}, cfg) {
		t.Fatal("resize must count")
	}
	pix, _, _ = s.render(snap(true, 0.0, 0.0))
	if len(pix) != 120*120*4 || s.widgets[0].(*gball).size != 120 {
		t.Errorf("after resize: len=%d size=%d", len(pix), s.widgets[0].(*gball).size)
	}
}

// Repainting only around the ball must leave exactly what a full redraw
// would: no ball (or trail) left behind where it was
func TestImageSlotRepaintLeavesNothingBehind(t *testing.T) {
	p := writeTestPNG(t, 8, 4)
	cfg := SlotConfig{PNG: p, PNGBox: &Box{X: 0, Y: 64, Width: 8, Height: 4},
		GBall: &GBallConfig{Source: "rb", Size: 64}}
	s := newImageSlot("imagefile", cfg, SlotGeometry{Path: "/tmp/x", Width: 64, Height: 68})
	steps := []map[string]any{
		snap(true, 0.0, 0.0), snap(true, 1.0, -1.0), snap(true, -1.4, 0.3),
		snap(false, 0.0, 0.0), snap(true, 0.5, 0.5), snap(true, 0.5, 0.5),
	}
	ref := newGBall(*cfg.GBall, 64) // the same trail history, drawn from scratch
	for i, d := range steps {
		pix, _, err := s.render(d)
		if err != nil {
			t.Fatal(err)
		}
		want := append([]byte(nil), s.base...)
		ref.draw(want, 64, 68, ref.place(d))
		if !bytes.Equal(pix, want) {
			t.Fatalf("step %d: repainted frame differs from a full redraw", i)
		}
	}
	zero := 0
	s2 := newImageSlot("imagefile", SlotConfig{GBall: &GBallConfig{Source: "rb", Trail: &zero}}, SlotGeometry{Path: "/tmp/x", Width: 64, Height: 64})
	s2.render(snap(true, 0.2, 0.2))
	if _, changed, _ := s2.render(snap(true, 0.2, 0.2)); changed {
		t.Error("ball in the same place without a trail must not be a change")
	}
}
