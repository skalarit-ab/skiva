package main

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// The equalizer's view: frequencies from 20 Hz to 20 kHz across, on a
// scale of octaves, and gains of eqRange decibels each way, up and
// down. Behind the bands, the sound's spectrum before the equalizer
// and after it.
const (
	eqLow, eqHigh = 20.0, 20000.0
	eqRange       = 18
	maxBands      = 8
	specPoints    = 240
	// specTilt lifts the spectrum this many decibels an octave above
	// 1 kHz, and lowers it below, so music, which has less in its
	// higher pitches, reads about level, as analyzers in studios show
	// it.
	specTilt = 4.5
	// specTop and specBottom are the levels the spectrum's view runs
	// between, after the tilt.
	specTop, specBottom = 0, -96
)

// bandColors gives each band a colour of its own, by its place.
var bandColors = []color.NRGBA{
	rgb(0xff, 0x8a, 0x5c), rgb(0xff, 0xc8, 0x57), rgb(0x9b, 0xe5, 0x6a), rgb(0x4f, 0xd6, 0xc0),
	rgb(0x5c, 0xb8, 0xff), rgb(0x8f, 0x8c, 0xff), rgb(0xd0, 0x7c, 0xff), rgb(0xff, 0x6f, 0xb4),
}

// kindNames names the kinds of band, in their order.
var kindNames = []string{"Bell", "Low shelf", "High shelf", "Low cut", "High cut", "Notch"}

// isCut says whether a band of kind k cuts, and so has a slope and
// no gain.
func isCut(k audio.FilterKind) bool { return k == audio.LowCut || k == audio.HighCut }

// hasGain says whether a band of kind k lifts and lowers.
func hasGain(k audio.FilterKind) bool {
	return k == audio.Bell || k == audio.LowShelf || k == audio.HighShelf
}

// hzText writes a frequency as a studio does: 80 Hz, 1.25 kHz.
func hzText(hz float32) string {
	switch {
	case hz >= 10000:
		return fmt.Sprintf("%.1f kHz", hz/1000)
	case hz >= 1000:
		return fmt.Sprintf("%.2f kHz", hz/1000)
	}
	return fmt.Sprintf("%.0f Hz", hz)
}

// bandText describes a band's settings in one line.
func bandText(b audio.Band) string {
	switch {
	case isCut(b.Kind):
		return fmt.Sprintf("%s  ·  %d dB/oct", hzText(b.Freq), max(b.Slope, 12))
	case b.Kind == audio.Notch:
		return fmt.Sprintf("%s  ·  Q %.2f", hzText(b.Freq), b.Q)
	}
	return fmt.Sprintf("%s  ·  %+.1f dB  ·  Q %.2f", hzText(b.Freq), b.Gain, b.Q)
}

// eqPanel is the equalizer, over the track playing: a heading with
// its buttons, the graph, and under it the band picked. It rises from
// the bottom as it opens.
type eqPanel struct {
	anim.Group
	root  *playerRoot
	open  *anim.Float
	graph *eqGraph
	strip *eqStrip
	// graphMenu is the graph with its points' menu.
	graphMenu *widget.ContextMenu
	close     *iconButton
	power     *iconButton
	presets   *iconButton
	menu      *widget.ContextMenu
	picks     []func(*gunim.UI)
	at        geom.Point
	size      geom.Size
}

func newEQPanel(r *playerRoot) *eqPanel {
	e := &eqPanel{root: r, open: anim.NewFloat(0)}
	e.Add(e.open)
	e.graph = newEQGraph(e)
	e.strip = &eqStrip{eq: e, slotOf: map[int]geom.Rect{}, held: -1, hot: -1}
	e.graphMenu = widget.NewContextMenu(e.graph, nil)
	e.graphMenu.Prepare = e.graph.prepare
	e.graphMenu.Picked = func(i int, u *gunim.UI) {
		if g := e.graph; i >= 0 && i < len(g.picks) && g.picks[i] != nil {
			g.picks[i](u)
		}
	}
	e.graph.menu = e.graphMenu
	e.close = newIconButton(icon.X, 36, func(u *gunim.UI) { e.show(false, u) })
	e.power = newIconButton(icon.Power, 36, func(u *gunim.UI) {
		e.graph.bypass = !e.graph.bypass
		e.power.setLit(!e.graph.bypass)
		e.graph.send(u)
	})
	e.power.setLit(true)
	e.presets = newIconButton(icon.Sparkles, 36, func(u *gunim.UI) { e.openPresets(u) })
	e.menu = widget.NewContextMenu(e.presets, nil)
	e.menu.Picked = func(i int, u *gunim.UI) {
		if i >= 0 && i < len(e.picks) && e.picks[i] != nil {
			e.picks[i](u)
		}
	}
	return e
}

// shown says whether the panel is open, or opening.
func (e *eqPanel) shown() bool { return e.open.Target() > 0.5 }

// show opens the panel, or shuts it.
func (e *eqPanel) show(on bool, u *gunim.UI) {
	to := float32(0)
	if on {
		to = 1
		u.Cue(gunim.CueOpen, e)
	} else {
		u.Cue(gunim.CueClose, e)
	}
	e.open.Animate(to, anim.Spring{Response: 0.42, Damping: 0.86})
	u.Invalidate()
}

// take takes the equalizer's settings from the application.
func (e *eqPanel) take(eq EQ) {
	e.graph.take(eq)
	e.power.setLit(!e.graph.bypass)
}

// openPresets opens the presets' menu under their button.
func (e *eqPanel) openPresets(u *gunim.UI) {
	var items menuItems
	for _, p := range presets {
		bands := p.bands
		items.add(p.name, nil, func(u *gunim.UI) { e.graph.preset(bands, u) })
	}
	items.set(e.menu)
	e.picks = items.do
	e.menu.Open(geom.Pt(0, 40), u)
}

