package main

import "math"

// A G-ball (friction circle) drawn into the image slot from two G values.
// The rings, the cross and the ball sprites are drawn once per size; a redraw
// copies them and places the ball and its trail with integer blending only
// (softfloat MIPS: about 1.4 ms per frame at 160 px, measured on the camera).
// Only the squares around the ball are repainted (repaint): copying the
// whole slot every frame was most of osd-feed's CPU on the camera.

const (
	minGBallSize  = 32
	maxGBallTrail = 32
	maxGBallMaxG  = 10
)

// sprite: a small straight-alpha BGRA picture, centred on where it is put
type sprite struct {
	w, h int
	p    []byte
}

type gball struct {
	cfg   GBallConfig // with the defaults filled in
	size  int
	trail int

	rings []byte // size x size BGRA: background disc, rings, cross
	ball  sprite
	dots  []sprite // trail sprites, oldest (faintest) first

	hist  [][2]int // past ball centres in pixels, oldest first
	drawn [][2]int // where the trail and ball are in the frame (nil: not drawn)
	next  [][2]int // where update wants them
}

// update, repaint: the G-ball as an image slot widget (image.go). Only the
// squares around the ball and trail are repainted.
func (g *gball) update(data map[string]any) bool {
	g.next = g.place(data)
	return !samePositions(g.next, g.drawn)
}

func (g *gball) repaint(frame, base []byte, cw, ch int) {
	r := g.reach()
	for _, p := range g.drawn {
		x0, x1 := max(0, p[0]-r), min(cw-1, p[0]+r)
		if x0 > x1 {
			continue
		}
		for y := max(0, p[1]-r); y <= min(ch-1, p[1]+r); y++ {
			o := (y*cw + x0) * 4
			copy(frame[o:o+(x1-x0+1)*4], base[o:])
		}
	}
	g.draw(frame, cw, ch, g.next)
	g.drawn = g.next
}

func samePositions(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func newGBall(cfg GBallConfig, size int) *gball {
	if cfg.Lateral == "" {
		cfg.Lateral = "g_y"
	}
	if cfg.Longitudinal == "" {
		cfg.Longitudinal = "g_x"
	}
	if cfg.MaxG <= 0 {
		cfg.MaxG = 1.5
	}
	trail := 8
	if cfg.Trail != nil {
		trail = *cfg.Trail
	}
	g := &gball{cfg: cfg, size: size, trail: trail}
	r := g.ballRadius()
	g.ball = discSprite(r, [3]uint8{255, 40, 40}, 255)
	// a white centre so the ball stays visible over red or dark video
	blitSprite(g.ball.p, g.ball.w, g.ball.h, discSprite(max(1, r/3), [3]uint8{255, 255, 255}, 255), g.ball.w/2, g.ball.h/2)
	for i := 0; i < trail; i++ {
		a := 90 + 140*(i+1)/trail
		g.dots = append(g.dots, discSprite(max(1, r*6/10), [3]uint8{255, 170, 40}, uint8(a)))
	}
	g.drawRings()
	return g
}

func (g *gball) ballRadius() int { return max(2, g.size/26) }

// outer - the radius of the outer ring (max_g) in pixels
func (g *gball) outer() float64 { return float64(g.size)/2 - 2 }

// drawRings - the static part: a translucent dark disc (readable over bright
// video), a ring every 0.5 G (the 1 G one bolder) and a cross. Done once per
// size; the per-pixel square root is taken only near a ring.
func (g *gball) drawRings() {
	s := g.size
	g.rings = make([]byte, s*s*4)
	c := float64(s-1) / 2
	R := g.outer()
	type ring struct{ r, w, lo, hi float64 }
	var rings []ring
	for v := 0.5; v <= g.cfg.MaxG+1e-9; v += 0.5 {
		w := 0.8
		if math.Abs(v-1) < 1e-9 {
			w = 1.4
		}
		r := v / g.cfg.MaxG * R
		lo, hi := math.Max(0, r-w-1), r+w+1
		rings = append(rings, ring{r, w, lo * lo, hi * hi})
	}
	for y := 0; y < s; y++ {
		dy := float64(y) - c
		for x := 0; x < s; x++ {
			dx := float64(x) - c
			d2 := dx*dx + dy*dy
			if d2 > (R+2)*(R+2) {
				continue
			}
			o := (y*s + x) * 4
			if d2 <= R*R {
				blendPixel(g.rings[o:o+4], 0, 0, 0, 90)
			}
			for _, rg := range rings {
				if d2 < rg.lo || d2 > rg.hi {
					continue
				}
				if cov := 1 - math.Abs(math.Sqrt(d2)-rg.r)/rg.w; cov > 0 {
					blendPixel(g.rings[o:o+4], 255, 255, 255, uint8(math.Min(cov, 1)*200))
				}
			}
			if d2 <= R*R && (math.Abs(dx) < 0.6 || math.Abs(dy) < 0.6) {
				blendPixel(g.rings[o:o+4], 255, 255, 255, 128)
			}
		}
	}
}

// discSprite - an anti-aliased disc of radius r
func discSprite(r int, rgb [3]uint8, alpha uint8) sprite {
	n := 2*r + 3
	sp := sprite{w: n, h: n, p: make([]byte, n*n*4)}
	c := float64(n-1) / 2
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			cov := float64(r) + 0.5 - math.Hypot(float64(x)-c, float64(y)-c)
			if cov <= 0 {
				continue
			}
			o := (y*n + x) * 4
			blendPixel(sp.p[o:o+4], rgb[0], rgb[1], rgb[2], uint8(math.Min(cov, 1)*float64(alpha)))
		}
	}
	return sp
}

