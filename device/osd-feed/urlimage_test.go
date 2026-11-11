package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// a JPEG, w x h, one flat colour
func testJPEG(t *testing.T, w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pictureServer serves whatever body/status are set at the moment
type pictureServer struct {
	*httptest.Server
	body   atomic.Pointer[[]byte]
	status atomic.Int32
	hits   atomic.Int32
}

func newPictureServer(t *testing.T, body []byte) *pictureServer {
	ps := &pictureServer{}
	ps.body.Store(&body)
	ps.status.Store(200)
	ps.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps.hits.Add(1)
		w.WriteHeader(int(ps.status.Load()))
		w.Write(*ps.body.Load())
	}))
	t.Cleanup(ps.Close)
	return ps
}

func TestURLPictureFetchesAndSkipsUnchanged(t *testing.T) {
	red := testJPEG(t, 8, 4, color.RGBA{R: 255, A: 255})
	ps := newPictureServer(t, red)
	p := newURLPicture(SlotConfig{URL: ps.URL + "/jpg?w=8", IntervalMs: 500}, 8, 4)
	if p.interval != 500*time.Millisecond || p.timeout != urlDefaultTimeout || p.stale != urlDefaultStale {
		t.Fatalf("defaults: %+v", p)
	}
	var seq uint64
	if pix, changed, err := p.latest(&seq); pix != nil || changed || err != nil {
		t.Fatalf("before any fetch: %v %v %v", pix, changed, err)
	}
	p.fetchOnce(context.Background())
	pix, changed, err := p.latest(&seq)
	if err != nil || !changed || len(pix) != 8*4*4 {
		t.Fatalf("first fetch: err %v changed %v len %d", err, changed, len(pix))
	}
	if got := px(pix, 8, 0, 0); got[2] < 240 || got[0] > 15 || got[3] != 255 { // BGRA: red, opaque
		t.Errorf("red JPEG -> %v", got)
	}
	// the same bytes again: nothing changes, and the picture is not decoded again (raw kept)
	p.fetchOnce(context.Background())
	if _, changed, err := p.latest(&seq); changed || err != nil {
		t.Errorf("same picture must not count as a change: %v %v", changed, err)
	}
	// a different picture: changed
	blue := testJPEG(t, 8, 4, color.RGBA{B: 255, A: 255})
	ps.body.Store(&blue)
	p.fetchOnce(context.Background())
	pix, changed, _ = p.latest(&seq)
	if !changed || px(pix, 8, 0, 0)[0] < 240 {
		t.Errorf("new picture must be seen: changed %v px %v", changed, px(pix, 8, 0, 0))
	}
	if ps.hits.Load() != 3 {
		t.Errorf("3 fetches expected, got %d", ps.hits.Load())
	}
}

