package main

import (
	"bytes"
	"context"
	"image/color"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/paint"
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
	// SetVolume sets the volume, 0 to 1.
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
	// NewPlaylist makes a playlist, with tracks or none. The window
	// picks its ID, so it can open the playlist at once.
	NewPlaylist struct {
		ID, Name string
		Tracks   []int
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
	// MoveInPlaylist moves the track at From on a playlist to To.
	MoveInPlaylist struct {
		ID       string
		From, To int
	}
)

// The settings of Repeat.
const (
	RepeatOff Repeat = iota
	RepeatAll
	RepeatOne
)

// AllTracks is the whole library, as a list to play from.
const AllTracks ListID = ""

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
	rng     *rand.Rand
	// seeks counts the seeks, for the media controls to hear of each.
	seeks int
	// peaksDone carries peaks read in the background.
	peaksDone chan peaksRead

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
	chosen chan chosen
}

type peaksRead struct {
	id    int
	peaks []float32
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
}

// newApp returns the application half, with the library kept in file
// and the demo songs.
func newApp(ctx context.Context, d *deck, file string) *app {
	a := &app{
		d: d, entries: map[int]*entry{}, byKey: map[string]*entry{},
		rng:       rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7)),
		peaksDone: make(chan peaksRead, 4),
		ctx:       ctx, file: file,
		changes:   make(chan change, 64),
		following: map[string]context.CancelFunc{},
		reading:   map[string]bool{},
		chosen:    make(chan chosen, 1),
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
	a.choose = func(o driver.ChooseOptions) ([]string, error) { return c.ChooseFiles(ctx, o) }
	kept, ok := loadSaved(at.file)
	a.kept = kept
	if !ok && at.home != "" {
		a.kept.Folders = append(a.kept.Folders, at.home)
		a.dirty = true
	}
	if kept.Volume != nil {
		a.Volume = max(0, min(1, *kept.Volume))
		d.setVolume(a.Volume)
	}
	a.Shuffle, a.Repeat = kept.Shuffle, kept.Repeat
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
	var batch, keep <-chan time.Time
	for {
		var ended <-chan struct{}
		if a.voice != nil {
			ended = a.voice.Done()
		}
		if a.dirty && keep == nil {
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
		case <-keep:
			keep = nil
			a.save()
			continue
		case got := <-a.chosen:
			a.addPaths(got.paths, got.playlist)
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

// save writes the library where it is kept, if it changed.
func (a *app) save() {
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
	if len(e.roots) > 0 || e.song != nil || slices.Contains(a.kept.Files, e.key) {
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
		a.Tracks = append(a.Tracks, e.Track)
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

// placeOf returns where on p the track at place at of the playlist the
// window sees lies, counting the tracks it cannot find, or -1.
func (a *app) placeOf(p *savedList, at int) int {
	n := 0
	for i, k := range p.Paths {
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

// ask shows the system's dialog in the background, and hands what was
// chosen to the loop.
func (a *app) ask(o driver.ChooseOptions, playlist string) {
	if a.choose == nil {
		return
	}
	go func() {
		paths, err := a.choose(o)
		if err != nil {
			log.Printf("music: %v", err)
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
		a.From = in.From
		a.start(in.ID)
	case TogglePlay:
		switch {
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
		a.Volume = max(0, min(1, in.Volume))
		a.d.setVolume(a.Volume)
		v := a.Volume
		a.kept.Volume = &v
		a.dirty = true
	case ToggleShuffle:
		a.Shuffle = !a.Shuffle
		a.kept.Shuffle = a.Shuffle
		a.dirty = true
	case CycleRepeat:
		a.Repeat = (a.Repeat + 1) % 3
		a.kept.Repeat = a.Repeat
		a.dirty = true
	case AddFolder:
		a.ask(driver.ChooseOptions{Title: "Add a folder to the library", Folders: true}, "")
	case ForgetFolder:
		a.forget(in.Path)
		a.refresh()
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
		if i := a.placeOf(p, in.At); i >= 0 {
			k := p.Paths[i]
			p.Paths = slices.Delete(p.Paths, i, i+1)
			if e := a.byKey[k]; e != nil && !a.wanted(e) {
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
		from, to := a.placeOf(p, in.From), a.placeOf(p, in.To)
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
	a.voice = a.d.play(src, closer, false)
	if a.Current != id {
		a.history = append(a.history, id)
		a.dropGone()
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

// dropGone forgets the track playing if it has left the library, as
// another starts.
func (a *app) dropGone() {
	if e := a.entries[a.Current]; e != nil && a.byKey[e.key] != e {
		delete(a.entries, a.Current)
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
	if a.Repeat == RepeatOne {
		a.start(a.Current)
		return
	}
	if n := a.next(); n != 0 {
		a.start(n)
		return
	}
	a.Playing = false
	a.dropGone()
	a.Current = 0
	a.d.stop()
	a.refresh()
}

// next returns the track after the current one on the list playing,
// as shuffle and repeat say, or zero at the end.
func (a *app) next() int {
	list := a.list(a.From)
	if len(list) == 0 {
		return 0
	}
	if a.Shuffle {
		if len(list) == 1 {
			return list[0]
		}
		for {
			if id := list[a.rng.IntN(len(list))]; id != a.Current {
				return id
			}
		}
	}
	i := slices.Index(list, a.Current)
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
		if n := a.next(); n != 0 {
			a.start(n)
		} else if len(list) > 0 {
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
	i := slices.Index(list, a.Current)
	if i > 0 {
		a.start(list[i-1])
	} else {
		a.d.seek(0)
	}
}
