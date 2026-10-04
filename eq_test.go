package main

import (
	"math"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
)

// stageEQ shows the player with the equalizer open, holding eq.
func stageEQ(t *testing.T, eq EQ) (w *gunim.Window, root *playerRoot, run func(int), graph geom.Rect) {
	t.Helper()
	s := library4()
	s.EQ = eq
	w, root, run = stage(t, geom.Sz(1100, 720), s)
	_ = w.Client().Focus("player")
	withUI(t, w, run, func(u *gunim.UI) { root.eq.show(true, u) })
	run(60)
	return w, root, run, boundsOf(t, w, run, root.eq.graph)
}

// lastEQ returns the settings the window sent last, and how many it
// sent.
func lastEQ(w *gunim.Window) (last EQ, n int) {
	for _, in := range intents(w) {
		if s, ok := in.(SetEQ); ok {
			last = s.EQ
			n++
		}
	}
	return last, n
}

func click(w *gunim.Window, at geom.Point, clicks int) {
	w.Input(input.PointerMove{Pos: at})
	w.Input(input.PointerDown{Pos: at, Button: input.ButtonPrimary, Clicks: clicks})
	w.Input(input.PointerUp{Pos: at, Button: input.ButtonPrimary})
}

func TestADoubleClickAddsABandThatGrowsIn(t *testing.T) {
	w, root, run, graph := stageEQ(t, EQ{})
	g := root.eq.graph
	at := graph.Min.Add(geom.Pt(g.xOf(1000), g.yOf(6)))
	click(w, at, 1)
	click(w, at, 2)
	run(1)
	eq, n := lastEQ(w)
	if n == 0 || len(eq.Bands) != 1 {
		t.Fatalf("a double-click sent %d settings, the last %+v; want one band", n, eq)
	}
	b := eq.Bands[0]
	if b.Kind != audio.Bell || math.Abs(float64(b.Freq)-1000) > 15 || math.Abs(float64(b.Gain)-6) > 0.3 || !b.On {
		t.Fatalf("the band added is %+v, want a bell at 1 kHz, +6 dB", b)
	}
	// Its point grows in, frame by frame, to its size.
	d := g.dots[b.ID]
	last := float32(-1)
	for f := range 40 {
		v := d.in.Value()
		if v < last-0.2 {
			t.Fatalf("frame %d: the point went from %v to %v as it grew in", f, last, v)
		}
		last = v
		run(1)
	}
	if v := d.in.Value(); math.Abs(float64(v)-1) > 0.02 {
		t.Fatalf("after 40 frames the point is %v grown", v)
	}
}

func TestAPointDragsItsBandAndTheReadoutFollows(t *testing.T) {
	bell := audio.Band{ID: 1, Kind: audio.Bell, Freq: 500, Gain: 0, Q: 1, On: true}
	w, root, run, graph := stageEQ(t, EQ{Bands: []audio.Band{bell}})
	g := root.eq.graph
	from := graph.Min.Add(geom.Pt(g.xOf(500), g.yOf(0)))
	w.Input(input.PointerMove{Pos: from})
	w.Input(input.PointerDown{Pos: from, Button: input.ButtonPrimary, Clicks: 1})
	run(1)
	to := graph.Min.Add(geom.Pt(g.xOf(4000), g.yOf(-9)))
	for i := 1; i <= 10; i++ {
		at := from.Add(to.Sub(from).Mul(float32(i) / 10))
		w.Input(input.PointerMove{Pos: at})
		run(1)
		// Each frame the point is where the pointer is.
		if got := graph.Min.Add(g.dotAt(g.dots[1])); math.Abs(float64(got.X-at.X)) > 1 || math.Abs(float64(got.Y-at.Y)) > 1 {
			t.Fatalf("step %d: the point is at %v, the pointer at %v", i, got, at)
		}
	}
	w.Input(input.PointerUp{Pos: to, Button: input.ButtonPrimary})
	run(1)
	eq, _ := lastEQ(w)
	b := eq.Bands[0]
	if math.Abs(float64(b.Freq)-4000) > 60 || math.Abs(float64(b.Gain)+9) > 0.3 {
		t.Fatalf("dragged to 4 kHz, -9 dB, the band is %+v", b)
	}
	if g.sel != 1 {
		t.Fatalf("the band dragged is not picked: picked %d", g.sel)
	}
	// The wheel on it narrows it.
	w.Input(input.Scroll{Pos: to, Notches: geom.Pt(0, 2)})
	run(1)
	eq, _ = lastEQ(w)
	if q := eq.Bands[0].Q; math.Abs(float64(q)-1.3225) > 0.01 {
		t.Fatalf("two notches up made Q %v, want 1.15² = 1.32", q)
	}
	// Delete takes it away.
	w.Input(input.KeyPress{Key: input.KeyDelete})
	run(1)
	if eq, _ = lastEQ(w); len(eq.Bands) != 0 {
		t.Fatalf("Delete left %+v", eq.Bands)
	}
}