func TestURLPictureErrorsKeepThenDropThePicture(t *testing.T) {
	ps := newPictureServer(t, testJPEG(t, 4, 4, color.RGBA{G: 255, A: 255}))
	stale := 30
	p := newURLPicture(SlotConfig{URL: ps.URL, StaleMs: &stale}, 4, 4)
	p.fetchOnce(context.Background())
	var seq uint64
	if _, _, err := p.latest(&seq); err != nil {
		t.Fatal(err)
	}
	// the server fails: the picture stays for stale_ms, with the error reported
	ps.status.Store(503)
	p.fetchOnce(context.Background())
	pix, changed, err := p.latest(&seq)
	if pix == nil || changed || err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("right after a failure: pix %v changed %v err %v", pix != nil, changed, err)
	}
	time.Sleep(40 * time.Millisecond)
	p.fetchOnce(context.Background())
	if pix, changed, err := p.latest(&seq); pix != nil || !changed || err == nil {
		t.Fatalf("after stale_ms: pix %v changed %v err %v", pix != nil, changed, err)
	}
	// back: shown again
	ps.status.Store(200)
	p.fetchOnce(context.Background())
	if pix, changed, err := p.latest(&seq); pix == nil || !changed || err != nil {
		t.Fatalf("recovered: pix %v changed %v err %v", pix != nil, changed, err)
	}
	// not a picture at all: like a fetch error, the last picture stays until stale_ms
	junk := []byte("<html>no</html>")
	ps.body.Store(&junk)
	p.fetchOnce(context.Background())
	if pix, changed, err := p.latest(&seq); pix == nil || changed || err == nil {
		t.Fatalf("junk body: pix %v changed %v err %v", pix != nil, changed, err)
	}
	// ... and comes down on time even when no fetch finishes meanwhile (a hanging GET)
	time.Sleep(40 * time.Millisecond)
	if pix, changed, err := p.latest(&seq); pix != nil || !changed || err == nil {
		t.Fatalf("stale without a fetch: pix %v changed %v err %v", pix != nil, changed, err)
	}
	// the same good picture after an error is not decoded again (the error clears)
	good := testJPEG(t, 4, 4, color.RGBA{G: 255, A: 255})
	ps.body.Store(&good)
	p.fetchOnce(context.Background())
	if _, _, err := p.latest(&seq); err != nil {
		t.Fatal(err)
	}
	ps.status.Store(503)
	p.fetchOnce(context.Background())
	ps.status.Store(200)
	p.fetchOnce(context.Background())
	if pix, changed, err := p.latest(&seq); pix == nil || err != nil {
		t.Fatalf("same picture after a transient error: pix %v changed %v err %v", pix != nil, changed, err)
	}
	if p.raw == nil || !bytes.Equal(p.raw, good) {
		t.Error("the raw bytes of the good picture must be kept for the comparison")
	}
	// stale_ms 0: the picture is never taken down
	zero := 0
	p = newURLPicture(SlotConfig{URL: ps.URL, StaleMs: &zero}, 4, 4)
	p.fetchOnce(context.Background())
	ps.status.Store(503)
	time.Sleep(5 * time.Millisecond)
	p.fetchOnce(context.Background())
	var seq2 uint64
	if pix, _, err := p.latest(&seq2); pix == nil || err == nil {
		t.Fatalf("stale_ms 0: pix %v err %v", pix != nil, err)
	}
}