// headH is the height of the panel's heading, and stripH of the strip
// under the graph, which takes two rows on a narrow window.
const (
	eqHeadH  = 64
	eqStripH = 76
)

// Children implements [gunim.Composite].
func (e *eqPanel) Children() []gunim.Node {
	return []gunim.Node{e.graphMenu, e.strip, e.close, e.power, e.menu}
}

// Layout implements [gunim.Node].
func (e *eqPanel) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	e.size = size
	area := geom.Rect{Max: size.Point()}.Inset(f.Safe)
	e.at = area.Min
	stripH := float32(eqStripH)
	if area.Size().W < 640 {
		stripH = 2*eqStripH - 16
	}
	graph := geom.Rect{Min: area.Min.Add(geom.Pt(16, eqHeadH)), Max: area.Max.Sub(geom.Pt(16, stripH+12))}
	kids.At(0).Layout(gunim.Tight(graph.Size()))
	kids.At(0).Place(graph.Min)
	kids.At(1).Layout(gunim.Tight(geom.Sz(area.Size().W-32, stripH)))
	kids.At(1).Place(geom.Pt(area.Min.X+16, area.Max.Y-stripH-4))
	for i, k := range []int{2, 3, 4} {
		kids.At(k).Layout(gunim.Tight(geom.Sz(36, 36)))
		kids.At(k).Place(geom.Pt(area.Max.X-52-float32(i)*44, area.Min.Y+16))
	}
	return size
}

// Paint implements [gunim.Node]: dark frosted glass, and the heading.
func (e *eqPanel) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, kids gunim.Children) {
	whole := geom.Rect{Max: box.Point()}
	end := p.Layer(paint.LayerOpts{Bounds: whole, Opacity: 1, Backdrop: 32, Clip: true, Radius: 24})
	p.RRect(whole, 24, paint.Solid(faded(night, 0.78)))
	end()
	p.RRectStroke(whole, 24, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 1, Color: faded(ink, 0.08)})
	shaped("Equalizer", 22, true).Paint(p, e.at.Add(geom.Pt(24, 18)), ink)
	n := len(e.graph.bands)
	what := fmt.Sprintf("%d bands", n)
	switch n {
	case 0:
		what = "No bands: the sound plays as recorded"
	case 1:
		what = "1 band"
	}
	if e.graph.bypass {
		what += " · bypassed"
	}
	if h := e.root.state.Headroom; h > 0.05 && !e.graph.bypass {
		what += fmt.Sprintf(" · %.1f dB lower, so its boosts stay unclipped", h)
	}
	shaped(what, 12, false).Paint(p, e.at.Add(geom.Pt(24, 44)), faded(ink, 0.5))
	for k := range kids.All {
		k.Paint(p)
	}
}

// eqDot is a band as the graph shows it: its settings gliding, how
// far it has grown in, and how lit it is.
type eqDot struct {
	// logHz is the band's frequency as an octave count, gain its gain,
	// logQ its Q as an octave count: each glides to the band's.
	logHz, gain, logQ *anim.Float
	in, on, hot       *anim.Float
	gone              bool
	band              audio.Band
}

// value returns the band as it shows now, mid-glide.
func (d *eqDot) value() audio.Band {
	b := d.band
	b.Freq = float32(math.Exp2(float64(d.logHz.Value())))
	b.Gain = d.gain.Value()
	b.Q = float32(math.Exp2(float64(d.logQ.Value())))
	b.On = true
	return b
}

// weight is how much of the band shows: grown in, and on.
func (d *eqDot) weight() float32 { return min(max(d.in.Value(), 0), 1) * min(max(d.on.Value(), 0), 1) }

// eqGraph draws the equalizer: the grid of frequencies and gains, the
// spectrum before and after it, the curve its bands make together, and
// a point for each band, which drags.
type eqGraph struct {
	anim.Group
	eq     *eqPanel
	bands  []audio.Band
	bypass bool
	dots   map[int]*eqDot
	// sel is the ID of the band picked, and hot of the one under the
	// pointer; zero for none.
	sel, hot int
	// dragging is the band being dragged, and grab where on its point
	// it was taken.
	dragging int
	grab     geom.Point
	nextID   int
	// seq counts the changes sent, and the application's settings
	// older than the last are passed over.
	seq   int
	size  geom.Size
	menu  *widget.ContextMenu
	picks []func(*gunim.UI)
	// freqs are the spectrum's frequencies; heard and before what it
	// measured last, before and after the equalizer, and the smooth
	// ones what it shows, falling slower than they rise.
	freqs             []float32
	heard, before     []float32
	heardSm, beforeSm []float32
	curve             []float32
	bypassAmt         *anim.Float
	// pointer is where the pointer is, for the readout.
	pointer geom.Point
}

func newEQGraph(e *eqPanel) *eqGraph {
	g := &eqGraph{eq: e, dots: map[int]*eqDot{}, nextID: 1, bypassAmt: anim.NewFloat(0)}
	g.Add(g.bypassAmt)
	g.freqs = make([]float32, specPoints)
	for i := range g.freqs {
		g.freqs[i] = float32(eqLow * math.Pow(eqHigh/eqLow, float64(i)/(specPoints-1)))
	}
	g.heard, g.before = make([]float32, specPoints), make([]float32, specPoints)
	g.heardSm, g.beforeSm = make([]float32, specPoints), make([]float32, specPoints)
	for i := range g.heardSm {
		g.heardSm[i], g.beforeSm[i] = specBottom, specBottom
	}
	return g
}

// The graph's scales: x from frequency and back, y from gain and
// back, and y from a level of the spectrum.
func (g *eqGraph) xOf(hz float64) float32 {
	return float32(math.Log(hz/eqLow) / math.Log(eqHigh/eqLow) * float64(g.size.W))
}

