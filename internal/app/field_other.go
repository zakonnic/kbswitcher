//go:build !windows

package app

import (
	"github.com/zakonnic/kbswitcher/internal/platform/input"
	"github.com/zakonnic/kbswitcher/internal/switcher"
)

// newField has nothing to offer: only Windows reads text fields without the
// clipboard so far.
func newField(*input.Keyboard) switcher.Field { return nil }
