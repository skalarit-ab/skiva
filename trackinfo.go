package main

import (
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
)

// pillW is the width of the loudness gain's button.
const pillW = 92

// gainNames names the gain modes, as their button shows them.
var gainNames = []string{"No gain", "Track gain", "Album gain"}

// gainPill is the button that steps through the gain modes: no gain,
// track gain and album gain. Its words turn over as it steps, and it
// lights in the track's colour while gain is on.
type gainPill struct {
	anim.Group
	mode, was        GainMode
	turn, hover, lit *anim.Float
	down             *anim.Float
	accent           *anim.Color
	held             bool
	size             geom.Size
}

func newGainPill() *gainPill {
	g := &gainPill{turn: anim.NewFloat(1), hover: anim.NewFloat(0), lit: anim.NewFloat(0), down: anim.NewFloat(0),
		accent: anim.NewColor(neutral), mode: -1}
	g.Add(g.turn, g.hover, g.lit, g.down, g.accent)
	return g
}

func (g *gainPill) show(m GainMode, accent color.NRGBA) {
	if m != g.mode {
		if g.mode >= 0 {
			g.was = g.mode
			g.turn.Jump(0)
			g.turn.Animate(1, anim.Spring{Response: 0.35, Damping: 0.8})
		} else {
			g.was = m
		}
		g.mode = m
	}
	g.lit.Animate(map[bool]float32{false: 0, true: 1}[m != GainOff], anim.Snappy)
	g.accent.Animate(accent, anim.Spring{Response: 0.8, Damping: 1})
}

// Handle implements [gunim.Handler]: a click steps to the next mode.
func (g *gainPill) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerEnter:
		g.hover.Animate(1, anim.Snappy)
	case input.PointerLeave:
		g.hover.Animate(0, anim.Gentle)
		g.down.Animate(0, anim.Gentle)
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		g.held = true
		g.down.Animate(1, anim.Spring{Response: 0.12, Damping: 1})
	case input.PointerUp:
		if !g.held {
			return false
		}
		g.held = false
		g.down.Animate(0, anim.Spring{Response: 0.4, Damping: 0.45})
		if (geom.Rect{Max: g.size.Point()}).Contains(e.Pos) {
			u.Cue(gunim.CueTick, g)
			next := (max(g.mode, 0) + 1) % GainMode(len(gainNames))
			g.show(next, g.accent.Target())
			u.Send(g, SetGainMode{Mode: next})
		}
	default:
		return false
	}
	u.Invalidate()
	return true
}

// Layout implements [gunim.Node].
func (g *gainPill) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	g.size = c.Max
	return c.Max
}

// Paint implements [gunim.Node]: the pill, its words turning over from
// the last mode's as the mode steps.
func (g *gainPill) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	whole := geom.Rect{Max: box.Point()}
	mid := geom.Pt(box.W/2, box.H/2)
	defer p.Push(paint.Scale(1-0.06*g.down.Value(), mid))()
	accent := g.accent.Value()
	lit := g.lit.Value()
	p.RRect(whole, box.H/2, paint.Solid(faded(mix(ink, accent, lit), 0.06+0.06*g.hover.Value()+0.06*lit)))
	words := mix(faded(ink, 0.6), accent, lit)
	turn := g.turn.Value()
	end := p.Layer(paint.LayerOpts{Bounds: whole, Opacity: 1, Clip: true, Radius: box.H / 2})
	defer end()
	word := func(m GainMode, dy, alpha float32) {
		if m < 0 || int(m) >= len(gainNames) || alpha < 0.01 {
			return
		}
		run := shaped(gainNames[m], 11, true)
		run.Paint(p, geom.Pt((box.W-run.Advance)/2, (box.H-14)/2+dy), faded(words, alpha))
	}
	if turn < 1 {
		word(g.was, -box.H*turn, 1-turn)
	}
	word(g.mode, box.H*(1-turn), turn)
}

