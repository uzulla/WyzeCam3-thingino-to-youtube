package main

// Wi-Fi: the saved networks of /etc/wpa_supplicant.conf (several, with a
// priority), a scan of what is in the air, and the live connection. A change
// rewrites the file (header lines kept, network blocks regenerated), also on
// the SD card when wifi-from-sd keeps a copy there (device/S37wifi-from-sd:
// the card's file is applied at boot when it changed), then tells
// wpa_supplicant to re-read it. Passwords are never sent to the browser.

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	wpaSDName = "wpa_supplicant.conf"
	wlanIface = "wlan0"
)

// wpaConf - the config wpa_supplicant runs with (a variable: tests use a temp file)
var wpaConf = "/etc/wpa_supplicant.conf"

type wifiNet struct {
	SSID     string   `json:"ssid"`
	Priority int      `json:"priority"`
	Open     bool     `json:"open"`          // no password (key_mgmt=NONE)
	HasPSK   bool     `json:"has_psk"`       // a password is stored (never shown)
	PSK      string   `json:"psk,omitempty"` // only in requests: a new password
	hexSSID  bool     // the file had ssid=<hex> (written back that way)
	rawSSID  bool     // ...and it is not valid UTF-8: SSID holds the hex itself
	extra    []string // other lines of the block, kept as they are
}

// decodeSSID - the ssid= value of wpa_supplicant.conf: "quoted", or hex
// digits for names that are not printable (the file shows hex, the air has
// the bytes). Returns the name as text plus how it was written.
func decodeSSID(v string) (name string, isHex, raw bool) {
	if m := wpaQuoted.FindStringSubmatch(v); m != nil {
		return m[1], false, false // wpa_supplicant takes the quoted text as it is (no escapes)
	}
	if b, err := hex.DecodeString(v); err == nil && len(v) > 0 && len(v)%2 == 0 {
		if utf8.Valid(b) {
			return string(b), true, false
		}
	}
	return v, true, true
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
			cur.SSID, cur.hexSSID, cur.rawSSID = decodeSSID(v)
		case "psk":
			cur.HasPSK = true
		case "priority":
			cur.Priority, _ = strconv.Atoi(v)
		case "key_mgmt":
			if v == "NONE" {
				cur.Open = true
			} else {
				cur.extra = append(cur.extra, t) // WPA-PSK is the default; SAE, WPA-EAP, ... must stay
			}
		case "":
			// blank
		default:
			cur.extra = append(cur.extra, t) // scan_ssid, bssid, sae_password, ...
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
			ssid, _, _ = decodeSSID(t[5:])
		case strings.HasPrefix(t, "psk="):
			psk = t[4:] // quoted passphrase or 64 hex digits, written back verbatim
		}
	}
	return out
}

// quotable - can the SSID be written as "..."? wpa_supplicant reads the
// quoted text verbatim, so a quote or a backslash inside, or anything
// outside printable ASCII, goes as hex instead.
func quotable(s string) bool {
	for _, c := range s {
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return false
		}
	}
	return s != ""
}

func quoteWPA(s string) string { return `"` + s + `"` }

func hasKey(lines []string, key string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, key+"=") {
			return true
		}
	}
	return false
}

func (w *wpaFile) render(psks map[string]string) (string, error) {
	var b strings.Builder
	for _, l := range w.header {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	for _, n := range w.nets {
		b.WriteString("\nnetwork={\n")
		switch {
		case n.rawSSID:
			b.WriteString("\tssid=" + n.SSID + "\n")
		case n.hexSSID || !quotable(n.SSID):
			b.WriteString("\tssid=" + hex.EncodeToString([]byte(n.SSID)) + "\n")
		default:
			b.WriteString("\tssid=" + quoteWPA(n.SSID) + "\n")
		}
		if !hasKey(n.extra, "scan_ssid") {
			b.WriteString("\tscan_ssid=1\n")
		}
		if n.Open {
			b.WriteString("\tkey_mgmt=NONE\n")
		} else if psk := psks[n.SSID]; psk != "" {
			b.WriteString("\tpsk=" + psk + "\n")
		} else if !hasKey(n.extra, "key_mgmt") && !hasKey(n.extra, "sae_password") && !hasKey(n.extra, "password") {
			return "", fmt.Errorf("%s: no password", n.SSID)
		} // else: a network whose secret is in its own lines (SAE, EAP), kept as they were
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
	return parseScanResults(out), nil
}

// parseScanResults - wpa_cli scan_results: a header line, then
// bssid / frequency / signal / flags / ssid separated by tabs; the best
// signal per SSID wins
func parseScanResults(out string) []scanNet {
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
		flags := f[3]
		secure := strings.Contains(flags, "WPA") || strings.Contains(flags, "RSN") || strings.Contains(flags, "SAE") || strings.Contains(flags, "WEP")
		n := scanNet{SSID: unescapeWPA(f[4]), Signal: sig, Secure: secure, Freq: freq}
		if b, ok := best[n.SSID]; !ok || n.Signal > b.Signal {
			best[n.SSID] = n
		}
	}
	list := []scanNet{}
	for _, n := range best {
		list = append(list, n)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Signal > list[j].Signal })
	return list
}

// unescapeWPA - wpa_cli prints SSIDs printf-style: \xe3\x81\x82 for bytes
// outside printable ASCII, \\ \" \n \t \e for the rest
func unescapeWPA(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'x':
			if i+2 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					b.WriteByte(byte(v))
					i += 2
					continue
				}
			}
			b.WriteString("\\x")
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'e':
			b.WriteByte(0x1b)
		default:
			b.WriteByte(s[i]) // \\ and \"
		}
	}
	return b.String()
}

