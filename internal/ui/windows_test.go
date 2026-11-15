package ui

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
	"github.com/zakonnic/kbswitcher/internal/config"
	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/updater"
)

// newApp builds the tray application on the test driver, with background
// work done at once so that a test sees its result when the call returns.
func newApp(t *testing.T, options Options) *App {
	t.Helper()
	fyneApp := test.NewTempApp(t)
	fyneApp.Settings().SetTheme(newTheme())
	saved := background
	background = func(work func() func()) { work()() }
	t.Cleanup(func() { background = saved })
	if options.Log == nil {
		options.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if options.Apply == nil {
		options.Apply = func(config.Config) error { return nil }
	}
	if options.Suspend == nil {
		options.Suspend = func(bool) {}
	}
	return &App{fyne: fyneApp, log: options.Log, options: options, config: options.Config}
}

// walk visits o and everything drawn inside it, widgets' renderers included.
func walk(o fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	visit(o)
	switch o := o.(type) {
	case *fyne.Container:
		for _, child := range o.Objects {
			walk(child, visit)
		}
	case fyne.Widget:
		for _, child := range test.WidgetRenderer(o).Objects() {
			walk(child, visit)
		}
	}
}

// texts lists the strings drawn in a window, its dialogs included.
func texts(w fyne.Window) string {
	var all []string
	collect := func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && text.Text != "" {
			all = append(all, text.Text)
		}
	}
	walk(w.Content(), collect)
	for _, overlay := range w.Canvas().Overlays().List() {
		walk(overlay, collect)
	}
	return strings.Join(all, "|")
}

// buttonNamed finds one of this package's buttons by its caption.
func buttonNamed(t *testing.T, w fyne.Window, caption string) *button {
	t.Helper()
	var found *button
	walk(w.Content(), func(o fyne.CanvasObject) {
		if b, ok := o.(*button); ok && b.text == caption {
			found = b
		}
	})
	if found == nil {
		t.Fatalf("no button %q in %s", caption, texts(w))
	}
	return found
}

// answer taps a button of the dialog on top of a window.
func answer(t *testing.T, w fyne.Window, caption string) {
	t.Helper()
	overlays := w.Canvas().Overlays().List()
	if len(overlays) == 0 {
		t.Fatalf("no dialog is open; the window shows %s", texts(w))
	}
	var found *widget.Button
	walk(overlays[len(overlays)-1], func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == caption {
			found = b
		}
	})
	if found == nil {
		t.Fatalf("the dialog has no %q button: %s", caption, texts(w))
	}
	test.Tap(found)
}

func TestAboutShowsHotkeysLayoutsAndWarnings(t *testing.T) {
	a := newApp(t, Options{
		Config:   config.Default(),
		Layouts:  keymap.Fallback(),
		Warnings: []string{"The hotkey could not be bound."},
	})
	a.showAbout()
	w := a.about.window
	shown := texts(w)
	for _, want := range []string{"last word + switch", "selection + switch", keymap.Fallback()[1].Name(),
		"RU", "The hotkey could not be bound.", "Updates unavailable", "GitHub"} {
		if !strings.Contains(shown, want) {
			t.Errorf("About does not show %q: %s", want, shown)
		}
	}
	if img := w.Canvas().Capture(); img.Bounds().Dx() == 0 {
		t.Error("About draws nothing")
	}

	// Opened again, it follows the settings.
	a.config.SwitchLayout = false
	a.showAbout()
	if shown := texts(a.about.window); !strings.Contains(shown, "|last word|") {
		t.Errorf("About still shows the old settings: %s", shown)
	}
	w.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyEscape})
}

func TestAboutTellsAHotkeyThatIsNotConfigured(t *testing.T) {
	newTestApp(t)
	cfg := config.Default()
	cfg.Hotkey.Modifiers = []string{"Hyper"}
	w := test.NewWindow(card(hotkeyRows(cfg)...))
	if !strings.Contains(texts(w), "The hotkey is not configured.") {
		t.Errorf("About shows %s", texts(w))
	}
}

func TestVersionTextNamesTheCommitOnce(t *testing.T) {
	version, commit := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() { buildinfo.Version, buildinfo.Commit = version, commit })

	buildinfo.Version, buildinfo.Commit = "v1.2.3", "abc1234"
	if got := versionText(); !strings.HasPrefix(got, "v1.2.3 · abc1234 · ") {
		t.Errorf("versionText() = %q", got)
	}
	buildinfo.Version = "v1.2.3-4-gabc1234"
	if got := versionText(); strings.Count(got, "abc1234") != 1 {
		t.Errorf("versionText() = %q, want the commit once", got)
	}
	buildinfo.Commit = "unknown"
	if got := versionText(); strings.Contains(got, "unknown") {
		t.Errorf("versionText() = %q", got)
	}
}

