package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func httpPoll(t *testing.T, handler http.HandlerFunc, timeout time.Duration) (Values, error) {
	srv := httptest.NewServer(handler)
	defer srv.Close()
	src, err := newHTTPSource(SourceConfig{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return src.Poll(ctx)
}

func TestHTTPSourceJSON(t *testing.T) {
	v, err := httpPoll(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"speed": 42.5, "status": "offline", "gps": {"lat": 35.6, "fix": {"ok": true}}, "list": [1,2]}`))
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if v["speed"] != 42.5 || v["status"] != 200 || v["gps_lat"] != 35.6 || v["gps_fix_ok"] != true {
		t.Errorf("got %v", v)
	}
	if _, nested := v["gps"]; nested {
		t.Error("nested object must be flattened, not kept")
	}
	if l, ok := v["list"].([]any); !ok || len(l) != 2 {
		t.Errorf("arrays must be kept: %v", v["list"])
	}
}

func TestHTTPSourceNonJSONAndNull(t *testing.T) {
	v, err := httpPoll(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("  hello\n")) }, time.Second)
	if err != nil || v["body"] != "hello" || v["status"] != 200 {
		t.Errorf("got %v %v", v, err)
	}
	v, err = httpPoll(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("null")) }, time.Second)
	if err != nil || v["body"] != "null" {
		t.Errorf("JSON null must land in body, got %v %v", v, err)
	}
}

func TestHTTPSourceErrors(t *testing.T) {
	if _, err := httpPoll(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }, time.Second); err == nil {
		t.Error("non-2xx must fail")
	}
	_, err := httpPoll(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(500 * time.Millisecond) }, 50*time.Millisecond)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) && !isTimeout(err) {
		t.Errorf("slow server must hit the deadline, got %v", err)
	}
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// a source that fails on demand (the flag is flipped from the test goroutine)
type flaky struct{ fail atomic.Bool }

func (f *flaky) Poll(context.Context) (Values, error) {
	if f.fail.Load() {
		return nil, errors.New("down")
	}
	return Values{"v": 1}, nil
}

func TestRunSourceKeepsLastValuesOnFailure(t *testing.T) {
	c := newCache()
	src := &flaky{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runSource(ctx, "f", SourceConfig{IntervalMs: 20}, src, c); close(done) }()
	time.Sleep(30 * time.Millisecond)
	snap := c.snapshot(time.Now())["f"].(map[string]any)
	if snap["v"] != 1 || snap["ok"] != true {
		t.Fatalf("first poll: %v", snap)
	}
	src.fail.Store(true)
	time.Sleep(60 * time.Millisecond)
	snap = c.snapshot(time.Now())["f"].(map[string]any)
	if snap["v"] != 1 || snap["ok"] != false {
		t.Errorf("after failure the value must stay with ok=false: %v", snap)
	}
	src.fail.Store(false)
	time.Sleep(60 * time.Millisecond)
	if snap = c.snapshot(time.Now())["f"].(map[string]any); snap["ok"] != true {
		t.Errorf("must recover: %v", snap)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("runSource must return when ctx is cancelled")
	}
}
