package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // covers in tags are JPEG or PNG
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dhowden/tag"

	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/paint"
)

// coverSize is the most pixels a cover keeps on a side.
const coverSize = 512

// peakCount is how many bars the seek bar draws a track as.
const peakCount = 160

// An entry is a track as the application knows it: what the window
// shows of it, and how to play it.
type entry struct {
	Track
	path string
	song *song
	// key names the track for good, across runs: its file, or the demo
	// song it is.
	key string
	// roots holds the folders followed it lies in.
	roots map[string]bool
	// order sorts the library: by artist, album, disc and track.
	order string
}

// open returns a source playing the entry from its start, and what to
// close when done with it.
func (e *entry) open() (audio.Seeker, func(), error) {
	if e.song != nil {
		return e.song.source(), func() {}, nil
	}
	f, err := os.Open(e.path)
	if err != nil {
		return nil, nil, err
	}
	src, err := audio.Decode(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return src, func() { _ = f.Close() }, nil
}

// demoEntries returns the demo songs, as an album.
func demoEntries() []*entry {
	out := make([]*entry, 0, len(demoSongs))
	for i, s := range demoSongs {
		cover := makeCover(coverSize, palettes[i], i)
		accent, glow := accents(cover)
		e := &entry{
			Track: Track{
				Title: s.title, Artist: s.artist, Album: "Made in Code",
				Length: time.Duration(s.length() * float64(time.Second)),
				Cover:  paint.NewImage(cover), Accent: accent, Glow: glow,
			},
			song:  s,
			key:   fmt.Sprintf("demo:%d", i+1),
			order: "￿" + string(rune('a'+i)),
		}
		e.Peaks = s.peaks()
		out = append(out, e)
	}
	return out
}

// audioExts are the files the library takes.
var audioExts = map[string]bool{".mp3": true, ".flac": true, ".ogg": true, ".oga": true, ".wav": true}

// maxTracks is as many files as the library follows in a folder.
const maxTracks = 5000

// readEntry reads a file's tags, cover and length, or returns nil for
// a file it cannot play.
func readEntry(path string) *entry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	e := &entry{path: path, key: path}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	e.Title, e.Album = name, filepath.Base(filepath.Dir(path))
	var art image.Image
	disc, track := 0, 0
	if m, tagErr := tag.ReadFrom(f); tagErr == nil {
		if t := strings.TrimSpace(m.Title()); t != "" {
			e.Title = t
		}
		e.Artist = strings.TrimSpace(m.Artist())
		if a := strings.TrimSpace(m.Album()); a != "" {
			e.Album = a
		}
		track, _ = m.Track()
		disc, _ = m.Disc()
		if p := m.Picture(); p != nil {
			art, _, _ = image.Decode(bytes.NewReader(p.Data))
		}
	}
	if _, err = f.Seek(0, 0); err != nil {
		return nil
	}
	src, err := audio.Decode(f)
	if err != nil {
		return nil
	}
	if l := src.Len(); l > 0 {
		e.Length = audio.Duration(l)
	}
	if art == nil {
		art = coverBeside(path)
	}
	if art == nil {
		art = makeCover(coverSize/2, paletteFor(e.Album+e.Artist), len(e.Album))
	}
	e.Cover = paint.NewImageFit(art, coverSize, coverSize)
	e.Accent, e.Glow = accents(art)
	e.order = strings.ToLower(e.Artist) + "\x00" + strings.ToLower(e.Album) + "\x00" +
		string(rune('0'+min(disc, 9))) + string(rune(0x100+min(track, 999))) + strings.ToLower(e.Title)
	return e
}

// coverBeside returns the picture a folder keeps for its album, as
// cover.jpg or folder.png, or nil.
func coverBeside(path string) image.Image {
	dir := filepath.Dir(path)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, d := range ents {
		n := strings.ToLower(d.Name())
		base := strings.TrimSuffix(n, filepath.Ext(n))
		if ext := filepath.Ext(n); ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
			continue
		}
		if base != "cover" && base != "folder" && base != "front" && base != "album" {
			continue
		}
		f, err := os.Open(filepath.Join(dir, d.Name()))
		if err != nil {
			continue
		}
		img, _, err := image.Decode(f)
		_ = f.Close()
		if err == nil {
			return img
		}
	}
	return nil
}

// sortEntries sorts a library by artist, album and track, the demo
// album last.
func sortEntries(es []*entry) {
	sort.SliceStable(es, func(i, j int) bool { return es[i].order < es[j].order })
}

// peaksOf reads src to its end and returns how loud each of peakCount
// stretches of it is, as [shape] scales it, for the seek bar to draw.
func peaksOf(src audio.Seeker) []float32 {
	total := src.Len()
	if total <= 0 {
		return nil
	}
	power := make([]float64, peakCount)
	counts := make([]int, peakCount)
	per := max(total/peakCount, 1)
	buf := make([]float32, 2*4096)
	var at int64
	for {
		n, err := src.Read(buf)
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
	for i := range power {
		power[i] /= float64(max(counts[i], 1))
	}
	return shape(power)
}

// shape turns each stretch's power, its mean square, into a height
// from 0 to 1. The ear hears loudness in decibels, and a track mastered
// loud sits near its top almost throughout, so the heights run over the
// track's own range: from its quieter stretches, a little up from the
// bottom, to its loudest at the top, so its verses and choruses tell
// apart. The range counts only stretches with sound in them, and spans
// 6 to 24 dB; quieter stretches and silence fall away under it.
func shape(power []float64) []float32 {
	db := make([]float64, len(power))
	for i, p := range power {
		db[i] = 10 * math.Log10(max(p, 1e-10))
	}
	sorted := slices.Clone(db)
	slices.Sort(sorted)
	top := sorted[len(sorted)-1]
	sounding := sorted[sort.SearchFloat64s(sorted, top-40):]
	low := sounding[len(sounding)/10]
	low = max(top-24, min(low, top-6))
	const base = 0.15
	out := make([]float32, len(power))
	for i, d := range db {
		var v float64
		if d >= low {
			v = base + (1-base)*(d-low)/(top-low)
		} else {
			v = base * (1 + (d-low)/12)
		}
		out[i] = float32(max(0, min(1, v)))
	}
	return out
}
