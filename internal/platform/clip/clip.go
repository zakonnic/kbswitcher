// Package clip borrows the system clipboard for a conversion and gives it
// back: text, images and file lists survive the round trip unless they are
// enormous.
package clip

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"golang.design/x/clipboard"
)

const (
	// DefaultWait is how long a copy is given before it is considered failed.
	DefaultWait = 600 * time.Millisecond
	// DefaultPoll is how often the clipboard is inspected while waiting.
	DefaultPoll = 15 * time.Millisecond
	// maxPreservedBytes bounds what is held in memory while the clipboard is
	// borrowed. Larger content is left behind rather than duplicated.
	maxPreservedBytes = 8 << 20
)

// ErrNoCopy means the focused application did not answer the copy shortcut:
// there was no selection, or it does not support copying at all.
var ErrNoCopy = errors.New("nothing was copied")

// preservedFormats are the formats worth putting back, in the order they are
// looked for.
var preservedFormats = []clipboard.Format{clipboard.FmtText, clipboard.FmtImage, clipboard.FmtFiles}

// Snapshot is the clipboard content found before the clipboard was borrowed.
// An empty snapshot means there was nothing worth restoring.
type Snapshot struct {
	format clipboard.Format
	data   []byte
}

// Empty reports whether the snapshot holds nothing.
func (s Snapshot) Empty() bool { return len(s.data) == 0 }

// Clipboard is the system clipboard.
type Clipboard struct {
	wait time.Duration
	poll time.Duration
}

// Open connects to the system clipboard.
func Open() (*Clipboard, error) {
	if err := clipboard.Init(); err != nil {
		return nil, fmt.Errorf("clipboard is not available: %w", err)
	}
	return &Clipboard{wait: DefaultWait, poll: DefaultPoll}, nil
}

// Close releases the clipboard connection.
func (c *Clipboard) Close() error { return nil }

// Snapshot copies aside whatever is on the clipboard. Oversized content and
// formats that cannot be written back yield an empty snapshot.
func (c *Clipboard) Snapshot(ctx context.Context) Snapshot {
	present, err := clipboard.Formats(ctx)
	if err != nil {
		present = []clipboard.Format{clipboard.FmtText}
	}

	for _, format := range preservedFormats {
		if !slices.Contains(present, format) {
			continue
		}
		data, err := clipboard.Read(ctx, format)
		if err != nil || len(data) == 0 || len(data) > maxPreservedBytes {
			continue
		}
		return Snapshot{format: format, data: data}
	}
	return Snapshot{}
}

// Restore puts a snapshot back on the clipboard. An empty snapshot clears it.
func (c *Clipboard) Restore(ctx context.Context, snapshot Snapshot) error {
	if snapshot.Empty() {
		return c.WriteText(ctx, "")
	}
	if _, err := clipboard.Write(ctx, snapshot.format, snapshot.data); err != nil {
		return fmt.Errorf("cannot restore the clipboard: %w", err)
	}
	return nil
}

// WriteText puts plain text on the clipboard.
func (c *Clipboard) WriteText(ctx context.Context, text string) error {
	if _, err := clipboard.Write(ctx, clipboard.FmtText, []byte(text)); err != nil {
		return fmt.Errorf("cannot write to the clipboard: %w", err)
	}
	return nil
}

// ReadText returns the plain text on the clipboard, or "" when there is none.
func (c *Clipboard) ReadText(ctx context.Context) (string, error) {
	data, err := clipboard.Read(ctx, clipboard.FmtText)
	if errors.Is(err, clipboard.ErrNoData) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot read the clipboard: %w", err)
	}
	return string(data), nil
}

// AwaitText waits until the clipboard holds text other than exclude, which is
// the marker primed before the copy shortcut, and returns it.
func (c *Clipboard) AwaitText(ctx context.Context, exclude string) (string, error) {
	deadline := time.NewTimer(c.wait)
	defer deadline.Stop()
	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()

	for {
		text, err := c.ReadText(ctx)
		if err != nil {
			return "", err
		}
		if text != "" && text != exclude {
			return text, nil
		}

		select {
		case <-ticker.C:
		case <-deadline.C:
			return "", ErrNoCopy
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
