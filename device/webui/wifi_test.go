package main

import (
	"encoding/hex"
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
	if quotable(`a"b`) || quotable(`a\b`) || quotable("日本") || !quotable("Home-2G") {
		t.Fatal("quotable: wrong verdicts")
	}
}

func TestWPAHexSSID(t *testing.T) {
	src := "ctrl_interface=/run/wpa_supplicant\n\nnetwork={\n\tssid=486f6d65\n\tpsk=\"p\"\n}\nnetwork={\n\tssid=ff00ff\n\tpsk=\"q\"\n}\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "wpa.conf")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := parseWPA(path)
	if err != nil {
		t.Fatal(err)
	}
	if w.nets[0].SSID != "Home" || !w.nets[0].hexSSID || w.nets[0].rawSSID {
		t.Fatalf("hex ssid parsed as %+v", w.nets[0])
	}
	if w.nets[1].SSID != "ff00ff" || !w.nets[1].rawSSID {
		t.Fatalf("non-utf8 hex ssid parsed as %+v", w.nets[1])
	}
	psks := readPSKs(path)
	if psks["Home"] != `"p"` || psks["ff00ff"] != `"q"` {
		t.Fatalf("psks keyed wrongly: %v", psks)
	}
	out, err := w.render(psks)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\tssid=486f6d65\n") || !strings.Contains(out, "\tssid=ff00ff\n") {
		t.Fatalf("hex ssids not written back as hex:\n%s", out)
	}
}

func TestWPAKeepsOtherKeyMgmt(t *testing.T) {
	src := "ctrl_interface=/run/wpa_supplicant\n\nnetwork={\n\tssid=\"Sae\"\n\tkey_mgmt=SAE\n\tsae_password=\"secret\"\n\tscan_ssid=0\n}\n"
	path := filepath.Join(t.TempDir(), "wpa.conf")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := parseWPA(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := w.render(readPSKs(path))
	if err != nil {
		t.Fatalf("a SAE network without psk must still render: %v", err)
	}
	for _, want := range []string{"\tkey_mgmt=SAE\n", "\tsae_password=\"secret\"\n", "\tscan_ssid=0\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "scan_ssid=") != 1 {
		t.Fatalf("scan_ssid written twice:\n%s", out)
	}
	// a name that cannot be quoted goes out as hex
	w.nets = []wifiNet{{SSID: `say "hi"`, Open: true}}
	out, _ = w.render(nil)
	if !strings.Contains(out, "\tssid="+hex.EncodeToString([]byte(`say "hi"`))+"\n") {
		t.Fatalf("quote in ssid not written as hex:\n%s", out)
	}
	if got := unescapeWPA(`\xe3\x81\x82b\\c\"`); got != "あb\\c\"" {
		t.Fatalf("unescape: %q", got)
	}
}
