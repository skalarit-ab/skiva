package main

import (
	"bytes"
	"image"
	_ "image/jpeg" // covers in tags are JPEG or PNG
	_ "image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
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
			order: "￿" + string(rune('a'+i)),
		}
		e.Peaks = s.peaks()
		out = append(out, e)
	}
	return out
}

// audioExts are the files the library takes.
var audioExts = map[string]bool{".mp3": true, ".flac": true, ".ogg": true, ".oga": true, ".wav": true}

// maxTracks is as many files as the library reads from a folder.
const maxTracks = 5000

// scan reads the tracks in dir and below, sending each as it is read.
func scan(dir string, found func(*entry)) {
	n := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // a folder it cannot read is skipped
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !audioExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if e := readEntry(path); e != nil {
			found(e)
			n++
		}
		if n >= maxTracks {
			return filepath.SkipAll
		}
		return nil
	})
}

// readEntry reads a file's tags, cover and length, or returns nil for
// a file it cannot play.
func readEntry(path string) *entry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	e := &entry{path: path}
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
// stretches of it is, from 0 to 1, for the seek bar to draw.
func peaksOf(src audio.Seeker) []float32 {
	total := src.Len()
	if total <= 0 {
		return nil
	}
	peaks := make([]float32, peakCount)
	per := total / peakCount
	buf := make([]float32, 2*4096)
	var at int64
	for {
		n, err := src.Read(buf)
		for i := range n {
			b := min(int((at+int64(i))/max(per, 1)), peakCount-1)
			v := float32(math.Abs(float64(buf[2*i])) + math.Abs(float64(buf[2*i+1])))
			peaks[b] = max(peaks[b], v/2)
		}
		at += int64(n)
		if err != nil || n == 0 {
			break
		}
	}
	normalize(peaks)
	return peaks
}

// normalize scales peaks to the loudest, so a quiet recording still
// shows its shape.
func normalize(peaks []float32) {
	top := float32(0)
	for _, p := range peaks {
		top = max(top, p)
	}
	if top > 0 {
		for i := range peaks {
			peaks[i] /= top
		}
	}
}
