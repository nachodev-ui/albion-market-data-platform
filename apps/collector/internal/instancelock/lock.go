package instancelock

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrAlreadyLocked = errors.New("receiver data directory is already locked")

type metadata struct {
	PID       int       `json:"pid"`
	Hostname  string    `json:"hostname"`
	StartedAt time.Time `json:"started_at"`
	Port      int       `json:"port"`
}

type Lock struct {
	path     string
	listener net.Listener
	once     sync.Once
}

func Acquire(path string) (*Lock, error) {
	if path == "" {
		return nil, fmt.Errorf("instance lock path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve instance lock path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return nil, err
	}
	port := lockPort(absolute)
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		content, _ := os.ReadFile(absolute)
		return nil, fmt.Errorf("%w: %s %s", ErrAlreadyLocked, absolute, string(content))
	}
	hostname, _ := os.Hostname()
	payload, _ := json.MarshalIndent(metadata{
		PID:       os.Getpid(),
		Hostname:  hostname,
		StartedAt: time.Now().UTC(),
		Port:      port,
	}, "", "  ")
	if err := os.WriteFile(absolute, payload, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	return &Lock{path: absolute, listener: listener}, nil
}

func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	var result error
	l.once.Do(func() {
		if err := l.listener.Close(); err != nil {
			result = err
		}
		if err := os.Remove(l.path); result == nil && err != nil && !errors.Is(err, os.ErrNotExist) {
			result = err
		}
	})
	return result
}

func lockPort(path string) int {
	digest := sha256.Sum256([]byte(filepath.Clean(path)))
	const first = 49152
	const span = 16384
	return first + int(binary.BigEndian.Uint16(digest[:2]))%span
}