func (g *eqGraph) hzAt(x float32) float64 {
	return eqLow * math.Pow(eqHigh/eqLow, float64(max(0, min(x/g.size.W, 1))))
}

// gainH is how far from the middle a gain of eqRange reaches.
func (g *eqGraph) gainH() float32 { return g.size.H/2 - 14 }

func (g *eqGraph) yOf(db float32) float32 { return g.size.H/2 - db/eqRange*g.gainH() }

func (g *eqGraph) dbAt(y float32) float32 { return (g.size.H/2 - y) / g.gainH() * eqRange }

func (g *eqGraph) specY(db float32) float32 {
	t := (db - specBottom) / (specTop - specBottom)
	return g.size.H * (1 - max(0, min(t, 1)))
}

// dotAt returns where band d's point is: at its gain, gliding, which
// for a cut or a notch is 0 dB.
func (g *eqGraph) dotAt(d *eqDot) geom.Point {
	b := d.value()
	return geom.Pt(g.xOf(float64(b.Freq)), g.yOf(b.Gain))
}

// hit returns the band whose point is at p, nearest first, or zero.
func (g *eqGraph) hit(p geom.Point) int {
	best, bestD := 0, float32(16*16)
	for _, b := range g.bands {
		d := g.dots[b.ID]
		q := g.dotAt(d).Sub(p)
		if dd := q.X*q.X + q.Y*q.Y; dd < bestD {
			best, bestD = b.ID, dd
		}
	}
	return best
}

// band returns the band with ID id, and its place, or nil and -1.
func (g *eqGraph) band(id int) (b *audio.Band, place int) {
	for i := range g.bands {
		if g.bands[i].ID == id {
			return &g.bands[i], i
		}
	}
	return nil, -1
}

// dot returns band b's dot, making it, grown in, where it is new.
func (g *eqGraph) dot(b audio.Band, grow bool) *eqDot {
	d := g.dots[b.ID]
	if d == nil {
		d = &eqDot{
			logHz: anim.NewFloat(float32(math.Log2(float64(b.Freq)))), gain: anim.NewFloat(b.Gain),
			logQ: anim.NewFloat(float32(math.Log2(float64(max(b.Q, 0.025))))),
			in:   anim.NewFloat(1), on: anim.NewFloat(1), hot: anim.NewFloat(0),
		}
		if !b.On {
			d.on.Jump(0)
		}
		if grow {
			d.in.Jump(0)
			d.in.Animate(1, anim.Spring{Response: 0.4, Damping: 0.55})
		}
		g.dots[b.ID] = d
		g.Add(d.logHz, d.gain, d.logQ, d.in, d.on, d.hot)
	}
	d.band, d.gone = b, false
	return d
}

// follow glides each dot to its band, at once while dragged, and lets
// the dots of bands gone shrink away.
func (g *eqGraph) follow(springy bool) {
	live := map[int]bool{}
	for _, b := range g.bands {
		live[b.ID] = true
		d := g.dot(b, true)
		to := []float32{float32(math.Log2(float64(b.Freq))), b.Gain, float32(math.Log2(float64(max(b.Q, 0.025))))}
		for i, a := range []*anim.Float{d.logHz, d.gain, d.logQ} {
			if springy {
				a.Animate(to[i], anim.Spring{Response: 0.45, Damping: 0.8})
			} else {
				a.Jump(to[i])
			}
		}
		on := float32(0)
		if b.On {
			on = 1
		}
		d.on.Animate(on, anim.Snappy)
	}
	for id, d := range g.dots {
		if !live[id] && !d.gone {
			d.gone = true
			d.in.Animate(0, anim.Spring{Response: 0.3, Damping: 1})
		}
	}
}

// take takes the application's settings, unless a band is being
// dragged, whose settings are the window's.
func (g *eqGraph) take(eq EQ) {
	if g.dragging != 0 || eq.Seq < g.seq {
		return
	}
	g.bands = slices.Clone(eq.Bands)
	g.bypass = eq.Bypass
	for _, b := range g.bands {
		g.nextID = max(g.nextID, b.ID+1)
	}
	g.follow(true)
	g.bypassAmt.Animate(map[bool]float32{false: 0, true: 1}[g.bypass], anim.Gentle)
}

// send tells the application the settings.
func (g *eqGraph) send(u *gunim.UI) {
	g.bypassAmt.Animate(map[bool]float32{false: 0, true: 1}[g.bypass], anim.Gentle)
	g.seq++
	u.Send(g, SetEQ{EQ: EQ{Bands: slices.Clone(g.bands), Bypass: g.bypass, Seq: g.seq}})
	u.Invalidate()
}

// changed sets the band picked, glides the dots, and tells the
// application.
func (g *eqGraph) changed(springy bool, u *gunim.UI) {
	g.follow(springy)
	g.send(u)
}

// add puts a band at p: a bell, or a cut near either end.
func (g *eqGraph) add(p geom.Point, u *gunim.UI) {
	if len(g.bands) >= maxBands {
		u.Cue(gunim.CueError, g)
		return
	}
	hz := float32(g.hzAt(p.X))
	b := audio.Band{ID: g.nextID, Kind: audio.Bell, Freq: hz, Gain: max(-eqRange, min(g.dbAt(p.Y), eqRange)), Q: 1, On: true}
	switch {
	case hz < 40:
		b.Kind, b.Gain, b.Q, b.Slope = audio.LowCut, 0, 0.707, 24
	case hz > 15000:
		b.Kind, b.Gain, b.Q, b.Slope = audio.HighCut, 0, 0.707, 24
	}
	g.nextID++
	g.bands = append(g.bands, b)
	g.sel = b.ID
	u.Cue(gunim.CueToggleOn, g)
	g.changed(false, u)
}

