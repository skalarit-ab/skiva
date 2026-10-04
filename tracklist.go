package main

import (
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// library is the library: beside the track playing, or as a sheet over
// it on a narrow window. Its front page, the shelf, lists the whole
// library, the playlists and the folders followed; a list picked
// slides in over it, and its back button slides it away.
type library struct {
	anim.Group
	root  *playerRoot
	state Player
	byID  map[int]Track

	shelf       *shelf
	shelfScroll *widget.Scroll
	shelfMenu   *widget.ContextMenu
	list        *trackList
	listScroll  *widget.Scroll
	listMenu    *widget.ContextMenu
	// page slides a list in over the shelf, 0 the shelf and 1 the list
	// open, which stays named as it slides away.
	page *anim.Float
	open ListID
	// pending is a playlist made here that the application has yet to
	// show.
	pending string
	back    *iconButton
	more    *iconButton
	menu    *widget.ContextMenu
	picks   []func(*gunim.UI)
	// name renames a playlist, shown in place of its title while
	// naming.
	name   *widget.TextField
	naming bool
	// want is the title of a list to open as soon as it is known.
	want string

	// viewH is the height the lists show, and at where the heading
	// starts, clear of a phone's bars.
	viewH float32
	at    geom.Point
	size  geom.Size
}

func newLibrary(r *playerRoot) *library {
	l := &library{root: r, page: anim.NewFloat(0), byID: map[int]Track{}}
	l.Add(l.page)
	l.shelf = newShelf(l)
	l.shelfMenu = widget.NewContextMenu(l.shelf)
	l.shelfMenu.Prepare = l.shelf.prepare
	l.shelfMenu.Picked = l.pick
	l.shelfScroll = widget.NewScroll(l.shelfMenu)
	l.list = newTrackList(r)
	l.listMenu = widget.NewContextMenu(l.list)
	l.listMenu.Prepare = l.list.prepare
	l.listMenu.Picked = l.pick
	l.listScroll = widget.NewScroll(l.listMenu)
	l.back = newIconButton(icon.ChevronLeft, 36, func(u *gunim.UI) { l.close(u) })
	l.more = newIconButton(icon.Ellipsis, 36, func(u *gunim.UI) { l.openMore(u) })
	l.menu = widget.NewContextMenu(l.more)
	l.menu.Picked = l.pick
	l.name = widget.NewTextField()
	l.name.Keys = func(k input.KeyPress, u *gunim.UI) bool {
		switch k.Key {
		case input.KeyEnter, input.KeyKPEnter:
			l.endNaming(true, u)
		case input.KeyEscape:
			l.endNaming(false, u)
		default:
			return false
		}
		return true
	}
	return l
}

func (l *library) show(s Player, u *gunim.UI) {
	l.state = s
	clear(l.byID)
	for _, t := range s.Tracks {
		l.byID[t.ID] = t
	}
	if l.pending != "" && l.findPlaylist(l.pending) != nil {
		l.pending = ""
	}
	// A list gone, as a playlist deleted, slides away.
	if l.page.Target() > 0.5 && !l.has(l.open) {
		l.close(u)
	}
	l.shelf.show(s, u)
	if l.want != "" {
		for _, r := range l.shelf.rows {
			if r.title == l.want && r.kind != shelfCaption {
				l.want = ""
				l.openList(r.list, u)
				l.page.Jump(1)
			}
		}
	}
	l.showList(u)
}

// has says whether the list id is there to show.
func (l *library) has(id ListID) bool {
	s := string(id)
	switch {
	case strings.HasPrefix(s, "p:"):
		return s[2:] == l.pending || l.findPlaylist(s[2:]) != nil
	case strings.HasPrefix(s, "f:"):
		return slices.ContainsFunc(l.state.Folders, func(f Folder) bool { return f.Path == s[2:] })
	}
	return true
}

func (l *library) findPlaylist(id string) *Playlist {
	for i := range l.state.Playlists {
		if l.state.Playlists[i].ID == id {
			return &l.state.Playlists[i]
		}
	}
	return nil
}

// listOf returns the list id's title and its tracks.
func (l *library) listOf(id ListID) (title string, ids []int) {
	s := string(id)
	switch {
	case strings.HasPrefix(s, "p:"):
		if p := l.findPlaylist(s[2:]); p != nil {
			return p.Name, p.Tracks
		}
		return "", nil
	case strings.HasPrefix(s, "f:"):
		for _, f := range l.state.Folders {
			if f.Path == s[2:] {
				return filepath.Base(f.Path), f.Tracks
			}
		}
		return "", nil
	}
	return "All tracks", l.state.Library
}

// showList gives the list page the list open.
func (l *library) showList(u *gunim.UI) {
	_, ids := l.listOf(l.open)
	l.list.show(ids, l.byID, l.open, l.state.Current, u)
}

// openList slides the list id in over the shelf.
func (l *library) openList(id ListID, u *gunim.UI) {
	if l.open != id {
		l.endNaming(false, u)
		l.open = id
		l.listScroll.JumpTo(0)
	}
	l.showList(u)
	l.page.Animate(1, pageSpring)
	u.Invalidate()
}

// close slides the list away, back to the shelf.
func (l *library) close(u *gunim.UI) {
	l.endNaming(false, u)
	l.page.Animate(0, pageSpring)
	u.Invalidate()
}

// newPlaylist makes a playlist of ids, or an empty one, opens it and
// asks for its name.
func (l *library) newPlaylist(ids []int, u *gunim.UI) {
	id := fmt.Sprintf("%x", time.Now().UnixNano())
	n := len(l.state.Playlists) + 1
	name := fmt.Sprintf("Playlist %d", n)
	for slices.ContainsFunc(l.state.Playlists, func(p Playlist) bool { return p.Name == name }) {
		n++
		name = fmt.Sprintf("Playlist %d", n)
	}
	l.pending = id
	u.Send(l.root, NewPlaylist{ID: id, Name: name, Tracks: ids})
	if len(ids) > 0 {
		return
	}
	l.openList(PlaylistList(id), u)
	l.startNaming(name, u)
}

// startNaming puts a field in place of the open playlist's title.
func (l *library) startNaming(name string, u *gunim.UI) {
	l.naming = true
	l.name.SetText(name)
	l.name.Select(0, len(name))
	u.Focus(l.name)
	u.Invalidate()
}

// endNaming takes the field away, renaming the playlist if keep.
func (l *library) endNaming(keep bool, u *gunim.UI) {
	if !l.naming {
		return
	}
	l.naming = false
	if s := string(l.open); keep && strings.HasPrefix(s, "p:") && strings.TrimSpace(l.name.Text()) != "" {
		u.Send(l.root, RenamePlaylist{ID: s[2:], Name: l.name.Text()})
	}
	u.Focus(l.root)
	u.Invalidate()
}

// openMore opens the open list's menu under its button.
func (l *library) openMore(u *gunim.UI) {
	var items menuItems
	s := string(l.open)
	switch {
	case strings.HasPrefix(s, "p:"):
		id := s[2:]
		items.add("Add files…", icon.FilePlus, func(u *gunim.UI) { u.Send(l.root, AddFiles{Playlist: id}) })
		items.add("Rename", icon.Pencil, func(u *gunim.UI) {
			if p := l.findPlaylist(id); p != nil {
				l.startNaming(p.Name, u)
			}
		})
		items.add("Delete playlist", icon.Trash2, func(u *gunim.UI) { u.Send(l.root, DeletePlaylist{ID: id}) })
	case strings.HasPrefix(s, "f:"):
		path := s[2:]
		items.add("Stop following this folder", icon.X, func(u *gunim.UI) { u.Send(l.root, ForgetFolder{Path: path}) })
	default:
		items.add("Add files…", icon.FilePlus, func(u *gunim.UI) { u.Send(l.root, AddFiles{}) })
		items.add("Add a folder…", icon.FolderPlus, func(u *gunim.UI) { u.Send(l.root, AddFolder{}) })
	}
	items.set(l.menu)
	l.picks = items.do
	l.menu.Open(geom.Pt(0, 40), u)
}

// pick does what the menu item i picked says.
func (l *library) pick(i int, u *gunim.UI) {
	if i >= 0 && i < len(l.picks) && l.picks[i] != nil {
		l.picks[i](u)
	}
}

// menuItems gathers a menu's items, its captions and what each does.
type menuItems struct {
	items    []string
	icons    []*icon.Icon
	captions []int
	breaks   []int
	do       []func(*gunim.UI)
}

func (m *menuItems) add(s string, ic *icon.Icon, do func(*gunim.UI)) {
	m.items = append(m.items, s)
	m.icons = append(m.icons, ic)
	m.do = append(m.do, do)
}

func (m *menuItems) caption(s string) {
	m.captions = append(m.captions, len(m.items))
	m.add(s, nil, nil)
}

func (m *menuItems) line() { m.breaks = append(m.breaks, len(m.items)) }

// set puts the items in menu.
func (m *menuItems) set(menu *widget.ContextMenu) {
	menu.Items, menu.Icons, menu.Captions, menu.Breaks = m.items, m.icons, m.captions, m.breaks
	menu.Checked, menu.Disabled, menu.Hints = nil, nil, nil
}

// headH is the height of the heading over the lists.
const headH = 76

// Children implements [gunim.Composite].
func (l *library) Children() []gunim.Node {
	return []gunim.Node{l.shelfScroll, l.listScroll, l.back, l.menu, l.name}
}

// Step implements [gunim.Animator].
func (l *library) Step(dt time.Duration) bool { return l.Group.Step(dt) }

// Layout implements [gunim.Node].
func (l *library) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	l.size = size
	// The glass runs under a phone's bars; the heading and the lists
	// keep clear of them: of the status bar only beside the track
	// playing, as a sheet starts below it.
	l.at = geom.Pt(f.Safe.Left, 0)
	if !l.root.narrow {
		l.at.Y = f.Safe.Top
	}
	w := size.W - l.at.X - f.Safe.Right
	l.viewH = max(size.H-l.at.Y-headH-f.Safe.Bottom, 0)
	page := l.page.Value()
	away := geom.Pt(-10000, 0)
	shelf, list, back, more, name := kids.At(0), kids.At(1), kids.At(2), kids.At(3), kids.At(4)
	// The list slides in from the right, and the shelf drifts left
	// under it.
	shelf.Layout(gunim.Tight(geom.Sz(w, l.viewH)))
	list.Layout(gunim.Tight(geom.Sz(w, l.viewH)))
	top := l.at.Y + headH
	if page < 0.999 {
		shelf.Place(geom.Pt(l.at.X-0.3*w*page, top))
	} else {
		shelf.Place(away)
	}
	if page > 0.001 {
		list.Place(geom.Pt(l.at.X+w*(1-page), top))
	} else {
		list.Place(away)
	}
	back.Layout(gunim.Tight(geom.Sz(36, 36)))
	more.Layout(gunim.Tight(geom.Sz(36, 36)))
	if page > 0.001 {
		back.Place(geom.Pt(l.at.X+12+w*(1-page), l.at.Y+18))
	} else {
		back.Place(away)
	}
	// The menu button serves the shelf and the lists alike.
	more.Place(geom.Pt(l.at.X+w-48, l.at.Y+18))
	name.Layout(gunim.Tight(geom.Sz(max(w-48-64, 40), 36)))
	if l.naming && page > 0.5 {
		name.Place(geom.Pt(l.at.X+52, l.at.Y+18))
	} else {
		name.Place(away)
	}
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
	page := l.page.Value()
	w := box.W - l.at.X
	// While a list slides, what slides is cut at the library's edges.
	if page > 0.001 && page < 0.999 {
		defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Clip: true})()
	}
	// The shelf's heading drifts left and fades as a list comes in; the
	// list's slides in with it.
	if page < 0.999 {
		at := l.at.Add(geom.Pt(-0.3*w*page, 0))
		a := 1 - page
		shaped("Library", 22, true).Paint(p, at.Add(geom.Pt(24, 22)), faded(ink, a))
		what := fmt.Sprintf("%d tracks", len(l.state.Library))
		if l.state.Scanning {
			what += " · reading folders…"
		}
		shaped(what, 12, false).Paint(p, at.Add(geom.Pt(24, 50)), faded(ink, 0.5*a))
	}
	if page > 0.001 {
		at := l.at.Add(geom.Pt(w*(1-page), 0))
		title, ids := l.listOf(l.open)
		room := w - 52 - 64
		if !l.naming {
			paintFit(p, title, 20, true, at.Add(geom.Pt(56, 22)), room, ink)
		}
		what := fmt.Sprintf("%d tracks", len(ids))
		if n := len(ids); n == 1 {
			what = "1 track"
		}
		var length time.Duration
		for _, id := range ids {
			length += l.byID[id].Length
		}
		if length > 0 {
			what += " · " + span(length)
		}
		shaped(what, 12, false).Paint(p, at.Add(geom.Pt(56, 50)), faded(ink, 0.5))
	}
	kids.At(0).Paint(p)
	kids.At(1).Paint(p)
	kids.At(2).Paint(p)
	kids.At(3).Paint(p)
	kids.At(4).Paint(p)
}

