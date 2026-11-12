package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/zakonnic/kbswitcher/internal/assets"
	"github.com/zakonnic/kbswitcher/internal/platform/window"
)

const (
	bodyPadding = 20
	bodyGap     = 16
)

// chrome is what a title bar would otherwise provide: a border, rounded
// corners, a shadow and moving the window by dragging it. As the layout of a
// frameless window it keeps the content inside the border the compositor
// draws over the edge of the window.
type chrome struct {
	app    *App
	window fyne.Window
	width  float32
	shown  bool
	// border is the width of the compositor's border in pixels.
	border int
	// unframed is set once framing failed and was logged.
	unframed bool
}

// newWindow opens a window that is frameless where the platform allows it.
func (a *App) newWindow(title string) *chrome {
	c := &chrome{app: a}
	if d, ok := a.fyne.Driver().(desktop.Driver); ok && window.Frameless {
		c.window = d.CreateSplashWindow()
		c.window.SetTitle(title)
	} else {
		c.window = a.fyne.NewWindow(title)
		c.window.SetPadded(false)
	}
	return c
}

// setContent puts content inside the frame and sizes the window to it.
func (c *chrome) setContent(content fyne.CanvasObject, width float32) {
	if window.Frameless {
		content = container.New(c, newDragArea(c.window), content)
	}
	c.width = width
	c.window.SetContent(content)
	c.window.Resize(fyne.NewSize(width, 0))
}

// show brings the window up where the user left it, centered the first time.
func (c *chrome) show() {
	if !c.shown {
		c.window.CenterOnScreen()
		c.shown = true
	}
	c.window.Show()
	c.window.RequestFocus()
	if !window.Frameless {
		return
	}
	// The border scales with the monitor, which may have changed since.
	border, err := window.Frame(nativeHandle(c.window), theme.Color(colorNameFrame))
	if err != nil && !c.unframed {
		c.app.log.Warn("could not frame the window", "error", err)
		c.unframed = true
	}
	if border != c.border {
		c.border = border
		c.window.Content().Refresh()
		c.window.Resize(fyne.NewSize(c.width, 0))
	}
}

func (c *chrome) inset() float32 { return float32(c.border) / c.window.Canvas().Scale() }

// MinSize is the content's plus the border.
func (c *chrome) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return objects[1].MinSize().AddWidthHeight(2*c.inset(), 2*c.inset())
}

// Layout places the drag area and the content inside the border.
func (c *chrome) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	inset := c.inset()
	for _, o := range objects {
		o.Move(fyne.NewPos(inset, inset))
		o.Resize(size.SubtractWidthHeight(2*inset, 2*inset))
	}
}

func nativeHandle(w fyne.Window) (hwnd uintptr) {
	if n, ok := w.(driver.NativeWindow); ok {
		n.RunNative(func(context any) {
			if c, ok := context.(driver.WindowsWindowContext); ok {
				hwnd = c.HWND
			}
		})
	}
	return hwnd
}

// dragArea lies under the content and lets the system move the window. The
// toolkit hands it a press only where no control lies above. It must not be
// draggable: the toolkit would then start a drag on it from under a control.
type dragArea struct {
	widget.BaseWidget
	window fyne.Window
}

func newDragArea(w fyne.Window) *dragArea {
	d := &dragArea{window: w}
	d.ExtendBaseWidget(d)
	return d
}

func (d *dragArea) MouseDown(e *desktop.MouseEvent) {
	if e.Button == desktop.MouseButtonPrimary {
		window.Drag(nativeHandle(d.window))
	}
}

func (d *dragArea) MouseUp(*desktop.MouseEvent) {}

func (d *dragArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// header is the dark gradient band at the top of a window.
func header(height float32, top, left float32, content fyne.CanvasObject) fyne.CanvasObject {
	rule := canvas.NewRectangle(rgba(0xFF, 0xFF, 0xFF, 0x12))
	rule.SetMinSize(fyne.NewSize(1, 1))
	return container.NewStack(
		gap(0, height),
		headerBackground(),
		container.NewBorder(nil, rule, nil, nil,
			container.New(layout.NewCustomPaddedLayout(top, top, left, left), content)),
	)
}

// headerBackground is an elliptical gradient spreading from the top left
// corner.
func headerBackground() *canvas.Raster {
	return canvas.NewRaster(func(w, h int) image.Image {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				img.SetNRGBA(x, y, headerGradient(x, y, w, h))
			}
		}
		return img
	})
}