// remove takes band id away.
func (g *eqGraph) remove(id int, u *gunim.UI) {
	if _, i := g.band(id); i >= 0 {
		g.bands = slices.Delete(g.bands, i, i+1)
		if g.sel == id {
			g.sel = 0
		}
		u.Cue(gunim.CueToggleOff, g)
		g.changed(false, u)
	}
}

// preset puts bands in place of the bands there, each gliding from the
// band at its place, as a point flying to its new home.
func (g *eqGraph) preset(bands []audio.Band, u *gunim.UI) {
	out := make([]audio.Band, len(bands))
	for i, b := range bands {
		if i < len(g.bands) {
			b.ID = g.bands[i].ID
		} else {
			b.ID = g.nextID
			g.nextID++
		}
		b.On = true
		out[i] = b
	}
	g.bands, g.sel, g.bypass = out, 0, false
	g.eq.power.setLit(true)
	g.changed(true, u)
}

// DragsTouch implements [gunim.TouchDragger]: a finger on a point drags
// it.
func (g *eqGraph) DragsTouch() bool { return g.dragging != 0 }

// Handle implements [gunim.Handler].
func (g *eqGraph) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerMove:
		g.pointer = e.Pos
		if g.dragging != 0 {
			g.drag(e.Pos, e.Mods, u)
			break
		}
		g.hover(g.hit(e.Pos))
	case input.PointerLeave:
		if g.dragging == 0 {
			g.hover(0)
		}
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		id := g.hit(e.Pos)
		switch {
		case id != 0 && e.Clicks == 2:
			if b, _ := g.band(id); b != nil {
				b.On = !b.On
				u.Cue(map[bool]gunim.Cue{true: gunim.CueToggleOn, false: gunim.CueToggleOff}[b.On], g)
				g.changed(false, u)
			}
		case id != 0:
			g.sel, g.dragging = id, id
			g.grab = e.Pos.Sub(g.dotAt(g.dots[id]))
			u.Cue(gunim.CueSelect, g)
		case e.Clicks == 2:
			g.add(e.Pos, u)
		default:
			g.sel = 0
		}
	case input.PointerUp:
		if g.dragging != 0 {
			g.dragging = 0
			g.send(u)
		}
	case input.Scroll:
		id := g.hot
		if id == 0 {
			id = g.sel
		}
		b, _ := g.band(id)
		if b == nil {
			return false
		}
		notches := e.Notches.Y
		if notches == 0 {
			notches = e.Delta.Y / 40
		}
		g.widen(b, notches)
		g.sel = id
		g.changed(false, u)
	default:
		return false
	}
	u.Invalidate()
	return true
}

// widen narrows band b by notches of the wheel, or for a cut steepens
// it.
func (g *eqGraph) widen(b *audio.Band, notches float32) {
	if isCut(b.Kind) {
		step := 12
		if notches < 0 {
			step = -12
		}
		b.Slope = max(12, min(max(b.Slope, 12)+step, 48))
		return
	}
	b.Q = float32(max(0.1, min(float64(b.Q)*math.Pow(1.15, float64(notches)), 18)))
}

// drag moves the band being dragged to p: its frequency across, and
// its gain up and down. Shift moves it finely.
func (g *eqGraph) drag(p geom.Point, mods input.Mods, u *gunim.UI) {
	b, _ := g.band(g.dragging)
	if b == nil {
		g.dragging = 0
		return
	}
	at := p.Sub(g.grab)
	if mods.Has(input.ModShift) {
		// A tenth as far: from where the point is, toward the pointer.
		now := g.dotAt(g.dots[b.ID])
		at = now.Add(at.Sub(now).Mul(0.1))
	}
	b.Freq = float32(max(eqLow, min(g.hzAt(at.X), eqHigh)))
	if hasGain(b.Kind) {
		b.Gain = max(-eqRange, min(g.dbAt(at.Y), eqRange))
	}
	g.changed(false, u)
}

func (g *eqGraph) hover(id int) {
	if id == g.hot {
		return
	}
	if d := g.dots[g.hot]; d != nil {
		d.hot.Animate(0, anim.Gentle)
	}
	g.hot = id
	if d := g.dots[id]; d != nil {
		d.hot.Animate(1, anim.Snappy)
	}
}

// prepare sets the menu for a right-click on a band's point: its kind,
// its slope, on or off, and away.
func (g *eqGraph) prepare(at geom.Point, u *gunim.UI) bool {
	id := g.hit(at)
	b, _ := g.band(id)
	if b == nil {
		return false
	}
	g.sel = id
	var items menuItems
	for k, name := range kindNames {
		kind := audio.FilterKind(k)
		items.addChecked(name, b.Kind == kind, func(u *gunim.UI) { g.setKind(id, kind, u) })
	}
	if isCut(b.Kind) {
		items.line()
		for _, s := range []int{12, 24, 36, 48} {
			items.addChecked(fmt.Sprintf("%d dB an octave", s), max(b.Slope, 12) == s, func(u *gunim.UI) {
				if cut, _ := g.band(id); cut != nil {
					cut.Slope = s
					g.changed(false, u)
				}
			})
		}
	}
	items.line()
	onText := "Turn off"
	if !b.On {
		onText = "Turn on"
	}
	items.add(onText, icon.Power, func(u *gunim.UI) {
		if band, _ := g.band(id); band != nil {
			band.On = !band.On
			g.changed(false, u)
		}
	})
	items.add("Delete band", icon.Trash2, func(u *gunim.UI) { g.remove(id, u) })
	items.set(g.menu)
	g.picks = items.do
	return true
}

// setKind turns band id into a band of kind, keeping what it can.
func (g *eqGraph) setKind(id int, kind audio.FilterKind, u *gunim.UI) {
	b, _ := g.band(id)
	if b == nil || b.Kind == kind {
		return
	}
	b.Kind = kind
	switch {
	case isCut(kind):
		b.Gain, b.Q, b.Slope = 0, 0.707, max(b.Slope, 24)
	case kind == audio.Notch:
		b.Gain, b.Q = 0, max(b.Q, 4)
	case kind == audio.LowShelf || kind == audio.HighShelf:
		b.Q = 0.707
	}
	g.changed(true, u)
}

