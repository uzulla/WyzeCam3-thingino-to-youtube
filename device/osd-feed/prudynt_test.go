package main

import "testing"

func defaults() map[string]SlotGeometry {
	return map[string]SlotGeometry{"textfile": defaultGeometry["textfile"], "textfile2": defaultGeometry["textfile2"]}
}

func TestParseGeometry(t *testing.T) {
	// stock prudynt / not up yet
	for _, raw := range []string{"", "{}", "garbage"} {
		g, ok := parseGeometry([]byte(raw), defaults())
		if ok || g["textfile"].Cols != 40 {
			t.Errorf("%q: ok=%v %+v", raw, ok, g["textfile"])
		}
	}
	// patched prudynt: one slot resized, the other missing from the answer
	raw := `{"textfile":{"enabled":true,"cols":52,"rows":25,"pos_x":8,"path":"/run/prudynt/osd-text","scale":0}}`
	g, ok := parseGeometry([]byte(raw), defaults())
	if !ok || g["textfile"].Cols != 52 || g["textfile"].Rows != 25 || !g["textfile"].Enabled {
		t.Errorf("got ok=%v %+v", ok, g["textfile"])
	}
	if g["textfile2"] != defaultGeometry["textfile2"] {
		t.Errorf("slot not in the answer must keep its default, got %+v", g["textfile2"])
	}
	// a slot we did not ask for, and one without a path, are ignored
	raw = `{"textfile3":{"path":"/x","cols":1},"textfile":{"cols":9}}`
	g, ok = parseGeometry([]byte(raw), defaults())
	if !ok || len(g) != 2 || g["textfile"].Cols != 40 {
		t.Errorf("got ok=%v %+v", ok, g)
	}
}
