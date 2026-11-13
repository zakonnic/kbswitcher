package ui

import (
	"image/color"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
)

const (
	keyFieldWidth   = 150
	keyFieldHeight  = 24
	keyFieldRadius  = 6
	keyFieldPadding = 10
)

// keyField records the keys pressed together while it has the focus, up to
// limit of them, and takes them once all are released. Keys are told by their
// position, so a letter is the same key in every layout. The field shows the
// keys, in grey while they are still the ones in force. Escape stops
// listening.
type keyField struct {
	widget.BaseWidget
	limit int
	// check says why keys cannot be taken, or returns nil.
	check func(keys []string) error
	// prompt is shown while the field listens and nothing is held yet.
	prompt string

	current, value []string
	pressed        []string        // taken in this press, in order
	down           map[string]bool // held now
	overflow       bool            // more than limit keys were pressed
	focused        bool
	// pressedOnce is set by the first press since the field got the focus;
	// the prompt shows until then.
	pressedOnce bool

	onChanged func([]string)
	// onMessage reports what the user should know about the last press, a
	// sentence a line, or "" when there is nothing to say.
	onMessage func(string)
	// onFocus reports that the field starts or stops listening.
	onFocus func(bool)
}

func newKeyField(limit int, prompt string, check func([]string) error) *keyField {
	f := &keyField{limit: limit, prompt: prompt, check: check, down: map[string]bool{}}
	f.ExtendBaseWidget(f)
	return f
}

// reset makes keys both the keys in force and the ones shown.
func (f *keyField) reset(keys []string) {
	f.current, f.value = keys, keys
	f.Refresh()
}

// Tapped starts listening.
func (f *keyField) Tapped(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(f); c != nil {
		c.Focus(f)
	}
}

func (f *keyField) FocusGained() { f.setFocused(true) }

func (f *keyField) FocusLost() { f.setFocused(false) }

func (f *keyField) setFocused(on bool) {
	f.focused, f.pressedOnce = on, false
	f.pressed, f.overflow = nil, false
	clear(f.down)
	f.Refresh()
	if f.onFocus != nil {
		f.onFocus(on)
	}
}

// AcceptsTab keeps Tab in the field, where it can be part of a combination.
func (f *keyField) AcceptsTab() bool { return true }

// KeyDown notes a key. It sees every key, including the ones shortcuts would
// eat; a key held down repeats, and is counted once.
func (f *keyField) KeyDown(e *fyne.KeyEvent) {
	name := keyName(e)
	if name == "" || name == "Escape" || f.down[name] {
		return
	}
	f.down[name] = true
	if slices.Contains(f.pressed, name) {
		return
	}
	if len(f.pressed) == f.limit {
		f.overflow = true
		return
	}
	f.pressed = append(f.pressed, name)
	f.Refresh()
}

// KeyUp takes the keys pressed once the last of them is released.
func (f *keyField) KeyUp(e *fyne.KeyEvent) {
	delete(f.down, keyName(e))
	if len(f.down) > 0 || len(f.pressed) == 0 {
		return
	}
	keys, overflow := f.pressed, f.overflow
	f.pressed, f.overflow, f.pressedOnce = nil, false, true
	defer f.Refresh()

	var notes []string
	switch {
	case overflow && f.limit == 1:
		notes = append(notes, "Only one key can be chosen here; the first one is kept.")
	case overflow:
		notes = append(notes, "At most two keys can be combined; the first two are kept.")
	}
	err := f.check(keys)
	if err != nil {
		notes = append(notes, sentence(err.Error()))
	}
	f.say(strings.Join(notes, "\n"))
	if err != nil {
		return
	}
	f.value = keys
	if f.onChanged != nil {
		f.onChanged(keys)
	}
}

func (f *keyField) say(message string) {
	if f.onMessage != nil {
		f.onMessage(message)
	}
}

// TypedKey lets Escape end listening without closing the window.
func (f *keyField) TypedKey(e *fyne.KeyEvent) {
	if e.Name != fyne.KeyEscape {
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(f); c != nil {
		c.Unfocus()
	}
}

// TypedRune drops characters: nothing typed is ever kept.
func (f *keyField) TypedRune(rune) {}

var _ desktop.Keyable = (*keyField)(nil)

// keyName names a key as the configuration does: modifiers by side-free name,
// every other key by the position its scan code gives.
func keyName(e *fyne.KeyEvent) string {
	switch e.Name {
	case desktop.KeyShiftLeft, desktop.KeyShiftRight:
		return "Shift"
	case desktop.KeyControlLeft, desktop.KeyControlRight:
		return "Ctrl"
	case desktop.KeyAltLeft, desktop.KeyAltRight:
		return "Alt"
	case desktop.KeySuperLeft, desktop.KeySuperRight:
		return "Super"
	}
	name, _ := hotkey.KeyForScanCode(e.Physical.ScanCode)
	return name
}

// keyLabels is how a list of keys reads on screen.
func keyLabels(keys []string) string {
	labels := make([]string, len(keys))
	for i, key := range keys {
		labels[i] = hotkey.Label(key)
	}
	return strings.Join(labels, " + ")
}

// sentence turns an error message into a sentence.
func sentence(message string) string {
	if message == "" {
		return ""
	}
	return strings.ToUpper(message[:1]) + message[1:] + "."
}

// CreateRenderer draws the face and the keys.
func (f *keyField) CreateRenderer() fyne.WidgetRenderer {
	r := &keyFieldRenderer{field: f, face: newShape(keyFieldRadius), text: canvas.NewText("", color.White)}
	r.Refresh()
	return r
}

type keyFieldRenderer struct {
	field *keyField
	face  *shape
	text  *canvas.Text
}

func (r *keyFieldRenderer) MinSize() fyne.Size { return fyne.NewSize(keyFieldWidth, keyFieldHeight) }

func (r *keyFieldRenderer) Layout(size fyne.Size) {
	r.face.Resize(size)
	r.face.Refresh()
	placeText(r.text, keyFieldPadding, size.Width-2*keyFieldPadding, size.Height)
}

func (r *keyFieldRenderer) Refresh() {
	f := r.field
	border := theme.Color(colorNameControlBorder)
	if f.focused {
		border = theme.Color(theme.ColorNamePrimary)
	}
	r.face.set(theme.Color(colorNameControl), border, false)

	r.text.TextSize = theme.Size(theme.SizeNameText)
	r.text.Color = theme.Color(theme.ColorNameForeground)
	switch {
	case len(f.pressed) > 0:
		r.text.Text = keyLabels(f.pressed)
	case f.focused && !f.pressedOnce:
		r.text.Text = f.prompt
		r.text.Color = theme.Color(colorNameSecondary)
	default:
		r.text.Text = keyLabels(f.value)
		if slices.Equal(f.value, f.current) {
			r.text.Color = theme.Color(colorNameSecondary)
		}
	}
	r.text.Refresh()
	r.Layout(f.Size())
}

func (r *keyFieldRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.face.Raster, r.text}
}

func (r *keyFieldRenderer) Destroy() {}