// Step implements [gunim.Animator]: while the panel is open and music
// plays, the spectrum moves, its levels rising at once and falling
// slower, as a meter's do.
func (g *eqGraph) Step(dt time.Duration) bool {
	moving := g.Group.Step(dt)
	for id, d := range g.dots {
		if d.gone && d.in.Value() < 0.01 && !d.in.Active() {
			g.Remove(d.logHz, d.gain, d.logQ, d.in, d.on, d.hot)
			delete(g.dots, id)
		}
	}
	if g.eq.open.Value() < 0.01 {
		return moving
	}
	playing := g.eq.root.state.Playing
	if playing {
		g.eq.root.now.d.spectrum(g.freqs, g.heard, g.before)
	} else {
		for i := range g.heard {
			g.heard[i], g.before[i] = specBottom-20, specBottom-20
		}
	}
	sec := float32(dt.Seconds())
	settled := true
	ease := func(v *float32, to float32) {
		rate := float32(30)
		if to < *v {
			rate = 6
		}
		*v += (to - *v) * min(1, rate*sec)
		if math.Abs(float64(to-*v)) > 0.3 {
			settled = false
		}
	}
	for i, f := range g.freqs {
		tilt := float32(specTilt * math.Log2(float64(f)/1000))
		ease(&g.heardSm[i], g.heard[i]+tilt)
		ease(&g.beforeSm[i], g.before[i]+tilt)
	}
	return moving || playing || !settled
}

// Layout implements [gunim.Node].
func (g *eqGraph) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	g.size = c.Max
	return c.Max
}

// Paint implements [gunim.Node].
func (g *eqGraph) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	whole := geom.Rect{Max: box.Point()}
	accent := g.eq.root.bg.accent.Value()
	p.RRect(whole, 16, paint.Solid(faded(night, 0.55)))
	end := p.Layer(paint.LayerOpts{Bounds: whole, Opacity: 1, Clip: true, Radius: 16})
	defer end()
	g.paintGrid(p, box)
	g.paintSpectrum(p, box, accent)
	bypass := g.bypassAmt.Value()
	g.paintCurve(p, box, accent, bypass)
	g.paintDots(p, bypass)
	g.paintReadout(p, box)
	if len(g.bands) == 0 {
		msg := "Double-click to add a band"
		if f.Safe.Bottom > 0 || box.W < 500 {
			msg = "Double-tap to add a band"
		}
		run := shaped(msg, 14, false)
		run.Paint(p, geom.Pt((box.W-run.Advance)/2, box.H/2-30), faded(ink, 0.55))
	}
}

// paintGrid draws the lines of the octaves' landmarks and of every 6
// dB, labelled.
func (g *eqGraph) paintGrid(p *paint.Painter, box geom.Size) {
	for _, hz := range []float64{50, 100, 200, 500, 1000, 2000, 5000, 10000} {
		x := g.xOf(hz)
		p.RRect(geom.Rc(x, 0, 1, box.H), 0, paint.Solid(faded(ink, 0.06)))
		label := fmt.Sprintf("%.0f", hz)
		if hz >= 1000 {
			label = fmt.Sprintf("%.0fk", hz/1000)
		}
		run := shaped(label, 10, false)
		run.Paint(p, geom.Pt(x+4, box.H-16), faded(ink, 0.35))
	}
	for db := -eqRange + 6; db < eqRange; db += 6 {
		y := g.yOf(float32(db))
		a := float32(0.05)
		if db == 0 {
			a = 0.14
		}
		p.RRect(geom.Rc(0, y, box.W, 1), 0, paint.Solid(faded(ink, a)))
		run := shaped(fmt.Sprintf("%+d", db), 10, false)
		if db == 0 {
			run = shaped("0", 10, false)
		}
		run.Paint(p, geom.Pt(box.W-run.Advance-6, y-13), faded(ink, 0.35))
	}
}

// paintSpectrum draws the sound after the equalizer, filled in the
// track's colour, and before it as a pale line over the fill: where the
// equalizer lifts, the colour rises above the line, and where it cuts,
// the line runs above the colour.
func (g *eqGraph) paintSpectrum(p *paint.Painter, box geom.Size, accent color.NRGBA) {
	open := min(g.eq.open.Value(), 1)
	heard, before := smooth(g.heardSm), smooth(g.beforeSm)
	for i := range g.freqs {
		x0, x1 := g.edges(i)
		// Whole pixels, each column starting where the last ended, so
		// none overlap and the fill is even.
		x0, x1 = float32(math.Round(float64(x0))), float32(math.Round(float64(x1)))
		if x1 <= x0 {
			continue
		}
		if y := g.specY(heard[i]); y < box.H {
			p.RRect(geom.Rc(x0, y, x1-x0, box.H-y), 0, paint.Solid(faded(accent, 0.2*open)))
		}
	}
	var prevHeard, prevBefore geom.Point
	for i, f := range g.freqs {
		x := g.xOf(float64(f))
		h, b := geom.Pt(x, g.specY(heard[i])), geom.Pt(x, g.specY(before[i]))
		if i > 0 {
			segment(p, prevHeard, h, 1.5, faded(accent, 0.7*open))
			segment(p, prevBefore, b, 1.2, faded(ink, 0.45*open))
		}
		prevHeard, prevBefore = h, b
	}
}

