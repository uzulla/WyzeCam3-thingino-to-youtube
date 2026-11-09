package main

// Wi-Fi: the saved networks of /etc/wpa_supplicant.conf (several, with a
// priority), a scan of what is in the air, and the live connection. A change
// rewrites the file (header lines kept, network blocks regenerated), also on
// the SD card when wifi-from-sd keeps a copy there (device/S37wifi-from-sd:
// the card's file is applied at boot when it changed), then tells
// wpa_supplicant to re-read it. Passwords are never sent to the browser.

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	wpaConf   = "/etc/wpa_supplicant.conf"
	wpaSDName = "wpa_supplicant.conf"
	wlanIface = "wlan0"
)

type wifiNet struct {
	SSID     string   `json:"ssid"`
	Priority int      `json:"priority"`
	Open     bool     `json:"open"`          // no password (key_mgmt=NONE)
	HasPSK   bool     `json:"has_psk"`       // a password is stored (never shown)
	PSK      string   `json:"psk,omitempty"` // only in requests: a new password
	extra    []string // other lines of the block, kept as they are
}

type wpaFile struct {
	header []string // every line outside network={} blocks
	nets   []wifiNet
}

var wpaQuoted = regexp.MustCompile(`^"(.*)"$`)

func parseWPA(path string) (*wpaFile, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &wpaFile{header: []string{"ctrl_interface=/run/wpa_supplicant", "update_config=1"}}, nil
		}
		return nil, err
	}
	defer f.Close()
	w := &wpaFile{}
	var cur *wifiNet
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		t := strings.TrimSpace(line)
		if cur == nil {
			if t == "network={" {
				cur = &wifiNet{}
				continue
			}
			w.header = append(w.header, line)
			continue
		}
		if t == "}" {
			w.nets = append(w.nets, *cur)
			cur = nil
			continue
		}
		k, v, _ := strings.Cut(t, "=")
		switch k {
		case "ssid":
			if m := wpaQuoted.FindStringSubmatch(v); m != nil {
				cur.SSID = m[1]
			} else {
				cur.SSID = v // hex form: kept as-is, shown raw
			}
		case "psk":
			cur.HasPSK = true
		case "priority":
			cur.Priority, _ = strconv.Atoi(v)
		case "key_mgmt":
			if v == "NONE" {
				cur.Open = true
			}
		case "scan_ssid", "":
			// regenerated / blank
		default:
			cur.extra = append(cur.extra, t)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// drop trailing blank header lines so the output stays tidy
	for len(w.header) > 0 && strings.TrimSpace(w.header[len(w.header)-1]) == "" {
		w.header = w.header[:len(w.header)-1]
	}
	return w, nil
}

// psks - the stored passwords by SSID (to keep them when the list is re-sent)
func readPSKs(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	var ssid, psk string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		switch {
		case t == "network={":
			ssid, psk = "", ""
		case t == "}":
			if ssid != "" && psk != "" {
				out[ssid] = psk
			}
		case strings.HasPrefix(t, "ssid="):
			if m := wpaQuoted.FindStringSubmatch(t[5:]); m != nil {
				ssid = m[1]
			} else {
				ssid = t[5:]
			}
		case strings.HasPrefix(t, "psk="):
			psk = t[4:] // quoted passphrase or 64 hex digits, written back verbatim
		}
	}
	return out
}

