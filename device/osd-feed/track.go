package main

import "math"

// A track map drawn into the image slot from GPS positions: every position
// is plotted and stays, so a few laps draw the circuit by themselves; the
// oldest are rubbed out once there are more than "points" of them. The view
// is the range of the plotted latitudes and longitudes (north up, equal
// metres on both axes), with a margin so that it does not have to be
// redrawn for every new point. A dot marks the latest position while the
// source is fresh.
//
// Cheap per redraw on the camera (softfloat MIPS): a new position costs one
// projection and a few pixels; the whole map (every point projected again)
// is redrawn only when the view changes, which settles after the first lap.

const (
	minTrackSize      = 32
	maxTrackPoints    = 36000 // an hour at the fastest redraw (100 ms)
	defaultTrackPts   = 6000  // 10 minutes at 100 ms
	defaultTrackMoveM = 3.0
	trackDot          = 2         // track dots are trackDot x trackDot pixels
	trackMinSpanM     = 100.0     // the view never zooms in closer than this
	trackResetM       = 20000.    // a jump this far starts a new map (another place)
	trackPad          = 0.1       // margin added on each side when the view grows
	metresPerUnit     = 0.0111319 // 1e-7 degree of latitude
)

type track struct {
	cfg  TrackConfig // with the defaults filled in
	x, y int         // top left in the slot
	w, h int

	// the plotted positions in 1e-7 degrees (the RaceBox's own unit), a ring
	// of at most cfg.Points, oldest at head
	lat, lon []int32
	cell     []int32 // each point's top left cell in the grid (-1: outside)
	head, n  int

	view     // the area mapped onto the widget, in 1e-7 degrees
	haveView bool

	count []uint16 // per pixel: how many points cover it

	bg          []byte // w x h BGRA: the translucent background (static part)
	marker      sprite
	sinceBounds int // new points since the view was last checked for shrinking

	// what repaint has to do
	full          bool
	dirty         []int32 // grid cells whose count went 0 <-> 1
	mark, drawnAt [2]int  // marker centre in the widget; -1 = none
}

func newTrack(cfg TrackConfig, x, y, w, h int) *track {
	if cfg.Lat == "" {
		cfg.Lat = "lat"
	}
	if cfg.Lon == "" {
		cfg.Lon = "lon"
	}
	if cfg.Valid == "" {
		cfg.Valid = "latlon_valid"
	}
	if cfg.Points <= 0 {
		cfg.Points = defaultTrackPts
	}
	if cfg.MinMoveM == nil {
		m := defaultTrackMoveM
		cfg.MinMoveM = &m
	}
	t := &track{cfg: cfg, x: x, y: y, w: w, h: h,
		lat: make([]int32, cfg.Points), lon: make([]int32, cfg.Points), cell: make([]int32, cfg.Points),
		count: make([]uint16, w*h), mark: [2]int{-1, -1}, drawnAt: [2]int{-1, -1}}
	t.marker = discSprite(max(2, min(w, h)/50), [3]uint8{255, 40, 40}, 255)
	blitSprite(t.marker.p, t.marker.w, t.marker.h, discSprite(1, [3]uint8{255, 255, 255}, 255), t.marker.w/2, t.marker.h/2)
	t.bg = make([]byte, w*h*4)
	for i := 0; i < w*h; i++ {
		blendPixel(t.bg[i*4:i*4+4], 0, 0, 0, 90)
	}
	return t
}

func (t *track) drawStatic(canvas []byte, cw, ch int) {
	for y := 0; y < t.h && t.y+y < ch; y++ {
		for x := 0; x < t.w && t.x+x < cw; x++ {
			s := t.bg[(y*t.w+x)*4:]
			o := ((t.y+y)*cw + t.x + x) * 4
			blendBGRA(canvas[o:o+4], s[0], s[1], s[2], s[3])
		}
	}
}

// update - take the snapshot's position: plot it (when it moved far enough
// from the last plotted one) and move the marker. Without a fresh, valid
// position only the marker goes; the map stays.
func (t *track) update(data map[string]any) bool {
	lat, lon, ok := t.values(data)
	if !ok {
		t.mark = [2]int{-1, -1}
		return t.mark != t.drawnAt || t.full || len(t.dirty) > 0
	}
	if t.n > 0 {
		last := (t.head + t.n - 1) % len(t.lat)
		d := t.metres(t.lat[last], t.lon[last], lat, lon)
		if d > trackResetM {
			t.reset()
			t.add(lat, lon)
		} else if d >= *t.cfg.MinMoveM {
			t.add(lat, lon)
		}
	} else {
		t.add(lat, lon)
	}
	t.mark = [2]int{-1, -1}
	if t.haveView {
		px, py := t.project(lat, lon)
		t.mark = [2]int{int(math.Round(px)), int(math.Round(py))}
	}
	return t.mark != t.drawnAt || t.full || len(t.dirty) > 0
}

