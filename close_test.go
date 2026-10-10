package main

import (
	"context"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/geom"
)

// sendIntent has the player's view send an intent, as a click would.
type sendIntent struct{ in gunim.Intent }

// Closing the window with a track paused leaves it quiet: stopping the
// paused track ends its voice at once, and the player, closing, takes
// that for no reason to play the next.
func TestClosingThePlayerWhilePausedPlaysNothing(t *testing.T) {
	m := audio.NewMixer()
	d := newDeck(m)
	stopMix := make(chan struct{})
	defer close(stopMix)
	go func() {
		// The speakers, pulling the mix along
		buf := make([]float32, 2*480)
		for {
			select {
			case <-stopMix:
				return
			default:
				m.Mix(buf)
				time.Sleep(time.Millisecond)
			}
		}
	}()
	w := gunim.NewOffscreen(geom.Sz(1100, 720), nil)
	registerViews(w, d, false, "", opened{})
	gunim.RegisterPatch(w, "player", func(r *playerRoot, s sendIntent, u *gunim.UI) { u.Send(r, s.in) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() {
		served <- serve(ctx, w.Client(), d, setup{}, startAt{on: true, track: 1}, func(*gunim.NowPlaying) error { return nil })
	}()
	frames := func(n int) {
		for range n {
			w.Frame(time.Second / 60)
			time.Sleep(2 * time.Millisecond)
		}
	}
	// until runs frames until ok says so, for up to ten seconds.
	until := func(ok func() bool) bool {
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end); {
			if ok() {
				return true
			}
			frames(1)
		}
		return false
	}
	send := func(in gunim.Intent) {
		t.Helper()
		// The view may not be mounted yet: try until it takes the patch
		if !until(func() bool { return w.Client().Patch("player", sendIntent{in}) == nil }) {
			t.Fatalf("the player's view never took %T", in)
		}
		frames(10)
	}
	voice := func() *audio.Voice {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.voice
	}
	if !until(func() bool { return voice() != nil }) {
		t.Fatal("the player never started its track")
	}
	send(TogglePlay{})
	paused := voice()
	if !until(func() bool { return paused.Paused() }) {
		t.Fatal("the track did not pause")
	}
	send(CloseAsked{})
	frames(60)
	if v := voice(); v != paused {
		t.Fatal("closing the player started another track")
	}
	select {
	case err := <-served:
		t.Fatalf("the player stopped serving before its window closed: %v", err)
	default:
	}
}
