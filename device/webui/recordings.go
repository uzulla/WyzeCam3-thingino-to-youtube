package main

// Recordings on the SD card: the files prudynt's recorder wrote (one per
// minute, under <mount>/<device_path>/%Y/%m/%d/), to play in the browser,
// download or delete; and the recorder's settings in /etc/prudynt.json
// (the same keys as Thingino's Video Recorder page: autostart, clip length,
// and the cleanup daemon recordmgr's limit / min free space).

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	prudyntConfig  = "/etc/prudynt.json"
	recordmgrInit  = "/etc/init.d/S95recordmgr"
	recordingsMax  = 400 // newest files listed
	clipMinSeconds = 10
	clipMaxSeconds = 3600
)

type recFile struct {
	Path      string `json:"path"` // relative to the recordings root, e.g. 2026/11/09/14-42-31.mp4
	Size      int64  `json:"size"`
	MTime     int64  `json:"mtime"`     // unix seconds
	Recording bool   `json:"recording"` // being written right now
}

type recSettings struct {
	Autostart      bool `json:"autostart"`
	Duration       int  `json:"duration_s"`
	CleanupEnabled bool `json:"cleanup_enabled"`
	LimitGB        int  `json:"limit_gb"`
	MinFreeMB      int  `json:"min_free_mb"`
}

// jctGet - one value of /etc/prudynt.json as text ("" when unset)
func jctGet(key string) string {
	out, err := runCmd("jct", prudyntConfig, "get", key)
	if err != nil || out == "null" {
		return ""
	}
	return strings.Trim(out, "\"")
}

func jctSet(key, value string) error {
	out, err := runCmd("jct", prudyntConfig, "set", key, value)
	if err != nil {
		return fmt.Errorf("jct set %s: %v %s", key, err, out)
	}
	return nil
}

// recordingsRoot - <mount>/<device_path with %hostname expanded>
func recordingsRoot() (string, error) {
	mount := jctGet("recorder.mount")
	if mount == "" {
		mount = recMountDef
	}
	dev := jctGet("recorder.device_path")
	if dev == "" {
		dev = "%hostname"
	}
	if strings.Contains(dev, "%hostname") {
		host, _ := os.Hostname()
		dev = strings.ReplaceAll(dev, "%hostname", host)
	}
	dev = strings.Trim(dev, "/")
	if !mounted(mount) {
		return "", errors.New("no SD card at " + mount)
	}
	return filepath.Join(mount, dev), nil
}

func listRecordings(root string) ([]recFile, error) {
	files := []recFile{} // never nil: the page expects a list, even an empty one
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // a vanished file: skip
		}
		if info.IsDir() || !strings.HasSuffix(strings.ToLower(info.Name()), ".mp4") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		files = append(files, recFile{Path: filepath.ToSlash(rel), Size: info.Size(), MTime: info.ModTime().Unix()})
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// newest first (by name: the recorder names files by time, so the order
	// does not depend on the clock the file was written with)
	sort.Slice(files, func(i, j int) bool { return files[i].Path > files[j].Path })
	if len(files) > recordingsMax {
		files = files[:recordingsMax]
	}
	return files, nil
}

func readRecSettings() recSettings {
	var s recSettings
	s.Autostart = jctGet("recorder.autostart") == "true"
	s.Duration, _ = strconv.Atoi(jctGet("recorder.duration"))
	if s.Duration == 0 {
		s.Duration = 60
	}
	s.CleanupEnabled = jctGet("recorder.cleanup_enabled") == "true"
	s.LimitGB, _ = strconv.Atoi(jctGet("recorder.limit"))
	s.MinFreeMB, _ = strconv.Atoi(jctGet("recorder.min_free_mb"))
	return s
}

// safeRel - a relative path from the client that stays inside the root
func safeRel(root, rel string) (string, error) {
	rel = filepath.Clean("/" + rel)
	if !strings.HasSuffix(strings.ToLower(rel), ".mp4") {
		return "", errors.New("not a recording")
	}
	full := filepath.Join(root, rel)
	if !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", errors.New("bad path")
	}
	return full, nil
}

// --- HTTP ---

