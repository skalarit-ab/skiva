package main

import (
	"image/color"
	"math"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// nowPlaying is the track playing: its record, spinning, ringed by
// the music's spectrum; its title; the seek bar; and the buttons.
type nowPlaying struct {
	root                              *playerRoot
	d                                 *deck
	record                            *record
	titles                            *titles
	seek                              *seekBar
	shuffle, back, play, next, repeat *iconButton
	volume                            *volumeBar
}

func newNowPlaying(r *playerRoot, d *deck) *nowPlaying {
	n := &nowPlaying{root: r, d: d}
	n.record = newRecord(r.meter)
	n.titles = newTitles()
	n.seek = newSeekBar(n)
	send := func(v gunim.Intent) func(*gunim.UI) { return func(u *gunim.UI) { u.Send(n, v) } }
	n.shuffle = newIconButton(icon.Shuffle, 40, send(ToggleShuffle{}))
	n.back = newIconButton(icon.SkipBack, 48, send(Skip{Back: true}))
	n.play = newIconButton(icon.Play, 72, send(TogglePlay{}))
	n.play.primary = true
	n.next = newIconButton(icon.SkipForward, 48, send(Skip{}))
	n.repeat = newIconButton(icon.Repeat, 40, send(CycleRepeat{}))
	n.volume = newVolumeBar(n)
	return n
}

func (n *nowPlaying) show(was, s Player, t Track) {
	fresh := s.Starts != was.Starts
	n.record.show(t, s.Playing, fresh)
	n.titles.show(t, s.Current != 0)
	n.seek.show(t, fresh)
	if s.Playing {
		n.play.morph(icon.Pause)
	} else {
		n.play.morph(icon.Play)
	}
	n.shuffle.setLit(s.Shuffle)
	n.repeat.setLit(s.Repeat != RepeatOff)
	if s.Repeat == RepeatOne {
		n.repeat.morph(icon.Repeat1)
	} else {
		n.repeat.morph(icon.Repeat)
	}
	for _, b := range []*iconButton{n.shuffle, n.back, n.play, n.next, n.repeat} {
		b.accent.Animate(t.Accent, anim.Spring{Response: 0.8, Damping: 1})
	}
	n.volume.show(s.Volume, t.Accent)
}

// Children implements [gunim.Composite].
func (n *nowPlaying) Children() []gunim.Node {
	return []gunim.Node{n.record, n.titles, n.seek, n.shuffle, n.back, n.play, n.next, n.repeat, n.volume}
}

// Layout implements [gunim.Node]: everything in a column, centred, the
// record as large as the room allows.
func (n *nowPlaying) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	const titlesH, seekH, buttonsH, volumeH, gap = 70, 52, 76, 36, 14
	rest := float32(titlesH + seekH + buttonsH + volumeH + 4*gap + 48)
	cover := min(size.W*0.5, (size.H-rest)/ringScale, 380)
	cover = max(cover, 80)
	rec := cover * ringScale
	total := rec + rest - 48
	y := max(24, (size.H-total)/2)
	cx := size.W / 2
	place := func(i int, w, h float32) {
		k := kids.At(i)
		k.Layout(gunim.Tight(geom.Sz(w, h)))
		k.Place(geom.Pt(cx-w/2, y))
	}
	place(0, rec, rec)
	y += rec + gap
	wide := min(size.W-48, 520)
	place(1, wide, titlesH)
	y += titlesH + gap
	place(2, wide, seekH)
	y += seekH + gap
	// The buttons, the play button in the middle.
	sizes := []float32{40, 48, 72, 48, 40}
	spread := []float32{-150, -86, 0, 86, 150}
	if size.W < 380 {
		spread = []float32{-130, -74, 0, 74, 130}
	}
	for i, s := range sizes {
		k := kids.At(3 + i)
		k.Layout(gunim.Tight(geom.Sz(s, s)))
		k.Place(geom.Pt(cx+spread[i]-s/2, y+(buttonsH-s)/2))
	}
	y += buttonsH + gap
	place(8, min(wide, 260), volumeH)
	return size
}

// Paint implements [gunim.Node].
func (n *nowPlaying) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(p)
	}
}

// ringScale is how much larger than the record its spectrum ring
// reaches.
const ringScale = 1.5

