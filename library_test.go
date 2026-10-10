package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
)

// copyTone puts the test tone at path, as a file changed long ago.
func copyTone(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile("testdata/tone.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

// age makes dir look changed long ago, as a folder settled is.
func age(t *testing.T, dir string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
}

// changed makes dir look changed at now, on the test's clock. A
// folder's time of change is the system's, which runs behind the
// test's clock, and Windows may leave it as it was where a folder
// changes again within a few milliseconds.
func changed(t *testing.T, dir string, now time.Time) {
	t.Helper()
	if err := os.Chtimes(dir, now, now); err != nil {
		t.Fatal(err)
	}
}

func TestAFollowedFolderTellsOfTracksAddedChangedAndTaken(t *testing.T) {
	root := t.TempDir()
	copyTone(t, filepath.Join(root, "a.mp3"))
	copyTone(t, filepath.Join(root, "Album", "b.mp3"))
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := newPoller(root)
	now := time.Now()
	read, gone := p.poll(now)
	slices.Sort(read)
	want := []string{filepath.Join(root, "Album", "b.mp3"), filepath.Join(root, "a.mp3")}
	if !slices.Equal(read, want) || len(gone) != 0 {
		t.Fatalf("the first poll read %v and lost %v, want %v and nothing", read, gone, want)
	}
	age(t, root)
	age(t, filepath.Join(root, "Album"))
	if read, lost := p.poll(now.Add(pollEvery)); len(read)+len(lost) != 0 {
		t.Fatalf("a poll with nothing changed read %v and lost %v", read, lost)
	}

	// A file arriving is read once it has lain still for settle.
	c := filepath.Join(root, "New", "c.mp3")
	copyTone(t, c)
	now = now.Add(2 * pollEvery)
	if read, _ := p.poll(now); !slices.Equal(read, []string{c}) {
		t.Fatalf("a new folder's track: read %v, want %v", read, []string{c})
	}

	// A file still being written waits until it is whole.
	d := filepath.Join(root, "d.mp3")
	if err := os.WriteFile(d, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	now = now.Add(pollEvery)
	if err := os.Chtimes(d, now, now); err != nil {
		t.Fatal(err)
	}
	changed(t, root, now)
	if read, _ := p.poll(now); slices.Contains(read, d) {
		t.Fatal("a file written a moment ago was read at once")
	}
	copyTone(t, d)
	now = now.Add(pollEvery)
	if read, _ := p.poll(now); slices.Contains(read, d) {
		t.Fatal("a file that changed since the last poll was read before it settled")
	}
	now = now.Add(pollEvery)
	if read, _ := p.poll(now); !slices.Contains(read, d) {
		t.Fatalf("a file whole and still for a poll was not read: %v", read)
	}

	// A track taken away, and a folder taken away with its tracks.
	if err := os.Remove(filepath.Join(root, "a.mp3")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "Album")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(pollEvery)
	changed(t, root, now)
	_, gone = p.poll(now)
	slices.Sort(gone)
	want = []string{filepath.Join(root, "Album", "b.mp3"), filepath.Join(root, "a.mp3")}
	if !slices.Equal(gone, want) {
		t.Fatalf("lost %v, want %v", gone, want)
	}
}

// ids returns the library's tracks in a test app.
func ids(a *app) []int { return slices.Clone(a.Library) }

func TestPlaylistsAreMadeEditedAndKept(t *testing.T) {
	file := filepath.Join(t.TempDir(), "library.json")
	a := newApp(context.Background(), newDeck(audio.NewMixer()), file)
	all := ids(a)
	a.handle(NewPlaylist{ID: "p1", Name: "Road", Tracks: []int{all[2]}})
	a.handle(AddToPlaylist{ID: "p1", Tracks: []int{all[0], all[3]}})
	a.handle(MoveInPlaylist{ID: "p1", From: 2, To: 0})
	a.handle(RemoveFromPlaylist{ID: "p1", At: 1})
	a.handle(RenamePlaylist{ID: "p1", Name: "  Road trip "})
	a.handle(NewPlaylist{ID: "p2", Name: "Gone"})
	a.handle(DeletePlaylist{ID: "p2"})
	want := Playlist{ID: "p1", Name: "Road trip", Tracks: []int{all[3], all[0]}}
	if len(a.Playlists) != 1 || !samePlaylist(a.Playlists[0], want) {
		t.Fatalf("playlists %v, want %v", a.Playlists, want)
	}
	a.save()

	b := newApp(context.Background(), newDeck(audio.NewMixer()), file)
	kept, ok := loadSaved(file)
	if !ok {
		t.Fatal("the library was not kept")
	}
	b.kept = kept
	b.refresh()
	if len(b.Playlists) != 1 || !samePlaylist(b.Playlists[0], want) {
		t.Fatalf("after a restart the playlists are %v, want %v", b.Playlists, want)
	}
}

func samePlaylist(a, b Playlist) bool {
	return a.ID == b.ID && a.Name == b.Name && slices.Equal(a.Tracks, b.Tracks)
}

func TestNextGoesAlongThePlaylistPlaying(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	a.handle(NewPlaylist{ID: "p", Name: "Two", Tracks: []int{all[3], all[1]}})
	a.handle(PlayTrack{ID: all[3], From: PlaylistList("p")})
	a.handle(Skip{})
	if a.Current != all[1] {
		t.Fatalf("Next on a playlist went to %d, want its second track, %d", a.Current, all[1])
	}
	a.ended()
	if a.Current != 0 {
		t.Fatalf("the playlist's last track ended, repeat off: playing %d, want nothing", a.Current)
	}
}

func TestAFolderForgottenTakesItsTracksAllButThoseOnAPlaylist(t *testing.T) {
	root := t.TempDir()
	one, two := filepath.Join(root, "one.mp3"), filepath.Join(root, "two.mp3")
	copyTone(t, one)
	copyTone(t, two)
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	a.kept.Folders = []string{root}
	for _, path := range []string{one, two} {
		e := readEntry(path)
		a.apply(change{root: root, e: e})
	}
	a.refresh()
	if len(a.Folders) != 1 || len(a.Folders[0].Tracks) != 2 {
		t.Fatalf("folders %v, want the folder with its two tracks", a.Folders)
	}
	a.handle(NewPlaylist{ID: "p", Name: "Keep", Tracks: []int{a.byKey[one].ID}})
	a.handle(ForgetFolder{Path: root})
	if a.byKey[two] != nil || a.byKey[one] == nil {
		t.Fatalf("after forgetting the folder: its other track kept %v, the playlist's kept %v; want only the playlist's",
			a.byKey[two] != nil, a.byKey[one] != nil)
	}
	if len(a.Folders) != 0 || len(a.Playlists[0].Tracks) != 1 {
		t.Fatalf("folders %v and playlist %v, want none and the one track", a.Folders, a.Playlists[0])
	}
}

func TestAFileAddedOnItsOwnLeavesTheLibraryAllButItsPlaylists(t *testing.T) {
	dir := t.TempDir()
	one, two := filepath.Join(dir, "one.mp3"), filepath.Join(dir, "two.mp3")
	copyTone(t, one)
	copyTone(t, two)
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	a.handle(AddPaths{Paths: []string{one, two}, To: AllTracks, At: -1})
	readAll(t, a)
	if a.byKey[one] == nil || a.byKey[two] == nil {
		t.Fatal("the files added are not in the library")
	}
	added := func(path string) bool {
		i := slices.Index(a.Library, a.byKey[path].ID)
		return i >= 0 && a.Tracks[i].Added
	}
	if !added(one) || !added(two) {
		t.Fatal("the files added on their own are not marked so")
	}
	a.handle(NewPlaylist{ID: "p", Name: "Keep", Tracks: []int{a.byKey[one].ID}})
	a.handle(RemoveFromLibrary{ID: a.byKey[one].ID})
	a.handle(RemoveFromLibrary{ID: a.byKey[two].ID})
	if a.byKey[two] != nil || slices.Contains(a.kept.Files, two) {
		t.Fatal("the file removed is still in the library")
	}
	// The one on a playlist stays known for it, but is no longer one
	// added on its own.
	if a.byKey[one] == nil || len(a.Playlists[0].Tracks) != 1 || added(one) || slices.Contains(a.kept.Files, one) {
		t.Fatalf("the playlist holds %v, and its track is still added on its own: %v", a.Playlists[0].Tracks, a.byKey[one] != nil && added(one))
	}
}

func TestARowsMenuRemovesAFileAddedOnItsOwnFromTheLibrary(t *testing.T) {
	s := library4()
	s.Tracks[1].Added = true
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	tap(w, run, geom.Pt(120, shelfRowY(root, "all")))
	run(40)
	l := root.lib
	labels := func(row int, u *gunim.UI) []string {
		if !l.list.prepare(geom.Pt(100, float32(row)*rowH+rowH/2), u) {
			t.Fatalf("row %d has no menu", row)
		}
		items := make([]string, 0, len(l.listMenu.Items()))
		for _, it := range l.listMenu.Items() {
			items = append(items, it.Label)
		}
		return items
	}
	withUI(t, w, run, func(u *gunim.UI) {
		if slices.Contains(labels(0, u), "Remove from library") {
			t.Fatal("a demo song offers to leave the library")
		}
		items := labels(1, u)
		i := slices.Index(items, "Remove from library")
		if i < 0 {
			t.Fatalf("the menu of a file added on its own holds %q, without Remove from library", items)
		}
		l.pick(i, u)
	})
	if got := intents(w); len(got) != 1 || got[0] != (RemoveFromLibrary{ID: 2}) {
		t.Fatalf("Remove from library sent %v, want track 2 removed", got)
	}
}

func TestATrackDeletedLeavesTheLibraryButPlaysOn(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "one.mp3")
	copyTone(t, path)
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	a.kept.Folders = []string{root}
	a.apply(change{root: root, e: readEntry(path)})
	a.refresh()
	id := a.byKey[path].ID
	a.handle(PlayTrack{ID: id})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	a.apply(change{root: root, gone: path})
	a.refresh()
	if slices.Contains(a.Library, id) {
		t.Fatal("a track deleted is still in the library")
	}
	if !slices.ContainsFunc(a.Tracks, func(tr Track) bool { return tr.ID == id }) {
		t.Fatal("the track playing, deleted, is gone from the tracks the window knows while it plays")
	}
	a.handle(PlayTrack{ID: a.Library[0]})
	a.refresh()
	if slices.ContainsFunc(a.Tracks, func(tr Track) bool { return tr.ID == id }) {
		t.Fatal("the track deleted is still known once another plays")
	}
}

func withPlaylist(s Player) Player {
	s.Playlists = []Playlist{{ID: "p", Name: "Mix", Tracks: []int{1, 2, 3}}}
	return s
}

// shelfRowY returns the middle of the shelf's row of key, in the
// window.
func shelfRowY(r *playerRoot, key string) float32 {
	var y float32 = headH
	for _, row := range r.lib.shelf.rows {
		if row.key == key {
			return y + row.height()/2
		}
		y += row.height()
	}
	return -1
}

func TestAListSlidesInOverTheShelfEveryFrameInsideTheLibrary(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), withPlaylist(library4()))
	tap(w, run, geom.Pt(120, shelfRowY(root, "p:p")))
	last := float32(sideWidth + 1)
	for f := range 40 {
		b := boundsOf(t, w, run, root.lib.listScroll)
		if b.Min.X > last+0.01 || b.Min.X < -0.01 {
			t.Fatalf("frame %d: the list is at x %v, after %v; want it sliding left to 0, never past", f, b.Min.X, last)
		}
		last = b.Min.X
	}
	if last > 0.5 {
		t.Fatalf("after 40 frames the list is at x %v, want 0", last)
	}
	if root.lib.open != PlaylistList("p") || len(root.lib.list.tracks) != 3 {
		t.Fatalf("open %q with %d tracks, want the playlist's three", root.lib.open, len(root.lib.list.tracks))
	}
	back := boundsOf(t, w, run, root.lib.back)
	tap(w, run, back.Min.Add(geom.Pt(18, 18)))
	run(40)
	if v := root.lib.page.Value(); v > 0.01 {
		t.Fatalf("40 frames after back, the list is %v in", v)
	}
}

