// Command trayicon draws the notification-area icons into internal/assets/tray.
//
// The application icon is too detailed to survive the tray's 16-48 pixels, so
// the tray gets the same tile redrawn per size: fewer, thicker parts whose
// edges sit on whole pixels.
//
//	go run ./tools/trayicon
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/vector"
)

// layout is the geometry of one size in whole pixels. Unit is the thickness
// of an arrow shaft and the height of a key.
type layout struct {
	size, border int
	unit         int // shaft thickness and key height
	head         int // how far an arrowhead reaches past its shaft, per side
	headLength   int
	shaftGap     int // between the two shafts
	keyboardGap  int // between the lower arrowhead and the keys
	rowGap       int
	keys         int // in the upper row
	keyWidth     int
	keyGap       int
	inset        int // of the lower row on each side
}

var layouts = []layout{
	{size: 16, border: 1, unit: 2, head: 1, headLength: 3, shaftGap: 2, keyboardGap: 1, rowGap: 1, keys: 3, keyWidth: 3, keyGap: 1, inset: 1},
	{size: 20, border: 1, unit: 2, head: 2, headLength: 4, shaftGap: 2, keyboardGap: 2, rowGap: 1, keys: 4, keyWidth: 3, keyGap: 1, inset: 1},
	{size: 24, border: 1, unit: 3, head: 2, headLength: 5, shaftGap: 2, keyboardGap: 2, rowGap: 1, keys: 4, keyWidth: 4, keyGap: 1, inset: 2},
	{size: 28, border: 1, unit: 3, head: 2, headLength: 5, shaftGap: 3, keyboardGap: 2, rowGap: 1, keys: 5, keyWidth: 4, keyGap: 1, inset: 2},
	{size: 32, border: 1, unit: 3, head: 3, headLength: 6, shaftGap: 3, keyboardGap: 2, rowGap: 1, keys: 5, keyWidth: 4, keyGap: 1, inset: 2},
	{size: 40, border: 2, unit: 4, head: 3, headLength: 7, shaftGap: 4, keyboardGap: 3, rowGap: 2, keys: 5, keyWidth: 5, keyGap: 2, inset: 3},
	{size: 48, border: 2, unit: 5, head: 4, headLength: 9, shaftGap: 5, keyboardGap: 3, rowGap: 2, keys: 5, keyWidth: 6, keyGap: 2, inset: 4},
}

// Colors sampled from the application icon.
var (
	fillTop    = rgb(0x1E, 0x23, 0x33)
	fillBottom = rgb(0x12, 0x12, 0x1C)
	// rim corners: top left, top right, bottom left, bottom right
	rim       = [4]color.NRGBA{rgb(0x8C, 0x9C, 0xC8), rgb(0x2E, 0x8A, 0xF5), rgb(0x8A, 0x2B, 0xE2), rgb(0x4A, 0x4A, 0xE0)}
	rightFrom = rgb(0x26, 0x86, 0xFB)
	rightTo   = rgb(0x10, 0xCC, 0xFC)
	leftFrom  = rgb(0xEA, 0x5E, 0xFD)
	leftTo    = rgb(0x4A, 0x46, 0xF9)
	keyColor  = rgb(0xDC, 0xE0, 0xEA)
)

func main() {
	out := flag.String("out", filepath.Join("internal", "assets", "tray"), "output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, l := range layouts {
		name := filepath.Join(*out, fmt.Sprintf("%d.png", l.size))
		if err := write(name, l.draw()); err != nil {
			log.Fatal(err)
		}
	}
}

func write(name string, img image.Image) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (l layout) draw() *image.NRGBA {
	s := float32(l.size)
	dst := image.NewNRGBA(image.Rect(0, 0, l.size, l.size))
	radius := s * 0.24
	b := float32(l.border)

	paint(dst, corners(l.size), roundRect(l.size, 0, 0, s, s, radius))
	paint(dst, vertical(fillTop, fillBottom, l.size), roundRect(l.size, b, b, s-b, s-b, radius-b))

	// Content: the keyboard sets the width, the stack of parts the height.
	width := l.keys*l.keyWidth + (l.keys-1)*l.keyGap
	height := l.head + l.unit + l.shaftGap + l.unit + l.head + l.keyboardGap + l.unit + l.rowGap + l.unit
	x0 := float32((l.size - width) / 2)
	y0 := float32((l.size - height + 1) / 2)
	w := float32(width)
	u := float32(l.unit)

	// The right arrow on top, the left arrow under it; each spans three
	// quarters of the width.
	span := float32(math.Round(float64(w) * 0.74))
	top := y0 + float32(l.head)
	paint(dst, horizontal(rightFrom, rightTo, x0+w-span, x0+w), arrow(l.size, x0+w-span, x0+w, top, u, float32(l.head), float32(l.headLength), true))
	bottom := top + u + float32(l.shaftGap)
	paint(dst, horizontal(leftFrom, leftTo, x0, x0+span), arrow(l.size, x0, x0+span, bottom, u, float32(l.head), float32(l.headLength), false))

	keyRadius := u * 0.25
	row := bottom + u + float32(l.head) + float32(l.keyboardGap)
	keys := &vector.Rasterizer{}
	keys.Reset(l.size, l.size)
	for i := range l.keys {
		x := x0 + float32(i*(l.keyWidth+l.keyGap))
		addRoundRect(keys, x, row, x+float32(l.keyWidth), row+u, keyRadius)
	}
	row += u + float32(l.rowGap)
	kw := float32(l.keyWidth)
	left, right := x0+float32(l.inset), x0+w-float32(l.inset)
	gap := float32(l.keyGap)
	addRoundRect(keys, left, row, left+kw, row+u, keyRadius)
	addRoundRect(keys, left+kw+gap, row, right-kw-gap, row+u, keyRadius)
	addRoundRect(keys, right-kw, row, right, row+u, keyRadius)
	paint(dst, image.NewUniform(keyColor), keys)
	return dst
}

