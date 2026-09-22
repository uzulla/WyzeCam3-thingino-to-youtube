// osd-feed - keeps prudynt's text overlays (docs/osd.md) up to date from data
// sources: memory, a demo counter, the clock, HTTP JSON, ... (source.go).
//
// Runs ON THE CAMERA, from the SD card (/mnt/mmcblk0p1/osd-feed, started by
// /etc/init.d/S94osd-feed). Built on the PC with build.sh (Go cross-compile,
// no toolchain needed). Configuration: osd-feed.json next to the binary.
//
// The contract with prudynt is the one from docs/osd.md: write the text to a
// temp file on tmpfs, mv it over the slot's file; prudynt redraws within 100 ms
// and skips unchanged content. Nothing is written to flash.
//
//	osd-feed [-config /mnt/mmcblk0p1/osd-feed.json] [-syslog] [-once]
//
// -once renders every slot once to stdout and exits (to try a template).
// -syslog sends the log to logread instead of stderr (the init script does).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/syslog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

func main() {
	configPath := flag.String("config", "/mnt/mmcblk0p1/osd-feed.json", "configuration file")
	once := flag.Bool("once", false, "render each slot once to stdout and exit")
	toSyslog := flag.Bool("syslog", false, "log to the syslog (logread) instead of stderr; the init script sets this")
	flag.Parse()
	log.SetFlags(0)
	if *toSyslog {
		// busybox syslogd listens on /dev/log; logread shows the messages
		if w, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, "osd-feed"); err == nil {
			log.SetOutput(w)
		} else {
			log.Printf("syslog not available (%v), logging to stderr", err)
		}
	}

	if msg := setLocalFromEtcTZ(); msg != "" {
		log.Printf("%s", msg)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	names := make([]string, 0, len(cfg.Slots))
	for n := range cfg.Slots {
		names = append(names, n)
	}
	sort.Strings(names)
	geo, answered := askPrudynt(context.Background(), names)
	if !answered {
		log.Printf("prudynt did not answer the osd.textfile query - using the documented defaults for path/cols/rows")
	}
	slots := make([]*Slot, 0, len(names))
	for _, n := range names {
		s, err := newSlot(n, cfg.Slots[n], geo[n])
		if err != nil {
			log.Fatalf("%v", err)
		}
		slots = append(slots, s)
		if answered && !geo[n].Enabled {
			log.Printf("slot %s is not enabled in prudynt (%s): the text is written but not shown until osd.%s.enabled is true (prudynt-osd.json or the web UI)", n, geo[n], n)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	cache := newCache()
	sources := map[string]Source{}
	for name, sc := range cfg.Sources {
		src, err := sourceTypes[sc.Type](sc)
		if err != nil {
			log.Fatalf("source %s: %v", name, err)
		}
		sources[name] = src
		cache.register(name)
	}
	// Dry run before anything is started: a template that names a source the
	// config does not have (a typo) fails here, once, instead of every tick
	for _, s := range slots {
		if _, err := s.render(cache.snapshot(time.Now())); err != nil {
			log.Fatalf("slot %s: template: %v (sources: %v)", s.Name, err, sourceNames(cfg))
		}
	}
	if *once {
		// Poll every source once, in turn, then render: what the templates
		// would show a moment after start-up
		var wg sync.WaitGroup
		for name, src := range sources {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pctx, cancel := context.WithTimeout(ctx, pollTimeout(cfg.Sources[name]))
				v, err := src.Poll(pctx)
				cancel()
				cache.put(name, v, err)
				if err != nil {
					log.Printf("source %s: %v", name, err)
				}
			}()
		}
		wg.Wait()
		data := cache.snapshot(time.Now())
		for _, s := range slots {
			text, err := s.render(data)
			if err != nil {
				log.Fatalf("slot %s: %v", s.Name, err)
			}
			fmt.Printf("--- %s (%s, %dx%d)\n%s", s.Name, s.Path, s.Cols, s.Rows, text)
		}
		return
	}

	for name, src := range sources {
		go runSource(ctx, name, cfg.Sources[name], src, cache)
	}
	if cfg.IntervalMs > 0 && cfg.IntervalMs < 100 {
		log.Printf("interval_ms %d is below prudynt's 100 ms poll: using 100", cfg.IntervalMs)
	}
	log.Printf("osd-feed: %d slots, %d sources, redraw every %v", len(slots), len(cfg.Sources), cfg.interval())
	last := map[string]string{}    // what each slot's file holds (or "error")
	lastErr := map[string]string{} // last problem reported per slot, once
	writes := 0
	t := time.NewTicker(cfg.interval())
	defer t.Stop()
	// prudynt is restarted now and then (osd-config changing the pool size, the
	// web UI) and its slots can be resized from the web UI while we run: ask
	// again every so often and follow. One prudyntctl every 30 s is nothing.
	// It runs off the render loop: a prudynt that does not answer must not
	// hold up the drawing (or the exit) for the 5 s the query may take.
	geoCh := make(chan map[string]SlotGeometry, 1)
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if g, ok := askPrudynt(ctx, names); ok {
					select {
					case geoCh <- g:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	report := func(slot, msg string) {
		if lastErr[slot] != msg {
			log.Printf("slot %s: %s", slot, msg)
			lastErr[slot] = msg
		}
	}
	recovered := func(slot string) {
		if lastErr[slot] != "" {
			log.Printf("slot %s: ok again", slot)
			lastErr[slot] = ""
		}
	}
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case g := <-geoCh:
			for _, s := range slots {
				if s.follow(g[s.Name], cfg.Slots[s.Name]) {
					log.Printf("slot %s: now %s", s.Name, g[s.Name])
					delete(last, s.Name) // redraw with the new size
				}
			}
		case now := <-t.C:
			data := cache.snapshot(now)
			for _, s := range slots {
				text, err := s.render(data)
				if err != nil {
					// A template that fails at run time (bad field type) is a
					// config problem: say so once per slot, keep the others going
					report(s.Name, "template: "+err.Error())
					continue
				}
				// Unchanged text is not rewritten, unless the file itself is
				// gone or not ours any more (the web UI's Clear or Show,
				// another writer): then the overlay would stay wrong for good
				if text == last[s.Name] && fileHolds(s.Path, text) {
					continue
				}
				if err := writeSlot(s.Path, text); err != nil {
					report(s.Name, err.Error())
					continue
				}
				recovered(s.Name)
				last[s.Name] = text
				writes++
			}
		}
	}
	if cfg.clearOnExit() {
		for _, s := range slots {
			os.Remove(s.Path)
		}
	}
	log.Printf("osd-feed: stopped after %d writes", writes)
}

// writeSlot - the tmp + mv contract: prudynt never sees a half-written file.
// The temp file sits in the same directory (same tmpfs), so the rename is
// atomic; its name carries the pid so two writers (this and, say, the web
// UI's Show) never share one.
func writeSlot(path, text string) error {
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	// /run/prudynt is prudynt's; should it be gone (prudynt not started yet
	// after a reboot), create it rather than fail until it appears
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	// WriteFile's mode is cut by the umask (077 under the init scripts): the
	// web UI's CGI and anyone debugging should still be able to read the file
	os.Chmod(tmp, 0o644)
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func sourceNames(cfg *Config) []string {
	names := make([]string, 0, len(cfg.Sources))
	for n := range cfg.Sources {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// fileHolds - the file exists and holds exactly text (a few hundred bytes
// read from tmpfs per slot per tick; another writer's text of the same size
// must be noticed too)
func fileHolds(path, text string) bool {
	b, err := os.ReadFile(path)
	return err == nil && string(b) == text
}
