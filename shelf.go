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

// The kinds of row on the shelf.
type shelfKind int

const (
	shelfAll shelfKind = iota
	shelfQueue
	shelfCaption
	shelfPlaylist
	shelfNewPlaylist
	shelfFolder
	shelfAddFolder
	shelfAddFiles
)

// shelfRow is one row of the shelf.
type shelfRow struct {
	kind shelfKind
	// key names the row across updates, for its animations.
	key       string
	title     string
	sub       string
	icon      *icon.Icon
	covers    []*paint.Image
	list      ListID
	path, pid string
}

// captionH is the height of a caption's row.
const captionH = 40

func (r shelfRow) height() float32 {
	if r.kind == shelfCaption {
		return captionH
	}
	return rowH
}

// shelf is the library's front page: the whole library, the playlists
// and the folders followed, a row each, with rows to make a playlist,
// add a folder and add files. A row new to it grows in, and the rows
// below glide to make room; a row gone lets them glide back.
type shelf struct {
	anim.Group
	lib  *library
	rows []shelfRow
	// ys is where each row is, gliding to where it belongs, in its
	// key; in how far it has grown in; lit how lit it is.
	ys, in, lit map[string]*anim.Float
	hot, down   string
	size        geom.Size
	// target is the row a drag would drop on, lit by aim, and goal
	// what a drop there does.
	target string
	aim    *anim.Float
	goal   string
	// shown says the shelf has been shown once, so the rows of the
	// first showing stand in place.
	shown bool
}

func newShelf(l *library) *shelf {
	s := &shelf{lib: l, ys: map[string]*anim.Float{}, in: map[string]*anim.Float{}, lit: map[string]*anim.Float{},
		aim: anim.NewFloat(0)}
	s.Add(s.aim)
	return s
}

// covers returns up to four covers of ids, each a different one.
func covers(ids []int, byID map[int]Track) []*paint.Image {
	var out []*paint.Image
	seen := map[*paint.Image]bool{}
	for _, id := range ids {
		c := byID[id].Cover
		if c == nil || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
		if len(out) == 4 {
			break
		}
	}
	return out
}

// queued says how many tracks are on Up next.
func queued(n int) string {
	if n == 0 {
		return "Drop tracks here to play them next"
	}
	return count(n)
}

func count(n int) string {
	if n == 1 {
		return "1 track"
	}
	return fmt.Sprintf("%d tracks", n)
}

func (s *shelf) show(st Player, u *gunim.UI) {
	byID := s.lib.byID
	rows := make([]shelfRow, 0, len(st.Playlists)+len(st.Folders)+7)
	rows = append(rows,
		shelfRow{kind: shelfAll, key: "all", title: "All tracks", sub: count(len(st.Library)),
			icon: icon.Library, covers: covers(st.Library, byID), list: AllTracks},
		shelfRow{kind: shelfQueue, key: "queue", title: "Up next", sub: queued(len(st.Queue)),
			icon: icon.ListStart, covers: covers(st.Queue, byID), list: QueueList},
		shelfRow{kind: shelfCaption, key: "cap:p", title: "Playlists"})
	for _, p := range st.Playlists {
		rows = append(rows, shelfRow{kind: shelfPlaylist, key: "p:" + p.ID, title: p.Name, sub: count(len(p.Tracks)),
			icon: icon.ListMusic, covers: covers(p.Tracks, byID), list: PlaylistList(p.ID), pid: p.ID})
	}
	rows = append(rows,
		shelfRow{kind: shelfNewPlaylist, key: "new", title: "New playlist", icon: icon.Plus},
		shelfRow{kind: shelfCaption, key: "cap:f", title: "Folders"})
	for _, f := range st.Folders {
		sub := count(len(f.Tracks))
		if f.Reading {
			sub += " · reading…"
		}
		rows = append(rows, shelfRow{kind: shelfFolder, key: "f:" + f.Path, title: filepath.Base(f.Path),
			sub: sub + " · " + filepath.Dir(f.Path), icon: icon.Folder, list: FolderList(f.Path), path: f.Path})
	}
	rows = append(rows,
		shelfRow{kind: shelfAddFolder, key: "addf", title: "Add a folder", icon: icon.FolderPlus},
		shelfRow{kind: shelfAddFiles, key: "addm", title: "Add files", icon: icon.FilePlus})
	s.rows = rows
	var y float32
	live := map[string]bool{}
	for _, r := range rows {
		live[r.key] = true
		at := s.ys[r.key]
		if at == nil {
			at = anim.NewFloat(y)
			s.ys[r.key] = at
			grow := anim.NewFloat(1)
			if s.shown {
				grow.Jump(0)
				grow.Animate(1, anim.Spring{Response: 0.45, Damping: 0.8})
			}
			s.in[r.key] = grow
			s.Add(at, grow)
		} else {
			at.Animate(y, anim.Spring{Response: 0.35, Damping: 0.85})
		}
		y += r.height()
	}
	for k, a := range s.ys {
		if live[k] {
			continue
		}
		s.Remove(a, s.in[k])
		if l := s.lit[k]; l != nil {
			s.Remove(l)
		}
		delete(s.ys, k)
		delete(s.in, k)
		delete(s.lit, k)
	}
	s.shown = true
	u.Invalidate()
}

