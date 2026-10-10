package main

import (
	"context"
	"errors"
	"image"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/install"
)

// Installing Skiva on a desktop, and keeping it up to date. gunim's
// install does both: the program downloaded from a release is its own
// installer, which puts it in the user's programs with a shortcut and
// offers to open music files with it. The installed copy looks for a
// newer release a minute after it starts and every four hours, and as
// the settings say, puts it in place for its next start, asks first, or
// does nothing. Beta releases come too where the settings take them.
//
// Releases are made by pushing a tag, v0.2.0 or v0.2.0-beta.1, which the
// release workflow builds, signs and publishes; see RELEASING.md.

// version is this build's version, set as a release is built:
//
//	go build -ldflags "-X main.version=v0.2.0"
//
// A build from a working tree is "dev": it runs as it is, with no
// installer, and looks for no updates.
var version = "dev"

// repo is where Skiva's releases are, on GitHub.
const repo = "skalarit-ab/skiva"

// updateKey is the public key Skiva's releases are signed with, as
// gunimsign -keygen printed it. An installed Skiva runs an update only
// when its SHA256SUMS carries a signature this key checks.
const updateKey = "XUIkX/TORff7PVhatXqMen5X7u8QS/P20eqUJWuSnfw="

// fileTypes are the music files the installer offers to open with
// Skiva: those its library reads, as audioExts lists them.
var fileTypes = []install.FileType{
	{Name: "MP3 audio", Exts: []string{".mp3"}, MIME: "audio/mpeg", Default: true},
	{Name: "FLAC audio", Exts: []string{".flac"}, MIME: "audio/flac", Default: true},
	{Name: "Ogg Vorbis audio", Exts: []string{".ogg", ".oga"}, MIME: "audio/x-vorbis+ogg", Default: true},
	{Name: "WAV audio", Exts: []string{".wav"}, MIME: "audio/x-wav", Default: true},
}

// installer describes Skiva to gunim's install.
func installer() install.App {
	return install.App{
		Name:        appName,
		ID:          dirName,
		Version:     version,
		Publisher:   "Skalarit AB",
		Description: "A music player whose record spins with the music",
		URL:         "https://github.com/" + repo,
		IconFunc:    func() image.Image { return drawIcon(256) },
		Categories:  "AudioVideo;Audio;Player;",
		FileTypes:   fileTypes,
		Updates:     releases{},
		UpdateKey:   updateKey,
		Available:   func(r install.Release) { tell(updateNews{release: r}) },
		Updated:     func(r install.Release) { tell(updateNews{release: r, ready: true}) },
		Data:        dataDirs(),
		Quit:        quitRunning,
	}
}

// releases are Skiva's releases on GitHub, with the betas where the
// settings take them: asked at each look, so turning betas on or off
// takes at the next look, with no restart.
type releases struct{}

func (releases) github() install.GitHub {
	return install.GitHub{Repo: repo, Prerelease: wantsBeta()}
}

// Latest implements [install.Source].
func (r releases) Latest(ctx context.Context) (install.Release, error) {
	return r.github().Latest(ctx)
}

// ReleaseNotes implements [install.Changelog].
func (r releases) ReleaseNotes(ctx context.Context) ([]install.ReleaseNotes, error) {
	return r.github().ReleaseNotes(ctx)
}

// wantsBeta reports whether updates take beta releases: as the settings
// say, and where they say nothing, when this build is a beta, so a beta
// goes on to the next.
func wantsBeta() bool {
	if s, ok := loadSaved(stateFile()); ok && s.Beta != nil {
		return *s.Beta
	}
	return isBeta(version)
}

// isBeta reports whether v names a beta release, as v0.2.0-beta.1.
func isBeta(v string) bool { return install.IsRelease(v) && strings.Contains(v, "-") }

