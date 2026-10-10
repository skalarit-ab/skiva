package main

import (
	"image/color"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/install"
	"github.com/marrasen/gunim/paint"
)

// settingsCard is Skiva's settings, opened from the library's menu over
// the middle of the window. On a desktop it sets how the installed
// Skiva takes updates, and whether betas come too; on a phone it says
// the app store updates Skiva.
type settingsCard struct {
	anim.Group
	root *playerRoot
	open *anim.Float
	// mode picks how updates come, beta whether betas come too, and
	// check opens the window about Skiva, to look for one now.
	mode, beta *segments
	check      *textButton
	size       geom.Size
}

// modeNames name the update modes, as the card shows them, in the order
// modeValues has them.
var (
	modeNames  = []string{"Automatic", "Ask first", "Off"}
	modeValues = []install.UpdateMode{install.UpdatesInstall, install.UpdatesNotify, install.UpdatesOff}
)

// modeHints say what each update mode does.
var modeHints = []string{
	"Updates take effect on the next start.",
	"Asks before fetching a new release.",
	"Never looks for new releases.",
}

func newSettingsCard(r *playerRoot) *settingsCard {
	c := &settingsCard{root: r, open: anim.NewFloat(0)}
	c.Add(c.open)
	c.mode = newSegments(modeNames, func(i int, u *gunim.UI) { u.Send(c, SetUpdateMode{Mode: string(modeValues[i])}) })
	c.beta = newSegments([]string{"Releases", "Betas too"}, func(i int, u *gunim.UI) { u.Send(c, SetBeta{On: i == 1}) })
	c.check = newTextButton("Check for updates…", func(u *gunim.UI) { u.Send(c, ShowAbout{}) })
	return c
}

func (c *settingsCard) shown() bool { return c.open.Target() > 0.5 }

func (c *settingsCard) show(on bool, u *gunim.UI) {
	to := float32(0)
	if on {
		to = 1
		u.Cue(gunim.CueOpen, c)
	}
	c.open.Animate(to, anim.Spring{Response: 0.38, Damping: 0.8})
	u.Invalidate()
}

// take takes the application's settings.
func (c *settingsCard) take(s Settings) {
	at := 0
	for i, m := range modeValues {
		if string(m) == s.Mode {
			at = i
		}
	}
	c.mode.set(at)
	c.beta.set(map[bool]int{false: 0, true: 1}[s.Beta])
}

// settingsW is the card's width.
const settingsW = 440

// heading is what the card says under its title: the version.
func (c *settingsCard) heading() string {
	v := c.root.state.Settings.Version
	if !install.IsRelease(v) {
		return appName + ", built from source"
	}
	return appName + " " + v
}

// note is what the card says in place of the update settings, where
// there are none: on a phone, and in a copy that is not installed.
func (c *settingsCard) note() string {
	switch c.root.state.Settings.Updates {
	case UpdatesFromStore:
		return "Google Play keeps Skiva up to date."
	case UpdatesNone:
		return "This copy is not installed, so it takes no updates."
	case UpdatesHere:
	}
	return ""
}

// rows are the y of each row, under the heading.
const (
	modeY  = 84
	hintY  = modeY + 40
	betaY  = hintY + 30
	checkY = betaY + 48
)

// Children implements [gunim.Composite].
func (c *settingsCard) Children() []gunim.Node { return []gunim.Node{c.mode, c.beta, c.check} }

// Layout implements [gunim.Node].
func (c *settingsCard) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	h := float32(modeY + 24 + 20)
	here := c.root.state.Settings.Updates == UpdatesHere
	if here {
		h = checkY + 32 + 22
	}
	c.size = cs.Constrain(geom.Sz(min(settingsW, cs.Max.W), h))
	room := c.size.W - labelX - 20
	for i, y := range []float32{modeY, betaY, checkY} {
		k := kids.At(i)
		if !here {
			k.Layout(gunim.Tight(geom.Size{}))
			k.Place(geom.Pt(-10000, 0))
			continue
		}
		w := room
		if i == 2 {
			w = min(room, c.check.width())
		}
		k.Layout(gunim.Tight(geom.Sz(w, 32)))
		k.Place(geom.Pt(labelX, y))
	}
	return c.size
}

