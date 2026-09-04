// Command icongen draws the tray icon: two arrows swapping places on a rounded
// square. Kept as code so it can be re-rendered at any size and reviewed; run
// "make icon" after changing it.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

// Geometry of the drawing, in the coordinates of the finished square icon.
const (
	size         = 512 // side of the rendered icon, in pixels
	supersample  = 4   // draw this many times larger, then average down
	cornerRadius = 112

	shaftThickness = 34
	headLength     = 70
	headThickness  = 88
	glyphLeft      = 122
	glyphRight     = 390
	glyphGap       = 44 // vertical distance between the two arrow centers
)

// Colors of the icon.
var (
	background = color.NRGBA{R: 0x2B, G: 0x5C, B: 0xE6, A: 0xFF} // blue
	foreground = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // white
)

func main() {
	outputDir := flag.String("out", filepath.Join("internal", "assets"), "directory to write the icon into")
	flag.Parse()

	icon := draw()

	var rendered bytes.Buffer
	if err := png.Encode(&rendered, icon); err != nil {
		log.Fatalf("icongen: cannot encode the icon: %v", err)
	}

	if err := os.MkdirAll(*outputDir, 0o755); err != nil { //nolint:gosec // generated assets are committed and world-readable
		log.Fatalf("icongen: %v", err)
	}
	write(filepath.Join(*outputDir, "icon.png"), rendered.Bytes())
	write(filepath.Join(*outputDir, "icon.ico"), icoFromPNG(rendered.Bytes(), size))
}

func write(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // generated assets are committed and world-readable
		log.Fatalf("icongen: cannot write %s: %v", path, err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", path, len(data))
}

// draw renders the icon oversampled and averages it down for smooth edges.
func draw() image.Image {
	large := image.NewNRGBA(image.Rect(0, 0, size*supersample, size*supersample))
	for y := range size * supersample {
		for x := range size * supersample {
			ix := (float64(x) + 0.5) / supersample
			iy := (float64(y) + 0.5) / supersample

			switch {
			case inArrows(ix, iy):
				large.SetNRGBA(x, y, foreground)
			case inRoundedSquare(ix, iy):
				large.SetNRGBA(x, y, background)
			}
		}
	}
	return downsample(large, size)
}

// inRoundedSquare reports whether a point lies on the icon's background shape.
func inRoundedSquare(x, y float64) bool {
	const radius = cornerRadius
	// Fold the point into the top-left quarter.
	dx := min(x, size-x)
	dy := min(y, size-y)
	if dx < 0 || dy < 0 {
		return false
	}
	if dx >= radius || dy >= radius {
		return true
	}
	ox, oy := radius-dx, radius-dy
	return ox*ox+oy*oy <= radius*radius
}

// inArrows reports whether a point lies on one of the two arrows: the upper one
// points right, the lower one points left.
func inArrows(x, y float64) bool {
	upper := float64(size)/2 - glyphGap/2 - shaftThickness/2
	lower := float64(size)/2 + glyphGap/2 + shaftThickness/2
	return inArrow(x, y, upper, true) || inArrow(x, y, lower, false)
}

// inArrow reports whether a point lies on one arrow, drawn along the horizontal
// line y = center and pointing right when pointsRight is set.
func inArrow(x, y, center float64, pointsRight bool) bool {
	if !pointsRight {
		x = size - x
	}
	const tip = float64(glyphRight)
	const base = tip - headLength

	switch {
	case x < glyphLeft || x > tip:
		return false
	case x <= base:
		return abs(y-center) <= shaftThickness/2
	default:
		remaining := (tip - x) / headLength
		return abs(y-center) <= remaining*headThickness/2
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func downsample(large *image.NRGBA, target int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, target, target))
	for y := range target {
		for x := range target {
			var r, g, b, a int
			for sy := range supersample {
				for sx := range supersample {
					c := large.NRGBAAt(x*supersample+sx, y*supersample+sy)
					// Weighting by alpha keeps the edges from blending into black.
					r += int(c.R) * int(c.A) / 255
					g += int(c.G) * int(c.A) / 255
					b += int(c.B) * int(c.A) / 255
					a += int(c.A)
				}
			}
			samples := supersample * supersample
			if a == 0 {
				continue
			}
			out.SetNRGBA(x, y, color.NRGBA{
				R: channel(r * 255 / a),
				G: channel(g * 255 / a),
				B: channel(b * 255 / a),
				A: channel(a / samples),
			})
		}
	}
	return out
}

// channel clamps an accumulated color value into one byte.
func channel(value int) uint8 {
	return uint8(min(max(value, 0), 255)) //nolint:gosec // clamped on the line itself
}

// icoFromPNG wraps a PNG in the Windows icon container, which has accepted
// PNG entries since Vista.
func icoFromPNG(pngData []byte, dimension int) []byte {
	// The format writes 256 or more as zero.
	side := byte(0)
	if dimension < 256 {
		side = channel(dimension)
	}

	var out bytes.Buffer
	write := func(value any) { _ = binary.Write(&out, binary.LittleEndian, value) }

	// ICONDIR: reserved, type 1 (icon), one image.
	write([3]uint16{0, 1, 1})
	// ICONDIRENTRY.
	out.Write([]byte{side, side, 0, 0})
	write(uint16(1))  // color planes
	write(uint16(32)) // bits per pixel
	//nolint:gosec // a PNG of this icon is a few kilobytes, never 4 GB
	write(uint32(len(pngData))) // size of the image data
	write(uint32(6 + 16))       // offset of the image data
	out.Write(pngData)
	return out.Bytes()
}
