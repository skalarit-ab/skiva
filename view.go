package main

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
)

// registerViews is the window half: the player's view, drawing from d
// as it plays.
func registerViews(w *gunim.Window, d *deck, openLibrary bool, list string, eqOpen, infoOpen bool) {
	gunim.RegisterView(w, "player",
		func(Player) *playerRoot {
			r := newPlayerRoot(d)
			if openLibrary {
				r.sheet.Jump(1)
			}
			r.lib.want = list
			if eqOpen {
				r.eq.open.Jump(1)
			}
			if infoOpen {
				r.info.open.Jump(1)
			}
			return r
		},
		func(r *playerRoot, s Player, u *gunim.UI) { r.show(s, u) })
}

// Colours the player keeps whatever plays.
var (
	ink     = color.NRGBA{R: 0xf4, G: 0xf5, B: 0xfa, A: 0xff}
	night   = color.NRGBA{R: 0x0b, G: 0x0c, B: 0x12, A: 0xff}
	neutral = color.NRGBA{R: 0x9a, G: 0xa4, B: 0xc8, A: 0xff}
	// hot marks the volume past full, where loud tracks reach the
	// limiter.
	hot = color.NRGBA{R: 0xff, G: 0x7a, B: 0x45, A: 0xff}
)

// faded is c at alpha a, from 0 to 1.
func faded(c color.NRGBA, a float32) color.NRGBA {
	c.A = uint8(float32(c.A) * min(max(a, 0), 1))
	return c
}

// mix blends a toward b by t, as anim does.
func mix(a, b color.NRGBA, t float32) color.NRGBA { return anim.Mix(anim.ColorCodec, a, b, t) }

// runs holds text shaped already, for nodes that paint every frame.
// It is reached from the UI goroutine alone.
var runs = map[runKey]text.Run{}

type runKey struct {
	s    string
	size float32
	bold bool
}

// shaped is s shaped at size, from the cache.
func shaped(s string, size float32, bold bool) text.Run {
	k := runKey{s, size, bold}
	if r, ok := runs[k]; ok {
		return r
	}
	if len(runs) > 4000 {
		clear(runs)
	}
	r := text.GoSans(bold, false).Shape(s, size)
	runs[k] = r
	return r
}