// labelX is where the controls start, right of their labels.
const labelX = 110

// Handle implements [gunim.Handler]: a press on the card is the card's,
// and leaves it open.
func (c *settingsCard) Handle(e input.Event, _ *gunim.UI) bool {
	switch e.(type) {
	case input.PointerDown, input.PointerUp:
		return c.shown()
	}
	return false
}

// Step implements [gunim.Animator].
func (c *settingsCard) Step(dt time.Duration) bool { return c.Group.Step(dt) }

// Paint implements [gunim.Node]: the card grows out of the middle of
// the window, fading in, as the track's card grows from its button.
func (c *settingsCard) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, kids gunim.Children) {
	open := c.open.Value()
	if open < 0.01 {
		return
	}
	defer p.Push(paint.Scale(0.85+0.15*min(open, 1.1), geom.Pt(box.W/2, box.H/2)))()
	alpha := min(open, 1)
	whole := geom.Rect{Max: box.Point()}
	end := p.Layer(paint.LayerOpts{Bounds: whole, Opacity: alpha, Backdrop: 28, Clip: true, Radius: 18})
	p.RRect(whole, 18, paint.Solid(faded(night, 0.86)))
	end()
	end = p.Layer(paint.LayerOpts{Bounds: whole, Opacity: alpha})
	defer end()
	t, ok := c.root.track()
	accent := neutral
	if ok {
		accent = t.Accent
	}
	p.RRectStroke(whole, 18, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 1, Color: faded(accent, 0.5)})
	paintFit(p, "Settings", 16, true, geom.Pt(20, 22), box.W-38, ink)
	paintFit(p, c.heading(), 12, false, geom.Pt(20, 46), box.W-38, faded(ink, 0.55))
	if note := c.note(); note != "" {
		paintFit(p, note, 13, false, geom.Pt(20, modeY), box.W-38, faded(ink, 0.85))
		return
	}
	label := func(s string, y float32) {
		shaped(s, 11, true).Paint(p, geom.Pt(20, y+9), faded(accent, 0.9))
	}
	label("Updates", modeY)
	paintFit(p, modeHints[c.mode.at], 12, false, geom.Pt(labelX, hintY), box.W-labelX-20, faded(ink, 0.55))
	label("Channel", betaY)
	c.mode.accent, c.beta.accent, c.check.accent = accent, accent, accent
	for i := range 3 {
		kids.At(i).Paint(p)
	}
}

// segments picks one of a few choices, side by side in a pill, the one
// picked lit in the track's colour, which slides to the choice picked.
type segments struct {
	anim.Group
	names []string
	at    int
	pick  func(i int, u *gunim.UI)
	// slide is where the lit part is, as a choice's index, and hover
	// how lit the choice under the pointer, over, is.
	slide, hover *anim.Float
	over, held   int
	accent       color.NRGBA
	size         geom.Size
}

func newSegments(names []string, pick func(i int, u *gunim.UI)) *segments {
	s := &segments{names: names, pick: pick, slide: anim.NewFloat(0), hover: anim.NewFloat(0), over: -1, held: -1, accent: neutral}
	s.Add(s.slide, s.hover)
	return s
}

// set lights choice i, sliding there.
func (s *segments) set(i int) {
	if i == s.at {
		return
	}
	s.at = i
	s.slide.Animate(float32(i), anim.Spring{Response: 0.32, Damping: 0.82})
}

// under returns the choice at p, or -1.
func (s *segments) under(p geom.Point) int {
	if len(s.names) == 0 || !(geom.Rect{Max: s.size.Point()}).Contains(p) {
		return -1
	}
	return min(int(p.X/(s.size.W/float32(len(s.names)))), len(s.names)-1)
}

