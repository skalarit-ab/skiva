package main

import (
	"math"
	"sync"
	"time"

	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/audio"
)

// A deck plays one track at a time through the mixer. The application
// half starts, pauses and moves it; the window half reads where it is
// and what it sounds like, every frame, to draw. Its methods are safe
// from any goroutine.
type deck struct {
	mix *audio.Mixer

	mu     sync.Mutex
	voice  *audio.Voice
	closer func()
	volume float32
	// an measures the mix, and bands holds what it measured last.
	an *audio.Analyzer
	// eq is the equalizer every track plays through.
	eq *audio.EQ
	// gain is the loudness gain of the track playing, as a ratio, on
	// top of the volume.
	gain float32
	// fading is done once the fade as the player closes has ended.
	fading <-chan struct{}
}

// bandCount is how many bands of pitch the visuals draw.
const bandCount = 36

func newDeck(mix *audio.Mixer) *deck {
	an := audio.NewAnalyzer(mix, bandCount)
	an.Tilt = 4
	return &deck{mix: mix, volume: 0.8, an: an, eq: audio.NewEQ(), gain: 1}
}

// crossfade is how long a track playing fades out as another starts.
const crossfade = 350 * time.Millisecond

// play starts src, fading out the track playing. closer runs once the
// voice is done with src. It returns the new voice.
func (d *deck) play(src audio.Seeker, closer func(), paused bool) *audio.Voice {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.voice != nil {
		old, oldClose := d.voice, d.closer
		old.Stop(crossfade)
		go func() {
			<-old.Done()
			oldClose()
		}()
	}
	// A volume of zero would play at 1, as Options takes it: a muted
	// track starts as near silent as makes no odds.
	d.voice = d.mix.Play(src, audio.Options{Volume: max(d.volume*d.gain, 1e-6), FadeIn: 30 * time.Millisecond, Paused: paused,
		Insert: d.eq.Insert()})
	d.closer = closer
	return d.voice
}

// stop ends the track playing, fading it out.
func (d *deck) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.voice == nil {
		return
	}
	v, c := d.voice, d.closer
	v.Stop(crossfade)
	go func() {
		<-v.Done()
		c()
	}()
	d.voice, d.closer = nil, nil
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
	} else {
		d.voice.Resume()
	}
}

// seek moves the track playing to at.
func (d *deck) seek(at time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.voice != nil {
		_ = d.voice.Seek(at)
	}
}

// setVolume moves the volume to v, smoothly.
func (d *deck) setVolume(v float32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.volume = v
	if d.voice != nil {
		d.voice.SetVolume(v*d.gain, anim.Spring{Response: 0.25, Damping: 1})
	}
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
// for the next track to start at, or gliding where glide, over most of
// a second, as a gain set while a track plays should go unnoticed.
func (d *deck) setGain(db float64, glide bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gain = float32(math.Pow(10, db/20))
	if d.voice != nil && glide {
		d.voice.SetVolume(d.volume*d.gain, anim.Spring{Response: 0.8, Damping: 1})
	}
}

// fadeOut fades the track playing out over fade, as the player closes;
// quiet waits for it, at most for long.
func (d *deck) fadeOut(fade time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.voice == nil {
		return
	}
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
