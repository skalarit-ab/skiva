package main

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/marrasen/gunim/audio"
)

func TestTheNextTrackIsQueuedToFollowWithoutAGap(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	a.handle(PlayTrack{ID: all[0]})
	if a.upNext == nil || a.upNext.ID != all[1] {
		t.Fatalf("queued %v, want the list's next, %d", a.upNext, all[1])
	}
	// Up next goes first: it takes the queue's place.
	a.handle(Enqueue{Tracks: []int{all[3]}})
	a.prepareNext()
	if a.upNext.ID != all[3] || !a.upFromQueue {
		t.Fatalf("with Up next, queued %d (from Up next %v), want %d from it", a.upNext.ID, a.upFromQueue, all[3])
	}
	starts := a.Starts
	a.turned()
	if a.Current != all[3] || a.Starts != starts+1 || len(a.Queue) != 0 {
		t.Fatalf("turned to %d, starts %d, Up next %v; want %d, one more start, and Up next empty",
			a.Current, a.Starts, a.Queue, all[3])
	}
	// The list goes on from where Up next broke into it.
	if a.upNext == nil || a.upNext.ID != all[1] {
		t.Fatalf("after Up next, queued %v, want the list's next, %d", a.upNext, all[1])
	}
}

func TestRepeatOneQueuesTheTrackAgain(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	a.handle(PlayTrack{ID: all[2]})
	a.handle(CycleRepeat{})
	a.handle(CycleRepeat{})
	a.prepareNext()
	if a.Repeat != RepeatOne || a.upNext == nil || a.upNext.ID != all[2] {
		t.Fatalf("repeating one, queued %v, want the track again", a.upNext)
	}
}

func TestShufflesPickHoldsSoNextPlaysWhatWasQueued(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	a.handle(ToggleShuffle{})
	a.handle(PlayTrack{ID: all[0]})
	queued := a.upNext.ID
	for range 5 {
		if n := a.next(); n != queued {
			t.Fatalf("shuffle picked %d, then %d, for the same track", queued, n)
		}
	}
	a.handle(Skip{})
	if a.Current != queued {
		t.Fatalf("Next played %d, want %d, the track queued", a.Current, queued)
	}
	if !slices.Contains(all, a.upNext.ID) || a.upNext.ID == a.Current {
		t.Fatalf("after Next, queued %d", a.upNext.ID)
	}
}

func TestTheTrackToFollowIsReadAheadForItsGain(t *testing.T) {
	a := newApp(context.Background(), newDeck(audio.NewMixer()), "")
	all := ids(a)
	reading := func() []int {
		a.z.mu.Lock()
		defer a.z.mu.Unlock()
		out := make([]int, 0, len(a.z.queue))
		for _, e := range a.z.queue {
			out = append(out, e.ID)
		}
		return out
	}
	a.handle(SetGainMode{Mode: GainTrack})
	if got := reading(); len(got) != 0 {
		t.Fatalf("with nothing playing, reading %v, want nothing", got)
	}
	// The track playing first, then the track to follow it.
	a.handle(PlayTrack{ID: all[0]})
	if got := reading(); !slices.Equal(got, all[:2]) {
		t.Fatalf("playing the first, reading %v, want %v", got, all[:2])
	}
	// The next track's loudness learned, it is queued again at its gain.
	a.entries[all[0]].analyzed(analysis{Loud: true, LUFS: -24, Peak: 0.2})
	a.entries[all[1]].analyzed(analysis{Loud: true, LUFS: -12, Peak: 0.5})
	a.prepareNext()
	if got := reading(); len(got) != 0 {
		t.Fatalf("with both read, reading %v, want nothing", got)
	}
	if want := targetLUFS - -12.0; math.Abs(a.upGain-want) > 0.01 {
		t.Fatalf("the next track queued at %+.2f dB, want %+.2f", a.upGain, want)
	}
	// Album gain wants the rest of the album as well.
	a.handle(SetGainMode{Mode: GainAlbum})
	a.prepareNext()
	if got := reading(); !slices.Equal(got, all[2:]) {
		t.Fatalf("with album gain, reading %v, want the rest of the album, %v", got, all[2:])
	}
	// Stopped, the player reads nothing.
	a.handle(PlayTrack{ID: all[3]})
	a.ended()
	if a.Current != 0 {
		t.Fatalf("after the last track, %d plays, want nothing", a.Current)
	}
	if got := reading(); len(got) != 0 {
		t.Fatalf("stopped, reading %v, want nothing", got)
	}
}
