package main

import (
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gunim/audio"
)

// fakeSpeaker counts what the deck asks of the speaker.
type fakeSpeaker struct {
	mu      sync.Mutex
	resting bool
}

func (f *fakeSpeaker) SetLatency(time.Duration)   {}
func (f *fakeSpeaker) Latency() time.Duration     { return 0 }
func (f *fakeSpeaker) Suspend() error             { f.set(true); return nil }
func (f *fakeSpeaker) Resume() error              { f.set(false); return nil }
func (f *fakeSpeaker) set(on bool)                { f.mu.Lock(); f.resting = on; f.mu.Unlock() }
func (f *fakeSpeaker) rests() bool                { f.mu.Lock(); defer f.mu.Unlock(); return f.resting }
func (f *fakeSpeaker) after(d time.Duration) bool { time.Sleep(d); return f.rests() }

func TestTheSpeakerRestsWhileNothingSounds(t *testing.T) {
	was := restAfter
	restAfter = 30 * time.Millisecond
	defer func() { restAfter = was }()
	long := 4 * restAfter
	d := newDeck(audio.NewMixer())
	spk := &fakeSpeaker{}
	d.setSpeaker(spk)
	if !spk.after(long) {
		t.Fatal("with nothing to play, the speaker runs on")
	}
	// A track taken up paused leaves it resting.
	d.play(audio.NewClip(make([]float32, 2*audio.SampleRate)).Source(), func() {}, true, 0)
	if !spk.after(long) {
		t.Fatal("with a track paused, the speaker runs on")
	}
	// Playing wakes it, and it stays awake while the track plays.
	d.setPaused(false)
	if spk.rests() {
		t.Fatal("playing, the speaker still rests")
	}
	if spk.after(long) {
		t.Fatal("while a track plays, the speaker rested")
	}
	// Paused, it rests a moment later, once the pause has faded.
	d.setPaused(true)
	if spk.rests() {
		t.Fatal("the speaker rested before the pause had faded")
	}
	if !spk.after(long) {
		t.Fatal("paused, the speaker runs on")
	}
	// A seek while paused wakes it, for the mixer to mark where the
	// seek lands, and it rests again after.
	d.seek(time.Second / 2)
	if spk.rests() {
		t.Fatal("a seek while paused left the speaker resting")
	}
	if !spk.after(long) {
		t.Fatal("after a seek while paused, the speaker runs on")
	}
	// Stopped, it wakes for the fade and rests after it.
	d.setPaused(false)
	d.stop()
	if spk.rests() {
		t.Fatal("the speaker rested before the track had faded out")
	}
	if !spk.after(long) {
		t.Fatal("stopped, the speaker runs on")
	}
}