// smooth returns levels evened across their neighbours, as a studio's
// analyzer evens them across a fraction of an octave.
func smooth(db []float32) []float32 {
	out := make([]float32, len(db))
	for i := range db {
		sum, n := float32(0), float32(0)
		for j := max(i-2, 0); j <= min(i+2, len(db)-1); j++ {
			w := float32(3 - abs(i-j))
			sum += db[j] * w
			n += w
		}
		out[i] = sum / n
	}
	return out
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

// edges returns where the spectrum's column i starts and ends.
func (g *eqGraph) edges(i int) (x0, x1 float32) {
	f := float64(g.freqs[i])
	step := math.Pow(eqHigh/eqLow, 0.5/(specPoints-1))
	return g.xOf(f / step), g.xOf(f * step)
}

// paintCurve draws the curve the bands make together, the band picked
// or under the pointer filled in its colour, dimmed while bypassed.
func (g *eqGraph) paintCurve(p *paint.Painter, box geom.Size, accent color.NRGBA, bypass float32) {
	n := max(int(box.W/3), 2)
	if len(g.curve) != n+1 {
		g.curve = make([]float32, n+1)
	}
	shown := make([]audio.Band, 0, len(g.dots))
	weights := make([]float32, 0, len(g.dots))
	ids := make([]int, 0, len(g.dots))
	for id, d := range g.dots {
		if w := d.weight(); w > 0.001 {
			shown = append(shown, d.value())
			weights = append(weights, w)
			ids = append(ids, id)
		}
	}
	focus := g.hot
	if focus == 0 {
		focus = g.sel
	}
	var own []float32
	for i := range n + 1 {
		x := box.W * float32(i) / float32(n)
		hz := g.hzAt(x)
		var db float32
		for j, b := range shown {
			r := float32(b.Response(hz)) * weights[j]
			db += r
			if ids[j] == focus {
				own = append(own, r)
			}
		}
		g.curve[i] = db
	}
	if d := g.dots[focus]; d != nil && len(own) == n+1 {
		// The band picked, filled from the line of 0 dB.
		c := bandColors[g.place(focus)%len(bandColors)]
		zero := g.yOf(0)
		w := box.W / float32(n)
		for i, r := range own {
			y := g.yOf(max(-eqRange-6, min(r, eqRange+6)))
			top, h := min(y, zero), float32(math.Abs(float64(y-zero)))
			p.RRect(geom.Rc(box.W*float32(i)/float32(n)-w/2, top, w+0.5, h), 0, paint.Solid(faded(c, 0.16*(1-bypass))))
		}
	}
	line := mix(ink, accent, 0.35)
	alpha := 1 - 0.65*bypass
	var prev geom.Point
	for i, db := range g.curve {
		pt := geom.Pt(box.W*float32(i)/float32(n), g.yOf(max(-eqRange-8, min(db, eqRange+8))))
		if i > 0 {
			segment(p, prev, pt, 6, faded(line, 0.12*alpha))
			segment(p, prev, pt, 2.2, faded(line, alpha))
		}
		prev = pt
	}
}

// segment draws a straight line from a to b, width wide.
func segment(p *paint.Painter, a, b geom.Point, width float32, c color.NRGBA) {
	d := b.Sub(a)
	l := float32(math.Hypot(float64(d.X), float64(d.Y)))
	if l < 0.01 {
		return
	}
	end := p.Push(paint.Rotate(float32(math.Atan2(float64(d.Y), float64(d.X))), a))
	p.RRect(geom.Rc(a.X-width/2, a.Y-width/2, l+width, width), width/2, paint.Solid(c))
	end()
}

// place returns where band id is among the bands, for its colour and
// its number.
func (g *eqGraph) place(id int) int {
	if _, i := g.band(id); i >= 0 {
		return i
	}
	return 0
}

// paintDots draws each band's point: numbered, in its colour, hollow
// while off, larger under the pointer, ringed while picked.
func (g *eqGraph) paintDots(p *paint.Painter, bypass float32) {
	for i, b := range g.bands {
		d := g.dots[b.ID]
		if d == nil {
			continue
		}
		g.paintDot(p, d, i, b.ID == g.sel, bypass)
	}
	// Points of bands gone, shrinking away.
	for _, d := range g.dots {
		if d.gone {
			g.paintDot(p, d, 0, false, bypass)
		}
	}
}

func (g *eqGraph) paintDot(p *paint.Painter, d *eqDot, i int, picked bool, bypass float32) {
	grow := max(d.in.Value(), 0)
	if grow < 0.01 {
		return
	}
	c := mix(bandColors[i%len(bandColors)], neutral, 0.7*bypass)
	at := g.dotAt(d)
	r := (9 + 3*d.hot.Value()) * grow
	if picked {
		p.RRectStroke(geom.Rc(at.X-r-5, at.Y-r-5, 2*r+10, 2*r+10), r+5, paint.Solid(color.NRGBA{}),
			paint.Stroke{Width: 2, Color: faded(c, 0.6)})
	}
	on := min(max(d.on.Value(), 0), 1)
	circle := geom.Rc(at.X-r, at.Y-r, 2*r, 2*r)
	p.ShadowRRect(circle, r, paint.Solid(mix(night, c, 0.25+0.75*on)),
		paint.Shadow{Blur: 10 + 8*d.hot.Value(), Color: faded(c, 0.5*on)})
	p.RRectStroke(circle, r, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 1.5, Color: c})
	if grow > 0.6 {
		run := shaped(strconv.Itoa(i+1), 11, true)
		run.Paint(p, geom.Pt(at.X-run.Advance/2, at.Y-8), mix(c, night, on))
	}
}