func (t *track) reset() {
	for i := range t.count {
		t.count[i] = 0
	}
	t.head, t.n, t.haveView, t.full = 0, 0, false, true
	t.dirty = t.dirty[:0]
}

func (t *track) add(lat, lon int32) {
	if t.n == len(t.lat) { // full: rub out the oldest
		t.unplot(t.cell[t.head])
		t.head = (t.head + 1) % len(t.lat)
		t.n--
	}
	i := (t.head + t.n) % len(t.lat)
	t.lat[i], t.lon[i] = lat, lon
	t.n++
	t.sinceBounds++
	if !t.haveView || lat < t.vLat0 || lat > t.vLat1 || lon < t.vLon0 || lon > t.vLon1 {
		t.fitView()
		return
	}
	// Old points leave: now and then see whether the view could zoom in
	if t.sinceBounds >= 100 {
		t.sinceBounds = 0
		if v := t.viewFor(t.bounds()); v.k > 1.5*t.k {
			t.fitView()
			return
		}
	}
	t.cell[i] = t.cellOf(lat, lon)
	t.plot(t.cell[i])
}

// fitView - a new view around every plotted point, with a margin, and every
// point placed in it again
func (t *track) fitView() {
	t.view = t.viewFor(t.bounds())
	t.haveView = true
	for i := range t.count {
		t.count[i] = 0
	}
	for j := 0; j < t.n; j++ {
		i := (t.head + j) % len(t.lat)
		t.cell[i] = t.cellOf(t.lat[i], t.lon[i])
		if c := t.cell[i]; c >= 0 {
			for dy := 0; dy < trackDot; dy++ {
				for dx := 0; dx < trackDot; dx++ {
					t.count[int(c)+dy*t.w+dx]++
				}
			}
		}
	}
	t.full = true
	t.dirty = t.dirty[:0]
	t.sinceBounds = 0
}

type view struct {
	vLat0, vLat1, vLon0, vLon1 int32
	k, cosLat, ox, oy          float64 // pixels per unit, and the offset that centres the view
}

// viewFor - the view for these bounds: a margin around them, at least
// trackMinSpanM across, the same scale on both axes and centred
func (t *track) viewFor(lat0, lat1, lon0, lon1 int32) view {
	var v view
	v.cosLat = math.Max(0.01, math.Cos((float64(lat0)+float64(lat1))/2*1e-7*math.Pi/180))
	// in latitude units: longitude units shrunk by cos
	spanY := float64(lat1) - float64(lat0)
	spanX := (float64(lon1) - float64(lon0)) * v.cosLat
	minSpan := trackMinSpanM / metresPerUnit
	padY := math.Max(spanY*trackPad, (minSpan-spanY)/2)
	padX := math.Max(spanX*trackPad, (minSpan-spanX)/2)
	v.vLat0, v.vLat1 = clampI32(float64(lat0)-padY), clampI32(float64(lat1)+padY)
	v.vLon0, v.vLon1 = clampI32(float64(lon0)-padX/v.cosLat), clampI32(float64(lon1)+padX/v.cosLat)
	vy := float64(v.vLat1) - float64(v.vLat0)
	vx := (float64(v.vLon1) - float64(v.vLon0)) * v.cosLat
	m := float64(t.marker.w/2 + 1) // the marker stays inside at the edge
	aw, ah := float64(t.w)-2*m-trackDot, float64(t.h)-2*m-trackDot
	v.k = math.Min(aw/math.Max(vx, 1), ah/math.Max(vy, 1))
	v.ox = m + (aw-vx*v.k)/2
	v.oy = m + (ah-vy*v.k)/2
	return v
}

func clampI32(v float64) int32 {
	return int32(math.Max(math.MinInt32, math.Min(math.MaxInt32, math.Round(v))))
}

func (t *track) bounds() (lat0, lat1, lon0, lon1 int32) {
	lat0, lon0 = math.MaxInt32, math.MaxInt32
	lat1, lon1 = math.MinInt32, math.MinInt32
	for j := 0; j < t.n; j++ {
		i := (t.head + j) % len(t.lat)
		lat0, lat1 = min(lat0, t.lat[i]), max(lat1, t.lat[i])
		lon0, lon1 = min(lon0, t.lon[i]), max(lon1, t.lon[i])
	}
	return
}

// project - the position in widget pixels (may be outside before fitView)
func (t *track) project(lat, lon int32) (float64, float64) {
	return t.ox + (float64(lon)-float64(t.vLon0))*t.cosLat*t.k, t.oy + (float64(t.vLat1)-float64(lat))*t.k
}

// cellOf - the top left grid cell of the point's dot, -1 when it does not fit
func (t *track) cellOf(lat, lon int32) int32 {
	px, py := t.project(lat, lon)
	x, y := int(px), int(py)
	if px < 0 || py < 0 || x+trackDot > t.w || y+trackDot > t.h {
		return -1
	}
	return int32(y*t.w + x)
}

