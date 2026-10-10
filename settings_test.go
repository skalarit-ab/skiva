package main

import (
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
)

// The installed Skiva's settings set how updates come, and whether
// betas come too, and Check for updates opens the window about Skiva.
func TestTheSettingsSetHowUpdatesCome(t *testing.T) {
	s := library4()
	s.Settings = Settings{Version: "v0.2.0", Updates: UpdatesHere, Mode: "install"}
	w, root, run := stage(t, geom.Sz(1100, 720), s)
	withUI(t, w, run, func(u *gunim.UI) { root.settings.show(true, u) })
	run(40)
	middle := func(n gunim.Node, of, at int) geom.Point {
		b := boundsOf(t, w, run, n)
		return geom.Pt(b.Min.X+b.Size().W*(float32(at)+0.5)/float32(of), b.Min.Y+b.Size().H/2)
	}
	tap(w, run, middle(root.settings.mode, 3, 1))
	tap(w, run, middle(root.settings.beta, 2, 1))
	tap(w, run, middle(root.settings.check, 1, 0))
	got := intents(w)
	want := []gunim.Intent{SetUpdateMode{Mode: "notify"}, SetBeta{On: true}, ShowAbout{}}
	if len(got) != len(want) {
		t.Fatalf("the settings sent %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the settings sent %v, want %v", got, want)
		}
	}
	if !root.settings.shown() {
		t.Fatal("a press on the settings put them away")
	}
}

// On a phone the settings say the app store keeps Skiva up to date, and
// offer no update settings.
func TestOnAPhoneTheStoreUpdatesSkiva(t *testing.T) {
	s := library4()
	s.Settings = Settings{Updates: UpdatesFromStore}
	w, root, run := stage(t, geom.Sz(400, 820), s)
	withUI(t, w, run, func(u *gunim.UI) { root.settings.show(true, u) })
	run(40)
	if b := boundsOf(t, w, run, root.settings.mode); b.Size().W > 0 && b.Min.X > -1000 {
		t.Fatalf("the update modes are at %v on a phone, want them out of sight", b)
	}
	if note := root.settings.note(); note != "Google Play keeps Skiva up to date." {
		t.Fatalf("the settings say %q", note)
	}
}
