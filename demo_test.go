package main

import (
	"math"
	"testing"
	"time"

	"github.com/marrasen/gunim/audio"
)

func TestADemoSongPlaysAtAHealthyLevelFasterThanItPlays(t *testing.T) {
	for _, s := range demoSongs {
		src := s.source()
		buf := make([]float32, 2*audio.SampleRate)
		// A second from the groove, where everything plays.
		if err := src.SeekFrame(int64(60 / s.bpm * 4 * (introBars + 2) * audio.SampleRate)); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		n, err := src.Read(buf)
		took := time.Since(start)
		if err != nil || n != audio.SampleRate {
			t.Fatalf("%s: read %d frames, %v", s.title, n, err)
		}
		peak, sum := 0.0, 0.0
		for _, v := range buf {
			peak = max(peak, math.Abs(float64(v)))
			sum += float64(v) * float64(v)
		}
		rms := math.Sqrt(sum / float64(len(buf)))
		t.Logf("%s: %v for a second, peak %.2f, rms %.3f, %v long", s.title, took, peak, rms, time.Duration(s.length()*float64(time.Second)).Round(time.Second))
		if took > 350*time.Millisecond {
			t.Errorf("%s takes %v to make a second, want well under real time", s.title, took)
		}
		if peak > 0.7 || rms < 0.08 {
			t.Errorf("%s: peak %.2f, rms %.3f; want loud enough, with headroom under 0.7", s.title, peak, rms)
		}
	}
}
