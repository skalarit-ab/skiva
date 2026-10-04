package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pollEvery is how often a followed folder is looked over.
const pollEvery = 2 * time.Second

// settle is how long a new file must lie unchanged before it is read,
// so a file still being copied in is read once it is whole.
const settle = time.Second

// A poller follows the tracks in a folder and below. Each poll looks at
// the folders it knows and reads again only those that changed, so a
// library of thousands of tracks costs a stat for each folder a poll,
// with the files of a folder that changed. The
// same code runs on every system, and on network drives, where the
// system's own change notices often stay quiet.
type poller struct {
	root string
	// dirs holds each folder's time of change as last read.
	dirs map[string]time.Time
	// files holds the tracks reported, and waiting the new ones still
	// changing, with what they were at the last poll.
	files, waiting map[string]fileStat
	// ready holds tracks found whole, to read at the end of the poll.
	ready []string
	last  time.Time
}

// fileStat is what tells a file changed.
type fileStat struct {
	size int64
	mod  time.Time
}

func statOf(fi fs.FileInfo) fileStat { return fileStat{fi.Size(), fi.ModTime()} }

func newPoller(root string) *poller {
	return &poller{root: root, dirs: map[string]time.Time{}, files: map[string]fileStat{}, waiting: map[string]fileStat{}}
}

// isTrack says whether path is a file the library takes.
func isTrack(path string) bool { return audioExts[strings.ToLower(filepath.Ext(path))] }

// poll looks the folder over at now, and returns the tracks to read,
// new or changed, and the tracks gone.
func (p *poller) poll(now time.Time) (read, gone []string) {
	first := p.last.IsZero()
	since := p.last
	p.last = now
	if first {
		p.walk(p.root, now, true)
	} else {
		for dir, mod := range p.dirs {
			fi, err := os.Stat(dir)
			if err != nil || !fi.IsDir() {
				gone = append(gone, p.drop(dir)...)
				continue
			}
			// A folder changed just before the last poll may have
			// changed again within its clock's step.
			if fi.ModTime().Equal(mod) && fi.ModTime().Before(since.Add(-2*time.Second)) {
				continue
			}
			p.dirs[dir] = fi.ModTime()
			gone = append(gone, p.reread(dir, now)...)
		}
	}
	read, p.ready = p.ready, nil
	// A new file is read once it has stopped changing.
	for path, st := range p.waiting {
		fi, err := os.Stat(path)
		if err != nil {
			delete(p.waiting, path)
			continue
		}
		if cur := statOf(fi); cur != st {
			p.waiting[path] = cur
			continue
		}
		if p.last.Sub(st.mod) < settle {
			continue
		}
		delete(p.waiting, path)
		p.files[path] = st
		read = append(read, path)
	}
	return read, gone
}

// walk learns dir and everything below it. Files found as the poller
// starts are whole, unless they changed a moment ago.
func (p *poller) walk(dir string, now time.Time, first bool) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // a folder it cannot read is skipped
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != p.root {
				return filepath.SkipDir
			}
			if fi, infoErr := d.Info(); infoErr == nil {
				p.dirs[path] = fi.ModTime()
			}
			return nil
		}
		if !isTrack(path) || len(p.files)+len(p.waiting) >= maxTracks {
			return nil
		}
		if _, ok := p.files[path]; ok {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // a file it cannot stat is skipped
		}
		if st := statOf(fi); first && now.Sub(st.mod) >= settle {
			p.files[path] = st
			p.ready = append(p.ready, path)
		} else {
			p.waiting[path] = st
		}
		return nil
	})
}

// reread reads dir again, learning the folders and files new in it,
// and returns the tracks gone from it.
func (p *poller) reread(dir string, now time.Time) (gone []string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	here := map[string]bool{}
	for _, d := range ents {
		path := filepath.Join(dir, d.Name())
		here[path] = true
		if d.IsDir() {
			if _, ok := p.dirs[path]; !ok && !strings.HasPrefix(d.Name(), ".") {
				p.walk(path, now, false)
			}
			continue
		}
		if !isTrack(path) {
			continue
		}
		fi, err := d.Info()
		if err != nil {
			continue
		}
		// A track known that changed, as its tags were edited, is read
		// again once it has settled.
		if st, known := p.files[path]; known {
			if statOf(fi) != st {
				delete(p.files, path)
				p.waiting[path] = statOf(fi)
			}
			continue
		}
		if _, waits := p.waiting[path]; !waits && len(p.files)+len(p.waiting) < maxTracks {
			p.waiting[path] = statOf(fi)
		}
	}
	for path := range p.files {
		if filepath.Dir(path) == dir && !here[path] {
			delete(p.files, path)
			gone = append(gone, path)
		}
	}
	for path := range p.dirs {
		if filepath.Dir(path) == dir && !here[path] {
			gone = append(gone, p.drop(path)...)
		}
	}
	return gone
}

// drop forgets dir and everything below it, and returns the tracks
// that were there.
func (p *poller) drop(dir string) (gone []string) {
	below := func(path string) bool {
		return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
	}
	for d := range p.dirs {
		if below(d) {
			delete(p.dirs, d)
		}
	}
	for path := range p.files {
		if below(path) {
			delete(p.files, path)
			gone = append(gone, path)
		}
	}
	for path := range p.waiting {
		if below(path) {
			delete(p.waiting, path)
		}
	}
	return gone
}

// A change is what a followed folder tells the library: a track read,
// a track gone, or that the folder has been read through once.
type change struct {
	root  string
	e     *entry
	gone  string
	ready bool
}

// follow keeps the library in step with the folder root until ctx
// ends: it reads the tracks there, then looks the folder over every
// pollEvery for tracks added, changed or taken away.
func follow(ctx context.Context, root string, out chan<- change) {
	p := newPoller(root)
	send := func(c change) bool {
		select {
		case out <- c:
			return true
		case <-ctx.Done():
			return false
		}
	}
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	first := true
	for {
		read, gone := p.poll(time.Now())
		for _, path := range gone {
			if !send(change{root: root, gone: path}) {
				return
			}
		}
		for _, path := range read {
			if ctx.Err() != nil {
				return
			}
			if e := readEntry(path); e != nil && !send(change{root: root, e: e}) {
				return
			}
		}
		if first {
			first = false
			if !send(change{root: root, ready: true}) {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