// arrow is a shaft with a rounded tail from x0 to x1 whose head points right
// or left.
func arrow(size int, x0, x1, y, thickness, head, length float32, right bool) *vector.Rasterizer {
	r := &vector.Rasterizer{}
	r.Reset(size, size)
	mid := y + thickness/2
	half := thickness/2 + head
	if right {
		base := x1 - length
		addRoundRect(r, x0, y, base+0.5, y+thickness, thickness/2)
		r.MoveTo(base, mid-half)
		r.LineTo(x1, mid)
		r.LineTo(base, mid+half)
		r.ClosePath()
	} else {
		base := x0 + length
		addRoundRect(r, base-0.5, y, x1, y+thickness, thickness/2)
		r.MoveTo(base, mid-half)
		r.LineTo(base, mid+half)
		r.LineTo(x0, mid)
		r.ClosePath()
	}
	return r
}

func roundRect(size int, x0, y0, x1, y1, radius float32) *vector.Rasterizer {
	r := &vector.Rasterizer{}
	r.Reset(size, size)
	addRoundRect(r, x0, y0, x1, y1, radius)
	return r
}

func addRoundRect(r *vector.Rasterizer, x0, y0, x1, y1, radius float32) {
	radius = min(radius, (x1-x0)/2, (y1-y0)/2)
	// A cubic with control points at 0.552 of the radius follows a quarter
	// circle closely.
	k := radius * (1 - 0.5523)
	r.MoveTo(x0+radius, y0)
	r.LineTo(x1-radius, y0)
	r.CubeTo(x1-k, y0, x1, y0+k, x1, y0+radius)
	r.LineTo(x1, y1-radius)
	r.CubeTo(x1, y1-k, x1-k, y1, x1-radius, y1)
	r.LineTo(x0+radius, y1)
	r.CubeTo(x0+k, y1, x0, y1-k, x0, y1-radius)
	r.LineTo(x0, y0+radius)
	r.CubeTo(x0, y0+k, x0+k, y0, x0+radius, y0)
	r.ClosePath()
}

// paint fills the rasterized shape with src over dst.
func paint(dst *image.NRGBA, src image.Image, shape *vector.Rasterizer) {
	mask := image.NewAlpha(shape.Bounds())
	shape.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	draw.DrawMask(dst, mask.Bounds(), src, mask.Bounds().Min, mask, mask.Bounds().Min, draw.Over)
}

// gradient is a color that depends on the position.
type gradient func(x, y float32) color.NRGBA

func (g gradient) ColorModel() color.Model { return color.NRGBAModel }

func (g gradient) Bounds() image.Rectangle {
	return image.Rect(-1<<20, -1<<20, 1<<20, 1<<20)
}

func (g gradient) At(x, y int) color.Color { return g(float32(x)+0.5, float32(y)+0.5) }

func horizontal(from, to color.NRGBA, x0, x1 float32) gradient {
	return func(x, _ float32) color.NRGBA { return mix(from, to, (x-x0)/(x1-x0)) }
}

func vertical(from, to color.NRGBA, size int) gradient {
	return func(_, y float32) color.NRGBA { return mix(from, to, y/float32(size)) }
}

func corners(size int) gradient {
	return func(x, y float32) color.NRGBA {
		tx, ty := x/float32(size), y/float32(size)
		return mix(mix(rim[0], rim[1], tx), mix(rim[2], rim[3], tx), ty)
	}
}

func mix(a, b color.NRGBA, t float32) color.NRGBA {
	t = min(max(t, 0), 1)
	f := func(a, b uint8) uint8 { return uint8(float32(a) + (float32(b)-float32(a))*t + 0.5) }
	return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), f(a.A, b.A)}
}

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{r, g, b, 0xFF} }