// record is the track's cover as a picture disc, spinning while the
// track plays and slowing to a stop as it pauses, ringed by bars that
// move with the music. A new track's record grows in as the last one
// fades away.
type record struct {
	anim.Group
	meter        *meter
	cover, last  *paint.Image
	accent, glow *anim.Color
	// spin is how fast it turns, 0 to 1 of 33⅓ turns a minute, and
	// angle where it has turned to.
	spin  *anim.Float
	angle float64
	// in brings the new record in, and pop is its size, which springs
	// down a little while paused.
	in, pop *anim.Float
}

func newRecord(m *meter) *record {
	r := &record{meter: m, accent: anim.NewColor(neutral), glow: anim.NewColor(neutral),
		spin: anim.NewFloat(0), in: anim.NewFloat(1), pop: anim.NewFloat(0.94)}
	r.Add(r.accent, r.glow, r.spin, r.in, r.pop)
	return r
}

func (r *record) show(t Track, playing, fresh bool) {
	if t.Cover != r.cover {
		r.last, r.cover = r.cover, t.Cover
		r.in.Jump(0)
		r.in.Animate(1, anim.Spring{Response: 0.6, Damping: 0.8})
	}
	if fresh {
		r.angle = 0
	}
	r.accent.Animate(t.Accent, anim.Spring{Response: 0.8, Damping: 1})
	r.glow.Animate(t.Glow, anim.Spring{Response: 0.8, Damping: 1})
	// A turntable takes a moment to come up to speed, and longer to
	// run down.
	if playing {
		r.spin.Animate(1, anim.Spring{Response: 0.9, Damping: 1})
		r.pop.Animate(1, anim.Spring{Response: 0.45, Damping: 0.55})
	} else {
		r.spin.Animate(0, anim.Spring{Response: 1.6, Damping: 1})
		r.pop.Animate(0.94, anim.Spring{Response: 0.5, Damping: 0.9})
	}
}

// Step implements [gunim.Animator].
func (r *record) Step(dt time.Duration) bool {
	moving := r.Group.Step(dt)
	if s := r.spin.Value(); s > 0.0005 {
		r.angle += dt.Seconds() * float64(s) * 2 * math.Pi * (100.0 / 3 / 60)
		moving = true
	}
	return moving
}

// Layout implements [gunim.Node].
func (r *record) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Max
}

// Paint implements [gunim.Node].
func (r *record) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	mid := geom.Pt(box.W/2, box.H/2)
	disc := box.W / ringScale / 2
	accent, glow := r.accent.Value(), r.glow.Value()
	m := r.meter

	// A glow behind, breathing with the music.
	gr := disc * (1.02 + 0.22*m.level)
	end := p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Blur: disc * 0.25})
	p.RRect(geom.Rc(mid.X-gr, mid.Y-gr, 2*gr, 2*gr), gr, paint.Solid(faded(accent, 0.35+0.4*m.level)))
	end()

	// The ring: a bar for each band, mirrored left and right, the bass
	// at the top.
	const bars = 2 * bandCount
	base := disc + 10
	width := max(3, float32(2*math.Pi)*base/bars*0.5)
	for i := range bars {
		band := i
		if i >= bandCount {
			band = bars - 1 - i
		}
		v := m.bands[band]
		length := 3 + v*disc*0.42
		angle := float32(-math.Pi/2) + (float32(i)+0.5)*float32(2*math.Pi)/bars
		c := mix(accent, glow, float32(band)/bandCount)
		func() {
			defer p.Push(paint.Rotate(angle+math.Pi/2, mid))()
			p.RRect(geom.Rc(mid.X-width/2, mid.Y-base-length, width, length), width/2, paint.Solid(faded(c, 0.3+0.7*v)))
		}()
	}

	// The records: the last fading out as it grows, the new growing in.
	in := r.in.Value()
	if r.last != nil && in < 1 {
		r.paintDisc(p, mid, disc*(1+0.15*in), r.last, 1-in)
	}
	r.paintDisc(p, mid, disc*r.pop.Value()*(0.6+0.4*in), r.cover, in)
}

