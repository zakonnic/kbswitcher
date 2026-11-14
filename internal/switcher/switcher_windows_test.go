package switcher

import (
	"io"
	"log/slog"
	"testing"

	"github.com/zakonnic/kbswitcher/internal/platform/layouts"
)

// TestConvertWithTheInstalledLayouts uses the layouts of the running Windows
// session, so that a mistake in the tables shows up as a wrong word here.
func TestConvertWithTheInstalledLayouts(t *testing.T) {
	reader, err := layouts.Open()
	if err != nil {
		t.Fatalf("layouts.Open: %v", err)
	}
	defer reader.Close()

	installed, err := reader.List()
	if err != nil {
		t.Fatalf("layouts.List: %v", err)
	}
	if len(installed) < 2 {
		t.Skip("this session has fewer than two layouts installed")
	}
	if installed[0].ID() != "00000409" || installed[1].ID() != "00000419" {
		t.Skipf("this test expects US and Russian, this session has %s and %s",
			installed[0], installed[1])
	}

	clipboard := &fakeClipboard{}
	editor := newEditor("hello ckjdj", clipboard)
	provider := &fakeLayouts{active: installed[0].ID()}

	options := DefaultOptions()
	options.KeyDelay = 0
	options.PasteSettle = 0

	deps := Deps{Layouts: provider, Keyboard: editor, Clipboard: clipboard}
	s, err := New(installed, deps, options, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if got := editor.String(); got != "hello слово" {
		t.Errorf("text is %q, want %q", got, "hello слово")
	}
}