func quoteWPA(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func (w *wpaFile) render(psks map[string]string) (string, error) {
	var b strings.Builder
	for _, l := range w.header {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	for _, n := range w.nets {
		b.WriteString("\nnetwork={\n")
		b.WriteString("\tssid=" + quoteWPA(n.SSID) + "\n")
		b.WriteString("\tscan_ssid=1\n")
		if n.Open {
			b.WriteString("\tkey_mgmt=NONE\n")
		} else {
			psk := psks[n.SSID]
			if psk == "" {
				return "", fmt.Errorf("%s: no password", n.SSID)
			}
			b.WriteString("\tpsk=" + psk + "\n")
		}
		if n.Priority != 0 {
			b.WriteString("\tpriority=" + strconv.Itoa(n.Priority) + "\n")
		}
		for _, e := range n.extra {
			b.WriteString("\t" + e + "\n")
		}
		b.WriteString("}\n")
	}
	return b.String(), nil
}

func wpaCli(args ...string) (string, error) {
	cli, err := exec.LookPath("wpa_cli")
	if err != nil {
		cli = "/usr/sbin/wpa_cli"
	}
	return runCmd(cli, append([]string{"-i", wlanIface}, args...)...)
}

type wifiCurrent struct {
	SSID    string `json:"ssid"`
	BSSID   string `json:"bssid"`
	State   string `json:"state"`
	IP      string `json:"ip"`
	Freq    int    `json:"freq"`
	RSSIdBm *int   `json:"rssi_dbm"`
}

func wifiStatus() wifiCurrent {
	var c wifiCurrent
	if out, err := wpaCli("status"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			k, v, _ := strings.Cut(line, "=")
			switch k {
			case "ssid":
				c.SSID = v
			case "bssid":
				c.BSSID = v
			case "wpa_state":
				c.State = v
			case "ip_address":
				c.IP = v
			case "freq":
				c.Freq, _ = strconv.Atoi(v)
			}
		}
	}
	c.RSSIdBm = wifiInfo(wlanIface).RSSIdBm
	return c
}

type scanNet struct {
	SSID   string `json:"ssid"`
	Signal int    `json:"signal_dbm"`
	Secure bool   `json:"secure"`
	Freq   int    `json:"freq"`
}

// wifiScan - trigger a scan, wait, and return the best entry per SSID
func wifiScan() ([]scanNet, error) {
	if out, err := wpaCli("scan"); err != nil && !strings.Contains(out, "FAIL-BUSY") {
		return nil, fmt.Errorf("scan: %v %s", err, out)
	}
	time.Sleep(4 * time.Second)
	out, err := wpaCli("scan_results")
	if err != nil {
		return nil, fmt.Errorf("scan_results: %v", err)
	}
	best := map[string]scanNet{}
	for i, line := range strings.Split(out, "\n") {
		if i == 0 {
			continue // header
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 || f[4] == "" {
			continue
		}
		sig, _ := strconv.Atoi(f[2])
		freq, _ := strconv.Atoi(f[1])
		n := scanNet{SSID: f[4], Signal: sig, Secure: strings.Contains(f[3], "WPA") || strings.Contains(f[3], "WEP"), Freq: freq}
		if b, ok := best[n.SSID]; !ok || n.Signal > b.Signal {
			best[n.SSID] = n
		}
	}
	var list []scanNet
	for _, n := range best {
		list = append(list, n)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Signal > list[j].Signal })
	return list, nil
}

// wifiSave - the new list of networks: write the config file(s) and make
// wpa_supplicant re-read them
func (s *server) wifiSave(nets []wifiNet) error {
	if len(nets) == 0 {
		return errors.New("at least one network must stay (or the camera falls into portal mode at the next boot)")
	}
	s.relay.mu.Lock()
	defer s.relay.mu.Unlock()
	w, err := parseWPA(wpaConf)
	if err != nil {
		return err
	}
	psks := readPSKs(wpaConf)
	old := map[string]wifiNet{}
	for _, n := range w.nets {
		old[n.SSID] = n
	}
	seen := map[string]bool{}
	for i := range nets {
		n := &nets[i]
		n.SSID = strings.TrimSpace(n.SSID)
		if n.SSID == "" || len(n.SSID) > 32 {
			return fmt.Errorf("network %d: the name must be 1 to 32 characters", i+1)
		}
		if seen[n.SSID] {
			return fmt.Errorf("%s is listed twice", n.SSID)
		}
		seen[n.SSID] = true
		if n.Priority < 0 || n.Priority > 100 {
			return fmt.Errorf("%s: priority must be 0 to 100", n.SSID)
		}
		if n.PSK != "" {
			if len(n.PSK) < 8 || len(n.PSK) > 63 {
				return fmt.Errorf("%s: the password must be 8 to 63 characters", n.SSID)
			}
			if strings.ContainsAny(n.PSK, "\"\\\n") {
				return fmt.Errorf("%s: the password has a quote or a backslash in it", n.SSID)
			}
			psks[n.SSID] = quoteWPA(n.PSK)
			n.PSK = ""
		}
		if !n.Open && psks[n.SSID] == "" {
			return fmt.Errorf("%s: a password is needed", n.SSID)
		}
		if o, ok := old[n.SSID]; ok {
			n.extra = o.extra
		}
	}
	w.nets = nets
	text, err := w.render(psks)
	if err != nil {
		return err
	}
	// the first rewrite keeps the original next to it (comments and all)
	backupOnce(wpaConf)
	if err := writeText(wpaConf, text, 0o600); err != nil {
		return err
	}
	sd := filepath.Join(s.sd, wpaSDName)
	if exists(sd) {
		backupOnce(sd)
		if err := writeText(sd, text, 0o644); err != nil {
			return fmt.Errorf("written to %s, but not to the SD card: %v", wpaConf, err)
		}
	}
	if out, err := wpaCli("reconfigure"); err != nil || !strings.Contains(out, "OK") {
		return fmt.Errorf("saved, but wpa_supplicant did not reload: %v %s", err, out)
	}
	return nil
}

// backupOnce - copy path to path.before-webui unless that exists already
func backupOnce(path string) {
	bak := path + ".before-webui"
	if exists(bak) {
		return
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(bak, b, 0o600)
	}
}

func writeText(path, text string, mode os.FileMode) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(text), mode); err != nil {
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

// --- HTTP ---

func (s *server) handleWifiGet(w http.ResponseWriter, r *http.Request) {
	wf, err := parseWPA(wpaConf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	nets := wf.nets
	if nets == nil {
		nets = []wifiNet{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current":  wifiStatus(),
		"networks": nets,
		"sd_copy":  exists(filepath.Join(s.sd, wpaSDName)),
	})
}

func (s *server) handleWifiScan(w http.ResponseWriter, r *http.Request) {
	list, err := wifiScan()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if list == nil {
		list = []scanNet{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"networks": list})
}

func (s *server) handleWifiSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Networks []wifiNet `json:"networks"`
	}
	if err := readJSON(r, &body, 64<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := s.wifiSave(body.Networks); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