// paintDisc draws a picture disc of radius rad at mid, turned to the
// record's angle.
func (r *record) paintDisc(p *paint.Painter, mid geom.Point, rad float32, cover *paint.Image, alpha float32) {
	if alpha <= 0.01 || rad <= 0 {
		return
	}
	box := geom.Rc(mid.X-rad, mid.Y-rad, 2*rad, 2*rad)
	p.ShadowRRect(box, rad, paint.Solid(faded(night, alpha)), paint.Shadow{Blur: rad * 0.18, Offset: geom.Pt(0, rad*0.06), Color: faded(color.NRGBA{A: 0xff}, 0.6*alpha)})
	func() {
		defer p.Push(paint.Rotate(float32(r.angle), mid))()
		if cover != nil {
			p.Image(cover, box, paint.ImageOpts{Radius: rad, Opacity: alpha})
		} else {
			p.RRect(box, rad, paint.Solid(faded(color.NRGBA{R: 0x1c, G: 0x1d, B: 0x26, A: 0xff}, alpha)))
		}
		// A mark near the edge, so a disc with no cover is seen to turn.
		p.RRect(geom.Rc(mid.X-rad*0.03, mid.Y-rad*0.9, rad*0.06, rad*0.12), rad*0.03, paint.Solid(faded(ink, 0.18*alpha)))
	}()
	// Grooves, and the light across them, which stay still as it turns.
	for _, g := range []float32{0.93, 0.78, 0.62} {
		gr := rad * g
		p.RRectStroke(geom.Rc(mid.X-gr, mid.Y-gr, 2*gr, 2*gr), gr, paint.Solid(faded(night, 0.16*alpha)), paint.Stroke{Width: 1})
	}
	p.RRect(box, rad, paint.Fill{Gradient: &paint.Gradient{
		From: box.Min, To: box.Max,
		Start: faded(ink, 0.16*alpha), End: faded(ink, 0),
	}})
	// The label in the middle, and the hole.
	lr := rad * 0.17
	p.RRect(geom.Rc(mid.X-lr, mid.Y-lr, 2*lr, 2*lr), lr, paint.Solid(faded(night, 0.85*alpha)))
	hr := rad * 0.035
	p.RRect(geom.Rc(mid.X-hr, mid.Y-hr, 2*hr, 2*hr), hr, paint.Solid(faded(ink, 0.6*alpha)))
}

// titles shows the track's title and artist. A new track's slide up
// into place as the last track's slide up and away.
type titles struct {
	anim.Group
	title, artist         string
	lastTitle, lastArtist string
	in                    *anim.Float
}

func newTitles() *titles {
	t := &titles{in: anim.NewFloat(1)}
	t.Add(t.in)
	return t
}

func (t *titles) show(tr Track, known bool) {
	title, artist := tr.Title, tr.Artist
	if !known {
		title, artist = "Pick a song", "or press Space to play"
	}
	if artist == "" {
		artist = tr.Album
	}
	if title == t.title && artist == t.artist {
		return
	}
	t.lastTitle, t.lastArtist = t.title, t.artist
	t.title, t.artist = title, artist
	t.in.Jump(0)
	t.in.Animate(1, anim.Spring{Response: 0.5, Damping: 0.85})
}

// Layout implements [gunim.Node].
func (t *titles) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Max
}

// Paint implements [gunim.Node].
func (t *titles) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	in := t.in.Value()
	if in < 1 {
		t.paintPair(p, box, t.lastTitle, t.lastArtist, -16*in, 1-in)
	}
	t.paintPair(p, box, t.title, t.artist, 16*(1-in), in)
}

// paintPair draws a title over an artist, centred, dy down, at alpha.
func (t *titles) paintPair(p *paint.Painter, box geom.Size, title, artist string, dy, alpha float32) {
	if alpha <= 0.01 {
		return
	}
	line := func(s string, size float32, bold bool, y float32, c color.NRGBA) {
		run := shaped(s, size, bold)
		scale := float32(1)
		if run.Advance > box.W {
			scale = box.W / run.Advance
		}
		x := (box.W - run.Advance*scale) / 2
		defer p.Push(paint.Scale(scale, geom.Pt(x, y)))()
		run.Paint(p, geom.Pt(x, y), c)
	}
	line(title, 26, true, 4+dy, faded(ink, alpha))
	line(artist, 16, false, 42+dy, faded(ink, 0.6*alpha))
}

