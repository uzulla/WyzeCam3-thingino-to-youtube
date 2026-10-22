package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Config is osd-feed.json (on the SD card next to the binary, see docs/osd-feed.md).
//
//	{
//	  "interval_ms": 250,
//	  "sources": { "mem": {"type": "meminfo", "interval_ms": 1000}, ... },
//	  "slots":   { "textfile": {"template": "MEM {{.mem.free_mb}}MB\n..."}, ... }
//	}
type Config struct {
	IntervalMs  int                     `json:"interval_ms"`   // how often the slots are redrawn (default 250)
	ClearOnExit *bool                   `json:"clear_on_exit"` // remove the overlay files on exit (default true)
	Sources     map[string]SourceConfig `json:"sources"`
	Slots       map[string]SlotConfig   `json:"slots"`
}

// SourceConfig: one data source, polled on its own schedule. Fields other than
// Type / IntervalMs are for particular types (see source.go).
type SourceConfig struct {
	Type       string            `json:"type"`
	IntervalMs int               `json:"interval_ms"`
	TimeoutMs  int               `json:"timeout_ms"` // http: per request (default 3000)
	URL        string            `json:"url"`        // http
	Headers    map[string]string `json:"headers"`    // http
	Step       int               `json:"step"`       // tick: increment per poll (default 1)
	Max        int               `json:"max"`        // tick: wrap after this value (default 100)
	Listen     string            `json:"listen"`     // udp: address to receive on (":5005")
	Match      map[string]string `json:"match"`      // udp: keep only objects whose members have these string values
	StaleMs    int               `json:"stale_ms"`   // udp: not ok after this long without a datagram (default 1000)
}

// SlotConfig: what to draw in one of prudynt's text overlays. The slot name is
// prudynt's (textfile, textfile2, textfile3); path / cols / rows come from the
// running prudynt and are only needed here when it cannot be asked.
type SlotConfig struct {
	Template string `json:"template"`
	Path     string `json:"path"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	// imagefile only (image.go): the PNG to show, and how to fit it into the
	// slot's width x height ("contain" = shrink to fit keeping the aspect
	// ratio, "none" = as is, cropped). width/height pin the geometry like
	// cols/rows do for text.
	PNG    string `json:"png"`
	Fit    string `json:"fit"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	// imagefile only: where in the slot the PNG goes (default: the whole
	// slot), so that it can share the slot with a G-ball
	PNGBox *Box `json:"png_box"`
	// imagefile only: a G-ball (gball.go) drawn from a source's values
	GBall *GBallConfig `json:"gball"`
}

// Box is a rectangle inside the image slot, in pixels from its top left.
type Box struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// GBallConfig: a G-ball (friction circle) in the image slot. The ball sits
// where the G pushes things in the car ("a ball in a bowl"): up on braking,
// down on acceleration, left in a right-hand turn. It is hidden while the
// source is not ok (nothing received lately) or the values are missing.
type GBallConfig struct {
	Source string `json:"source"` // the source the values come from (required)
	// Keys in that source: sideways G, positive to the right, and lengthwise
	// G, positive forwards (the accelerometer's reading: + on acceleration).
	// Defaults are esp32-racebox's (RaceBox: X front/back, Y right/left).
	Lateral      string `json:"lateral"`      // default "g_y"
	Longitudinal string `json:"longitudinal"` // default "g_x"
	// The RaceBox documents its axes but not their signs: flip here when the
	// ball goes the wrong way (tilt the RaceBox: the ball must roll downhill)
	InvertLateral      bool    `json:"invert_lateral"`
	InvertLongitudinal bool    `json:"invert_longitudinal"`
	MaxG               float64 `json:"max_g"` // G at the outer ring (default 1.5); rings every 0.5 G
	Trail              *int    `json:"trail"` // past positions drawn behind the ball, one per redraw (default 8, 0 = none)
	X                  int     `json:"x"`     // top left in the slot (default 0, 0)
	Y                  int     `json:"y"`
	Size               int     `json:"size"` // side in pixels (default: what fits in the slot from x, y)
}

