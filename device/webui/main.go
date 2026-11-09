// webui - a small web app for the camera, made for a phone at the track
// (docs/webui.md): live preview, start/stop the YouTube stream and the SD
// recording, flip the picture, see the state of the camera. One static Go
// binary on the SD card, started by /etc/init.d/S97webui.
//
//	webui [-listen :8000] [-sd /mnt/mmcblk0p1] [-prudynt http://127.0.0.1:8080] [-syslog] [-check]
//
// It changes nothing by itself: every action is what the existing scripts do
// (record, /etc/init.d/S93youtube-relay, service, prudyntctl) and every
// setting lives where it lived before (youtube-relay.json, prudynt-osd.json
// on the SD card). Its own file is webui.json next to them (stream profiles).
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
}

func main() {
	listen := flag.String("listen", ":8000", "address to serve on")
	sd := flag.String("sd", "/mnt/mmcblk0p1", "SD card mount (youtube-relay.json, prudynt-osd.json, webui.json live there)")
	prudynt := flag.String("prudynt", "http://127.0.0.1:8080", "prudynt's HTTP API, for the preview")
	toSyslog := flag.Bool("syslog", false, "log to the syslog (logread) instead of stderr; the init script sets this")
	shadow := flag.String("shadow", shadowFile, "the shadow file holding root's password hash (for testing with another file)")
	check := flag.Bool("check", false, "print what the status endpoint would answer and exit (the installer runs this)")
	flag.Parse()
	log.SetFlags(0)
	shadowFile = *shadow
	if *toSyslog {
		if w, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, "webui"); err == nil {
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

	if *check {
		st := s.status.collect()
		fmt.Println(string(mustJSON(st)))
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go s.status.run(ctx)

	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("%v", err)
	}
	mux := http.NewServeMux()
	// pages and assets: no login needed for the shell itself, the app asks
	// /api/me and shows the sign-in form
	mux.Handle("/", http.FileServer(http.FS(static)))
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.handleMe)
	// everything else needs a session
	mux.Handle("GET /api/status", s.auth(http.HandlerFunc(s.handleStatus)))
	mux.Handle("GET /api/preview.mjpeg", s.auth(http.HandlerFunc(s.handlePreview)))
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

	srv := &http.Server{
		Addr:              *listen,
		Handler:           noCache(mux),
		ReadHeaderTimeout: 10 * time.Second,
		// no WriteTimeout: the MJPEG preview is one long response
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

// noCache - the app and its API must never be served stale by the phone
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// exists - is there a file at path?
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
