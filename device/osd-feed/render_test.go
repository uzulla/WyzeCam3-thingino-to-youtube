package main

import (
	"testing"
	"time"
)

func timeIn(loc *time.Location) (string, int) {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, loc).Zone()
}

func TestAsciiCut(t *testing.T) {
	if got := asciiCut("ab\tc日本d", 6); got != "ab c??" {
		t.Errorf("got %q", got)
	}
	if got := asciiCut("abc", 0); got != "abc" {
		t.Errorf("cols 0 must not cut, got %q", got)
	}
}

func TestRenderCutsToGeometry(t *testing.T) {
	s, err := newSlot("textfile", SlotConfig{Template: "{{bar 12 .tick.pct}} {{lpad 3 .tick.pct}}%\nline2\nline3"}, SlotGeometry{Path: "x", Cols: 10, Rows: 2})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.render(map[string]any{"tick": map[string]any{"pct": 50}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "[#####----]\nline2\n"[:0]+"[#####----\nline2\n" {
		t.Errorf("got %q", out)
	}
}

func TestBarAcceptsFloatFromJSON(t *testing.T) {
	if got := funcs["bar"].(func(int, any) string)(6, 75.0); got != "[###-]" {
		t.Errorf("got %q", got)
	}
}

func TestUnknownSourceFails(t *testing.T) {
	s, _ := newSlot("textfile", SlotConfig{Template: "v={{.nothere.x}}"}, SlotGeometry{Cols: 40, Rows: 4})
	if _, err := s.render(newCache().snapshot(time.Now())); err == nil {
		t.Fatal("a source the config does not have must be an error (caught at start-up)")
	}
}

func TestRegisteredSourceRendersBlankUntilPolled(t *testing.T) {
	c := newCache()
	c.register("car")
	s, _ := newSlot("textfile", SlotConfig{Template: "v={{.car.speed}} ok={{.car.ok}} age={{.car.age_s}}"}, SlotGeometry{Cols: 40, Rows: 4})
	out, err := s.render(c.snapshot(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if out != "v= ok=false age=-1\n" {
		t.Errorf("got %q", out)
	}
}