// blendPixel - colour r, g, b at alpha a over one straight-alpha BGRA pixel
func blendPixel(d []byte, r, g, b, a uint8) {
	blendBGRA(d, b, g, r, a)
}

func blendBGRA(d []byte, b, g, r, a uint8) {
	sa := uint32(a)
	if sa == 0 {
		return
	}
	if sa == 255 {
		d[0], d[1], d[2], d[3] = b, g, r, 255
		return
	}
	da := uint32(d[3]) * (255 - sa) / 255 // what shows through of the old pixel
	oa := sa + da
	d[0] = uint8((uint32(b)*sa + uint32(d[0])*da) / oa)
	d[1] = uint8((uint32(g)*sa + uint32(d[1])*da) / oa)
	d[2] = uint8((uint32(r)*sa + uint32(d[2])*da) / oa)
	d[3] = uint8(oa)
}

// blitSprite - sp over the w x h canvas, centred at cx, cy; clipped
func blitSprite(canvas []byte, w, h int, sp sprite, cx, cy int) {
	ox, oy := cx-sp.w/2, cy-sp.h/2
	for y := 0; y < sp.h; y++ {
		dy := oy + y
		if dy < 0 || dy >= h {
			continue
		}
		for x := 0; x < sp.w; x++ {
			dx := ox + x
			if dx < 0 || dx >= w {
				continue
			}
			s := sp.p[(y*sp.w+x)*4:]
			d := canvas[(dy*w+dx)*4:]
			blendBGRA(d[:4], s[0], s[1], s[2], s[3])
		}
	}
}

// drawStatic - the rings into the canvas (cw x ch) at the configured place
func (g *gball) drawStatic(canvas []byte, cw, ch int) {
	for y := 0; y < g.size; y++ {
		cy := g.cfg.Y + y
		if cy >= ch {
			break
		}
		for x := 0; x < g.size; x++ {
			cx := g.cfg.X + x
			if cx >= cw {
				break
			}
			s := g.rings[(y*g.size+x)*4:]
			blendBGRA(canvas[(cy*cw+cx)*4:(cy*cw+cx)*4+4], s[0], s[1], s[2], s[3])
		}
	}
}

// place - where the trail (oldest first) and then the ball go for this
// snapshot, in canvas pixels, and the ball added to the trail for next time.
// nil without usable values (source not ok, keys missing or not numbers):
// nothing is drawn and the trail is forgotten, so the ball does not come
// back dragging a stale tail.
func (g *gball) place(data map[string]any) [][2]int {
	lat, lon, ok := g.values(data)
	if !ok {
		g.hist = g.hist[:0]
		return nil
	}
	if g.cfg.InvertLateral {
		lat = -lat
	}
	if g.cfg.InvertLongitudinal {
		lon = -lon
	}
	// A ball in a bowl: pushed opposite to the measured acceleration
	c := float64(g.size-1) / 2
	scale := g.outer() / g.cfg.MaxG
	px, py := -lat*scale, lon*scale
	lim := g.outer() - float64(g.ballRadius()) // more than max_g: on the rim
	if d := math.Hypot(px, py); d > lim {
		px, py = px*lim/d, py*lim/d
	}
	ball := [2]int{g.cfg.X + int(math.Round(c+px)), g.cfg.Y + int(math.Round(c+py))}
	pos := append(append(make([][2]int, 0, len(g.hist)+1), g.hist...), ball)
	if g.trail > 0 {
		if len(g.hist) == g.trail {
			copy(g.hist, g.hist[1:])
			g.hist = g.hist[:g.trail-1]
		}
		g.hist = append(g.hist, ball)
	}
	return pos
}

// draw - the trail and the ball at what place returned
func (g *gball) draw(canvas []byte, cw, ch int, pos [][2]int) {
	if len(pos) == 0 {
		return
	}
	n := len(pos) - 1
	for i, p := range pos[:n] {
		blitSprite(canvas, cw, ch, g.dots[len(g.dots)-n+i], p[0], p[1])
	}
	blitSprite(canvas, cw, ch, g.ball, pos[n][0], pos[n][1])
}

// reach - how far from a placed centre a sprite can paint
func (g *gball) reach() int { return g.ball.w/2 + 1 }

func (g *gball) values(data map[string]any) (lat, lon float64, ok bool) {
	src, _ := data[g.cfg.Source].(map[string]any)
	if src == nil {
		return 0, 0, false
	}
	if fresh, _ := src["ok"].(bool); !fresh {
		return 0, 0, false
	}
	lat, ok1 := toFloat(src[g.cfg.Lateral])
	lon, ok2 := toFloat(src[g.cfg.Longitudinal])
	return lat, lon, ok1 && ok2
}