// clock writes d as minutes and seconds.
func clock(d time.Duration) string {
	s := int(max(d, 0).Round(time.Second) / time.Second)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// meter is the sound as the visuals show it: each band rises at once
// and falls back slower, so the bars move like a meter's needles.
type meter struct {
	d      *deck
	bands  [bandCount]float32
	raw    [bandCount]float32
	level  float32
	bass   float32
	active bool
}

// update measures what is heard now, and eases toward it.
func (m *meter) update(dt time.Duration, playing bool) bool {
	var lvl float32
	if playing {
		lvl = m.d.measure(m.raw[:])
	} else {
		clear(m.raw[:])
	}
	sec := float32(dt.Seconds())
	ease := func(v *float32, to float32) {
		rate := float32(18) // rising
		if to < *v {
			rate = 5 // falling
		}
		*v += (to - *v) * min(1, rate*sec)
		if *v < 0.002 {
			*v = 0
		}
	}
	moving := false
	for i := range m.bands {
		ease(&m.bands[i], m.raw[i])
		moving = moving || m.bands[i] > 0
	}
	ease(&m.level, lvl)
	// The bass is the lowest bands', for things that pulse with the
	// beat.
	b := (m.raw[0] + m.raw[1] + m.raw[2] + m.raw[3]) / 4
	ease(&m.bass, b*b)
	m.active = playing || moving || m.level > 0
	return m.active
}

// playerRoot is the whole window: the background, the library, and
// the track playing. Under narrowWidth the track playing fills the
// window and the library slides up over it as a sheet.
type playerRoot struct {
	anim.Group
	state Player
	meter *meter
	bg    *background
	now   *nowPlaying
	lib   *library
	// sheet opens the library over a narrow window, 0 shut and 1 open;
	// listButton opens it.
	sheet      *anim.Float
	listButton *iconButton
	// eq is the equalizer, over the track playing, and eqButton opens
	// it.
	eq       *eqPanel
	eqButton *iconButton
	// resumed is the last of the application's Resumed taken.
	resumed int
	// info tells about the track playing, and infoButton opens it.
	info       *infoCard
	infoButton *iconButton
	narrow     bool
	size       geom.Size
}

// narrowWidth is the width under which the library becomes a sheet.
const narrowWidth = 820

// sideWidth is the library's width beside the track playing.
const sideWidth = 340

func newPlayerRoot(d *deck) *playerRoot {
	r := &playerRoot{meter: &meter{d: d}, sheet: anim.NewFloat(0)}
	r.Add(r.sheet)
	r.bg = newBackground(r.meter)
	r.now = newNowPlaying(r, d)
	r.lib = newLibrary(r)
	r.listButton = newIconButton(icon.ListMusic, 40, func(u *gunim.UI) { r.openSheet(r.sheet.Target() < 0.5, u) })
	r.eq = newEQPanel(r)
	r.eqButton = newIconButton(icon.SlidersHorizontal, 40, func(u *gunim.UI) {
		if r.info.shown() {
			r.info.show(false, u)
		}
		if r.narrow && r.sheet.Target() > 0.5 {
			r.openSheet(false, u)
		}
		r.eq.show(!r.eq.shown(), u)
	})
	r.info = newInfoCard(r)
	r.infoButton = newIconButton(icon.Info, 40, func(u *gunim.UI) { r.info.show(!r.info.shown(), u) })
	return r
}

// track returns the track playing, or false.
func (r *playerRoot) track() (Track, bool) {
	for _, t := range r.state.Tracks {
		if t.ID == r.state.Current {
			return t, true
		}
	}
	return Track{}, false
}

// show takes the application's state.
func (r *playerRoot) show(s Player, u *gunim.UI) {
	was := r.state
	r.state = s
	t, ok := r.track()
	if !ok {
		t = Track{Accent: neutral, Glow: neutral}
	}
	r.bg.show(t)
	r.now.show(was, s, t)
	r.lib.show(s, u)
	r.eq.take(s.EQ)
	if s.Resumed != r.resumed {
		// The player took up its last run's track: the list it played
		// from opens, as it was.
		r.resumed = s.Resumed
		r.lib.openList(s.From, u)
		r.lib.page.Jump(1)
	}
	r.eqButton.setLit(len(s.EQ.Bands) > 0 && !s.EQ.Bypass)
	u.Invalidate()
}

func (r *playerRoot) openSheet(on bool, u *gunim.UI) {
	to := float32(0)
	if on {
		to = 1
		// The library takes the window: the track's card and the
		// equalizer make way.
		if r.info.shown() {
			r.info.show(false, u)
		}
		if r.eq.shown() {
			r.eq.show(false, u)
		}
	}
	r.sheet.Animate(to, anim.Spring{Response: 0.42, Damping: 0.86})
	u.Invalidate()
}

// Step implements [gunim.Animator]: the meter measures with each frame
// while anything plays.
func (r *playerRoot) Step(dt time.Duration) bool {
	moving := r.Group.Step(dt)
	if r.meter.update(dt, r.state.Playing) {
		moving = true
	}
	return moving
}

// Children implements [gunim.Composite].
func (r *playerRoot) Children() []gunim.Node {
	return []gunim.Node{r.bg, r.now, r.lib, r.listButton, r.eqButton, r.eq, r.infoButton, r.info}
}

// Focusable implements [gunim.Focusable]: the player's keys come here.
func (r *playerRoot) Focusable() bool { return true }

// Layout implements [gunim.Node].
func (r *playerRoot) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	r.size = size
	r.narrow = size.W < narrowWidth
	bg, now, lib, btn := kids.At(0), kids.At(1), kids.At(2), kids.At(3)
	eqBtn, eq := kids.At(4), kids.At(5)
	defer func() {
		// The equalizer rises from the bottom over the track playing,
		// over the whole window where it is narrow, up to the row of
		// buttons along the top, which stays.
		eqArea := geom.Rect{Max: size.Point()}
		safe := eqArea.Inset(f.Safe)
		eqArea.Min.Y = safe.Min.Y + 64
		btnAt := geom.Pt(safe.Max.X-56, safe.Min.Y+16)
		if r.narrow {
			btnAt.X -= 48
		} else {
			eqArea.Min.X = sideWidth + f.Safe.Left
		}
		eqBtn.Layout(gunim.Tight(geom.Sz(40, 40)))
		eqBtn.Place(btnAt)
		// The track's card unfolds from its button, beside the
		// equalizer's, under it, at the window's right.
		infoAt := btnAt.Sub(geom.Pt(48, 0))
		kids.At(6).Layout(gunim.Tight(geom.Sz(40, 40)))
		kids.At(6).Place(infoAt)
		card := kids.At(7).Layout(gunim.Loose(geom.Sz(min(infoW, safe.Size().W-24), safe.Size().H)))
		cardAt := geom.Pt(max(safe.Min.X+12, btnAt.X+40-card.W), btnAt.Y+52)
		r.info.from = infoAt.Add(geom.Pt(20, 20)).Sub(cardAt)
		if r.info.open.Value() < 0.01 {
			cardAt = geom.Pt(-10000, 0)
		}
		kids.At(7).Place(cardAt)
		open := r.eq.open.Value()
		eq.Layout(gunim.Tight(eqArea.Size()))
		if open < 0.001 {
			eq.Place(geom.Pt(-10000, 0))
		} else {
			eq.Place(geom.Pt(eqArea.Min.X, eqArea.Min.Y+(1-open)*eqArea.Size().H))
		}
	}()
	// The background, and the library's glass, run under a phone's
	// bars; the track playing and the buttons keep clear of them.
	bg.Layout(gunim.Tight(size))
	bg.Place(geom.Point{})
	area := geom.Rect{Max: size.Point()}.Inset(f.Safe)
	if r.narrow {
		now.Layout(gunim.Tight(area.Size()))
		now.Place(area.Min)
		h := size.H * 0.86
		lib.Layout(gunim.Tight(geom.Sz(size.W, h)))
		lib.Place(geom.Pt(0, size.H-h*r.sheet.Value()))
		btn.Layout(gunim.Tight(geom.Sz(40, 40)))
		btn.Place(geom.Pt(area.Max.X-56, area.Min.Y+16))
	} else {
		libW := sideWidth + f.Safe.Left
		lib.Layout(gunim.Tight(geom.Sz(libW, size.H)))
		lib.Place(geom.Point{})
		now.Layout(gunim.Tight(geom.Sz(area.Max.X-libW, area.Size().H)))
		now.Place(geom.Pt(libW, area.Min.Y))
		btn.Layout(gunim.Tight(geom.Size{}))
		btn.Place(geom.Pt(-100, -100))
	}
	return size
}

