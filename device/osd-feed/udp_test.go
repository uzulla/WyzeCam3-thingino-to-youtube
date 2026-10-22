package main

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// a racebox datagram as esp32-racebox sends it (shortened)
const raceboxJSON = `{"type":"racebox","seq":7,"speed_mps":12.5,"g":{"x":-0.4,"y":0.9,"z":0.98}}`

func TestUDPSourceReceivesMatchingObjects(t *testing.T) {
	src, err := newUDPSource(SourceConfig{Listen: "127.0.0.1:0", Match: map[string]string{"type": "racebox"}, StaleMs: 300})
	if err != nil {
		t.Fatal(err)
	}
	u := src.(*udpSource)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := u.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Poll(ctx); err == nil || !strings.Contains(err.Error(), "yet") {
		t.Fatalf("before any datagram: %v", err)
	}
	// Start bound an ephemeral port: find it by sending to the same socket
	// address the listener reports
	addr := u.conn.LocalAddr().String()
	c, err := net.Dial("udp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Only the newest datagram is parsed at each poll: send one, poll
	send := func(s string) {
		c.Write([]byte(s))
		time.Sleep(20 * time.Millisecond)
		u.Poll(ctx)
	}
	send(`not json`)
	send(`{"type":"status","rssi":-50}`) // valid, but filtered out
	if _, err := u.Poll(ctx); err == nil {
		t.Fatal("garbage and status must not count as received")
	}
	c.Write([]byte(raceboxJSON))
	var v Values
	for i := 0; i < 50; i++ {
		if v, err = u.Poll(ctx); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if v["g_x"] != json.Number("-0.4") || v["g_y"] != json.Number("0.9") || v["speed_mps"] != json.Number("12.5") || v["type"] != "racebox" {
		t.Errorf("got %v", v)
	}
	// a status message afterwards must not replace the racebox values
	send(`{"type":"status","rssi":-50}`)
	time.Sleep(50 * time.Millisecond)
	if v, _ = u.Poll(ctx); v["g_x"] != json.Number("-0.4") {
		t.Errorf("status replaced the values: %v", v)
	}
	// stale after stale_ms without a match
	time.Sleep(350 * time.Millisecond)
	if _, err := u.Poll(ctx); err == nil || !strings.Contains(err.Error(), "for") {
		t.Errorf("must be stale: %v", err)
	}
}

func TestUDPSourceNeedsListenAndBindErrors(t *testing.T) {
	if _, err := newUDPSource(SourceConfig{}); err == nil {
		t.Error("no listen must fail")
	}
	src, _ := newUDPSource(SourceConfig{Listen: "127.0.0.1:0"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := src.(*udpSource).Start(ctx); err != nil {
		t.Fatal(err)
	}
	taken := src.(*udpSource).conn.LocalAddr().String()
	other, _ := newUDPSource(SourceConfig{Listen: taken})
	if err := other.(*udpSource).Start(ctx); err == nil {
		t.Error("a port in use must fail at Start")
	}
}
