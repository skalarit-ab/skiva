package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
}

// savedList is a playlist as kept: its tracks by their files.
type savedList struct {
	ID, Name string
	Paths    []string
}

// stateFile returns where the library is kept, in the user's settings.
func stateFile() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "gunim-music", "library.json")
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
