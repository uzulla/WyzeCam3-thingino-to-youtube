package main

// Preview: prudynt serves the main stream as MJPEG on :8080/mjpeg (5 fps,
// JPEG channel). webui relays it so the phone talks to one host and one
// port, and thins it to the fps the client asked for (default 4) so a phone
// on a weak Wi-Fi is not flooded. Works in every browser (an <img>); the
// fMP4 variant for Android comes later (docs/webui.md).

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
	"time"
)

const (
	previewMaxFPS   = 10
	previewDefFPS   = 4
	previewBoundary = "webuiframe"
	frameMaxBytes   = 2 << 20 // a JPEG from prudynt is ~50 KB
)

// openMJPEG - connect to prudynt's MJPEG and return a reader of its parts
func (s *server) openMJPEG(ctx context.Context) (*multipart.Reader, io.Closer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.prudynt+"/mjpeg", nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
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
	mr, body, err := s.openMJPEG(r.Context())
	if err != nil {
		log.Printf("preview: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+previewBoundary)
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	minGap := time.Second / time.Duration(fps)
	var last time.Time
	for {
		frame, err := nextFrame(mr)
		if err != nil {
			if r.Context().Err() == nil {
				log.Printf("preview: upstream ended: %v", err)
			}
			return
		}
		now := time.Now()
		if now.Sub(last) < minGap {
			continue // thin: drop this frame
		}
		last = now
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
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer body.Close()
	frame, err := nextFrame(mr)
	if err != nil {
		http.Error(w, "no frame: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(frame)))
	_, _ = w.Write(frame)
}
