package main

import (
	"context"
	"image/color"
	"log"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/paint"
)

// The vocabulary the two halves share.
type (
	// Player is what the window shows.
	Player struct {
		// Tracks is the library, in order.
		Tracks []Track
		// Current is the ID of the track playing or paused, zero for
		// none; Starts counts tracks started, so the window can tell a
		// track begun again from one carrying on.
		Current int
		Starts  int
		Playing bool
		Shuffle bool
		Repeat  Repeat
		Volume  float32
		// Scanning says the library is still reading the folder.
		Scanning bool
	}
	// Track is one track of the library.
	Track struct {
		ID                   int
		Title, Artist, Album string
		Length               time.Duration
		Cover                *paint.Image
		// Accent is the colour the player lights up in while the track
		// plays, and Glow a second, for the background.
		Accent, Glow color.NRGBA
		// Peaks is how loud the track is along its length, from 0 to 1,
		// for the seek bar; nil until it is known.
		Peaks []float32
	}
	// Repeat says what happens as the last track ends.
	Repeat int

	// PlayTrack plays a track from its start.
	PlayTrack struct{ ID int }
	// TogglePlay pauses or resumes.
	TogglePlay struct{}
	// Skip goes to the next track, or the one before with Back. Back
	// a few seconds into a track starts it again instead.
	Skip struct{ Back bool }
	// SeekTo moves the track playing.
	SeekTo struct{ At time.Duration }
	// SetVolume sets the volume, 0 to 1.
	SetVolume struct{ Volume float32 }
	// ToggleShuffle turns shuffle on or off.
	ToggleShuffle struct{}
	// CycleRepeat steps through repeat's settings.
	CycleRepeat struct{}
)

// The settings of Repeat.
const (
	RepeatOff Repeat = iota
	RepeatAll
	RepeatOne
)

// playerTopic is what the player view watches.
const playerTopic = "player"

// keeper keeps the application running in the background while on, as
// gunim.App.KeepRunning does.
type keeper func(on bool, title, text string) error

// app is the application half.
type app struct {
	Player
	d       *deck
	entries map[int]*entry
	order   []*entry
	ids     int
	// voice is the track playing's voice, to hear it end.
	voice *audio.Voice
	// history is the tracks played, for Back in shuffle.
	history []int
	rng     *rand.Rand
	// peaksDone carries peaks read in the background.
	peaksDone chan peaksRead
}

type peaksRead struct {
	id    int
	peaks []float32
}

// serve keeps the player's state and hears what the window sends.
func serve(ctx context.Context, c gunim.Client, d *deck, dir string, play startAt, keep keeper) error {
	a := &app{
		d: d, entries: map[int]*entry{},
		rng:       rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7)),
		peaksDone: make(chan peaksRead, 4),
	}
	a.Volume = d.volume
	for _, e := range demoEntries() {
		a.add(e)
	}
	found := make(chan *entry, 64)
	scanned := make(chan struct{})
	if dir != "" {
		a.Scanning = true
		go func() {
			scan(dir, func(e *entry) {
				select {
				case found <- e:
				case <-ctx.Done():
				}
			})
			close(scanned)
		}()
	}
	if err := c.Mount(gunim.Root, "player", "player", a.Player, playerTopic); err != nil {
		return err
	}
	_ = c.Focus("player")
	// -play starts once the library is read, so its track counts the
	// folder's too.
	startPlay := func() {
		if play.on && len(a.order) > 0 {
			a.start(a.order[min(max(play.track, 1), len(a.order))-1].ID)
			a.d.seek(play.at)
		}
	}
	if dir == "" {
		startPlay()
	}
	// While a track plays, a phone keeps the player running in the
	// background, saying what plays.
	var kept string
	publish := func() {
		_ = c.Publish(playerTopic, a.Player)
		now := ""
		if e := a.entries[a.Current]; a.Playing && e != nil {
			now = e.Title + "\x00" + e.Artist
		}
		if now == kept || keep == nil {
			return
		}
		kept = now
		if now == "" {
			_ = keep(false, "", "")
			return
		}
		e := a.entries[a.Current]
		_ = keep(true, e.Title, e.Artist)
	}
	publish()
	// Tracks found come in batches, so a big folder does not publish
	// the library once for each.
	var batch <-chan time.Time
	for {
		var ended <-chan struct{}
		if a.voice != nil {
			ended = a.voice.Done()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-found:
			a.add(e)
			if batch == nil {
				batch = time.After(150 * time.Millisecond)
			}
			continue
		case <-batch:
			batch = nil
		case <-scanned:
			scanned = nil
			a.Scanning = false
			for len(found) > 0 {
				a.add(<-found)
			}
			startPlay()
		case p := <-a.peaksDone:
			if e := a.entries[p.id]; e != nil {
				e.Peaks = p.peaks
				a.refresh()
			}
		case <-ended:
			a.ended()
		case ev, ok := <-c.Intents():
			if !ok {
				return c.Err()
			}
			a.handle(ev.Intent)
		}
		publish()
	}
}

