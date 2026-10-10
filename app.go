package main

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"io/fs"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/install"
	"github.com/marrasen/gunim/paint"

	"github.com/skalarit-ab/skiva/internal/single"
)

// The vocabulary the two halves share.
type (
	// Player is what the window shows.
	Player struct {
		// Tracks holds every track known, and Library the library's
		// tracks by their IDs, in order.
		Tracks  []Track
		Library []int
		// Playlists are the user's playlists, and Folders the folders
		// the library follows, each with its tracks.
		Playlists []Playlist
		Folders   []Folder
		// Queue is the tracks to play next, in order, before the list
		// playing goes on.
		Queue []int
		// Current is the ID of the track playing or paused, zero for
		// none; Starts counts tracks started, so the window can tell a
		// track begun again from one carrying on. From is the list it
		// was played from, which Next and Back go along.
		Current int
		Starts  int
		From    ListID
		Playing bool
		Shuffle bool
		Repeat  Repeat
		Volume  float32
		// Scanning says the library is still reading a folder through
		// for the first time.
		Scanning bool
		// EQ is the equalizer's settings.
		EQ EQ
		// GainMode is how loudness gain evens tracks out; Gain is the
		// gain the track playing plays at, in decibels, GainBy what it
		// follows, and AlbumLUFS its album's loudness, where that is
		// what it follows.
		GainMode  GainMode
		Gain      float32
		GainBy    GainSource
		AlbumLUFS float32
		// Headroom is how far the track plays lowered so the equalizer's
		// boosts leave its peaks unclipped, in decibels.
		Headroom float32
		// Resumed counts the times the player took up the track and the
		// list of its last run, so the window opens that list.
		Resumed int
		// Settings is what the settings card shows, and Update tells of
		// a newer release.
		Settings Settings
		Update   UpdateNotice
	}
	// Settings is what the settings card shows and sets.
	Settings struct {
		// Version is this build's version: "dev" for one from a working
		// tree.
		Version string
		// Updates says where updates come from.
		Updates UpdatesFrom
		// Mode is how the installed Skiva takes newer releases, as
		// install.UpdateMode names it: "install" puts one in place for
		// the next start, "notify" asks first, and "off" does nothing.
		// Beta says beta releases come too.
		Mode string
		Beta bool
	}
	// UpdatesFrom says where a Skiva's updates come from.
	UpdatesFrom int
	// UpdateNotice tells the window of a newer release, to show: one to
	// fetch, one Ready, in place for the next start, or, with From, the
	// release this start is the first of, updated from that version.
	// Seq counts them, so the window shows each once.
	UpdateNotice struct {
		Seq     int
		Release install.Release
		Ready   bool
		From    string
	}
	// EQ is the equalizer: its bands, and whether it is bypassed.
	EQ struct {
		Bands  []audio.Band
		Bypass bool
		// Seq counts the window's changes, so the window can tell its
		// own settings coming back from older ones.
		Seq int
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
		// File is the track's file, and Size its size in bytes; empty
		// for a song made in code.
		File string
		Size int64
		// Added says the file was added to the library on its own, not
		// found in a folder it follows, so it can be taken out again.
		Added bool
		// Format is what the track is stored as; Measured says its
		// loudness is known: LUFS, as BS.1770 measures it, and Peak, its
		// loudest sample, 1 at full scale.
		Format   audio.Format
		Measured bool
		LUFS     float32
		Peak     float32
	}
	// GainMode says how loudness gain evens tracks out: not at all,
	// each track to the same loudness, or each album, so an album's
	// quiet songs stay quiet. An album plays at its album's gain in
	// order, and at each track's when shuffled or from a playlist or
	// Up next, where tracks of many albums meet.
	GainMode int
	// GainSource says what the gain playing follows.
	GainSource int
	// Playlist is a list of tracks the user made. A track may be on it
	// more than once.
	Playlist struct {
		ID, Name string
		Tracks   []int
	}
	// Folder is a folder the library follows: tracks added to it join
	// the library, and tracks taken from it leave.
	Folder struct {
		Path   string
		Tracks []int
		// Reading says the folder is being read through for the first
		// time.
		Reading bool
	}
	// ListID names a list of tracks to play from: the library, a
	// playlist or a folder.
	ListID string
	// Repeat says what happens as the last track ends.
	Repeat int

	// PlayTrack plays a track from its start, going on along the list
	// it was picked from.
	PlayTrack struct {
		ID   int
		From ListID
	}
	// TogglePlay pauses or resumes.
	TogglePlay struct{}
	// Skip goes to the next track, or the one before with Back. Back
	// a few seconds into a track starts it again instead.
	Skip struct{ Back bool }
	// SeekTo moves the track playing.
	SeekTo struct{ At time.Duration }
	// SetVolume sets the volume, 0 to 1, and while loudness gain is on
	// up to maxBoost, past full: 2 lifts every track 6 dB over the
	// level the gain brings it to.
	SetVolume struct{ Volume float32 }
	// ToggleShuffle turns shuffle on or off.
	ToggleShuffle struct{}
	// CycleRepeat steps through repeat's settings.
	CycleRepeat struct{}

	// AddFolder asks, with the system's dialog, for a folder for the
	// library to follow.
	AddFolder struct{}
	// ForgetFolder stops following a folder. Its tracks leave the
	// library, all but those on a playlist.
	ForgetFolder struct{ Path string }
	// AddFiles asks, with the system's dialog, for files to add to a
	// playlist, or to the library where Playlist is empty.
	AddFiles struct{ Playlist string }
	// ShowPrivacy opens Skiva's privacy policy in the browser.
	ShowPrivacy struct{}
	// NewPlaylist makes a playlist, with tracks or none, and files, as
	// dropped on it. The window picks its ID, so it can open the
	// playlist at once.
	NewPlaylist struct {
		ID, Name string
		Tracks   []int
		Paths    []string
	}
	// RenamePlaylist renames a playlist.
	RenamePlaylist struct{ ID, Name string }
	// DeletePlaylist deletes a playlist.
	DeletePlaylist struct{ ID string }
	// AddToPlaylist adds tracks to the end of a playlist.
	AddToPlaylist struct {
		ID     string
		Tracks []int
	}
	// RemoveFromPlaylist takes the track at a place on a playlist off
	// it.
	RemoveFromPlaylist struct {
		ID string
		At int
	}
	// RemoveFromLibrary takes a file added on its own out of the
	// library. It stays on the playlists and Up next that hold it.
	RemoveFromLibrary struct{ ID int }
	// MoveInPlaylist moves the track at From on a playlist to To.
	MoveInPlaylist struct {
		ID       string
		From, To int
	}

	// AddPaths adds files from another program, as dropped on the
	// window, to a list: to a playlist or Up next at a place, At, or
	// at its end where At is -1, or to the library. A folder's tracks
	// join a playlist or Up next, and a folder dropped on the library
	// is followed. Play plays the files at once, the first now and the
	// rest first on Up next, as files opened with Skiva do.
	AddPaths struct {
		Paths []string
		To    ListID
		At    int
		Play  bool
	}
	// Enqueue puts tracks on Up next: at its end, or first with Next.
	// Play plays them at once: the first now, and the rest first on Up
	// next.
	Enqueue struct {
		Tracks     []int
		Next, Play bool
	}
	// Unqueue takes the track at a place off Up next.
	Unqueue struct{ At int }
	// MoveInQueue moves the track at From on Up next to To.
	MoveInQueue struct{ From, To int }
	// ClearQueue empties Up next.
	ClearQueue struct{}
	// CloseAsked asks to close the window: the music fades out as it
	// closes.
	CloseAsked struct{}
	// SetGainMode sets how loudness gain evens tracks out.
	SetGainMode struct{ Mode GainMode }
	// SetEQ sets the equalizer, as it is changed: often, while a band
	// is dragged.
	SetEQ struct{ EQ EQ }

	// SetUpdateMode sets how the installed Skiva takes newer releases:
	// Settings.Mode names the modes.
	SetUpdateMode struct{ Mode string }
	// SetBeta has updates take beta releases too, or no longer.
	SetBeta struct{ On bool }
	// ShowAbout opens the window about Skiva: its version, what each
	// release changed, and Check for Updates.
	ShowAbout struct{}
	// ShowUpdate opens the window of a newer release: what it changes,
	// and Update Now, or Restart Now for one Ready.
	ShowUpdate struct {
		Release install.Release
		Ready   bool
	}
	// ShowWhatsNew opens a window of what changed since version From.
	ShowWhatsNew struct{ From string }
)

