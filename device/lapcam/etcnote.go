package main

// osd-config (device/osd-config) keeps a note next to each prudynt-osd.json
// (.osd-config-image: file=, etc=, host=, fw=) saying which "image" section
// it last sent, what /etc/prudynt.json looked like then, on which camera and
// on which firmware build. A later change of /etc/prudynt.json that the note
// does not know about is taken for a save in Thingino's Web UI, which then
// wins over the SD card's flip (unless the firmware was reflashed since: a
// fresh /etc/prudynt.json is no save). lapcam writes /etc/prudynt.json too
// (video quality, recorder settings), so it does what osd-config does after
// its own writes: notes that matched the file before the write, on this
// camera and this firmware, follow it (note_etc_refresh).

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const osdNoteName = ".osd-config-image"

// etcSum - osd-config's etc_sum: md5 of /etc/prudynt.json and its mtime
func etcSum() string {
	b, err := os.ReadFile(prudyntConfig)
	if err != nil {
		return "-"
	}
	fi, err := os.Stat(prudyntConfig)
	if err != nil {
		return "-"
	}
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:]) + "-" + fmt.Sprint(fi.ModTime().Unix())
}

// hostID - osd-config's host_id: the first MAC address of the camera, sorted
func hostID() string {
	matches, _ := filepath.Glob("/sys/class/net/*/address")
	var macs []string
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		mac := strings.TrimSpace(string(b))
		if mac != "" && mac != "00:00:00:00:00:00" {
			macs = append(macs, mac)
		}
	}
	sort.Strings(macs)
	if len(macs) == 0 {
		return ""
	}
	return macs[0]
}

// fwID - osd-config's fw_id: the firmware build, BUILD_ID of /etc/os-release
// (quotes stripped, date included)
func fwID() string {
	return keyValues(osReleaseFile)["BUILD_ID"]
}

// noteEtcRefresh - after lapcam wrote /etc/prudynt.json: for each config
// candidate, a note whose etc= matched the file before the write (and whose
// host and firmware are this camera's) is updated to the file as it is now,
// keeping its fw=
func (s *server) noteEtcRefresh(before string) {
	host := hostID()
	fw := fwID()
	for _, cfg := range []string{filepath.Join(s.sd, osdConfigName), "/etc/" + osdConfigName} {
		note := filepath.Join(filepath.Dir(cfg), osdNoteName)
		b, err := os.ReadFile(note)
		if err != nil {
			continue
		}
		var file, etc, h, nfw string
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "file="); ok && file == "" {
				file = v
			} else if v, ok := strings.CutPrefix(line, "etc="); ok && etc == "" {
				etc = v
			} else if v, ok := strings.CutPrefix(line, "host="); ok && h == "" {
				h = v
			} else if v, ok := strings.CutPrefix(line, "fw="); ok && nfw == "" {
				nfw = v
			}
		}
		if file == "" || etc != before || h != host || nfw != fw {
			continue
		}
		text := fmt.Sprintf("file=%s\netc=%s\nhost=%s\nfw=%s\n", file, etcSum(), host, fw)
		_ = writeText(note, text, 0o644)
	}
}