// wifiSave - the new list of networks: write the config file(s) and make
// wpa_supplicant re-read them. applied is false when the camera is an access
// point (wifi-ap / portal): the running supplicant holds the AP config then,
// and a reconfigure would drop the phone; the file waits for the reboot.
func (s *server) wifiSave(nets []wifiNet) (applied bool, err error) {
	if len(nets) == 0 {
		return false, errors.New("at least one network must stay (or the camera falls into portal mode at the next boot)")
	}
	s.relay.mu.Lock()
	defer s.relay.mu.Unlock()
	w, err := parseWPA(wpaConf)
	if err != nil {
		return false, err
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
		if o, ok := old[n.SSID]; ok {
			n.hexSSID, n.rawSSID, n.extra = o.hexSSID, o.rawSSID, o.extra
		}
		if n.SSID == "" || (!n.rawSSID && len(n.SSID) > 32) {
			return false, fmt.Errorf("network %d: the name must be 1 to 32 bytes", i+1)
		}
		if strings.ContainsAny(n.SSID, "\x00\r\n") || strings.ContainsAny(n.PSK, "\x00\r\n") {
			return false, fmt.Errorf("%s: the name or password has a line break in it", n.SSID)
		}
		if seen[n.SSID] {
			return false, fmt.Errorf("%s is listed twice", n.SSID)
		}
		seen[n.SSID] = true
		if n.Priority < 0 || n.Priority > 100 {
			return false, fmt.Errorf("%s: priority must be 0 to 100", n.SSID)
		}
		if n.PSK != "" {
			if len(n.PSK) < 8 || len(n.PSK) > 63 {
				return false, fmt.Errorf("%s: the password must be 8 to 63 characters", n.SSID)
			}
			if !quotable(n.PSK) {
				return false, fmt.Errorf("%s: the password may use printable ASCII without quotes or backslashes", n.SSID)
			}
			psks[n.SSID] = quoteWPA(n.PSK)
			n.PSK = ""
		}
		if !n.Open && psks[n.SSID] == "" && !hasKey(n.extra, "key_mgmt") && !hasKey(n.extra, "sae_password") {
			return false, fmt.Errorf("%s: a password is needed", n.SSID)
		}
	}
	// Thingino's S38wpa_supplicant (and wifi-from-sd) take a file without a
	// psk= line for "not set up" and start the setup portal at boot: a list
	// of only open or SAE networks would lose the camera at the next reboot
	hasPSK := false
	for _, n := range nets {
		if !n.Open && psks[n.SSID] != "" {
			hasPSK = true
		}
	}
	if !hasPSK {
		return false, errors.New("at least one network with a WPA2 password must stay: the camera's Wi-Fi start-up treats a list without one as unset and opens the setup portal")
	}
	w.nets = nets
	text, err := w.render(psks)
	if err != nil {
		return false, err
	}
	// the first rewrite keeps the original next to it (comments and all)
	backupOnce(wpaConf)
	prevBytes, _ := os.ReadFile(wpaConf)
	previous := string(prevBytes)
	if err := writeText(wpaConf, text, 0o600); err != nil {
		return false, err
	}
	sd := filepath.Join(s.sd, wpaSDName)
	if exists(sd) {
		backupOnce(sd)
		if err := writeText(sd, text, 0o644); err != nil {
			return false, fmt.Errorf("written to %s, but not to the SD card: %v", wpaConf, err)
		}
	}
	if exists("/run/wlan_ap_mode") || exists("/run/portal_mode") {
		return false, nil // the access point stays up; the saved networks come with the reboot
	}
	wasUp := wifiStatus().State == "COMPLETED"
	if out, err := wpaCli("reconfigure"); err != nil || !strings.Contains(out, "OK") {
		return false, fmt.Errorf("saved, but wpa_supplicant did not reload: %v %s", err, out)
	}
	if wasUp && previous != "" {
		go s.wifiWatch(previous, sd)
	}
	return true, nil
}

// wifiWatch - after a save that the camera was connected before: if it is
// not connected again within 30 s (a typo in the only reachable network),
// the previous file comes back and is loaded again, so the camera is not
// lost until someone holds the button for the access point
func (s *server) wifiWatch(previous, sd string) {
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		if wifiStatus().State == "COMPLETED" {
			s.setWifiNote("")
			return
		}
	}
	s.relay.mu.Lock()
	defer s.relay.mu.Unlock()
	_ = writeText(wpaConf, previous, 0o600)
	if sd != "" && exists(sd) {
		_ = writeText(sd, previous, 0o644)
	}
	_, _ = wpaCli("reconfigure")
	msg := "The last Wi-Fi save was undone: the camera did not reconnect within 30 s. Check the name and password."
	log.Printf("wifi: %s", msg)
	s.setWifiNote(msg)
}

func (s *server) setWifiNote(msg string) {
	s.wifiMu.Lock()
	s.wifiNote = msg
	s.wifiMu.Unlock()
}

func (s *server) getWifiNote() string {
	s.wifiMu.Lock()
	defer s.wifiMu.Unlock()
	return s.wifiNote
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
		"ap_mode":  exists("/run/wlan_ap_mode") || exists("/run/portal_mode"),
		"note":     s.getWifiNote(),
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
	applied, err := s.wifiSave(body.Networks)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "applied": applied})
}
