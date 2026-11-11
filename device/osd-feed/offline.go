package main

// "OFFLINE" over a remote camera's picture when it cannot be fetched
// (urlimage.go, on_stale "offline"): the last picture darkened with the
// word across it, or, when there never was one, a dark translucent box
// with the word. A tiny 5x7 font for just the letters needed; prudynt has
// no text in the image slots and osd-feed has no other font.

// glyph rows top to bottom, 5 bits each, the leftmost pixel in bit 4
var offlineFont = map[byte][7]byte{
	'O': {0b01110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01110},
	'F': {0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b10000},
	'L': {0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b11111},
	'I': {0b11111, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100, 0b11111},
	'N': {0b10001, 0b11001, 0b10101, 0b10011, 0b10001, 0b10001, 0b10001},
	'E': {0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b11111},
}

const (
	offlineText    = "OFFLINE"
	glyphW, glyphH = 5, 7
	glyphGap       = 1
)

// offlineFrame - what to show for a w x h box while the remote camera is
// not answering: last (the last good BGRA picture, or nil) darkened, with
// the word drawn across the middle. Always a new slice.
func offlineFrame(last []byte, w, h int) []byte {
	out := make([]byte, w*h*4)
	if len(last) == len(out) {
		for i := 0; i < len(out); i += 4 {
			out[i], out[i+1], out[i+2], out[i+3] = last[i]>>1, last[i+1]>>1, last[i+2]>>1, last[i+3]
		}
	} else {
		for i := 0; i < len(out); i += 4 {
			out[i], out[i+1], out[i+2], out[i+3] = 0x20, 0x20, 0x20, 0xb0
		}
	}
	drawOfflineText(out, w, h)
	return out
}

// drawOfflineText - the word centred, as large as fits (up to 4x the 5x7
// font), white with a one-pixel dark outline so that it reads on anything
func drawOfflineText(frame []byte, w, h int) {
	cols := len(offlineText)*(glyphW+glyphGap) - glyphGap
	scale := min(4, min(w/(cols+2), h/(glyphH+2)))
	if scale < 1 {
		return // the box is too small for the word; the darkening says it
	}
	tw, th := cols*scale, glyphH*scale
	x0, y0 := (w-tw)/2, (h-th)/2
	put := func(px, py int, b, g, r, a uint8) {
		if px < 0 || py < 0 || px >= w || py >= h {
			return
		}
		o := (py*w + px) * 4
		frame[o], frame[o+1], frame[o+2], frame[o+3] = b, g, r, a
	}
	stamp := func(dx, dy int, b, g, r uint8) {
		for i := 0; i < len(offlineText); i++ {
			rows := offlineFont[offlineText[i]]
			gx := x0 + i*(glyphW+glyphGap)*scale
			for ry := 0; ry < glyphH; ry++ {
				for rx := 0; rx < glyphW; rx++ {
					if rows[ry]&(1<<(glyphW-1-rx)) == 0 {
						continue
					}
					for sy := 0; sy < scale; sy++ {
						for sx := 0; sx < scale; sx++ {
							put(gx+rx*scale+sx+dx, y0+ry*scale+sy+dy, b, g, r, 255)
						}
					}
				}
			}
		}
	}
	for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		stamp(d[0], d[1], 0, 0, 0)
	}
	stamp(0, 0, 255, 255, 255)
}
