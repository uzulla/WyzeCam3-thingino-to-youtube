package main

// SD recording: prudynt's own recorder, driven exactly like the web UI's Rec
// button and the camera's button (docs/recording.md): "record -c 0 start"
// for a continuous recording with the Video Recorder settings, "record
// stop" to end it. The state is prudynt's /run/prudynt/mp4ctl-ch0.active.

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	recActiveGlob = "/run/prudynt/mp4ctl-ch*.active"
	recMountDef   = "/mnt/mmcblk0p1"
)

type recorder struct {
	mu    sync.Mutex
	since time.Time // when the poller first saw the recording (zero: not seen)
}

func newRecorder() *recorder { return &recorder{} }

type recState struct {
	Active    bool   `json:"active"`
	Path      string `json:"path,omitempty"`      // the file being written
	Segment   int    `json:"segment_s,omitempty"` // seconds per file
	Elapsed   int    `json:"elapsed_s"`           // since webui first saw it recording
	Mount     string `json:"mount"`
	SDMounted bool   `json:"sd_mounted"`
	FreeMB    int64  `json:"free_mb"`
	TotalMB   int64  `json:"total_mb"`
}

// activeFile - prudynt's state file, parsed (path=..., duration=...)
func activeFile() (path string, duration int, ok bool) {
	matches, _ := filepath.Glob(recActiveGlob)
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, found := strings.CutPrefix(line, "path="); found {
				path = strings.TrimSpace(v)
			} else if v, found := strings.CutPrefix(line, "duration="); found {
				duration, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		return path, duration, true
	}
	return "", 0, false
}

// recorderMount - where the recorder writes (recorder.mount of prudynt.json)
func recorderMount() string {
	out, err := runCmd("jct", "/etc/prudynt.json", "get", "recorder.mount")
	if err != nil || out == "" || out == "null" {
		return recMountDef
	}
	return strings.Trim(out, "\"")
}

func mounted(path string) bool {
	var a, b syscall.Stat_t
	if syscall.Stat(path, &a) != nil || syscall.Stat(filepath.Dir(path), &b) != nil {
		return false
	}
	return a.Dev != b.Dev
}

func diskMB(path string) (free, total int64) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0, 0
	}
	bs := int64(st.Bsize)
	return int64(st.Bavail) * bs / (1 << 20), int64(st.Blocks) * bs / (1 << 20)
}

func (rc *recorder) state(mount string) recState {
	path, dur, active := activeFile()
	rc.mu.Lock()
	if active && rc.since.IsZero() {
		rc.since = time.Now()
	}
	if !active {
		rc.since = time.Time{}
	}
	since := rc.since
	rc.mu.Unlock()
	st := recState{Active: active, Path: path, Segment: dur, Mount: mount, SDMounted: mounted(mount)}
	if active {
		st.Elapsed = int(time.Since(since).Seconds())
	}
	if st.SDMounted {
		st.FreeMB, st.TotalMB = diskMB(mount)
	}
	return st
}

func (rc *recorder) op(op string) (string, error) {
	switch op {
	case "start":
		return runCmd("record", "-c", "0", "start")
	case "stop":
		return runCmd("record", "stop")
	}
	return "", fmt.Errorf("unknown op %q", op)
}

func (s *server) handleRecordOp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Op string `json:"op"`
	}
	if err := readJSON(r, &body, 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	out, err := s.rec.op(body.Op)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": fmt.Sprintf("record %s failed: %v %s", body.Op, err, out)})
		return
	}
	// prudynt creates the state file within a second
	for i := 0; i < 20; i++ {
		_, _, active := activeFile()
		if active == (body.Op == "start") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.status.refresh()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "op": body.Op, "output": out})
}