// seekBar is the track drawn as its loudness along it: the part played
// lit in the track's colour. The bars swell about the playhead as
// under a lens, more while the pointer is on the bar, and a seek glides
// the playhead, and the lens with it, to its new place. A press or a
// drag moves the track there.
type seekBar struct {
	anim.Group
	n      *nowPlaying
	peaks  []float32
	length time.Duration
	accent *anim.Color
	// reveal grows the bars in, left to right, as a track's peaks come.
	reveal *anim.Float
	hover  *anim.Float
	// head is where the playhead shows, 0 to 1 along the track: it
	// follows the track playing, and glides where the track jumps.
	head *anim.Float
	// flash lights the bars about the playhead as it lands from a
	// seek.
	flash *anim.Float
	// held says the pointer drags the playhead, to at, from 0 to 1,
	// and last is where the pointer was last.
	held bool
	at   float32
	last geom.Point
	size geom.Size
}

func newSeekBar(n *nowPlaying) *seekBar {
	s := &seekBar{n: n, accent: anim.NewColor(neutral), reveal: anim.NewFloat(0), hover: anim.NewFloat(0),
		head: anim.NewFloat(0), flash: anim.NewFloat(0)}
	s.Add(s.accent, s.reveal, s.hover, s.head, s.flash)
	return s
}

func (s *seekBar) show(t Track, fresh bool) {
	if fresh || (s.peaks == nil) != (t.Peaks == nil) {
		s.reveal.Jump(0)
		s.reveal.Animate(1, anim.Tween{Duration: 900 * time.Millisecond, Ease: anim.EaseOut})
	}
	s.peaks, s.length = t.Peaks, t.Length
	s.accent.Animate(t.Accent, anim.Spring{Response: 0.8, Damping: 1})
}

// The lens: how much wider the bars at the playhead are than those far
// from it, while playing and while the pointer is on the bar, and how
// far along the track it reaches, as a fraction of it.
const (
	lensRest  = 0.6
	lensHover = 1.6
	lensWidth = 0.05
)

// lens maps a place along the track, u from 0 to 1, to a place along
// the bar, 0 to 1, where the bars about focus are spread by strength
// and the rest pressed together to make room. Each bar's room is
// 1 + strength·e^(−((u−focus)/lensWidth)²), summed from the start.
type lens struct{ focus, strength float64 }

// erfFrom is the room from the track's start to u, unscaled.
func (l lens) room(u float64) float64 {
	k := l.strength * lensWidth * math.Sqrt(math.Pi) / 2
	return u + k*(math.Erf((u-l.focus)/lensWidth)-math.Erf(-l.focus/lensWidth))
}

func (l lens) at(u float64) float64 { return l.room(u) / l.room(1) }

// zoom is how much the lens spreads the bars at u.
func (l lens) zoom(u float64) float64 {
	d := (u - l.focus) / lensWidth
	return (1 + l.strength*math.Exp(-d*d)) / l.room(1)
}

