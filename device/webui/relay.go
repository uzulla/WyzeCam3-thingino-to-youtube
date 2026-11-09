package main

// The YouTube stream: the youtube-relay supervisor (device/youtube-relay)
// and its config. webui adds "profiles" (several stream keys to switch
// between) in its own webui.json; the chosen one is written into
// youtube-relay.json, which stays the single file the relay reads.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	relayInit    = "/etc/init.d/S93youtube-relay"
	relayPidfile = "/run/youtube-relay.pid"
	relayEtcCfg  = "/etc/youtube-relay.json"
	relayCfgName = "youtube-relay.json"
	webuiCfgName = "webui.json"
	cmdTimeout   = 20 * time.Second
)

type profile struct {
	Name      string `json:"name"`
	RTMPURL   string `json:"rtmp_url"`
	StreamKey string `json:"stream_key"`
}

type webuiConfig struct {
	Profiles []profile `json:"profiles"`
	Active   string    `json:"active"`
}

type relay struct {
	sd string
	mu sync.Mutex // one config write / service operation at a time
}

func newRelay(sd string) *relay { return &relay{sd: sd} }

// configPath - the file the relay reads: the SD card first, then /etc
// (device/youtube-relay: CONFIG_CANDIDATES); for writing, the SD card when
// it is mounted
func (rl *relay) configPath() string {
	sdCfg := filepath.Join(rl.sd, relayCfgName)
	if exists(sdCfg) {
		return sdCfg
	}
	if exists(relayEtcCfg) {
		return relayEtcCfg
	}
	if mounted(rl.sd) {
		return sdCfg
	}
	return relayEtcCfg
}

func (rl *relay) webuiPath() string { return filepath.Join(rl.sd, webuiCfgName) }

// readJSONFile - a JSON object from path (empty map when the file is absent)
func readJSONFile(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: not valid JSON: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// writeJSONFile - tmp + rename, so a reader never sees half a file
func writeJSONFile(path string, v any, mode os.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// loadProfiles - webui.json; without one, a single profile made from the
// relay's current config, so the first screen shows what runs today
func (rl *relay) loadProfiles() (webuiConfig, error) {
	var cfg webuiConfig
	b, err := os.ReadFile(rl.webuiPath())
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("%s: not valid JSON: %w", rl.webuiPath(), err)
		}
	case errors.Is(err, os.ErrNotExist):
		relayCfg, err := readJSONFile(rl.configPath())
		if err != nil {
			return cfg, err
		}
		key, _ := relayCfg["stream_key"].(string)
		url, _ := relayCfg["rtmp_url"].(string)
		if url == "" {
			url = "rtmps://a.rtmps.youtube.com:443/live2"
		}
		cfg.Profiles = []profile{{Name: "YouTube", RTMPURL: url, StreamKey: key}}
		cfg.Active = "YouTube"
	default:
		return cfg, err
	}
	return cfg, nil
}

var profileName = regexp.MustCompile(`^[^\x00-\x1f]{1,40}$`)

