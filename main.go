// Command music is a music player, to show what gunim's audio and
// animation do together.
//
// The track playing is a picture disc that spins while it plays and
// runs down slowly as it pauses, ringed by bars that move with the
// music, pitch by pitch. Lights in the cover's colours drift behind
// everything, swelling with the bass; a new track's colours flow
// through the whole window. The seek bar is the track itself, drawn as
// its loudness along it.
//
//	go run ./example/music
//	go run ./example/music -dir ~/Music
//
// The player always has four songs made in code, so it has something
// to play anywhere. The library follows folders of MP3, FLAC, Ogg
// Vorbis and WAV files, with their tags and covers, as files come and
// go: the user's music folder from the first run, folders added from
// the library, and the one -dir names. Files can be added one by one,
// and gathered into playlists. The library is kept in the user's
// settings, or in the file -state names.
//
// The equalizer is parametric: up to eight bands, each a bell, a shelf,
// a cut or a notch, dragged about a graph with the sound's spectrum
// before and after it behind them.
//
// Keys: Space plays and pauses, Left and Right seek, Up and Down set
// the volume, N and P skip, S shuffles, R repeats, E opens the
// equalizer, and I the track's card.
//
// Loudness gain evens tracks out as ReplayGain 2 does: each track's
// loudness is measured as ITU-R BS.1770 defines it, and the track, or
// its album played in order, plays at -18 LUFS. Its button steps
// between no gain, track gain and album gain.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/audio/speaker"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/widget"
)

func main() {
	dir := flag.String("dir", "", "a folder of music for the library to follow")
	state := flag.String("state", stateFile(), "the file the library is kept in; empty keeps nothing")
	play := flag.Bool("play", false, "start playing as the window opens")
	track := flag.Int("track", 1, "the track -play starts with, counted from 1")
	at := flag.Duration("at", 0, "how far into the track -play starts")
	runFor := flag.Duration("for", 0, "quit after this long; zero runs until the window closes")
	shot := flag.String("shot", "", "write the window to this PNG file after -after, and quit")
	after := flag.Duration("after", 2*time.Second, "how long -shot waits")
	size := flag.String("size", "1100x720", "the window's size, as 400x820 for one shaped like a phone; without it the window opens where it last closed")
	library := flag.Bool("library", false, "open with the library over the track playing, on a narrow window")
	eqOpen := flag.Bool("eq", false, "open with the equalizer showing, for -shot")
	infoOpen := flag.Bool("info", false, "open with the track's card showing, for -shot")
	iconOut := flag.String("write-icon", "", "write the icon, 512 pixels square, to this PNG file, and quit")
	list := flag.String("list", "", "open the library on the list of this name, as a playlist's, for -shot")
	flag.Parse()
	if *iconOut != "" {
		if err := writeIcon(*iconOut, 512); err != nil {
			log.Fatal(err)
		}
		return
	}
	var w, h float32
	if _, err := fmt.Sscanf(*size, "%gx%g", &w, &h); err != nil || w <= 0 || h <= 0 {
		log.Fatalf("music: -size %q: want a width and a height, as 400x820", *size)
	}
	start := startAt{on: *play, track: *track, at: *at}
	lib := setup{file: *state, dir: *dir}
	// The window opens where it last closed, unless asked for a size,
	// or for a shot, which wants the same window every time.
	var place *driver.Placement
	if !flagSet("size") && *shot == "" {
		place = placement(*state)
	}
	if err := run(lib, start, *library, *list, *eqOpen, *infoOpen, *runFor, *shot, *after, geom.Sz(w, h), place); err != nil {
		log.Fatal(err)
	}
}

// startAt says what to play as the window opens, if anything.
type startAt struct {
	on    bool
	track int
	at    time.Duration
}

// flagSet reports whether the flag of this name was given.
func flagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

func run(at setup, play startAt, library bool, list string, eqOpen, infoOpen bool, runFor time.Duration, shot string, after time.Duration, size geom.Size, place *driver.Placement) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if runFor > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, runFor)
		defer cancel()
	}
	mix := audio.NewMixer()
	d := newDeck(mix)
	// The player works a little further ahead than a game would: its
	// buttons still answer at once, and the music rides out a busy
	// moment.
	if spk, err := speaker.Open(mix, speaker.Options{Name: "gunim music", Latency: seenLatency}); err != nil {
		log.Printf("music: no sound: %v", err)
	} else {
		d.setSpeaker(spk)
	}
	err := gunim.Main(ctx, func(a *gunim.App) error {
		// The player draws the whole window, its title bar over it, as
		// it draws under a phone's status bar. The bar says Music; the
		// window's title, which the taskbar and Alt+Tab show, names the
		// track as well.
		bar := widget.NewTitleBar("")
		bar.Name, bar.TitleAtStart = appName, true
		w, err := a.NewWindow(gunim.WindowOptions{
			Title: appName, Size: size, Place: place, Icons: icons(), AskToClose: CloseAsked{},
			TitleBar: bar, UnderTitleBar: true,
		})
		if err != nil {
			return fmt.Errorf("music: %w", err)
		}
		registerViews(w, d, library, list, eqOpen, infoOpen)
		// The user's music folder, as the system names it, and leave to
		// read it, which a phone asks the user for.
		at.home = a.UserFolder(driver.FolderMusic)
		at.permitted = func() bool { return a.Permitted(driver.PermissionMusic) }
		at.ask = func() bool { return a.Ask(driver.PermissionMusic) }
		at.placement = w.Placement
		c := w.Client()
		if shot != "" {
			go func() {
				select {
				case <-time.After(after):
				case <-ctx.Done():
					return
				}
				if err := writeShot(ctx, c, shot); err != nil {
					log.Print(err)
				}
				c.Close()
			}()
		}
		return serve(ctx, c, d, at, play, a.SetNowPlaying)
	})
	// The music fades out after the window has gone.
	d.quiet(2 * closeFade)
	if errors.Is(err, driver.ErrNoDriver) {
		log.Print("gunim has no driver for this operating system yet")
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// writeShot writes what the window shows to a PNG file.
func writeShot(ctx context.Context, c gunim.Client, path string) error {
	img, err := c.Shot(ctx)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return errors.Join(png.Encode(f, img), f.Close())
}