// Where updates come from.
const (
	// UpdatesNone: a copy that is not installed, as one built from a
	// working tree, takes none.
	UpdatesNone UpdatesFrom = iota
	// UpdatesHere: the installed Skiva on a desktop updates itself.
	UpdatesHere
	// UpdatesFromStore: on a phone, the app store updates Skiva.
	UpdatesFromStore
)

// The settings of Repeat.
const (
	RepeatOff Repeat = iota
	RepeatAll
	RepeatOne
)

// boostDB is how far past full the volume goes while loudness gain is
// on, in decibels, and maxBoost that as a volume.
const boostDB = 12

var maxBoost = float32(math.Pow(10, boostDB/20.0))

// maxVolume returns the loudest the volume goes in mode m.
func maxVolume(m GainMode) float32 {
	if m == GainOff {
		return 1
	}
	return maxBoost
}

// The settings of GainMode, and what a gain may follow.
const (
	GainOff GainMode = iota
	GainTrack
	GainAlbum
)

const (
	// GainNone: no gain, as gain is off, or the track is silent.
	GainNone GainSource = iota
	GainByTrack
	GainByAlbum
	// GainMeasuring: the track's loudness is still being measured.
	GainMeasuring
)

// AllTracks is the whole library, as a list to play from, and
// QueueList the tracks to play next.
const (
	AllTracks ListID = ""
	QueueList ListID = "q"
)

// PlaylistList and FolderList name a playlist's and a folder's lists.
func PlaylistList(id string) ListID { return ListID("p:" + id) }
func FolderList(path string) ListID { return ListID("f:" + path) }

// playerTopic is what the player view watches.
const playerTopic = "player"

// nowShower shows what plays in the system's media controls, as
// gunim.App.SetNowPlaying does.
type nowShower func(np *gunim.NowPlaying) error

// nowKey is what, changing, the media controls are told of.
type nowKey struct {
	id, starts, seeks int
	playing           bool
}

// app is the application half.
type app struct {
	Player
	d       *deck
	entries map[int]*entry
	byKey   map[string]*entry
	order   []*entry
	ids     int
	// voice is the track playing's voice, to hear it end.
	voice *audio.Voice
	// history is the tracks played, for Back in shuffle.
	history []int
	// queue holds Up next, by the tracks' keys, so files dropped there
	// find their place before they are read. listAt is the track of
	// the list playing that played last, which the list goes on from
	// once Up next is done.
	queue  []string
	listAt int
	// spread carries files with the folders among them opened, for
	// files dropped on a list.
	spread chan spread
	rng    *rand.Rand
	// seeks counts the seeks, for the media controls to hear of each.
	seeks int
	// resume is the track of the last run, by its key, and the list
	// it played from, to take up again once the library has it.
	resume     string
	resumeFrom ListID
	// upNext is the track queued in the deck to follow the one playing
	// without a gap, at gain upGain; upFromQueue says it is Up next's
	// first. shuffleNext is the track shuffle picked to follow
	// shuffleFor, kept so Next and the queue agree.
	upNext      *entry
	upFromQueue bool
	upGain      float64
	// turning says the voice has turned to upNext, which the speakers
	// have yet to reach.
	turning     bool
	shuffleFor  int
	shuffleNext int
	// eqBoost is the most the equalizer lifts any frequency, in
	// decibels, zero where it lifts none.
	eqBoost float64
	// z reads tracks through in the background, for their loudness and
	// their peaks, which analyses keeps, in afile; analysesDirty says
	// they changed since they were written.
	z             *analyzer
	analyses      map[string]analysis
	afile         string
	analysesDirty bool

	// kept is the library as kept between runs, in file, and dirty
	// says it changed since it was written.
	kept  saved
	file  string
	dirty bool
	// ctx lasts as long as serve does; changes carries what the
	// folders followed and the files read tell the library, following
	// holds how to stop following each folder, and reading the folders
	// read through for the first time.
	ctx       context.Context
	changes   chan change
	following map[string]context.CancelFunc
	reading   map[string]bool
	// choose shows the system's dialog for choosing files, and chosen
	// carries what was chosen.
	choose func(driver.ChooseOptions) ([]string, error)
	// openLink opens a web page in the system's browser.
	openLink func(url string) error
	chosen   chan chosen
	// home is the user's music folder, askMusic asks the user for
	// leave to read their music, and granted carries their answer.
	home     string
	askMusic func() bool
	granted  chan bool
	// placement says where the window is, to open it there next run;
	// nil where there is no window to ask.
	placement func() (driver.Placement, bool)
	// windows opens gunim's windows about updates; nil opens none.
	windows *updateWindows
	// openedAt is when files were last opened with Skiva; lastOpened
	// is the last of them, and playNow the one to play once it is read.
	// See open.
	openedAt   time.Time
	lastOpened string
	playNow    string
}

// spread is files dropped on a list, with the folders among them
// opened, for place to put on the list.
type spread struct {
	paths []string
	to    ListID
	at    int
	// opened says the files were opened with Skiva, to play at once,
	// and together that they follow those opened a moment before.
	opened, together bool
}

// chosen is what the system's dialog gave, for a playlist or for the
// library where playlist is empty.
type chosen struct {
	paths    []string
	playlist string
}

