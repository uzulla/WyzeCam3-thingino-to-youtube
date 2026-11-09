package main

// OSD layout: the seven overlay elements of the OSD-patched prudynt (the
// timestamp "burnin", three text boxes, three images; docs/osd.md), moved
// and sized on the preview. Changes go to prudynt at once (prudyntctl json)
// and into osd.* of prudynt-osd.json on the SD card so they come back after
// a restart (osd-config sends that file). What the text boxes show and what
// the images contain is osd-feed's business (osd-feed.json: templates, the
// logo PNG, the G-ball, the track map); those edits restart osd-feed.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	osdFeedName   = "osd-feed.json"
	osdFeedInit   = "/etc/init.d/S94osd-feed"
	logoDefault   = "logo.png"
	logoMaxBytes  = 4 << 20
	logoMaxSide   = 1024    // osd-feed decodes the whole PNG in RAM (96 MB camera)
	logoMaxPixels = 1 << 20 // 1 Mpx: the OSD cannot show more than the stream's width anyway
	textCellW     = 8       // prudynt's 8x8 bitmap font: cell 8 x 9 px, padding 2 px, times scale
	textCellH     = 9
	textCellPad   = 2
	textMaxCols   = 128
	textMaxRows   = 32
	osdMaxScale   = 10
	osdMinImageWH = 16
)

var osdElements = []string{"burnin", "textfile", "textfile2", "textfile3", "imagefile", "imagefile2", "imagefile3"}

func isText(name string) bool  { return strings.HasPrefix(name, "textfile") }
func isImage(name string) bool { return strings.HasPrefix(name, "imagefile") }

// osdElem - the fields webui edits; pointers so that "not given" is distinct
type osdElem struct {
	Enabled *bool `json:"enabled,omitempty"`
	PosX    *int  `json:"pos_x,omitempty"`
	PosY    *int  `json:"pos_y,omitempty"`
	Scale   *int  `json:"scale,omitempty"` // text and burnin; 0 = auto (stream width / 480)
	Cols    *int  `json:"cols,omitempty"`
	Rows    *int  `json:"rows,omitempty"`
	Width   *int  `json:"width,omitempty"` // images
	Height  *int  `json:"height,omitempty"`
}

// osdQuery - all elements and the stream size, from the running prudynt
func osdQuery() (elems map[string]osdElem, w, h int, err error) {
	q := map[string]any{"stream0": map[string]any{"width": nil, "height": nil}, "osd": map[string]any{}}
	for _, n := range osdElements {
		q["osd"].(map[string]any)[n] = nil
	}
	out, err := runCmd("prudyntctl", "json", string(mustJSON(q)))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("prudyntctl: %v %s", err, out)
	}
	var v struct {
		Stream0 struct{ Width, Height int } `json:"stream0"`
		OSD     map[string]osdElem          `json:"osd"`
	}
	// prudynt answers {"stream0":{...},"burnin":{...},...} (the osd keys at
	// the top level) or nested, depending on the build: accept both
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		return nil, 0, 0, fmt.Errorf("prudyntctl answered %q", out)
	}
	if raw, ok := top["stream0"]; ok {
		_ = json.Unmarshal(raw, &v.Stream0)
	}
	v.OSD = map[string]osdElem{}
	if raw, ok := top["osd"]; ok {
		_ = json.Unmarshal(raw, &v.OSD)
	}
	for _, n := range osdElements {
		if raw, ok := top[n]; ok {
			var e osdElem
			if json.Unmarshal(raw, &e) == nil {
				v.OSD[n] = e
			}
		}
	}
	if v.Stream0.Width == 0 || v.Stream0.Height == 0 {
		v.Stream0.Width, v.Stream0.Height = 1280, 720
	}
	return v.OSD, v.Stream0.Width, v.Stream0.Height, nil
}