// span writes a list's length in hours and minutes.
func span(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d s", int(d.Round(time.Second)/time.Second))
	}
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

// rowH is the height of a track's row.
const rowH = 60

// gripW is the width of the grip a playlist's row is moved by.
const gripW = 44

// trackList draws a list's tracks, one a row: the cover, the title,
// the artist and the length. The row playing is lit, with bars that
// move with the music where its length was. A playlist's rows are
// moved by their grips, the other rows making way.
type trackList struct {
	anim.Group
	root   *playerRoot
	ids    []int
	byID   map[int]Track
	list   ListID
	cur    int
	tracks []Track
	// hot is the row the pointer is over, and lit how lit each row is,
	// by place.
	hot  int
	lit  map[int]*anim.Float
	down int
	// moving is the row being moved by its grip, -1 for none: from
	// where, grabbed how far down it, and where the pointer is. to is
	// where it would land, and shift how far each other row has made
	// way, in rows.
	moving  int
	grab, y float32
	to      int
	shift   map[int]*anim.Float
	size    geom.Size
}

func newTrackList(r *playerRoot) *trackList {
	return &trackList{root: r, hot: -1, down: -1, moving: -1, lit: map[int]*anim.Float{}, shift: map[int]*anim.Float{}}
}

// editable says whether the list is a playlist, whose rows move.
func (t *trackList) editable() bool { return strings.HasPrefix(string(t.list), "p:") }

