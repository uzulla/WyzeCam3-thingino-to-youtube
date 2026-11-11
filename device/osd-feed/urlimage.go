package main

// A picture fetched over HTTP (a JPEG or PNG from another camera, a
// rendered graph, ...) shown in an image slot the way a PNG file is
// (docs/osd-feed.md: slots.imagefile.url). The fetching and the decoding run
// in their own goroutine on the slot's interval, so a slow or absent server
// never holds up the redraw loop; render only picks up the latest pixels.
// The URL is passed as given: size, quality, rotation and so on are the
// server's own parameters, put in the URL by the user.

import (
	"bytes"
	"context"
	"fmt"
	_ "image/jpeg" // what the typical camera serves; image.Decode picks it by the header
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	urlDefaultInterval = 1000 * time.Millisecond
	urlMinInterval     = 100 * time.Millisecond // prudynt looks at the files every 100 ms
	urlMaxInterval     = time.Hour
	urlDefaultTimeout  = 3000 * time.Millisecond
	urlDefaultStale    = 10000 * time.Millisecond
	urlMaxBody         = 4 << 20 // a picture for a slot of at most 1280x720; a JPEG that size is well under this
)

// urlPicture - the fetcher and its latest result
type urlPicture struct {
	url      string
	interval time.Duration
	timeout  time.Duration
	stale    time.Duration // after this long without a successful fetch the picture is marked (0: never)
	hide     bool          // on_stale "hide": take the picture down instead of writing OFFLINE on it
	fit      string
	client   *http.Client

	mu      sync.Mutex
	w, h    int    // the box the pixels are converted for (set by the slot; a change asks for a new conversion)
	raw     []byte // the body of the last successful fetch (to skip decoding an unchanged picture)
	good    []byte // the last picture that converted, nil when there was none
	pixels  []byte // what the slot shows now: good, or its OFFLINE version, or nil
	offline bool   // pixels is the OFFLINE version (or the OFFLINE box)
	seq     uint64 // bumped whenever pixels change (content or presence)
	err     error  // the last fetch / decode problem, nil after a success
	okAt    time.Time
	// closed when the first fetch attempt has been made (for -once and tests)
	first     chan struct{}
	firstOnce sync.Once
}

// newURLPicture - the fetcher for a slot's url, converting for a w x h box
func newURLPicture(cfg SlotConfig, w, h int) *urlPicture {
	// An uncompressed 320x240 picture is 300 KB: read it in few, large
	// syscalls (the default 4 KB buffer means 80 reads per picture, and
	// the kernel side of each shows as system time on this CPU)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ReadBufferSize = 64 << 10
	tr.DialContext = dialLocal // a .local host is resolved by mDNS (mdns.go)
	p := &urlPicture{url: cfg.URL, fit: cfg.Fit, w: w, h: h,
		interval: urlDefaultInterval, timeout: urlDefaultTimeout, stale: urlDefaultStale,
		client: &http.Client{Transport: tr}, first: make(chan struct{})}
	if p.fit == "" {
		p.fit = "contain"
	}
	if cfg.IntervalMs > 0 {
		p.interval = max(urlMinInterval, min(urlMaxInterval, time.Duration(cfg.IntervalMs)*time.Millisecond))
	}
	if cfg.TimeoutMs > 0 {
		p.timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	}
	if cfg.StaleMs != nil {
		p.stale = time.Duration(*cfg.StaleMs) * time.Millisecond
	}
	p.hide = cfg.OnStale == "hide"
	return p
}

// resize - the box changed (the slot followed prudynt): convert the last
// picture again for the new size at the next tick, show nothing until then
func (p *urlPicture) resize(w, h int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.w == w && p.h == h {
		return
	}
	p.w, p.h = w, h
	p.good = nil
	if p.pixels != nil {
		p.pixels, p.offline = nil, false
		p.seq++
	}
}

// start - fetch on the interval until ctx is done. A fetch that takes longer
// than the interval is not overlapped with the next one.
func (p *urlPicture) start(ctx context.Context) {
	go func() {
		p.fetch(ctx)
		t := time.NewTicker(p.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.fetch(ctx)
			}
		}
	}()
}

// fetchOnce - one fetch, synchronously (-once)
func (p *urlPicture) fetchOnce(ctx context.Context) { p.fetch(ctx) }

