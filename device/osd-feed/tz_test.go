package main

import "testing"

func TestParsePosixTZ(t *testing.T) {
	cases := []struct {
		spec string
		name string
		east int
		dst  bool
		err  bool
	}{
		{"JST-9", "JST", 9 * 3600, false, false},
		{"UTC0", "UTC", 0, false, false},
		{"EST5EDT,M3.2.0,M11.1.0", "EST", -5 * 3600, true, false},
		{"<+0530>-5:30", "+0530", 5*3600 + 30*60, false, false},
		{"CET-1CEST", "CET", 3600, true, false},
		{"", "", 0, false, true},
		{"JST", "", 0, false, true},
		{"-9", "", 0, false, true},
		{"JST-9::30", "", 0, false, true},
		{"JST-9:", "", 0, false, true},
		{"JST-9:30:00:1", "", 0, false, true},
		{"JST-9:x", "", 0, false, true},
	}
	for _, c := range cases {
		loc, dst, err := parsePosixTZ(c.spec)
		if (err != nil) != c.err {
			t.Errorf("%q: err=%v", c.spec, err)
			continue
		}
		if err != nil {
			continue
		}
		name, off := loc.String(), 0
		_, off = timeIn(loc)
		if name != c.name || off != c.east || dst != c.dst {
			t.Errorf("%q: got %s %d dst=%v, want %s %d dst=%v", c.spec, name, off, dst, c.name, c.east, c.dst)
		}
	}
}
