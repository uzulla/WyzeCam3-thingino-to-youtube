package main

// The fragmented MP4 relay against a fake prudynt: bytes pass through, bad
// upstream answers become 502, the viewer slots are limited and given back.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePrudynt - answers /ch0.mp4 with what the test says; holds the body
// open until release is closed when hold is true
type fakePrudynt struct {
	status      string
	contentType string
	body        string
	hold        chan struct{}
	mu          sync.Mutex
	opened      int
}

func (f *fakePrudynt) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.opened++
	f.mu.Unlock()
	if f.status != "" {
		http.Error(w, f.status, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", f.contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, f.body)
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-r.Context().Done():
		}
	}
}

func relayServer(t *testing.T, fp *fakePrudynt) (*httptest.Server, *http.Cookie) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(fp.handler))
	t.Cleanup(up.Close)
	srv, _ := testServer(t)
	// point the relay at the fake and get a session the simple way
	s := srvOf(t, srv)
	s.prudynt = up.URL
	tok := s.sessions.create()
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

// srvOf - the *server behind a test httptest.Server (kept in a registry by testServer)
var testServers sync.Map

func srvOf(t *testing.T, srv *httptest.Server) *server {
	t.Helper()
	v, ok := testServers.Load(srv)
	if !ok {
		t.Fatal("unknown test server")
	}
	return v.(*server)
}

func TestMP4RelayPassesBytes(t *testing.T) {
	fp := &fakePrudynt{contentType: "video/mp4", body: "\x00\x00\x00\x1cftypiso5" + strings.Repeat("x", 100)}
	srv, cookie := relayServer(t, fp)
	resp := jsonReq(t, "GET", srv.URL+"/api/preview.mp4", "", cookie, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("relay: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != fp.body {
		t.Fatalf("relayed %d bytes, want %d; %q", len(b), len(fp.body), b[:min(len(b), 20)])
	}
}

func TestMP4RelayRefusesBadUpstream(t *testing.T) {
	for _, fp := range []*fakePrudynt{
		{status: "gone"}, // prudynt says 404
		{contentType: "text/html", body: "<html>portal</html>"}, // something else answers on the port
	} {
		srv, cookie := relayServer(t, fp)
		if c := code(jsonReq(t, "GET", srv.URL+"/api/preview.mp4", "", cookie, nil)); c != http.StatusBadGateway {
			t.Errorf("upstream %+v: %d, want 502", fp, c)
		}
	}
}

func TestPreviewSlotsAreLimitedAndReleased(t *testing.T) {
	fp := &fakePrudynt{contentType: "video/mp4", body: "\x00\x00\x00\x1cftypiso5", hold: make(chan struct{})}
	srv, cookie := relayServer(t, fp)
	var open []*http.Response
	for i := 0; i < previewMaxSlots; i++ {
		resp := jsonReq(t, "GET", srv.URL+"/api/preview.mp4", "", cookie, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("viewer %d: %d", i+1, resp.StatusCode)
		}
		open = append(open, resp)
	}
	// one more than the slots: refused at once, no connection to prudynt
	before := fp.opened
	if c := code(jsonReq(t, "GET", srv.URL+"/api/preview.mp4", "", cookie, nil)); c != http.StatusServiceUnavailable {
		t.Fatalf("extra viewer: %d, want 503", c)
	}
	if fp.opened != before {
		t.Fatal("the refused viewer still reached prudynt")
	}
	// a viewer leaves: its slot is free again
	open[0].Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp := jsonReq(t, "GET", srv.URL+"/api/preview.mp4", "", cookie, nil)
		if resp.StatusCode == http.StatusOK {
			open = append(open, resp)
			break
		}
		resp.Body.Close()
		if time.Now().After(deadline) {
			t.Fatal("slot not released after the viewer left")
		}
		time.Sleep(50 * time.Millisecond)
	}
	close(fp.hold)
	for _, r := range open {
		r.Body.Close()
	}
}
