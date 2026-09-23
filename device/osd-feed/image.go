package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/png" // the format the config points at; others registered here would work too
	"os"
	"time"
)

// ImageSlot is prudynt's osd.imagefile overlay: a raw BGRA file of exactly
// Width x Height pixels (straight alpha, no header; docs/osd.md). The PNG
// named in the config is decoded here, fitted into that size and converted;
// the file is rewritten when the PNG changes on disk or the slot's geometry
// does. prudynt has no image decoder: this is where PNG becomes pixels.
type ImageSlot struct {
	Name   string
	Path   string
	Width  int
	Height int
	PNG    string
	Fit    string // "contain" (default) or "none"

	pngMod  time.Time
	pngSize int64
	pixels  []byte // the last conversion, or nil when the PNG could not be read
	err     string
}

func newImageSlot(name string, cfg SlotConfig, geo SlotGeometry) *ImageSlot {
	s := &ImageSlot{Name: name, Path: geo.Path, Width: geo.Width, Height: geo.Height, PNG: cfg.PNG, Fit: cfg.Fit}
	if s.Fit == "" {
		s.Fit = "contain"
	}
	if cfg.Path != "" {
		s.Path = cfg.Path
	}
	if cfg.Width > 0 {
		s.Width = cfg.Width
	}
	if cfg.Height > 0 {
		s.Height = cfg.Height
	}
	return s
}

// follow - take over prudynt's current geometry unless pinned by the config
func (s *ImageSlot) follow(geo SlotGeometry, cfg SlotConfig) bool {
	n := *s
	if cfg.Path == "" && geo.Path != "" {
		n.Path = geo.Path
	}
	if cfg.Width <= 0 && geo.Width > 0 {
		n.Width = geo.Width
	}
	if cfg.Height <= 0 && geo.Height > 0 {
		n.Height = geo.Height
	}
	changed := n.Path != s.Path || n.Width != s.Width || n.Height != s.Height
	if changed {
		n.pngMod = time.Time{} // convert again for the new size
		n.pixels = nil
	}
	*s = n
	return changed
}

// render - the BGRA bytes for the slot's file, converted anew only when the
// PNG changed (mtime or size) since the last call. The second result says
// whether they differ from the last call's. A PNG that cannot be read yields
// nil (the caller hides the overlay) and an error once per distinct problem.
func (s *ImageSlot) render() (pix []byte, changed bool, err error) {
	st, statErr := os.Stat(s.PNG)
	if statErr != nil {
		changed = s.pixels != nil
		s.pixels = nil
		s.pngMod = time.Time{}
		return nil, changed, statErr
	}
	if s.pixels != nil && st.ModTime().Equal(s.pngMod) && st.Size() == s.pngSize {
		return s.pixels, false, nil
	}
	pix, err = convertPNG(s.PNG, s.Width, s.Height, s.Fit)
	s.pngMod, s.pngSize = st.ModTime(), st.Size()
	if err != nil {
		changed = s.pixels != nil
		s.pixels = nil
		return nil, changed, err
	}
	changed = !bytes.Equal(pix, s.pixels)
	s.pixels = pix
	return pix, changed, nil
}

// convertPNG - the file decoded, fitted into w x h and laid out as BGRA with
// straight alpha. "contain": shrunk (never enlarged) to fit, keeping the
// aspect ratio, and centred on a transparent canvas; "none": centred, cropped
// when larger. Integer arithmetic only (softfloat MIPS target).
func convertPNG(path string, w, h int, fit string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if w < 2 || h < 2 {
		return nil, fmt.Errorf("slot size %dx%d", w, h)
	}
	// Premultiplied RGBA is what averaging must happen in (a fully
	// transparent pixel's colour must not bleed into its neighbours)
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)

	sw, sh := rgba.Rect.Dx(), rgba.Rect.Dy()
	dw, dh := sw, sh
	if fit == "contain" && (sw > w || sh > h) {
		// Largest size within w x h with the source's aspect ratio
		if sw*h > sh*w {
			dw, dh = w, max(1, sh*w/sw)
		} else {
			dw, dh = max(1, sw*h/sh), h
		}
	}
	scaled := rgba
	if dw != sw || dh != sh {
		scaled = boxScale(rgba, dw, dh)
	}

	// Centre on the canvas; crop what does not fit ("none" with a big source)
	out := make([]byte, w*h*4)
	ox, oy := (w-dw)/2, (h-dh)/2
	for y := 0; y < dh; y++ {
		cy := oy + y
		if cy < 0 || cy >= h {
			continue
		}
		for x := 0; x < dw; x++ {
			cx := ox + x
			if cx < 0 || cx >= w {
				continue
			}
			i := scaled.PixOffset(x, y)
			r, g, bb, a := scaled.Pix[i], scaled.Pix[i+1], scaled.Pix[i+2], scaled.Pix[i+3]
			if a != 0 && a != 255 { // back to straight alpha for the OSD
				r = uint8(int(r) * 255 / int(a))
				g = uint8(int(g) * 255 / int(a))
				bb = uint8(int(bb) * 255 / int(a))
			}
			o := (cy*w + cx) * 4
			out[o], out[o+1], out[o+2], out[o+3] = bb, g, r, a
		}
	}
	return out, nil
}

// boxScale - shrink a premultiplied RGBA image to dw x dh by averaging the
// source pixels each destination pixel covers (a box filter, in integers)
func boxScale(src *image.RGBA, dw, dh int) *image.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		y0, y1 := y*sh/dh, (y+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0, x1 := x*sw/dw, (x+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, a, n int
			for sy := y0; sy < y1; sy++ {
				i := src.PixOffset(x0, sy)
				for sx := x0; sx < x1; sx++ {
					r += int(src.Pix[i])
					g += int(src.Pix[i+1])
					b += int(src.Pix[i+2])
					a += int(src.Pix[i+3])
					n++
					i += 4
				}
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = uint8(r/n), uint8(g/n), uint8(b/n), uint8(a/n)
		}
	}
	return dst
}
