package main

import (
	"encoding/json"
	"testing"
)

func TestBuild(t *testing.T) {
	c, err := build([]string{"image", "vflip=toggle", "hflip=false"}, "cam")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ID) != 16 {
		t.Errorf("id %q", c.ID)
	}
	c.ID = "x"
	b, _ := json.Marshal(c)
	want := `{"type":"thingino-command","v":1,"id":"x","host":"cam","cmd":"image","args":{"hflip":false,"vflip":"toggle"}}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}

	c, _ = build([]string{"ping"}, "")
	c.ID = "x"
	b, _ = json.Marshal(c)
	if string(b) != `{"type":"thingino-command","v":1,"id":"x","cmd":"ping"}` {
		t.Errorf("ping: %s", b)
	}

	for _, bad := range []string{"vflip", "=true"} {
		if _, err := build([]string{"image", bad}, ""); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValue(t *testing.T) {
	if value("true") != true || value("false") != false || value("10") != 10.0 || value("toggle") != "toggle" {
		t.Error("value")
	}
}
