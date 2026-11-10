package main

// The lapcam version (docs/release.md): one number for the whole release,
// from the git tag at build time (tools/version.sh -> build.sh -ldflags).
// The lapcam firmware (the flashed image, a Thingino build with this
// project's files in it) carries the same number in /etc/lapcam-release,
// written by firmware/prepare.sh; the status shows both and flags a mismatch
// (the card was updated without the firmware, or the other way round). The
// Thingino build the firmware is based on is reported too, for diagnosis,
// but not shown: it is implied by the firmware version.

import (
	"os"
	"strings"
	"sync"
)

// version - set by build.sh; "dev" when built by hand
var version = "dev"

// (variables: tests point them at files of their own)
var (
	releaseFile   = "/etc/lapcam-release"
	osReleaseFile = "/etc/os-release"
)

type versions struct {
	Lapcam   string `json:"lapcam"`             // this binary
	Firmware string `json:"firmware,omitempty"` // /etc/lapcam-release VERSION; empty on a camera without the lapcam firmware
	Thingino string `json:"thingino,omitempty"` // BUILD_ID of the Thingino build it is based on, e.g. ciao+da40db6
	Mismatch bool   `json:"mismatch"`           // firmware and binary differ (both known)
}

// keyValues - KEY=value lines (os-release style, quotes stripped)
func keyValues(path string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.HasPrefix(k, "#") {
			continue
		}
		m[k] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return m
}

// readVersions - the files are read once: they change only with a reflash,
// which reboots the camera (so does a new binary on the card)
func readVersions() versions {
	v := versions{Lapcam: version}
	v.Firmware = keyValues(releaseFile)["VERSION"]
	if id := keyValues(osReleaseFile)["BUILD_ID"]; id != "" {
		v.Thingino, _, _ = strings.Cut(id, ",") // "ciao+da40db6, 2026-11-10 ..." -> "ciao+da40db6"
		v.Thingino = strings.TrimSpace(v.Thingino)
	}
	// exact: a card with a binary from another commit than the firmware is
	// what this is for, even during development (docs/release.md says so)
	v.Mismatch = v.Firmware != "" && v.Firmware != v.Lapcam
	return v
}

var (
	versionsOnce   sync.Once
	versionsCached versions
)

func collectVersions() versions {
	versionsOnce.Do(func() { versionsCached = readVersions() })
	return versionsCached
}