// infoCard tells about the track playing: its file, its format, its
// loudness and the gain it plays at. It unfolds from its button.
type infoCard struct {
	anim.Group
	root *playerRoot
	open *anim.Float
	// from is where the card unfolds from, its button's middle.
	from geom.Point
	size geom.Size
}

func newInfoCard(r *playerRoot) *infoCard {
	c := &infoCard{root: r, open: anim.NewFloat(0)}
	c.Add(c.open)
	return c
}

func (c *infoCard) shown() bool { return c.open.Target() > 0.5 }

func (c *infoCard) show(on bool, u *gunim.UI) {
	to := float32(0)
	if on {
		to = 1
		u.Cue(gunim.CueOpen, c)
	}
	c.open.Animate(to, anim.Spring{Response: 0.38, Damping: 0.8})
	u.Invalidate()
}

// infoW is the card's width; its height follows its lines.
const infoW = 380

// lines returns what the card says, a label and a value a line.
func (c *infoCard) lines() (head [2]string, rows [][2]string) {
	s := c.root.state
	t, ok := c.root.track()
	if !ok {
		return [2]string{"Nothing playing", ""}, nil
	}
	head = [2]string{t.Title, t.Artist}
	if t.Album != "" && head[1] != "" {
		head[1] += " · " + t.Album
	} else if t.Album != "" {
		head[1] = t.Album
	}
	if t.File == "" {
		rows = append(rows, [2]string{"File", "Made in code, played as it goes"})
	} else {
		rows = append(rows, [2]string{"File", filepath.Base(t.File)}, [2]string{"Folder", filepath.Dir(t.File)},
			[2]string{"Size", sizeText(t.Size)})
	}
	if f := t.Format; f.Name != "" {
		q := f.Name + " · " + rateText(f.SampleRate)
		if f.Bits > 0 {
			q += fmt.Sprintf(" · %d-bit", f.Bits)
		}
		switch f.Channels {
		case 1:
			q += " · mono"
		case 2:
			q += " · stereo"
		case 0:
		default:
			q += fmt.Sprintf(" · %d channels", f.Channels)
		}
		if t.Size > 0 && t.Length > 0 && (f.Name == "MP3" || f.Name == "Ogg Vorbis") {
			q += fmt.Sprintf(" · %d kbps", int(float64(t.Size)*8/t.Length.Seconds()/1000))
		}
		rows = append(rows, [2]string{"Quality", q})
	}
	rows = append(rows, [2]string{"Length", clock(t.Length)})
	switch {
	case t.Measured:
		rows = append(rows, [2]string{"Loudness", fmt.Sprintf("%.1f LUFS · peak %.1f dBFS", t.LUFS, dB(float64(max(t.Peak, 1e-6))))})
	case t.Format.Name != "" || t.Peaks != nil && s.GainBy != GainMeasuring:
		rows = append(rows, [2]string{"Loudness", "Silent"})
	default:
		rows = append(rows, [2]string{"Loudness", "Measuring…"})
	}
	if s.GainBy == GainByAlbum {
		rows = append(rows, [2]string{"Album", fmt.Sprintf("%.1f LUFS", s.AlbumLUFS)})
	}
	rows = append(rows, [2]string{"Gain", gainText(s, t)})
	if s.Headroom > 0.05 {
		rows = append(rows, [2]string{"Headroom", fmt.Sprintf("%.1f dB lower, for the equalizer's boosts", s.Headroom)})
	}
	return head, rows
}