func (t *track) plot(c int32) {
	if c < 0 {
		return
	}
	for dy := 0; dy < trackDot; dy++ {
		for dx := 0; dx < trackDot; dx++ {
			p := int(c) + dy*t.w + dx
			if t.count[p]++; t.count[p] == 1 && !t.full {
				t.dirty = append(t.dirty, int32(p))
			}
		}
	}
}

func (t *track) unplot(c int32) {
	if c < 0 {
		return
	}
	for dy := 0; dy < trackDot; dy++ {
		for dx := 0; dx < trackDot; dx++ {
			p := int(c) + dy*t.w + dx
			if t.count[p] == 0 {
				continue
			}
			if t.count[p]--; t.count[p] == 0 && !t.full {
				t.dirty = append(t.dirty, int32(p))
			}
		}
	}
}

// metres - the distance between two positions (flat earth: fine for a circuit)
func (t *track) metres(lat0, lon0, lat1, lon1 int32) float64 {
	c := math.Cos(float64(lat0) * 1e-7 * math.Pi / 180)
	return math.Hypot(float64(lat1)-float64(lat0), (float64(lon1)-float64(lon0))*c) * metresPerUnit
}

// pixel - one widget pixel of the frame as it should look: the base, then
// the track when a point covers it
func (t *track) pixel(frame, base []byte, cw, p int) {
	x, y := t.x+p%t.w, t.y+p/t.w
	o := (y*cw + x) * 4
	copy(frame[o:o+4], base[o:o+4])
	if t.count[p] > 0 {
		blendPixel(frame[o:o+4], 255, 255, 255, 230)
	}
}

func (t *track) repaint(frame, base []byte, cw, ch int) {
	w, h := min(t.w, cw-t.x), min(t.h, ch-t.y)
	if w <= 0 || h <= 0 {
		return
	}
	inside := func(p int) bool { return p%t.w < w && p/t.w < h }
	if t.full {
		for p := range t.count {
			if inside(p) {
				t.pixel(frame, base, cw, p)
			}
		}
	} else {
		for _, p := range t.dirty {
			if inside(int(p)) {
				t.pixel(frame, base, cw, int(p))
			}
		}
		// where the marker was: the base and the track back
		if m := t.drawnAt; m[0] >= 0 {
			r := t.marker.w / 2
			for y := max(0, m[1]-r); y <= min(h-1, m[1]+r); y++ {
				for x := max(0, m[0]-r); x <= min(w-1, m[0]+r); x++ {
					t.pixel(frame, base, cw, y*t.w+x)
				}
			}
		}
	}
	t.full = false
	t.dirty = t.dirty[:0]
	if m := t.mark; m[0] >= 0 {
		// clipped to the widget, so it never paints over its neighbours
		blitSpriteClip(frame, cw, ch, t.marker, t.x+m[0], t.y+m[1], t.x, t.y, t.x+w, t.y+h)
	}
	t.drawnAt = t.mark
}

// blitSpriteClip - blitSprite limited to the rectangle x0,y0 - x1,y1 (exclusive)
func blitSpriteClip(canvas []byte, cw, ch int, sp sprite, cx, cy, x0, y0, x1, y1 int) {
	ox, oy := cx-sp.w/2, cy-sp.h/2
	for y := 0; y < sp.h; y++ {
		dy := oy + y
		if dy < max(0, y0) || dy >= min(ch, y1) {
			continue
		}
		for x := 0; x < sp.w; x++ {
			dx := ox + x
			if dx < max(0, x0) || dx >= min(cw, x1) {
				continue
			}
			s := sp.p[(y*sp.w+x)*4:]
			d := canvas[(dy*cw+dx)*4:]
			blendBGRA(d[:4], s[0], s[1], s[2], s[3])
		}
	}
}

// values - the position in 1e-7 degrees; not ok when the source is not
// fresh, the keys are missing, the valid flag (when the source has it) is
// false, or the numbers are out of range (0,0 is a receiver without a fix)
func (t *track) values(data map[string]any) (lat, lon int32, ok bool) {
	src, _ := data[t.cfg.Source].(map[string]any)
	if src == nil {
		return 0, 0, false
	}
	if fresh, _ := src["ok"].(bool); !fresh {
		return 0, 0, false
	}
	if v, has := src[t.cfg.Valid]; has {
		if b, _ := v.(bool); !b {
			return 0, 0, false
		}
	}
	la, ok1 := toFloat(src[t.cfg.Lat])
	lo, ok2 := toFloat(src[t.cfg.Lon])
	if !ok1 || !ok2 || la < -90 || la > 90 || lo < -180 || lo > 180 || (la == 0 && lo == 0) {
		return 0, 0, false
	}
	return int32(math.Round(la * 1e7)), int32(math.Round(lo * 1e7)), true
}
