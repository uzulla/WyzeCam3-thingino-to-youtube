package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKeyValues(t *testing.T) {
	p := writeTemp(t, "f", "# comment\nVERSION=v2.0.0\nNAME=\"Thingino\"\r\nBUILD_ID='ciao+da40db6, 2026-11-10 08:30 UTC'\n\nbroken line\n  SPACED = x \n")
	m := keyValues(p)
	want := map[string]string{"VERSION": "v2.0.0", "NAME": "Thingino", "BUILD_ID": "ciao+da40db6, 2026-11-10 08:30 UTC", "SPACED": "x"}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
	if _, ok := m["broken line"]; ok {
		t.Errorf("a line without = must be skipped")
	}
	if len(keyValues(filepath.Join(t.TempDir(), "missing"))) != 0 {
		t.Errorf("a missing file must read as empty")
	}
}

func TestReadVersions(t *testing.T) {
	defer func(r, o string) { releaseFile, osReleaseFile = r, o }(releaseFile, osReleaseFile)
	osReleaseFile = writeTemp(t, "os-release", "NAME=Thingino\nBUILD_ID=\"ciao+da40db6, 2026-11-10 08:30:00 UTC\"\n")

	// the same release everywhere
	releaseFile = writeTemp(t, "lapcam-release", "VERSION="+version+"\nCOMMIT=abc\n")
	v := readVersions()
	if v.Lapcam != version || v.Firmware != version || v.Thingino != "ciao+da40db6" || v.Mismatch {
		t.Errorf("same release: %+v", v)
	}

	// the card was updated without the firmware
	releaseFile = writeTemp(t, "lapcam-release", "VERSION=v1.9.0\n")
	v = readVersions()
	if v.Firmware != "v1.9.0" || !v.Mismatch {
		t.Errorf("other firmware: %+v", v)
	}

	// a camera set up by hand: no lapcam-release, no mismatch to report
	releaseFile = filepath.Join(t.TempDir(), "none")
	v = readVersions()
	if v.Firmware != "" || v.Mismatch || v.Thingino != "ciao+da40db6" {
		t.Errorf("no firmware file: %+v", v)
	}

	// a file without the VERSION line counts as no version
	releaseFile = writeTemp(t, "lapcam-release", "COMMIT=abc\n")
	osReleaseFile = filepath.Join(t.TempDir(), "none")
	v = readVersions()
	if v.Firmware != "" || v.Mismatch || v.Thingino != "" {
		t.Errorf("empty files: %+v", v)
	}
}
