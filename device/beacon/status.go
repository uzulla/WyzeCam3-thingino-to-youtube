package main

// What the message carries, read from /proc and friends. The parsers take the
// file's contents (and findStream a /proc directory), so the tests can use
// fixtures instead of a camera.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Message - one beacon, as JSON. Fields that could not be read are left out
// (or null), never guessed.
type Message struct {
	Type      string  `json:"type"` // always "thingino-beacon"
	Version   int     `json:"v"`
	Seq       uint64  `json:"seq"` // +1 per round, the same on every interface
	Host      string  `json:"host"`
	Iface     string  `json:"iface"`
	IP        string  `json:"ip"`
	MAC       string  `json:"mac,omitempty"`
	Build     string  `json:"build,omitempty"`
	UptimeS   int64   `json:"uptime_s"`
	Stream    Stream  `json:"stream"`
	RSSI      *int    `json:"rssi_dbm,omitempty"`
	MemFreeMB *int    `json:"mem_free_mb,omitempty"`
	Load1     *string `json:"load1,omitempty"`
}

const (
	msgType    = "thingino-beacon"
	msgVersion = 1
)

// Stream - the YouTube relay (device/youtube-relay) as the web UI's status sees it
type Stream struct {
	// stopped    - the youtube-relay supervisor is not running
	// idle       - it runs but has no ffmpeg (no config, network down, or the
	//              pause between retries)
	// connecting - its ffmpeg has run for less than healthyAfter
	// streaming  - its ffmpeg has run for healthyAfter or longer
	State    string `json:"state"`
	ElapsedS *int64 `json:"elapsed_s"` // how long that ffmpeg has run; null without one
}

// healthyAfter - the relay counts a run this long as healthy (youtube-relay:
// a key YouTube refuses ends the ffmpeg within seconds)
const healthyAfter = 60

func readFile(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

// parseUptime - whole seconds from /proc/uptime ("12345.67 23456.78"); -1 if unreadable
func parseUptime(s string) int64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return -1
	}
	n, err := strconv.ParseInt(strings.SplitN(f[0], ".", 2)[0], 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// parseBuildID - the firmware build from /etc/os-release
// (BUILD_ID="ciao+da40db6, 2026-09-14 ..." -> "ciao+da40db6")
func parseBuildID(osRelease string) string {
	for _, line := range strings.Split(osRelease, "\n") {
		if v, ok := strings.CutPrefix(line, "BUILD_ID="); ok {
			v = strings.Trim(v, `"'`)
			v, _, _ = strings.Cut(v, ",")
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseMemFreeMB - free memory from /proc/meminfo, counting buffers/cache as
// free (like osd-feed's mem source and "free" on the camera)
func parseMemFreeMB(meminfo string) *int {
	kb := map[string]int{}
	for _, line := range strings.Split(meminfo, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if n, err := strconv.Atoi(f[1]); err == nil {
			kb[strings.TrimSuffix(f[0], ":")] = n
		}
	}
	if _, ok := kb["MemFree"]; !ok {
		return nil
	}
	mb := (kb["MemFree"] + kb["Buffers"] + kb["Cached"]) / 1024
	return &mb
}

// parseLoad1 - the 1 minute load average from /proc/loadavg, as the text it is there
func parseLoad1(loadavg string) *string {
	f := strings.Fields(loadavg)
	if len(f) == 0 {
		return nil
	}
	return &f[0]
}

// parseRSSI - the signal from "wpa_cli signal_poll" (RSSI=-27 ...)
func parseRSSI(out string) *int {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "RSSI="); ok {
			if n, err := strconv.Atoi(v); err == nil {
				return &n
			}
		}
	}
	return nil
}

// rssi - the Wi-Fi signal of ifname from wpa_supplicant. /proc/net/wireless
// shows 0 on this camera's driver, so ask wpa_supplicant instead.
func rssi(ifname string) *int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "wpa_cli", "-i", ifname, "signal_poll").Output()
	if err != nil {
		return nil
	}
	return parseRSSI(string(out))
}

// parseStat - the parent pid and the start time (in clock ticks since boot)
// from /proc/<pid>/stat. The command name in ( ) may contain spaces, so the
// fields are counted from the last ')'.
func parseStat(s string) (ppid int, start uint64, ok bool) {
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, 0, false
	}
	f := strings.Fields(s[i+1:])
	// f[0] is field 3 (state): ppid is field 4, starttime field 22
	if len(f) < 20 {
		return 0, 0, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	start, err2 := strconv.ParseUint(f[19], 10, 64)
	return ppid, start, err1 == nil && err2 == nil
}

// clockTicks - USER_HZ, the unit of starttime: 100 on this kernel (as the
// web UI's status also assumes)
const clockTicks = 100

// findStream - the relay's state, the way json-youtube.cgi finds it: the
// supervisor from its pidfile (checked against its command line: pidfiles
// outlive processes), then its ffmpeg child by the relay's fixed first
// options, not by name (ffmpeg_bin may point anywhere, and the web UI's
// recorder runs an ffmpeg of its own).
func findStream(proc, pidfile string, uptime int64) Stream {
	pid := strings.TrimSpace(readFile(pidfile))
	if pid == "" || !strings.Contains(readFile(filepath.Join(proc, pid, "cmdline")), "youtube-relay") {
		return Stream{State: "stopped"}
	}
	relay, _ := strconv.Atoi(pid)
	entries, _ := os.ReadDir(proc)
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		ppid, start, ok := parseStat(readFile(filepath.Join(proc, e.Name(), "stat")))
		if !ok || ppid != relay {
			continue
		}
		cmd := []byte(readFile(filepath.Join(proc, e.Name(), "cmdline")))
		if !bytes.Contains(cmd, []byte("\x00-nostdin\x00-hide_banner\x00")) {
			continue
		}
		el := uptime - int64(start/clockTicks)
		if uptime < 0 || el < 0 {
			el = 0
		}
		st := Stream{State: "connecting", ElapsedS: &el}
		if el >= healthyAfter {
			st.State = "streaming"
		}
		return st
	}
	return Stream{State: "idle"}
}

// collect - the part of the message that is the same on every interface
func collect(host, build string, seq uint64) Message {
	uptime := parseUptime(readFile("/proc/uptime"))
	return Message{
		Type:      msgType,
		Version:   msgVersion,
		Seq:       seq,
		Host:      host,
		Build:     build,
		UptimeS:   uptime,
		Stream:    findStream("/proc", "/run/youtube-relay.pid", uptime),
		MemFreeMB: parseMemFreeMB(readFile("/proc/meminfo")),
		Load1:     parseLoad1(readFile("/proc/loadavg")),
	}
}

// on - the message as sent on one interface: its address, and its Wi-Fi signal
func (m Message) on(tg target) Message {
	m.Iface, m.IP, m.MAC = tg.ifname, tg.ip.String(), tg.mac
	if strings.HasPrefix(tg.ifname, "wlan") {
		m.RSSI = rssi(tg.ifname)
	}
	return m
}
