package input

import (
	"testing"
	"unsafe"
)

// TestKeyboardInputSize guards the mirror of the Win32 INPUT structure;
// SendInput silently does nothing when the size is wrong.
func TestKeyboardInputSize(t *testing.T) {
	const want = 40 // sizeof(INPUT) on 64-bit Windows
	if got := unsafe.Sizeof(keyboardInput{}); got != want {
		t.Errorf("sizeof(keyboardInput) = %d, want %d", got, want)
	}
}
