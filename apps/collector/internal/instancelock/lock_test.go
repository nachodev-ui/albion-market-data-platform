package instancelock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLockRejectsSecondInstanceAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receiver.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(path); !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("err=%v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
}