// setup is where the library is kept, and where it starts.
type setup struct {
	// file is where the library is kept; empty keeps nothing.
	file string
	// dir is a folder to follow, as -dir names, and home the folder
	// followed on the first run, the user's music folder.
	dir, home string
	// permitted reports whether the player may read the user's music,
	// and ask asks the user, as a phone's system does; nil for always.
	permitted, ask func() bool
	// placement says where the window is, kept as the window closes.
	placement func() (driver.Placement, bool)
	// files are files opened with Skiva, to play as it starts, and
	// handovers carries those a Skiva started later hands this one.
	files     []string
	handovers <-chan single.Handover
	// windows opens gunim's windows about updates; nil opens none.
	windows *updateWindows
}

// newApp returns the application half, with the library kept in file
// and the demo songs.
func newApp(ctx context.Context, d *deck, file string) *app {
	a := &app{
		d: d, entries: map[int]*entry{}, byKey: map[string]*entry{},
		rng:      rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7)),
		z:        newAnalyzer(),
		analyses: map[string]analysis{},
		ctx:      ctx, file: file,
		changes:   make(chan change, 64),
		following: map[string]context.CancelFunc{},
		reading:   map[string]bool{},
		chosen:    make(chan chosen, 1),
		granted:   make(chan bool, 1),
		spread:    make(chan spread, 4),
	}
	a.Volume = d.volume
	for _, e := range demoEntries() {
		a.add(e)
	}
	return a
}

// serve keeps the player's state and hears what the window sends.
func serve(ctx context.Context, c gunim.Client, d *deck, at setup, play startAt, show nowShower) error {
	a := newApp(ctx, d, at.file)
	if at.file != "" {
		a.afile = analysisFile()
		a.analyses = loadAnalyses(a.afile)
		for _, e := range a.order {
			a.learn(e)
		}
	}
	go a.z.run(ctx.Done())
	a.choose = func(o driver.ChooseOptions) ([]string, error) { return c.ChooseFiles(ctx, o) }
	a.openLink = c.OpenLink
	a.home, a.askMusic, a.placement, a.windows = at.home, at.ask, at.placement, at.windows
	kept, ok := loadSaved(at.file)
	a.kept = kept
	if !ok && at.home != "" {
		a.kept.Folders = append(a.kept.Folders, at.home)
		a.dirty = true
	}
	a.Shuffle, a.Repeat = kept.Shuffle, kept.Repeat
	if kept.GainMode != nil {
		a.GainMode = *kept.GainMode
	} else {
		a.GainMode = GainAlbum
	}
	// The volume, after the gain mode: with gain on it may pass full.
	if kept.Volume != nil {
		a.Volume = max(0, min(maxVolume(a.GainMode), *kept.Volume))
		d.setVolume(a.Volume)
	}
	if kept.EQ != nil {
		a.setEQ(*kept.EQ)
	}
	a.Settings = currentSettings(kept)
	if from := install.UpdatedFrom(); from != "" {
		// The first start of a release an update put in place by itself.
		a.Update = UpdateNotice{Seq: 1, From: from}
	}
	if at.dir != "" {
		a.follow(at.dir)
	}
	for _, f := range a.kept.Folders {
		a.startFollowing(f)
	}
	a.readMissing()
	a.refresh()
	if err := c.Mount(gunim.Root, "player", "player", a.Player, playerTopic); err != nil {
		return err
	}
	_ = c.Focus("player")
	// On a phone the player asks leave to read the user's music, as it
	// starts, until it has it.
	if at.permitted != nil && !at.permitted() {
		a.askLeave()
	}
	// -play starts once the folders are read, so its track counts
	// theirs too.
	startPlay := func() {
		if play.on && len(a.order) > 0 {
			a.start(a.order[min(max(play.track, 1), len(a.order))-1].ID)
			a.d.seek(play.at)
		}
		play.on = false
	}
	if !a.Scanning {
		startPlay()
	}
	// Files opened with Skiva play; without them, or -play, the player
	// takes up the track and the list of its last run, paused, as soon
	// as the library has the track.
	a.open(at.files)
	if !play.on && len(at.files) == 0 {
		a.resume, a.resumeFrom = a.kept.Last, a.kept.LastFrom
		a.takeUp()
	}
	// The system's media controls, as a phone's lock screen, show what
	// plays, as the track, its playing or a seek changes; the phone keeps
	// the player running while it plays in the background.
	var shown nowKey
	covers := map[int][]byte{}
	publish := func() {
		_ = c.Publish(playerTopic, a.Player)
		if show == nil {
			return
		}
		e := a.entries[a.Current]
		k := nowKey{id: a.Current, playing: a.Playing, starts: a.Starts, seeks: a.seeks}
		if k == shown {
			return
		}
		shown = k
		if e == nil {
			_ = show(nil)
			return
		}
		if _, ok := covers[e.ID]; !ok && e.Cover != nil {
			var b bytes.Buffer
			if e.Cover.EncodePNG(&b) == nil {
				covers[e.ID] = b.Bytes()
			}
		}
		at, length := a.d.position()
		if length <= 0 {
			length = e.Length
		}
		_ = show(&gunim.NowPlaying{
			Title: e.Title, Artist: e.Artist, Album: e.Album, Cover: covers[e.ID],
			Length: length, Position: at, Playing: a.Playing,
		})
	}
	publish()
	defer a.save()
	// Tracks found come in batches, so a big folder does not publish
	// the library once for each; the library is kept a moment after it
	// changes, once for a burst of changes.
	var batch, keep, turnLater <-chan time.Time
	var turnVoice *audio.Voice
	for {
		var ended, turned <-chan struct{}
		if a.voice != nil {
			ended, turned = a.voice.Done(), a.voice.Turned()
		}
		if (a.dirty || a.analysesDirty) && keep == nil {
			keep = time.After(500 * time.Millisecond)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch := <-a.changes:
			a.apply(ch)
			if ch.ready && !a.Scanning {
				startPlay()
			}
			if batch == nil {
				batch = time.After(150 * time.Millisecond)
			}
			continue
		case <-batch:
			batch = nil
			a.refresh()
			a.playOpened()
			a.takeUp()
			a.prepareNext()
		case <-keep:
			keep = nil
			a.save()
			continue
		case got := <-a.chosen:
			a.addPaths(got.paths, got.playlist)
		case ok := <-a.granted:
			a.onGranted(ok)
		case x := <-a.spread:
			a.place(x)
		case r := <-a.z.out:
			a.analyses[r.key] = r.a
			a.analysesDirty = true
			if e := a.byKey[r.key]; e != nil {
				e.analyzed(r.a)
				a.refresh()
				if cur := a.entries[a.Current]; cur != nil && albumKey(cur) == albumKey(e) {
					a.applyGain()
				}
				// The track to follow may play at a gain just learned.
				a.prepareNext()
			}
		case <-ended:
			a.ended()
		case <-turned:
			// The voice turned to the track queued, without a gap; the
			// player moves on to it as the speakers reach it.
			turnLater, turnVoice = time.After(a.d.lag()), a.voice
			a.turning = true
			continue
		case <-turnLater:
			turnLater = nil
			if a.voice == turnVoice {
				a.turned()
			}
			a.turning = false
		case h := <-at.handovers:
			// A Skiva started again, as for a file opened with it.
			files, quit := handedOver(h)
			h.Take(true)
			if quit {
				return a.close(ctx, c)
			}
			c.ToFront()
			a.open(files)
		case n := <-news:
			a.Update = UpdateNotice{Seq: a.Update.Seq + 1, Release: n.release, Ready: n.ready}
		case <-quitAsked:
			return a.close(ctx, c)
		case ev, ok := <-c.Intents():
			if !ok {
				return c.Err()
			}
			if _, ok := ev.Intent.(CloseAsked); ok {
				return a.close(ctx, c)
			}
			a.handle(ev.Intent)
			a.prepareNext()
		}
		publish()
	}
}

