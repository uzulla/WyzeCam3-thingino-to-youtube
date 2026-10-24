package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"
)

// lap - a position on an oval circuit (about 1000 m x 400 m) near Fuji
// Speedway, f = 0..1 around it
func lap(f float64) [2]float64 {
	a := 2 * math.Pi * f
	return [2]float64{35.37 + 0.0018*math.Sin(a), 138.93 + 0.0055*math.Cos(a)}
}

func gps(ok bool, p [2]float64) map[string]any {
	lat, lon := p[0], p[1]
	return map[string]any{"rb": map[string]any{"ok": ok, "latlon_valid": true,
		"lat": json.Number(fmt.Sprintf("%.7f", lat)), "lon": json.Number(fmt.Sprintf("%.7f", lon))}}
}

// trackSlot - a slot with only a track map, rendered from the snapshots
func trackSlot(cfg TrackConfig, w, h int) *ImageSlot {
	return &ImageSlot{Name: "imagefile3", Width: w, Height: h, tcfg: &cfg}
}

// fresh - what the slot shows when drawn from scratch now: the incremental
// repaints must always end up at the same picture
func fresh(s *ImageSlot) []byte {
	tr := s.widgets[0].(*track)
	f := append([]byte(nil), s.base...)
	tr.full, tr.drawnAt = true, [2]int{-1, -1}
	tr.repaint(f, s.base, s.Width, s.Height)
	return f
}

func trackPixels(pix []byte) int {
	n := 0
	for i := 0; i < len(pix); i += 4 {
		if pix[i] > 200 && pix[i+1] > 200 && pix[i+2] > 200 { // white
			n++
		}
	}
	return n
}

func TestTrackDrawsTheCircuitAndMatchesAFullRedraw(t *testing.T) {
	const W, H = 200, 120
	zero := 0.0
	s := trackSlot(TrackConfig{Source: "rb", MinMoveM: &zero}, W, H)
	var pix []byte
	for i := 0; i < 600; i++ { // two laps
		var err error
		if pix, _, err = s.render(gps(true, lap(float64(i)/300))); err != nil {
			t.Fatal(err)
		}
		if i%37 == 0 && !bytes.Equal(pix, fresh(s)) {
			t.Fatalf("step %d: the incremental picture differs from a full redraw", i)
		}
	}
	if !bytes.Equal(pix, fresh(s)) {
		t.Fatal("the incremental picture differs from a full redraw")
	}
	// The oval spans the width (wider than tall): the leftmost and rightmost
	// white columns are near the edges
	x0, x1 := W, 0
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			if p := px(pix, W, x, y); p[0] > 200 && p[1] > 200 && p[2] > 200 {
				x0, x1 = min(x0, x), max(x1, x)
			}
		}
	}
	if x0 > 25 || x1 < W-25 { // a margin of 10% on each side
		t.Errorf("track spans x %d..%d of %d", x0, x1, W)
	}
	if _, _, ok := ballAt(t, pix, W, H); !ok {
		t.Error("no marker at the latest position")
	}
}

func TestTrackRubsOutOldPoints(t *testing.T) {
	const W, H = 160, 160
	zero := 0.0
	s := trackSlot(TrackConfig{Source: "rb", MinMoveM: &zero, Points: 50}, W, H)
	for i := 0; i < 300; i++ {
		s.render(gps(true, lap(float64(i)/300)))
	}
	pix, _, _ := s.render(gps(true, lap(0.999)))
	tr := s.widgets[0].(*track)
	if tr.n != 50 {
		t.Errorf("%d points kept, want 50", tr.n)
	}
	if !bytes.Equal(pix, fresh(s)) {
		t.Fatal("the incremental picture differs from a full redraw")
	}
	// 50 of 300 points: about a sixth of the lap is left
	all := trackSlot(TrackConfig{Source: "rb", MinMoveM: &zero}, W, H)
	for i := 0; i < 300; i++ {
		all.render(gps(true, lap(float64(i)/300)))
	}
	full, _, _ := all.render(gps(true, lap(0.999)))
	if a, b := trackPixels(pix), trackPixels(full); a*3 > b {
		t.Errorf("%d track pixels with 50 points, %d with all", a, b)
	}
}