// osdValidate - keep prudynt's limits (docs/osd.md); positions may be
// negative (from the right/bottom edge)
func osdValidate(name string, e osdElem, w, h int) error {
	found := false
	for _, n := range osdElements {
		if n == name {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("unknown element %q", name)
	}
	chk := func(v *int, lo, hi int, what string) error {
		if v != nil && (*v < lo || *v > hi) {
			return fmt.Errorf("%s: %s must be %d to %d", name, what, lo, hi)
		}
		return nil
	}
	if name == "burnin" && (e.PosX != nil || e.PosY != nil || e.Cols != nil || e.Rows != nil || e.Width != nil || e.Height != nil) {
		return errors.New("burnin: only enabled and scale can be set")
	}
	if isImage(name) && (e.Scale != nil || e.Cols != nil || e.Rows != nil) {
		return fmt.Errorf("%s: an image has width and height, not scale/cols/rows", name)
	}
	if isText(name) && (e.Width != nil || e.Height != nil) {
		return fmt.Errorf("%s: a text box has cols/rows/scale, not width/height", name)
	}
	for _, c := range []error{
		chk(e.PosX, -w, w, "pos_x"), chk(e.PosY, -h, h, "pos_y"),
		chk(e.Scale, 0, osdMaxScale, "scale"), chk(e.Cols, 1, textMaxCols, "cols"), chk(e.Rows, 1, textMaxRows, "rows"),
		chk(e.Width, osdMinImageWH, w, "width"), chk(e.Height, osdMinImageWH, h, "height"),
	} {
		if c != nil {
			return c
		}
	}
	if (e.Width != nil && *e.Width%2 != 0) || (e.Height != nil && *e.Height%2 != 0) {
		return fmt.Errorf("%s: width and height must be even", name)
	}
	return nil
}

// osdApply - to prudynt now, then into the SD card's prudynt-osd.json
func (s *server) osdApply(name string, e osdElem) error {
	fields := map[string]any{}
	b, _ := json.Marshal(e)
	_ = json.Unmarshal(b, &fields) // only the fields that were given
	if len(fields) == 0 {
		return errors.New("nothing to change")
	}
	req := mustJSON(map[string]any{"osd": map[string]any{name: fields}})
	if out, err := runCmd("prudyntctl", "json", string(req)); err != nil {
		return fmt.Errorf("prudyntctl: %v %s", err, out)
	}
	path := filepath.Join(s.sd, osdConfigName)
	if !exists(path) {
		log.Printf("osd: no %s, the change holds until prudynt restarts", path)
		return nil
	}
	s.relay.mu.Lock()
	defer s.relay.mu.Unlock()
	cfg, err := readJSONFile(path)
	if err != nil {
		return err
	}
	osd, _ := cfg["osd"].(map[string]any)
	if osd == nil {
		osd = map[string]any{}
	}
	cur, _ := osd[name].(map[string]any)
	if cur == nil {
		cur = map[string]any{}
	}
	for k, v := range fields {
		cur[k] = v
	}
	osd[name] = cur
	cfg["osd"] = osd
	return writeJSONFile(path, cfg, 0o644)
}

// --- osd-feed (what the slots show) ---

type feedSlot struct {
	Template *string `json:"template,omitempty"` // text slots
	PNG      string  `json:"png,omitempty"`      // image slots: a PNG on the SD card
	GBall    bool    `json:"gball"`              // image slots: the G-ball
	Track    bool    `json:"track"`              // image slots: the track map
}

func (s *server) feedPath() string { return filepath.Join(s.sd, osdFeedName) }

func (s *server) feedRead() (map[string]feedSlot, error) {
	cfg, err := readJSONFile(s.feedPath())
	if err != nil {
		return nil, err
	}
	slots, _ := cfg["slots"].(map[string]any)
	out := map[string]feedSlot{}
	for name, v := range slots {
		m, _ := v.(map[string]any)
		var fs feedSlot
		if t, ok := m["template"].(string); ok {
			fs.Template = &t
		}
		fs.PNG, _ = m["png"].(string)
		_, fs.GBall = m["gball"]
		_, fs.Track = m["track"]
		out[name] = fs
	}
	return out, nil
}

// feedEdit - change one slot of osd-feed.json and restart osd-feed. The
// shape osd-feed expects (docs/osd-feed.md): a text slot has "template", an
// image slot has at least one of "png", "gball", "track".
func (s *server) feedEdit(slot string, template *string, gball, track *bool) error {
	if !isText(slot) && !isImage(slot) {
		return fmt.Errorf("unknown slot %q", slot)
	}
	s.relay.mu.Lock()
	defer s.relay.mu.Unlock()
	cfg, err := readJSONFile(s.feedPath())
	if err != nil {
		return err
	}
	slots, _ := cfg["slots"].(map[string]any)
	if slots == nil {
		slots = map[string]any{}
	}
	m, _ := slots[slot].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	if template != nil {
		if !isText(slot) {
			return fmt.Errorf("%s: only a text slot has a template", slot)
		}
		if len(*template) > 2000 {
			return errors.New("the template is too long (2000 bytes at most)")
		}
		m["template"] = *template
	}
	if gball != nil {
		if !isImage(slot) {
			return fmt.Errorf("%s: only an image slot can show the G-ball", slot)
		}
		if *gball {
			if _, ok := m["gball"]; !ok {
				m["gball"] = map[string]any{"source": "racebox", "invert_lateral": true}
			}
		} else {
			delete(m, "gball")
		}
	}
	if track != nil {
		if !isImage(slot) {
			return fmt.Errorf("%s: only an image slot can show the track map", slot)
		}
		if *track {
			if _, ok := m["track"]; !ok {
				m["track"] = map[string]any{"source": "racebox"}
			}
		} else {
			delete(m, "track")
		}
	}
	if isImage(slot) {
		_, hasPNG := m["png"]
		_, hasG := m["gball"]
		_, hasT := m["track"]
		if !hasPNG && !hasG && !hasT {
			delete(slots, slot) // an image slot with nothing to draw is not allowed
		} else {
			slots[slot] = m
		}
	} else {
		slots[slot] = m
	}
	cfg["slots"] = slots
	if err := writeJSONFile(s.feedPath(), cfg, 0o644); err != nil {
		return err
	}
	if out, err := runCmd("sh", osdFeedInit, "restart"); err != nil {
		return fmt.Errorf("osd-feed restart: %v %s", err, out)
	}
	return nil
}

// logoPath - where the logo PNG lives (slots.imagefile.png, default logo.png on the SD card)
func (s *server) logoPath() string {
	slots, err := s.feedRead()
	if err == nil {
		if p := slots["imagefile"].PNG; p != "" {
			return p
		}
	}
	return filepath.Join(s.sd, logoDefault)
}

// --- HTTP ---

func (s *server) handleOSDGet(w http.ResponseWriter, r *http.Request) {
	elems, sw, sh, err := osdQuery()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	resp := map[string]any{
		"stream":   map[string]int{"width": sw, "height": sh},
		"elements": elems,
		"cell":     map[string]int{"w": textCellW, "h": textCellH, "pad": textCellPad},
	}
	if slots, err := s.feedRead(); err == nil {
		resp["feed"] = slots
	} else {
		resp["feed_error"] = err.Error()
	}
	resp["logo_path"] = s.logoPath()
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleOSDElement(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		osdElem
	}
	if err := readJSON(r, &body, 4096); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	_, sw, sh, err := osdQuery()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if err := osdValidate(body.Name, body.osdElem, sw, sh); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if body.osdElem == (osdElem{}) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "nothing to change"})
		return
	}
	if err := s.osdApply(body.Name, body.osdElem); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleOSDFeed(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Slot     string  `json:"slot"`
		Template *string `json:"template"`
		GBall    *bool   `json:"gball"`
		Track    *bool   `json:"track"`
	}
	if err := readJSON(r, &body, 16<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if body.Template == nil && body.GBall == nil && body.Track == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "nothing to change"})
		return
	}
	if err := s.feedEdit(body.Slot, body.Template, body.GBall, body.Track); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restarted": "osd-feed"})
}

