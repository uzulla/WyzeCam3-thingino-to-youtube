package main

// Tests of the pure checks that stand between the phone and the camera's
// files and commands: paths, input limits, the key redaction, /proc parsing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeRel(t *testing.T) {
	root := "/mnt/sd/cam"
	ok := []struct{ in, want string }{
		{"2026/11/09/14-42-31.mp4", root + "/2026/11/09/14-42-31.mp4"},
		{"/2026/11/09/a.mp4", root + "/2026/11/09/a.mp4"}, // a leading slash is dropped
		{"a/../b.mp4", root + "/b.mp4"},                   // normalized inside the root
		{"../../etc/x.mp4", root + "/etc/x.mp4"},          // cannot climb above the root
		{"A.MP4", root + "/A.MP4"},                        // extension check ignores case
	}
	for _, c := range ok {
		got, err := safeRel(root, c.in)
		if err != nil || got != c.want {
			t.Errorf("safeRel(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	bad := []string{"", "a.mp4/../b", "x.jpg", "notes.txt", "../../etc/passwd", "a.mp4\x00.txt"}
	for _, in := range bad {
		if got, err := safeRel(root, in); err == nil {
			t.Errorf("safeRel(%q) accepted as %q", in, got)
		}
	}
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]string{"stream0.width": "1", "recorder.limit": "2", "a": "3"})
	if strings.Join(got, ",") != "a,recorder.limit,stream0.width" {
		t.Fatalf("order %v", got)
	}
}

func TestRedactKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Starting ffmpeg -> rtmp://a.rtmp.youtube.com/live2/abcd-1234-efgh (config: x)", "Starting ffmpeg -> rtmp://a.rtmp.youtube.com/live2/<key> (config: x)"},
		{"rtmps://a.rtmps.youtube.com:443/live2/k3y", "rtmps://a.rtmps.youtube.com:443/live2/<key>"},
		{"no url here", "no url here"},
		{"two rtmp://h/app/k1 and rtmp://h/app/k2", "two rtmp://h/app/<key> and rtmp://h/app/<key>"},
	}
	for _, c := range cases {
		if got := redactKey(c.in); got != c.want {
			t.Errorf("redactKey(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestOSDValidate(t *testing.T) {
	i := func(v int) *int { return &v }
	b := func(v bool) *bool { return &v }
	w, h := 1280, 720
	good := []struct {
		name string
		e    osdElem
	}{
		{"burnin", osdElem{Enabled: b(true), Scale: i(0)}},
		{"textfile", osdElem{PosX: i(-8), PosY: i(40), Cols: i(27), Rows: i(2), Scale: i(1)}},
		{"textfile3", osdElem{PosX: i(-1280), PosY: i(720)}},
		{"imagefile", osdElem{Width: i(128), Height: i(128)}},
		{"imagefile3", osdElem{Width: i(16), Height: i(720)}},
	}
	for _, c := range good {
		if err := osdValidate(c.name, c.e, w, h); err != nil {
			t.Errorf("%s %+v rejected: %v", c.name, c.e, err)
		}
	}
	bad := []struct {
		name string
		e    osdElem
	}{
		{"nope", osdElem{Enabled: b(true)}},
		{"burnin", osdElem{PosX: i(5)}},                     // the timestamp has no position
		{"imagefile", osdElem{Scale: i(2)}},                 // images have no scale
		{"textfile", osdElem{Width: i(100)}},                // text boxes have no width
		{"imagefile", osdElem{Width: i(127), Height: i(2)}}, // odd
		{"imagefile", osdElem{Width: i(1282)}},              // wider than the stream
		{"textfile", osdElem{Cols: i(0)}},
		{"textfile", osdElem{Rows: i(33)}},
		{"textfile", osdElem{Scale: i(11)}},
		{"textfile", osdElem{PosX: i(-1281)}},
	}
	for _, c := range bad {
		if err := osdValidate(c.name, c.e, w, h); err == nil {
			t.Errorf("%s %+v accepted", c.name, c.e)
		}
	}
}

func TestQualityValidate(t *testing.T) {
	l := sensorLimits{Width: 1920, Height: 1080, MinFPS: 5, MaxFPS: 30}
	if err := (quality{1280, 720, 10, 1024, "CBR"}).validate(l); err != nil {
		t.Fatalf("the current setting rejected: %v", err)
	}
	bad := []quality{
		{1000, 700, 10, 1024, "CBR"},  // not a listed size
		{1280, 720, 4, 1024, "CBR"},   // below the sensor's minimum
		{1280, 720, 31, 1024, "CBR"},  // above its maximum
		{1280, 720, 10, 100, "CBR"},   // bitrate too low
		{1280, 720, 10, 9000, "CBR"},  // too high
		{1280, 720, 10, 1024, "ABR"},  // unknown mode
		{1920, 1080, 10, 1024, "VBR"}, // fine size, but a small sensor refuses it below
	}
	small := sensorLimits{Width: 1280, Height: 720, MinFPS: 5, MaxFPS: 30}
	for i, q := range bad {
		lim := l
		if i == len(bad)-1 {
			lim = small
		}
		if err := q.validate(lim); err == nil {
			t.Errorf("%+v accepted", q)
		}
	}
}

func TestParseStat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stat")
	// a command name with spaces and parentheses, as /proc allows
	line := "1234 (ffmpeg (relay) x) S 1317 1234 1234 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 1 0 216802 2703360 657 4294967295 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	ppid, start, ok := parseStat(path)
	if !ok || ppid != 1317 || start != 216802 {
		t.Fatalf("parseStat = %d, %d, %v", ppid, start, ok)
	}
	if err := os.WriteFile(path, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := parseStat(path); ok {
		t.Fatal("garbage accepted")
	}
	if _, _, ok := parseStat(filepath.Join(dir, "missing")); ok {
		t.Fatal("missing file accepted")
	}
}
