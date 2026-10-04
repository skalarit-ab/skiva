package main

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
)

func TestUpNextPlaysBeforeTheListGoesOn(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	a.handle(PlayTrack{ID: all[0]})
	a.handle(Enqueue{Tracks: []int{all[3]}})
	a.handle(Enqueue{Tracks: []int{all[2]}, Next: true})
	if !slices.Equal(a.Queue, []int{all[2], all[3]}) {
		t.Fatalf("Up next is %v, want %v", a.Queue, []int{all[2], all[3]})
	}
	played := make([]int, 0, 3)
	for range 3 {
		a.handle(Skip{})
		played = append(played, a.Current)
	}
	// Up next, in order, and then the list goes on after the track it
	// broke into.
	if want := []int{all[2], all[3], all[1]}; !slices.Equal(played, want) {
		t.Fatalf("Next played %v, want %v", played, want)
	}
	if len(a.Queue) != 0 {
		t.Fatalf("Up next holds %v once played, want nothing", a.Queue)
	}
}

func TestATrackPickedOnUpNextPassesThoseBeforeIt(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	a.handle(PlayTrack{ID: all[0]})
	a.handle(Enqueue{Tracks: []int{all[1], all[2], all[3]}})
	a.handle(PlayTrack{ID: all[2], From: QueueList})
	if a.Current != all[2] || !slices.Equal(a.Queue, []int{all[3]}) {
		t.Fatalf("playing %d with Up next %v, want %d and %v", a.Current, a.Queue, all[2], []int{all[3]})
	}
	a.handle(Skip{})
	a.handle(Skip{})
	if a.Current != all[1] {
		t.Fatalf("after Up next the list went on to %d, want %d, after the track it broke into", a.Current, all[1])
	}
}

// readAll applies what the background reads of a have sent, until they
// stop for a moment.
func readAll(t *testing.T, a *app) {
	t.Helper()
	for {
		select {
		case ch := <-a.changes:
			a.apply(ch)
		case x := <-a.spread:
			a.place(x)
		case <-time.After(300 * time.Millisecond):
			a.refresh()
			a.startSoon()
			return
		}
	}
}

func TestFilesDroppedOnAPlaylistLandAtTheirPlace(t *testing.T) {
	dir := t.TempDir()
	one, two := filepath.Join(dir, "Album", "one.mp3"), filepath.Join(dir, "Album", "two.mp3")
	copyTone(t, one)
	copyTone(t, two)
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	a.handle(NewPlaylist{ID: "p", Name: "Mix", Tracks: []int{all[0], all[1]}})
	a.handle(AddPaths{Paths: []string{filepath.Join(dir, "Album")}, To: PlaylistList("p"), At: 1})
	readAll(t, a)
	got := a.Playlists[0].Tracks
	want := []int{all[0], a.byKey[one].ID, a.byKey[two].ID, all[1]}
	if !slices.Equal(got, want) {
		t.Fatalf("the playlist is %v, want the folder's two tracks in its middle: %v", got, want)
	}
	if len(a.kept.Folders) != 0 {
		t.Fatalf("a folder dropped on a playlist is followed: %v", a.kept.Folders)
	}
}

func TestFilesDroppedToPlayStartOnceRead(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "one.mp3")
	copyTone(t, one)
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	a.handle(AddPaths{Paths: []string{one}, To: QueueList, At: -1, Play: true})
	readAll(t, a)
	if e := a.byKey[one]; e == nil || a.Current != e.ID || !a.Playing {
		t.Fatalf("playing %d, want the file dropped, read", a.Current)
	}
}

func TestFilesDraggedOverTheTrackPlayingGoOnUpNext(t *testing.T) {
	s := library4()
	s.Current, s.Playing = 1, true
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	at := geom.Pt(700, 300)
	files := []string{"/music/a.mp3"}
	w.Input(driver.FilesOver{Pos: at})
	// The frame grows in, and once in it stays, while the files hover.
	last, in := float32(0), false
	for f := range 20 {
		w.Input(driver.FilesOver{Pos: at, Paths: files})
		run(1)
		v := root.now.drop.on.Value()
		if !in && v < last-0.001 || in && v < 0.95 {
			t.Fatalf("frame %d: the drop frame went from %v to %v while files hovered", f, last, v)
		}
		in = in || v >= 0.95
		last = v
	}
	if last < 0.95 || root.now.drop.text != "Add a.mp3 to Up next" {
		t.Fatalf("the frame is %v in, saying %q; want it in, saying Add a.mp3 to Up next", last, root.now.drop.text)
	}
	w.Input(input.Drop{Pos: at, Paths: files})
	run(1)
	got := intents(w)
	if len(got) != 1 {
		t.Fatalf("a drop sent %v", got)
	}
	if a, ok := got[0].(AddPaths); !ok || !slices.Equal(a.Paths, files) || a.To != QueueList || !a.Play {
		t.Fatalf("a drop sent %v, want the file added to Up next, to play", got[0])
	}
	run(30)
	if v := root.now.drop.on.Value(); v > 0.01 {
		t.Fatalf("half a second after the drop the frame is %v in", v)
	}
}