func (t *trackList) show(ids []int, byID map[int]Track, list ListID, cur int, u *gunim.UI) {
	if list != t.list {
		t.moving = -1
		for _, s := range t.shift {
			s.Jump(0)
		}
	}
	t.ids, t.byID, t.list, t.cur = ids, byID, list, cur
	t.tracks = t.tracks[:0]
	for _, id := range ids {
		t.tracks = append(t.tracks, byID[id])
	}
	u.Invalidate()
}

// light lights row i, or dims it.
func (t *trackList) light(i int, on bool) {
	a := t.lit[i]
	if a == nil {
		a = anim.NewFloat(0)
		t.lit[i] = a
		t.Add(a)
	}
	if on {
		a.Animate(1, anim.Snappy)
	} else {
		a.Animate(0, anim.Gentle)
	}
}

// shiftOf returns how far row i has made way, in rows.
func (t *trackList) shiftOf(i int) *anim.Float {
	a := t.shift[i]
	if a == nil {
		a = anim.NewFloat(0)
		t.shift[i] = a
		t.Add(a)
	}
	return a
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

// onGrip says whether p is on a playlist row's grip.
func (t *trackList) onGrip(p geom.Point) bool {
	return t.editable() && p.X > t.size.W-10-gripW && t.rowAt(p) >= 0
}

// DragsTouch implements [gunim.TouchDragger]: a finger on a grip moves
// its row, and anywhere else scrolls.
func (t *trackList) DragsTouch() bool { return t.moving >= 0 }

// Handle implements [gunim.Handler].
func (t *trackList) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerMove:
		if t.moving >= 0 {
			t.drag(e.Pos.Y)
			break
		}
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
		if t.onGrip(e.Pos) {
			t.moving, t.to = t.down, t.down
			t.grab = e.Pos.Y - float32(t.down)*rowH
			t.y = e.Pos.Y
			u.Cue(gunim.CueSelect, t)
		}
	case input.PointerUp:
		if t.moving >= 0 {
			t.drop(u)
			break
		}
		i := t.rowAt(e.Pos)
		if i >= 0 && i == t.down {
			u.Cue(gunim.CueSelect, t)
			u.Send(t, PlayTrack{ID: t.tracks[i].ID, From: t.list})
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

// drag moves the row moving to y, and the rows it passes make way.
func (t *trackList) drag(y float32) {
	t.y = y
	to := int((y - t.grab + rowH/2) / rowH)
	to = max(0, min(to, len(t.tracks)-1))
	if to == t.to {
		return
	}
	t.to = to
	for i := range t.tracks {
		var s float32
		switch {
		case i == t.moving:
			continue
		case t.moving < i && i <= to:
			s = -1
		case to <= i && i < t.moving:
			s = 1
		}
		t.shiftOf(i).Animate(s, anim.Spring{Response: 0.25, Damping: 0.85})
	}
}

// drop lets the row moving go where it is, and tells the application.
func (t *trackList) drop(u *gunim.UI) {
	from, to := t.moving, t.to
	t.moving, t.down = -1, -1
	// The row lands where it was let go, and the others hold where they
	// made way, so the list shows its new order at once.
	land := t.y - t.grab - float32(to)*rowH
	for _, s := range t.shift {
		s.Jump(0)
	}
	if from == to {
		t.shiftOf(to).Jump(land / rowH)
		t.shiftOf(to).Animate(0, anim.Spring{Response: 0.3, Damping: 0.8})
		return
	}
	ids := slices.Clone(t.ids)
	id := ids[from]
	ids = slices.Insert(slices.Delete(ids, from, from+1), to, id)
	t.show(ids, t.byID, t.list, t.cur, u)
	t.shiftOf(to).Jump(land / rowH)
	t.shiftOf(to).Animate(0, anim.Spring{Response: 0.3, Damping: 0.8})
	u.Cue(gunim.CueTick, t)
	if s := string(t.list); strings.HasPrefix(s, "p:") {
		u.Send(t, MoveInPlaylist{ID: s[2:], From: from, To: to})
	}
}

func (t *trackList) hover(i int) {
	if i == t.hot {
		return
	}
	if t.hot >= 0 {
		t.light(t.hot, false)
	}
	t.hot = i
	if i >= 0 {
		t.light(i, true)
	}
}

// prepare sets the menu for the row pressed at at: to add its track to
// a playlist, or take it off the playlist open.
func (t *trackList) prepare(at geom.Point, u *gunim.UI) bool {
	i := t.rowAt(at)
	if i < 0 || t.moving >= 0 {
		return false
	}
	l := t.root.lib
	id := t.tracks[i].ID
	var items menuItems
	items.add("Play", icon.Play, func(u *gunim.UI) { u.Send(t, PlayTrack{ID: id, From: t.list}) })
	items.line()
	items.caption("Add to playlist")
	for _, p := range l.state.Playlists {
		pid := p.ID
		items.add(p.Name, icon.ListMusic, func(u *gunim.UI) { u.Send(t, AddToPlaylist{ID: pid, Tracks: []int{id}}) })
	}
	items.add("New playlist", icon.Plus, func(u *gunim.UI) { l.newPlaylist([]int{id}, u) })
	if s := string(t.list); strings.HasPrefix(s, "p:") {
		items.line()
		items.add("Remove from this playlist", icon.X, func(u *gunim.UI) {
			u.Send(t, RemoveFromPlaylist{ID: s[2:], At: i})
		})
	}
	items.set(l.listMenu)
	l.picks = items.do
	return true
}

// Layout implements [gunim.Node]: as tall as its rows.
func (t *trackList) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	t.size = c.Constrain(geom.Sz(c.Max.W, float32(len(t.tracks))*rowH+16))
	return t.size
}

// Paint implements [gunim.Node].
func (t *trackList) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	if len(t.tracks) == 0 {
		msg := "Tracks added show up here"
		if t.editable() {
			msg = "Right-click or hold a track to add it here"
		}
		run := shaped(msg, 14, false)
		run.Paint(p, geom.Pt((box.W-run.Advance)/2, 40), faded(ink, 0.45))
		return
	}
	// Only the rows in view are drawn, for a library of thousands.
	top := t.root.lib.listScroll.Offset()
	first := max(int(top/rowH)-1, 0)
	last := min(int((top+t.root.lib.viewH)/rowH)+1, len(t.tracks)-1)
	for i := first; i <= last; i++ {
		if i == t.moving {
			continue
		}
		y := float32(i) * rowH
		if s := t.shift[i]; s != nil {
			y += s.Value() * rowH
		}
		t.paintRow(p, f, i, y, box, false)
	}
	if t.moving >= 0 {
		t.paintRow(p, f, t.moving, t.y-t.grab, box, true)
	}
}