// Focusable implements [gunim.Focusable]: the player's keys stay with
// the window.
func (s *segments) Focusable() bool { return false }

// Handle implements [gunim.Handler].
func (s *segments) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerEnter:
		s.over = s.under(e.Pos)
		s.hover.Animate(1, anim.Snappy)
	case input.PointerMove:
		s.over = s.under(e.Pos)
	case input.PointerLeave:
		s.hover.Animate(0, anim.Gentle)
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		s.held = s.under(e.Pos)
	case input.PointerUp:
		i := s.under(e.Pos)
		held := s.held
		s.held = -1
		if i < 0 || i != held || i == s.at {
			return held >= 0
		}
		u.Cue(gunim.CueTick, s)
		s.set(i)
		s.pick(i, u)
	default:
		return false
	}
	u.Invalidate()
	return true
}

// Layout implements [gunim.Node].
func (s *segments) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	s.size = c.Max
	return c.Max
}

// Paint implements [gunim.Node].
func (s *segments) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	if box.W <= 0 || len(s.names) == 0 {
		return
	}
	r := box.H / 2
	p.RRect(geom.Rect{Max: box.Point()}, r, paint.Solid(faded(ink, 0.06)))
	w := box.W / float32(len(s.names))
	if s.over >= 0 && s.over != s.at {
		p.RRect(geom.Rc(float32(s.over)*w, 0, w, box.H), r, paint.Solid(faded(ink, 0.06*s.hover.Value())))
	}
	lit := geom.Rc(s.slide.Value()*w, 0, w, box.H)
	p.RRect(lit, r, paint.Solid(faded(s.accent, 0.22)))
	p.RRectStroke(lit, r, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 1, Color: faded(s.accent, 0.6)})
	for i, name := range s.names {
		c := faded(ink, 0.65)
		if i == s.at {
			c = mix(ink, s.accent, 0.5)
		}
		run := shaped(name, 12, true)
		run.Paint(p, geom.Pt(float32(i)*w+(w-run.Advance)/2, (box.H-15)/2), c)
	}
}

// textButton is a pill with words on it, which does press as it is
// clicked.
type textButton struct {
	anim.Group
	label       string
	press       func(*gunim.UI)
	hover, down *anim.Float
	held        bool
	accent      color.NRGBA
	size        geom.Size
}

func newTextButton(label string, press func(*gunim.UI)) *textButton {
	b := &textButton{label: label, press: press, hover: anim.NewFloat(0), down: anim.NewFloat(0), accent: neutral}
	b.Add(b.hover, b.down)
	return b
}

// width is as wide as the button wants to be, for its words.
func (b *textButton) width() float32 { return shaped(b.label, 12, true).Advance + 36 }

// Focusable implements [gunim.Focusable].
func (b *textButton) Focusable() bool { return false }

// Handle implements [gunim.Handler].
func (b *textButton) Handle(e input.Event, u *gunim.UI) bool {
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
		if (geom.Rect{Max: b.size.Point()}).Contains(e.Pos) {
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
func (b *textButton) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	b.size = c.Max
	return c.Max
}

// Paint implements [gunim.Node].
func (b *textButton) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	if box.W <= 0 {
		return
	}
	mid := geom.Pt(box.W/2, box.H/2)
	defer p.Push(paint.Scale(1-0.06*b.down.Value(), mid))()
	hover := b.hover.Value()
	p.RRect(geom.Rect{Max: box.Point()}, box.H/2, paint.Solid(faded(mix(ink, b.accent, hover), 0.1+0.08*hover)))
	run := shaped(b.label, 12, true)
	run.Paint(p, geom.Pt((box.W-run.Advance)/2, (box.H-15)/2), mix(faded(ink, 0.85), b.accent, 0.4*hover))
}