func TestFilesDraggedOverTheShelfLightTheRowTheyWouldJoin(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), withPlaylist(library4()))
	files := []string{"/music/a.mp3"}
	cases := []struct {
		key, aim string
		want     gunim.Intent
	}{
		{"queue", "queue", AddPaths{Paths: files, To: QueueList, At: -1}},
		{"p:p", "p:p", AddPaths{Paths: files, To: PlaylistList("p"), At: -1}},
		{"addf", "all", AddPaths{Paths: files, To: AllTracks, At: -1}},
	}
	for _, c := range cases {
		at := geom.Pt(120, shelfRowY(root, c.key))
		w.Input(driver.FilesOver{Pos: at, Paths: files})
		run(10)
		if root.lib.shelf.target != c.aim || root.lib.shelf.aim.Value() < 0.5 {
			t.Fatalf("files over %s light %q at %v, want %q lit", c.key, root.lib.shelf.target, root.lib.shelf.aim.Value(), c.aim)
		}
		w.Input(input.Drop{Pos: at, Paths: files})
		run(1)
		got := intents(w)
		if len(got) != 1 {
			t.Fatalf("a drop on %s sent %v", c.key, got)
		}
		a, ok := got[0].(AddPaths)
		want, _ := c.want.(AddPaths)
		if !ok || !slices.Equal(a.Paths, want.Paths) || a.To != want.To || a.At != want.At {
			t.Fatalf("a drop on %s sent %v, want %v", c.key, got[0], want)
		}
	}
}

func TestATrackDraggedOntoTheTrackPlayingPlays(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), library4())
	tap(w, run, geom.Pt(120, shelfRowY(root, "all")))
	run(40)
	from := geom.Pt(120, headH+rowH+rowH/2)
	w.Input(input.PointerMove{Pos: from})
	w.Input(input.PointerDown{Pos: from, Button: input.ButtonPrimary, Clicks: 1})
	run(1)
	to := geom.Pt(700, 300)
	for i := 1; i <= 10; i++ {
		w.Input(input.PointerMove{Pos: from.Add(to.Sub(from).Mul(float32(i) / 10))})
		run(1)
	}
	if v := root.now.drop.on.Value(); v < 0.5 || root.now.drop.text != "Play this track" {
		t.Fatalf("a track over the track playing: frame %v saying %q, want it in, saying Play this track", v, root.now.drop.text)
	}
	w.Input(input.PointerUp{Pos: to, Button: input.ButtonPrimary})
	run(2)
	got := intents(w)
	if len(got) != 1 {
		t.Fatalf("the drop sent %v", got)
	}
	if e, ok := got[0].(Enqueue); !ok || !slices.Equal(e.Tracks, []int{2}) || !e.Play {
		t.Fatalf("the drop sent %v, want track 2 on Up next, to play", got[0])
	}
}

func TestADragRestingOnBackSlidesTheListAway(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), withPlaylist(library4()))
	tap(w, run, geom.Pt(120, shelfRowY(root, "all")))
	run(40)
	back := boundsOf(t, w, run, root.lib.back)
	files := []string{"/music/a.mp3"}
	w.Input(driver.FilesOver{Pos: back.Min.Add(geom.Pt(18, 18)), Paths: files})
	run(int(dwell/(time.Second/60)) + 40)
	if v := root.lib.page.Value(); v > 0.01 {
		t.Fatalf("a drag resting on back for its while left the list %v in", v)
	}
	// Then it drops on the shelf's playlist.
	at := geom.Pt(120, shelfRowY(root, "p:p"))
	w.Input(driver.FilesOver{Pos: at, Paths: files})
	w.Input(input.Drop{Pos: at, Paths: files})
	run(1)
	if got := intents(w); len(got) != 1 || got[0] == nil || addedTo(got[0]) != PlaylistList("p") {
		t.Fatalf("the drop sent %v, want the file added to the playlist", got)
	}
}

func TestFilesDroppedOnAPlaylistsPageLandInTheGap(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), withPlaylist(library4()))
	tap(w, run, geom.Pt(120, shelfRowY(root, "p:p")))
	run(40)
	list := boundsOf(t, w, run, root.lib.listScroll)
	files := []string{"/music/a.mp3"}
	// Just under the first row's middle: the gap after it.
	at := list.Min.Add(geom.Pt(100, rowH*0.9))
	w.Input(driver.FilesOver{Pos: at, Paths: files})
	run(10)
	if root.lib.list.at != 1 {
		t.Fatalf("the gap is at %d, want 1, after the first row", root.lib.list.at)
	}
	w.Input(input.Drop{Pos: at, Paths: files})
	run(1)
	got := intents(w)
	if len(got) != 1 {
		t.Fatalf("the drop sent %v", got)
	}
	if a, ok := got[0].(AddPaths); !ok || a.To != PlaylistList("p") || a.At != 1 {
		t.Fatalf("the drop sent %v, want the file added to the playlist at 1", got[0])
	}
}

// addedTo returns the list an AddPaths adds to.
func addedTo(in gunim.Intent) ListID {
	a, _ := in.(AddPaths)
	return a.To
}
