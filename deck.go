package main

import (
	"math"
	"sync"
	"time"

	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/audio/speaker"
)

// A deck plays one track at a time through the mixer. The application
// half starts, pauses and moves it; the window half reads where it is
// and what it sounds like, every frame, to draw. Its methods are safe
// from any goroutine.
type deck struct {
	mix *audio.Mixer

	mu     sync.Mutex
	voice  *audio.Voice
	volume float32
	// cur is the track the voice plays, and next the one it turns to
	// once cur ends, without a gap.
	cur, next *track
	// spk is the speaker, whose buffer the deck sets: short while the
	// player is seen, long while it plays on unseen.
	spk speakers
	// an measures the mix, and bands holds what it measured last.
	an *audio.Analyzer
	// eq is the equalizer every track plays through.
	eq *audio.EQ
	// fading is done once the fade as the player closes has ended.
	fading <-chan struct{}
	// resting says the speaker is suspended, as nothing has sounded for
	// a moment; woken counts the times it was woken, so a rest asked
	// for before the last waking lapses.
	resting bool
	woken   int
}

// bandCount is how many bands of pitch the visuals draw.
const bandCount = 36

func newDeck(mix *audio.Mixer) *deck {
	an := audio.NewAnalyzer(mix, bandCount)
	an.Tilt = 4
	return &deck{mix: mix, volume: 0.8, an: an, eq: audio.NewEQ()}
}

// crossfade is how long a track playing fades out as another starts.
const crossfade = 350 * time.Millisecond

// A track is a source the deck plays, at its own loudness gain, and
// what to close once it is done with.
type track struct {
	src    *gained
	closer func()
}

// play starts src, at its own loudness gain db, fading out the track
// playing at the gain it had. closer runs once the voice is done with
// src. It returns the new voice.
func (d *deck) play(src audio.Seeker, closer func(), paused bool, db float64) *audio.Voice {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.wake()
	d.retire()
	d.cur = &track{src: newGained(src, ratio(db)), closer: closer}
	// A volume of zero would play at 1, as Options takes it: a muted
	// track starts as near silent as makes no odds.
	d.voice = d.mix.Play(d.cur.src, audio.Options{Volume: max(d.volume, 1e-6), FadeIn: 30 * time.Millisecond,
		Paused: paused, Insert: d.eq.Insert()})
	if paused {
		d.rest()
	}
	return d.voice
}

// retire fades the voice playing out, and closes its tracks once it is
// done. It runs with mu held.
func (d *deck) retire() {
	if d.voice == nil {
		return
	}
	old, tracks := d.voice, []*track{d.cur, d.next}
	// A track ending as it fades turns to nothing.
	old.Then(nil)
	old.Stop(crossfade)
	go func() {
		<-old.Done()
		for _, t := range tracks {
			if t != nil {
				t.closer()
			}
		}
	}()
	d.voice, d.cur, d.next = nil, nil, nil
}

// queue has the voice turn to src, at its own loudness gain db, once
// the track playing ends, without a gap; nil takes a track queued away.
func (d *deck) queue(src audio.Seeker, closer func(), db float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.voice == nil {
		if closer != nil {
			closer()
		}
		return
	}
	if d.next != nil {
		d.next.closer()
		d.next = nil
	}
	if src == nil {
		d.voice.Then(nil)
		return
	}
	d.next = &track{src: newGained(src, ratio(db)), closer: closer}
	d.voice.Then(d.next.src)
}

// turned takes the track the voice turned to as the one playing, and
// closes the one before, which the voice reads no more.
func (d *deck) turned() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.next == nil {
		return
	}
	if d.cur != nil {
		d.cur.closer()
	}
	d.cur, d.next = d.next, nil
}

// stop ends the track playing, fading it out.
func (d *deck) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.wake()
	d.retire()
	d.rest()
}

// setPaused pauses or resumes the track playing.
func (d *deck) setPaused(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.voice == nil {
		return
	}
	if on {
		d.voice.Pause()
		d.rest()
	} else {
		d.wake()
		d.voice.Resume()
	}
}

// seek moves the track playing to at.
func (d *deck) seek(at time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.voice != nil {
		// The mixer marks where a seek lands as it mixes: the speaker
		// runs a moment, paused or not, for the playhead to show it.
		d.wake()
		_ = d.voice.Seek(at)
		if d.voice.Paused() {
			d.rest()
		}
	}
}

// setVolume moves the volume to v, smoothly.
func (d *deck) setVolume(v float32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.volume = v
	if d.voice != nil {
		d.voice.SetVolume(max(v, 1e-6), anim.Spring{Response: 0.25, Damping: 1})
	}
}

// Latencies the deck sets the speaker to: short while the player is
// seen, so its buttons answer at once, and long while it plays on
// unseen, as with a phone's screen off, so the sound rides out the
// moments the system is busy elsewhere. What the window shows of the
// sound follows it as heard, whatever the latency.
const (
	seenLatency   = 120 * time.Millisecond
	unseenLatency = 400 * time.Millisecond
)