// saveProfiles - validate, write webui.json, and put the active profile's
// key and URL into the relay's config (keeping its other keys). Returns
// whether the relay's config changed.
func (rl *relay) saveProfiles(cfg webuiConfig) (changed bool, err error) {
	if len(cfg.Profiles) == 0 {
		return false, errors.New("at least one profile is needed")
	}
	seen := map[string]bool{}
	var active *profile
	for i := range cfg.Profiles {
		p := &cfg.Profiles[i]
		p.Name = strings.TrimSpace(p.Name)
		p.RTMPURL = strings.TrimSpace(p.RTMPURL)
		p.StreamKey = strings.TrimSpace(p.StreamKey)
		if !profileName.MatchString(p.Name) {
			return false, fmt.Errorf("profile %d: the name must be 1 to 40 characters", i+1)
		}
		if seen[p.Name] {
			return false, fmt.Errorf("two profiles are named %q", p.Name)
		}
		seen[p.Name] = true
		if !strings.HasPrefix(p.RTMPURL, "rtmp://") && !strings.HasPrefix(p.RTMPURL, "rtmps://") {
			return false, fmt.Errorf("%s: the URL must start with rtmp:// or rtmps://", p.Name)
		}
		if strings.ContainsAny(p.StreamKey, " \t\"'\\") {
			return false, fmt.Errorf("%s: the stream key has a space or a quote in it", p.Name)
		}
		if p.Name == cfg.Active {
			active = p
		}
	}
	if active == nil {
		return false, fmt.Errorf("active profile %q is not in the list", cfg.Active)
	}
	if active.StreamKey == "" {
		return false, fmt.Errorf("%s: the stream key is empty (the relay would stop the stream)", active.Name)
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if !mounted(rl.sd) {
		return false, errors.New("no SD card: profiles are kept on it")
	}
	if err := writeJSONFile(rl.webuiPath(), cfg, 0o600); err != nil {
		return false, err
	}
	path := rl.configPath()
	relayCfg, err := readJSONFile(path)
	if err != nil {
		return false, err
	}
	if relayCfg["stream_key"] == active.StreamKey && relayCfg["rtmp_url"] == active.RTMPURL {
		return false, nil
	}
	relayCfg["stream_key"] = active.StreamKey
	relayCfg["rtmp_url"] = active.RTMPURL
	if _, ok := relayCfg["rtsp_url"]; !ok {
		relayCfg["rtsp_url"] = "rtsp://thingino:thingino@127.0.0.1:554/ch0"
	}
	if _, ok := relayCfg["enabled"]; !ok {
		relayCfg["enabled"] = true
	}
	if err := writeJSONFile(path, relayCfg, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// runCmd - run a command with a timeout; its combined output comes back
func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out := make(chan struct {
		b   []byte
		err error
	}, 1)
	go func() {
		b, err := cmd.CombinedOutput()
		out <- struct {
			b   []byte
			err error
		}{b, err}
	}()
	select {
	case r := <-out:
		return strings.TrimSpace(string(r.b)), r.err
	case <-time.After(cmdTimeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", fmt.Errorf("%s: no answer in %v", name, cmdTimeout)
	}
}

// op - start/stop/restart now (through sh, so it works while disabled at
// boot too), enable/disable = at boot, via Thingino's "service"
func (rl *relay) op(op string) (string, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	switch op {
	case "start", "stop", "restart":
		return runCmd("sh", relayInit, op)
	case "enable", "disable":
		return runCmd("service", op, "youtube-relay")
	}
	return "", fmt.Errorf("unknown op %q", op)
}

// restartIfRunning - after a config change: the relay picks up a new key only
// when restarted (the stream drops for a few seconds)
func (rl *relay) restartIfRunning() (restarted bool, out string, err error) {
	if !relayRunning() {
		return false, "", nil
	}
	out, err = rl.op("restart")
	return err == nil, out, err
}

// relayRunning - the supervisor of relayPidfile is alive and is the relay
func relayRunning() bool {
	pid := readPid(relayPidfile)
	if pid <= 0 {
		return false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return err == nil && strings.Contains(string(cmdline), "youtube-relay")
}

func relayAutostart() bool {
	fi, err := os.Stat(relayInit)
	return err == nil && fi.Mode()&0o100 != 0
}

// relayFfmpeg - the ffmpeg the relay started: a child of the supervisor with
// the relay's fixed options on its command line (as json-youtube.cgi finds it)
func relayFfmpeg(uptime float64) (pid int, elapsed int) {
	rpid := readPid(relayPidfile)
	if rpid <= 0 {
		return 0, 0
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name()[0] < '0' || e.Name()[0] > '9' {
			continue
		}
		ppid, start, ok := parseStat(filepath.Join("/proc", e.Name(), "stat"))
		if !ok || ppid != rpid {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || !strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), " -nostdin -hide_banner ") {
			continue
		}
		fmt.Sscanf(e.Name(), "%d", &pid)
		elapsed = int(uptime - float64(start)/100)
		if elapsed < 0 {
			elapsed = 0
		}
		return pid, elapsed
	}
	return 0, 0
}

// relayLog - the relay's last lines from the syslog ring (stream key hidden)
func relayLog(n int) []string {
	out, err := runCmd("logread")
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "youtube-relay") {
			lines = append(lines, redactKey(l))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

var keyInURL = regexp.MustCompile(`(rtmps?://[^\s/]+/[^\s/]+/)[^\s"']+`)

func redactKey(s string) string { return keyInURL.ReplaceAllString(s, "${1}<key>") }

// --- HTTP ---

func (s *server) handleRelayOp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Op string `json:"op"`
	}
	if err := readJSON(r, &body, 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	out, err := s.relay.op(body.Op)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": fmt.Sprintf("%s failed: %v %s", body.Op, err, out)})
		return
	}
	s.status.refresh()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "op": body.Op, "output": redactKey(out)})
}

func (s *server) handleRelayConfigGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.relay.loadProfiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	sort.SliceStable(cfg.Profiles, func(i, j int) bool { return false }) // keep the file's order
	writeJSON(w, http.StatusOK, map[string]any{
		"profiles":    cfg.Profiles,
		"active":      cfg.Active,
		"config_path": s.relay.configPath(),
		"autostart":   relayAutostart(),
	})
}

func (s *server) handleRelayConfigSet(w http.ResponseWriter, r *http.Request) {
	var cfg webuiConfig
	if err := readJSON(r, &cfg, 64<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	changed, err := s.relay.saveProfiles(cfg)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	resp := map[string]any{"ok": true, "changed": changed, "restarted": false}
	if changed {
		restarted, out, err := s.relay.restartIfRunning()
		resp["restarted"] = restarted
		if err != nil {
			resp["error"] = "saved, but the restart failed: " + redactKey(out)
		}
	}
	s.status.refresh()
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleLog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"lines": relayLog(30)})
}
