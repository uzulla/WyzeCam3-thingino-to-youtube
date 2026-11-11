package main

import (
	"context"
	"image/color"
	"testing"
	"time"
)

// whites - how many fully white opaque pixels a BGRA frame has
func whites(pix []byte) int {
	n := 0
	for i := 0; i+3 < len(pix); i += 4 {
		if pix[i] == 255 && pix[i+1] == 255 && pix[i+2] == 255 && pix[i+3] == 255 {
			n++
		}
	}
	return n
}

func TestOfflineFrameDrawsTheWord(t *testing.T) {
	// a box with no picture: translucent dark, with white letters in it
	f := offlineFrame(nil, 160, 120)
	if len(f) != 160*120*4 || whites(f) == 0 || f[3] != 0xb0 {
		t.Fatalf("box: len %d whites %d alpha %d", len(f), whites(f), f[3])
	}
	// over a picture: the picture darkened (half), letters on top
	last := make([]byte, 160*120*4)
	for i := range last {
		last[i] = 200
	}
	f = offlineFrame(last, 160, 120)
	if f[0] != 100 || f[3] != 200 || whites(f) == 0 {
		t.Errorf("darkened: %v whites %d", f[:4], whites(f))
	}
	// the word scales with the box and is centred: a bigger box has more white
	if whites(offlineFrame(nil, 320, 240)) <= whites(offlineFrame(nil, 160, 120)) {
		t.Error("the word must scale up in a bigger box")
	}
	// too small for the word: still a frame, just darkened / the box
	if f := offlineFrame(nil, 16, 8); len(f) != 16*8*4 || whites(f) != 0 {
		t.Errorf("tiny box: len %d whites %d", len(f), whites(f))
	}
	// every letter of the word has a glyph
	for i := 0; i < len(offlineText); i++ {
		if _, ok := offlineFont[offlineText[i]]; !ok {
			t.Errorf("no glyph for %q", offlineText[i])
		}
	}
}

// The default (on_stale "offline"): a camera that never answered shows the
// OFFLINE box at once; one that stops answering keeps its last picture for
// stale_ms, then shows it darkened with OFFLINE; when it is back the
// picture returns
func TestURLPictureOffline(t *testing.T) {
	p := newURLPicture(SlotConfig{URL: "http://127.0.0.1:1/jpg", TimeoutMs: 200}, 64, 32)
	p.fetchOnce(context.Background())
	var seq uint64
	pix, changed, err := p.latest(&seq)
	if pix == nil || !changed || err == nil || whites(pix) == 0 || !p.offline {
		t.Fatalf("never answered: pix %v changed %v err %v", pix != nil, changed, err)
	}
	p.fetchOnce(context.Background())
	if _, changed, _ := p.latest(&seq); changed {
		t.Error("the same OFFLINE box again is not a change")
	}

	ps := newPictureServer(t, testJPEG(t, 64, 32, color.RGBA{G: 255, A: 255}))
	stale := 30
	p = newURLPicture(SlotConfig{URL: ps.URL, StaleMs: &stale}, 64, 32)
	p.fetchOnce(context.Background())
	pix, _, err = p.latest(&seq)
	if err != nil || whites(pix) != 0 {
		t.Fatalf("good picture: err %v whites %d", err, whites(pix))
	}
	ps.status.Store(503)
	p.fetchOnce(context.Background())
	if pix, changed, err := p.latest(&seq); changed || err == nil || whites(pix) != 0 {
		t.Fatalf("just after a failure the picture stays as it is: changed %v err %v", changed, err)
	}
	time.Sleep(40 * time.Millisecond)
	pix, changed, err = p.latest(&seq)
	if !changed || err == nil || whites(pix) == 0 || len(pix) != 64*32*4 {
		t.Fatalf("after stale_ms: changed %v err %v whites %d", changed, err, whites(pix))
	}
	if g := px(pix, 64, 0, 0); g[1] > 128 {
		t.Errorf("the old picture must be darkened under the word: %v", g)
	}
	ps.status.Store(200)
	p.fetchOnce(context.Background())
	if pix, changed, err := p.latest(&seq); !changed || err != nil || whites(pix) != 0 {
		t.Fatalf("back: changed %v err %v whites %d", changed, err, whites(pix))
	}
	// config: on_stale is checked
	if _, err := loadConfig(writeCfg(t, `{"slots":{"imagefile4":{"url":"http://h/p","on_stale":"blink"}}}`)); err == nil {
		t.Error("on_stale blink must be rejected")
	}
	if _, err := loadConfig(writeCfg(t, `{"slots":{"imagefile4":{"png":"/x.png","on_stale":"hide"}}}`)); err == nil {
		t.Error("on_stale without url must be rejected")
	}
}