// setSeen sets the speaker's buffer for the player seen or unseen.
func (d *deck) setSeen(seen bool) {
	d.mu.Lock()
	spk := d.spk
	d.mu.Unlock()
	if spk == nil {
		return
	}
	if seen {
		spk.SetLatency(seenLatency)
	} else {
		spk.SetLatency(unseenLatency)
	}
}

// speakers is what the deck asks of the speaker: a
// [*speaker.Speaker], or a stand-in in tests.
type speakers interface {
	SetLatency(time.Duration)
	Latency() time.Duration
	Suspend() error
	Resume() error
}

var _ speakers = (*speaker.Speaker)(nil)

// restAfter is how long the speaker plays on once nothing sounds,
// before it rests: long enough for a pause's or a track's fade to end,
// and for the playhead to reach a seek.
var restAfter = time.Second

// setSpeaker gives the deck the speaker, which rests until something
// plays.
func (d *deck) setSpeaker(spk speakers) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.spk = spk
	d.rest()
}

// wake starts the speaker again where it rests, and lets a rest asked
// for lapse. A deck that sounds, or changes what it would sound, wakes
// it: a voice stops, and a seek lands, only as the mixer runs. It runs
// with mu held.
func (d *deck) wake() {
	d.woken++
	if d.resting {
		d.resting = false
		_ = d.spk.Resume()
	}
}

// rest suspends the speaker restAfter from now, unless it is woken
// first or a voice plays then, so a player paused or stopped mixes no
// silence and the system's sound server has nothing to play. It runs
// with mu held.
func (d *deck) rest() {
	if d.spk == nil {
		return
	}
	woken := d.woken
	time.AfterFunc(restAfter, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.woken != woken || d.resting || d.voice != nil && !d.voice.Paused() {
			return
		}
		d.resting = true
		_ = d.spk.Suspend()
	})
}

// lag is how long the speaker takes to play what is mixed.
func (d *deck) lag() time.Duration {
	d.mu.Lock()
	spk := d.spk
	d.mu.Unlock()
	if spk == nil {
		return 0
	}
	return spk.Latency()
}

// position returns where the track playing is, as heard, and its
// length.
func (d *deck) position() (at, length time.Duration) {
	d.mu.Lock()
	v := d.voice
	d.mu.Unlock()
	if v == nil {
		return 0, 0
	}
	return v.Position(), v.Len()
}

// measure fills bands with how loud each band of pitch is being heard,
// and returns the level of the whole sound.
func (d *deck) measure(bands []float32) float32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.an.Bands(bands)
}

// spectrum fills heard and before with how loud the sound is at each
// of freqs, in decibels: as heard, and before the equalizer.
func (d *deck) spectrum(freqs, heard, before []float32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.an.Spectrum(freqs, heard, before)
}

// setGain sets the loudness gain of the track playing, in decibels:
// gliding where glide, over most of a second, as a gain set while a
// track plays should go unnoticed. A track fading out as another starts
// keeps its own.
func (d *deck) setGain(db float64, glide bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cur != nil {
		d.cur.src.set(ratio(db), glide)
	}
}

// ratio is a gain in decibels as a ratio.
func ratio(db float64) float32 { return float32(math.Pow(10, db/20)) }

// gained plays a source at a gain of its own, which glides where it is
// set to. Its methods are safe from any goroutine.
type gained struct {
	src audio.Seeker
	mu  sync.Mutex
	// g is the gain now, and to the gain it glides to.
	g, to float32
}

func newGained(src audio.Seeker, g float32) *gained { return &gained{src: src, g: g, to: g} }

// gainGlide is how far toward its gain a track's gain goes each frame:
// most of the way in about a third of a second.
var gainGlide = float32(1 - math.Exp(-1/(0.3*audio.SampleRate)))

// set sets the gain: at once, or gliding.
func (s *gained) set(g float32, glide bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.to = g
	if !glide {
		s.g = g
	}
}

func (s *gained) target() float32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.to
}

// Read implements [audio.Source].
func (s *gained) Read(dst []float32) (int, error) {
	n, err := s.src.Read(dst)
	s.mu.Lock()
	g, to := s.g, s.to
	for i := range n {
		if g != to {
			g += (to - g) * gainGlide
			if math.Abs(float64(to-g)) < 1e-5 {
				g = to
			}
		}
		dst[2*i] *= g
		dst[2*i+1] *= g
	}
	s.g = g
	s.mu.Unlock()
	return n, err
}

// SeekFrame implements [audio.Seeker].
func (s *gained) SeekFrame(f int64) error { return s.src.SeekFrame(f) }

// Len implements [audio.Seeker].
func (s *gained) Len() int64 { return s.src.Len() }

// fadeOut fades the track playing out over fade, as the player closes;
// quiet waits for it, at most for long.
func (d *deck) fadeOut(fade time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.voice == nil {
		return
	}
	d.wake()
	d.voice.Stop(fade)
	d.fading = d.voice.Done()
}

// quiet waits for the fade fadeOut began to end, at most for long.
func (d *deck) quiet(long time.Duration) {
	d.mu.Lock()
	done := d.fading
	d.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(long):
	}
}
