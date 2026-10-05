package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/marrasen/gunim/audio"
)

// targetLUFS is the level loudness gain brings each track or album to:
// ReplayGain 2's, which leaves room to lift a quiet track without
// clipping it. The volume sets how loud that plays.
const targetLUFS = -18

// analysis is what reading a track through tells: how loud it is as
// the ear hears it, its loudest sample, its loudness along it for the
// seek bar, and what it is stored as. Size and Mod tell whether the
// file changed since.
type analysis struct {
	Size int64
	Mod  time.Time
	// LUFS is the track's integrated loudness, and Loud says it has
	// one: a silent track has none.
	LUFS   float64
	Loud   bool
	Peak   float32
	Peaks  []float32
	Format audio.Format
}

// analyzed is an analysis done, for the track of key.
type analyzed struct {
	key string
	a   analysis
}

// analyze reads src to its end: its loudness, its peak, and its
// loudness along it, in peakCount stretches, as shape scales them.
func analyze(src audio.Seeker) analysis {
	var m audio.LoudnessMeter
	total := src.Len()
	power := make([]float64, peakCount)
	counts := make([]int, peakCount)
	per := max(total/peakCount, 1)
	buf := make([]float32, 2*4096)
	var at int64
	for {
		n, err := src.Read(buf)
		m.Write(buf[:2*n])
		for i := range n {
			b := min(int((at+int64(i))/per), peakCount-1)
			l, r := float64(buf[2*i]), float64(buf[2*i+1])
			power[b] += (l*l + r*r) / 2
			counts[b]++
		}
		at += int64(n)
		if err != nil || n == 0 {
			break
		}
	}
	var a analysis
	if l, ok := m.Integrated(); ok {
		a.LUFS, a.Loud = l, true
	}
	a.Peak = m.Peak()
	if total > 0 {
		for i := range power {
			power[i] /= float64(max(counts[i], 1))
		}
		a.Peaks = shape(power)
	}
	a.Format, _ = audio.FormatOf(src)
	return a
}

// analyzer reads tracks through in the background, one at a time, as
// the player needs them: the tracks it asks for last, most wanted first.
type analyzer struct {
	mu    sync.Mutex
	queue []*entry
	wake  chan struct{}
	out   chan analyzed
}

func newAnalyzer() *analyzer {
	return &analyzer{wake: make(chan struct{}, 1), out: make(chan analyzed, 16)}
}

// want sets the tracks to read, most wanted first, in place of those
// asked for before and not read yet. Nil reads nothing more.
func (z *analyzer) want(es []*entry) {
	z.mu.Lock()
	z.queue = es
	z.mu.Unlock()
	select {
	case z.wake <- struct{}{}:
	default:
	}
}

// run reads the tracks queued until done is closed.
func (z *analyzer) run(done <-chan struct{}) {
	seen := map[string]bool{}
	for {
		z.mu.Lock()
		var e *entry
		for len(z.queue) > 0 && e == nil {
			e, z.queue = z.queue[0], z.queue[1:]
			if seen[e.key] {
				e = nil
			}
		}
		z.mu.Unlock()
		if e == nil {
			select {
			case <-z.wake:
				continue
			case <-done:
				return
			}
		}
		seen[e.key] = true
		src, closer, err := e.open()
		if err != nil {
			continue
		}
		a := analyze(src)
		closer()
		if fi, err := os.Stat(e.path); err == nil {
			a.Size, a.Mod = fi.Size(), fi.ModTime()
		}
		select {
		case z.out <- analyzed{e.key, a}:
		case <-done:
			return
		}
	}
}

// analysisFile returns where the analyses are kept: in the user's
// cache, as they can be done again.
func analysisFile() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "gunim-music", "analysis.json")
}

// loadAnalyses reads the analyses kept in file.
func loadAnalyses(file string) map[string]analysis {
	out := map[string]analysis{}
	if file == "" {
		return out
	}
	if b, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// writeAnalyses keeps as in file, whole or not at all.
func writeAnalyses(file string, as map[string]analysis) error {
	if file == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(as)
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// fresh says whether a, kept for the file at path, still holds: the
// file is the same size and unchanged since.
func (a analysis) fresh(path string) bool {
	if path == "" {
		return a.Peaks != nil
	}
	fi, err := os.Stat(path)
	return err == nil && fi.Size() == a.Size && fi.ModTime().Equal(a.Mod)
}

// dB is a gain as a ratio.
func dB(ratio float64) float64 { return 20 * math.Log10(ratio) }
