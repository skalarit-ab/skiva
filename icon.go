package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// iconSizes are the sizes the window's icon is drawn at, for the title
// bar, the taskbar and the switcher.
var iconSizes = []int{16, 24, 32, 48, 64, 128, 256}

// icons returns the player's icon at each of iconSizes.
func icons() []image.Image {
	out := make([]image.Image, len(iconSizes))
	var wg sync.WaitGroup
	for i, n := range iconSizes {
		wg.Go(func() { out[i] = drawIcon(n) })
	}
	wg.Wait()
	return out
}

// writeIcon writes the icon n pixels square to a PNG file, as the
// launcher's icon on a phone; or, to a path ending in .ico, at each of
// iconSizes to a Windows icon file, which the release build makes the
// program's own icon, for Explorer and the shortcuts.
func writeIcon(path string, n int) error {
	var b bytes.Buffer
	if strings.EqualFold(filepath.Ext(path), ".ico") {
		if err := encodeICO(&b, icons()); err != nil {
			return err
		}
	} else if err := png.Encode(&b, drawIcon(n)); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// encodeICO writes imgs, each square and 256 pixels or less, as a
// Windows icon file, each held as a PNG.
func encodeICO(w io.Writer, imgs []image.Image) error {
	pngs := make([][]byte, len(imgs))
	for i, img := range imgs {
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			return err
		}
		pngs[i] = b.Bytes()
	}
	// The header: reserved, 1 for an icon, and how many there are; then
	// an entry for each, and the images after them all.
	head := []any{uint16(0), uint16(1), uint16(len(imgs))}
	at := 6 + 16*len(imgs)
	for i, img := range imgs {
		// A side of 256 is written as 0, all the one byte holds.
		side := uint8(img.Bounds().Dx())
		head = append(head, side, side, uint8(0), uint8(0), uint16(1), uint16(32), uint32(len(pngs[i])), uint32(at))
		at += len(pngs[i])
	}
	for _, v := range head {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	for _, p := range pngs {
		if _, err := w.Write(p); err != nil {
			return err
		}
	}
	return nil
}

// drawIcon draws the icon n pixels square: a record on a tile of
// sunset, ringed by bars, as the player's record is. Each pixel is
// sampled sixteen times, four at the large sizes, so the edges are
// smooth at every size.
func drawIcon(n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	ss := 4
	if n > 64 {
		ss = 2
	}
	for py := range n {
		for px := range n {
			var r, g, b, a float64
			for sy := range ss {
				for sx := range ss {
					x := (float64(px) + (float64(sx)+0.5)/float64(ss)) / float64(n)
					y := (float64(py) + (float64(sy)+0.5)/float64(ss)) / float64(n)
					cr, cg, cb, ca := iconAt(x, y)
					r += cr * ca
					g += cg * ca
					b += cb * ca
					a += ca
				}
			}
			if a == 0 {
				continue
			}
			k := float64(ss * ss)
			// Premultiplied, as image.RGBA holds it.
			img.SetRGBA(px, py, color.RGBA{
				R: uint8(math.Round(r / k * 255)), G: uint8(math.Round(g / k * 255)),
				B: uint8(math.Round(b / k * 255)), A: uint8(math.Round(a / k * 255)),
			})
		}
	}
	return img
}

// iconBars are the lengths of the bars round the record, as a ring of
// the spectrum caught in a moment: long in the bass, at the top left,
// shorter on round.
var iconBars = func() (out [28]float64) {
	for i := range out {
		t := float64(i) / float64(len(out))
		out[i] = 0.03 + 0.05*math.Abs(math.Sin(t*math.Pi*3+0.6))*(1-0.5*t)
	}
	return out
}()

// iconAt is the icon's colour at x, y, in icon units, 0 to 1 across, as
// straight red, green, blue and alpha from 0 to 1.
func iconAt(x, y float64) (r, g, b, a float64) {
	const inset, radius = 0.04, 0.22
	if tileDist(x, y, inset, 1-inset, radius) > 0 {
		return 0, 0, 0, 0
	}
	// Sunset: deep violet at the top, through plum, to a warm coral.
	r, g, b = sunset(y)
	a = 1
	over := func(cr, cg, cb, ca float64) {
		r, g, b = r+(cr-r)*ca, g+(cg-g)*ca, b+(cb-b)*ca
	}
	dx, dy := x-0.5, y-0.5
	d := math.Hypot(dx, dy)
	// The bars round the record, pale cyan.
	const ring = 0.335
	if d > ring && d < ring+0.09 {
		angle := math.Atan2(dy, dx) + math.Pi
		k := len(iconBars)
		pos := angle / (2 * math.Pi) * float64(k)
		i := int(math.Round(pos)) % k
		off := math.Abs(pos-math.Round(pos)) * 2 * math.Pi / float64(k) * d
		if off < 0.011 && d < ring+iconBars[i] {
			over(0.62, 1, 0.94, 0.95)
		}
	}
	// A soft glow about the record.
	if d > 0.3 && d < 0.36 {
		over(1, 0.75, 0.6, 0.18*(1-(d-0.3)/0.06))
	}
	switch {
	case d < 0.022:
		// The hole.
		over(0.08, 0.05, 0.12, 1)
	case d < 0.11:
		// The label, coral to gold.
		t := (y - 0.39) / 0.22
		over(1, 0.42+0.3*t, 0.5-0.15*t, 1)
	case d < 0.3:
		// The disc: near black, with fine grooves and a sheen across.
		over(0.07, 0.06, 0.1, 1)
		if math.Mod(d*48, 1) < 0.18 {
			over(1, 1, 1, 0.05)
		}
		sheen := math.Max(0, 1-math.Abs(dx+dy)/0.12)
		over(1, 1, 1, 0.12*sheen)
	}
	return r, g, b, a
}

// sunset is the tile's colour t of the way down.
func sunset(t float64) (r, g, b float64) {
	stops := [][3]float64{{0.17, 0.07, 0.36}, {0.48, 0.12, 0.45}, {1, 0.48, 0.38}}
	t = math.Max(0, math.Min(t, 1)) * 2
	i := min(int(t), 1)
	f := t - float64(i)
	lerp := func(a, b float64) float64 { return a + (b-a)*f }
	return lerp(stops[i][0], stops[i+1][0]), lerp(stops[i][1], stops[i+1][1]), lerp(stops[i][2], stops[i+1][2])
}

// tileDist is how far x, y lies outside a rounded square from lo to hi
// on both axes, negative inside.
func tileDist(x, y, lo, hi, radius float64) float64 {
	c := (lo + hi) / 2
	h := (hi-lo)/2 - radius
	dx, dy := math.Abs(x-c)-h, math.Abs(y-c)-h
	return math.Hypot(math.Max(dx, 0), math.Max(dy, 0)) + math.Min(math.Max(dx, dy), 0) - radius
}