func TestAPlaylistsRowMovesByItsGrip(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), withPlaylist(library4()))
	tap(w, run, geom.Pt(120, shelfRowY(root, "p:p")))
	run(40)
	list := boundsOf(t, w, run, root.lib.listScroll)
	grip := geom.Pt(list.Max.X-30, list.Min.Y+rowH/2)
	w.Input(input.PointerMove{Pos: grip})
	w.Input(input.PointerDown{Pos: grip, Button: input.ButtonPrimary, Clicks: 1})
	run(1)
	// Down past two rows, a frame at a time: the row follows the
	// pointer, and the rows it passes make way.
	for i := 1; i <= 20; i++ {
		at := grip.Add(geom.Pt(0, float32(i)*2*rowH/20))
		w.Input(input.PointerMove{Pos: at})
		run(1)
		if got := root.lib.list.y - root.lib.list.grab; got < float32(i)*2*rowH/20-1 || got > float32(i)*2*rowH/20+1 {
			t.Fatalf("step %d: the row moving is at %v, want it under the pointer at %v", i, got, float32(i)*2*rowH/20)
		}
	}
	end := grip.Add(geom.Pt(0, 2*rowH))
	w.Input(input.PointerUp{Pos: end, Button: input.ButtonPrimary})
	run(1)
	got := intents(w)
	if len(got) != 1 {
		t.Fatalf("a row moved two down sent %v", got)
	}
	if m, ok := got[0].(MoveInPlaylist); !ok || m != (MoveInPlaylist{ID: "p", From: 0, To: 2}) {
		t.Fatalf("a row moved two down sent %v, want MoveInPlaylist from 0 to 2", got[0])
	}
	if ids := root.lib.list.ids; !slices.Equal(ids, []int{2, 3, 1}) {
		t.Fatalf("the list shows %v at once, want 2 3 1", ids)
	}
}