// paintRow draws row i at y, lifted as it is moved.
func (t *trackList) paintRow(p *paint.Painter, f gunim.Frame, i int, y float32, box geom.Size, lifted bool) {
	tr := t.tracks[i]
	row := geom.Rc(10, y+2, box.W-20, rowH-4)
	playing := tr.ID == t.cur
	if lifted {
		p.ShadowRRect(row, 12, paint.Solid(mix(night, ink, 0.12)),
			paint.Shadow{Blur: 18, Offset: geom.Pt(0, 6), Color: faded(night, 0.6)})
	}
	if a := t.lit[i]; a != nil && a.Value() > 0.01 {
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
	hot := float32(0)
	if a := t.lit[i]; a != nil {
		hot = a.Value()
	}
	if t.editable() && (hot > 0.01 || lifted) {
		// A playlist's row shows its grip as the pointer comes over it.
		if lifted {
			hot = 1
		}
		g := geom.Rc(row.Max.X-gripW+10, y+(rowH-20)/2, 20, 20)
		widget.PaintIcon(p, f.Theme, icon.GripVertical, g, faded(ink, 0.7*hot))
		return
	}
	if playing {
		t.paintBars(p, geom.Pt(row.Max.X-34, y+rowH/2), tr)
	} else if tr.Length > 0 {
		run := shaped(clock(tr.Length), 12, false)
		run.Paint(p, geom.Pt(row.Max.X-12-run.Advance, y+22), faded(ink, 0.45))
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

// playList plays the list id from its first track.
func (l *library) playList(id ListID, u *gunim.UI) {
	if _, ids := l.listOf(id); len(ids) > 0 {
		u.Send(l.root, PlayTrack{ID: ids[0], From: id})
	}
}

// pageSpring slides a list in and out: quick, and settling without
// passing its place, so the library never shows past a list's edge.
var pageSpring = anim.Spring{Response: 0.36, Damping: 1}
