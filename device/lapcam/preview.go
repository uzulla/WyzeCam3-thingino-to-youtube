package main

// Preview, two ways (docs/lapcam.md): prudynt's MJPEG (:8080/mjpeg, 5 fps,
// JPEG channel), relayed and thinned to the fps the client asked for, which
// every browser shows in an <img>; and prudynt's fragmented MP4 (:8080/ch0.mp4,
// the stream as it goes to YouTube), relayed byte for byte for the page's
// MediaSource player. Both share a small number of viewer slots.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	previewMaxFPS   = 10
	previewDefFPS   = 4
	previewBoundary = "webuiframe"
	frameMaxBytes   = 2 << 20 // a JPEG from prudynt is ~50 KB
	previewMaxSlots = 3       // phones watching at the same time
	frameWriteLimit = 10 * time.Second
)

// previewSlots - each viewer is one connection to prudynt; a few phones at
// most, so the camera's Wi-Fi is not spent on preview pictures
var previewSlots = make(chan struct{}, previewMaxSlots)

// upstream - how prudynt is reached: a prudynt that accepts and never
// answers must not hold a slot for ever
var upstream = &http.Transport{ResponseHeaderTimeout: 10 * time.Second, DisableKeepAlives: true}

const upstreamIdle = 15 * time.Second // no bytes from prudynt for this long: give up

// openMJPEG - connect to prudynt's MJPEG and return a reader of its parts
func (s *server) openMJPEG(ctx context.Context) (*multipart.Reader, io.Closer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.prudynt+"/mjpeg", nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := upstream.RoundTrip(req)
	if err != nil {
		return nil, nil, fmt.Errorf("prudynt mjpeg: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("prudynt mjpeg: HTTP %d", resp.StatusCode)
	}
	mt, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/x-mixed-replace" || params["boundary"] == "" {
		resp.Body.Close()
		return nil, nil, errors.New("prudynt mjpeg: not a multipart stream")
	}
	return multipart.NewReader(resp.Body, params["boundary"]), resp.Body, nil
}

// nextFrame - the next JPEG from the upstream stream
func nextFrame(mr *multipart.Reader) ([]byte, error) {
	p, err := mr.NextPart()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(p, frameMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if n > frameMaxBytes {
		return nil, errors.New("frame too large")
	}
	return buf.Bytes(), nil
}

func (s *server) handlePreview(w http.ResponseWriter, r *http.Request) {
	fps := previewDefFPS
	if v, err := strconv.Atoi(r.URL.Query().Get("fps")); err == nil && v >= 1 && v <= previewMaxFPS {
		fps = v
	}
	select {
	case previewSlots <- struct{}{}:
		defer func() { <-previewSlots }()
	default:
		http.Error(w, "too many viewers", http.StatusServiceUnavailable)
		return
	}
	upCtx, upCancel := context.WithCancel(r.Context())
	defer upCancel()
	mr, body, err := s.openMJPEG(upCtx)
	if err != nil {
		log.Printf("preview: %v", err)
		http.Error(w, "no picture from the camera's encoder", http.StatusBadGateway)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+previewBoundary)
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	rc := http.NewResponseController(w)

	// even thinning: a frame goes out when its time has come (the upstream
	// is 5 fps; comparing with the last sent time alone would skip every
	// other frame and give ~2.4 fps for fps=4)
	minGap := time.Second / time.Duration(fps)
	next := time.Now()
	idle := time.AfterFunc(upstreamIdle, upCancel) // a silent prudynt ends the relay
	defer idle.Stop()
	for {
		frame, err := nextFrame(mr)
		if err != nil {
			if r.Context().Err() == nil {
				log.Printf("preview: upstream ended: %v", err)
			}
			return
		}
		idle.Reset(upstreamIdle)
		now := time.Now()
		if now.Before(next.Add(-minGap / 2)) {
			continue // thin: drop this frame
		}
		next = next.Add(minGap)
		if next.Before(now) {
			next = now
		}
		// a phone that vanished (no FIN) must not hold this slot and the
		// prudynt connection until TCP gives up
		_ = rc.SetWriteDeadline(time.Now().Add(frameWriteLimit))
		if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", previewBoundary, len(frame)); err != nil {
			return
		}
		if _, err := w.Write(frame); err != nil {
			return
		}
		if _, err := io.WriteString(w, "\r\n"); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// handleSnapshot - one JPEG (the next frame of the stream)
func (s *server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	mr, body, err := s.openMJPEG(ctx)
	if err != nil {
		log.Printf("snapshot: %v", err)
		http.Error(w, "no picture from the camera's encoder", http.StatusBadGateway)
		return
	}
	defer body.Close()
	frame, err := nextFrame(mr)
	if err != nil {
		http.Error(w, "no frame from the camera's encoder", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(frame)))
	_, _ = w.Write(frame)
}

// handlePreviewMP4 - the main stream as fragmented MP4 (prudynt's /ch0.mp4),
// relayed byte for byte: the page feeds it to a <video> through MediaSource
// for a smooth picture where the browser can (Android, PC). Same viewer
// limit and per-write deadline as the MJPEG.
func (s *server) handlePreviewMP4(w http.ResponseWriter, r *http.Request) {
	select {
	case previewSlots <- struct{}{}:
		defer func() { <-previewSlots }()
	default:
		http.Error(w, "too many viewers", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.prudynt+"/ch0.mp4", nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := upstream.RoundTrip(req)
	if err != nil {
		log.Printf("preview.mp4: %v", err)
		http.Error(w, "no video from the camera's encoder", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "video/mp4") {
		log.Printf("preview.mp4: prudynt answered %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
		http.Error(w, "no video from the camera's encoder", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	rc := http.NewResponseController(w)
	idle := time.AfterFunc(upstreamIdle, cancel) // a silent prudynt ends the relay
	defer idle.Stop()
	buf := make([]byte, 32<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			idle.Reset(upstreamIdle)
			_ = rc.SetWriteDeadline(time.Now().Add(frameWriteLimit))
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if rerr != io.EOF && r.Context().Err() == nil {
				log.Printf("preview.mp4: upstream ended: %v", rerr)
			}
			return
		}
	}
}