func TestANewPlaylistOpensAndTakesItsName(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), library4())
	_ = w.Client().Focus("player")
	run(1)
	tap(w, run, geom.Pt(120, shelfRowY(root, "new")))
	run(40)
	got := intents(w)
	if len(got) != 1 {
		t.Fatalf("New playlist sent %v", got)
	}
	np, ok := got[0].(NewPlaylist)
	if !ok || np.Name != "Playlist 1" || np.ID == "" {
		t.Fatalf("New playlist sent %v, want a NewPlaylist named Playlist 1", got[0])
	}
	if root.lib.open != PlaylistList(np.ID) || !root.lib.naming {
		t.Fatalf("open %q, naming %v; want the new playlist open, its name asked for", root.lib.open, root.lib.naming)
	}
	// What is typed is the name's, Space too, not the player's.
	w.Input(input.KeyPress{Key: input.KeySpace})
	w.Input(input.TextInput{Text: "Road trip"})
	w.Input(input.KeyPress{Key: input.KeyEnter})
	run(2)
	got = intents(w)
	if len(got) != 1 || got[0] != (RenamePlaylist{ID: np.ID, Name: "Road trip"}) {
		t.Fatalf("typing a name and Enter sent %v, want only RenamePlaylist to Road trip", got)
	}
	if root.lib.naming {
		t.Fatal("Enter left the name being asked for")
	}
}