// light lights the row of key, or dims it.
func (s *shelf) light(key string, on bool) {
	a := s.lit[key]
	if a == nil {
		a = anim.NewFloat(0)
		s.lit[key] = a
		s.Add(a)
	}
	if on {
		a.Animate(1, anim.Snappy)
	} else {
		a.Animate(0, anim.Gentle)
	}
}

// rowAt returns the row at p, and false over none or a caption.
func (s *shelf) rowAt(p geom.Point) (shelfRow, bool) {
	if p.X < 0 || p.X > s.size.W {
		return shelfRow{}, false
	}
	var y float32
	for _, r := range s.rows {
		if p.Y >= y && p.Y < y+r.height() {
			return r, r.kind != shelfCaption
		}
		y += r.height()
	}
	return shelfRow{}, false
}

// Handle implements [gunim.Handler].
func (s *shelf) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerMove:
		if e.Touch {
			return false
		}
		r, _ := s.rowAt(e.Pos)
		s.hover(r.key)
	case input.PointerLeave:
		s.hover("")
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		r, _ := s.rowAt(e.Pos)
		s.down = r.key
	case input.PointerUp:
		r, ok := s.rowAt(e.Pos)
		if ok && r.key == s.down {
			u.Cue(gunim.CueSelect, s)
			s.press(r, u)
		}
		s.down = ""
	case input.DragOver:
		files, tracks, ok := dropped(e.Data)
		if !ok {
			return false
		}
		r, _ := s.rowAt(e.Pos)
		key, goal := s.dropOn(r, files, tracks)
		if key == "" {
			s.unaim()
			return false
		}
		s.aimAt(key, goal)
		u.AnswerDrag(hint(goal))
	case input.DragLeave:
		s.unaim()
	case input.Drop:
		files, tracks, ok := dropped(e.Data)
		s.unaim()
		if !ok {
			return false
		}
		r, _ := s.rowAt(e.Pos)
		if key, _ := s.dropOn(r, files, tracks); key == "" {
			return false
		}
		u.Cue(gunim.CueSelect, s)
		s.land(r, files, tracks, u)
	default:
		return false
	}
	u.Invalidate()
	return true
}

// dropOn returns the row a drag of files or tracks over r drops on,
// and what a drop does there; no row for a drop it refuses. Files over
// any row but a playlist's or Up next join the library.
func (s *shelf) dropOn(r shelfRow, files []string, tracks []int) (key, goal string) {
	it := what(files, tracks)
	switch r.kind {
	case shelfQueue:
		return r.key, "Play " + it + " next"
	case shelfPlaylist:
		return r.key, "Add " + it + " to " + r.title
	case shelfNewPlaylist:
		return r.key, "Make a playlist of " + it
	case shelfAll, shelfCaption, shelfFolder, shelfAddFolder, shelfAddFiles:
	}
	if tracks != nil {
		return "", ""
	}
	return "all", "Add " + it + " to the library"
}