func (s *server) handleRecordingsList(w http.ResponseWriter, r *http.Request) {
	settings := readRecSettings()
	root, err := recordingsRoot()
	resp := map[string]any{"settings": settings}
	if err != nil {
		resp["error"] = err.Error()
		resp["files"] = []recFile{}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	files, err := listRecordings(root)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if cur, _, active := activeFile(); active {
		for i := range files {
			files[i].Recording = filepath.Clean(cur) == filepath.Join(root, files[i].Path)
		}
	}
	free, total := diskMB(root)
	var used int64
	for _, f := range files {
		used += f.Size
	}
	resp["root"] = root
	resp["files"] = files
	resp["truncated"] = len(files) == recordingsMax
	resp["free_mb"] = free
	resp["total_mb"] = total
	resp["listed_mb"] = used >> 20
	writeJSON(w, http.StatusOK, resp)
}

// handleRecordingFile - the file itself, with Range requests (the <video>
// element seeks with them); ?dl=1 makes the browser save it instead
func (s *server) handleRecordingFile(w http.ResponseWriter, r *http.Request) {
	root, err := recordingsRoot()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	full, err := safeRel(root, r.URL.Query().Get("p"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(full)+`"`)
	}
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeFile(w, r, full)
}

// handleRecordingDelete - one path or several; the file being written is
// skipped, empty day/month/year folders go with their last file
func (s *server) handleRecordingDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &body, 256<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if body.Path != "" {
		body.Paths = append(body.Paths, body.Path)
	}
	if len(body.Paths) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "nothing selected"})
		return
	}
	root, err := recordingsRoot()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	cur, _, active := activeFile()
	cur = filepath.Clean(cur)
	deleted, skipped := 0, []string{}
	for _, p := range body.Paths {
		full, err := safeRel(root, p)
		if err != nil {
			skipped = append(skipped, p+": "+err.Error())
			continue
		}
		if active && cur == full {
			skipped = append(skipped, p+": being recorded now")
			continue
		}
		if err := os.Remove(full); err != nil {
			skipped = append(skipped, p+": "+err.Error())
			continue
		}
		deleted++
		// empty day/month/year folders go too, except while a recording runs
		// (its next segment expects today's folder to be there)
		if active {
			continue
		}
		for d := filepath.Dir(full); d != root && strings.HasPrefix(d, root); d = filepath.Dir(d) {
			if os.Remove(d) != nil {
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted, "skipped": skipped})
}

// --- zip of several clips ---
// The phone asks for a zip in two steps: POST the list (a short id comes
// back), then GET the zip by id as a normal download (a <a href>, so the
// browser streams it to its downloads instead of holding it in memory).
// Files are stored, not compressed: video does not shrink, and the camera's
// CPU is better spent on the stream.

type zipJob struct {
	paths []string
	made  time.Time
}

var (
	zipMu   sync.Mutex
	zipJobs = map[string]zipJob{}
)

func (s *server) handleZipPrepare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &body, 256<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	root, err := recordingsRoot()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	var total int64
	var paths []string
	seen := map[string]bool{}
	for _, p := range body.Paths {
		full, err := safeRel(root, p)
		if err != nil {
			continue
		}
		fi, err := os.Stat(full)
		if err != nil || seen[full] {
			continue
		}
		seen[full] = true
		total += fi.Size()
		rel, _ := filepath.Rel(root, full) // the cleaned name, never the client's spelling
		paths = append(paths, filepath.ToSlash(rel))
	}
	if len(paths) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "nothing selected"})
		return
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	zipMu.Lock()
	for k, j := range zipJobs {
		if time.Since(j.made) > 15*time.Minute {
			delete(zipJobs, k)
		}
	}
	zipJobs[id] = zipJob{paths: paths, made: time.Now()}
	zipMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "files": len(paths), "bytes": total})
}

