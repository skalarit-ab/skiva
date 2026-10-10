package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/marrasen/gunim/driver"
)

// saved is the library as it is kept between runs: the folders it
// follows, the files added one by one, the playlists, and the player's
// settings.
type saved struct {
	Folders   []string
	Files     []string
	Playlists []savedList
	// Volume is nil until the player has run once.
	Volume  *float32 `json:",omitempty"`
	Shuffle bool
	Repeat  Repeat
	// Last is the track played last, by its key, and LastFrom the list
	// it played from, to take up as the player starts.
	Last     string `json:",omitempty"`
	LastFrom ListID `json:",omitempty"`
	// Played is the tracks played before Last, by their keys, the
	// last last.
	Played []string `json:",omitempty"`
	// GainMode is nil until it is first set: album gain.
	GainMode *GainMode `json:",omitempty"`
	// EQ is nil until the equalizer is first set.
	EQ *EQ `json:",omitempty"`
	// Window is where the window was as it last closed, and how big;
	// nil until it has closed once.
	Window *driver.Placement `json:",omitempty"`
	// Beta says updates take beta releases too; nil until it is first
	// set, when they do for a beta.
	Beta *bool `json:",omitempty"`
}

// savedList is a playlist as kept: its tracks by their files.
type savedList struct {
	ID, Name string
	Paths    []string
}

// placement is where the window was as it last closed, kept in file,
// or nil where it has not closed yet. The window opens there, made safe
// for the monitors attached now; see [gunim.WindowOptions.Place].
func placement(file string) *driver.Placement {
	s, _ := loadSaved(file)
	return s.Window
}

// stateFile returns where the library is kept, in the user's settings.
func stateFile() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, dirName, "library.json")
}

// dirName names the folders Skiva keeps its library in, in the user's
// settings, and its analyses in, in the user's cache.
const dirName = "skiva"

// moveSettings moves the library and the analyses to dirName from
// where the player kept them while it was gunim's music example, the
// first time it runs under its own name.
func moveSettings() {
	for _, dir := range []func() (string, error){os.UserConfigDir, os.UserCacheDir} {
		d, err := dir()
		if err != nil {
			continue
		}
		old, now := filepath.Join(d, "gunim-music"), filepath.Join(d, dirName)
		if _, err := os.Stat(now); err == nil {
			continue
		}
		if _, err := os.Stat(old); err == nil {
			if err := os.Rename(old, now); err != nil {
				log.Printf("skiva: moving the settings from %s: %v", old, err)
			}
		}
	}
}

// loadSaved reads the library kept in file, and reports whether there
// was one.
func loadSaved(file string) (saved, bool) {
	var s saved
	if file == "" {
		return s, false
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return s, false
	}
	if json.Unmarshal(b, &s) != nil {
		return saved{}, false
	}
	return s, true
}

// write keeps s in file, whole or not at all.
func (s saved) write(file string) error {
	if file == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "\t")
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
