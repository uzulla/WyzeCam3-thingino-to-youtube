package main

import (
	"encoding/json"
	"fmt"
)

// flatJSON - one JSON object straight into flattened Values (nested objects
// joined with "_", as flatten does): strings as string, numbers as
// json.Number (their text), true/false as bool, null as nil. Arrays are
// skipped. It is a single pass over the bytes without reflection:
// encoding/json took about 3 ms for one 600-byte RaceBox datagram on the
// camera, several % of the CPU at 25 datagrams a second; this is for the udp
// source, which only needs a few of the values.
func flatJSON(b []byte, out Values) error {
	p := &jsonScan{b: b}
	p.space()
	if !p.eat('{') {
		return fmt.Errorf("not a JSON object")
	}
	if err := p.object("", out); err != nil {
		return err
	}
	p.space()
	if p.i != len(p.b) {
		return fmt.Errorf("unexpected content after the JSON object at byte %d", p.i)
	}
	return nil
}

type jsonScan struct {
	b []byte
	i int
}

func (p *jsonScan) space() {
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonScan) eat(c byte) bool {
	p.space()
	if p.i < len(p.b) && p.b[p.i] == c {
		p.i++
		return true
	}
	return false
}

func (p *jsonScan) fail(what string) error {
	return fmt.Errorf("bad JSON at byte %d: %s", p.i, what)
}

// object - the members after '{', up to and including '}'
func (p *jsonScan) object(prefix string, out Values) error {
	if p.eat('}') {
		return nil
	}
	for {
		p.space()
		key, err := p.str()
		if err != nil {
			return err
		}
		if !p.eat(':') {
			return p.fail("':' expected")
		}
		if err := p.value(prefix+key, out); err != nil {
			return err
		}
		if p.eat(',') {
			continue
		}
		if p.eat('}') {
			return nil
		}
		return p.fail("',' or '}' expected")
	}
}

func (p *jsonScan) value(key string, out Values) error {
	p.space()
	if p.i >= len(p.b) {
		return p.fail("value expected")
	}
	put := func(v any) error {
		if _, dup := out[key]; dup {
			return fmt.Errorf("JSON members collide after flattening: %q", key)
		}
		out[key] = v
		return nil
	}
	switch c := p.b[p.i]; {
	case c == '{':
		p.i++
		return p.object(key+"_", out)
	case c == '[':
		return p.skipArray()
	case c == '"':
		s, err := p.str()
		if err != nil {
			return err
		}
		return put(s)
	case c == '-' || (c >= '0' && c <= '9'):
		start := p.i
		for p.i < len(p.b) {
			c := p.b[p.i]
			if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
				p.i++
				continue
			}
			break
		}
		n := string(p.b[start:p.i])
		if !validNumber(n) {
			return p.fail("bad number " + n)
		}
		return put(json.Number(n))
	case p.word("true"):
		return put(true)
	case p.word("false"):
		return put(false)
	case p.word("null"):
		return put(nil)
	}
	return p.fail("value expected")
}

// validNumber - JSON's number syntax, -?int(.digits)?([eE][+-]?digits)?,
// checked without converting (converting is what this file avoids)
func validNumber(n string) bool {
	i := 0
	digits := func() int {
		s := i
		for i < len(n) && n[i] >= '0' && n[i] <= '9' {
			i++
		}
		return i - s
	}
	if i < len(n) && n[i] == '-' {
		i++
	}
	if i < len(n) && n[i] == '0' {
		i++
	} else if digits() == 0 {
		return false
	}
	if i < len(n) && n[i] == '.' {
		i++
		if digits() == 0 {
			return false
		}
	}
	if i < len(n) && (n[i] == 'e' || n[i] == 'E') {
		i++
		if i < len(n) && (n[i] == '+' || n[i] == '-') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return i == len(n)
}

// word - the literal at the current position (consumed when it matches)
func (p *jsonScan) word(w string) bool {
	if len(p.b)-p.i >= len(w) && string(p.b[p.i:p.i+len(w)]) == w {
		p.i += len(w)
		return true
	}
	return false
}

// str - a string at the current position. Without escapes (the usual case)
// it is the bytes as they are; with escapes encoding/json decodes it (rare).
func (p *jsonScan) str() (string, error) {
	if p.i >= len(p.b) || p.b[p.i] != '"' {
		return "", p.fail("string expected")
	}
	start := p.i
	p.i++
	escaped := false
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case '\\':
			escaped = true
			p.i += 2
			continue
		case '"':
			p.i++
			if !escaped {
				return string(p.b[start+1 : p.i-1]), nil
			}
			var s string
			if err := json.Unmarshal(p.b[start:p.i], &s); err != nil {
				return "", p.fail("bad string")
			}
			return s, nil
		}
		p.i++
	}
	return "", p.fail("unterminated string")
}

// skipArray - past the array at the current position, nested ones included.
// Its syntax is checked with json.Valid (no values are built, so it is
// cheap), so that a broken array fails the datagram as encoding/json would.
func (p *jsonScan) skipArray() error {
	start := p.i
	depth := 0
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case '"':
			if _, err := p.str(); err != nil {
				return err
			}
			continue
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				p.i++
				if !json.Valid(p.b[start:p.i]) {
					return p.fail("bad array")
				}
				return nil
			}
		}
		p.i++
	}
	return p.fail("unterminated array")
}