func TestLinkOpensItsAddress(t *testing.T) {
	newTestApp(t)
	l := repositoryLink().(*link)
	test.NewWindow(l)
	test.Tap(l)
	if l.Cursor() != desktop.PointerCursor {
		t.Error("a link does not show the pointer")
	}
}

// fakeUpdater is an update server and installer that answers as told.
type fakeUpdater struct {
	release              updater.Release
	newer                bool
	latestErr, updateErr error
	updated              []string
}

func (u *fakeUpdater) Latest(context.Context) (updater.Release, error) { return u.release, u.latestErr }
func (u *fakeUpdater) IsNewer(updater.Release) bool                    { return u.newer }
func (u *fakeUpdater) Update(_ context.Context, r updater.Release) (string, error) {
	u.updated = append(u.updated, r.Version)
	return "kbswitcher.exe", u.updateErr
}

func TestUpdateCheck(t *testing.T) {
	release := updater.Release{Version: "v9.0.0"}
	tests := []struct {
		name      string
		updater   *fakeUpdater
		answers   []string // to the dialogs, in order
		status    string
		installed bool
		restart   bool
	}{
		{"the check fails", &fakeUpdater{latestErr: errors.New("offline")}, []string{"OK"}, "Check failed", false, false},
		{"up to date", &fakeUpdater{release: release}, nil, "Up to date", false, false},
		{"declined", &fakeUpdater{release: release, newer: true}, []string{"No"}, "v9.0.0 available", false, false},
		{"installed, restart later", &fakeUpdater{release: release, newer: true}, []string{"Yes", "No"}, "v9.0.0 installed", true, false},
		{"installed and restarted", &fakeUpdater{release: release, newer: true}, []string{"Yes", "Yes"}, "v9.0.0 installed", true, true},
		{"the download fails", &fakeUpdater{release: release, newer: true, updateErr: errors.New("checksum")},
			[]string{"Yes", "OK"}, "Update failed", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newApp(t, Options{Config: config.Default(), Updater: tt.updater})
			a.showAbout()
			w := a.about.window
			test.Tap(buttonNamed(t, w, "Check for updates"))
			for _, caption := range tt.answers {
				answer(t, w, lang.L(caption))
			}
			if shown := texts(w); !strings.Contains(shown, tt.status) {
				t.Errorf("the status is not %q: %s", tt.status, shown)
			}
			installed := a.fyne.Preferences().String(lastUpdateKey) != ""
			if installed != tt.installed || a.restart != tt.restart {
				t.Errorf("installed %v, restart %v; want %v, %v", installed, a.restart, tt.installed, tt.restart)
			}
			if tt.installed && !strings.HasPrefix(idleStatus(a.fyne.Preferences()), "Updated ") {
				t.Errorf("the idle status is %q", idleStatus(a.fyne.Preferences()))
			}
		})
	}
}

// recorder notes what the settings window asks of the application.
type recorder struct {
	mu        sync.Mutex
	applied   []config.Config
	applyErr  error
	suspended []bool
	resumed   chan struct{}
}

func (r *recorder) apply(cfg config.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applied = append(r.applied, cfg)
	return r.applyErr
}

func (r *recorder) suspend(on bool) {
	r.mu.Lock()
	r.suspended = append(r.suspended, on)
	r.mu.Unlock()
	if !on {
		r.resumed <- struct{}{}
	}
}

func settingsApp(t *testing.T, r *recorder) *App {
	t.Helper()
	r.resumed = make(chan struct{}, 4)
	a := newApp(t, Options{Config: config.Default(), Apply: r.apply, Suspend: r.suspend})
	a.showSettings()
	return a
}

func TestSettingsSaveAppliesAndCloses(t *testing.T) {
	r := &recorder{}
	a := settingsApp(t, r)
	w := a.settings.window
	if img := w.Canvas().Capture(); img.Bounds().Dx() == 0 {
		t.Error("Settings draws nothing")
	}
	test.Tap(a.settings.form.autostart)
	test.Tap(a.settings.form.switchWord)
	test.Tap(buttonNamed(t, w, "Save"))

	if len(r.applied) != 1 || !r.applied[0].Autostart || r.applied[0].SwitchLayout {
		t.Fatalf("applied %+v", r.applied)
	}
	if !a.config.Autostart {
		t.Error("the configuration in force did not change")
	}
	a.showSettings() // opened again, it shows what is in force
	if !a.settings.form.autostart.on {
		t.Error("the settings window forgot the saved configuration")
	}
}

