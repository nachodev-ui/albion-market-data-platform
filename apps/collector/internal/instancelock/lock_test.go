package instancelock

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
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

func TestLockFallsBackWhenPreferredPortIsOccupied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receiver.lock")
	preferred := lockPort(path)
	occupied, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", preferred))
	if err != nil {
		t.Skipf("preferred test port unavailable before test: %v", err)
	}
	defer occupied.Close()

	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire() failed after preferred-port conflict: %v", err)
	}
	defer lock.Close()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var current metadata
	if err := json.Unmarshal(content, &current); err != nil {
		t.Fatal(err)
	}
	if current.Port == preferred {
		t.Fatalf("lock reused occupied preferred port %d", preferred)
	}
	if _, err := Acquire(path); !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("second Acquire() err=%v, want ErrAlreadyLocked", err)
	}
}

func TestLockRemovesStaleMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receiver.lock")
	stale, err := json.Marshal(metadata{Port: lockPort(path), Token: "stale"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire() failed with stale metadata: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}