// add puts a track in the library.
func (a *app) add(e *entry) {
	a.ids++
	e.ID = a.ids
	a.entries[e.ID] = e
	a.order = append(a.order, e)
	sortEntries(a.order)
	a.refresh()
}

// refresh copies the library into the state the window sees.
func (a *app) refresh() {
	a.Tracks = a.Tracks[:0:0]
	for _, e := range a.order {
		a.Tracks = append(a.Tracks, e.Track)
	}
}

func (a *app) handle(in gunim.Intent) {
	switch in := in.(type) {
	case PlayTrack:
		a.start(in.ID)
	case TogglePlay:
		switch {
		case a.Current == 0 && len(a.order) > 0:
			a.start(a.order[0].ID)
		case a.Current != 0:
			a.Playing = !a.Playing
			a.d.setPaused(!a.Playing)
		}
	case Skip:
		a.skip(in.Back)
	case SeekTo:
		a.d.seek(in.At)
	case SetVolume:
		a.Volume = max(0, min(1, in.Volume))
		a.d.setVolume(a.Volume)
	case ToggleShuffle:
		a.Shuffle = !a.Shuffle
	case CycleRepeat:
		a.Repeat = (a.Repeat + 1) % 3
	}
}

// start plays track id from its start.
func (a *app) start(id int) {
	e := a.entries[id]
	if e == nil {
		return
	}
	src, closer, err := e.open()
	if err != nil {
		log.Printf("music: %s: %v", e.Title, err)
		return
	}
	a.voice = a.d.play(src, closer, false)
	if a.Current != id {
		a.history = append(a.history, id)
	}
	a.Current, a.Playing = id, true
	a.Starts++
	if e.Peaks == nil && e.path != "" {
		go func(id int, path string) {
			if p := filePeaks(path); p != nil {
				a.peaksDone <- peaksRead{id, p}
			}
		}(id, e.path)
	}
}

// filePeaks reads a file's peaks, or returns nil.
func filePeaks(path string) []float32 {
	e := &entry{path: path}
	src, closer, err := e.open()
	if err != nil {
		return nil
	}
	defer closer()
	return peaksOf(src)
}

// ended moves on as a track ends.
func (a *app) ended() {
	a.voice = nil
	switch {
	case a.Repeat == RepeatOne:
		a.start(a.Current)
	case a.next() != 0:
		a.start(a.next())
	default:
		a.Playing = false
		a.Current = 0
		a.d.stop()
	}
}

// next returns the track after the current one, as shuffle and repeat
// say, or zero at the end.
func (a *app) next() int {
	if len(a.order) == 0 {
		return 0
	}
	if a.Shuffle {
		if len(a.order) == 1 {
			return a.order[0].ID
		}
		for {
			if e := a.order[a.rng.IntN(len(a.order))]; e.ID != a.Current {
				return e.ID
			}
		}
	}
	i := slices.IndexFunc(a.order, func(e *entry) bool { return e.ID == a.Current })
	switch {
	case i+1 < len(a.order):
		return a.order[i+1].ID
	case a.Repeat == RepeatAll:
		return a.order[0].ID
	}
	return 0
}

// skip goes to the next track, or back.
func (a *app) skip(back bool) {
	if !back {
		if n := a.next(); n != 0 {
			a.start(n)
		} else if len(a.order) > 0 {
			a.start(a.order[0].ID)
		}
		return
	}
	// A few seconds in, Back starts the track again.
	if at, _ := a.d.position(); at > 3*time.Second || a.Current == 0 {
		if a.Current != 0 {
			a.d.seek(0)
		}
		return
	}
	if a.Shuffle && len(a.history) > 1 {
		a.history = a.history[:len(a.history)-1]
		prev := a.history[len(a.history)-1]
		a.history = a.history[:len(a.history)-1]
		a.start(prev)
		return
	}
	i := slices.IndexFunc(a.order, func(e *entry) bool { return e.ID == a.Current })
	if i > 0 {
		a.start(a.order[i-1].ID)
	} else {
		a.d.seek(0)
	}
}
