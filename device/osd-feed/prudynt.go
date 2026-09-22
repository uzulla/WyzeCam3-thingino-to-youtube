package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// SlotGeometry - the slot as the running prudynt has it (osd.textfileN.*).
type SlotGeometry struct {
	Path    string
	Cols    int
	Rows    int
	Enabled bool
}

// The documented defaults (docs/osd.md), used when prudynt cannot be asked
var defaultGeometry = map[string]SlotGeometry{
	"textfile":  {Path: "/run/prudynt/osd-text", Cols: 40, Rows: 4},
	"textfile2": {Path: "/run/prudynt/osd-text2", Cols: 24, Rows: 2},
	"textfile3": {Path: "/run/prudynt/osd-text3", Cols: 24, Rows: 2},
}

// askPrudynt - the geometry of the named slots from the running prudynt via
// prudyntctl (the same query docs/osd.md shows). Slots it does not know (a
// prudynt without the OSD patch) fall back to the defaults; the second result
// says whether prudynt answered at all.
func askPrudynt(ctx context.Context, names []string) (map[string]SlotGeometry, bool) {
	out := map[string]SlotGeometry{}
	for _, n := range names {
		out[n] = defaultGeometry[n]
	}
	q := map[string]any{}
	for _, n := range names {
		q[n] = nil
	}
	body, _ := json.Marshal(map[string]any{"osd": q})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "prudyntctl", "json", string(body)).Output()
	if err != nil {
		return out, false
	}
	return parseGeometry(raw, out)
}

// parseGeometry - prudynt's answer ({"textfile":{"path":...,"cols":..},...})
// merged over the defaults. An empty or {} answer (stock prudynt, or one that
// is still starting) leaves the defaults and reports no answer; a slot missing
// from the answer or without a path keeps its default.
func parseGeometry(raw []byte, out map[string]SlotGeometry) (map[string]SlotGeometry, bool) {
	var ans map[string]struct {
		Path    string `json:"path"`
		Cols    int    `json:"cols"`
		Rows    int    `json:"rows"`
		Enabled bool   `json:"enabled"`
	}
	if json.Unmarshal(raw, &ans) != nil || len(ans) == 0 {
		return out, false
	}
	for n, a := range ans {
		if _, want := out[n]; !want || a.Path == "" {
			continue
		}
		out[n] = SlotGeometry{Path: a.Path, Cols: a.Cols, Rows: a.Rows, Enabled: a.Enabled}
	}
	return out, true
}

func (g SlotGeometry) String() string {
	state := "enabled"
	if !g.Enabled {
		state = "DISABLED"
	}
	return fmt.Sprintf("%s %dx%d %s", g.Path, g.Cols, g.Rows, state)
}
