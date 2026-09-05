package ui

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2/canvas"
)

// A superellipse with this exponent is close to the continuous corner macOS
// draws. Its corners look tighter than circular ones of the same radius, so
// the radius is scaled up to match.
const (
	cornerExponent = 4.0
	cornerScale    = 1.25
	shadowOpacity  = 0.10
)

// shape is a rectangle with continuous corners, drawn pixel by pixel because
// the toolkit only knows circular ones. It is meant to sit behind a control.
// Renderers must hand the toolkit the embedded Raster, the only type it paints.
type shape struct {
	*canvas.Raster
	radius float32
	fill   color.NRGBA
	border color.NRGBA
	shadow bool
}

func newShape(radius float32) *shape {
	s := &shape{radius: radius}
	s.Raster = canvas.NewRaster(s.draw)
	return s
}

// set changes the colors and redraws.
func (s *shape) set(fill, border color.Color, shadow bool) {
	s.fill, s.border, s.shadow = nrgba(fill), nrgba(border), shadow
	s.Refresh()
}

func (s *shape) draw(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w == 0 || h == 0 {
		return img
	}
	scale := float32(1)
	if size := s.Size(); size.Width > 0 {
		scale = float32(w) / size.Width
	}
	width, height := float32(w), float32(h)
	radius := min(s.radius*cornerScale*scale, width/2, height/2)
	borderWidth := max(1, scale/2)
	shadowOffset, shadowBlur := scale, 3*scale
	black := color.NRGBA{A: 0xFF}

	for y := range h {
		for x := range w {
			px, py := float32(x)+0.5, float32(y)+0.5
			d := cornerDistance(px, py, width, height, radius)
			var p pixel
			if s.shadow {
				if ds := cornerDistance(px, py-shadowOffset, width, height, radius); ds > 0 {
					p = p.over(black, shadowOpacity*clamp(1-ds/shadowBlur))
				}
			}
			p = p.over(s.fill, clamp(0.5-d))
			p = p.over(s.border, clamp(0.5-d)*clamp(d+borderWidth+0.5))
			img.SetNRGBA(x, y, p.nrgba())
		}
	}
	return img
}

// cornerDistance is the signed distance from a point to the edge of a w by h
// rectangle whose corners are superellipses of the given radius; negative
// inside.
func cornerDistance(x, y, w, h, r float32) float32 {
	qx := abs(x-w/2) - (w/2 - r)
	qy := abs(y-h/2) - (h/2 - r)
	if qx > 0 && qy > 0 {
		sum := math.Pow(float64(qx), cornerExponent) + math.Pow(float64(qy), cornerExponent)
		return float32(math.Pow(sum, 1/cornerExponent)) - r
	}
	return max(qx, qy) - r
}

// pixel accumulates one pixel with straight alpha, every channel in 0..1.
type pixel struct{ r, g, b, a float32 }

// over composites a color on top with the given coverage.
func (p pixel) over(c color.NRGBA, coverage float32) pixel {
	sa := float32(c.A) / 255 * coverage
	if sa <= 0 {
		return p
	}
	a := sa + p.a*(1-sa)
	src, dst := sa/a, p.a*(1-sa)/a
	return pixel{
		r: float32(c.R)/255*src + p.r*dst,
		g: float32(c.G)/255*src + p.g*dst,
		b: float32(c.B)/255*src + p.b*dst,
		a: a,
	}
}

func (p pixel) nrgba() color.NRGBA {
	return color.NRGBA{
		R: uint8(p.r*255 + 0.5),
		G: uint8(p.g*255 + 0.5),
		B: uint8(p.b*255 + 0.5),
		A: uint8(p.a*255 + 0.5),
	}
}

func clamp(v float32) float32 { return min(max(v, 0), 1) }

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
