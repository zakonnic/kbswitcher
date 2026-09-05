package ui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	toggleWidth  = 30
	toggleHeight = 18
	toggleInset  = 2
	toggleMotion = 160 * time.Millisecond
)

// toggle is an on/off switch drawn like the macOS one. It carries no caption:
// the row it sits in names the setting.
type toggle struct {
	widget.BaseWidget
	on        bool
	onChanged func(bool)
}

func newToggle(changed func(bool)) *toggle {
	t := &toggle{onChanged: changed}
	t.ExtendBaseWidget(t)
	return t
}

func (t *toggle) setOn(on bool) {
	if t.on == on {
		return
	}
	t.on = on
	t.Refresh()
	if t.onChanged != nil {
		t.onChanged(on)
	}
}

// Tapped flips the switch.
func (t *toggle) Tapped(*fyne.PointEvent) { t.setOn(!t.on) }

// CreateRenderer draws the track and the knob.
func (t *toggle) CreateRenderer() fyne.WidgetRenderer {
	r := &toggleRenderer{
		toggle: t,
		track:  canvas.NewRectangle(color.Transparent),
		shadow: canvas.NewCircle(color.Transparent),
		knob:   canvas.NewCircle(color.Transparent),
	}
	if t.on {
		r.progress = 1
	}
	r.Refresh()
	return r
}

type toggleRenderer struct {
	toggle *toggle
	track  *canvas.Rectangle
	shadow *canvas.Circle
	knob   *canvas.Circle

	// progress is 0 when off, 1 when on and in between while the knob moves.
	progress float32
	motion   *fyne.Animation
}

func (r *toggleRenderer) MinSize() fyne.Size { return fyne.NewSize(toggleWidth, toggleHeight) }

// Layout keeps the switch at its own size, centered in whatever it is given.
func (r *toggleRenderer) Layout(size fyne.Size) {
	r.track.Move(fyne.NewPos((size.Width-toggleWidth)/2, (size.Height-toggleHeight)/2))
	r.track.Resize(fyne.NewSize(toggleWidth, toggleHeight))
	r.track.CornerRadius = toggleHeight / 2
	r.place()
}

// place puts the knob where progress says and blends the track color to match.
func (r *toggleRenderer) place() {
	const knob = toggleHeight - 2*toggleInset
	origin := r.track.Position()
	x := origin.X + toggleInset + r.progress*(toggleWidth-toggleHeight)
	y := origin.Y + toggleInset
	r.knob.Position1 = fyne.NewPos(x, y)
	r.knob.Position2 = fyne.NewPos(x+knob, y+knob)
	r.shadow.Position1 = fyne.NewPos(x-0.5, y+0.5)
	r.shadow.Position2 = fyne.NewPos(x+knob+0.5, y+knob+1.5)

	r.track.FillColor = blend(theme.Color(colorNameTrackOff), theme.Color(theme.ColorNamePrimary), r.progress)
	r.track.StrokeColor = withAlpha(theme.Color(colorNameControlBorder), 1-r.progress)
	r.track.StrokeWidth = 1
	r.track.Refresh()
	r.shadow.Refresh()
	r.knob.Refresh()
}

func (r *toggleRenderer) Refresh() {
	r.knob.FillColor = theme.Color(colorNameKnob)
	r.shadow.FillColor = withAlpha(color.Black, 0.15)

	target := float32(0)
	if r.toggle.on {
		target = 1
	}
	if r.progress == target {
		r.place()
		return
	}
	r.slide(target)
}

// slide moves the knob to its new place instead of jumping there.
func (r *toggleRenderer) slide(target float32) {
	if r.motion != nil {
		r.motion.Stop()
	}
	from := r.progress
	r.motion = fyne.NewAnimation(toggleMotion, func(f float32) {
		r.progress = from + (target-from)*f
		r.place()
	})
	r.motion.Curve = fyne.AnimationEaseInOut
	r.motion.Start()
}

func (r *toggleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.track, r.shadow, r.knob}
}

func (r *toggleRenderer) Destroy() {
	if r.motion != nil {
		r.motion.Stop()
	}
}
