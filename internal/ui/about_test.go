package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestIdleStatusShowsLastUpdate(t *testing.T) {
	prefs := test.NewTempApp(t).Preferences()
	for _, tc := range []struct{ stored, want string }{
		{"", "Not checked"},
		{"garbage", "Not checked"},
		{"2026-11-13", "Updated 13 Nov 2026"},
	} {
		prefs.SetString(lastUpdateKey, tc.stored)
		if got := idleStatus(prefs); got != tc.want {
			t.Errorf("stored %q: got %q, want %q", tc.stored, got, tc.want)
		}
	}
}