// Paint implements [gunim.Node].
// Paint implements [gunim.Node]: the track playing, the library over
// it or beside it, the equalizer over both, then the row of buttons,
// over everything but the track's card.
func (r *playerRoot) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
	kids.At(1).Paint(p)
	if s := r.sheet.Value(); !r.narrow {
		kids.At(2).Paint(p)
	} else if s > 0.001 {
		// The window dims under the sheet as it rises.
		p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(faded(night, 0.55*min(s, 1))))
		kids.At(2).Paint(p)
	}
	if open := r.eq.open.Value(); open > 0.001 {
		p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(faded(night, 0.35*min(open, 1))))
		kids.At(5).Paint(p)
	}
	if r.narrow {
		kids.At(3).Paint(p)
	}
	kids.At(4).Paint(p)
	kids.At(6).Paint(p)
	kids.At(7).Paint(p)
}

// Handle implements [gunim.Handler]: the player's keys, and a tap
// above the open sheet shuts it.
func (r *playerRoot) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerDown:
		if r.info.shown() {
			// A press anywhere but the card puts it away.
			r.info.show(false, u)
			return true
		}
		if r.narrow && r.sheet.Target() > 0.5 && e.Pos.Y < r.size.H*0.14 {
			r.openSheet(false, u)
			return true
		}
		return false
	case input.KeyPress:
		return r.key(e, u)
	case input.MediaSeek:
		// A seek from the system's media controls.
		u.Send(r, SeekTo{At: e.At})
		return true
	}
	return false
}

// isMedia says whether k is one of the media keys, which work the
// player wherever the focus is.
func isMedia(k input.Key) bool {
	return k == input.KeyMediaPlayPause || k == input.KeyMediaPlay || k == input.KeyMediaPause ||
		k == input.KeyMediaStop || k == input.KeyMediaNext || k == input.KeyMediaPrevious
}

