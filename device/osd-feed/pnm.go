package main

// Uncompressed pictures for the image slots, next to PNG and JPEG: PGM (P5,
// gray8) and PPM (P6, RGB24) with their one-line header, and headerless BGRA
// (prudynt's own layout, B G R A per pixel). A camera that can send these
// (uzulla/m5camera-ble-wifi: /image?fmt=pgm|ppm|bgra) spares this camera the
// JPEG decode, which is what costs the CPU (docs/osd-feed.md). The pixels are
// opaque (BGRA keeps the alpha it carries).

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"strconv"
)

// pictureKind - what the bytes are, from their magic (PNM) or their length
// (headerless BGRA for the box); "" means "ask image.Decode" (PNG, JPEG)
func pictureKind(data []byte, w, h int) string {
	if len(data) > 2 && data[0] == 'P' && (data[1] == '5' || data[1] == '6') && isPNMSpace(data[2]) {
		return "pnm"
	}
	if w > 0 && h > 0 && len(data) == w*h*4 && (len(data) < 8 || !looksLikeImageFile(data)) {
		return "bgra"
	}
	return ""
}

func isPNMSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\r' || c == '\t' }

// looksLikeImageFile - the magic of the formats image.Decode knows here
func looksLikeImageFile(data []byte) bool {
	return bytes.HasPrefix(data, []byte("\x89PNG")) || bytes.HasPrefix(data, []byte("\xff\xd8"))
}

// decodePNM - a binary PGM (P5) or PPM (P6) with maxval 255 as an RGBA
// image (opaque). The header is "P5 <w> <h> 255" in whitespace, a single
// whitespace byte, then the pixels; "#" comments are allowed as in the
// netpbm spec. Only what the slots can show is accepted (prudynt's
// 1280x720 limit, in bytes).
func decodePNM(data []byte) (*image.RGBA, error) {
	if len(data) < 3 || data[0] != 'P' || (data[1] != '5' && data[1] != '6') {
		return nil, errors.New("not a binary PGM/PPM (P5/P6)")
	}
	gray := data[1] == '5'
	pos := 2
	var vals [3]int
	for i := range vals {
		// skip whitespace and comments
		for pos < len(data) {
			if isPNMSpace(data[pos]) {
				pos++
			} else if data[pos] == '#' {
				for pos < len(data) && data[pos] != '\n' {
					pos++
				}
			} else {
				break
			}
		}
		start := pos
		for pos < len(data) && data[pos] >= '0' && data[pos] <= '9' {
			pos++
		}
		if start == pos {
			return nil, errors.New("PGM/PPM: bad header")
		}
		v, err := strconv.Atoi(string(data[start:pos]))
		if err != nil {
			return nil, fmt.Errorf("PGM/PPM: bad header: %v", err)
		}
		vals[i] = v
	}
	// exactly one whitespace byte after maxval, then the raster
	if pos >= len(data) || !isPNMSpace(data[pos]) {
		return nil, errors.New("PGM/PPM: bad header")
	}
	pos++
	w, h, maxval := vals[0], vals[1], vals[2]
	if maxval != 255 {
		return nil, fmt.Errorf("PGM/PPM: maxval %d (only 255, 8 bits per sample)", maxval)
	}
	if w <= 0 || h <= 0 || w > maxSourceSide || h > maxSourceSide || int64(w)*int64(h)*4 > maxSourceBytes {
		return nil, fmt.Errorf("PGM/PPM: %dx%d is too large to show (at most %d pixels)", w, h, maxSourcePixels)
	}
	bpp := 3
	if gray {
		bpp = 1
	}
	if len(data)-pos != w*h*bpp {
		return nil, fmt.Errorf("PGM/PPM: %dx%d needs %d bytes of pixels, got %d", w, h, w*h*bpp, len(data)-pos)
	}
	src := data[pos:]
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	pix := img.Pix
	if gray {
		for i, v := range src {
			o := i * 4
			pix[o], pix[o+1], pix[o+2], pix[o+3] = v, v, v, 255
		}
	} else {
		for i := 0; i < w*h; i++ {
			s, o := i*3, i*4
			pix[o], pix[o+1], pix[o+2], pix[o+3] = src[s], src[s+1], src[s+2], 255
		}
	}
	return img, nil
}

// bgraPixels - headerless BGRA of exactly the box's size is the slot's own
// format: handed through as it is (no decoding, no copy)
func bgraPixels(data []byte, w, h int) ([]byte, error) {
	if len(data) != w*h*4 {
		return nil, fmt.Errorf("BGRA: %d bytes, but the box is %dx%d (%d bytes): ask the camera for w=%d&h=%d", len(data), w, h, w*h*4, w, h)
	}
	return data, nil
}