// paintReadout draws the settings of the band dragged, or under the
// pointer, beside its point.
func (g *eqGraph) paintReadout(p *paint.Painter, box geom.Size) {
	id := g.dragging
	if id == 0 {
		id = g.hot
	}
	b, i := g.band(id)
	if b == nil {
		return
	}
	text := bandText(*b)
	if !b.On {
		text += "  ·  off"
	}
	run := shaped(text, 12, true)
	at := g.dotAt(g.dots[id])
	w, h := run.Advance+20, float32(26)
	x := max(4, min(at.X-w/2, box.W-w-4))
	y := at.Y - 46
	if y < 4 {
		y = at.Y + 24
	}
	c := bandColors[i%len(bandColors)]
	p.RRect(geom.Rc(x, y, w, h), h/2, paint.Solid(faded(night, 0.92)))
	p.RRectStroke(geom.Rc(x, y, w, h), h/2, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 1, Color: faded(c, 0.7)})
	run.Paint(p, geom.Pt(x+10, y+6), ink)
}

// eqStrip is the band picked, under the graph: its kind, and its
// frequency, gain and Q, or slope, as values to drag; a button to turn
// it off and one to delete it.
type eqStrip struct {
	eq *eqPanel
	// slotOf is where each of the strip's parts is, by its slot.
	slotOf map[int]geom.Rect
	// held is the value being dragged, and from where the drag began.
	held     int
	heldFrom geom.Point
	heldBand audio.Band
	hot      int
	size     geom.Size
}

// The strip's slots: a kind for each of 0 to 5, then the values and
// the buttons.
const (
	slotFreq = 10 + iota
	slotGain
	slotQ
	slotOn
	slotDelete
)

// picked returns the band picked, and its place, or nil.
func (s *eqStrip) picked() (b *audio.Band, place int) { return s.eq.graph.band(s.eq.graph.sel) }

// DragsTouch implements [gunim.TouchDragger]: a finger on a value
// drags it.
func (s *eqStrip) DragsTouch() bool { return s.held >= slotFreq && s.held <= slotQ }

// Layout implements [gunim.Node]: the kinds in a row, then the values
// and the buttons, on one line where there is room and two where not.
func (s *eqStrip) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	s.size = c.Max
	clear(s.slotOf)
	twoRows := s.size.H > eqStripH+10
	x, y := float32(0), float32(14)
	pillW := float32(74)
	if twoRows {
		pillW = min(74, (s.size.W-5*4)/6)
	}
	for k := range kindNames {
		s.slotOf[k] = geom.Rc(x, y, pillW, 30)
		x += pillW + 4
	}
	if twoRows {
		x, y = 0, y+60
	} else {
		x += 16
		y = 6
	}
	for _, slot := range []int{slotFreq, slotGain, slotQ} {
		s.slotOf[slot] = geom.Rc(x, y, 84, 46)
		x += 90
	}
	s.slotOf[slotOn] = geom.Rc(x+6, y+5, 36, 36)
	s.slotOf[slotDelete] = geom.Rc(x+48, y+5, 36, 36)
	return s.size
}

// slotAt returns the slot at p, or -1.
func (s *eqStrip) slotAt(p geom.Point) int {
	for slot, r := range s.slotOf {
		if r.Contains(p) {
			return slot
		}
	}
	return -1
}

// Handle implements [gunim.Handler].
func (s *eqStrip) Handle(e input.Event, u *gunim.UI) bool {
	g := s.eq.graph
	b, _ := s.picked()
	switch e := e.(type) {
	case input.PointerMove:
		if s.held >= slotFreq && s.held <= slotQ && b != nil {
			s.slide(b, e.Pos, u)
			break
		}
		s.hot = s.slotAt(e.Pos)
	case input.PointerLeave:
		s.hot = -1
	case input.PointerDown:
		if e.Button != input.ButtonPrimary || b == nil {
			return false
		}
		slot := s.slotAt(e.Pos)
		switch {
		case slot >= 0 && slot < len(kindNames):
			g.setKind(b.ID, audio.FilterKind(slot), u)
		case slot == slotOn:
			b.On = !b.On
			s.eq.graph.changed(false, u)
		case slot == slotDelete:
			g.remove(b.ID, u)
		case slot >= slotFreq && slot <= slotQ && e.Clicks == 2:
			// A double-click sets the value back.
			switch slot {
			case slotGain:
				b.Gain = 0
			case slotQ:
				b.Q, b.Slope = 1, 24
				if isCut(b.Kind) || b.Kind == audio.LowShelf || b.Kind == audio.HighShelf {
					b.Q = 0.707
				}
			case slotFreq:
				b.Freq = 1000
			}
			g.changed(true, u)
		case slot >= slotFreq && slot <= slotQ:
			s.held, s.heldFrom, s.heldBand = slot, e.Pos, *b
		default:
			return false
		}
	case input.PointerUp:
		if s.held >= 0 {
			s.held = -1
			g.send(u)
		}
	case input.Scroll:
		slot := s.slotAt(e.Pos)
		if b == nil || slot < slotFreq || slot > slotQ {
			return false
		}
		n := e.Notches.Y
		if n == 0 {
			n = e.Delta.Y / 40
		}
		s.nudge(b, slot, n)
		g.changed(false, u)
	default:
		return false
	}
	u.Invalidate()
	return true
}

// slide moves the value held as the pointer goes up, or right, from
// where the drag began.
func (s *eqStrip) slide(b *audio.Band, p geom.Point, u *gunim.UI) {
	d := (p.X - s.heldFrom.X) - (p.Y - s.heldFrom.Y)
	from := s.heldBand
	switch s.held {
	case slotFreq:
		b.Freq = float32(max(eqLow, min(float64(from.Freq)*math.Exp2(float64(d)/80), eqHigh)))
	case slotGain:
		if hasGain(b.Kind) {
			b.Gain = max(-eqRange, min(from.Gain+d/8, eqRange))
		}
	case slotQ:
		if isCut(b.Kind) {
			b.Slope = max(12, min(max(from.Slope, 12)+12*int(d/30), 48))
		} else {
			b.Q = float32(max(0.1, min(float64(from.Q)*math.Exp2(float64(d)/80), 18)))
		}
	}
	s.eq.graph.changed(false, u)
}

