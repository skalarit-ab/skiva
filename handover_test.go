package main

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/gunim/audio"

	"github.com/skalarit-ab/skiva/internal/single"
)

// A Skiva started again hands over the files it names, made whole
// against the folder it started in, and leaves its own flags out.
func TestAHandoverNamesItsFiles(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "b.flac")
	files, quit := handedOver(single.Handover{Args: []string{"-play", "a.mp3", abs}, Dir: dir})
	if want := []string{filepath.Join(dir, "a.mp3"), abs}; !slices.Equal(files, want) || quit {
		t.Fatalf("the handover named %q, quit %v; want %q", files, quit, want)
	}
	if _, quit := handedOver(single.Handover{Args: []string{"-quit"}}); !quit {
		t.Fatal("-quit did not ask the Skiva running to close")
	}
}

// A file opened with Skiva plays at once, in place of the track
// playing, and the list goes on where it was after it.
func TestAFileOpenedPlaysAtOnce(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "one.mp3")
	copyTone(t, one)
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	a.handle(PlayTrack{ID: all[0]})
	a.open([]string{one})
	readAll(t, a)
	if e := a.entries[a.Current]; e == nil || e.File != one {
		t.Fatalf("playing %v, want the file opened", a.entries[a.Current])
	}
	a.handle(Skip{})
	if a.Current != all[1] {
		t.Fatalf("after the file opened the list went on to %d, want %d", a.Current, all[1])
	}
}

// Files opened together, as several chosen at once in the file
// manager, come one Skiva each: the first plays, and the rest follow
// it, in order.
func TestFilesOpenedTogetherFollowTheFirst(t *testing.T) {
	dir := t.TempDir()
	files := make([]string, 0, 3)
	for _, name := range []string{"one.mp3", "two.mp3", "three.mp3"} {
		f := filepath.Join(dir, name)
		copyTone(t, f)
		files = append(files, f)
	}
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	a.handle(PlayTrack{ID: ids(a)[0]})
	for _, f := range files {
		a.open([]string{f})
		readAll(t, a)
	}
	var played []string
	for e := a.entries[a.Current]; e != nil && len(played) < 3; e = a.entries[a.Current] {
		played = append(played, e.File)
		a.handle(Skip{})
	}
	if !slices.Equal(played, files) {
		t.Fatalf("played %q, want %q", played, files)
	}
}
