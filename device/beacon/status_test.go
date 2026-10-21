package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestParsers(t *testing.T) {
	if got := parseUptime("12345.67 23456.78\n"); got != 12345 {
		t.Errorf("uptime = %d", got)
	}
	if got := parseUptime(""); got != -1 {
		t.Errorf("empty uptime = %d", got)
	}
	osr := "NAME=\"Thingino\"\nBUILD_ID=\"ciao+da40db6, 2026-09-14 22:14:14 UTC\"\n"
	if got := parseBuildID(osr); got != "ciao+da40db6" {
		t.Errorf("build = %q", got)
	}
	mem := "MemTotal:  95580 kB\nMemFree:   38500 kB\nBuffers:    2048 kB\nCached:    32388 kB\n"
	if got := parseMemFreeMB(mem); got == nil || *got != 71 {
		t.Errorf("mem = %v", got)
	}
	if got := parseMemFreeMB("garbage"); got != nil {
		t.Errorf("mem of garbage = %v", *got)
	}
	if got := parseLoad1("0.52 0.40 0.35 1/80 1234\n"); got == nil || *got != "0.52" {
		t.Errorf("load1 = %v", got)
	}
	wpa := "RSSI=-27\nLINKSPEED=65\nNOISE=9999\nFREQUENCY=2412\nAVG_RSSI=-27\n"
	if got := parseRSSI(wpa); got == nil || *got != -27 {
		t.Errorf("rssi = %v", got)
	}
	if got := parseRSSI("FAIL\n"); got != nil {
		t.Errorf("rssi of FAIL = %v", *got)
	}
}

func TestParseStat(t *testing.T) {
	// a command name with spaces and a ')' must not shift the fields
	s := "321 (my (odd) cmd) S 1176 321 321 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 1 0 54321 1000 200\n"
	ppid, start, ok := parseStat(s)
	if !ok || ppid != 1176 || start != 54321 {
		t.Errorf("got %d %d %v", ppid, start, ok)
	}
	if _, _, ok := parseStat("321 (short) S 1"); ok {
		t.Error("short stat accepted")
	}
}

// fakeProc - a /proc with the given processes: pid -> {ppid, start ticks, cmdline}
func fakeProc(t *testing.T, procs map[string][3]string) string {
	dir := t.TempDir()
	for pid, p := range procs {
		d := filepath.Join(dir, pid)
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		stat := pid + " (x) S " + p[0] + " 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 " + p[1] + " 0 0\n"
		if err := os.WriteFile(filepath.Join(d, "stat"), []byte(stat), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "cmdline"), []byte(p[2]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFindStream(t *testing.T) {
	relayCmd := "/bin/sh\x00/usr/sbin/youtube-relay\x00"
	ffCmd := "/mnt/mmcblk0p1/ffmpeg\x00-nostdin\x00-hide_banner\x00-loglevel\x00warning\x00"
	pidfile := filepath.Join(t.TempDir(), "relay.pid")
	if err := os.WriteFile(pidfile, []byte("100\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		procs  map[string][3]string
		uptime int64
		state  string
		el     int64
	}{
		{"no relay", map[string][3]string{}, 1000, "stopped", -1},
		{"pid reused by something else", map[string][3]string{"100": {"1", "0", "/sbin/udhcpc\x00"}}, 1000, "stopped", -1},
		{"relay without ffmpeg", map[string][3]string{
			"100": {"1", "0", relayCmd},
			"200": {"100", "50000", "logger\x00-t\x00youtube-relay\x00"}, // a child, but not the ffmpeg
			"300": {"1", "50000", ffCmd},                                 // an ffmpeg, but not the relay's
		}, 1000, "idle", -1},
		{"just started", map[string][3]string{
			"100": {"1", "0", relayCmd},
			"200": {"100", "95000", ffCmd},
		}, 1000, "connecting", 50},
		{"streaming", map[string][3]string{
			"100": {"1", "0", relayCmd},
			"200": {"100", "40000", ffCmd},
		}, 1000, "streaming", 600},
	}
	for _, c := range cases {
		st := findStream(fakeProc(t, c.procs), pidfile, c.uptime)
		if st.State != c.state {
			t.Errorf("%s: state %q, want %q", c.name, st.State, c.state)
		}
		switch {
		case c.el < 0 && st.ElapsedS != nil:
			t.Errorf("%s: elapsed %d, want none", c.name, *st.ElapsedS)
		case c.el >= 0 && (st.ElapsedS == nil || *st.ElapsedS != c.el):
			t.Errorf("%s: elapsed %v, want %d", c.name, st.ElapsedS, c.el)
		}
	}
	// no uptime: an ffmpeg that has run long, but the time is unknown
	long := fakeProc(t, map[string][3]string{"100": {"1", "0", relayCmd}, "200": {"100", "40000", ffCmd}})
	if st := findStream(long, pidfile, -1); st.State != "connecting" || st.ElapsedS != nil {
		t.Errorf("no uptime: %q %v", st.State, st.ElapsedS)
	}
	if st := findStream(t.TempDir(), filepath.Join(t.TempDir(), "none.pid"), 1000); st.State != "stopped" {
		t.Errorf("no pidfile: %q", st.State)
	}
}

func TestBroadcastAddr(t *testing.T) {
	cases := map[string]string{
		"192.168.11.29/24": "192.168.11.255",
		"10.1.2.3/8":       "10.255.255.255",
		"172.16.5.4/30":    "172.16.5.7",
		"10.0.0.1/32":      "",
		"10.0.0.1/31":      "",
		"fe80::1/64":       "",
	}
	for cidr, want := range cases {
		ip, n, _ := net.ParseCIDR(cidr)
		n.IP = ip
		got := broadcastAddr(n)
		if (want == "" && got != nil) || (want != "" && got.String() != want) {
			t.Errorf("%s: got %v, want %q", cidr, got, want)
		}
	}
}
