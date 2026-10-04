package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
)

// measured gives the demo songs loudnesses of their own, as if read
// through: the last quiet but with a peak near full scale, as a quiet
// track with sharp drums.
func measured(a *app) {
	for i, e := range a.order {
		an := analysis{Loud: true, LUFS: []float64{-24, -22, -12, -30}[i], Peak: []float32{0.2, 0.2, 0.5, 0.9}[i]}
		e.analyzed(an)
	}
	a.refresh()
}

func TestGainEvensTracksOrAlbumsAsTheModeSays(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	measured(a)
	all := ids(a)
	near := func(got float32, want float64, what string) {
		t.Helper()
		if math.Abs(float64(got)-want) > 0.05 {
			t.Fatalf("%s: gain %+.2f dB, want %+.2f", what, got, want)
		}
	}
	a.handle(SetGainMode{Mode: GainTrack})
	a.handle(PlayTrack{ID: all[0]})
	near(a.Gain, -18-(-24), "track gain on a track at -24 LUFS")
	if a.GainBy != GainByTrack {
		t.Fatalf("it follows %v, want the track", a.GainBy)
	}
	// Lifted 12 dB, the last track would pass full scale: its peak
	// holds it under.
	a.handle(PlayTrack{ID: all[3]})
	near(a.Gain, -dB(0.9), "track gain on a track at -30 LUFS peaking at 0.9")

	// Album gain, in order: every track of the album at the album's.
	a.handle(SetGainMode{Mode: GainAlbum})
	lufs, peak, _ := a.albumLoudness(a.entries[all[0]])
	want := min(-18-lufs, -dB(float64(peak)))
	a.handle(PlayTrack{ID: all[0]})
	near(a.Gain, want, "album gain on the first track")
	a.handle(PlayTrack{ID: all[2]})
	near(a.Gain, want, "album gain on the third track")
	if a.GainBy != GainByAlbum {
		t.Fatalf("in order it follows %v, want the album", a.GainBy)
	}
	// Shuffled, each track evens out on its own.
	a.handle(ToggleShuffle{})
	if a.GainBy != GainByTrack {
		t.Fatalf("shuffled, it follows %v, want the track", a.GainBy)
	}
	near(a.Gain, -18-(-12), "shuffled, the third track")

	a.handle(SetGainMode{Mode: GainOff})
	if a.Gain != 0 || a.GainBy != GainNone {
		t.Fatalf("gain off: %+.1f dB by %v, want none", a.Gain, a.GainBy)
	}
}

func TestAnAnalysisIsKeptUntilItsFileChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one.mp3")
	copyTone(t, path)
	e := readEntry(path)
	src, closer, err := e.open()
	if err != nil {
		t.Fatal(err)
	}
	an := analyze(src)
	closer()
	fi, _ := os.Stat(path)
	an.Size, an.Mod = fi.Size(), fi.ModTime()
	if !an.Loud || an.Format.Name != "MP3" || len(an.Peaks) != peakCount {
		t.Fatalf("the tone read as %+v, want its loudness, MP3 and its peaks", an)
	}
	file := filepath.Join(dir, "analysis.json")
	if err := writeAnalyses(file, map[string]analysis{path: an}); err != nil {
		t.Fatal(err)
	}
	kept := loadAnalyses(file)[path]
	if !kept.fresh(path) || math.Abs(kept.LUFS-an.LUFS) > 1e-9 {
		t.Fatalf("kept as %+v: fresh %v; want it as measured", kept, kept.fresh(path))
	}
	later := time.Now()
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if kept.fresh(path) {
		t.Fatal("an analysis still holds after its file changed")
	}
}

func TestTheGainButtonStepsThroughTheModes(t *testing.T) {
	s := library4()
	s.GainMode = GainAlbum
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	b := boundsOf(t, w, run, root.now.gain)
	at := b.Min.Add(geom.Pt(b.Size().W/2, b.Size().H/2))
	var got []GainMode
	for range 3 {
		tap(w, run, at)
		for _, in := range intents(w) {
			if m, ok := in.(SetGainMode); ok {
				got = append(got, m.Mode)
			}
		}
	}
	want := []GainMode{GainOff, GainTrack, GainAlbum}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("three clicks from album gain sent %v, want %v", got, want)
	}
}

func TestTheVolumeBarShowsTheGainAsThePointerComesOver(t *testing.T) {
	s := library4()
	s.Current, s.Playing = 1, true
	s.Tracks[0].Measured, s.Tracks[0].LUFS = true, -22
	s.GainMode, s.Gain, s.GainBy = GainTrack, 4, GainByTrack
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	v := root.now.volume
	if v.words[0] != "+4.0 dB track gain" || v.words[1] != "-22.0 LUFS to -18" {
		t.Fatalf("the bar says %q, want the track gain and its loudness", v.words)
	}
	b := boundsOf(t, w, run, v)
	w.Input(input.PointerMove{Pos: b.Min.Add(geom.Pt(b.Size().W/2, b.Size().H/2))})
	last := float32(0)
	for f := range 30 {
		run(1)
		on := v.gainOn.Value()
		if on < last-0.25 {
			t.Fatalf("frame %d: the gain's showing fell from %v to %v while the pointer stayed", f, last, on)
		}
		last = on
	}
	if last < 0.95 {
		t.Fatalf("half a second over the bar the gain shows %v", last)
	}
	w.Input(input.PointerMove{Pos: geom.Pt(10, 10)})
	run(40)
	if v.gainOn.Value() > 0.05 {
		t.Fatalf("with the pointer gone the gain still shows %v", v.gainOn.Value())
	}
}

var _ = gunim.Root
