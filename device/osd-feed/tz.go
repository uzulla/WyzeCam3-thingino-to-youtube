package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Thingino keeps the time zone busybox-style: a POSIX TZ string in /etc/TZ
// (e.g. "JST-9"), no /etc/localtime and no TZ in the environment. Go's time
// package knows neither, so time.Now() would be UTC. Set time.Local from
// /etc/TZ instead. Only the standard-time part is used (name and offset); a
// DST rule after it ("CET-1CEST,M3.5.0,...") is ignored, which is right for
// Japan and one hour off in summer elsewhere - the clock source says so once.
func setLocalFromEtcTZ() string {
	if os.Getenv("TZ") != "" {
		return "" // Go handles TZ itself (a zoneinfo name)
	}
	b, err := os.ReadFile("/etc/TZ")
	if err != nil {
		return ""
	}
	spec := strings.TrimSpace(string(b))
	loc, dst, err := parsePosixTZ(spec)
	if err != nil {
		return fmt.Sprintf("/etc/TZ %q not understood (%v), using UTC", spec, err)
	}
	time.Local = loc
	if dst {
		return fmt.Sprintf("/etc/TZ %q has a DST rule, which is ignored: the clock may be an hour off in summer", spec)
	}
	return ""
}

// parsePosixTZ - "JST-9", "UTC0", "EST5EDT,M3.2.0,M11.1.0", "<+09>-9":
// the name and the standard offset. POSIX offsets are west-positive
// (JST-9 = UTC+9). Returns whether a DST part followed.
func parsePosixTZ(spec string) (*time.Location, bool, error) {
	if spec == "" {
		return nil, false, fmt.Errorf("empty")
	}
	i := 0
	var name string
	if spec[0] == '<' {
		end := strings.IndexByte(spec, '>')
		if end < 0 {
			return nil, false, fmt.Errorf("unterminated <name>")
		}
		name, i = spec[1:end], end+1
	} else {
		for i < len(spec) && isAlpha(spec[i]) {
			i++
		}
		name = spec[:i]
	}
	if name == "" {
		return nil, false, fmt.Errorf("no zone name")
	}
	// offset: [+-]hh[:mm[:ss]]
	sign := 1
	if i < len(spec) && (spec[i] == '+' || spec[i] == '-') {
		if spec[i] == '-' {
			sign = -1
		}
		i++
	}
	start := i
	for i < len(spec) && (spec[i] >= '0' && spec[i] <= '9' || spec[i] == ':') {
		i++
	}
	if start == i {
		return nil, false, fmt.Errorf("no offset")
	}
	parts := strings.Split(spec[start:i], ":")
	if len(parts) > 3 {
		return nil, false, fmt.Errorf("offset %q: too many parts", spec[start:i])
	}
	var hms [3]int
	for k, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false, fmt.Errorf("offset %q: bad %s", spec[start:i], [...]string{"hours", "minutes", "seconds"}[k])
		}
		hms[k] = n
	}
	h, m, s := hms[0], hms[1], hms[2]
	if h > 24 || m > 59 || s > 59 {
		return nil, false, fmt.Errorf("offset out of range")
	}
	// POSIX: positive means west of UTC; Go wants seconds east of UTC
	east := -sign * (h*3600 + m*60 + s)
	return time.FixedZone(name, east), i < len(spec), nil
}

func isAlpha(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }
