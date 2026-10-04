package main

import (
	"fmt"
	"image/color"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// library is the list of tracks, under a heading: beside the track
// playing, or as a sheet over it on a narrow window.
type library struct {
	root   *playerRoot
	list   *trackList
	scroll *widget.Scroll
	count  int
	busy   bool
	// viewH is the height the list shows, and at where the heading
	// starts, clear of a phone's bars.
	viewH float32
	at    geom.Point
}

func newLibrary(r *playerRoot) *library {
	l := &library{root: r}
	l.list = newTrackList(r)
	l.scroll = widget.NewScroll(l.list)
	return l
}

func (l *library) show(s Player, u *gunim.UI) {
	l.count, l.busy = len(s.Tracks), s.Scanning
	l.list.show(s, u)
}

// headH is the height of the heading over the list.
const headH = 76

// Children implements [gunim.Composite].
func (l *library) Children() []gunim.Node { return []gunim.Node{l.scroll} }

// Layout implements [gunim.Node].
func (l *library) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	// The glass runs under a phone's bars; the heading and the list keep
	// clear of them: of the status bar only beside the track playing, as
	// a sheet starts below it.
	l.at = geom.Pt(f.Safe.Left, 0)
	if !l.root.narrow {
		l.at.Y = f.Safe.Top
	}
	k := kids.At(0)
	l.viewH = max(size.H-l.at.Y-headH-f.Safe.Bottom, 0)
	k.Layout(gunim.Tight(geom.Sz(size.W-l.at.X-f.Safe.Right, l.viewH)))
	k.Place(geom.Pt(l.at.X, l.at.Y+headH))
	return size
}

// Paint implements [gunim.Node]: frosted glass over the background,
// rounded at the top as a sheet.
func (l *library) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, kids gunim.Children) {
	whole := geom.Rect{Max: box.Point()}
	radius := float32(0)
	if l.root.narrow {
		radius = 22
		// The corners round only at the top: the rest runs off the
		// window's bottom.
		whole.Max.Y += radius
	}
	end := p.Layer(paint.LayerOpts{Bounds: whole, Opacity: 1, Backdrop: 28, Clip: true, Radius: radius})
	p.RRect(whole, radius, paint.Solid(faded(night, 0.55)))
	p.RRect(whole, radius, paint.Solid(faded(ink, 0.03)))
	end()
	if l.root.narrow {
		p.RRect(geom.Rc(box.W/2-20, 8, 40, 4), 2, paint.Solid(faded(ink, 0.3)))
	} else {
		p.RRect(geom.Rc(box.W-1, 0, 1, box.H), 0, paint.Solid(faded(ink, 0.08)))
	}
	shaped("Library", 22, true).Paint(p, l.at.Add(geom.Pt(24, 22)), ink)
	what := fmt.Sprintf("%d tracks", l.count)
	if l.busy {
		what += " · reading the folder…"
	}
	shaped(what, 12, false).Paint(p, l.at.Add(geom.Pt(24, 50)), faded(ink, 0.5))
	kids.At(0).Paint(p)
}

// rowH is the height of a track's row.
const rowH = 60

// trackList draws the tracks, one a row: the cover, the title, the
// artist and the length. The row playing is lit, with bars that move
// with the music where its length was.
type trackList struct {
	anim.Group
	root   *playerRoot
	tracks []Track
	cur    int
	// hot is the row the pointer is over, and lit how lit each row is,
	// by track.
	hot  int
	lit  map[int]*anim.Float
	down int
	size geom.Size
}

func newTrackList(r *playerRoot) *trackList {
	return &trackList{root: r, hot: -1, down: -1, lit: map[int]*anim.Float{}}
}

func (t *trackList) show(s Player, u *gunim.UI) {
	t.tracks, t.cur = s.Tracks, s.Current
	u.Invalidate()
}

// light lights the row of track id, or dims it.
func (t *trackList) light(id int, on bool) {
	a := t.lit[id]
	if a == nil {
		a = anim.NewFloat(0)
		t.lit[id] = a
		t.Add(a)
	}
	if on {
		a.Animate(1, anim.Snappy)
	} else {
		a.Animate(0, anim.Gentle)
	}
}

// Step implements [gunim.Animator]: the row playing has bars to move.
func (t *trackList) Step(dt time.Duration) bool {
	return t.Group.Step(dt) || t.cur != 0 && t.root.meter.active
}

