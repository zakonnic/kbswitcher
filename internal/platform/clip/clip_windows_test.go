package clip

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

// readElsewhere reads the clipboard text the way another application would:
// from a thread with no window of its own.
func readElsewhere(t *testing.T) string {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		for range openAttempts {
			if ok, _, _ := procOpenClipboard.Call(0); ok != 0 {
				break
			}
			time.Sleep(openRetry)
		}
		defer closeClipboard()
		data := getData(cfUnicodeText)
		if len(data) < 2 {
			got <- ""
			return
		}
		got <- string(fromUTF16(data))
	}()
	select {
	case text := <-got:
		return text
	case <-time.After(2 * time.Second):
		t.Fatal("reading the clipboard hung")
		return ""
	}
}

func fromUTF16(data []byte) []rune {
	var runes []rune
	for i := 0; i+1 < len(data); i += 2 {
		unit := uint16(data[i]) | uint16(data[i+1])<<8
		if unit == 0 {
			break
		}
		runes = append(runes, rune(unit))
	}
	return runes
}

func available(format uint32) bool {
	ok, _, _ := procIsClipboardFormatAvailable.Call(uintptr(format))
	return ok != 0
}

// TestOfferAndRestore borrows the real clipboard and gives it back.
func TestOfferAndRestore(t *testing.T) {
	c, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()
	saved := c.Snapshot(t.Context())
	if saved.unread {
		t.Skip("another application holds the clipboard")
	}
	defer func() {
		if err := c.Restore(t.Context(), saved); err != nil {
			t.Errorf("Restore: %v", err)
		}
	}()

	read, err := c.Offer(t.Context(), "привет")
	if err != nil {
		t.Fatalf("Offer: %v", err)
	}
	select {
	case <-read:
		t.Fatal("the offer counts as read before anyone asked for it")
	default:
	}
	if !available(fmtHistory) || !available(fmtCloud) || !available(fmtExcludeFromMonitors) {
		t.Error("the offered text is not marked private")
	}
	if got := readElsewhere(t); got != "привет" {
		t.Errorf("another application reads %q, want %q", got, "привет")
	}
	select {
	case <-read:
	case <-time.After(time.Second):
		t.Fatal("the read was not reported")
	}

	// A copy made by someone else after the conversion must survive the
	// restore that follows it.
	marker := c.Snapshot(t.Context())
	if err := c.WriteText(t.Context(), "converted"); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	c.do(func() { c.seq-- }) // as if another application had written since
	if err := c.Restore(t.Context(), marker); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := readElsewhere(t); got != "converted" {
		t.Errorf("after a foreign write the restore replaced the clipboard with %q", got)
	}
	c.do(func() { c.seq = sequence() })
}

// writeElsewhere puts text on the clipboard as another application's copy
// shortcut does.
func writeElsewhere(t *testing.T, text string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		for range openAttempts {
			if ok, _, _ := procOpenClipboard.Call(0); ok != 0 {
				break
			}
			time.Sleep(openRetry)
		}
		defer closeClipboard()
		_, _, _ = procEmptyClipboard.Call()
		setData(cfUnicodeText, utf16Bytes(text))
		markPrivate() // kept out of the user's clipboard history and cloud sync
	}()
	<-done
}

// TestAwaitTextSeesACopy borrows the real clipboard and gives it back.
func TestAwaitTextSeesACopy(t *testing.T) {
	c, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()
	saved := c.Snapshot(t.Context())
	if saved.unread {
		t.Skip("another application holds the clipboard")
	}
	defer func() {
		if err := c.Restore(t.Context(), saved); err != nil {
			t.Errorf("Restore: %v", err)
		}
	}()

	if err := c.WriteText(t.Context(), "marker"); err != nil {
		t.Fatal(err)
	}
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		time.Sleep(20 * time.Millisecond)
		writeElsewhere(t, "copied")
	}()
	got, err := c.AwaitText(t.Context(), "marker")
	<-copied // before the restore, which must come last
	if got != "copied" || err != nil {
		t.Errorf("AwaitText = %q, %v", got, err)
	}

	// Nothing copied: the shortcut found nothing to copy.
	if err := c.WriteText(t.Context(), "marker"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AwaitText(t.Context(), "marker"); !errors.Is(err, ErrNoCopy) {
		t.Errorf("AwaitText with nothing copied = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.AwaitText(ctx, "marker"); !errors.Is(err, context.Canceled) {
		t.Errorf("AwaitText after a cancel = %v", err)
	}
	if !(Snapshot{}).Empty() || (Snapshot{items: []item{{format: cfUnicodeText}}}).Empty() {
		t.Error("Empty misreads a snapshot")
	}
}
