package main

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/driver"
)

func TestLeaveToReadMusicFollowsTheMusicFolder(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "one.mp3")
	copyTone(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := newApp(ctx, newDeck(audio.NewMixer()), "")
	a.home = home
	a.onGranted(true)
	if !slices.Contains(a.kept.Folders, home) {
		t.Fatalf("folders %v, want the music folder followed", a.kept.Folders)
	}
	deadline := time.After(5 * time.Second)
	for a.byKey[path] == nil {
		select {
		case ch := <-a.changes:
			a.apply(ch)
		case <-deadline:
			t.Fatal("the music folder's track never joined the library")
		}
	}
}

func TestAddAFolderWithNoChooserAsksLeaveForTheMusicFolder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := newApp(ctx, newDeck(audio.NewMixer()), "")
	asked := 0
	a.choose = func(driver.ChooseOptions) ([]string, error) { return nil, driver.ErrNoChooser }
	a.askMusic = func() bool { asked++; return true }
	a.handle(AddFolder{})
	select {
	case ok := <-a.granted:
		if !ok || asked != 1 {
			t.Fatalf("granted %v after %d asks, want leave asked for once", ok, asked)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Add a folder, with no chooser, asked nothing")
	}
}
