package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
)

func TestThePlayerTakesUpItsLastTrackAndListPaused(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	a.handle(NewPlaylist{ID: "p", Name: "Mix", Tracks: []int{all[3], all[1]}})
	a.handle(PlayTrack{ID: all[1], From: PlaylistList("p")})
	if a.kept.Last != a.entries[all[1]].key || a.kept.LastFrom != PlaylistList("p") {
		t.Fatalf("kept %q from %q, want the track played and its playlist", a.kept.Last, a.kept.LastFrom)
	}

	// The next run, with the same library kept.
	b := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	b.kept = a.kept
	b.refresh()
	b.resume, b.resumeFrom = b.kept.Last, b.kept.LastFrom
	b.takeUp()
	if b.Current != b.byKey[a.kept.Last].ID || b.Playing || b.From != PlaylistList("p") || b.Resumed != 1 {
		t.Fatalf("took up %d from %q, playing %v, resumed %d; want the track, paused, from the playlist",
			b.Current, b.From, b.Playing, b.Resumed)
	}
	if b.voice == nil || !b.voice.Paused() {
		t.Fatal("the track taken up is not ready, paused")
	}
	// Play goes on from it, and Next along its playlist.
	b.handle(TogglePlay{})
	if !b.Playing {
		t.Fatal("play after taking up did not play")
	}
	// It was the playlist's last: Next starts the playlist over.
	b.handle(Skip{})
	if want := b.byKey[a.entries[all[3]].key].ID; b.Current != want {
		t.Fatalf("Next went to %d, want the playlist's first, %d", b.Current, want)
	}
}

func TestTheWindowOpensTheListTheTrackTakenUpPlaysFrom(t *testing.T) {
	s := withPlaylist(library4())
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	s.Current, s.From, s.Resumed = 2, PlaylistList("p"), 1
	if err := w.Client().Publish(playerTopic, s); err != nil {
		t.Fatal(err)
	}
	run(2)
	if root.lib.open != PlaylistList("p") || root.lib.page.Value() < 0.99 {
		t.Fatalf("open %q at %v, want the playlist open at once", root.lib.open, root.lib.page.Value())
	}
}

func TestTheMusicFadesOutAsThePlayerCloses(t *testing.T) {
	m := audio.NewMixer()
	d := newDeck(m)
	d.play(&songSilence{}, func() {}, false)
	d.fadeOut(100 * time.Millisecond)
	go func() {
		buf := make([]float32, 2*480)
		for range 40 {
			m.Mix(buf)
		}
	}()
	start := time.Now()
	d.quiet(time.Second)
	if time.Since(start) > 900*time.Millisecond {
		t.Fatal("the fade as the player closes never ended")
	}
}

// songSilence plays silence without end.
type songSilence struct{}

func (*songSilence) Read(dst []float32) (int, error) { clear(dst); return len(dst) / 2, nil }
func (*songSilence) SeekFrame(int64) error           { return nil }
func (*songSilence) Len() int64                      { return -1 }

func TestTheWindowOpensWhereItLastClosed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "library.json")
	if placement(file) != nil {
		t.Fatal("a first run has a placement to open at")
	}
	a := newApp(context.Background(), newDeck(audio.NewMixer()), file)
	at := driver.Placement{Bounds: geom.Rc(2000, 120, 900, 640), Maximized: true}
	a.placement = func() (driver.Placement, bool) { return at, true }
	a.keepPlacement()
	a.save()
	if got := placement(file); got == nil || *got != at {
		t.Fatalf("the next run opens at %v, want %v", got, at)
	}
	// A window that cannot say where it is leaves the last place kept.
	a.placement = func() (driver.Placement, bool) { return driver.Placement{}, false }
	a.keepPlacement()
	a.save()
	if got := placement(file); got == nil || *got != at {
		t.Fatalf("the next run opens at %v, want %v still", got, at)
	}
}