func (s *server) handleZipGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	zipMu.Lock()
	job, ok := zipJobs[id]
	zipMu.Unlock()
	if !ok {
		http.Error(w, "this download link has expired; select the files again", http.StatusNotFound)
		return
	}
	root, err := recordingsRoot()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// open everything first: a missing file is an error before any byte is
	// sent, not a silently shorter archive
	type entry struct {
		rel string
		f   *os.File
		fi  os.FileInfo
	}
	var entries []entry
	defer func() {
		for _, e := range entries {
			e.f.Close()
		}
	}()
	for _, p := range job.paths {
		full, err := safeRel(root, p)
		if err != nil {
			http.Error(w, "bad path in the selection", http.StatusBadRequest)
			return
		}
		f, err := os.Open(full)
		if err != nil {
			http.Error(w, p+" is gone; select the files again", http.StatusNotFound)
			return
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			http.Error(w, p+": "+err.Error(), http.StatusInternalServerError)
			return
		}
		entries = append(entries, entry{rel: p, f: f, fi: fi})
	}
	name := "recordings-" + time.Now().Format("20060102-150405") + ".zip"
	if len(job.paths) > 0 {
		first := strings.TrimSuffix(filepath.Base(job.paths[0]), ".mp4")
		day := strings.ReplaceAll(filepath.Dir(job.paths[0]), "/", "-")
		name = fmt.Sprintf("rec-%s-%s-%dfiles.zip", day, first, len(job.paths))
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	zw := zip.NewWriter(w)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.rel, Method: zip.Store, Modified: e.fi.ModTime(), UncompressedSize64: uint64(e.fi.Size())}
		ww, err := zw.CreateHeader(hdr)
		if err != nil {
			panic(http.ErrAbortHandler) // cut the connection: a half zip must not look complete
		}
		if _, err := io.Copy(ww, e.f); err != nil {
			panic(http.ErrAbortHandler) // the phone went away, or the card failed
		}
	}
	if err := zw.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *server) handleRecordingSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Autostart      *bool `json:"autostart"`
		Duration       *int  `json:"duration_s"`
		CleanupEnabled *bool `json:"cleanup_enabled"`
		LimitGB        *int  `json:"limit_gb"`
		MinFreeMB      *int  `json:"min_free_mb"`
	}
	if err := readJSON(r, &body, 4096); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if body.Duration != nil && (*body.Duration < clipMinSeconds || *body.Duration > clipMaxSeconds) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("clip length must be %d to %d seconds", clipMinSeconds, clipMaxSeconds)})
		return
	}
	if body.LimitGB != nil && (*body.LimitGB < 0 || *body.LimitGB > 2000) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be 0 to 2000 GB"})
		return
	}
	if body.MinFreeMB != nil && (*body.MinFreeMB < 0 || *body.MinFreeMB > 1000000) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "min free must be 0 to 1000000 MB"})
		return
	}
	s.relay.mu.Lock()
	defer s.relay.mu.Unlock()
	sets := map[string]string{}
	if body.Autostart != nil {
		sets["recorder.autostart"] = strconv.FormatBool(*body.Autostart)
	}
	if body.Duration != nil {
		sets["recorder.duration"] = strconv.Itoa(*body.Duration)
	}
	cleanupTouched := false
	if body.CleanupEnabled != nil {
		sets["recorder.cleanup_enabled"] = strconv.FormatBool(*body.CleanupEnabled)
		cleanupTouched = true
	}
	if body.LimitGB != nil {
		sets["recorder.limit"] = strconv.Itoa(*body.LimitGB)
		cleanupTouched = true
	}
	if body.MinFreeMB != nil {
		sets["recorder.min_free_mb"] = strconv.Itoa(*body.MinFreeMB)
		cleanupTouched = true
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "nothing to change"})
		return
	}
	before := etcSum()
	for _, k := range sortedKeys(sets) {
		if err := jctSet(k, sets[k]); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}
	s.noteEtcRefresh(before) // not a Web UI save of the image settings (etcnote.go)
	resp := map[string]any{"ok": true, "settings": readRecSettings()}
	if cleanupTouched {
		// recordmgr reads its settings at start; S95recordmgr starts it only
		// when cleanup is enabled
		if out, err := runCmd("sh", recordmgrInit, "restart"); err != nil {
			resp["error"] = "saved, but recordmgr did not restart: " + out
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// sortedKeys - a fixed order for writing several keys (a map's order is random)
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
