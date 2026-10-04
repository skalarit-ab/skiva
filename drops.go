package main

import (
	"fmt"
	"image/color"
	"path/filepath"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// trackDrag is tracks dragged inside the player, from a list's rows.
type trackDrag struct {
	IDs  []int
	From ListID
}

// dropped says what a drag carries: files from another program, or
// tracks of the player. ok is false for anything else.
func dropped(data any) (files []string, tracks []int, ok bool) {
	switch d := data.(type) {
	case input.Files:
		return d.Paths, nil, true
	case trackDrag:
		return nil, d.IDs, true
	}
	return nil, nil, false
}

// what names what a drag carries, for the words of a drop target.
func what(files []string, tracks []int) string {
	switch {
	case len(tracks) == 1:
		return "this track"
	case len(tracks) > 1:
		return fmt.Sprintf("%d tracks", len(tracks))
	case len(files) == 1:
		return filepath.Base(files[0])
	case len(files) > 1:
		return fmt.Sprintf("%d files", len(files))
	}
	return "these files"
}

// hint is what a drop does, for the picture a drag of tracks carries.
func hint(text string) widget.DropHint { return widget.DropHint{Text: text, Effect: widget.DropCopy} }

// trackCard is the picture a dragged track carries: its cover and its
// title, on a dark card.
type trackCard struct {
	tr Track
}

// cardSize is the size of a dragged track's card.
var cardSize = geom.Sz(240, 56)

// Layout implements [gunim.Node].
func (c *trackCard) Layout(gunim.Constraints, gunim.Frame, gunim.Children) geom.Size { return cardSize }

// Paint implements [gunim.Node].
func (c *trackCard) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	whole := geom.Rect{Max: box.Point()}
	p.ShadowRRect(whole, 12, paint.Solid(mix(night, ink, 0.1)),
		paint.Shadow{Blur: 16, Offset: geom.Pt(0, 6), Color: faded(night, 0.5)})
	p.RRectStroke(whole, 12, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 1, Color: faded(c.tr.Accent, 0.6)})
	cover := geom.Rc(6, 6, 44, 44)
	if c.tr.Cover != nil {
		p.Image(c.tr.Cover, cover, paint.ImageOpts{Radius: 8, Opacity: 1})
	}
	room := box.W - cover.Max.X - 20
	paintFit(p, c.tr.Title, 14, true, geom.Pt(cover.Max.X+10, 10), room, ink)
	paintFit(p, c.tr.Artist, 12, false, geom.Pt(cover.Max.X+10, 30), room, faded(ink, 0.55))
}

// dropArea is the track playing as a drop target: files or tracks let
// go over it go on Up next, and play at once if nothing plays. As a
// drag comes over it, a frame in the track's colour grows in round it,
// with what a drop would do in its middle.
type dropArea struct {
	anim.Group
	on, pop *anim.Float
	text    string
	playing bool
}

func newDropArea() *dropArea {
	d := &dropArea{on: anim.NewFloat(0), pop: anim.NewFloat(0)}
	d.Add(d.on, d.pop)
	return d
}

// over shows the frame for a drag of files or tracks, or takes it away.
func (d *dropArea) over(on bool, files []string, tracks []int) {
	if !on {
		d.on.Animate(0, anim.Gentle)
		return
	}
	if d.on.Target() < 0.5 {
		d.pop.Jump(0)
		d.pop.Animate(1, anim.Spring{Response: 0.4, Damping: 0.55})
	}
	d.on.Animate(1, anim.Snappy)
	if d.playing {
		d.text = "Add " + what(files, tracks) + " to Up next"
	} else {
		d.text = "Play " + what(files, tracks)
	}
}

// handle takes drags over the track playing, for n.
func (d *dropArea) handle(n gunim.Node, e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.DragOver:
		files, tracks, ok := dropped(e.Data)
		if !ok {
			return false
		}
		d.over(true, files, tracks)
		u.AnswerDrag(hint(d.text))
	case input.DragLeave:
		d.over(false, nil, nil)
	case input.Drop:
		files, tracks, ok := dropped(e.Data)
		d.over(false, nil, nil)
		if !ok {
			return false
		}
		u.Cue(gunim.CueSelect, n)
		if tracks != nil {
			u.Send(n, Enqueue{Tracks: tracks, Play: true})
		} else {
			u.Send(n, AddPaths{Paths: files, To: QueueList, At: -1, Play: true})
		}
	default:
		return false
	}
	u.Invalidate()
	return true
}

// paint draws the frame over box, in accent.
func (d *dropArea) paint(p *paint.Painter, f gunim.Frame, box geom.Size, accent color.NRGBA) {
	v := d.on.Value()
	if v < 0.005 {
		return
	}
	pop := d.pop.Value()
	area := geom.Rect{Max: box.Point()}.Inset(geom.Uniform(16 - 6*v))
	// What plays blurs away behind the frame, so its words stand clear.
	end := p.Layer(paint.LayerOpts{Bounds: area, Opacity: 1, Backdrop: 24 * min(v, 1), Clip: true, Radius: 28})
	p.RRect(area, 28, paint.Solid(faded(night, 0.72*min(v, 1))))
	end()
	p.RRectStroke(area, 28, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 2, Color: faded(accent, 0.9*v)})
	mid := geom.Pt(box.W/2, box.H/2-20)
	r := 44 * (0.6 + 0.4*pop)
	p.ShadowRRect(geom.Rc(mid.X-r, mid.Y-r, 2*r, 2*r), r, paint.Solid(faded(accent, v)),
		paint.Shadow{Blur: 30, Color: faded(accent, 0.5*v)})
	ic := icon.ListPlus
	if !d.playing {
		ic = icon.Play
	}
	s := r * 0.8
	widget.PaintIcon(p, f.Theme, ic, geom.Rc(mid.X-s/2, mid.Y-s/2, s, s), faded(night, v))
	room := box.W - 64
	run := shaped(d.text, 20, true)
	if run.Advance <= room {
		run.Paint(p, geom.Pt(mid.X-run.Advance/2, mid.Y+r+20), faded(ink, v))
	} else {
		paintFit(p, d.text, 20, true, geom.Pt(32, mid.Y+r+20), room, faded(ink, v))
	}
}

// dwell is how long a drag rests on a list's back button before the
// list slides away, to drop on the shelf.
const dwell = 600 * time.Millisecond