var _ gunim.Intent = MoveInPlaylist{}

// withUI runs fn on the window's UI goroutine.
func withUI(t *testing.T, w *gunim.Window, run func(int), fn func(u *gunim.UI)) {
	t.Helper()
	gunim.RegisterPatch(w, "player", func(_ *playerRoot, _ struct{}, u *gunim.UI) { fn(u) })
	if err := w.Client().Patch("player", struct{}{}); err != nil {
		t.Fatal(err)
	}
	run(1)
}

func TestARowsMenuAddsItsTrackToAPlaylistOrTakesItOff(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), withPlaylist(library4()))
	tap(w, run, geom.Pt(120, shelfRowY(root, "p:p")))
	run(40)
	l := root.lib
	var items []string
	withUI(t, w, run, func(u *gunim.UI) {
		if !l.list.prepare(geom.Pt(100, rowH+rowH/2), u) {
			t.Fatal("the second row has no menu")
		}
		for _, it := range l.listMenu.Items() {
			items = append(items, it.Label)
		}
		l.pick(slices.Index(items, "Mix"), u)
		l.pick(slices.Index(items, "Remove from this playlist"), u)
	})
	want := []string{"Play", "Play next", "Add to Up next", "Add to playlist", "Mix", "New playlist", "Remove from this playlist"}
	if !slices.Equal(items, want) {
		t.Fatalf("the menu holds %q, want %q", items, want)
	}
	var captions []int
	for i, it := range l.listMenu.Items() {
		if it.Caption {
			captions = append(captions, i)
		}
	}
	if !slices.Equal(captions, []int{3}) {
		t.Fatalf("captions %v, want Add to playlist's", captions)
	}
	got := intents(w)
	if len(got) != 2 {
		t.Fatalf("the picks sent %v", got)
	}
	if a, ok := got[0].(AddToPlaylist); !ok || a.ID != "p" || !slices.Equal(a.Tracks, []int{2}) {
		t.Fatalf("Mix sent %v, want track 2 added to it", got[0])
	}
	if got[1] != (RemoveFromPlaylist{ID: "p", At: 1}) {
		t.Fatalf("Remove sent %v, want the second place taken off", got[1])
	}
}