// land drops files or tracks on row r.
func (s *shelf) land(r shelfRow, files []string, tracks []int, u *gunim.UI) {
	switch r.kind {
	case shelfQueue:
		if tracks != nil {
			u.Send(s, Enqueue{Tracks: tracks})
		} else {
			u.Send(s, AddPaths{Paths: files, To: QueueList, At: -1})
		}
	case shelfPlaylist:
		if tracks != nil {
			u.Send(s, AddToPlaylist{ID: r.pid, Tracks: tracks})
		} else {
			u.Send(s, AddPaths{Paths: files, To: r.list, At: -1})
		}
	case shelfNewPlaylist:
		s.lib.newPlaylistOf(tracks, files, u)
	case shelfAll, shelfCaption, shelfFolder, shelfAddFolder, shelfAddFiles:
		u.Send(s, AddPaths{Paths: files, To: AllTracks, At: -1})
	}
}

// aimAt lights row key for a drag over it.
func (s *shelf) aimAt(key, goal string) {
	if key != s.target || s.aim.Target() < 0.5 {
		s.aim.Jump(0)
	}
	s.target, s.goal = key, goal
	s.aim.Animate(1, anim.Spring{Response: 0.3, Damping: 0.6})
}

// unaim puts the light of a drag out.
func (s *shelf) unaim() { s.aim.Animate(0, anim.Gentle) }

// press does what row r is for.
func (s *shelf) press(r shelfRow, u *gunim.UI) {
	switch r.kind {
	case shelfAll, shelfQueue, shelfPlaylist, shelfFolder:
		s.lib.openList(r.list, u)
	case shelfNewPlaylist:
		s.lib.newPlaylist(nil, u)
	case shelfAddFolder:
		u.Send(s, AddFolder{})
	case shelfAddFiles:
		u.Send(s, AddFiles{})
	case shelfCaption:
	}
}

func (s *shelf) hover(key string) {
	if key == s.hot {
		return
	}
	if s.hot != "" {
		s.light(s.hot, false)
	}
	s.hot = key
	if key != "" {
		s.light(key, true)
	}
}

// prepare sets the menu for the row pressed at at.
func (s *shelf) prepare(at geom.Point, u *gunim.UI) bool {
	r, ok := s.rowAt(at)
	if !ok {
		return false
	}
	l := s.lib
	var items menuItems
	switch r.kind {
	case shelfQueue:
		items.add("Play", icon.Play, func(u *gunim.UI) { l.playList(QueueList, u) })
		items.add("Clear Up next", icon.X, func(u *gunim.UI) { u.Send(s, ClearQueue{}) })
	case shelfAll:
		items.add("Play", icon.Play, func(u *gunim.UI) { l.playList(AllTracks, u) })
		items.line()
		items.add("Add files…", icon.FilePlus, func(u *gunim.UI) { u.Send(s, AddFiles{}) })
		items.add("Add a folder…", icon.FolderPlus, func(u *gunim.UI) { u.Send(s, AddFolder{}) })
	case shelfPlaylist:
		items.add("Play", icon.Play, func(u *gunim.UI) { l.playList(r.list, u) })
		items.line()
		items.add("Add files…", icon.FilePlus, func(u *gunim.UI) { u.Send(s, AddFiles{Playlist: r.pid}) })
		items.add("Rename", icon.Pencil, func(u *gunim.UI) {
			l.openList(r.list, u)
			l.startNaming(r.title, u)
		})
		items.add("Delete playlist", icon.Trash2, func(u *gunim.UI) { u.Send(s, DeletePlaylist{ID: r.pid}) })
	case shelfFolder:
		items.add("Play", icon.Play, func(u *gunim.UI) { l.playList(r.list, u) })
		items.line()
		items.add("Stop following this folder", icon.X, func(u *gunim.UI) { u.Send(s, ForgetFolder{Path: r.path}) })
	default:
		return false
	}
	items.set(l.shelfMenu)
	l.picks = items.do
	return true
}

