package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	windowMargin = 20
	cardRadius   = 10
	cardPadding  = 12
	rowPadding   = 3
)

func windowPadding() fyne.Layout {
	return layout.NewCustomPaddedLayout(windowMargin, windowMargin, windowMargin, windowMargin)
}

// card groups rows on a panel with hairlines between them, the way macOS
// System Settings does.
func card(rows ...fyne.CanvasObject) fyne.CanvasObject {
	objects := make([]fyne.CanvasObject, 0, 2*len(rows))
	for i, row := range rows {
		if i > 0 {
			objects = append(objects, hairline())
		}
		objects = append(objects, row)
	}
	return container.NewStack(newPanel(), container.New(column{}, objects...))
}

// row is one line of a card: a caption at the left and a control at the right.
func row(text string, control fyne.CanvasObject) fyne.CanvasObject {
	return container.New(
		layout.NewCustomPaddedLayout(rowPadding, rowPadding, cardPadding, cardPadding),
		container.NewHBox(widget.NewLabel(text), layout.NewSpacer(), container.NewCenter(control)),
	)
}

func hairline() fyne.CanvasObject {
	line := canvas.NewRectangle(theme.Color(colorNameHairline))
	line.SetMinSize(fyne.NewSize(1, 1))
	return container.New(layout.NewCustomPaddedLayout(0, 0, cardPadding, 0), line)
}

// gap is empty space of a fixed size.
func gap(width, height float32) fyne.CanvasObject {
	g := canvas.NewRectangle(color.Transparent)
	g.SetMinSize(fyne.NewSize(width, height))
	return g
}

// column stacks objects top to bottom with no space between them, which the
// toolkit's box layout cannot do.
type column struct{}

func (column) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		size.Width = max(size.Width, o.MinSize().Width)
		size.Height += o.MinSize().Height
	}
	return size
}

func (column) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var y float32
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		height := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, height))
		y += height
	}
}

// panel is the background of a card: a rounded rectangle with a hairline.
type panel struct{ widget.BaseWidget }

func newPanel() *panel {
	p := &panel{}
	p.ExtendBaseWidget(p)
	return p
}

// CreateRenderer draws the panel.
func (p *panel) CreateRenderer() fyne.WidgetRenderer {
	r := &panelRenderer{face: newShape(cardRadius)}
	r.Refresh()
	return r
}

type panelRenderer struct{ face *shape }

func (r *panelRenderer) MinSize() fyne.Size { return fyne.Size{} }

func (r *panelRenderer) Layout(size fyne.Size) {
	r.face.Resize(size)
	r.face.Refresh()
}

func (r *panelRenderer) Refresh() {
	r.face.set(theme.Color(colorNameCard), theme.Color(colorNameHairline), false)
}

func (r *panelRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.face.Raster} }

func (r *panelRenderer) Destroy() {}