// interval - the redraw period. prudynt looks at the files every 100 ms, so
// anything shorter only burns CPU: clamp there.
func (c *Config) interval() time.Duration {
	if c.IntervalMs <= 0 {
		return 250 * time.Millisecond
	}
	if c.IntervalMs < 100 {
		return 100 * time.Millisecond
	}
	return time.Duration(c.IntervalMs) * time.Millisecond
}

func (c *Config) clearOnExit() bool { return c.ClearOnExit == nil || *c.ClearOnExit }

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	// A misspelt key ("intervl_ms") must not silently become the default
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// One JSON value and nothing after it (a second object pasted below the
	// first would otherwise be ignored without a word)
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return nil, fmt.Errorf("%s: unexpected content after the JSON object", path)
	}
	if len(c.Slots) == 0 {
		return nil, fmt.Errorf("%s: no slots", path)
	}
	for name, sc := range c.Slots {
		if !validSlot(name) {
			return nil, fmt.Errorf("%s: unknown slot %q (textfile, textfile2, textfile3 or imagefile)", path, name)
		}
		if name == "imagefile" {
			if sc.PNG == "" && sc.GBall == nil {
				return nil, fmt.Errorf("%s: slot imagefile needs \"png\" (the file to show) or \"gball\"", path)
			}
			if b := sc.PNGBox; b != nil && (sc.PNG == "" || b.X < 0 || b.Y < 0 || b.Width < 1 || b.Height < 1) {
				return nil, fmt.Errorf("%s: slot imagefile: png_box needs png, x/y >= 0 and width/height >= 1", path)
			}
			if g := sc.GBall; g != nil {
				if g.Source == "" {
					return nil, fmt.Errorf("%s: slot imagefile: gball needs \"source\"", path)
				}
				if _, ok := c.Sources[g.Source]; !ok {
					return nil, fmt.Errorf("%s: slot imagefile: gball source %q is not in sources", path, g.Source)
				}
				if g.MaxG < 0 || g.X < 0 || g.Y < 0 || g.Size < 0 || (g.Size != 0 && g.Size < minGBallSize) {
					return nil, fmt.Errorf("%s: slot imagefile: gball max_g, x, y must not be negative, size at least %d", path, minGBallSize)
				}
				if g.Trail != nil && (*g.Trail < 0 || *g.Trail > maxGBallTrail) {
					return nil, fmt.Errorf("%s: slot imagefile: gball trail must be 0..%d", path, maxGBallTrail)
				}
			}
			if sc.Fit != "" && sc.Fit != "contain" && sc.Fit != "none" {
				return nil, fmt.Errorf("%s: slot imagefile: fit must be \"contain\" or \"none\"", path)
			}
			// prudynt's own rules for osd.imagefile.width/height: a value it
			// would refuse makes a file it can never show
			if sc.Width < 0 || sc.Width > maxImageWidth || sc.Width%2 != 0 || (sc.Width != 0 && sc.Width < 2) {
				return nil, fmt.Errorf("%s: slot imagefile: width must be an even number 2..%d (or left out)", path, maxImageWidth)
			}
			if sc.Height < 0 || sc.Height > maxImageHeight || sc.Height%2 != 0 || (sc.Height != 0 && sc.Height < 2) {
				return nil, fmt.Errorf("%s: slot imagefile: height must be an even number 2..%d (or left out)", path, maxImageHeight)
			}
		} else if sc.PNG != "" || sc.Fit != "" || sc.Width != 0 || sc.Height != 0 || sc.PNGBox != nil || sc.GBall != nil {
			return nil, fmt.Errorf("%s: slot %s: png/fit/width/height/png_box/gball are for the imagefile slot", path, name)
		}
	}
	for name, s := range c.Sources {
		if _, ok := sourceTypes[s.Type]; !ok {
			return nil, fmt.Errorf("%s: source %q: unknown type %q", path, name, s.Type)
		}
		// udp only hands out what was received: polling that every second
		// (the default for the others) would show a G-ball at 1 Hz. Once per
		// redraw is enough; each poll parses a datagram (0.5 ms on the camera)
		if s.Type == "udp" && s.IntervalMs <= 0 {
			s.IntervalMs = 100
			c.Sources[name] = s
		}
	}
	return &c, nil
}

func validSlot(name string) bool {
	return name == "textfile" || name == "textfile2" || name == "textfile3" || name == "imagefile"
}
