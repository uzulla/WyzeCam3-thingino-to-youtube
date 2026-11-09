package main

// The camera's state for the top screen, gathered every few seconds by one
// poller so that several phones polling /api/status do not each run the
// commands. Also the picture flip (prudynt image.hflip / vflip).

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const osdConfigName = "prudynt-osd.json"

type status struct {
	Time      string   `json:"time"`
	UptimeS   int64    `json:"uptime_s"`
	IP        string   `json:"ip"`
	Hostname  string   `json:"hostname"`
	Load1     string   `json:"load1"`
	MemFreeMB int      `json:"mem_free_mb"`
	WiFi      wifi     `json:"wifi"`
	Stream    stream   `json:"stream"`
	Relay     relaySt  `json:"relay"`
	Recording recState `json:"recording"`
	Image     image    `json:"image"`
	APMode    bool     `json:"ap_mode"` // the camera is an access point (wifi-ap / Thingino portal) until reboot
	Errors    []string `json:"errors,omitempty"`
}

type wifi struct {
	SSID    string `json:"ssid"`
	RSSIdBm *int   `json:"rssi_dbm"`
}

type stream struct {
	FPS    int `json:"fps"`     // encoder output (prudynt stats)
	Kbps   int `json:"kbps"`    // encoder output
	TxKbps int `json:"tx_kbps"` // wlan0 transmit, all traffic
}

type relaySt struct {
	Running       bool   `json:"running"`        // the supervisor
	Autostart     bool   `json:"autostart"`      // at boot
	FfmpegRunning bool   `json:"ffmpeg_running"` // the stream itself
	FfmpegElapsed int    `json:"ffmpeg_elapsed_s"`
	Restarts      int    `json:"restarts"` // ffmpeg restarts seen since lapcam started
	Profile       string `json:"profile"`
}

type image struct {
	HFlip bool `json:"hflip"`
	VFlip bool `json:"vflip"`
}

type statusPoller struct {
	relay    *relay
	rec      *recorder
	interval time.Duration

	mu       sync.RWMutex
	last     status
	lastAt   time.Time
	seenAt   time.Time // last /api/status
	ffmpeg   int       // last ffmpeg pid seen
	restarts int
	txBytes  int64
	txAt     time.Time
	kick     chan struct{}
}

func newStatusPoller(rl *relay, rc *recorder, interval time.Duration) *statusPoller {
	return &statusPoller{relay: rl, rec: rc, interval: interval, kick: make(chan struct{}, 1)}
}

// run - every interval while someone is looking (a /api/status within the
// last 30 s), otherwise ten times slower: the collection starts a few
// processes each time, and the camera is usually streaming with nobody watching
func (p *statusPoller) run(ctx context.Context) {
	p.update()
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if p.watched() {
				p.update()
			} else if time.Since(p.lastAt) >= 10*p.interval {
				p.update()
			}
		case <-p.kick:
			p.update()
		}
	}
}

func (p *statusPoller) watched() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return time.Since(p.seenAt) < 30*time.Second
}

// current - the last status; also notes that someone is looking
func (p *statusPoller) current() status {
	p.mu.Lock()
	wasWatched := time.Since(p.seenAt) < 30*time.Second
	p.seenAt = time.Now()
	st := p.last
	stale := time.Since(p.lastAt) > 2*p.interval
	p.mu.Unlock()
	if !wasWatched || stale {
		p.refresh() // the first look after a quiet spell gets fresh data soon
	}
	return st
}

// refresh - after an action, so the next /api/status shows the result
func (p *statusPoller) refresh() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *statusPoller) update() {
	st := p.collect()
	p.mu.Lock()
	p.last = st
	p.lastAt = time.Now()
	p.mu.Unlock()
}

func (p *statusPoller) collect() status {
	var st status
	now := time.Now()
	st.Time = now.Format(time.RFC3339)
	st.UptimeS = int64(uptime())
	st.Hostname, _ = os.Hostname()
	st.IP = ipOf("wlan0")
	st.Load1 = load1()
	st.MemFreeMB = memAvailableMB()
	st.WiFi = wifiInfo("wlan0")
	st.Stream.FPS, st.Stream.Kbps = prudyntStats()
	st.Stream.TxKbps = p.txRate(now)

	st.Relay.Running = relayRunning()
	st.Relay.Autostart = relayAutostart()
	pid, el := relayFfmpeg(uptime())
	st.Relay.FfmpegRunning = pid > 0
	st.Relay.FfmpegElapsed = el
	p.mu.Lock()
	if pid > 0 && p.ffmpeg > 0 && pid != p.ffmpeg {
		p.restarts++
	}
	if pid > 0 {
		p.ffmpeg = pid
	}
	st.Relay.Restarts = p.restarts
	p.mu.Unlock()
	if cfg, err := p.relay.loadProfiles(); err == nil {
		st.Relay.Profile = cfg.Active
	} else {
		st.Errors = append(st.Errors, err.Error())
	}

	st.APMode = exists("/run/wlan_ap_mode") || exists("/run/portal_mode")
	st.Recording = p.rec.state(p.rec.recorderMount())
	img, err := imageGet()
	if err != nil {
		st.Errors = append(st.Errors, "flip: "+err.Error())
	}
	st.Image = img
	return st
}

// txRate - wlan0's transmit rate since the last poll, in kbit/s
func (p *statusPoller) txRate(now time.Time) int {
	b, err := os.ReadFile("/sys/class/net/wlan0/statistics/tx_bytes")
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	p.mu.Lock()
	defer p.mu.Unlock()
	var rate int
	if !p.txAt.IsZero() && n >= p.txBytes {
		dt := now.Sub(p.txAt).Seconds()
		if dt > 0 {
			rate = int(float64(n-p.txBytes) * 8 / 1000 / dt)
		}
	}
	p.txBytes, p.txAt = n, now
	return rate
}