func TestTrackHidesTheMarkerButKeepsTheMap(t *testing.T) {
	const W, H = 160, 160
	s := trackSlot(TrackConfig{Source: "rb"}, W, H)
	for i := 0; i < 300; i++ {
		s.render(gps(true, lap(float64(i)/300)))
	}
	before := trackPixels(s.frame)
	pix, changed, _ := s.render(gps(false, lap(0.5)))
	if _, _, ok := ballAt(t, pix, W, H); ok || !changed {
		t.Error("the marker must go while the source is not fresh")
	}
	if trackPixels(pix) < before*9/10 {
		t.Error("the map must stay")
	}
	if !bytes.Equal(pix, fresh(s)) {
		t.Fatal("the incremental picture differs from a full redraw")
	}
	// not valid (no fix): not plotted, no marker
	n := s.widgets[0].(*track).n
	snap := gps(true, [2]float64{35.0, 139.0})
	snap["rb"].(map[string]any)["latlon_valid"] = false
	pix, _, _ = s.render(snap)
	if s.widgets[0].(*track).n != n {
		t.Error("an invalid position must not be plotted")
	}
	if _, _, ok := ballAt(t, pix, W, H); ok {
		t.Error("no marker for an invalid position")
	}
}

func TestTrackStandingStillAndJumps(t *testing.T) {
	s := trackSlot(TrackConfig{Source: "rb"}, 160, 160)
	for i := 0; i < 100; i++ {
		s.render(gps(true, [2]float64{35.37, 138.93})) // in the pit
	}
	tr := s.widgets[0].(*track)
	if tr.n != 1 {
		t.Errorf("standing still: %d points, want 1", tr.n)
	}
	s.render(gps(true, [2]float64{35.37, 138.9301})) // ~9 m
	if tr.n != 2 {
		t.Errorf("after moving: %d points, want 2", tr.n)
	}
	s.render(gps(true, [2]float64{34.84, 136.54})) // another circuit (Suzuka): a new map
	if tr.n != 1 {
		t.Errorf("after a jump: %d points, want 1", tr.n)
	}
	pix, _, _ := s.render(gps(true, [2]float64{34.84, 136.54}))
	if _, _, ok := ballAt(t, pix, 160, 160); !ok {
		t.Error("no marker after a jump")
	}
}

// The view zooms back in once the far-away part of the track has been
// rubbed out (a long straight driven once, then laps on a small part)
func TestTrackZoomsInWhenOldPointsGo(t *testing.T) {
	zero := 0.0
	s := trackSlot(TrackConfig{Source: "rb", MinMoveM: &zero, Points: 200}, 160, 160)
	for i := 0; i < 100; i++ {
		s.render(gps(true, [2]float64{35.37, 138.93 + 0.0001*float64(i)})) // ~900 m east
	}
	tr := s.widgets[0].(*track)
	k := tr.k
	small := func(f float64) [2]float64 {
		p := lap(f)
		return [2]float64{35.37 + (p[0]-35.37)/10, 138.93 + (p[1]-138.93)/10}
	}
	for i := 0; i < 600; i++ {
		s.render(gps(true, small(float64(i)/100)))
	}
	if tr.k < 3*k {
		t.Errorf("scale %g, was %g on the straight: must zoom in", tr.k, k)
	}
	pix, _, _ := s.render(gps(true, small(0)))
	if !bytes.Equal(pix, fresh(s)) {
		t.Fatal("the incremental picture differs from a full redraw")
	}
}

// A slot with a PNG and a track map: a new PNG rebuilds the base, and the
// map drawn so far must come back with it (it is not only redrawn where it
// changed)
func TestTrackSurvivesANewPNG(t *testing.T) {
	const W, H = 160, 200
	p := writeTestPNG(t, 8, 8)
	zero := 0.0
	s := &ImageSlot{Name: "imagefile3", Width: W, Height: H, PNG: p, Fit: "contain",
		box: &Box{X: 0, Y: 0, Width: W, Height: 40}, tcfg: &TrackConfig{Source: "rb", MinMoveM: &zero, Y: 40}}
	for i := 0; i < 200; i++ {
		s.render(gps(true, lap(float64(i)/200)))
	}
	before := trackPixels(s.frame)
	// another picture at the same path (a different size: other bytes)
	q := writeTestPNG(t, 6, 6)
	b, _ := os.ReadFile(q)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, time.Now().Add(time.Second), time.Now().Add(time.Second))
	pix, changed, _ := s.render(gps(true, lap(0)))
	if !changed {
		t.Fatal("a new PNG must count as a change")
	}
	if !bytes.Equal(pix, fresh(s)) {
		t.Fatal("the picture after a new PNG differs from a full redraw")
	}
	if after := trackPixels(pix); after < before*9/10 {
		t.Errorf("track pixels %d after the new PNG, %d before", after, before)
	}
}