// dataDirs are the folders Skiva keeps the user's library and its
// analyses in, which an uninstall offers to take away too.
func dataDirs() []string {
	var dirs []string
	for _, dir := range []func() (string, error){os.UserConfigDir, os.UserCacheDir} {
		if d, err := dir(); err == nil {
			dirs = append(dirs, filepath.Join(d, dirName))
		}
	}
	return dirs
}

// installed reports whether this is the installed Skiva, which updates
// itself, as a copy run from anywhere else does not.
func installed() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	_, to, err := install.Where(installer())
	return err == nil && samePath(exe, to)
}

// updateMode is how the installed Skiva takes newer releases, as its
// install keeps it.
func updateMode() install.UpdateMode {
	if in, err := install.Find(installer()); err == nil {
		return in.Updates
	}
	return install.UpdatesInstall
}

// updateNews is a newer release gunim's updates found: one to ask about,
// or one put in place already, ready for the next start.
type updateNews struct {
	release install.Release
	ready   bool
}

// news carries what gunim's updates found to the player, which they tell
// on a goroutine of their own, from before the player has started.
var news = make(chan updateNews, 4)

// tell passes n on to the player, or drops it where the player has yet
// to take what came before: the next look tells again.
func tell(n updateNews) {
	select {
	case news <- n:
	default:
	}
}

// quitAsked asks the player to close, as for the restart into an
// update, or for an uninstall.
var quitAsked = make(chan struct{}, 1)

// quitForUpdate asks the player to close, for the restart into an
// update: the update's new copy waits for it, and starts once it has.
func quitForUpdate() error {
	select {
	case quitAsked <- struct{}{}:
	default:
	}
	return nil
}

// quitRunning asks the Skiva running, if one is, to close, as before an
// uninstall: it hands it -quit, as a Skiva started again hands it files.
func quitRunning(context.Context) error {
	dir := settingsDir()
	if dir == "" {
		return errors.New("skiva: no settings folder to find the Skiva running by")
	}
	_, err := handTo(dir, []string{"-quit"})
	return err
}

// updateWindows opens gunim's windows about updates, in the installer's
// look: the update window, what's new, and the window about Skiva, with
// Check for Updates. Nil, as in a test, opens none.
type updateWindows struct {
	ctx context.Context
	app *gunim.App
}

// exe is the program a release replaces: "" for the installed Skiva,
// and its own path for a copy run from elsewhere, which updates itself
// where it is.
func (*updateWindows) exe() string {
	if installed() {
		return ""
	}
	exe, _ := os.Executable()
	return exe
}

func (w *updateWindows) showUpdate(r install.Release, ready bool) error {
	if w == nil {
		return nil
	}
	return install.ShowUpdate(w.ctx, w.app, installer(), install.Update{Release: r, Ready: ready, Exe: w.exe(), Quit: quitForUpdate})
}

func (w *updateWindows) showWhatsNew(from string) error {
	if w == nil {
		return nil
	}
	return install.ShowWhatsNew(w.ctx, w.app, installer(), from)
}

func (w *updateWindows) showAbout() error {
	if w == nil {
		return nil
	}
	return install.ShowAbout(w.ctx, w.app, installer(), install.About{Quit: quitForUpdate, Exe: w.exe()})
}

// samePath reports whether two paths name one file, letter case aside
// where the system ignores it, and through links.
func samePath(a, b string) bool {
	a, b = resolvePath(a), resolvePath(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// resolvePath is path cleaned, with its links resolved where it exists.
func resolvePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// currentSettings is what the settings card shows as the player starts,
// with the library kept as kept.
func currentSettings(kept saved) Settings {
	s := Settings{Version: version, Beta: isBeta(version)}
	if kept.Beta != nil {
		s.Beta = *kept.Beta
	}
	switch {
	case runtime.GOOS == "android":
		s.Updates = UpdatesFromStore
	case installed():
		s.Updates = UpdatesHere
		s.Mode = string(updateMode())
	}
	return s
}

// showing logs a window about updates that did not open.
func (a *app) showing(err error) {
	if err != nil {
		log.Printf("skiva: %v", err)
	}
}
