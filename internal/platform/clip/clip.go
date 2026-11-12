// Package clip borrows the system clipboard for a conversion and gives it
// back. What kbswitcher itself puts there (the copy marker, the converted
// text, the restored content) is marked so that clipboard history and cloud
// sync leave it out, where the platform supports that.
package clip

import (
	"errors"
	"time"
)

const (
	// DefaultWait is how long a copy is given before it is considered failed.
	DefaultWait = 600 * time.Millisecond
	// maxPreservedBytes bounds what is held in memory while the clipboard is
	// borrowed. Larger content is left behind rather than duplicated.
	maxPreservedBytes = 8 << 20
)

// ErrNoCopy means the focused application did not answer the copy shortcut:
// there was no selection, or it does not support copying at all.
var ErrNoCopy = errors.New("nothing was copied")
