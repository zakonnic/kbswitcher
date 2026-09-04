package singleinstance

import (
	"errors"
	"testing"
)

func TestLockIsExclusive(t *testing.T) {
	release, err := Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	if _, err := Lock(); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second Lock = %v, want ErrAlreadyRunning", err)
	}

	release()
	release() // releasing twice must be harmless

	again, err := Lock()
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	again()
}
