package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestHTTPSourceFlattenCollision(t *testing.T) {
	if _, err := httpPoll(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"gps": {"lat": 1}, "gps_lat": 2}`))
	}, time.Second); err == nil {
		t.Error("colliding keys must be an error")
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

func TestCPUSource(t *testing.T) {
	dir := t.TempDir()
	stat, load := dir+"/stat", dir+"/loadavg"
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(load, "1.65 1.70 1.41 1/117 15110\n")
	// user nice system idle iowait irq softirq steal guest guest_nice
	write(stat, "cpu  100 0 100 700 100 0 0 0 50 0\ncpu0 100 0 100 700 100 0 0 0 50 0\nintr 1\n")
	src := &cpuSource{stat: stat, loadavg: load}
	v, err := src.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// since boot: busy 200 of 1000 (guest is not counted twice)
	if v["used_pct"] != 20 || v["load1"] != "1.65" || v["load15"] != "1.41" {
		t.Errorf("first poll: %v", v)
	}
	// +60 busy, +40 idle since the previous poll
	write(stat, "cpu  150 0 110 730 110 0 0 0 50 0\n")
	if v, _ = src.Poll(context.Background()); v["used_pct"] != 60 {
		t.Errorf("second poll: %v", v)
	}
	// unchanged counters (no time passed) must not divide by zero
	if v, _ = src.Poll(context.Background()); v["used_pct"] != 0 {
		t.Errorf("no change: %v", v)
	}
	write(stat, "intr 1\n")
	if _, err := src.Poll(context.Background()); err == nil {
		t.Error("missing cpu line must be an error")
	}
}
