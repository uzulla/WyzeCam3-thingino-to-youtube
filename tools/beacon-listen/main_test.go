package main

import (
	"net"
	"testing"
	"time"
)

// as device/beacon sends it on the camera
const sample = `{"type":"thingino-beacon","v":1,"seq":1,"host":"ing-wyze-cam3-9793","iface":"wlan0","ip":"192.168.11.29","mac":"02:b4:91:43:97:94","build":"ciao+da40db6","uptime_s":943,"stream":{"state":"streaming","elapsed_s":866},"rssi_dbm":-26,"mem_free_mb":68,"load1":"2.08"}`

func TestParseAndFormat(t *testing.T) {
	m, ok := parse([]byte(sample))
	if !ok {
		t.Fatal("sample not accepted")
	}
	now := time.Date(2026, 10, 21, 13, 7, 1, 0, time.Local)
	got := format(now, m, net.IPv4(192, 168, 11, 29))
	want := "13:07:01  ing-wyze-cam3-9793        192.168.11.29    streaming 14m26s      wifi -26dBm  up 15m43s  mem 68MB  load 2.08  ciao+da40db6"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}

	// without the optional fields, and without an ip: the sender's address
	m, ok = parse([]byte(`{"type":"thingino-beacon","v":1,"host":"cam","uptime_s":5,"stream":{"state":"stopped","elapsed_s":null}}`))
	if !ok {
		t.Fatal("minimal message not accepted")
	}
	got = format(now, m, net.IPv4(10, 0, 0, 7))
	want = "13:07:01  cam                       10.0.0.7         stopped               up 5s"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{"", "hello", `{"type":"other"}`, `[1,2]`} {
		if _, ok := parse([]byte(s)); ok {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestDur(t *testing.T) {
	cases := map[int64]string{-1: "?", 0: "0s", 45: "45s", 866: "14m26s", 3*3600 + 2*60 + 9: "3h02m", 2*86400 + 3*3600: "2d03h"}
	for s, want := range cases {
		if got := dur(s); got != want {
			t.Errorf("dur(%d) = %q, want %q", s, got, want)
		}
	}
}
