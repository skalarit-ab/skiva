package main

import (
	"hash/fnv"
	"image"
	"image/color"
	"math"
	"sort"
)

// palette is the colours a made cover is painted in: the sky from top
// to horizon, the sun from top to bottom, and the grid.
type palette struct {
	skyTop, skyLow, sunTop, sunLow, grid color.NRGBA
}

// palettes are the made covers' colours, one for each demo song, and
// for other covers in turn.
var palettes = []palette{
	{rgb(0x14, 0x0a, 0x2e), rgb(0x6b, 0x1f, 0x7a), rgb(0xff, 0xd3, 0x6e), rgb(0xff, 0x3d, 0x8b), rgb(0xff, 0x5e, 0xc4)},
	{rgb(0x04, 0x1f, 0x24), rgb(0x10, 0x6b, 0x66), rgb(0xd9, 0xff, 0xc4), rgb(0x3f, 0xd0, 0x9e), rgb(0x6c, 0xf2, 0xd2)},
	{rgb(0x05, 0x0b, 0x2b), rgb(0x1b, 0x3c, 0x9e), rgb(0x8c, 0xf6, 0xff), rgb(0xc4, 0x4d, 0xff), rgb(0x3f, 0xb7, 0xff)},
	{rgb(0x2b, 0x14, 0x0e), rgb(0x9c, 0x4a, 0x2a), rgb(0xff, 0xf1, 0xc9), rgb(0xff, 0x9b, 0x54), rgb(0xff, 0xb8, 0x7a)},
	{rgb(0x1a, 0x05, 0x10), rgb(0x8a, 0x16, 0x3a), rgb(0xff, 0xe0, 0xa6), rgb(0xff, 0x55, 0x55), rgb(0xff, 0x7a, 0x7a)},
}

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: 0xff} }

// paletteFor returns the palette for a cover named name.
func paletteFor(name string) palette {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return palettes[h.Sum32()%uint32(len(palettes))]
}

// makeCover paints a cover size pixels square: a sun setting in
// stripes behind a grid running to the horizon. seed moves the sun.
func makeCover(size int, p palette, seed int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	horizon := 0.66 * s
	sunX := s * (0.5 + 0.08*math.Sin(float64(seed)*2.1))
	sunY := horizon - 0.02*s
	sunR := 0.3 * s
	for y := range size {
		for x := range size {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			var c color.NRGBA
			if fy < horizon {
				c = mixRGB(p.skyTop, p.skyLow, smooth01(fy/horizon))
				// The sun, in stripes that widen toward the horizon.
				d := math.Hypot(fx-sunX, fy-sunY)
				if d < sunR {
					v := (fy - (sunY - sunR)) / sunR
					// Stripes cut the sun's lower half, wider toward the
					// horizon.
					stripe := v > 0.5 && math.Mod((fy-sunY)/(0.04*s)+10, 1) < 0.1+0.9*(v-0.5)
					if !stripe {
						sun := mixRGB(p.sunTop, p.sunLow, min(1, v/1.4))
						// A soft edge.
						c = mixRGB(c, sun, min(1, (sunR-d)/1.5))
					}
				} else if d < sunR*1.6 {
					// A glow round it.
					fall := 1 - (d-sunR)/(0.6*sunR)
					c = mixRGB(c, p.sunLow, 0.25*fall*fall)
				}
			} else {
				// The floor: dark, with a grid in perspective. A point
				// depth of the way down the floor is 1/depth away, and
				// its lines keep a steady width on screen.
				depth := (fy - horizon) / (s - horizon)
				c = mixRGB(p.skyTop, p.skyLow, 0.2*depth)
				z := 1 / depth
				wx := (fx - s/2) / (depth * s)
				const rows, cols = 0.9, 5.0
				rowDist := math.Abs(math.Mod(z*rows+0.5, 1)-0.5) / (rows / (depth * depth * (s - horizon)))
				colDist := math.Abs(math.Mod(wx*cols+100.5, 1)-0.5) / (cols / (depth * s))
				if cover := 1.2 - min(rowDist, colDist); cover > 0 {
					c = mixRGB(c, p.grid, min(1, cover)*(0.3+0.6*depth))
				}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func smooth01(x float64) float64 {
	x = max(0, min(1, x))
	return x * x * (3 - 2*x)
}

// mixRGB blends a toward b by t.
func mixRGB(a, b color.NRGBA, t float64) color.NRGBA {
	t = max(0, min(1, t))
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: 0xff}
}

// accents picks two colours from a cover for the player to light up
// in: the most vivid, made bright enough to read on a dark window, and
// another vivid one of a different hue, for the background's glow.
func accents(img image.Image) (accent, glow color.NRGBA) {
	b := img.Bounds()
	type sample struct {
		c       color.NRGBA
		h, s, v float64
	}
	const grid = 24
	all := make([]sample, 0, grid*grid)
	for gy := range grid {
		for gx := range grid {
			x := b.Min.X + (2*gx+1)*b.Dx()/(2*grid)
			y := b.Min.Y + (2*gy+1)*b.Dy()/(2*grid)
			c, _ := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			h, s, v := hsv(c)
			all = append(all, sample{c, h, s, v})
		}
	}
	// Vivid and not dark counts most.
	sort.Slice(all, func(i, j int) bool { return all[i].s*all[i].v > all[j].s*all[j].v })
	best := all[0]
	other := best
	for _, s := range all[1:] {
		if hueGap(s.h, best.h) > 0.12 && s.s*s.v > 0.15 {
			other = s
			break
		}
	}
	return lifted(best.h, best.s, 0.95), lifted(other.h, other.s, 0.8)
}

// lifted is the colour of hue h at saturation s, at least as bright as
// v, and saturated enough to tell.
func lifted(h, s, v float64) color.NRGBA {
	return fromHSV(h, max(0.45, min(s, 0.8)), v)
}

func hueGap(a, b float64) float64 {
	d := math.Abs(a - b)
	return math.Min(d, 1-d)
}

// hsv returns c's hue, saturation and value, each 0 to 1.
func hsv(c color.NRGBA) (h, s, v float64) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	v = hi
	if hi == 0 {
		return 0, 0, 0
	}
	s = (hi - lo) / hi
	if hi == lo {
		return 0, s, v
	}
	switch hi {
	case r:
		h = (g - b) / (hi - lo)
	case g:
		h = 2 + (b-r)/(hi-lo)
	default:
		h = 4 + (r-g)/(hi-lo)
	}
	h /= 6
	if h < 0 {
		h++
	}
	return h, s, v
}

// fromHSV returns the colour of hue h, saturation s and value v.
func fromHSV(h, s, v float64) color.NRGBA {
	h = math.Mod(h, 1) * 6
	i := math.Floor(h)
	f := h - i
	p, q, t := v*(1-s), v*(1-s*f), v*(1-s*(1-f))
	var r, g, b float64
	switch int(i) {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return color.NRGBA{R: uint8(r*255 + 0.5), G: uint8(g*255 + 0.5), B: uint8(b*255 + 0.5), A: 0xff}
}
