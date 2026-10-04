package main

import (
	"io"
	"math"

	"github.com/marrasen/gunim/audio"
)

// A song is a demo track made in code: drums, bass, a pad, an
// arpeggio and a lead over four chords that repeat, in sections that
// come and go. Each sample is a function of its time alone, so the
// song plays from anywhere at once.
type song struct {
	title, artist string
	bpm           float64
	// root is the key's root, in Hz, and chords the four chords, as
	// semitones above it.
	root   float64
	chords [4][3]int
	// lazy picks the drum pattern: four on the floor, or a lazier
	// one with the kick off the beat.
	lazy bool
	// lead is the melody, a scale step for each eighth note of two
	// bars: -1 rests, and -2 holds the note before.
	lead [16]int
	// bright tilts the sound: how much of the arpeggio and hats.
	bright float64
}

// Sections, in bars: the intro brings the pad and hats, then the
// groove, a breakdown without drums, the groove again with the lead,
// and an outro that fades.
const (
	introBars = 4
	grooveEnd = 16
	breakEnd  = 24
	leadEnd   = 36
	songBars  = 40
)

// length returns how long the song lasts.
func (s *song) length() float64 { return songBars * 4 * 60 / s.bpm }

// note returns the frequency semitones above the root, octave octaves
// up.
func (s *song) note(semitones, octave int) float64 {
	return s.root * math.Exp2(float64(semitones)/12+float64(octave))
}

// The beats of a bar a kick lands on.
var (
	fourOnTheFloor = [...]float64{0, 1, 2, 3}
	lazyKick       = [...]float64{0, 1.5, 2.5}
)

// minorScale is the steps of the natural minor scale.
var minorScale = [7]int{0, 2, 3, 5, 7, 8, 10}

// at returns the song's left and right samples at time t, in seconds.
func (s *song) at(t float64, n int64) (l, r float64) {
	spb := 60 / s.bpm
	beat := t / spb
	bar := int(beat / 4)
	if bar >= songBars {
		return 0, 0
	}
	chord := s.chords[bar%4]
	groove := bar >= introBars && bar < grooveEnd || bar >= breakEnd && bar < leadEnd+2
	drums := groove
	hats := bar >= introBars/2 && bar < leadEnd+2

	// Drums.
	if drums {
		// The kick on each beat, or on 1, the and of 2, and 3.
		hits := fourOnTheFloor[:]
		if s.lazy {
			hits = lazyKick[:]
		}
		b := math.Mod(beat, 4)
		last := hits[0]
		for _, h := range hits {
			if h <= b {
				last = h
			}
		}
		k := kick((b - last) * spb)
		l += 0.55 * k
		r += 0.55 * k
		if b := math.Mod(beat, 2); b >= 1 {
			sn := snare((b-1)*spb, n)
			l += 0.22 * sn
			r += 0.2 * sn
		}
	}
	if hats {
		half := math.Mod(beat, 1)
		if half >= 0.5 {
			h := hat((half-0.5)*spb, n)
			l += 0.05 * s.bright * h
			r += 0.07 * s.bright * h
		}
		if s.bright > 1 && drums {
			// Sixteenths, quieter, between.
			q := math.Mod(beat, 0.5)
			h := hat(q*spb, n+7)
			l += 0.02 * h
			r += 0.025 * h
		}
	}

	// Bass, eighth notes on the chord's root.
	if groove || bar >= grooveEnd && bar < breakEnd && bar%2 == 1 {
		eighth := math.Mod(beat*2, 1) * spb / 2
		f := s.note(chord[0], -1)
		b := bass(t, f) * envelope(eighth, 0.005, spb/2*0.85, 0.05)
		l += 0.2 * b
		r += 0.2 * b
	}

	// Pad: the chord, each note two voices a little apart, swelling
	// with each bar.
	inBar := math.Mod(beat, 4) / 4
	swell := 0.55 + 0.45*math.Sin(math.Pi*inBar)
	for i, semi := range chord {
		f := s.note(semi, 1)
		a := math.Sin(2 * math.Pi * f * 1.002 * t)
		b := math.Sin(2 * math.Pi * f * 0.998 * t)
		w := 0.045 * swell
		l += w * (a + 0.6*b) * (1 - 0.15*float64(i))
		r += w * (b + 0.6*a) * (0.85 + 0.15*float64(i))
	}

	// Arpeggio, sixteenth notes up the chord over two octaves.
	if bar >= introBars/2 {
		six := beat * 4
		step := int(six) % 6
		semi := chord[step%3] + 12*(step/3)
		f := s.note(semi, 2)
		tau := (six - math.Floor(six)) * spb / 4
		v := pluck(tau, f)
		pan := 0.5 + 0.35*math.Sin(beat*math.Pi/2)
		l += 0.06 * s.bright * v * (1 - pan) * 2
		r += 0.06 * s.bright * v * pan * 2
	}

	// Lead, in the second groove.
	if bar >= breakEnd && bar < leadEnd {
		// The eighth of the two-bar phrase, and the note sounding in
		// it: a held eighth carries on the note before.
		pos := math.Mod(beat*2, 16)
		start := int(pos)
		for start > 0 && s.lead[start] == -2 {
			start--
		}
		if deg := s.lead[start]; deg >= 0 {
			tau := (pos - float64(start)) * spb / 2
			semi := minorScale[deg%7] + 12*(deg/7)
			f := s.note(semi, 2)
			v := lead(tau, f, t)
			l += 0.09 * v
			r += 0.09 * v
		}
	}

	// The song fades in over its first two seconds, and out over its
	// last four bars.
	fade := min(1, t/2)
	if bar >= songBars-4 {
		fade *= (songBars*4 - beat) / 16
	}
	return soft(0.55 * l * fade), soft(0.55 * r * fade)
}