func TestARowTakenOffAPlaylistFadesAndTheRowsBelowGlideUp(t *testing.T) {
	s := withPlaylist(library4())
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	tap(w, run, geom.Pt(120, shelfRowY(root, "p:p")))
	run(40)
	l := root.lib.list
	s.Playlists = []Playlist{{ID: "p", Name: "Mix", Tracks: []int{1, 3}}}
	if err := w.Client().Publish(playerTopic, s); err != nil {
		t.Fatal(err)
	}
	run(1)
	if len(l.leaving) != 1 || l.leaving[0].tr.ID != 2 {
		t.Fatalf("leaving %v, want track 2 fading", l.leaving)
	}
	// Track 3, now second, starts where it was, third, and glides up
	// a row, every frame no further from its place than the last.
	last := float32(2)
	for f := range 40 {
		at := 1 + l.shift[1].Value()
		if f == 0 && math.Abs(float64(at-2)) > 0.1 {
			t.Fatalf("the frame track 2 went, track 3 is at row %v, want it still at 2", at)
		}
		if at > last+0.01 {
			t.Fatalf("frame %d: track 3 went back down from row %v to %v", f, last, at)
		}
		last = at
		run(1)
	}
	if math.Abs(float64(last-1)) > 0.02 {
		t.Fatalf("after 40 frames track 3 is at row %v, want 1", last)
	}
	if len(l.leaving) != 0 {
		t.Fatalf("track 2 is still fading after 40 frames")
	}
}

func TestAfterARowIsDroppedOnlyTheRowUnderThePointerIsLit(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), withPlaylist(library4()))
	tap(w, run, geom.Pt(120, shelfRowY(root, "p:p")))
	run(40)
	list := boundsOf(t, w, run, root.lib.listScroll)
	grip := geom.Pt(list.Max.X-30, list.Min.Y+rowH/2)
	w.Input(input.PointerMove{Pos: grip})
	run(10)
	w.Input(input.PointerDown{Pos: grip, Button: input.ButtonPrimary, Clicks: 1})
	end := grip.Add(geom.Pt(0, 2*rowH))
	for i := 1; i <= 10; i++ {
		w.Input(input.PointerMove{Pos: grip.Add(geom.Pt(0, float32(i)*2*rowH/10))})
		run(1)
	}
	w.Input(input.PointerUp{Pos: end, Button: input.ButtonPrimary})
	run(1)
	l := root.lib.list
	for i := range l.tracks {
		want := float32(0)
		if i == 2 {
			want = 1
		}
		if math.Abs(float64(l.litOf(i)-want)) > 0.01 {
			t.Fatalf("the frame after the drop row %d is lit %v, want %v: only the row dropped, under the pointer", i, l.litOf(i), want)
		}
	}
}

func TestTheLibrarysMenuEndsWithSettingsAndThePrivacyPolicy(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), library4())
	l := root.lib
	var items []string
	withUI(t, w, run, func(u *gunim.UI) {
		l.openMore(u)
		for _, it := range l.menu.Items() {
			items = append(items, it.Label)
		}
		l.pick(len(items)-1, u)
	})
	n := len(items)
	if n < 3 || items[n-2] != "Settings" || items[n-1] != "Privacy policy" || !l.menu.Items()[n-2].Break {
		t.Fatalf("the menu holds %q, want Settings and Privacy policy last, under a line", items)
	}
	if got := intents(w); len(got) != 1 || got[0] != (ShowPrivacy{}) {
		t.Fatalf("Privacy policy sent %v, want ShowPrivacy", got)
	}
	withUI(t, w, run, func(u *gunim.UI) {
		l.openMore(u)
		l.pick(n-2, u)
	})
	if !root.settings.shown() {
		t.Fatal("Settings left the settings closed")
	}
}
