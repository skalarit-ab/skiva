package main

import (
	"math"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
)

// newTestApp returns the application half with the demo songs, playing
// through a mixer no speaker reads.
func newTestApp() *app {
	a := &app{d: newDeck(audio.NewMixer()), entries: map[int]*entry{}, peaksDone: make(chan peaksRead, 4)}
	a.rng = nil
	for _, e := range demoEntries() {
		a.add(e)
	}
	return a
}

func (a *app) idList() []int {
	out := make([]int, 0, len(a.order))
	for _, e := range a.order {
		out = append(out, e.ID)
	}
	return out
}

func TestTheDemoSongsAreReadyToPlay(t *testing.T) {
	for _, e := range demoEntries() {
		if e.Cover == nil || e.Length < time.Minute || len(e.Peaks) != peakCount {
			t.Errorf("%s: cover %v, length %v, %d peaks; want a cover, over a minute, and its peaks", e.Title, e.Cover != nil, e.Length, len(e.Peaks))
		}
		if e.Accent == e.Glow {
			t.Errorf("%s lights in one colour, %v, want two", e.Title, e.Accent)
		}
	}
}

func TestNextGoesDownTheListAndRepeatsAsAsked(t *testing.T) {
	a := newTestApp()
	ids := a.idList()
	a.start(ids[0])
	if a.Current != ids[0] || !a.Playing {
		t.Fatalf("started, current %d, playing %v", a.Current, a.Playing)
	}
	a.handle(Skip{})
	if a.Current != ids[1] {
		t.Fatalf("Next went to %d, want %d", a.Current, ids[1])
	}
	a.start(ids[len(ids)-1])
	a.ended()
	if a.Current != 0 || a.Playing {
		t.Fatalf("the last track ended, with repeat off: current %d, playing %v; want stopped", a.Current, a.Playing)
	}
	a.start(ids[len(ids)-1])
	a.handle(CycleRepeat{})
	a.ended()
	if a.Current != ids[0] {
		t.Fatalf("the last track ended, repeating all: current %d, want the first, %d", a.Current, ids[0])
	}
	a.handle(CycleRepeat{})
	a.ended()
	if a.Current != ids[0] || a.Repeat != RepeatOne {
		t.Fatalf("a track ended, repeating one: current %d, repeat %v; want it again", a.Current, a.Repeat)
	}
}

func TestBackStartsATrackAgainOrGoesBack(t *testing.T) {
	a := newTestApp()
	ids := a.idList()
	a.start(ids[2])
	a.handle(Skip{Back: true})
	if a.Current != ids[1] {
		t.Fatalf("Back at a track's start went to %d, want the one before, %d", a.Current, ids[1])
	}
	// Seconds in, Back starts it over.
	a.d.seek(10 * time.Second)
	a.d.mix.Mix(make([]float32, 2*512))
	a.handle(Skip{Back: true})
	if a.Current != ids[1] {
		t.Fatalf("Back ten seconds in went to %d, want the same track again", a.Current)
	}
	if at, _ := a.d.position(); at > time.Second {
		t.Fatalf("Back ten seconds in left the track at %v, want its start", at)
	}
}

func TestPauseHoldsTheTrack(t *testing.T) {
	a := newTestApp()
	a.handle(TogglePlay{})
	if a.Current == 0 || !a.Playing {
		t.Fatalf("play with nothing playing: current %d, playing %v; want the first track", a.Current, a.Playing)
	}
	a.handle(TogglePlay{})
	if a.Playing || !a.voice.Paused() {
		t.Fatalf("pause: playing %v, voice paused %v", a.Playing, a.voice.Paused())
	}
}

func TestAFileWithNoTagsIsNamedForItself(t *testing.T) {
	e := readEntry("../../audio/testdata/tone.mp3")
	if e == nil {
		t.Fatal("a plain MP3 did not read")
	}
	if e.Title != "tone" || e.Album != "testdata" || e.Cover == nil {
		t.Errorf("read as %q on %q, cover %v; want its file's and folder's names, and a made cover", e.Title, e.Album, e.Cover != nil)
	}
	if e.Length < 450*time.Millisecond || e.Length > 550*time.Millisecond {
		t.Errorf("its length is %v, want half a second", e.Length)
	}
}

// stage mounts the player view in an offscreen window of size, showing
// s, and returns the window, the root, and a way to step frames.
func stage(t *testing.T, size geom.Size, s Player) (*gunim.Window, *playerRoot, func(int)) {
	t.Helper()
	d := newDeck(audio.NewMixer())
	var root *playerRoot
	w := gunim.NewOffscreen(size, nil)
	gunim.RegisterView(w, "player",
		func(Player) *playerRoot { root = newPlayerRoot(d); return root },
		func(r *playerRoot, s Player, u *gunim.UI) { r.show(s, u) })
	if err := w.Client().Mount(gunim.Root, "player", "player", s, playerTopic); err != nil {
		t.Fatal(err)
	}
	run := func(n int) {
		for range n {
			w.Frame(time.Second / 60)
		}
	}
	run(2)
	return w, root, run
}

