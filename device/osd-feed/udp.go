package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// ---- udp: JSON datagrams pushed to us (e.g. esp32-racebox's broadcast of a
// RaceBox at 25 Hz). Unlike the other sources nothing is fetched: a listener
// keeps the latest datagram and Poll parses it - only the newest one, and
// only when it is new: decoding JSON is most of what this source costs on the
// camera (softfloat), and 25 a second is more than the 10 redraws need.
// Keys are the JSON members, flattened like the http source's
// ({"g":{"x":..}} -> g_x). A poll fails (ok=false, the old values kept) when
// nothing matching has arrived for stale_ms: the sender is off, out of range,
// or not sending yet.

type udpSource struct {
	listen string
	match  map[string]string
	stale  time.Duration

	conn net.PacketConn // set by Start

	mu     sync.Mutex
	raw    []byte    // the latest datagram, not parsed yet (nil: none since the last poll)
	rawAt  time.Time // when it arrived
	rawSrc net.Addr

	// Poll's own (one poll at a time)
	latest  Values
	at      time.Time // when latest arrived (zero: nothing yet)
	lastErr string
}

func newUDPSource(c SourceConfig) (Source, error) {
	if c.Listen == "" {
		return nil, fmt.Errorf("udp source needs \"listen\" (e.g. \":5005\")")
	}
	stale := time.Duration(c.StaleMs) * time.Millisecond
	if stale <= 0 {
		stale = time.Second
	}
	return &udpSource{listen: c.Listen, match: c.Match, stale: stale}, nil
}

// Start binds the port now, so that a port in use stops osd-feed at start-up
// with a clear message rather than leaving the source silently stale; the
// datagrams are read in the background until ctx ends.
func (u *udpSource) Start(ctx context.Context) error {
	conn, err := net.ListenPacket("udp4", u.listen)
	if err != nil {
		return err
	}
	u.conn = conn
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	go u.read(conn)
	return nil
}

func (u *udpSource) read(conn net.PacketConn) {
	// esp32-racebox keeps its datagrams under 1000 bytes; anything up to the
	// largest UDP payload is read whole (a cut-off JSON would not parse)
	buf := make([]byte, 65536)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		u.mu.Lock()
		u.raw = append(u.raw[:0], buf[:n]...)
		u.rawAt, u.rawSrc = time.Now(), from
		u.mu.Unlock()
	}
}

// parse - the flattened object, or nil when it does not match. flatJSON,
// not encoding/json: that took about 3 ms per RaceBox datagram on the camera.
func (u *udpSource) parse(b []byte) (Values, error) {
	v := Values{}
	if err := flatJSON(b, v); err != nil {
		return nil, err
	}
	for k, want := range u.match {
		if s, ok := v[k].(string); !ok || s != want {
			return nil, nil
		}
	}
	return v, nil
}

func (u *udpSource) Poll(context.Context) (Values, error) {
	u.mu.Lock()
	raw, at, from := u.raw, u.rawAt, u.rawSrc
	u.raw = nil // the reader appends to a fresh buffer from now on
	u.mu.Unlock()
	if raw != nil {
		// A datagram that does not match (esp32-racebox's once-a-second
		// "status") leaves the last match in place; it only hides a match
		// that came just before it, and the next one is 40 ms away
		v, err := u.parse(raw)
		switch {
		case err != nil:
			// Once per distinct problem: a sender spamming garbage at 25 Hz
			// must not fill the log
			if msg := fmt.Sprintf("%v (from %v)", err, from); msg != u.lastErr {
				log.Printf("source udp %s: ignoring a datagram: %s", u.listen, msg)
				u.lastErr = msg
			}
		case v != nil:
			u.latest, u.at = v, at
		}
	}
	if u.at.IsZero() {
		return nil, fmt.Errorf("nothing received on %s yet", u.listen)
	}
	if age := time.Since(u.at); age > u.stale {
		return nil, fmt.Errorf("nothing received on %s for %v", u.listen, age.Truncate(100*time.Millisecond))
	}
	out := make(Values, len(u.latest))
	for k, v := range u.latest {
		out[k] = v
	}
	return out, nil
}