func (p *urlPicture) fetch(ctx context.Context) {
	defer p.firstOnce.Do(func() { close(p.first) })
	body, err := p.get(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if err == nil {
		same := p.raw != nil && bytes.Equal(body, p.raw)
		p.raw = body
		if same && p.good != nil {
			// the same picture again: nothing to decode (and a transient
			// fetch error in between is over)
			p.err, p.okAt = nil, now
			p.show(p.good, false)
			return
		}
		if p.w < 2 || p.h < 2 {
			err = fmt.Errorf("slot size %dx%d", p.w, p.h)
		} else {
			var pix []byte
			if pix, err = convertPNGBytes(body, p.url, p.w, p.h, p.fit); err == nil {
				p.err, p.okAt = nil, now
				p.good = pix
				p.show(pix, false)
				return
			}
		}
	}
	// A fetch or decode problem: keep showing the last good picture for a
	// while (a missed or garbled frame is nothing); after stale_ms it is
	// marked OFFLINE (or taken down) rather than left frozen. With no
	// picture at all yet, say so at once.
	p.err = err
	if p.good == nil && !p.hide {
		p.show(offlineFrame(nil, p.w, p.h), true)
		return
	}
	p.dropIfStale(now)
}

// show - make pix what the slot shows (offline: it is the OFFLINE version),
// counting a change only when the bytes or the kind differ. Lock held.
func (p *urlPicture) show(pix []byte, offline bool) {
	if offline == p.offline && bytes.Equal(pix, p.pixels) {
		return
	}
	p.pixels, p.offline, p.seq = pix, offline, p.seq+1
}

// dropIfStale - when the last good fetch is older than stale_ms (0: never)
// and the slot still shows it as is: write OFFLINE over it, or take it
// down (on_stale "hide"). Called with the lock held.
func (p *urlPicture) dropIfStale(now time.Time) {
	if p.good == nil || p.offline || p.stale == 0 || now.Sub(p.okAt) <= p.stale {
		return
	}
	if p.hide {
		p.pixels, p.offline, p.seq = nil, false, p.seq+1
		return
	}
	p.show(offlineFrame(p.good, p.w, p.h), true)
}

// get - GET the URL within the timeout; the body on a 2xx, the error otherwise
func (p *urlPicture) get(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		// the device may have moved: next time, ask the network again
		if h := req.URL.Hostname(); isLocalName(h) {
			resolver.forget(h)
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s: HTTP %d", p.url, resp.StatusCode)
	}
	if resp.ContentLength > urlMaxBody {
		return nil, fmt.Errorf("%s: the picture is over %d MB", p.url, urlMaxBody>>20)
	}
	var body []byte
	if n := resp.ContentLength; n > 0 {
		// the size is known: one buffer of that size, filled in place (ReadAll
		// would grow and copy its way up to it, several copies of a big picture)
		body = make([]byte, n)
		if _, err := io.ReadFull(resp.Body, body); err != nil {
			return nil, fmt.Errorf("%s: %w", p.url, err)
		}
	} else {
		body, err = io.ReadAll(io.LimitReader(resp.Body, urlMaxBody+1))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.url, err)
		}
		if len(body) > urlMaxBody {
			return nil, fmt.Errorf("%s: the picture is over %d MB", p.url, urlMaxBody>>20)
		}
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("%s: empty response", p.url)
	}
	return body, nil
}

// latest - the pixels to show (nil: none), whether they differ from the
// last call's (by seq), and the current problem if any
func (p *urlPicture) latest(lastSeq *uint64) (pix []byte, changed bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// a GET that hangs (timeout_ms can be longer than stale_ms) must not
	// keep an old picture up past its time either
	p.dropIfStale(time.Now())
	changed = p.seq != *lastSeq
	*lastSeq = p.seq
	return p.pixels, changed, p.err
}

// describe - for the -once report and the start-up log
func (p *urlPicture) describe() string {
	return fmt.Sprintf("%s every %v (fit %s)", p.url, p.interval, p.fit)
}

// logURLStart - one line at start-up saying what the slot fetches
func logURLStart(name string, p *urlPicture) {
	log.Printf("slot %s: picture from %s", name, p.describe())
}
