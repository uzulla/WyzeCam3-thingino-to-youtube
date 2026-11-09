package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWPAParseRender(t *testing.T) {
	src := "ctrl_interface=/run/wpa_supplicant\nupdate_config=1\ndik=ABC\n\nnetwork={\n\tssid=\"Home\"\n\tscan_ssid=1\n\tpsk=\"home-pass\"\n\tpriority=10\n}\nnetwork={\n\tssid=\"Pocket\"\n\tpsk=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n\tbssid=00:11:22:33:44:55\n}\nnetwork={\n\tssid=\"Cafe\"\n\tkey_mgmt=NONE\n}\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "wpa.conf")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := parseWPA(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.nets) != 3 || w.nets[0].SSID != "Home" || w.nets[0].Priority != 10 || !w.nets[0].HasPSK ||
		w.nets[1].extra[0] != "bssid=00:11:22:33:44:55" || !w.nets[2].Open || w.nets[2].HasPSK {
		t.Fatalf("parsed %+v", w.nets)
	}
	if len(w.header) != 3 || w.header[2] != "dik=ABC" {
		t.Fatalf("header %q", w.header)
	}
	psks := readPSKs(path)
	if psks["Home"] != `"home-pass"` || !strings.HasPrefix(psks["Pocket"], "0123") {
		t.Fatalf("psks %v", psks)
	}
	// reorder, drop one, keep passwords
	w.nets = []wifiNet{{SSID: "Pocket", Priority: 5}, {SSID: "Home"}}
	out, err := w.render(psks)
	if err != nil {
		t.Fatal(err)
	}
	want := "ctrl_interface=/run/wpa_supplicant\nupdate_config=1\ndik=ABC\n\nnetwork={\n\tssid=\"Pocket\"\n\tscan_ssid=1\n\tpsk=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n\tpriority=5\n}\n\nnetwork={\n\tssid=\"Home\"\n\tscan_ssid=1\n\tpsk=\"home-pass\"\n}\n"
	if out != want {
		t.Fatalf("rendered:\n%s\nwant:\n%s", out, want)
	}
	// a network without a stored password cannot be written
	w.nets = []wifiNet{{SSID: "New"}}
	if _, err := w.render(psks); err == nil {
		t.Fatal("rendered a network without a password")
	}
	if quoteWPA(`a"b\c`) != `"a\"b\\c"` {
		t.Fatalf("quote: %s", quoteWPA(`a"b\c`))
	}
}