// gainText says what gain the track plays at, and why.
func gainText(s Player, t Track) string {
	switch s.GainBy {
	case GainByTrack:
		text := fmt.Sprintf("%+.1f dB track gain, to %d LUFS", s.Gain, targetLUFS)
		if math.Abs(float64(s.Gain)-(targetLUFS-float64(t.LUFS))) > 0.05 {
			text = fmt.Sprintf("%+.1f dB track gain, held under its peak", s.Gain)
		}
		return text
	case GainByAlbum:
		text := fmt.Sprintf("%+.1f dB album gain, to %d LUFS", s.Gain, targetLUFS)
		if math.Abs(float64(s.Gain)-(targetLUFS-float64(s.AlbumLUFS))) > 0.05 {
			text = fmt.Sprintf("%+.1f dB album gain, held under its peak", s.Gain)
		}
		return text
	case GainMeasuring:
		return "None until its loudness is measured"
	case GainNone:
	}
	if s.GainMode == GainOff {
		return "None: loudness gain is off"
	}
	return "None"
}

func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n/1024)
}

func rateText(hz int) string {
	if hz%1000 == 0 {
		return fmt.Sprintf("%d kHz", hz/1000)
	}
	return fmt.Sprintf("%.1f kHz", float64(hz)/1000)
}

// infoH returns the card's height for its lines.
func infoH(rows int) float32 { return 84 + float32(rows)*24 + 12 }

// Layout implements [gunim.Node].
func (c *infoCard) Layout(cs gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	_, rows := c.lines()
	c.size = cs.Constrain(geom.Sz(min(infoW, cs.Max.W), infoH(len(rows))))
	return c.size
}

// Handle implements [gunim.Handler]: a press on the card is the card's,
// and leaves it open.
func (c *infoCard) Handle(e input.Event, _ *gunim.UI) bool {
	switch e.(type) {
	case input.PointerDown, input.PointerUp:
		return c.shown()
	}
	return false
}

// Step implements [gunim.Animator].
func (c *infoCard) Step(dt time.Duration) bool { return c.Group.Step(dt) }

// Paint implements [gunim.Node]: the card grows out of its button,
// fading in.
func (c *infoCard) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	open := c.open.Value()
	if open < 0.01 {
		return
	}
	defer p.Push(paint.Scale(0.6+0.4*min(open, 1.1), c.from))()
	alpha := min(open, 1)
	whole := geom.Rect{Max: box.Point()}
	end := p.Layer(paint.LayerOpts{Bounds: whole, Opacity: alpha, Backdrop: 28, Clip: true, Radius: 18})
	p.RRect(whole, 18, paint.Solid(faded(night, 0.82)))
	end()
	end = p.Layer(paint.LayerOpts{Bounds: whole, Opacity: alpha})
	defer end()
	t, _ := c.root.track()
	p.RRectStroke(whole, 18, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 1, Color: faded(t.Accent, 0.5)})
	head, rows := c.lines()
	x := float32(20)
	if t.Cover != nil {
		p.Image(t.Cover, geom.Rc(18, 18, 48, 48), paint.ImageOpts{Radius: 8, Opacity: 1})
		x = 78
	}
	paintFit(p, head[0], 16, true, geom.Pt(x, 22), box.W-x-18, ink)
	paintFit(p, head[1], 12, false, geom.Pt(x, 46), box.W-x-18, faded(ink, 0.55))
	y := float32(84)
	for _, r := range rows {
		shaped(r[0], 11, true).Paint(p, geom.Pt(20, y+3), faded(t.Accent, 0.9))
		room := box.W - 110 - 18
		if r[0] == "Folder" {
			paintFitStart(p, r[1], 13, geom.Pt(110, y), room, faded(ink, 0.85))
		} else {
			paintFit(p, r[1], 13, false, geom.Pt(110, y), room, faded(ink, 0.85))
		}
		y += 24
	}
}

// paintFitStart draws s, cut short at its start where it is wider than
// room, as a long folder's end says the most.
func paintFitStart(p *paint.Painter, s string, size float32, at geom.Point, room float32, c color.NRGBA) {
	rs := []rune(s)
	for n := range rs {
		text := string(rs[n:])
		if n > 0 {
			text = "…" + text
		}
		if run := shaped(text, size, false); run.Advance <= room {
			run.Paint(p, at, c)
			return
		}
	}
}
