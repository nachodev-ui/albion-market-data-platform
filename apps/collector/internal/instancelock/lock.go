package instancelock

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrAlreadyLocked = errors.New("receiver data directory is already locked")

type metadata struct {
	PID       int       `json:"pid"`
	Hostname  string    `json:"hostname"`
	StartedAt time.Time `json:"started_at"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
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

	for attempt := 0; attempt < 3; attempt++ {
		locked, err := existingLockIsActive(absolute)
		if err != nil {
			return nil, err
		}
		if locked {
			content, _ := os.ReadFile(absolute)
			return nil, fmt.Errorf("%w: %s %s", ErrAlreadyLocked, absolute, string(content))
		}

		listener, port, err := listenForLock(absolute)
		if err != nil {
			return nil, err
		}
		token, err := randomToken()
		if err != nil {
			_ = listener.Close()
			return nil, fmt.Errorf("create instance lock token: %w", err)
		}
		go serveLock(listener, token)

		hostname, _ := os.Hostname()
		payload, err := json.MarshalIndent(metadata{
			PID:       os.Getpid(),
			Hostname:  hostname,
			StartedAt: time.Now().UTC(),
			Port:      port,
			Token:     token,
		}, "", "  ")
		if err != nil {
			_ = listener.Close()
			return nil, err
		}

		file, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			_ = listener.Close()
			continue
		}
		if err != nil {
			_ = listener.Close()
			return nil, err
		}
		writeErr := func() error {
			defer file.Close()
			if _, err := file.Write(payload); err != nil {
				return err
			}
			return file.Sync()
		}()
		if writeErr != nil {
			_ = listener.Close()
			_ = os.Remove(absolute)
			return nil, writeErr
		}
		return &Lock{path: absolute, listener: listener}, nil
	}

	content, _ := os.ReadFile(absolute)
	return nil, fmt.Errorf("%w: %s %s", ErrAlreadyLocked, absolute, string(content))
}

func existingLockIsActive(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var current metadata
	if err := json.Unmarshal(content, &current); err == nil && current.Port > 0 && current.Token != "" {
		if pingLock(current.Port, current.Token) {
			return true, nil
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove stale instance lock %s: %w", path, err)
	}
	return false, nil
}

func listenForLock(path string) (net.Listener, int, error) {
	preferred := lockPort(path)
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", preferred))
	if err == nil {
		return listener, preferred, nil
	}

	listener, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf("allocate local instance-lock port for %s: %w", path, err)
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port <= 0 {
		_ = listener.Close()
		return nil, 0, fmt.Errorf("resolve allocated instance-lock port for %s", path)
	}
	return listener, address.Port, nil
}

func randomToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func serveLock(listener net.Listener, token string) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
			line, err := bufio.NewReader(connection).ReadString('\n')
			if err != nil || strings.TrimSpace(line) != "PING "+token {
				return
			}
			_, _ = fmt.Fprintf(connection, "OK %s\n", token)
		}()
	}
}

func pingLock(port int, token string) bool {
	connection, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return false
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprintf(connection, "PING %s\n", token); err != nil {
		return false
	}
	line, err := bufio.NewReader(connection).ReadString('\n')
	return err == nil && strings.TrimSpace(line) == "OK "+token
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