// headed puts a header at the top of a window and the body at the bottom, so
// that the fraction of a pixel the window rounds its height up by never shows
// between the footer and the bottom edge.
func headed(header, body fyne.CanvasObject) fyne.CanvasObject {
	return container.New(headedLayout{}, header, body)
}

type headedLayout struct{}

func (headedLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	top, bottom := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(max(top.Width, bottom.Width), top.Height+bottom.Height)
}

func (headedLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	top, bottom := objects[0].MinSize().Height, objects[1].MinSize().Height
	objects[0].Resize(fyne.NewSize(size.Width, top))
	objects[1].Move(fyne.NewPos(0, size.Height-bottom))
	objects[1].Resize(fyne.NewSize(size.Width, bottom))
}

var headerStops = [...]struct {
	at    float64
	color color.NRGBA
}{
	{0, rgba(0x2B, 0x3A, 0x67, 0xFF)},
	{0.6, rgba(0x14, 0x18, 0x24, 0xFF)},
	{1, rgba(0x11, 0x15, 0x1F, 0xFF)},
}

func headerGradient(x, y, w, h int) color.NRGBA {
	dx := float64(x) / (1.3 * float64(w))
	dy := float64(y) / (1.7 * float64(h))
	d := min(math.Hypot(dx, dy), 1)
	for i := 1; i < len(headerStops); i++ {
		from, to := headerStops[i-1], headerStops[i]
		if d <= to.at {
			return nrgba(blend(from.color, to.color, float32((d-from.at)/(to.at-from.at))))
		}
	}
	return headerStops[len(headerStops)-1].color
}

// logoTile is the application icon in white on a translucent tile.
func logoTile(size, iconSize, radius float32) fyne.CanvasObject {
	face := newShape(radius)
	face.set(rgba(0xFF, 0xFF, 0xFF, 0x14), rgba(0xFF, 0xFF, 0xFF, 0x1F), false)
	icon := canvas.NewImageFromImage(whiteIcon())
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSquareSize(iconSize))
	return container.NewStack(gap(size, size), face.Raster, container.NewCenter(icon))
}

// whiteIcon is the black application icon with every pixel made white.
var whiteIcon = sync.OnceValue(func() image.Image {
	src, err := png.Decode(bytes.NewReader(assets.IconPNG))
	if err != nil {
		return image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}
	img := image.NewNRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = 0xFF, 0xFF, 0xFF
	}
	return img
})

// closeButton is the round button with a cross in the corner of a header.
type closeButton struct {
	widget.BaseWidget
	onTapped func()
}

const closeButtonSize = 26

func newCloseButton(tapped func()) *closeButton {
	b := &closeButton{onTapped: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *closeButton) Tapped(*fyne.PointEvent) { b.onTapped() }

func (b *closeButton) MinSize() fyne.Size { return fyne.NewSquareSize(closeButtonSize) }

// CreateRenderer draws the disc and the cross.
func (b *closeButton) CreateRenderer() fyne.WidgetRenderer {
	const arm = 4.5
	ink := rgba(0xFF, 0xFF, 0xFF, 0xD9)
	disc := canvas.NewCircle(rgba(0xFF, 0xFF, 0xFF, 0x14))
	disc.Position2 = fyne.NewPos(closeButtonSize, closeButtonSize)
	cross := [2]*canvas.Line{canvas.NewLine(ink), canvas.NewLine(ink)}
	c := float32(closeButtonSize) / 2
	cross[0].Position1, cross[0].Position2 = fyne.NewPos(c-arm, c-arm), fyne.NewPos(c+arm, c+arm)
	cross[1].Position1, cross[1].Position2 = fyne.NewPos(c-arm, c+arm), fyne.NewPos(c+arm, c-arm)
	for _, l := range cross {
		l.StrokeWidth = 1.4
	}
	return widget.NewSimpleRenderer(container.NewWithoutLayout(disc, cross[0], cross[1]))
}
