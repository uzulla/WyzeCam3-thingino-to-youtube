package main

import (
	"fmt"
	"strings"
	"text/template"
)

// Template functions available in a slot template, besides text/template's
// own (printf, if, range, ...):
//
//	{{bar 30 .tick.pct}}        [#########---------------------]  (width 30, 0..100 %)
//	{{lpad 8 .mem.free_mb}}     right-aligned in 8 columns
//	{{rpad 8 .clock.hms}}       left-aligned in 8 columns
//	{{trunc 10 .car.name}}      at most 10 characters
//	{{if .car.ok}}...{{else}}?{{end}}   the source's last poll worked
var funcs = template.FuncMap{
	"bar": func(width int, pct any) string {
		p := toInt(pct)
		if p < 0 {
			p = 0
		}
		if p > 100 {
			p = 100
		}
		if width < 2 {
			width = 2
		}
		inner := width - 2
		fill := inner * p / 100
		return "[" + strings.Repeat("#", fill) + strings.Repeat("-", inner-fill) + "]"
	},
	"lpad": func(n int, v any) string { return fmt.Sprintf("%*s", n, fmt.Sprint(v)) },
	"rpad": func(n int, v any) string { return fmt.Sprintf("%-*s", n, fmt.Sprint(v)) },
	"trunc": func(n int, v any) string {
		s := fmt.Sprint(v)
		if n >= 0 && len(s) > n {
			return s[:n]
		}
		return s
	},
}

// toInt - template arguments come as int (our sources), float64 (JSON from
// http) or strings; the bar wants a plain integer
func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		var n int
		fmt.Sscanf(x, "%d", &n)
		return n
	}
	return 0
}

// Slot is one text overlay: where prudynt reads it and how big it is.
type Slot struct {
	Name string
	Path string
	Cols int
	Rows int
	tmpl *template.Template
}

func newSlot(name string, cfg SlotConfig, geo SlotGeometry) (*Slot, error) {
	t, err := template.New(name).Funcs(funcs).Option("missingkey=zero").Parse(cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("slot %s: template: %w", name, err)
	}
	s := &Slot{Name: name, Path: geo.Path, Cols: geo.Cols, Rows: geo.Rows, tmpl: t}
	// The config may pin values when prudynt could not be asked (or on purpose)
	if cfg.Path != "" {
		s.Path = cfg.Path
	}
	if cfg.Cols > 0 {
		s.Cols = cfg.Cols
	}
	if cfg.Rows > 0 {
		s.Rows = cfg.Rows
	}
	return s, nil
}

// render - the text for this slot from a snapshot, cut to rows x cols and
// reduced to what prudynt's 8x8 font can show (printable ASCII). Ends with a
// newline like the shell examples; prudynt does not care either way.
func (s *Slot) render(data map[string]any) (string, error) {
	var b strings.Builder
	if err := s.tmpl.Execute(&b, data); err != nil {
		return "", err
	}
	// A key the source has not produced (yet) prints as text/template's
	// "<no value>"; on an overlay a blank is what one wants
	out := strings.ReplaceAll(b.String(), "<no value>", "")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if s.Rows > 0 && len(lines) > s.Rows {
		lines = lines[:s.Rows]
	}
	for i, line := range lines {
		lines[i] = asciiCut(line, s.Cols)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// asciiCut - tabs become a space, anything outside printable ASCII a '?'
// (multi-byte UTF-8 sequences count as one '?'), then cut to cols
func asciiCut(line string, cols int) string {
	var b strings.Builder
	n := 0
	for _, r := range line {
		if cols > 0 && n >= cols {
			break
		}
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r > 0x7e:
			b.WriteByte('?')
		default:
			b.WriteRune(r)
		}
		n++
	}
	return b.String()
}
