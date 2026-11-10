package main

// The lapcam version (docs/release.md): one number for the whole release,
// from the git tag at build time (tools/version.sh -> build.sh -ldflags).
// The firmware image carries the same number in /etc/lapcam-release, written
// by firmware/prepare.sh; the status shows both, and flags a mismatch (the
// card was updated without the image, or the other way round).

import (
	"os"
	"strings"
)

// version - set by build.sh; "dev" when built by hand
var version = "dev"

const (
	releaseFile   = "/etc/lapcam-release"
	osReleaseFile = "/etc/os-release"
)

type versions struct {
	Lapcam   string `json:"lapcam"`             // this binary
	Image    string `json:"image,omitempty"`    // /etc/lapcam-release VERSION; empty on a camera without the image
	Thingino string `json:"thingino,omitempty"` // BUILD_ID of the Thingino firmware, e.g. ciao+da40db6
	Mismatch bool   `json:"mismatch"`           // image and binary differ (both known)
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
		if !ok || k == "" || strings.HasPrefix(k, "#") {
			continue
		}
		m[k] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return m
}

func collectVersions() versions {
	v := versions{Lapcam: version}
	v.Image = keyValues(releaseFile)["VERSION"]
	if id := keyValues(osReleaseFile)["BUILD_ID"]; id != "" {
		v.Thingino, _, _ = strings.Cut(id, ",") // "ciao+da40db6, 2026-11-10 ..." -> "ciao+da40db6"
		v.Thingino = strings.TrimSpace(v.Thingino)
	}
	v.Mismatch = v.Image != "" && v.Image != v.Lapcam
	return v
}
