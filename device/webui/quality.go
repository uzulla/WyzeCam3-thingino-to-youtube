package main

// Video quality of the main stream (stream0 of /etc/prudynt.json: size, fps,
// bitrate, CBR/VBR). prudynt reads these at start, so a change restarts it
// the way osd-config and the old YouTube page do: under the shared lock
// /run/prudynt-restart.lock, stop, wait until gone, start, wait for an
// answer. The stream to YouTube drops for a few seconds and the relay's
// ffmpeg reconnects by itself.

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	prudyntInit = "/etc/init.d/S31prudynt"
	prudyntLock = "/run/prudynt-restart.lock"
	lockStaleS  = 120
)

type quality struct {
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	FPS     int    `json:"fps"`
	Bitrate int    `json:"bitrate"` // kbit/s
	Mode    string `json:"mode"`    // CBR or VBR
}

var qualitySizes = [][2]int{{1920, 1080}, {1280, 720}, {960, 540}, {640, 360}}

func readQuality() quality {
	var q quality
	q.Width, _ = strconv.Atoi(jctGet("stream0.width"))
	q.Height, _ = strconv.Atoi(jctGet("stream0.height"))
	q.FPS, _ = strconv.Atoi(jctGet("stream0.fps"))
	q.Bitrate, _ = strconv.Atoi(jctGet("stream0.bitrate"))
	q.Mode = jctGet("stream0.mode")
	return q
}

type sensorLimits struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	MinFPS int `json:"min_fps"`
	MaxFPS int `json:"max_fps"`
}

func readSensor() sensorLimits {
	var l sensorLimits
	l.Width, _ = strconv.Atoi(jctGet("sensor.width"))
	l.Height, _ = strconv.Atoi(jctGet("sensor.height"))
	l.MinFPS, _ = strconv.Atoi(jctGet("sensor.min_fps"))
	l.MaxFPS, _ = strconv.Atoi(jctGet("sensor.fps"))
	if l.Width == 0 {
		l.Width, l.Height = 1920, 1080
	}
	if l.MinFPS == 0 {
		l.MinFPS = 5
	}
	if l.MaxFPS == 0 {
		l.MaxFPS = 30
	}
	return l
}

func (q quality) validate(l sensorLimits) error {
	ok := false
	for _, s := range qualitySizes {
		if s[0] == q.Width && s[1] == q.Height && s[0] <= l.Width && s[1] <= l.Height {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("size %dx%d is not one of the choices", q.Width, q.Height)
	}
	if q.FPS < l.MinFPS || q.FPS > l.MaxFPS {
		return fmt.Errorf("fps must be %d to %d", l.MinFPS, l.MaxFPS)
	}
	if q.Bitrate < 128 || q.Bitrate > 8192 {
		return errors.New("bitrate must be 128 to 8192 kb/s")
	}
	if q.Mode != "CBR" && q.Mode != "VBR" {
		return errors.New("mode must be CBR or VBR")
	}
	return nil
}

// prudyntRestart - the shared-lock sequence (json-youtube.cgi: restart_prudynt)
func prudyntRestart() (seconds int, err error) {
	if !exists(prudyntInit) {
		return 0, errors.New(prudyntInit + " not found")
	}
	if err := os.Mkdir(prudyntLock, 0o755); err != nil {
		// osd-config writes "since" with date +%s, newline included
		since := int64(0)
		if b, err := os.ReadFile(filepath.Join(prudyntLock, "since")); err == nil {
			since, _ = strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		}
		if since == 0 {
			if fi, err := os.Stat(prudyntLock); err == nil {
				since = fi.ModTime().Unix()
			}
		}
		if time.Now().Unix()-since < lockStaleS {
			return 0, errors.New("prudynt is being restarted by something else (osd-config?) - try again in a moment")
		}
		os.RemoveAll(prudyntLock)
		if err := os.Mkdir(prudyntLock, 0o755); err != nil {
			return 0, errors.New("prudynt is being restarted by something else")
		}
	}
	defer os.RemoveAll(prudyntLock)
	_ = os.WriteFile(filepath.Join(prudyntLock, "owner"), []byte(strconv.Itoa(os.Getpid())), 0o644)
	_ = os.WriteFile(filepath.Join(prudyntLock, "since"), []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o644)

	t0 := time.Now()
	_, _ = runCmd("sh", prudyntInit, "stop")
	for i := 0; i < 20 && prudyntPid() > 0; i++ {
		time.Sleep(time.Second)
	}
	if prudyntPid() > 0 {
		return 0, errors.New("prudynt did not stop within 20 s - not starting a second one")
	}
	_, _ = runCmd("sh", prudyntInit, "start")
	for i := 1; i <= 30; i++ {
		time.Sleep(time.Second)
		if out, err := runCmd("prudyntctl", "json", `{"general":{"loglevel":null}}`); err == nil && len(out) > 0 && out[0] == '{' {
			return int(time.Since(t0).Seconds()), nil
		}
		if i >= 5 && prudyntPid() == 0 {
			return 0, errors.New("prudynt exited right after starting - see logread")
		}
	}
	return 0, errors.New("prudynt started but does not answer after 30 s")
}

func prudyntPid() int {
	out, err := runCmd("pidof", "prudynt")
	f := strings.Fields(out)
	if err != nil || len(f) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(f[0])
	return n
}

// --- HTTP ---

func (s *server) handleQualityGet(w http.ResponseWriter, r *http.Request) {
	l := readSensor()
	sizes := [][2]int{}
	for _, sz := range qualitySizes {
		if sz[0] <= l.Width && sz[1] <= l.Height {
			sizes = append(sizes, sz)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"current": readQuality(), "sensor": l, "sizes": sizes})
}

func (s *server) handleQualitySet(w http.ResponseWriter, r *http.Request) {
	var q quality
	if err := readJSON(r, &q, 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := q.validate(readSensor()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.relay.mu.Lock()
	defer s.relay.mu.Unlock()
	cur := readQuality()
	if cur == q {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": false})
		return
	}
	// a 2 s GOP (keyframe every 2 s) like the current 10 fps / gop 20 setting
	sets := map[string]string{
		"stream0.width": strconv.Itoa(q.Width), "stream0.height": strconv.Itoa(q.Height),
		"stream0.fps": strconv.Itoa(q.FPS), "stream0.bitrate": strconv.Itoa(q.Bitrate),
		"stream0.mode": q.Mode, "stream0.gop": strconv.Itoa(q.FPS * 2), "stream0.max_gop": strconv.Itoa(q.FPS * 3),
	}
	before := etcSum()
	for _, k := range sortedKeys(sets) {
		if err := jctSet(k, sets[k]); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error() + " (the settings may be half written: check them before applying again)"})
			return
		}
	}
	s.noteEtcRefresh(before) // not a Web UI save of the image settings (etcnote.go)
	secs, err := prudyntRestart()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "saved, but the restart failed: " + err.Error(), "settings": readQuality()})
		return
	}
	s.status.refresh()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": true, "restart_s": secs, "settings": readQuality()})
}