func TestSettingsSaveRefusesAnInvalidConfiguration(t *testing.T) {
	r := &recorder{}
	a := settingsApp(t, r)
	form := a.settings.form
	form.key.reset([]string{"Ctrl", "Q"})
	form.modifier.reset([]string{"Ctrl"})
	form.refresh()
	test.Tap(buttonNamed(t, a.settings.window, "Save"))

	if len(r.applied) != 0 {
		t.Errorf("applied %+v", r.applied)
	}
	if shown := texts(a.settings.window); !strings.Contains(shown, "already holds") {
		t.Errorf("no message on the conflict: %s", shown)
	}
}

func TestSettingsSaveReportsAFailureAndShowsWhatIsInForce(t *testing.T) {
	r := &recorder{applyErr: errors.New("the hotkey is taken")}
	a := settingsApp(t, r)
	test.Tap(a.settings.form.autostart)
	test.Tap(buttonNamed(t, a.settings.window, "Save"))

	if shown := texts(a.settings.window); !strings.Contains(strings.ToLower(shown), "the hotkey is taken") {
		t.Errorf("no error shown: %s", shown)
	}
	if a.settings.form.autostart.on || a.config.Autostart {
		t.Error("the form keeps a configuration that was not applied")
	}
}

func TestSettingsCloseWithoutSaving(t *testing.T) {
	r := &recorder{}
	a := settingsApp(t, r)
	w := a.settings.window
	test.Tap(buttonNamed(t, w, "Cancel"))
	w.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyEscape})
	walk(w.Content(), func(o fyne.CanvasObject) {
		if c, ok := o.(*closeButton); ok {
			test.Tap(c)
		}
	})
	if len(r.applied) != 0 {
		t.Errorf("applied %+v", r.applied)
	}
}

