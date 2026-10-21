package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
)

// fakePrudynt - answers like prudynt: a null asks for the current value, a
// value sets it (and is echoed back)
type fakePrudynt struct {
	image map[string]bool
	sent  []string
	down  bool
}

func (f *fakePrudynt) call(_ context.Context, body string) (string, error) {
	if f.down {
		return "", errors.New("prudyntctl: exit status 1 (is prudynt running?)")
	}
	f.sent = append(f.sent, body)
	var req struct {
		Image map[string]any `json:"image"`
	}
	if json.Unmarshal([]byte(body), &req) != nil || req.Image == nil {
		return "{}", nil
	}
	out := map[string]any{}
	for k, v := range req.Image {
		if b, ok := v.(bool); ok {
			f.image[k] = b
		}
		out[k] = f.image[k]
	}
	b, _ := json.Marshal(map[string]any{"image": out})
	return string(b), nil
}

func newTestCommander() (*commander, *fakePrudynt) {
	f := &fakePrudynt{image: map[string]bool{"vflip": true, "hflip": true}}
	return &commander{
		host:    "ing-wyze-cam3-9793",
		status:  func(net.IP) Message { return Message{Type: msgType, Host: "ing-wyze-cam3-9793", IP: "192.168.11.29"} },
		prudynt: f.call,
	}, f
}

var pc = &net.UDPAddr{IP: net.IPv4(192, 168, 11, 5), Port: 50000}

func answer(t *testing.T, c *commander, pkt string) (Result, bool) {
	t.Helper()
	b := c.handle([]byte(pkt), pc)
	if b == nil {
		return Result{}, false
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("answer is not JSON: %s", b)
	}
	return r, true
}

func TestImageCommand(t *testing.T) {
	c, f := newTestCommander()
	r, _ := answer(t, c, `{"type":"thingino-command","v":1,"id":"a","cmd":"image","args":{"vflip":"toggle"}}`)
	if !r.OK || r.ID != "a" || r.Host != "ing-wyze-cam3-9793" || r.Cmd != "image" {
		t.Fatalf("toggle: %+v", r)
	}
	if f.image["vflip"] != false || f.image["hflip"] != true {
		t.Errorf("after toggle: %v", f.image)
	}
	if got := f.sent; len(got) != 2 || got[0] != `{"image":{"vflip":null}}` || got[1] != `{"image":{"vflip":false}}` {
		t.Errorf("sent %q", got)
	}

	r, _ = answer(t, c, `{"type":"thingino-command","v":1,"id":"b","cmd":"image","args":{"vflip":true,"hflip":false}}`)
	if !r.OK || f.image["vflip"] != true || f.image["hflip"] != false {
		t.Errorf("set: %+v, %v", r, f.image)
	}
}

func TestResentCommandRunsOnce(t *testing.T) {
	c, f := newTestCommander()
	cmd := `{"type":"thingino-command","v":1,"id":"same","cmd":"image","args":{"vflip":"toggle"}}`
	first := c.handle([]byte(cmd), pc)
	again := c.handle([]byte(cmd), pc)
	if string(first) != string(again) {
		t.Errorf("different answers:\n%s\n%s", first, again)
	}
	if f.image["vflip"] != false || len(f.sent) != 2 {
		t.Errorf("toggled twice? %v, sent %q", f.image, f.sent)
	}
	// the same id from another sender is another command
	other := &net.UDPAddr{IP: net.IPv4(192, 168, 11, 6), Port: 50000}
	c.handle([]byte(cmd), other)
	if f.image["vflip"] != true {
		t.Errorf("other sender's command not run: %v", f.image)
	}
}

func TestCommandErrors(t *testing.T) {
	cases := map[string]string{
		`{"type":"thingino-command","v":1,"cmd":"reboot"}`:                                    "unknown command",
		`{"type":"thingino-command","v":2,"cmd":"ping"}`:                                      "unsupported version",
		`{"type":"thingino-command","v":1,"cmd":"image"}`:                                     "args must be",
		`{"type":"thingino-command","v":1,"cmd":"image","args":{"brightness":10}}`:            "cannot be changed",
		`{"type":"thingino-command","v":1,"cmd":"image","args":{"vflip":"yes"}}`:              "toggle",
		`{"type":"thingino-command","v":1,"cmd":"image","args":{"vflip":1}}`:                  "toggle",
		`{"type":"thingino-command","v":1,"cmd":"image","args":{"vflip":true,"rm -rf":true}}`: "cannot be changed",
	}
	for pkt, want := range cases {
		c, f := newTestCommander()
		r, ok := answer(t, c, pkt)
		if !ok || r.OK || !strings.Contains(r.Error, want) {
			t.Errorf("%s: %+v, want an error with %q", pkt, r, want)
		}
		if len(f.sent) != 0 {
			t.Errorf("%s: sent %q to prudynt", pkt, f.sent)
		}
	}

	c, f := newTestCommander()
	f.down = true
	r, _ := answer(t, c, `{"type":"thingino-command","v":1,"cmd":"image","args":{"vflip":true}}`)
	if r.OK || !strings.Contains(r.Error, "prudynt") {
		t.Errorf("prudynt down: %+v", r)
	}
}

func TestNotForUs(t *testing.T) {
	c, _ := newTestCommander()
	for _, pkt := range []string{
		``, `hello`, `{"type":"thingino-beacon","v":1}`,
		`{"type":"thingino-command","v":1,"host":"other-cam","cmd":"ping"}`,
	} {
		if _, ok := answer(t, c, pkt); ok {
			t.Errorf("%q answered", pkt)
		}
	}
	// the name is matched without case
	r, ok := answer(t, c, `{"type":"thingino-command","v":1,"host":"ING-WYZE-CAM3-9793","cmd":"ping"}`)
	if !ok || !r.OK {
		t.Errorf("ping with host: %+v", r)
	}
	if m, _ := r.Result.(map[string]any); m["ip"] != "192.168.11.29" {
		t.Errorf("ping result: %v", r.Result)
	}
}
