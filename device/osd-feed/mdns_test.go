package main

import (
	"context"
	"encoding/binary"
	"errors"
	"image/color"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

// aAnswer - a response with one A record for name (with an unrelated TXT
// first, name-compressed like responders do)
func aAnswer(name string, ip net.IP) []byte {
	// response, 1 question, 2 answers; then the question (its name at offset 12)
	q, _ := buildQuery(name, typeA)
	msg := append([]byte{0, 0, 0x84, 0, 0, 1, 0, 2, 0, 0, 0, 0}, q[12:]...)
	rr := func(nameRef []byte, typ uint16, data []byte) []byte {
		b := append([]byte(nil), nameRef...)
		b = binary.BigEndian.AppendUint16(b, typ)
		b = binary.BigEndian.AppendUint16(b, classIN|0x8000)
		b = binary.BigEndian.AppendUint32(b, 120)
		b = binary.BigEndian.AppendUint16(b, uint16(len(data)))
		return append(b, data...)
	}
	ptr := []byte{0xC0, 12}
	msg = append(msg, rr(ptr, 16, []byte("\x04id=1"))...)
	msg = append(msg, rr(ptr, typeA, ip.To4())...)
	return msg
}

func TestARecordFor(t *testing.T) {
	msg := aAnswer("M5cam-06f808.local", net.IPv4(192, 168, 11, 113))
	if ip := aRecordFor(msg, "m5cam-06f808.local"); !ip.Equal(net.IPv4(192, 168, 11, 113)) {
		t.Errorf("got %v", ip)
	}
	if ip := aRecordFor(msg, "other.local"); ip != nil {
		t.Errorf("another host's answer must not count: %v", ip)
	}
	q, _ := buildQuery("m5cam-06f808.local", typeA)
	if ip := aRecordFor(q, "m5cam-06f808.local"); ip != nil {
		t.Errorf("a query is not an answer: %v", ip)
	}
	// a lying responder: loopback, any, multicast are not addresses to connect to
	for _, bad := range []net.IP{net.IPv4(127, 0, 0, 1), net.IPv4(0, 0, 0, 0), net.IPv4(224, 0, 0, 251), net.IPv4(255, 255, 255, 255)} {
		if ip := aRecordFor(aAnswer("m5cam-06f808.local", bad), "m5cam-06f808.local"); ip != nil {
			t.Errorf("%v must not be believed", bad)
		}
	}
	// a forward pointer (a loop in disguise) and an over-long name are refused
	loop := []byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0xC0, 12}
	if ip := aRecordFor(loop, "x.local"); ip != nil {
		t.Error("pointer loop")
	}
	if _, _, err := readName(loop, 12); err == nil {
		t.Error("a pointer to itself must be an error")
	}
	long := []byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	for i := 0; i < 5; i++ {
		long = append(long, 63)
		long = append(long, strings.Repeat("a", 63)...)
	}
	long = append(long, 0)
	if _, _, err := readName(long, 12); err == nil {
		t.Error("a 320-byte name must be an error")
	}
	if _, err := buildQuery("", typeA); err == nil {
		t.Error("an empty name cannot be queried")
	}
	if _, err := buildQuery(strings.Repeat("a", 64)+".local", typeA); err == nil {
		t.Error("a 64-byte label cannot be queried")
	}
	if !isLocalName("cam.local.") {
		t.Error("a trailing dot is still a .local name")
	}
	for n := 12; n < len(msg); n++ {
		if ip := aRecordFor(msg[:n], "m5cam-06f808.local"); ip != nil {
			t.Fatalf("truncated to %d bytes gave %v", n, ip)
		}
	}
	if !isLocalName("m5cam-06f808.local") || !isLocalName("M5CAM.LOCAL") || isLocalName("192.168.11.113") || isLocalName("example.com") {
		t.Error("isLocalName")
	}
}

// A .local host in the URL is resolved here (by the swapped-in lookup),
// cached, and forgotten after a failed fetch
func TestURLPictureResolvesLocalNames(t *testing.T) {
	ps := newPictureServer(t, testJPEG(t, 4, 4, color.RGBA{R: 255, A: 255}))
	u, _ := url.Parse(ps.URL)
	_, port, _ := net.SplitHostPort(u.Host)
	calls := 0
	old := resolver.lookupFn
	resolver.lookupFn = func(ctx context.Context, host string) (net.IP, error) {
		calls++
		if host != "cam.local" {
			return nil, errors.New("unknown " + host)
		}
		return net.IPv4(127, 0, 0, 1), nil
	}
	t.Cleanup(func() { resolver.lookupFn = old; resolver.forget("cam.local") })
	resolver.forget("cam.local")

	p := newURLPicture(SlotConfig{URL: "http://Cam.local:" + port + "/image?fmt=jpg"}, 4, 4)
	p.fetchOnce(context.Background())
	var seq uint64
	if pix, _, err := p.latest(&seq); err != nil || len(pix) != 4*4*4 {
		t.Fatalf("via .local: err %v len %d", err, len(pix))
	}
	p.fetchOnce(context.Background())
	if calls != 1 {
		t.Errorf("the address must be cached: %d lookups", calls)
	}
	// the server goes away: the fetch fails, the name is forgotten, the next fetch asks again
	ps.Close()
	p.fetchOnce(context.Background())
	if _, _, err := p.latest(&seq); err == nil {
		t.Fatal("a closed server must be an error")
	}
	p.fetchOnce(context.Background())
	if calls != 2 {
		t.Errorf("after a failure the name must be looked up again: %d lookups", calls)
	}
	// a 404 (another device at the old address, say) also forgets the name
	ps2 := newPictureServer(t, testJPEG(t, 4, 4, color.RGBA{R: 255, A: 255}))
	u2, _ := url.Parse(ps2.URL)
	_, port2, _ := net.SplitHostPort(u2.Host)
	p = newURLPicture(SlotConfig{URL: "http://cam.local:" + port2 + "/image"}, 4, 4)
	p.fetchOnce(context.Background())
	before404 := calls
	ps2.status.Store(404)
	p.fetchOnce(context.Background())
	p.fetchOnce(context.Background()) // the failed fetch dropped the kept connection too: this one dials, so it looks up
	if calls != before404+1 {
		t.Errorf("after a 404 the name must be looked up again: %d lookups (was %d)", calls, before404)
	}
	// an unknown name is an error that says so
	p = newURLPicture(SlotConfig{URL: "http://nobody.local/image"}, 4, 4)
	p.fetchOnce(context.Background())
	if _, _, err := p.latest(&seq); err == nil || !strings.Contains(err.Error(), "nobody.local") {
		t.Errorf("unknown .local: %v", err)
	}
	// the cache is per host and expires
	resolver.mu.Lock()
	resolver.cache["cam.local"] = localEntry{ip: net.IPv4(127, 0, 0, 1), at: time.Now().Add(-mdnsCache - time.Second)}
	resolver.mu.Unlock()
	before := calls
	resolver.lookup(context.Background(), "cam.local")
	if calls != before+1 {
		t.Error("an expired entry must be looked up again")
	}
}
