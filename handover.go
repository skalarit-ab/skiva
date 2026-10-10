package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/skalarit-ab/skiva/internal/single"
)

// Files opened with Skiva, as from the file manager once the installer
// has made Skiva open music files, come as its arguments. One Skiva runs
// for the user: a Skiva started while another runs hands its arguments
// to that one, which comes to the front and plays them, and ends.

// settingsDir is the folder Skiva keeps its library in, where the Skiva
// running says where to hand it files; empty where there is none.
func settingsDir() string {
	if f := stateFile(); f != "" {
		return filepath.Dir(f)
	}
	return ""
}

// handTo hands args to the Skiva running for dir, and reports whether
// one took them.
func handTo(dir string, args []string) (bool, error) {
	cwd, _ := os.Getwd()
	return single.Hand(dir, single.Handover{Args: args, Dir: cwd})
}

// handedOver is what a Skiva started again asks for: the files it names,
// made whole against the folder it started in, or with -quit, for the
// one running to close. Other flags are its own, and left out.
func handedOver(h single.Handover) (files []string, quit bool) {
	for _, arg := range h.Args {
		switch {
		case arg == "-quit" || arg == "--quit":
			quit = true
		case strings.HasPrefix(arg, "-"):
		case filepath.IsAbs(arg) || h.Dir == "":
			files = append(files, arg)
		default:
			files = append(files, filepath.Join(h.Dir, arg))
		}
	}
	return files, quit
}

// openTogether is how soon after one file is opened another counts as
// opened with it. Several files opened at once from the file manager
// come one Skiva each, a moment apart: the first plays, and the rest
// follow it on Up next, in place of each taking the last one's place.
const openTogether = 2 * time.Second

// open plays files opened with Skiva at once, before Up next, or
// follows those opened a moment before with them; see openTogether.
func (a *app) open(paths []string) {
	if len(paths) == 0 {
		return
	}
	at := time.Now()
	together := at.Sub(a.openedAt) < openTogether
	a.openedAt = at
	a.spreadOut(paths, spread{to: QueueList, at: -1, opened: true, together: together})
}

// placeOpened puts files opened with Skiva on Up next, as spread out:
// the first of a new opening at the front, to play at once, and those
// opened with it after the last of them still waiting.
func (a *app) placeOpened(paths []string, together bool) {
	i := 0
	if together {
		if j := slices.Index(a.queue, a.lastOpened); j >= 0 {
			i = j + 1
		}
	} else {
		a.playNow = paths[0]
	}
	a.queue = slices.Insert(a.queue, i, paths...)
	a.lastOpened = paths[len(paths)-1]
}

// playOpened plays the file opened with Skiva last, or dropped on the
// track playing, once it is read.
func (a *app) playOpened() {
	e := a.byKey[a.playNow]
	if a.playNow == "" || e == nil {
		return
	}
	a.playNow = ""
	if i := slices.Index(a.queue, e.key); i >= 0 {
		a.queue = slices.Delete(a.queue, i, i+1)
	}
	a.refresh()
	// A track opened leaves the list where it was, as Up next's do.
	at := a.listAt
	a.start(e.ID)
	a.listAt = at
}