// back returns the place along the track the bar's place x lies at.
func (l lens) back(x float64) float64 {
	lo, hi := 0.0, 1.0
	for range 30 {
		mid := (lo + hi) / 2
		if l.at(mid) < x {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// lensNow is the lens as it is this frame.
func (s *seekBar) lensNow() lens {
	return lens{focus: float64(s.head.Value()), strength: lensRest + (lensHover-lensRest)*float64(s.hover.Value())}
}

// DragsTouch implements [gunim.TouchDragger]: a finger on the bar
// moves the playhead rather than scroll.
func (s *seekBar) DragsTouch() bool { return true }

// barH is the height of the bars' band, under which the times go.
const barH = 34

// Handle implements [gunim.Handler].
func (s *seekBar) Handle(e input.Event, u *gunim.UI) bool {
	// Where the pointer is along the track, through the lens as drawn.
	frac := func(p geom.Point) float32 {
		x := min(max(p.X/max(s.size.W, 1), 0), 1)
		return float32(s.lensNow().back(float64(x)))
	}
	switch e := e.(type) {
	case input.PointerEnter:
		s.hover.Animate(1, anim.Spring{Response: 0.35, Damping: 0.8})
	case input.PointerLeave:
		if !s.held {
			s.hover.Animate(0, anim.Spring{Response: 0.5, Damping: 1})
		}
	case input.PointerDown:
		if e.Button != input.ButtonPrimary || s.length <= 0 {
			return false
		}
		s.held, s.at, s.last = true, frac(e.Pos), e.Pos
		s.head.Animate(s.at, anim.Spring{Response: 0.3, Damping: 0.85})
	case input.PointerMove:
		if !s.held {
			return false
		}
		s.at, s.last = frac(e.Pos), e.Pos
		s.head.Animate(s.at, anim.Spring{Response: 0.12, Damping: 1})
	case input.PointerUp:
		if !s.held {
			return false
		}
		s.held = false
		// The time shown as the drag ended is the time it lands on,
		// though the lens has moved under the pointer since.
		if e.Pos != s.last {
			s.at = frac(e.Pos)
		}
		s.head.Animate(s.at, anim.Spring{Response: 0.3, Damping: 0.85})
		s.flash.Jump(1)
		s.flash.Animate(0, anim.Tween{Duration: 600 * time.Millisecond})
		u.Send(s, SeekTo{At: time.Duration(float64(s.at) * float64(s.length))})
		if e.Pos.Y < 0 || e.Pos.Y > s.size.H {
			s.hover.Animate(0, anim.Spring{Response: 0.5, Damping: 1})
		}
	default:
		return false
	}
	u.Invalidate()
	return true
}

// Step implements [gunim.Animator]: the playhead follows the track as
// it plays, and glides where the track jumps.
func (s *seekBar) Step(dt time.Duration) bool {
	if !s.held {
		at, length := s.n.d.position()
		if length <= 0 {
			length = s.length
		}
		if length > 0 {
			to := float32(float64(at) / float64(length))
			if d := to - s.head.Target(); d > 0.01 || d < -0.01 {
				// A jump, as a seek from the keys or Back: glide there.
				s.head.Animate(to, anim.Spring{Response: 0.4, Damping: 0.82})
				if !s.flash.Active() {
					s.flash.Jump(1)
					s.flash.Animate(0, anim.Tween{Duration: 600 * time.Millisecond})
				}
			} else if s.head.Active() {
				s.head.Retarget(to, anim.Spring{Response: 0.4, Damping: 0.82})
			} else {
				s.head.Jump(to)
			}
		}
	}
	moving := s.Group.Step(dt)
	return moving || s.n.root.state.Playing
}

// Layout implements [gunim.Node].
func (s *seekBar) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	s.size = c.Max
	return c.Max
}

// Paint implements [gunim.Node].
func (s *seekBar) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	at, length := s.n.d.position()
	if length <= 0 {
		length = s.length
	}
	if s.held {
		at = time.Duration(float64(s.at) * float64(length))
	}
	head := float64(s.head.Value())
	l := s.lensNow()
	accent := s.accent.Value()
	hover := s.hover.Value()
	flash := s.flash.Value()
	count := int(min(float32(peakCount), box.W/4))
	mid := float32(barH) / 2
	reveal := s.reveal.Value()
	for i := range count {
		u0, u1 := float64(i)/float64(count), float64(i+1)/float64(count)
		uc := (u0 + u1) / 2
		x0, x1 := float32(l.at(u0))*box.W, float32(l.at(u1))*box.W
		w := max(1.5, (x1-x0)*0.55)
		v := float32(0.15)
		if len(s.peaks) > 0 {
			v = s.peaks[i*len(s.peaks)/count]
		}
		// Each bar grows in a little after the one before.
		grow := min(max(reveal*1.4-float32(i)/float32(count)*0.4, 0), 1)
		// The lens lifts the bars it spreads, a little.
		lift := float32(l.zoom(uc)-1/l.room(1)) / float32(lensHover+1) * 0.6
		h := (3 + v*(barH-10)) * (1 + lift) * grow
		c := faded(ink, 0.2+0.25*lift)
		if uc <= head {
			c = accent
		}
		if flash > 0.01 {
			d := (uc - head) / (2 * lensWidth)
			c = mix(c, ink, flash*0.7*float32(math.Exp(-d*d)))
		}
		p.RRect(geom.Rc((x0+x1)/2-w/2, mid-h/2, w, max(h, 1)), w/2, paint.Solid(c))
	}
	// The playhead.
	if length > 0 {
		x := float32(l.at(head)) * box.W
		hw := 1.5 + 1*hover
		p.RRect(geom.Rc(x-hw, -2, 2*hw, barH+4), hw, paint.Solid(faded(ink, 0.6+0.4*hover)))
	}
	dim := faded(ink, 0.55)
	left := shaped(clock(at), 12, false)
	left.Paint(p, geom.Pt(0, barH+4), dim)
	if length > 0 {
		right := shaped("−"+clock(length-at), 12, false)
		right.Paint(p, geom.Pt(box.W-right.Advance, barH+4), dim)
	}
}

// iconButton is a round button with an icon: lit as the pointer comes
// over it, squashed as it is pressed. Its icon turns into another as
// it changes, as play into pause.
type iconButton struct {
	anim.Group
	ic, from *icon.Icon
	press    func(*gunim.UI)
	// primary fills the button with the accent, for play.
	primary bool
	// lit marks a setting that is on, as shuffle.
	lit                      bool
	hover, down, turn, litAt *anim.Float
	accent                   *anim.Color
	held                     bool
	size                     geom.Size
}

func newIconButton(ic *icon.Icon, _ float32, press func(*gunim.UI)) *iconButton {
	b := &iconButton{ic: ic, press: press, hover: anim.NewFloat(0), down: anim.NewFloat(0),
		turn: anim.NewFloat(1), litAt: anim.NewFloat(0), accent: anim.NewColor(neutral)}
	b.Add(b.hover, b.down, b.turn, b.litAt, b.accent)
	return b
}

// morph turns the icon into ic.
func (b *iconButton) morph(ic *icon.Icon) {
	if ic == b.ic {
		return
	}
	b.from, b.ic = b.ic, ic
	b.turn.Jump(0)
	b.turn.Animate(1, anim.Spring{Response: 0.35, Damping: 0.75})
}

func (b *iconButton) setLit(on bool) {
	b.lit = on
	to := float32(0)
	if on {
		to = 1
	}
	b.litAt.Animate(to, anim.Snappy)
}

// Focusable implements [gunim.Focusable]: the player's own keys work
// the buttons, so a click leaves them with the window.
func (b *iconButton) Focusable() bool { return false }

// Handle implements [gunim.Handler].
func (b *iconButton) Handle(e input.Event, u *gunim.UI) bool {
	inside := func(p geom.Point) bool { return (geom.Rect{Max: b.size.Point()}).Contains(p) }
	switch e := e.(type) {
	case input.PointerEnter:
		b.hover.Animate(1, anim.Snappy)
	case input.PointerLeave:
		b.hover.Animate(0, anim.Gentle)
		b.down.Animate(0, anim.Gentle)
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		b.held = true
		b.down.Animate(1, anim.Spring{Response: 0.12, Damping: 1})
	case input.PointerUp:
		if !b.held {
			return false
		}
		b.held = false
		b.down.Animate(0, anim.Spring{Response: 0.4, Damping: 0.45})
		if inside(e.Pos) {
			u.Cue(gunim.CuePress, b)
			b.press(u)
		}
	default:
		return false
	}
	u.Invalidate()
	return true
}

// Layout implements [gunim.Node].
func (b *iconButton) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	b.size = c.Max
	return c.Max
}