// soft rounds off peaks rather than clipping them.
func soft(x float64) float64 { return math.Tanh(1.3*x) / math.Tanh(1.3) * 0.8 }

// envelope rises over attack, holds until hold, and falls over
// release.
func envelope(tau, attack, hold, release float64) float64 {
	switch {
	case tau < attack:
		return tau / attack
	case tau < hold:
		return 1
	case tau < hold+release:
		return 1 - (tau-hold)/release
	}
	return 0
}

// kick is a kick drum tau seconds after it is struck: a sine whose
// pitch drops from 150 to 50 Hz.
func kick(tau float64) float64 {
	phase := 2 * math.Pi * (50*tau + 100*0.03*(1-math.Exp(-tau/0.03)))
	return math.Sin(phase) * math.Exp(-tau/0.28)
}

// snare is a snare drum: noise and a low ring.
func snare(tau float64, n int64) float64 {
	return noise(n)*math.Exp(-tau/0.11) + 0.5*math.Sin(2*math.Pi*185*tau)*math.Exp(-tau/0.07)
}

// hat is a closed hi-hat: the noise's quick changes alone, cut short.
func hat(tau float64, n int64) float64 {
	return (noise(n) - noise(n-1)) * math.Exp(-tau/0.025)
}

// bass is a soft saw at f: its first five harmonics.
func bass(t, f float64) float64 {
	v := 0.0
	for k := 1.0; k <= 5; k++ {
		v += math.Sin(2*math.Pi*f*k*t) / k
	}
	return v
}

// pluck is a plucked note tau seconds old.
func pluck(tau, f float64) float64 {
	w := 2 * math.Pi * f * tau
	return (math.Sin(w) + 0.25*math.Sin(2*w)*math.Exp(-tau/0.04)) * math.Exp(-tau/0.18)
}

