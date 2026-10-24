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
	// image slots only (image.go): the PNG to show, and how to fit it into the
	// slot's width x height ("contain" = shrink to fit keeping the aspect
	// ratio, "none" = as is, cropped). width/height pin the geometry like
	// cols/rows do for text.
	PNG    string `json:"png"`
	Fit    string `json:"fit"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	// image slots only: where in the slot the PNG goes (default: the whole
	// slot), so that it can share the slot with a G-ball
	PNGBox *Box `json:"png_box"`
	// image slots only: a G-ball (gball.go) drawn from a source's values
	GBall *GBallConfig `json:"gball"`
	// image slots only: a track map (track.go) plotted from a source's GPS position
	Track *TrackConfig `json:"track"`
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

// TrackConfig: a track map in the image slot, plotted from a source's GPS
// position at every redraw (track.go). The latest position is marked while
// the source is fresh; the plotted track stays, the oldest points rubbed out
// beyond "points".
type TrackConfig struct {
	Source string `json:"source"` // the source the values come from (required)
	// Keys in that source, in degrees; defaults are esp32-racebox's. A
	// position whose valid key is false is not plotted (a source without that
	// key: every position counts).
	Lat    string `json:"lat"`    // default "lat"
	Lon    string `json:"lon"`    // default "lon"
	Valid  string `json:"valid"`  // default "latlon_valid"
	Points int    `json:"points"` // positions kept on the map (default 6000: 10 minutes at a 100 ms redraw)
	// a position closer than this to the last plotted one is not plotted, so
	// that standing still does not use up "points" (default 3; 0 = every one)
	MinMoveM *float64 `json:"min_move_m"`
	X        int      `json:"x"` // top left in the slot (default 0, 0)
	Y        int      `json:"y"`
	Width    int      `json:"width"` // default: the rest of the slot from x, y
	Height   int      `json:"height"`
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
			return nil, fmt.Errorf("%s: unknown slot %q (textfile, textfile2, textfile3, imagefile, imagefile2 or imagefile3)", path, name)
		}
		if isImageSlot(name) {
			if sc.PNG == "" && sc.GBall == nil && sc.Track == nil {
				return nil, fmt.Errorf("%s: slot %s needs \"png\" (the file to show), \"gball\" or \"track\"", path, name)
			}
			if b := sc.PNGBox; b != nil && (sc.PNG == "" || b.X < 0 || b.Y < 0 || b.Width < 1 || b.Height < 1) {
				return nil, fmt.Errorf("%s: slot %s: png_box needs png, x/y >= 0 and width/height >= 1", path, name)
			}
			if g := sc.GBall; g != nil {
				if g.Source == "" {
					return nil, fmt.Errorf("%s: slot %s: gball needs \"source\"", path, name)
				}
				if _, ok := c.Sources[g.Source]; !ok {
					return nil, fmt.Errorf("%s: slot %s: gball source %q is not in sources", path, name, g.Source)
				}
				// The upper limits keep a typo from building the rings forever
				// (a ring every 0.5 G) or allocating gigabytes at the first draw
				if g.MaxG < 0 || g.MaxG > maxGBallMaxG || g.X < 0 || g.Y < 0 || g.Size < 0 || g.Size > maxImageHeight || (g.Size != 0 && g.Size < minGBallSize) {
					return nil, fmt.Errorf("%s: slot %s: gball max_g must be 0..%d, x and y >= 0, size %d..%d (or left out)", path, name, maxGBallMaxG, minGBallSize, maxImageHeight)
				}
				if g.Trail != nil && (*g.Trail < 0 || *g.Trail > maxGBallTrail) {
					return nil, fmt.Errorf("%s: slot %s: gball trail must be 0..%d", path, name, maxGBallTrail)
				}
			}
			if tr := sc.Track; tr != nil {
				if tr.Source == "" {
					return nil, fmt.Errorf("%s: slot %s: track needs \"source\"", path, name)
				}
				if _, ok := c.Sources[tr.Source]; !ok {
					return nil, fmt.Errorf("%s: slot %s: track source %q is not in sources", path, name, tr.Source)
				}
				if tr.Points < 0 || tr.Points > maxTrackPoints || (tr.MinMoveM != nil && *tr.MinMoveM < 0) {
					return nil, fmt.Errorf("%s: slot %s: track points must be 0..%d (0 = default), min_move_m >= 0", path, name, maxTrackPoints)
				}
				if tr.X < 0 || tr.Y < 0 || tr.Width < 0 || tr.Width > maxImageWidth || tr.Height < 0 || tr.Height > maxImageHeight ||
					(tr.Width != 0 && tr.Width < minTrackSize) || (tr.Height != 0 && tr.Height < minTrackSize) {
					return nil, fmt.Errorf("%s: slot %s: track x and y must be >= 0, width %d..%d and height %d..%d (or left out)", path, name, minTrackSize, maxImageWidth, minTrackSize, maxImageHeight)
				}
			}
			if sc.Fit != "" && sc.Fit != "contain" && sc.Fit != "none" {
				return nil, fmt.Errorf("%s: slot %s: fit must be \"contain\" or \"none\"", path, name)
			}
			// prudynt's own rules for osd.imagefile.width/height: a value it
			// would refuse makes a file it can never show
			if sc.Width < 0 || sc.Width > maxImageWidth || sc.Width%2 != 0 || (sc.Width != 0 && sc.Width < 2) {
				return nil, fmt.Errorf("%s: slot %s: width must be an even number 2..%d (or left out)", path, name, maxImageWidth)
			}
			if sc.Height < 0 || sc.Height > maxImageHeight || sc.Height%2 != 0 || (sc.Height != 0 && sc.Height < 2) {
				return nil, fmt.Errorf("%s: slot %s: height must be an even number 2..%d (or left out)", path, name, maxImageHeight)
			}
		} else if sc.PNG != "" || sc.Fit != "" || sc.Width != 0 || sc.Height != 0 || sc.PNGBox != nil || sc.GBall != nil || sc.Track != nil {
			return nil, fmt.Errorf("%s: slot %s: png/fit/width/height/png_box/gball/track are for the imagefile slots", path, name)
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
	return name == "textfile" || name == "textfile2" || name == "textfile3" || isImageSlot(name)
}

// isImageSlot - prudynt's three image overlays (e.g. a logo, a G-ball and a
// track map, each placed on its own)
func isImageSlot(name string) bool {
	return name == "imagefile" || name == "imagefile2" || name == "imagefile3"
}