// handleLogoUpload - a PNG from the phone becomes the logo (osd-feed notices
// the new file by its mtime and converts it within a second)
func (s *server) handleLogoUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, logoMaxBytes+64<<10)
	f, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no file (PNG up to 4 MB): " + err.Error()})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, logoMaxBytes+1))
	if err != nil || len(data) > logoMaxBytes {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the file is over 4 MB"})
		return
	}
	cfgImg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "not a PNG file"})
		return
	}
	if cfgImg.Width > logoMaxSide || cfgImg.Height > logoMaxSide || cfgImg.Width*cfgImg.Height > logoMaxPixels {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("the PNG is %dx%d; 1024 px per side and 1 megapixel at most (the camera decodes it in RAM)", cfgImg.Width, cfgImg.Height)})
		return
	}
	if !mounted(s.sd) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "no SD card"})
		return
	}
	path := s.logoPath()
	s.relay.mu.Lock() // one writer of SD files at a time (and one upload at a time)
	err = writeBytes(path, data, 0o644)
	s.relay.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	log.Printf("logo: %s replaced (%dx%d PNG, %d bytes)", path, cfgImg.Width, cfgImg.Height, len(data))
	resp := map[string]any{"ok": true, "path": path, "width": cfgImg.Width, "height": cfgImg.Height}
	// make sure osd-feed draws it in the logo slot
	if slots, _ := s.feedRead(); slots["imagefile"].PNG == "" {
		if err := s.feedSetPNG("imagefile", path); err != nil {
			resp["warning"] = "saved, but osd-feed could not be pointed at it: " + err.Error()
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeBytes - tmp + rename, with a name no other writer uses
func writeBytes(path string, data []byte, mode os.FileMode) error {
	tmp := fmt.Sprintf("%s.%d.new", path, os.Getpid())
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// feedSetPNG - point an image slot at a PNG and restart osd-feed
func (s *server) feedSetPNG(slot, path string) error {
	s.relay.mu.Lock()
	cfg, err := readJSONFile(s.feedPath())
	if err != nil {
		s.relay.mu.Unlock()
		return err
	}
	slots, _ := cfg["slots"].(map[string]any)
	if slots == nil {
		slots = map[string]any{}
	}
	m, _ := slots[slot].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	m["png"] = path
	if _, ok := m["fit"]; !ok {
		m["fit"] = "contain"
	}
	slots[slot] = m
	cfg["slots"] = slots
	err = writeJSONFile(s.feedPath(), cfg, 0o644)
	s.relay.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = runCmd("sh", osdFeedInit, "restart")
	return err
}