// boundsOf returns where n is in the window.
func boundsOf(t *testing.T, w *gunim.Window, run func(int), n gunim.Node) geom.Rect {
	t.Helper()
	var r geom.Rect
	gunim.RegisterPatch(w, "player", func(_ *playerRoot, _ struct{}, u *gunim.UI) { r, _ = u.Bounds(n) })
	if err := w.Client().Patch("player", struct{}{}); err != nil {
		t.Fatal(err)
	}
	run(1)
	return r
}

func tap(w *gunim.Window, run func(int), p geom.Point) {
	w.Input(input.PointerMove{Pos: p})
	w.Input(input.PointerDown{Pos: p, Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerUp{Pos: p, Button: input.ButtonPrimary})
	run(2)
}

// intents returns what the window sent.
func intents(w *gunim.Window) []gunim.Intent {
	var out []gunim.Intent
	for len(w.Client().Intents()) > 0 {
		out = append(out, (<-w.Client().Intents()).Intent)
	}
	return out
}

func library4() Player {
	var s Player
	for i, e := range demoEntries() {
		e.ID = i + 1
		s.Tracks = append(s.Tracks, e.Track)
	}
	s.Volume = 0.8
	return s
}

func TestTheWindowSendsWhatIsClicked(t *testing.T) {
	w, root, run := stage(t, geom.Sz(1100, 720), library4())
	// The third row of the library.
	tap(w, run, geom.Pt(120, headH+2*rowH+rowH/2))
	play := boundsOf(t, w, run, root.now.play)
	tap(w, run, play.Min.Add(geom.Pt(play.Size().W/2, play.Size().H/2)))
	got := intents(w)
	if len(got) != 2 || got[0] != (PlayTrack{ID: 3}) || got[1] != (TogglePlay{}) {
		t.Fatalf("a click on the third row and on play sent %v, want PlayTrack 3 and TogglePlay", got)
	}
}

func TestAPressOnTheSeekBarSeeksThere(t *testing.T) {
	s := library4()
	s.Current = 1
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	bar := boundsOf(t, w, run, root.now.seek)
	at := bar.Min.Add(geom.Pt(bar.Size().W/4, barH/2))
	w.Input(input.PointerMove{Pos: at})
	w.Input(input.PointerDown{Pos: at, Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerUp{Pos: at, Button: input.ButtonPrimary})
	run(1)
	got := intents(w)
	// A quarter along the bar, through the lens at the track's start
	// as the press found it.
	u := lens{focus: 0, strength: lensRest}.back(0.25)
	want := time.Duration(u * float64(s.Tracks[0].Length))
	if len(got) != 1 {
		t.Fatalf("a press a quarter along the bar sent %v", got)
	}
	if seek, ok := got[0].(SeekTo); !ok || (seek.At-want).Abs() > time.Second {
		t.Fatalf("a press a quarter along the bar sent %v, want a seek to where the bar shows, about %v", got[0], want)
	}
}

func TestOnAPhoneTheLibraryIsASheet(t *testing.T) {
	w, root, run := stage(t, geom.Sz(400, 820), library4())
	if !root.narrow {
		t.Fatal("a window 400 wide is not narrow")
	}
	btn := boundsOf(t, w, run, root.listButton)
	if btn.Size().W != 40 {
		t.Fatalf("the library's button is %v, want 40 square", btn)
	}
	tap(w, run, btn.Min.Add(geom.Pt(20, 20)))
	run(60)
	if v := root.sheet.Value(); v < 0.99 {
		t.Fatalf("a second after its button, the sheet is %v open", v)
	}
	lib := boundsOf(t, w, run, root.lib)
	tap(w, run, geom.Pt(120, lib.Min.Y+headH+rowH/2))
	if got := intents(w); len(got) != 1 || got[0] != (PlayTrack{ID: 1}) {
		t.Fatalf("a tap on the sheet's first row sent %v, want PlayTrack 1", got)
	}
	run(60)
	if v := root.sheet.Value(); v > 0.01 {
		t.Fatalf("a second after a track was picked, the sheet is %v open, want shut", v)
	}
}

func TestPlayingAnimatesAndStopsWhenPaused(t *testing.T) {
	s := library4()
	s.Current, s.Playing, s.Starts = 2, true, 1
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	run(60)
	if root.now.record.spin.Value() < 0.9 {
		t.Fatalf("a second into playing, the record turns at %v of its speed", root.now.record.spin.Value())
	}
	s.Playing = false
	if err := w.Client().Publish(playerTopic, s); err != nil {
		t.Fatal(err)
	}
	run(10)
	if v := root.now.record.spin.Value(); v < 0.2 || v > 0.95 {
		t.Fatalf("a moment after pausing, the record turns at %v of its speed, want it running down", v)
	}
	run(300)
	if v := root.now.record.spin.Value(); v > 0.001 {
		t.Fatalf("five seconds after pausing, the record still turns at %v", v)
	}
	if root.Step(time.Second/60) || root.now.record.Step(time.Second/60) {
		t.Fatal("five seconds after pausing, the player still animates")
	}
}

func TestTracksChangedQuicklyEndOnTheLastWithEveryFrameInBetween(t *testing.T) {
	s := library4()
	s.Playing = true
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	// Three tracks a frame apart, then a pause while the record is
	// still coming up to speed.
	for id := 1; id <= 3; id++ {
		s.Current, s.Starts = id, s.Starts+1
		if err := w.Client().Publish(playerTopic, s); err != nil {
			t.Fatal(err)
		}
		run(1)
	}
	s.Playing = false
	if err := w.Client().Publish(playerTopic, s); err != nil {
		t.Fatal(err)
	}
	r := root.now.record
	prevIn := float32(-1)
	for f := range 120 {
		run(1)
		in := r.in.Value()
		if in < prevIn-0.02 && prevIn < 0.98 {
			t.Fatalf("frame %d: the new record went back from %v to %v as it came in", f, prevIn, in)
		}
		prevIn = in
		if r.cover != s.Tracks[2].Cover {
			t.Fatalf("frame %d: the record shows another cover than the last track's", f)
		}
		if root.now.titles.title != s.Tracks[2].Title {
			t.Fatalf("frame %d: the title is %q, want the last track's, %q", f, root.now.titles.title, s.Tracks[2].Title)
		}
	}
	if r.in.Value() < 0.99 || r.spin.Value() > 0.05 {
		t.Fatalf("two seconds on, the record is %v in and turns at %v; want in, and stopping", r.in.Value(), r.spin.Value())
	}
}

func TestTheLensSpreadsBarsAboutItsFocusAndKeepsTheirOrder(t *testing.T) {
	l := lens{focus: 0.4, strength: lensHover}
	if l.at(0) != 0 || math.Abs(l.at(1)-1) > 1e-9 {
		t.Fatalf("the lens maps the ends to %v and %v, want 0 and 1", l.at(0), l.at(1))
	}
	prev := -1.0
	for i := range 1001 {
		u := float64(i) / 1000
		x := l.at(u)
		if x <= prev {
			t.Fatalf("at %v the lens maps back, to %v after %v", u, x, prev)
		}
		prev = x
		if b := l.back(x); math.Abs(b-u) > 1e-6 {
			t.Fatalf("back(at(%v)) is %v", u, b)
		}
	}
	if near, far := l.zoom(0.4), l.zoom(0.9); near < 2*far {
		t.Fatalf("the lens spreads its focus %v and far from it %v, want the focus much wider", near, far)
	}
}

func TestTheShapeTellsVersesFromChorusesInALoudMaster(t *testing.T) {
	// A loud master: every stretch within 4 dB of the loudest, a
	// chorus at the top, a verse 3 dB under, and silence at the end.
	power := make([]float64, 40)
	for i := range power {
		switch {
		case i < 30 && i%10 < 5:
			power[i] = 0.5
		case i < 30:
			power[i] = 0.25
		default:
			power[i] = 0
		}
	}
	got := shape(power)
	if got[0] != 1 {
		t.Errorf("the chorus is at %v, want the top", got[0])
	}
	if got[5] > 0.7 || got[5] < 0.3 {
		t.Errorf("the verse, 3 dB under, is at %v, want it plainly lower, about halfway", got[5])
	}
	if got[35] != 0 {
		t.Errorf("silence is at %v, want the bottom", got[35])
	}
}

func TestADragLandsOnTheTimeItShowed(t *testing.T) {
	s := library4()
	s.Current = 1
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	bar := boundsOf(t, w, run, root.now.seek)
	y := bar.Min.Y + barH/2
	w.Input(input.PointerMove{Pos: geom.Pt(bar.Min.X+bar.Size().W*0.7, y)})
	w.Input(input.PointerDown{Pos: geom.Pt(bar.Min.X+bar.Size().W*0.7, y), Button: input.ButtonPrimary, Clicks: 1})
	run(1)
	end := geom.Pt(bar.Min.X+bar.Size().W*0.3, y)
	for x := float32(0.7); x > 0.3; x -= 0.05 {
		w.Input(input.PointerMove{Pos: geom.Pt(bar.Min.X+bar.Size().W*x, y)})
		run(1)
	}
	w.Input(input.PointerMove{Pos: end})
	run(1)
	shown := root.now.seek.at
	// The lens follows the playhead a few frames on before the release.
	run(5)
	w.Input(input.PointerUp{Pos: end, Button: input.ButtonPrimary})
	run(1)
	got := intents(w)
	want := time.Duration(float64(shown) * float64(s.Tracks[0].Length))
	if len(got) != 1 || got[0] != (SeekTo{At: want}) {
		t.Fatalf("a drag let go where it showed %v sent %v, want a seek there", want, got)
	}
}