// Paint implements [gunim.Node].
func (b *iconButton) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	if box.W <= 0 {
		return
	}
	mid := geom.Pt(box.W/2, box.H/2)
	scale := 1 - 0.1*b.down.Value() + 0.04*b.hover.Value()
	defer p.Push(paint.Scale(scale, mid))()
	accent := b.accent.Value()
	hover := b.hover.Value()
	r := box.W / 2
	c := faded(ink, 0.85+0.15*hover)
	if b.primary {
		p.ShadowRRect(geom.Rect{Max: box.Point()}, r, paint.Solid(accent),
			paint.Shadow{Blur: 18 + 10*hover, Offset: geom.Pt(0, 4), Color: faded(accent, 0.45)})
		c = night
	} else if hover > 0.01 {
		p.RRect(geom.Rect{Max: box.Point()}, r, paint.Solid(faded(ink, 0.08*hover)))
	}
	if lit := b.litAt.Value(); lit > 0.01 {
		c = mix(c, accent, lit)
		// A dot under a setting that is on.
		p.RRect(geom.Rc(mid.X-2, box.H-6, 4, 4), 2, paint.Solid(faded(accent, lit)))
	}
	side := box.W * 0.42
	if b.primary {
		side = box.W * 0.4
	}
	turn := b.turn.Value()
	paintIcon := func(ic *icon.Icon, alpha, angle float32) {
		if ic == nil || alpha <= 0.01 {
			return
		}
		defer p.Push(paint.Rotate(angle, mid))()
		widget.PaintIcon(p, f.Theme, ic, geom.Rc(mid.X-side/2, mid.Y-side/2, side, side), faded(c, alpha))
	}
	if turn < 1 {
		paintIcon(b.from, 1-turn, turn*math.Pi/2)
	}
	paintIcon(b.ic, turn, (turn-1)*math.Pi/2)
}