// Layout implements [gunim.Node]: as tall as its rows.
func (s *shelf) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	var h float32 = 16
	for _, r := range s.rows {
		h += r.height()
	}
	s.size = c.Constrain(geom.Sz(c.Max.W, h))
	return s.size
}

// Step implements [gunim.Animator].
func (s *shelf) Step(dt time.Duration) bool { return s.Group.Step(dt) }

// Paint implements [gunim.Node].
func (s *shelf) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	for _, r := range s.rows {
		y := s.ys[r.key].Value()
		grow := s.in[r.key].Value()
		if grow < 0.01 {
			continue
		}
		if r.kind == shelfCaption {
			shaped(r.title, 13, true).Paint(p, geom.Pt(24, y+18), faded(ink, 0.55))
			continue
		}
		row := geom.Rc(10, y+2, box.W-20, rowH-4)
		mid := row.Min.Add(geom.Pt(row.Size().W/2, row.Size().H/2))
		end := p.Push(paint.Scale(0.85+0.15*grow, mid))
		if a := s.lit[r.key]; a != nil && a.Value() > 0.01 {
			p.RRect(row, 12, paint.Solid(faded(ink, 0.07*a.Value()*grow)))
		}
		if l := s.lib; l.page.Value() > 0.001 && r.list == l.open && r.kind != shelfNewPlaylist {
			p.RRect(row, 12, paint.Solid(faded(ink, 0.05*grow)))
		}
		aimed := float32(0)
		if r.key == s.target {
			aimed = s.aim.Value()
		}
		if aimed > 0.005 {
			// The row a drag would drop on swells, ringed in the
			// track's colour, and says what a drop does.
			accent := s.lib.root.bg.accent.Value()
			lift := p.Push(paint.Scale(1+0.04*aimed, mid))
			p.RRect(row, 12, paint.Solid(faded(accent, 0.18*min(aimed, 1))))
			p.RRectStroke(row, 12, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 1.5, Color: faded(accent, min(aimed, 1))})
			lift()
		}
		tile := geom.Rc(row.Min.X+8, y+(rowH-44)/2, 44, 44)
		s.paintTile(p, f, r, tile, grow)
		textX := tile.Max.X + 12
		room := row.Max.X - 12 - textX
		title := faded(ink, 0.92*grow)
		sub := r.sub
		if aimed > 0.5 {
			sub = s.goal
		}
		if sub == "" {
			paintFit(p, r.title, 15, true, geom.Pt(textX, y+21), room, title)
		} else {
			paintFit(p, r.title, 15, true, geom.Pt(textX, y+12), room, title)
			paintFit(p, sub, 12, false, geom.Pt(textX, y+33), room, faded(ink, 0.5*grow))
		}
		end()
	}
}

// paintTile draws a row's picture: the covers of its first tracks, four
// in a square, or its icon.
func (s *shelf) paintTile(p *paint.Painter, f gunim.Frame, r shelfRow, tile geom.Rect, alpha float32) {
	switch {
	case len(r.covers) >= 4:
		half := tile.Size().W / 2
		for i, c := range r.covers[:4] {
			at := geom.Rc(tile.Min.X+float32(i%2)*half, tile.Min.Y+float32(i/2)*half, half, half)
			p.Image(c, at, paint.ImageOpts{Radius: 3, Opacity: alpha})
		}
	case len(r.covers) > 0:
		p.Image(r.covers[0], tile, paint.ImageOpts{Radius: 8, Opacity: alpha})
	default:
		action := r.kind == shelfNewPlaylist || r.kind == shelfAddFolder || r.kind == shelfAddFiles
		fill := faded(ink, 0.08*alpha)
		if action {
			fill = faded(ink, 0.04*alpha)
		}
		p.RRect(tile, 8, paint.Solid(fill))
		side := float32(20)
		ic := geom.Rc(tile.Min.X+(tile.Size().W-side)/2, tile.Min.Y+(tile.Size().H-side)/2, side, side)
		widget.PaintIcon(p, f.Theme, r.icon, ic, faded(ink, 0.8*alpha))
	}
}
