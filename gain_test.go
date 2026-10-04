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
	if v.words[0] != "+4.0 dB track gain" || v.words[1] != "-22.0 LUFS, plays at -19.9 LUFS" {
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

func TestTheVolumePassesFullOnlyWhileGainIsOn(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	a.handle(SetGainMode{Mode: GainTrack})
	a.handle(SetVolume{Volume: 2.5})
	if a.Volume != 2.5 {
		t.Fatalf("with gain on, the volume went to %v, want 2.5", a.Volume)
	}
	a.handle(SetVolume{Volume: 9})
	if a.Volume != maxBoost {
		t.Fatalf("the volume went to %v, want no further than %v, +%d dB", a.Volume, maxBoost, boostDB)
	}
	a.handle(SetGainMode{Mode: GainOff})
	if a.Volume != 1 {
		t.Fatalf("with gain turned off, the volume is %v, want full, 1", a.Volume)
	}
	a.handle(SetVolume{Volume: 2})
	if a.Volume != 1 {
		t.Fatalf("with gain off, the volume went to %v, want no further than 1", a.Volume)
	}
}

func TestTheEqualizersBoostsLowerATrackOnlyAsFarAsItsPeakNeeds(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	// A track peaking at -1 dBFS, played as it is, with gain off.
	a.entries[all[0]].analyzed(analysis{Loud: true, LUFS: -12, Peak: float32(math.Pow(10, -1.0/20))})
	a.handle(SetGainMode{Mode: GainOff})
	a.handle(SetVolume{Volume: 1})
	a.handle(PlayTrack{ID: all[0]})
	a.handle(SetEQ{EQ: EQ{Bands: []audio.Band{{ID: 1, Kind: audio.Bell, Freq: 100, Gain: 6, Q: 1, On: true}}}})
	// The bell would lift its peak 6 dB, 5 past full: it plays 5 dB
	// lower.
	if math.Abs(float64(a.Headroom)-5) > 0.05 {
		t.Fatalf("at full volume the headroom is %.2f dB, want 5", a.Headroom)
	}
	// At half volume, 6 dB down, the boost fits.
	a.handle(SetVolume{Volume: 0.5})
	if a.Headroom > 0.05 {
		t.Fatalf("at half volume the headroom is %.2f dB, want none", a.Headroom)
	}
	a.handle(SetVolume{Volume: 1})
	a.handle(SetEQ{EQ: EQ{Bands: []audio.Band{{ID: 1, Kind: audio.Bell, Freq: 100, Gain: 6, Q: 1, On: true}}, Bypass: true}})
	if a.Headroom != 0 {
		t.Fatalf("bypassed, the equalizer still takes %.2f dB", a.Headroom)
	}
}

func TestTheVolumeBarReadsBackWhereItPutsAVolume(t *testing.T) {
	s := library4()
	s.GainMode = GainTrack
	_, root, run := stage(t, geom.Sz(1100, 720), s)
	run(60)
	v := root.now.volume
	for u := float32(0); u <= 1; u += 0.05 {
		if got := v.along(v.volumeAt(u)); math.Abs(float64(got-u)) > 1e-4 {
			t.Fatalf("at %.2f along, the volume reads back %.4f along", u, got)
		}
	}
	// Full lies three quarters along, and +6 dB halfway past it.
	if f := v.along(1); math.Abs(float64(f)-0.75) > 1e-4 {
		t.Fatalf("full volume lies %v along, want 0.75", f)
	}
	if got := v.along(float32(math.Pow(10, 6.0/20))); math.Abs(float64(got)-0.875) > 1e-3 {
		t.Fatalf("+6 dB lies %v along, want 0.875", got)
	}
}

func TestTheButtonsStayInARowAboveTheEqualizer(t *testing.T) {
	for _, size := range []geom.Size{geom.Sz(1100, 720), geom.Sz(400, 820)} {
		w, root, run := stage(t, size, library4())
		withUI(t, w, run, func(u *gunim.UI) { root.info.show(true, u) })
		b := boundsOf(t, w, run, root.eqButton)
		tap(w, run, b.Min.Add(geom.Pt(20, 20)))
		run(60)
		if root.info.shown() {
			t.Fatalf("%v: the track's card stayed open as the equalizer opened", size)
		}
		eq := boundsOf(t, w, run, root.eq)
		buttons := []gunim.Node{root.infoButton, root.eqButton}
		if root.narrow {
			buttons = append(buttons, root.listButton)
		}
		for _, n := range buttons {
			if r := boundsOf(t, w, run, n); r.Min.X < 0 || r.Max.Y > eq.Min.Y {
				t.Fatalf("%v: a button is at %v, the equalizer from %v; want the button in place, above it", size, r, eq.Min)
			}
		}
	}
}

func TestTheLibraryOpeningPutsTheCardAway(t *testing.T) {
	w, root, run := stage(t, geom.Sz(400, 820), library4())
	withUI(t, w, run, func(u *gunim.UI) { root.info.show(true, u) })
	run(30)
	b := boundsOf(t, w, run, root.listButton)
	tap(w, run, b.Min.Add(geom.Pt(20, 20)))
	run(30)
	if root.info.shown() {
		t.Fatal("the track's card stayed open over the library")
	}
}

// A track fading out as another starts keeps its own gain: the new
// track's gain is the new track's alone.
func TestATrackFadingOutKeepsItsOwnGain(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	measured(a)
	all := ids(a)
	a.handle(SetGainMode{Mode: GainTrack})
	a.handle(PlayTrack{ID: all[0]})
	old := a.d.cur.src
	before := old.target()
	// The third track is 12 dB louder: it plays 12 dB lower
	a.handle(PlayTrack{ID: all[2]})
	if got := old.target(); got != before {
		t.Fatalf("the track fading out went from a gain of %.3f to %.3f as the next started", before, got)
	}
	if got, want := a.d.cur.src.target(), ratio(float64(a.Gain-a.Headroom)); math.Abs(float64(got-want)) > 1e-4 {
		t.Fatalf("the new track plays at a gain of %.3f, want %.3f", got, want)
	}
	if math.Abs(float64(a.d.cur.src.target()/before)-math.Pow(10, -12.0/20)) > 1e-3 {
		t.Fatalf("the new track plays at %.3f of the first's gain, want 12 dB lower", a.d.cur.src.target()/before)
	}
}