// close closes the window: it shrinks a little and fades as it leaves,
// and the music fades out with it.
func (a *app) close(ctx context.Context, c gunim.Client) error {
	a.keepPlacement()
	a.save()
	a.d.fadeOut(closeFade)
	c.Leave()
	return leaving(ctx, c)
}

// leaving waits for the window to close once it has begun to leave.
// The player acts on nothing more meanwhile: a paused track ends at once
// as it stops, and taking that for the track's end would play the next.
func leaving(ctx context.Context, c gunim.Client) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-c.Intents():
			if !ok {
				return c.Err()
			}
		}
	}
}

// keepPlacement notes where the window is and how big, to open it
// there next run.
func (a *app) keepPlacement() {
	if a.placement == nil {
		return
	}
	if p, ok := a.placement(); ok && (a.kept.Window == nil || *a.kept.Window != p) {
		a.kept.Window = &p
		a.dirty = true
	}
}

// save writes the library where it is kept, and the analyses, if they
// changed.
func (a *app) save() {
	if a.analysesDirty {
		a.analysesDirty = false
		if err := writeAnalyses(a.afile, a.analyses); err != nil {
			log.Printf("music: keeping the analyses: %v", err)
		}
	}
	if !a.dirty {
		return
	}
	a.dirty = false
	if err := a.kept.write(a.file); err != nil {
		log.Printf("music: keeping the library: %v", err)
	}
}

// add puts a track in the library, or brings one known up to date as
// its file changed.
func (a *app) add(e *entry) {
	a.apply(change{e: e})
	a.refresh()
}

// apply takes in what a folder followed or a file read told.
func (a *app) apply(ch change) {
	switch {
	case ch.e != nil:
		if old := a.byKey[ch.e.key]; old != nil {
			id, roots := old.ID, old.roots
			*old = *ch.e
			old.ID, old.roots = id, roots
			sortEntries(a.order)
		} else {
			a.ids++
			ch.e.ID = a.ids
			a.entries[ch.e.ID] = ch.e
			a.byKey[ch.e.key] = ch.e
			a.order = append(a.order, ch.e)
			sortEntries(a.order)
		}
		a.learn(a.byKey[ch.e.key])
		if ch.root != "" {
			e := a.byKey[ch.e.key]
			if e.roots == nil {
				e.roots = map[string]bool{}
			}
			e.roots[ch.root] = true
		}
	case ch.gone != "":
		e := a.byKey[ch.gone]
		if e == nil {
			return
		}
		delete(e.roots, ch.root)
		if _, err := os.Stat(e.path); err != nil || !a.wanted(e) {
			a.remove(e)
		}
	case ch.ready:
		delete(a.reading, ch.root)
		a.Scanning = len(a.reading) > 0
	}
}

// wanted says whether the library keeps e: it lies in a folder
// followed, or was added on its own, or is on a playlist.
func (a *app) wanted(e *entry) bool {
	if len(e.roots) > 0 || e.song != nil || slices.Contains(a.kept.Files, e.key) || slices.Contains(a.queue, e.key) {
		return true
	}
	for _, p := range a.kept.Playlists {
		if slices.Contains(p.Paths, e.key) {
			return true
		}
	}
	return false
}

// remove takes e out of the library. The track playing stays known
// until it stops, so the window still shows it.
func (a *app) remove(e *entry) {
	delete(a.byKey, e.key)
	if i := slices.Index(a.order, e); i >= 0 {
		a.order = slices.Delete(a.order, i, i+1)
	}
	if e.ID != a.Current {
		delete(a.entries, e.ID)
	}
}

// refresh copies the library into the state the window sees.
func (a *app) refresh() {
	a.Tracks = a.Tracks[:0:0]
	a.Library = a.Library[:0:0]
	for _, e := range a.order {
		tr := e.Track
		tr.Added = slices.Contains(a.kept.Files, e.key)
		a.Tracks = append(a.Tracks, tr)
		a.Library = append(a.Library, e.ID)
	}
	if e := a.entries[a.Current]; e != nil && a.byKey[e.key] != e {
		a.Tracks = append(a.Tracks, e.Track)
	}
	a.Playlists = a.Playlists[:0:0]
	for _, p := range a.kept.Playlists {
		pl := Playlist{ID: p.ID, Name: p.Name, Tracks: []int{}}
		for _, k := range p.Paths {
			if e := a.byKey[k]; e != nil {
				pl.Tracks = append(pl.Tracks, e.ID)
			}
		}
		a.Playlists = append(a.Playlists, pl)
	}
	a.Queue = a.Queue[:0:0]
	for _, k := range a.queue {
		if e := a.byKey[k]; e != nil {
			a.Queue = append(a.Queue, e.ID)
		}
	}
	a.Folders = a.Folders[:0:0]
	for _, path := range a.kept.Folders {
		f := Folder{Path: path, Tracks: []int{}, Reading: a.reading[path]}
		for _, e := range a.order {
			if e.roots[path] {
				f.Tracks = append(f.Tracks, e.ID)
			}
		}
		a.Folders = append(a.Folders, f)
	}
	a.Scanning = len(a.reading) > 0
}

// list returns the tracks of the list from, by their IDs, in order.
func (a *app) list(from ListID) []int {
	s := string(from)
	switch {
	case from == QueueList:
		return a.Queue
	case strings.HasPrefix(s, "p:"):
		for _, p := range a.Playlists {
			if p.ID == s[2:] {
				return p.Tracks
			}
		}
	case strings.HasPrefix(s, "f:"):
		for _, f := range a.Folders {
			if f.Path == s[2:] {
				return f.Tracks
			}
		}
	default:
		return a.Library
	}
	return nil
}

// follow starts following dir, and keeps it among the folders
// followed.
func (a *app) follow(dir string) {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if slices.Contains(a.kept.Folders, dir) {
		return
	}
	a.kept.Folders = append(a.kept.Folders, dir)
	a.dirty = true
	a.startFollowing(dir)
}

