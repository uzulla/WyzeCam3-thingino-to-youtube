// lapcam - a small web app for the camera, made for a phone at the track
// (docs/lapcam.md): live preview, start/stop the YouTube stream and the SD
// recording, flip the picture, see the state of the camera. One static Go
// binary on the SD card, started by /etc/init.d/S97lapcam.
//
//	lapcam [-listen :8000] [-sd /mnt/mmcblk0p1] [-prudynt http://127.0.0.1:8080] [-syslog] [-check]
//
// It changes nothing by itself: every action is what the existing scripts do
// (record, /etc/init.d/S93youtube-relay, service, prudyntctl) and every
// setting lives where it lived before (youtube-relay.json, prudynt-osd.json
// on the SD card). Its own file is lapcam.json next to them (stream profiles).
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"log/syslog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed static
var staticFS embed.FS

type server struct {
	sd       string // SD card mount
	prudynt  string // prudynt's HTTP API (MJPEG, fMP4)
	sessions *sessions
	status   *statusPoller
	relay    *relay
	rec      *recorder

	wifiMu   sync.Mutex
	wifiNote string // the last Wi-Fi save was undone (wifi.go)
}

func main() {
	listen := flag.String("listen", ":8000", "address to serve on")
	sd := flag.String("sd", "/mnt/mmcblk0p1", "SD card mount (youtube-relay.json, prudynt-osd.json, lapcam.json live there)")
	prudynt := flag.String("prudynt", "http://127.0.0.1:8080", "prudynt's HTTP API, for the preview")
	toSyslog := flag.Bool("syslog", false, "log to the syslog (logread) instead of stderr; the init script sets this")
	shadow := flag.String("shadow", shadowFile, "the shadow file holding root's password hash (for testing with another file)")
	check := flag.Bool("check", false, "print what the status endpoint would answer and exit (the installer runs this)")
	flag.Parse()
	log.SetFlags(0)
	shadowFile = *shadow
	if *toSyslog {
		if w, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, "lapcam"); err == nil {
			log.SetOutput(w)
		} else {
			log.Printf("syslog not available (%v), logging to stderr", err)
		}
	}
	if _, _, err := net.SplitHostPort(*listen); err != nil {
		log.Fatalf("-listen %q: %v", *listen, err)
	}

	s := &server{
		sd:       *sd,
		prudynt:  strings.TrimRight(*prudynt, "/"),
		sessions: newSessions(30 * 24 * time.Hour),
		relay:    newRelay(*sd),
		rec:      newRecorder(),
	}
	s.status = newStatusPoller(s.relay, s.rec, 2*time.Second)
	if !*check {
		s.sessions.persistTo("/run/lapcam-sessions.json")
	}

	if *check {
		st := s.status.collect()
		fmt.Println(string(mustJSON(st)))
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go s.status.run(ctx)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute, // a 4 MB logo over a weak Wi-Fi
		IdleTimeout:       60 * time.Second,
		// no WriteTimeout: the MJPEG preview and the zip are long responses
		// (the preview sets its own deadline per frame)
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("Started: listening on %s", *listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("%v", err)
	}
	log.Printf("Stopped")
}

// routes - the whole HTTP surface: pages, sign-in, and the API behind auth
func (s *server) routes() http.Handler {
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("%v", err)
	}
	mux := http.NewServeMux()
	// pages and assets: no login needed for the shell itself, the app asks
	// /api/me and shows the sign-in form
	mux.Handle("/", http.FileServer(http.FS(static)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such API"})
	})
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.handleMe)
	// everything else needs a session
	mux.Handle("GET /api/status", s.auth(http.HandlerFunc(s.handleStatus)))
	mux.Handle("GET /api/preview.mjpeg", s.auth(http.HandlerFunc(s.handlePreview)))
	mux.Handle("GET /api/preview.mp4", s.auth(http.HandlerFunc(s.handlePreviewMP4)))
	mux.Handle("GET /api/snapshot.jpg", s.auth(http.HandlerFunc(s.handleSnapshot)))
	mux.Handle("POST /api/relay", s.auth(http.HandlerFunc(s.handleRelayOp)))
	mux.Handle("GET /api/relay/config", s.auth(http.HandlerFunc(s.handleRelayConfigGet)))
	mux.Handle("POST /api/relay/config", s.auth(http.HandlerFunc(s.handleRelayConfigSet)))
	mux.Handle("POST /api/record", s.auth(http.HandlerFunc(s.handleRecordOp)))
	mux.Handle("POST /api/image", s.auth(http.HandlerFunc(s.handleImageSet)))
	mux.Handle("POST /api/reboot", s.auth(http.HandlerFunc(s.handleReboot)))
	mux.Handle("GET /api/log", s.auth(http.HandlerFunc(s.handleLog)))
	mux.Handle("GET /api/osd", s.auth(http.HandlerFunc(s.handleOSDGet)))
	mux.Handle("POST /api/osd/element", s.auth(http.HandlerFunc(s.handleOSDElement)))
	mux.Handle("POST /api/osd/feed", s.auth(http.HandlerFunc(s.handleOSDFeed)))
	mux.Handle("POST /api/osd/logo", s.auth(http.HandlerFunc(s.handleLogoUpload)))
	mux.Handle("GET /api/wifi", s.auth(http.HandlerFunc(s.handleWifiGet)))
	mux.Handle("GET /api/wifi/scan", s.auth(http.HandlerFunc(s.handleWifiScan)))
	mux.Handle("POST /api/wifi/networks", s.auth(http.HandlerFunc(s.handleWifiSet)))
	mux.Handle("GET /api/quality", s.auth(http.HandlerFunc(s.handleQualityGet)))
	mux.Handle("POST /api/quality", s.auth(http.HandlerFunc(s.handleQualitySet)))
	mux.Handle("GET /api/recordings", s.auth(http.HandlerFunc(s.handleRecordingsList)))
	mux.Handle("GET /api/recordings/file", s.auth(http.HandlerFunc(s.handleRecordingFile)))
	mux.Handle("POST /api/recordings/zip", s.auth(http.HandlerFunc(s.handleZipPrepare)))
	mux.Handle("GET /api/recordings/zip/{id}", s.auth(http.HandlerFunc(s.handleZipGet)))
	mux.Handle("POST /api/recordings/delete", s.auth(http.HandlerFunc(s.handleRecordingDelete)))
	mux.Handle("POST /api/recordings/settings", s.auth(http.HandlerFunc(s.handleRecordingSettings)))

	return guard(mux)
}

// guard - headers for every response, and the rules for API writes:
//   - nothing is cached by the phone
//   - the page may not be framed and scripts come only from here
//   - a POST to the API must come from this app (same-site, JSON or the
//     logo's multipart body), so a page on another site cannot drive the
//     camera through the browser's cookie (CSRF). Browsers without
//     Sec-Fetch-Site are old; the Content-Type rule still stops a plain form.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-site request refused"})
				return
			}
			ct := r.Header.Get("Content-Type")
			isJSON := strings.HasPrefix(ct, "application/json")
			isUpload := strings.HasPrefix(ct, "multipart/form-data") && r.URL.Path == "/api/osd/logo"
			if !isJSON && !isUpload {
				writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "send JSON (application/json)"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// exists - is there a file at path?
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