func (r *playerRoot) key(k input.KeyPress, u *gunim.UI) bool {
	// Keys typed into a field, as a playlist's name, are the field's.
	if t, ok := u.Focused().(interface{ TakesText() bool }); ok && t.TakesText() && !isMedia(k.Key) {
		return false
	}
	at, _ := r.now.d.position()
	switch k.Key {
	case input.KeySpace, input.KeyK, input.KeyMediaPlayPause:
		u.Send(r, TogglePlay{})
	case input.KeyMediaPlay:
		if !r.state.Playing {
			u.Send(r, TogglePlay{})
		}
	case input.KeyMediaPause, input.KeyMediaStop:
		if r.state.Playing {
			u.Send(r, TogglePlay{})
		}
	case input.KeyMediaNext:
		u.Send(r, Skip{})
	case input.KeyMediaPrevious:
		u.Send(r, Skip{Back: true})
	case input.KeyRight, input.KeyL:
		u.Send(r, SeekTo{At: at + 5*time.Second})
	case input.KeyLeft, input.KeyJ:
		u.Send(r, SeekTo{At: max(0, at-5*time.Second)})
	case input.KeyUp:
		u.Send(r, SetVolume{Volume: r.state.Volume + 0.05})
	case input.KeyDown:
		u.Send(r, SetVolume{Volume: r.state.Volume - 0.05})
	case input.KeyN:
		u.Send(r, Skip{})
	case input.KeyP, input.KeyB:
		u.Send(r, Skip{Back: true})
	case input.KeyS:
		u.Send(r, ToggleShuffle{})
	case input.KeyR:
		u.Send(r, CycleRepeat{})
	case input.KeyDelete, input.KeyBackspace:
		if r.eq.shown() && r.eq.graph.sel != 0 {
			r.eq.graph.remove(r.eq.graph.sel, u)
			return true
		}
		return false
	case input.KeyE:
		r.eq.show(!r.eq.shown(), u)
	case input.KeyI:
		r.info.show(!r.info.shown(), u)
	case input.KeyEscape:
		if r.info.shown() {
			r.info.show(false, u)
			return true
		}
		if r.eq.shown() {
			r.eq.show(false, u)
			return true
		}
		if r.narrow && r.sheet.Target() > 0.5 {
			r.openSheet(false, u)
			return true
		}
		return false
	default:
		return false
	}
	return true
}

// background is the window's backdrop: a dark field with three great
// soft lights in the track's colours drifting about it, swelling with
// the bass.
type background struct {
	anim.Group
	meter        *meter
	accent, glow *anim.Color
	// drift turns the lights about, faster while the music plays.
	phase float64
	speed *anim.Float
}

func newBackground(m *meter) *background {
	b := &background{meter: m, accent: anim.NewColor(neutral), glow: anim.NewColor(neutral), speed: anim.NewFloat(0)}
	b.Add(b.accent, b.glow, b.speed)
	return b
}

func (b *background) show(t Track) {
	slow := anim.Spring{Response: 1.4, Damping: 1}
	if b.accent.Target() != t.Accent {
		b.accent.Animate(t.Accent, slow)
		b.glow.Animate(t.Glow, slow)
	}
}

// Step implements [gunim.Animator].
func (b *background) Step(dt time.Duration) bool {
	moving := b.Group.Step(dt)
	speed := float32(0)
	if b.meter.active {
		speed = 1
	}
	if b.speed.Target() != speed {
		b.speed.Animate(speed, anim.Spring{Response: 1.5, Damping: 1})
	}
	if v := b.speed.Value(); v > 0.001 {
		b.phase += dt.Seconds() * 0.12 * float64(v)
		moving = true
	}
	return moving
}

// Layout implements [gunim.Node].
func (b *background) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Max
}

// Paint implements [gunim.Node].
func (b *background) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	whole := geom.Rect{Max: box.Point()}
	p.RRect(whole, 0, paint.Solid(night))
	big := max(box.W, box.H)
	swell := 1 + 0.35*b.meter.bass
	lights := []struct {
		c            color.NRGBA
		ax, ay, r, a float32
		ph           float64
	}{
		{b.accent.Value(), 0.3, 0.3, 0.42, 0.55, 0},
		{b.glow.Value(), 0.75, 0.65, 0.38, 0.45, 2.1},
		{mix(b.accent.Value(), b.glow.Value(), 0.5), 0.6, 0.15, 0.3, 0.35, 4.2},
	}
	end := p.Layer(paint.LayerOpts{Bounds: whole, Opacity: 1, Blur: big * 0.08})
	for _, l := range lights {
		x := box.W * (l.ax + 0.12*float32(math.Sin(b.phase+l.ph)))
		y := box.H * (l.ay + 0.1*float32(math.Cos(b.phase*1.3+l.ph)))
		rad := big * l.r * swell
		p.RRect(geom.Rc(x-rad, y-rad, 2*rad, 2*rad), rad, paint.Solid(faded(l.c, l.a)))
	}
	end()
	// A veil over the lights keeps the text above them easy to read.
	p.RRect(whole, 0, paint.Solid(faded(night, 0.45)))
}