// lead is a soft lead note tau seconds old, with a slow vibrato.
func lead(tau, f, t float64) float64 {
	vib := 1 + 0.004*math.Sin(2*math.Pi*5.5*t)
	w := 2 * math.Pi * f * vib * tau
	att := min(1, tau/0.02)
	return att * (math.Sin(w) + 0.3*math.Sin(3*w)/3) * math.Exp(-tau/0.9)
}

// noise returns a sample of white noise for frame n: the same for the
// same n, so a song plays the same each time.
func noise(n int64) float64 {
	x := uint64(n) * 0x9e3779b97f4a7c15
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return float64(x>>11)/float64(1<<53)*2 - 1
}

// peaks returns the song's loudness along it, as analyze does, from
// samples spread through each stretch: a song is a function of time,
// so it need not be played through.
func (s *song) peaks() []float32 {
	const per = 96
	total := s.length()
	power := make([]float64, peakCount)
	for i := range power {
		for k := range per {
			t := (float64(i) + float64(k)/per) * total / peakCount
			l, r := s.at(t, int64(t*audio.SampleRate))
			power[i] += (l*l + r*r) / 2 / per
		}
	}
	return shape(power)
}

// songSource plays a song as an [audio.Seeker].
type songSource struct {
	s  *song
	at int64
}

func (s *song) source() *songSource { return &songSource{s: s} }

// Read implements [audio.Source].
func (src *songSource) Read(dst []float32) (int, error) {
	total := src.Len()
	n := 0
	for n < len(dst)/2 && src.at < total {
		t := float64(src.at) / audio.SampleRate
		l, r := src.s.at(t, src.at)
		dst[2*n], dst[2*n+1] = float32(l), float32(r)
		n++
		src.at++
	}
	if src.at >= total {
		return n, io.EOF
	}
	return n, nil
}

// SeekFrame implements [audio.Seeker].
func (src *songSource) SeekFrame(f int64) error {
	src.at = max(0, min(f, src.Len()))
	return nil
}

// Len implements [audio.Seeker].
func (src *songSource) Len() int64 { return int64(src.s.length() * audio.SampleRate) }

// demoSongs are the songs the player always has.
var demoSongs = []*song{
	{
		title: "Night Drive", artist: "The Oscillators", bpm: 100, root: 110,
		// Am F C G
		chords: [4][3]int{{0, 3, 7}, {-4, 0, 3}, {3, 7, 10}, {-2, 2, 5}},
		lead:   [16]int{4, -2, 3, 2, 0, -2, -1, 2, 4, -2, 5, 4, 2, -2, -2, -1},
		bright: 1,
	},
	{
		title: "Glass Garden", artist: "Sine Language", bpm: 84, root: 146.83,
		// Dm Bb F C
		chords: [4][3]int{{0, 3, 7}, {-4, 0, 3}, {3, 7, 10}, {-2, 2, 5}},
		lead:   [16]int{7, -2, -2, 6, 4, -2, 2, -1, 3, -2, 4, -2, 2, -2, -2, -1},
		lazy:   true, bright: 0.7,
	},
	{
		title: "Neon Tide", artist: "The Oscillators", bpm: 122, root: 87.31,
		// Fm Db Ab Eb
		chords: [4][3]int{{0, 3, 7}, {-4, 0, 3}, {3, 7, 10}, {-2, 2, 5}},
		lead:   [16]int{0, 2, 4, -2, 7, -2, 4, 2, 3, -2, 2, 0, -1, 4, -2, -1},
		bright: 1.4,
	},
	{
		title: "Paper Moons", artist: "Low Pass Club", bpm: 76, root: 130.81,
		// Cm Ab Eb Bb, played slow and soft
		chords: [4][3]int{{0, 3, 7}, {-4, 0, 3}, {3, 7, 10}, {-2, 2, 5}},
		lead:   [16]int{2, -2, 4, -2, 3, 2, 0, -1, 4, -2, -2, 2, 0, -2, -2, -1},
		lazy:   true, bright: 0.55,
	},
}