// nudge moves a value by notches of the wheel.
func (s *eqStrip) nudge(b *audio.Band, slot int, n float32) {
	switch slot {
	case slotFreq:
		b.Freq = float32(max(eqLow, min(float64(b.Freq)*math.Exp2(float64(n)/12), eqHigh)))
	case slotGain:
		if hasGain(b.Kind) {
			b.Gain = max(-eqRange, min(b.Gain+n*0.5, eqRange))
		}
	case slotQ:
		s.eq.graph.widen(b, n)
	}
}

// Paint implements [gunim.Node].
func (s *eqStrip) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	b, i := s.picked()
	if b == nil {
		msg := "Pick a band to change it. Drag a point to move it, scroll on it to change its width, and right-click it for more."
		if box.W < 640 {
			msg = "Tap a point to pick it, drag to move it, hold for more."
		}
		paintFit(p, msg, 13, false, geom.Pt(4, 26), box.W-8, faded(ink, 0.5))
		return
	}
	c := bandColors[i%len(bandColors)]
	for k, name := range kindNames {
		r := s.slotOf[k]
		sel := audio.FilterKind(k) == b.Kind
		switch {
		case sel:
			p.RRect(r, r.Size().H/2, paint.Solid(c))
		case s.hot == k:
			p.RRect(r, r.Size().H/2, paint.Solid(faded(ink, 0.12)))
		default:
			p.RRect(r, r.Size().H/2, paint.Solid(faded(ink, 0.06)))
		}
		text := faded(ink, 0.8)
		if sel {
			text = night
		}
		run := shaped(name, 11, sel)
		if run.Advance > r.Size().W-8 {
			paintFit(p, name, 11, sel, geom.Pt(r.Min.X+4, r.Min.Y+9), r.Size().W-8, text)
		} else {
			run.Paint(p, geom.Pt(r.Min.X+(r.Size().W-run.Advance)/2, r.Min.Y+9), text)
		}
	}
	values := map[int][2]string{
		slotFreq: {"FREQ", hzText(b.Freq)},
		slotGain: {"GAIN", fmt.Sprintf("%+.1f dB", b.Gain)},
		slotQ:    {"Q", fmt.Sprintf("%.2f", b.Q)},
	}
	if !hasGain(b.Kind) {
		values[slotGain] = [2]string{"GAIN", "—"}
	}
	if isCut(b.Kind) {
		values[slotQ] = [2]string{"SLOPE", fmt.Sprintf("%d dB/oct", max(b.Slope, 12))}
	}
	for _, slot := range []int{slotFreq, slotGain, slotQ} {
		r := s.slotOf[slot]
		fill := faded(ink, 0.06)
		if s.held == slot {
			fill = faded(c, 0.25)
		} else if s.hot == slot {
			fill = faded(ink, 0.1)
		}
		p.RRect(r, 10, paint.Solid(fill))
		v := values[slot]
		shaped(v[0], 9, true).Paint(p, r.Min.Add(geom.Pt(10, 7)), faded(c, 0.9))
		paintFit(p, v[1], 14, true, r.Min.Add(geom.Pt(10, 21)), r.Size().W-14, ink)
	}
	on := s.slotOf[slotOn]
	pc := faded(ink, 0.4)
	if b.On {
		pc = c
	}
	p.RRect(on, on.Size().W/2, paint.Solid(faded(ink, 0.06)))
	widget.PaintIcon(p, f.Theme, icon.Power, on.Inset(geom.Uniform(9)), pc)
	del := s.slotOf[slotDelete]
	p.RRect(del, del.Size().W/2, paint.Solid(faded(ink, 0.06)))
	widget.PaintIcon(p, f.Theme, icon.Trash2, del.Inset(geom.Uniform(9)), faded(ink, 0.7))
}

// A preset is a set of bands with a name.
type preset struct {
	name  string
	bands []audio.Band
}

// presets are the equalizer's presets, each a starting point to shape.
var presets = []preset{
	{"Flat", nil},
	{"Warm", []audio.Band{
		{Kind: audio.LowShelf, Freq: 120, Gain: 3, Q: 0.707},
		{Kind: audio.Bell, Freq: 350, Gain: 1.5, Q: 1},
		{Kind: audio.HighShelf, Freq: 8000, Gain: -2.5, Q: 0.707},
	}},
	{"Bright", []audio.Band{
		{Kind: audio.LowCut, Freq: 30, Q: 0.707, Slope: 24},
		{Kind: audio.Bell, Freq: 300, Gain: -2, Q: 1.2},
		{Kind: audio.HighShelf, Freq: 6000, Gain: 4, Q: 0.707},
	}},
	{"Vocal presence", []audio.Band{
		{Kind: audio.LowCut, Freq: 90, Q: 0.707, Slope: 24},
		{Kind: audio.Bell, Freq: 250, Gain: -2.5, Q: 1.4},
		{Kind: audio.Bell, Freq: 3000, Gain: 3.5, Q: 1.1},
		{Kind: audio.HighShelf, Freq: 12000, Gain: 1.5, Q: 0.707},
	}},
	{"Bass boost", []audio.Band{
		{Kind: audio.LowShelf, Freq: 90, Gain: 6, Q: 0.8},
		{Kind: audio.Bell, Freq: 400, Gain: -1.5, Q: 1},
	}},
	{"Loudness", []audio.Band{
		{Kind: audio.LowShelf, Freq: 80, Gain: 5, Q: 0.707},
		{Kind: audio.Bell, Freq: 1000, Gain: -1.5, Q: 0.8},
		{Kind: audio.HighShelf, Freq: 10000, Gain: 4, Q: 0.707},
	}},
	{"Low cut", []audio.Band{
		{Kind: audio.LowCut, Freq: 80, Q: 0.707, Slope: 24},
	}},
}