func (t *trackList) rowAt(p geom.Point) int {
	if p.Y < 0 || p.X < 0 || p.X > t.size.W {
		return -1
	}
	i := int(p.Y / rowH)
	if i >= len(t.tracks) {
		return -1
	}
	return i
}

// Handle implements [gunim.Handler].
func (t *trackList) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerMove:
		if e.Touch {
			return false
		}
		t.hover(t.rowAt(e.Pos))
	case input.PointerLeave:
		t.hover(-1)
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		t.down = t.rowAt(e.Pos)
	case input.PointerUp:
		i := t.rowAt(e.Pos)
		if i >= 0 && i == t.down {
			u.Cue(gunim.CueSelect, t)
			u.Send(t, PlayTrack{ID: t.tracks[i].ID})
			if t.root.narrow {
				t.root.openSheet(false, u)
			}
		}
		t.down = -1
	default:
		return false
	}
	u.Invalidate()
	return true
}

func (t *trackList) hover(i int) {
	if i == t.hot {
		return
	}
	if t.hot >= 0 && t.hot < len(t.tracks) {
		t.light(t.tracks[t.hot].ID, false)
	}
	t.hot = i
	if i >= 0 {
		t.light(t.tracks[i].ID, true)
	}
}

// Layout implements [gunim.Node]: as tall as its rows.
func (t *trackList) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	t.size = c.Constrain(geom.Sz(c.Max.W, float32(len(t.tracks))*rowH+16))
	return t.size
}

// Paint implements [gunim.Node].
func (t *trackList) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, _ gunim.Children) {
	// Only the rows in view are drawn, for a library of thousands.
	top := t.root.lib.scroll.Offset()
	first := max(int(top/rowH)-1, 0)
	last := min(int((top+t.root.lib.viewH)/rowH)+1, len(t.tracks)-1)
	for i := first; i <= last; i++ {
		tr := t.tracks[i]
		y := float32(i) * rowH
		row := geom.Rc(10, y+2, box.W-20, rowH-4)
		playing := tr.ID == t.cur
		if a := t.lit[tr.ID]; a != nil && a.Value() > 0.01 {
			p.RRect(row, 12, paint.Solid(faded(ink, 0.07*a.Value())))
		}
		if playing {
			p.RRect(row, 12, paint.Solid(faded(tr.Accent, 0.14)))
		}
		cover := geom.Rc(row.Min.X+8, y+(rowH-44)/2, 44, 44)
		if tr.Cover != nil {
			p.Image(tr.Cover, cover, paint.ImageOpts{Radius: 8, Opacity: 1})
		}
		textX := cover.Max.X + 12
		room := row.Max.X - 64 - textX
		title := faded(ink, 0.92)
		if playing {
			title = tr.Accent
		}
		paintFit(p, tr.Title, 15, true, geom.Pt(textX, y+12), room, title)
		sub := tr.Artist
		if tr.Album != "" {
			if sub != "" {
				sub += " · "
			}
			sub += tr.Album
		}
		paintFit(p, sub, 12, false, geom.Pt(textX, y+33), room, faded(ink, 0.5))
		if playing {
			t.paintBars(p, geom.Pt(row.Max.X-34, y+rowH/2), tr)
		} else if tr.Length > 0 {
			run := shaped(clock(tr.Length), 12, false)
			run.Paint(p, geom.Pt(row.Max.X-12-run.Advance, y+22), faded(ink, 0.45))
		}
	}
}

// paintBars draws three little bars moving with the music, low, middle
// and high, centred on mid.
func (t *trackList) paintBars(p *paint.Painter, mid geom.Point, tr Track) {
	m := t.root.meter
	for i, band := range []int{1, bandCount / 3, 2 * bandCount / 3} {
		v := m.bands[band]
		h := 4 + 16*v
		x := mid.X - 9 + float32(i)*7
		p.RRect(geom.Rc(x, mid.Y+10-h, 4, h), 2, paint.Solid(tr.Accent))
	}
}

// paintFit draws s at size, its top left at at, cut short with an
// ellipsis where it is wider than room.
func paintFit(p *paint.Painter, s string, size float32, bold bool, at geom.Point, room float32, c color.NRGBA) {
	run := shaped(s, size, bold)
	if run.Advance <= room {
		run.Paint(p, at, c)
		return
	}
	rs := []rune(s)
	for n := len(rs) - 1; n > 0; n-- {
		cut := shaped(string(rs[:n])+"…", size, bold)
		if cut.Advance <= room {
			cut.Paint(p, at, c)
			return
		}
	}
}
