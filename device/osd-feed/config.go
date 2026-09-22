package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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
}

// SlotConfig: what to draw in one of prudynt's text overlays. The slot name is
// prudynt's (textfile, textfile2, textfile3); path / cols / rows come from the
// running prudynt and are only needed here when it cannot be asked.
type SlotConfig struct {
	Template string `json:"template"`
	Path     string `json:"path"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
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
	if len(c.Slots) == 0 {
		return nil, fmt.Errorf("%s: no slots", path)
	}
	for name := range c.Slots {
		if !validSlot(name) {
			return nil, fmt.Errorf("%s: unknown slot %q (textfile, textfile2 or textfile3)", path, name)
		}
	}
	for name, s := range c.Sources {
		if _, ok := sourceTypes[s.Type]; !ok {
			return nil, fmt.Errorf("%s: source %q: unknown type %q", path, name, s.Type)
		}
	}
	return &c, nil
}

func validSlot(name string) bool {
	return name == "textfile" || name == "textfile2" || name == "textfile3"
}