func TestAPresetsPointsGlideFromWhereTheBandsWere(t *testing.T) {
	bell := audio.Band{ID: 1, Kind: audio.Bell, Freq: 100, Gain: -6, Q: 1, On: true}
	w, root, run, _ := stageEQ(t, EQ{Bands: []audio.Band{bell}})
	g := root.eq.graph
	start := g.dotAt(g.dots[1])
	withUI(t, w, run, func(u *gunim.UI) { g.preset(presets[3].bands, u) })
	// "Vocal presence": its first band is a low cut, so the point of
	// band 1 flies from the bell's place to the cut's, frame by frame.
	first := g.dotAt(g.dots[1])
	if d := first.Sub(start); math.Hypot(float64(d.X), float64(d.Y)) > 30 {
		t.Fatalf("a frame after the preset the point jumped from %v to %v", start, first)
	}
	run(90)
	want := geom.Pt(g.xOf(90), g.yOf(0))
	if got := g.dotAt(g.dots[1]); math.Abs(float64(got.X-want.X)) > 1 || math.Abs(float64(got.Y-want.Y)) > 1 {
		t.Fatalf("1.5 s after the preset the point is at %v, want %v", got, want)
	}
	eq, _ := lastEQ(w)
	if len(eq.Bands) != 4 || eq.Bands[0].ID != 1 || eq.Bands[0].Kind != audio.LowCut {
		t.Fatalf("the preset sent %+v, want its four bands, the first keeping ID 1", eq.Bands)
	}
}

func TestSettingsOlderThanTheWindowsArePassedOver(t *testing.T) {
	bell := audio.Band{ID: 1, Kind: audio.Bell, Freq: 1000, Gain: 3, Q: 1, On: true}
	w, root, run, _ := stageEQ(t, EQ{Bands: []audio.Band{bell}})
	g := root.eq.graph
	withUI(t, w, run, func(u *gunim.UI) {
		g.bands[0].Gain = 9
		g.send(u)
		g.bands[0].Gain = 12
		g.send(u)
	})
	// The application's answer to the first change comes back.
	old := EQ{Bands: []audio.Band{{ID: 1, Kind: audio.Bell, Freq: 1000, Gain: 9, Q: 1, On: true}}, Seq: g.seq - 1}
	withUI(t, w, run, func(*gunim.UI) { g.take(old) })
	if g.bands[0].Gain != 12 {
		t.Fatalf("an older answer set the gain back to %v, want 12", g.bands[0].Gain)
	}
}

func TestAKindPickedInTheStripTurnsTheBand(t *testing.T) {
	bell := audio.Band{ID: 1, Kind: audio.Bell, Freq: 80, Gain: 4, Q: 1, On: true}
	w, root, run, graph := stageEQ(t, EQ{Bands: []audio.Band{bell}})
	g := root.eq.graph
	click(w, graph.Min.Add(geom.Pt(g.xOf(80), g.yOf(4))), 1)
	run(2)
	strip := boundsOf(t, w, run, root.eq.strip)
	pill := root.eq.strip.slotOf[int(audio.LowCut)]
	click(w, strip.Min.Add(pill.Min).Add(geom.Pt(10, 10)), 1)
	run(1)
	eq, _ := lastEQ(w)
	if b := eq.Bands[0]; b.Kind != audio.LowCut || b.Slope != 24 || b.Gain != 0 {
		t.Fatalf("Low cut picked made %+v, want a low cut, 24 dB an octave", b)
	}
}