func TestURLPictureUnreachableAndTimeout(t *testing.T) {
	p := newURLPicture(SlotConfig{URL: "http://127.0.0.1:1/jpg", TimeoutMs: 200}, 4, 4)
	p.fetchOnce(context.Background())
	var seq uint64
	if pix, _, err := p.latest(&seq); pix != nil || err == nil {
		t.Fatalf("unreachable: pix %v err %v", pix != nil, err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	p = newURLPicture(SlotConfig{URL: slow.URL, TimeoutMs: 100}, 4, 4)
	start := time.Now()
	p.fetchOnce(context.Background())
	if d := time.Since(start); d > time.Second {
		t.Errorf("the timeout must cut a slow server short, took %v", d)
	}
	if _, _, err := p.latest(&seq); err == nil {
		t.Error("a timed-out fetch must be an error")
	}
}

func TestURLPictureResizeReconverts(t *testing.T) {
	ps := newPictureServer(t, testJPEG(t, 16, 8, color.RGBA{R: 255, A: 255}))
	p := newURLPicture(SlotConfig{URL: ps.URL}, 16, 8)
	p.fetchOnce(context.Background())
	var seq uint64
	if pix, _, _ := p.latest(&seq); len(pix) != 16*8*4 {
		t.Fatalf("len %d", len(pix))
	}
	p.resize(8, 4)
	if pix, changed, _ := p.latest(&seq); pix != nil || !changed {
		t.Fatalf("after a resize nothing is shown until the next fetch: pix %v changed %v", pix != nil, changed)
	}
	p.fetchOnce(context.Background()) // same bytes from the server, but no pixels yet: converted for 8x4
	if pix, changed, err := p.latest(&seq); len(pix) != 8*4*4 || !changed || err != nil {
		t.Fatalf("reconverted: len %d changed %v err %v", len(pix), changed, err)
	}
}

// The slot takes the URL's pixels like a PNG's, in a box next to a G-ball too
func TestImageSlotWithURL(t *testing.T) {
	ps := newPictureServer(t, testJPEG(t, 8, 4, color.RGBA{R: 255, A: 255}))
	path := t.TempDir() + "/osd-image"
	s := newImageSlot("imagefile", SlotConfig{URL: ps.URL, IntervalMs: 100}, SlotGeometry{Path: path, Width: 8, Height: 4})
	if s.pic == nil || !s.hasPicture() {
		t.Fatal("a URL slot must have a picture")
	}
	if pix, changed, err := s.render(nil); pix != nil || changed || err != nil {
		t.Fatalf("nothing fetched yet: %v %v %v", pix, changed, err)
	}
	s.pic.fetchOnce(context.Background())
	pix, changed, err := s.render(nil)
	if err != nil || !changed || len(pix) != 8*4*4 {
		t.Fatalf("render: err %v changed %v len %d", err, changed, len(pix))
	}
	if _, changed, _ := s.render(nil); changed {
		t.Error("a second render of the same fetch must not be a change")
	}
	// prudynt resized the slot: the picture is converted for the new box at the next fetch
	if !s.follow(SlotGeometry{Path: path, Width: 16, Height: 8}, SlotConfig{URL: ps.URL}) {
		t.Fatal("follow must report the new size")
	}
	s.pic.fetchOnce(context.Background())
	if pix, _, err := s.render(nil); err != nil || len(pix) != 16*8*4 {
		t.Fatalf("after follow: err %v len %d", err, len(pix))
	}
	// with a G-ball next to it (png_box): the composed frame has the slot's size
	stale := 0
	s = newImageSlot("imagefile", SlotConfig{URL: ps.URL, StaleMs: &stale, PNGBox: &Box{X: 0, Y: 0, Width: 8, Height: 4},
		GBall: &GBallConfig{Source: "rb", X: 0, Y: 4, Size: 32}}, SlotGeometry{Path: path, Width: 40, Height: 40})
	s.pic.fetchOnce(context.Background())
	if pix, _, err = s.render(map[string]any{}); err != nil || len(pix) != 40*40*4 {
		t.Fatalf("composed: err %v len %d", err, len(pix))
	}
	if got := px(pix, 40, 0, 0); got[2] < 240 {
		t.Errorf("the picture must be in its box: %v", got)
	}
}

func TestLoadConfigURLPicture(t *testing.T) {
	c, err := loadConfig(writeCfg(t, `{"slots":{"imagefile3":{"url":"http://192.168.11.113/jpg?w=320&h=240&q=60&gray=1","interval_ms":500,"timeout_ms":1000,"stale_ms":5000,"fit":"none"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	sc := c.Slots["imagefile3"]
	if sc.URL == "" || sc.IntervalMs != 500 || sc.TimeoutMs != 1000 || *sc.StaleMs != 5000 || sc.Fit != "none" {
		t.Errorf("got %+v", sc)
	}
	for name, body := range map[string]string{
		"png and url":        `{"slots":{"imagefile":{"png":"/x.png","url":"http://h/jpg"}}}`,
		"not http":           `{"slots":{"imagefile":{"url":"ftp://h/jpg"}}}`,
		"no host":            `{"slots":{"imagefile":{"url":"http:///jpg"}}}`,
		"relative":           `{"slots":{"imagefile":{"url":"/jpg"}}}`,
		"interval too long":  `{"slots":{"imagefile":{"url":"http://h/jpg","interval_ms":9999999}}}`,
		"negative stale":     `{"slots":{"imagefile":{"url":"http://h/jpg","stale_ms":-1}}}`,
		"timeout too long":   `{"slots":{"imagefile":{"url":"http://h/jpg","timeout_ms":99999}}}`,
		"interval with png":  `{"slots":{"imagefile":{"png":"/x.png","interval_ms":500}}}`,
		"url on a text slot": `{"slots":{"textfile":{"template":"x","url":"http://h/jpg"}}}`,
		"png_box without":    `{"slots":{"imagefile":{"png_box":{"x":0,"y":0,"width":8,"height":8}}}}`,
	} {
		if _, err := loadConfig(writeCfg(t, body)); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
	// interval_ms below prudynt's poll is clamped, not refused
	p := newURLPicture(SlotConfig{URL: "http://h/jpg", IntervalMs: 10}, 4, 4)
	if p.interval != urlMinInterval {
		t.Errorf("interval must clamp to %v, got %v", urlMinInterval, p.interval)
	}
}