// startFollowing reads dir and follows it as it changes.
func (a *app) startFollowing(dir string) {
	if _, ok := a.following[dir]; ok || a.ctx == nil {
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.following[dir] = cancel
	a.reading[dir] = true
	a.Scanning = true
	go follow(ctx, dir, a.changes)
}

// forget stops following dir. Its tracks leave the library, all but
// those it keeps for another reason.
func (a *app) forget(dir string) {
	i := slices.Index(a.kept.Folders, dir)
	if i < 0 {
		return
	}
	a.kept.Folders = slices.Delete(a.kept.Folders, i, i+1)
	a.dirty = true
	if cancel := a.following[dir]; cancel != nil {
		cancel()
	}
	delete(a.following, dir)
	delete(a.reading, dir)
	for _, e := range slices.Clone(a.order) {
		if e.roots[dir] {
			delete(e.roots, dir)
			if !a.wanted(e) {
				a.remove(e)
			}
		}
	}
	if a.From == FolderList(dir) {
		a.From = AllTracks
	}
}

// readMissing reads, in the background, the files the library keeps
// that no folder followed holds: files added on their own and tracks
// on playlists.
func (a *app) readMissing() {
	var paths []string
	seen := map[string]bool{}
	want := func(k string) {
		if seen[k] || a.byKey[k] != nil || strings.HasPrefix(k, "demo:") {
			return
		}
		seen[k] = true
		for _, f := range a.kept.Folders {
			if strings.HasPrefix(k, f+string(filepath.Separator)) {
				return
			}
		}
		paths = append(paths, k)
	}
	for _, k := range a.kept.Files {
		want(k)
	}
	for _, p := range a.kept.Playlists {
		for _, k := range p.Paths {
			want(k)
		}
	}
	a.readFiles(paths)
}

// readFiles reads paths in the background, into the library.
func (a *app) readFiles(paths []string) {
	if len(paths) == 0 || a.ctx == nil {
		return
	}
	go func() {
		for _, path := range paths {
			e := readEntry(path)
			if e == nil {
				continue
			}
			select {
			case a.changes <- change{e: e}:
			case <-a.ctx.Done():
				return
			}
		}
	}()
}

// addPaths adds files to a playlist, or to the library where playlist
// is empty. A folder joins the folders followed.
func (a *app) addPaths(paths []string, playlist string) {
	var read []string
	for _, path := range paths {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		if fi.IsDir() {
			a.follow(path)
			continue
		}
		if !isTrack(path) {
			continue
		}
		if playlist == "" {
			if !slices.Contains(a.kept.Files, path) {
				a.kept.Files = append(a.kept.Files, path)
			}
		} else if p := a.playlist(playlist); p != nil {
			p.Paths = append(p.Paths, path)
		}
		a.dirty = true
		if a.byKey[path] == nil {
			read = append(read, path)
		}
	}
	a.readFiles(read)
	a.refresh()
}

// playlist returns the playlist kept as id, or nil.
func (a *app) playlist(id string) *savedList {
	for i := range a.kept.Playlists {
		if a.kept.Playlists[i].ID == id {
			return &a.kept.Playlists[i]
		}
	}
	return nil
}

// keys returns the tracks ids as the library keeps them.
func (a *app) keys(ids []int) []string {
	var out []string
	for _, id := range ids {
		if e := a.entries[id]; e != nil {
			out = append(out, e.key)
		}
	}
	return out
}

// placeOf returns where in keys the track at place at of the list the
// window sees lies, counting the tracks not read yet, or -1.
func (a *app) placeOf(keys []string, at int) int {
	n := 0
	for i, k := range keys {
		if a.byKey[k] == nil {
			continue
		}
		if n == at {
			return i
		}
		n++
	}
	return -1
}

// askLeave asks the user, in the background, for leave to read their
// music, and hands their answer to the loop.
func (a *app) askLeave() {
	if a.askMusic == nil {
		return
	}
	go func() {
		ok := a.askMusic()
		select {
		case a.granted <- ok:
		case <-a.ctx.Done():
		}
	}()
}

// onGranted follows the user's music folder once the user lets the
// player read their music, and reads every folder followed through
// again: read before, they showed nothing.
func (a *app) onGranted(ok bool) {
	if !ok {
		return
	}
	if a.home != "" {
		a.follow(a.home)
	}
	for _, dir := range slices.Clone(a.kept.Folders) {
		if cancel := a.following[dir]; cancel != nil {
			cancel()
		}
		delete(a.following, dir)
		a.startFollowing(dir)
	}
	a.refresh()
}

// ask shows the system's dialog in the background, and hands what was
// chosen to the loop. Where the system has no dialog to choose a
// folder, as a phone's, the player follows the user's music folder,
// once the user lets it read their music.
func (a *app) ask(o driver.ChooseOptions, playlist string) {
	if a.choose == nil {
		return
	}
	go func() {
		paths, err := a.choose(o)
		if errors.Is(err, driver.ErrNoChooser) && o.Folders {
			ok := a.askMusic == nil || a.askMusic()
			select {
			case a.granted <- ok:
			case <-a.ctx.Done():
			}
			return
		}
		if err != nil {
			log.Printf("music: %v", err)
			return
		}
		// A folder chosen on a phone is read with leave to read music.
		if o.Folders && len(paths) > 0 && a.askMusic != nil && !a.askMusic() {
			return
		}
		if len(paths) > 0 {
			select {
			case a.chosen <- chosen{paths, playlist}:
			case <-a.ctx.Done():
			}
		}
	}()
}

func (a *app) handle(in gunim.Intent) {
	switch in := in.(type) {
	case PlayTrack:
		if in.From != QueueList {
			a.From = in.From
			a.start(in.ID)
			break
		}
		// A track picked on Up next plays, and those before it are
		// passed by; the list playing goes on after.
		if i := slices.Index(a.Queue, in.ID); i >= 0 {
			a.queue = a.queue[a.placeOf(a.queue, i)+1:]
			a.refresh()
		}
		at := a.listAt
		a.start(in.ID)
		a.listAt = at
	case AddPaths:
		switch to := string(in.To); {
		case in.Play:
			a.spreadOut(in.Paths, spread{to: QueueList, at: -1, opened: true})
		case in.To == QueueList || strings.HasPrefix(to, "p:"):
			a.spreadOut(in.Paths, spread{to: in.To, at: in.At})
		default:
			a.addPaths(in.Paths, "")
		}
	case Enqueue:
		keys := a.keys(in.Tracks)
		switch {
		case in.Play && len(keys) > 0:
			a.queue = slices.Concat(keys[1:], a.queue)
			a.refresh()
			// The track played leaves the list where it was, as Up
			// next's do.
			at := a.listAt
			a.start(a.byKey[keys[0]].ID)
			a.listAt = at
		case in.Next:
			a.queue = append(keys, a.queue...)
		default:
			a.queue = append(a.queue, keys...)
		}
		a.refresh()
	case Unqueue:
		if i := a.placeOf(a.queue, in.At); i >= 0 {
			k := a.queue[i]
			a.queue = slices.Delete(a.queue, i, i+1)
			if e := a.byKey[k]; e != nil && !a.wanted(e) {
				a.remove(e)
			}
			a.refresh()
		}
	case MoveInQueue:
		from, to := a.placeOf(a.queue, in.From), a.placeOf(a.queue, in.To)
		if from >= 0 && to >= 0 && from != to {
			k := a.queue[from]
			a.queue = slices.Insert(slices.Delete(a.queue, from, from+1), to, k)
			a.refresh()
		}
	case SetGainMode:
		a.GainMode = in.Mode
		m := in.Mode
		a.kept.GainMode = &m
		a.dirty = true
		// Past full, the volume needs the gain: with it off, the volume
		// comes back to full.
		if a.Volume > maxVolume(m) {
			a.Volume = maxVolume(m)
			a.d.setVolume(a.Volume)
			v := a.Volume
			a.kept.Volume = &v
		}
		a.applyGain()
	case SetEQ:
		a.setEQ(in.EQ)
		eq := in.EQ
		a.kept.EQ = &eq
		a.dirty = true
		a.applyGain()
	case ClearQueue:
		gone := a.queue
		a.queue = nil
		for _, k := range gone {
			if e := a.byKey[k]; e != nil && !a.wanted(e) {
				a.remove(e)
			}
		}
		a.refresh()
	case TogglePlay:
		switch {
		case a.Current == 0 && len(a.Queue) > 0:
			a.playNext()
		case a.Current == 0 && len(a.Library) > 0:
			a.From = AllTracks
			a.start(a.Library[0])
		case a.Current != 0:
			a.Playing = !a.Playing
			a.d.setPaused(!a.Playing)
		}
	case Skip:
		a.skip(in.Back)
	case SeekTo:
		a.d.seek(in.At)
		a.seeks++
	case SetVolume:
		a.Volume = max(0, min(maxVolume(a.GainMode), in.Volume))
		a.d.setVolume(a.Volume)
		v := a.Volume
		a.kept.Volume = &v
		a.dirty = true
		a.applyGain()
	case ToggleShuffle:
		a.Shuffle = !a.Shuffle
		a.kept.Shuffle = a.Shuffle
		a.dirty = true
		a.applyGain()
	case CycleRepeat:
		a.Repeat = (a.Repeat + 1) % 3
		a.kept.Repeat = a.Repeat
		a.dirty = true
	case AddFolder:
		a.ask(driver.ChooseOptions{Title: "Add a folder to the library", Folders: true}, "")
	case ForgetFolder:
		a.forget(in.Path)
		a.refresh()
	case SetUpdateMode:
		if err := install.SetUpdates(installer(), install.UpdateMode(in.Mode)); err != nil {
			log.Printf("skiva: setting how updates come: %v", err)
		}
		a.Settings.Mode = string(updateMode())
	case SetBeta:
		a.kept.Beta = &in.On
		a.dirty = true
		a.Settings.Beta = in.On
	case ShowAbout:
		a.showing(a.windows.showAbout())
	case ShowUpdate:
		a.showing(a.windows.showUpdate(in.Release, in.Ready))
	case ShowWhatsNew:
		a.showing(a.windows.showWhatsNew(in.From))
	case ShowPrivacy:
		if a.openLink != nil {
			go func() {
				if err := a.openLink(privacyURL); err != nil {
					log.Printf("music: %v", err)
				}
			}()
		}
	case AddFiles:
		a.ask(driver.ChooseOptions{Title: "Add music", Multiple: true, Filters: []driver.FileFilter{
			{Name: "Music", Patterns: []string{"*.mp3", "*.flac", "*.ogg", "*.oga", "*.wav"}},
		}}, in.Playlist)
	case NewPlaylist:
		if in.ID == "" || a.playlist(in.ID) != nil {
			return
		}
		a.kept.Playlists = append(a.kept.Playlists, savedList{ID: in.ID, Name: in.Name, Paths: a.keys(in.Tracks)})
		a.dirty = true
		a.refresh()
		if len(in.Paths) > 0 {
			a.spreadOut(in.Paths, spread{to: PlaylistList(in.ID), at: -1})
		}
	case RenamePlaylist:
		if p := a.playlist(in.ID); p != nil && strings.TrimSpace(in.Name) != "" {
			p.Name = strings.TrimSpace(in.Name)
			a.dirty = true
			a.refresh()
		}
	case DeletePlaylist:
		i := slices.IndexFunc(a.kept.Playlists, func(p savedList) bool { return p.ID == in.ID })
		if i < 0 {
			return
		}
		paths := a.kept.Playlists[i].Paths
		a.kept.Playlists = slices.Delete(a.kept.Playlists, i, i+1)
		for _, k := range paths {
			if e := a.byKey[k]; e != nil && !a.wanted(e) {
				a.remove(e)
			}
		}
		if a.From == PlaylistList(in.ID) {
			a.From = AllTracks
		}
		a.dirty = true
		a.refresh()
	case AddToPlaylist:
		if p := a.playlist(in.ID); p != nil {
			p.Paths = append(p.Paths, a.keys(in.Tracks)...)
			a.dirty = true
			a.refresh()
		}
	case RemoveFromPlaylist:
		p := a.playlist(in.ID)
		if p == nil {
			return
		}
		if i := a.placeOf(p.Paths, in.At); i >= 0 {
			k := p.Paths[i]
			p.Paths = slices.Delete(p.Paths, i, i+1)
			if e := a.byKey[k]; e != nil && !a.wanted(e) {
				a.remove(e)
			}
			a.dirty = true
			a.refresh()
		}
	case RemoveFromLibrary:
		e := a.entries[in.ID]
		if e == nil {
			return
		}
		if i := slices.Index(a.kept.Files, e.key); i >= 0 {
			a.kept.Files = slices.Delete(a.kept.Files, i, i+1)
			if !a.wanted(e) {
				a.remove(e)
			}
			a.dirty = true
			a.refresh()
		}
	case MoveInPlaylist:
		p := a.playlist(in.ID)
		if p == nil {
			return
		}
		from, to := a.placeOf(p.Paths, in.From), a.placeOf(p.Paths, in.To)
		if from < 0 || to < 0 || from == to {
			return
		}
		k := p.Paths[from]
		p.Paths = slices.Insert(slices.Delete(p.Paths, from, from+1), to, k)
		a.dirty = true
		a.refresh()
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
	// The track starts at its own gain, worked out for the list it
	// plays from; the track before fades out at its own.
	a.voice = a.d.play(src, closer, false, a.gainFor(e))
	a.upNext, a.turning = nil, false
	// The list playing goes on from its track played last.
	if slices.Contains(a.list(a.From), id) {
		a.listAt = id
	}
	if a.Current != id {
		a.history = append(a.history, id)
		a.dropGone()
	}
	a.Current, a.Playing = id, true
	a.Starts++
	a.remember(e)
	a.prepareNext()
}

// closeFade is how long the music takes to fade as the window closes.
const closeFade = 700 * time.Millisecond

// remember keeps e, and the list it plays from, for the next run.
func (a *app) remember(e *entry) {
	if a.kept.Last != e.key || a.kept.LastFrom != a.From {
		a.kept.Last, a.kept.LastFrom = e.key, a.From
		a.dirty = true
	}
}

// takeUp takes up the track of the last run, paused, with the list it
// played from, once the library has it; it lets it go once the folders
// are read through without it, or something else plays.
func (a *app) takeUp() {
	if a.resume == "" {
		return
	}
	if a.Current != 0 {
		a.resume = ""
		return
	}
	e := a.byKey[a.resume]
	if e == nil {
		if !a.Scanning {
			a.resume = ""
		}
		return
	}
	a.resume = ""
	a.From = AllTracks
	if from := a.resumeFrom; from == QueueList || a.list(from) != nil {
		a.From = from
	}
	if a.From == QueueList {
		a.From = AllTracks
	}
	src, closer, err := e.open()
	if err != nil {
		return
	}
	a.voice = a.d.play(src, closer, true, a.gainFor(e))
	a.upNext, a.turning = nil, false
	if slices.Contains(a.list(a.From), e.ID) {
		a.listAt = e.ID
	}
	a.history = append(a.history, e.ID)
	a.Current, a.Playing = e.ID, false
	a.Starts++
	a.Resumed++
	a.prepareNext()
}

// learn gives e what was kept of it from a run before, where its file
// is unchanged since. A track not kept is read through once the player
// needs it; see scanAhead.
func (a *app) learn(e *entry) {
	if e == nil || e.an != nil {
		return
	}
	if an, ok := a.analyses[e.key]; ok && an.fresh(e.path) {
		e.analyzed(an)
	}
}

// gainOf returns the gain e plays at, in decibels, what it follows,
// and its album's loudness where that is it.
// It returns too the peak that gain lifts, in dBFS, zero where it is
// not known yet.
func (a *app) gainOf(e *entry) (db float64, by GainSource, album, peak float64) {
	if e != nil && e.an != nil && e.an.Peak > 0 {
		peak = dB(float64(e.an.Peak))
	}
	if e == nil || a.GainMode == GainOff {
		return 0, GainNone, 0, peak
	}
	if e.an == nil {
		return 0, GainMeasuring, 0, peak
	}
	if a.albumGain() {
		if lufs, p, ok := a.albumLoudness(e); ok {
			peak = dB(float64(max(p, 1e-6)))
			return min(targetLUFS-lufs, -peak), GainByAlbum, lufs, peak
		}
	}
	if !e.an.Loud {
		return 0, GainNone, 0, peak
	}
	// A track lifted plays no louder than its peak lets it, unclipped.
	return min(targetLUFS-e.an.LUFS, -peak), GainByTrack, 0, peak
}

// albumGain says whether tracks play at their album's gain. Album gain
// fits an album played in order; shuffled, or from a list of many
// albums' tracks, each track evens out on its own.
func (a *app) albumGain() bool {
	return a.GainMode == GainAlbum && !a.Shuffle && (a.From == AllTracks || strings.HasPrefix(string(a.From), "f:"))
}

// albumKey names e's album: its title, in its folder.
func albumKey(e *entry) string {
	return filepath.Dir(e.path) + "\x00" + strings.ToLower(e.Album)
}

// albumLoudness returns the loudness of e's album, from its tracks
// measured so far, each counting for its length, and its loudest
// sample.
func (a *app) albumLoudness(e *entry) (lufs float64, peak float32, ok bool) {
	key := albumKey(e)
	var energy, weight float64
	for _, o := range a.order {
		if o.an == nil || !o.an.Loud || albumKey(o) != key {
			continue
		}
		w := max(o.Length.Seconds(), 1)
		energy += w * math.Pow(10, o.an.LUFS/10)
		weight += w
		peak = max(peak, o.an.Peak)
	}
	if weight == 0 {
		return 0, 0, false
	}
	return 10 * math.Log10(energy/weight), peak, true
}

// applyGain sets the gain the track playing plays at, gliding to it.
func (a *app) applyGain() { a.d.setGain(a.gainFor(a.entries[a.Current]), true) }

// gainFor returns the gain track e plays at, in decibels, and shows it
// as the gain of the track playing.
//
// The equalizer's boosts would lift the track's peaks with them: the
// track plays lowered by as much of the boost as would take its peaks
// past full scale, and no more, so a boost still lifts where there is
// room, and never drives the limiter on its own. The volume past full
// may still: that is the listener's to choose.
func (a *app) gainFor(e *entry) float64 {
	db, by, album, headroom := a.gainAll(e)
	a.Gain, a.GainBy, a.AlbumLUFS, a.Headroom = float32(db), by, float32(album), float32(headroom)
	return db - headroom
}

// gainAll returns e's loudness gain, what it follows and its album's
// loudness, as gainOf does, and the headroom the equalizer takes from
// it.
func (a *app) gainAll(e *entry) (db float64, by GainSource, album, headroom float64) {
	db, by, album, peak := a.gainOf(e)
	vol := dB(float64(max(min(a.Volume, maxVolume(a.GainMode)), 1e-6)))
	over := peak + db + min(vol, 0) + a.eqBoost
	headroom = max(0, min(over, a.eqBoost))
	return db, by, album, headroom
}

// peekNext returns the track to follow the one playing: itself again
// on repeat one, Up next's first, or the next of the list playing, as
// shuffle and repeat say; fromQueue says it is Up next's.
func (a *app) peekNext() (e *entry, fromQueue bool) {
	if a.Repeat == RepeatOne {
		return a.entries[a.Current], false
	}
	for _, k := range a.queue {
		if e := a.byKey[k]; e != nil {
			return e, true
		}
	}
	return a.entries[a.next()], false
}

// prepareNext queues the track to follow the one playing in the deck,
// to play on into it without a gap, at its own gain: again wherever
// what follows, or its gain, has changed since.
func (a *app) prepareNext() {
	if a.voice == nil || a.Current == 0 {
		a.z.want(nil)
		return
	}
	e, fromQueue := a.peekNext()
	a.scanAhead(e)
	// The voice has turned to the track queued, which the speakers
	// have yet to reach: it is the one playing, and stays.
	if a.turning {
		return
	}
	var gain float64
	if e != nil {
		db, _, _, headroom := a.gainAll(e)
		gain = db - headroom
	}
	if e == a.upNext && fromQueue == a.upFromQueue && math.Abs(gain-a.upGain) < 0.01 {
		return
	}
	a.upNext, a.upFromQueue, a.upGain = e, fromQueue, gain
	if e == nil {
		a.d.queue(nil, nil, 0)
		return
	}
	src, closer, err := e.open()
	if err != nil {
		a.upNext = nil
		a.d.queue(nil, nil, 0)
		return
	}
	a.d.queue(src, closer, gain)
}

// scanAhead has the tracks the gain needs read through, where no
// analysis is kept of them: the track playing, then the track to
// follow it, and, where album gain applies, the rest of their albums,
// whose loudness the album's gain sums. The rest of the library is read
// as it comes to play, so a player stopped reads nothing.
func (a *app) scanAhead(next *entry) {
	var want []*entry
	add := func(e *entry) {
		if e != nil && e.an == nil && !slices.Contains(want, e) {
			want = append(want, e)
		}
	}
	cur := a.entries[a.Current]
	add(cur)
	add(next)
	if a.albumGain() {
		for _, e := range []*entry{cur, next} {
			if e == nil {
				continue
			}
			key := albumKey(e)
			for _, o := range a.order {
				if albumKey(o) == key {
					add(o)
				}
			}
		}
	}
	a.z.want(want)
}

// turned moves the player on to the track queued, which the speakers
// now play, run on from the last without a gap.
func (a *app) turned() {
	e, fromQueue := a.upNext, a.upFromQueue
	a.upNext, a.turning = nil, false
	a.d.turned()
	if e == nil {
		return
	}
	if fromQueue {
		if i := slices.Index(a.queue, e.key); i >= 0 {
			a.queue = slices.Delete(a.queue, i, i+1)
			a.refresh()
		}
	}
	if a.Current != e.ID {
		a.history = append(a.history, e.ID)
		a.dropGone()
	}
	if !fromQueue && slices.Contains(a.list(a.From), e.ID) {
		a.listAt = e.ID
	}
	a.Current = e.ID
	a.Starts++
	a.remember(e)
	a.d.setGain(a.gainFor(e), false)
	a.prepareNext()
}

// dropGone forgets the track playing if it has left the library, as
// another starts.
func (a *app) dropGone() {
	if e := a.entries[a.Current]; e != nil && a.byKey[e.key] != e {
		delete(a.entries, a.Current)
	}
}

// setEQ puts the equalizer's settings in play, and works out the most
// it lifts any frequency.
func (a *app) setEQ(eq EQ) {
	a.EQ = eq
	a.d.eq.Set(eq.Bands)
	a.d.eq.SetBypass(eq.Bypass)
	a.eqBoost = 0
	if !eq.Bypass {
		for i := range 200 {
			hz := 20 * math.Pow(1000, float64(i)/199)
			a.eqBoost = max(a.eqBoost, audio.Response(eq.Bands, hz))
		}
	}
}

// ended moves on as a track ends.
func (a *app) ended() {
	a.voice = nil
	if a.Repeat == RepeatOne {
		a.start(a.Current)
		return
	}
	if a.playNext() {
		return
	}
	a.Playing = false
	a.dropGone()
	a.Current = 0
	a.d.stop()
	a.refresh()
	a.prepareNext()
}

// playNext plays what comes next: the first track on Up next, or the
// track after the last of the list playing. It reports whether there
// was one.
func (a *app) playNext() bool {
	for len(a.queue) > 0 {
		k := a.queue[0]
		a.queue = a.queue[1:]
		if e := a.byKey[k]; e != nil {
			a.refresh()
			// A track of Up next leaves the list where it was.
			at := a.listAt
			a.start(e.ID)
			a.listAt = at
			return true
		}
	}
	if n := a.next(); n != 0 {
		a.start(n)
		return true
	}
	return false
}

// next returns the track after the list playing's last, as shuffle
// and repeat say, or zero at the end.
func (a *app) next() int {
	list := a.list(a.From)
	if len(list) == 0 {
		return 0
	}
	if a.Shuffle {
		if len(list) == 1 {
			return list[0]
		}
		// The pick for this track holds, so the track queued to follow
		// it is the one Next goes to.
		if a.shuffleFor == a.Current && a.shuffleNext != a.Current && slices.Contains(list, a.shuffleNext) {
			return a.shuffleNext
		}
		for {
			if id := list[a.rng.IntN(len(list))]; id != a.Current {
				a.shuffleFor, a.shuffleNext = a.Current, id
				return id
			}
		}
	}
	i := slices.Index(list, a.listAt)
	switch {
	case i+1 < len(list):
		return list[i+1]
	case a.Repeat == RepeatAll:
		return list[0]
	}
	return 0
}

// skip goes to the next track, or back.
func (a *app) skip(back bool) {
	list := a.list(a.From)
	if !back {
		if !a.playNext() && len(list) > 0 {
			a.start(list[0])
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
	// From a track of Up next, Back goes to the list's track it broke
	// into.
	if a.Current != a.listAt && slices.Contains(list, a.listAt) {
		a.start(a.listAt)
		return
	}
	i := slices.Index(list, a.Current)
	if i > 0 {
		a.start(list[i-1])
	} else {
		a.d.seek(0)
	}
}

// spreadOut opens the folders among paths, in the background, and
// hands the files to place, for the list and the place x says.
func (a *app) spreadOut(paths []string, x spread) {
	go func() {
		var files []string
		for _, path := range paths {
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
			fi, err := os.Stat(path)
			switch {
			case err != nil:
			case fi.IsDir():
				var inside []string
				_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() && isTrack(p) && len(inside) < maxTracks {
						inside = append(inside, p)
					}
					return nil
				})
				slices.Sort(inside)
				files = append(files, inside...)
			case isTrack(path):
				files = append(files, path)
			}
		}
		x.paths = files
		select {
		case a.spread <- x:
		case <-a.ctx.Done():
		}
	}()
}

// place puts files dropped on a list on it, at their place, and reads
// those the library has yet to.
func (a *app) place(x spread) {
	if len(x.paths) == 0 {
		return
	}
	insert := func(keys []string) []string {
		i := len(keys)
		if x.at >= 0 {
			if j := a.placeOf(keys, x.at); j >= 0 {
				i = j
			}
		}
		return slices.Insert(keys, i, x.paths...)
	}
	switch s := string(x.to); {
	case x.opened:
		a.placeOpened(x.paths, x.together)
	case x.to == QueueList:
		a.queue = insert(a.queue)
	case strings.HasPrefix(s, "p:"):
		p := a.playlist(s[2:])
		if p == nil {
			return
		}
		p.Paths = insert(p.Paths)
		a.dirty = true
	}
	var read []string
	for _, path := range x.paths {
		if a.byKey[path] == nil && !slices.Contains(read, path) {
			read = append(read, path)
		}
	}
	a.readFiles(read)
	a.refresh()
	a.playOpened()
}

// privacyURL is Skiva's privacy policy, which Google Play asks an app to
// link to from inside it.
const privacyURL = "https://skalarit-ab.github.io/skiva/privacy.html"