func TestSettingsReleaseTheHotkeysWhileAFieldListens(t *testing.T) {
	r := &recorder{}
	a := settingsApp(t, r)
	form := a.settings.form

	// Moving from one field to the other is left out: the test driver runs
	// the delayed check on the timer's goroutine, not on the main one.
	test.Tap(form.key) // listens: the hotkeys are released
	a.settings.window.Canvas().Unfocus()
	select {
	case <-r.resumed:
	case <-time.After(5 * time.Second):
		t.Fatal("the hotkeys were not bound again")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.Equal(r.suspended, []bool{true, false}) {
		t.Errorf("suspended %v, want [true false]", r.suspended)
	}
}

func TestKeyFieldStopsListeningOnEscape(t *testing.T) {
	newTestApp(t)
	field := newKeyField(2, "Press keys", checkHotkey)
	w := test.NewWindow(field)
	test.Tap(field)
	if !field.focused {
		t.Fatal("a tap does not start listening")
	}
	field.TypedRune('q')
	field.TypedKey(&fyne.KeyEvent{Name: fyne.KeyA})
	field.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if field.focused || w.Canvas().Focused() != nil {
		t.Error("Escape does not end listening")
	}
	if !field.AcceptsTab() {
		t.Error("Tab cannot be part of a combination")
	}
}

func TestButton(t *testing.T) {
	newTestApp(t)
	tapped := 0
	b := newButton("Go", func() { tapped++ })
	b.primary = true
	test.NewWindow(b).Canvas().Capture()

	b.MouseIn(&desktop.MouseEvent{})
	b.MouseMoved(&desktop.MouseEvent{})
	b.MouseDown(&desktop.MouseEvent{})
	if !b.pressed {
		t.Error("a press does not show")
	}
	b.MouseOut()
	b.MouseDown(&desktop.MouseEvent{})
	b.MouseUp(&desktop.MouseEvent{})
	if b.pressed {
		t.Error("the button stays pressed")
	}
	test.Tap(b)

	b.disable()
	b.MouseDown(&desktop.MouseEvent{})
	test.Tap(b)
	b.enable()
	test.Tap(b)
	if tapped != 2 || b.pressed {
		t.Errorf("tapped %d times, pressed %v; want 2 taps of the enabled button", tapped, b.pressed)
	}
	newButton("Nothing", nil).Tapped(nil)
}

func TestTheme(t *testing.T) {
	th := newTheme()
	if th.Color(colorNameCard, 0) != colors[colorNameCard] || th.Color("unknown", 0) == nil {
		t.Error("colors")
	}
	for _, name := range []fyne.ThemeSizeName{"text", "headingText", "subHeadingText", "helperText",
		"padding", "inputRadius", "selectionRadius", "scrollBar", "scrollBarSmall", "iconInline"} {
		if th.Size(name) <= 0 {
			t.Errorf("size %s is %v", name, th.Size(name))
		}
	}
	for _, style := range []fyne.TextStyle{{}, {Bold: true}, {Monospace: true}} {
		if th.Font(style) == nil {
			t.Errorf("no font for %+v", style)
		}
	}
	if (&appTheme{Theme: th}).Font(fyne.TextStyle{}) == nil {
		t.Error("no font without the system's")
	}
	if fontFile(t.TempDir(), "missing.ttf") != nil {
		t.Error("a missing font file loads")
	}
	if r, _, _, _ := darken(rgba(0x80, 0x80, 0x80, 0xFF), 0.5).RGBA(); r >= 0x8080 {
		t.Errorf("darken left red at %#x", r)
	}
}

func TestTrayIcon(t *testing.T) {
	if trayIcon() == nil {
		t.Fatal("no tray icon")
	}
	img, err := trayImage(1000)
	if err != nil || img.Bounds().Dx() == 0 {
		t.Errorf("trayImage(1000) = %v, %v", img, err)
	}
}

func TestRunNeedsASystemTray(t *testing.T) {
	a := newApp(t, Options{Config: config.Default()})
	if a.Run() {
		t.Error("Run asked for a restart")
	}
}

func TestKeyFieldEdges(t *testing.T) {
	newTestApp(t)
	field := newKeyField(1, "", checkModifier)
	var message string
	field.onMessage = func(m string) { message = m }

	field.KeyDown(&fyne.KeyEvent{Name: fyne.KeyEscape})       // Escape is never a key of a combination
	field.KeyDown(&fyne.KeyEvent{Name: desktop.KeySuperLeft}) // the system key
	field.KeyDown(&fyne.KeyEvent{Name: desktop.KeySuperLeft}) // held: repeats, counted once
	field.KeyDown(&fyne.KeyEvent{Name: desktop.KeyAltLeft})   // one key too many
	field.KeyUp(&fyne.KeyEvent{Name: desktop.KeyAltLeft})     // not the last one up
	field.KeyUp(&fyne.KeyEvent{Name: desktop.KeySuperLeft})   // the last one up
	if !slices.Equal(field.value, []string{"Super"}) || !strings.HasPrefix(message, "Only one key") {
		t.Errorf("value %v, message %q", field.value, message)
	}
	if sentence("") != "" {
		t.Error("an empty sentence")
	}
}

func TestSettingsFormEdges(t *testing.T) {
	newTestApp(t)
	if _, err := comboOf([]string{"Q", "W"}); err == nil {
		t.Error("two keys make a hotkey")
	}
	if err := checkHotkey([]string{"Q", "W"}); err == nil {
		t.Error("checkHotkey took two keys")
	}
	broken := config.Default()
	broken.Hotkey.Modifiers = []string{"Hyper"}
	broken.SelectionModifier = "Hyper"
	form := newSettingsForm(broken)
	if got := form.config(); got.Hotkey.Key != config.Default().Hotkey.Key || got.SelectionModifier != "Shift" {
		t.Errorf("a broken configuration shows as %+v, want the defaults", got)
	}
}

func TestRenderersLetGo(t *testing.T) {
	newTestApp(t)
	sw := newToggle(nil)
	test.NewWindow(sw)
	test.Tap(sw) // starts the knob moving
	for _, w := range []fyne.Widget{sw, newButton("x", nil), newChip("x", pillOK), newKeyField(2, "", checkHotkey), newPanel()} {
		test.WidgetRenderer(w).Destroy()
	}
	hidden := label("hidden", 12, colors[colorNameSecondary], false)
	hidden.Hide()
	test.NewWindow(card(row(30, hidden, gap(0, 0)), hidden)).Canvas().Capture()
	if img := newShape(4).draw(0, 0); img.Bounds().Dx() != 0 {
		t.Error("a shape of no size draws pixels")
	}
	if headerGradient(100, 100, 100, 100) != headerStops[len(headerStops)-1].color {
		t.Error("the far corner of the header is not the last color")
	}
	newButton("x", nil).release()
}