// volumeBar sets the volume: a speaker, which mutes, and a line to
// drag along.
type volumeBar struct {
	anim.Group
	n      *nowPlaying
	volume float32
	// before is the volume before a mute, for the speaker to bring back.
	before float32
	shown  *anim.Float
	hover  *anim.Float
	accent *anim.Color
	held   bool
	size   geom.Size
}

func newVolumeBar(n *nowPlaying) *volumeBar {
	v := &volumeBar{n: n, shown: anim.NewFloat(0.8), hover: anim.NewFloat(0), accent: anim.NewColor(neutral), before: 0.8}
	v.Add(v.shown, v.hover, v.accent)
	return v
}

func (v *volumeBar) show(vol float32, accent color.NRGBA) {
	v.volume = vol
	if !v.held {
		v.shown.Animate(vol, anim.Snappy)
	}
	v.accent.Animate(accent, anim.Spring{Response: 0.8, Damping: 1})
}

// track returns where the line runs.
func (v *volumeBar) track() (x0, x1 float32) { return 40, v.size.W - 8 }

// DragsTouch implements [gunim.TouchDragger].
func (v *volumeBar) DragsTouch() bool { return true }

// Handle implements [gunim.Handler].
func (v *volumeBar) Handle(e input.Event, u *gunim.UI) bool {
	x0, x1 := v.track()
	at := func(p geom.Point) float32 { return min(max((p.X-x0)/(x1-x0), 0), 1) }
	switch e := e.(type) {
	case input.PointerEnter:
		v.hover.Animate(1, anim.Snappy)
	case input.PointerLeave:
		if !v.held {
			v.hover.Animate(0, anim.Gentle)
		}
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		if e.Pos.X < x0-8 {
			// The speaker mutes, and brings the volume back.
			to := float32(0)
			if v.volume == 0 {
				to = max(v.before, 0.1)
			} else {
				v.before = v.volume
			}
			u.Send(v, SetVolume{Volume: to})
			return true
		}
		v.held = true
		v.shown.Jump(at(e.Pos))
		u.Send(v, SetVolume{Volume: at(e.Pos)})
	case input.PointerMove:
		if !v.held {
			return false
		}
		v.shown.Jump(at(e.Pos))
		u.Send(v, SetVolume{Volume: at(e.Pos)})
	case input.PointerUp:
		if !v.held {
			return false
		}
		v.held = false
		if e.Pos.Y < 0 || e.Pos.Y > v.size.H {
			v.hover.Animate(0, anim.Gentle)
		}
	default:
		return false
	}
	u.Invalidate()
	return true
}

// Layout implements [gunim.Node].
func (v *volumeBar) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	v.size = c.Max
	return c.Max
}

// Paint implements [gunim.Node].
func (v *volumeBar) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	vol := v.shown.Value()
	ic := icon.Volume2
	switch {
	case v.volume == 0:
		ic = icon.VolumeX
	case v.volume < 0.4:
		ic = icon.Volume1
	}
	mid := box.H / 2
	widget.PaintIcon(p, f.Theme, ic, geom.Rc(4, mid-10, 20, 20), faded(ink, 0.7))
	x0, x1 := v.track()
	h := 4 + 2*v.hover.Value()
	p.RRect(geom.Rc(x0, mid-h/2, x1-x0, h), h/2, paint.Solid(faded(ink, 0.15)))
	p.RRect(geom.Rc(x0, mid-h/2, (x1-x0)*vol, h), h/2, paint.Solid(v.accent.Value()))
	k := 5 + 3*v.hover.Value()
	kx := x0 + (x1-x0)*vol
	p.RRect(geom.Rc(kx-k, mid-k, 2*k, 2*k), k, paint.Solid(ink))
}