func uptime() float64 {
	b, err := os.ReadFile("/proc/uptime")
	f := strings.Fields(string(b))
	if err != nil || len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

func load1() string {
	b, err := os.ReadFile("/proc/loadavg")
	f := strings.Fields(string(b))
	if err != nil || len(f) == 0 {
		return ""
	}
	return f[0]
}

func memAvailableMB() int {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	free, cached := 0, 0
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "MemFree:":
			free, _ = strconv.Atoi(f[1])
		case "Cached:":
			cached, _ = strconv.Atoi(f[1])
		}
	}
	return (free + cached) / 1024
}

func ipOf(ifname string) string {
	ifc, err := net.InterfaceByName(ifname)
	if err != nil {
		return ""
	}
	addrs, _ := ifc.Addrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.String()
		}
	}
	return ""
}

// wifiInfo - SSID and signal from wpa_supplicant (/proc/net/wireless shows 0
// with this camera's driver, as beacon found)
func wifiInfo(ifname string) wifi {
	var w wifi
	cli, err := exec.LookPath("wpa_cli")
	if err != nil {
		for _, c := range []string{"/usr/sbin/wpa_cli", "/sbin/wpa_cli", "/usr/bin/wpa_cli"} {
			if exists(c) {
				cli, err = c, nil
				break
			}
		}
	}
	if err != nil {
		return w
	}
	if out, err := runCmd(cli, "-i", ifname, "status"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if v, ok := strings.CutPrefix(line, "ssid="); ok {
				w.SSID = v
			}
		}
	}
	if out, err := runCmd(cli, "-i", ifname, "signal_poll"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "RSSI="); ok {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					w.RSSIdBm = &n
				}
			}
		}
	}
	return w
}

// prudyntStats - the main stream's encoder output: {"stream0":{"stats":{"fps":11,"Bps":99962}}}
func prudyntStats() (fps, kbps int) {
	out, err := runCmd("prudyntctl", "json", `{"stream0":{"stats":null}}`)
	if err != nil {
		return 0, 0
	}
	var v struct {
		Stream0 struct {
			Stats struct {
				FPS int `json:"fps"`
				Bps int `json:"Bps"`
			} `json:"stats"`
		} `json:"stream0"`
	}
	if json.Unmarshal([]byte(out), &v) != nil {
		return 0, 0
	}
	return v.Stream0.Stats.FPS, v.Stream0.Stats.Bps * 8 / 1000
}

// --- flip ---

func imageGet() (image, error) {
	out, err := runCmd("prudyntctl", "json", `{"image":{"hflip":null,"vflip":null}}`)
	if err != nil {
		return image{}, fmt.Errorf("prudyntctl: %v", err)
	}
	var v struct {
		Image image `json:"image"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return image{}, fmt.Errorf("prudyntctl answered %q", out)
	}
	return v.Image, nil
}

// imageSet - apply now (prudyntctl) and keep it (image.* of prudynt-osd.json
// on the SD card, which osd-config sends again after a restart). Only the
// keys given are touched.
func (s *server) imageSet(hflip, vflip *bool) error {
	img := map[string]any{}
	if hflip != nil {
		img["hflip"] = *hflip
	}
	if vflip != nil {
		img["vflip"] = *vflip
	}
	if len(img) == 0 {
		return nil
	}
	req := mustJSON(map[string]any{"image": img})
	if out, err := runCmd("prudyntctl", "json", string(req)); err != nil {
		return fmt.Errorf("prudyntctl: %v %s", err, out)
	}
	path := filepath.Join(s.sd, osdConfigName)
	if !exists(path) {
		log.Printf("flip: no %s, the change holds until prudynt restarts", path)
		return nil
	}
	s.relay.mu.Lock() // one writer of SD files at a time
	defer s.relay.mu.Unlock()
	cfg, err := readJSONFile(path)
	if err != nil {
		return err
	}
	cur, _ := cfg["image"].(map[string]any)
	if cur == nil {
		cur = map[string]any{}
	}
	for k, v := range img {
		cur[k] = v
	}
	cfg["image"] = cur
	return writeJSONFile(path, cfg, 0o644)
}

func (s *server) handleImageSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HFlip *bool `json:"hflip"`
		VFlip *bool `json:"vflip"`
	}
	if err := readJSON(r, &body, 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := s.imageSet(body.HFlip, body.VFlip); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.status.refresh()
	img, err := imageGet()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "image": img, "warning": "applied, but prudynt did not answer the read-back: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "image": img})
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.status.current())
}

// --- /proc helpers (as beacon's status.go) ---

func readPid(pidfile string) int {
	b, err := os.ReadFile(pidfile)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// parseStat - ppid and start time (USER_HZ ticks) from /proc/<pid>/stat; the
// command name in parentheses may contain spaces, so split after the ')'
func parseStat(path string) (ppid int, start uint64, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return 0, 0, false
	}
	f := strings.Fields(s[i+1:]) // f[0] = state, f[1] = ppid, ..., f[19] = starttime (field 22)
	if len(f) < 20 {
		return 0, 0, false
	}
	ppid, err = strconv.Atoi(f[1])
	if err != nil {
		return 0, 0, false
	}
	start, err = strconv.ParseUint(f[19], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return ppid, start, true
}

// handleReboot - answer first, then reboot (the way back from the access
// point mode of wifi-ap without touching the camera)
func (s *server) handleReboot(w http.ResponseWriter, r *http.Request) {
	log.Printf("reboot requested from %s", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	go func() {
		time.Sleep(500 * time.Millisecond)
		_, _ = runCmd("sh", "-c", "sync; reboot")
	}()
}
