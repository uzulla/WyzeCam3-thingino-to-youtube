package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Values is what one poll of a source produced: the template sees it as
// .<source>.<key>. Every source also gets "ok" (last poll succeeded) and
// "age_s" (seconds since the last successful poll) added by the cache.
type Values map[string]any

// Source is one data source. Poll is called on the source's own schedule from
// its own goroutine; it must respect ctx (deadline) and may fail - the cache
// then keeps the previous values and marks them not ok.
type Source interface {
	Poll(ctx context.Context) (Values, error)
}

// To add a data source: implement Source, register the constructor here, and
// document the keys it produces in docs/osd-feed.md.
var sourceTypes = map[string]func(SourceConfig) (Source, error){
	"meminfo": func(SourceConfig) (Source, error) { return meminfoSource{}, nil },
	"clock":   func(SourceConfig) (Source, error) { return clockSource{}, nil },
	"tick":    newTickSource,
	"http":    newHTTPSource,
}

// ---- cache: the latest values of every source, read by the render loop

type entry struct {
	values Values
	okAt   time.Time // zero until the first successful poll
	ok     bool
}

type Cache struct {
	mu   sync.Mutex
	data map[string]*entry
}

func newCache() *Cache { return &Cache{data: map[string]*entry{}} }

// register - make the source known before its first poll, so a template can
// refer to it from the first render (its values are blank, ok=false until then)
func (c *Cache) register(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data[name] == nil {
		c.data[name] = &entry{values: Values{}}
	}
}

func (c *Cache) put(name string, v Values, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.data[name]
	if e == nil {
		e = &entry{values: Values{}}
		c.data[name] = e
	}
	if err != nil {
		e.ok = false
		return
	}
	e.values, e.ok, e.okAt = v, true, time.Now()
}

// snapshot - a copy for one render pass: map[source]map[key]value, plus the
// bookkeeping keys. Sources that never answered yet are present but empty
// (so templates do not fail on a missing map), with ok=false.
func (c *Cache) snapshot(now time.Time) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]any, len(c.data)+1)
	for name, e := range c.data {
		m := make(map[string]any, len(e.values)+2)
		for k, v := range e.values {
			m[k] = v
		}
		m["ok"] = e.ok
		if e.okAt.IsZero() {
			m["age_s"] = -1
		} else {
			m["age_s"] = int(now.Sub(e.okAt) / time.Second)
		}
		out[name] = m
	}
	return out
}

func pollTimeout(cfg SourceConfig) time.Duration {
	if cfg.TimeoutMs <= 0 {
		return 3 * time.Second
	}
	return time.Duration(cfg.TimeoutMs) * time.Millisecond
}

// runSource polls forever at its interval until ctx is cancelled. A poll that
// takes longer than the interval is not overlapped with the next one.
func runSource(ctx context.Context, name string, cfg SourceConfig, src Source, cache *Cache) {
	interval := time.Duration(cfg.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = time.Second
	}
	lastErr := ""
	poll := func() {
		pctx, cancel := context.WithTimeout(ctx, pollTimeout(cfg))
		v, err := src.Poll(pctx)
		cancel()
		cache.put(name, v, err)
		// Log each distinct failure once, and the recovery
		switch {
		case err != nil && err.Error() != lastErr:
			log.Printf("source %s: %v", name, err)
			lastErr = err.Error()
		case err == nil && lastErr != "":
			log.Printf("source %s: ok again", name)
			lastErr = ""
		}
	}
	poll()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			poll()
		}
	}
}

// ---- meminfo: free_kb, free_mb, total_mb, used_pct (from /proc/meminfo;
// "used" counts buffers/cache as free, like "free" on the camera does)

type meminfoSource struct{}

func (meminfoSource) Poll(context.Context) (Values, error) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	kb := map[string]int{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.Atoi(f[1])
		if err == nil {
			kb[strings.TrimSuffix(f[0], ":")] = n
		}
	}
	total := kb["MemTotal"]
	free := kb["MemFree"] + kb["Buffers"] + kb["Cached"]
	if total <= 0 {
		return nil, fmt.Errorf("no MemTotal in /proc/meminfo")
	}
	return Values{
		"free_kb":  free,
		"free_mb":  free / 1024,
		"total_mb": total / 1024,
		"used_pct": (total - free) * 100 / total,
	}, nil
}

// ---- clock: hms, date, unix (local time as the camera has it)

type clockSource struct{}

func (clockSource) Poll(context.Context) (Values, error) {
	now := time.Now()
	return Values{
		"hms":  now.Format("15:04:05"),
		"date": now.Format("2006-01-02"),
		"unix": now.Unix(),
	}, nil
}

// ---- tick: a counter for demos and progress bars. n counts up by step each
// poll and wraps after max (default 100); pct is n as a percentage of max.

type tickSource struct {
	n, step, max int
}

func newTickSource(c SourceConfig) (Source, error) {
	t := &tickSource{step: c.Step, max: c.Max}
	if t.step <= 0 {
		t.step = 1
	}
	if t.max <= 0 {
		t.max = 100
	}
	t.n = -t.step // first poll shows 0
	return t, nil
}

func (t *tickSource) Poll(context.Context) (Values, error) {
	t.n += t.step
	if t.n > t.max {
		t.n = 0
	}
	return Values{"n": t.n, "pct": t.n * 100 / t.max, "max": t.max}, nil
}

// ---- http: GET url, expect a JSON object; its top-level members become the
// keys (nested objects stay nested: .car.gps.lat). A non-object or non-JSON
// body is exposed as "body" (string). "status" is the HTTP status code.

type httpSource struct {
	url     string
	headers map[string]string
	client  *http.Client
}

func newHTTPSource(c SourceConfig) (Source, error) {
	if c.URL == "" {
		return nil, fmt.Errorf("http source needs \"url\"")
	}
	return &httpSource{url: c.URL, headers: c.Headers, client: &http.Client{}}, nil
}

func (h *httpSource) Poll(ctx context.Context) (Values, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	v := Values{"status": resp.StatusCode}
	var obj map[string]any
	if json.Unmarshal(body, &obj) == nil {
		for k, val := range obj {
			v[k] = val
		}
	} else {
		v["body"] = strings.TrimSpace(string(body))
	}
	return v, nil
}
