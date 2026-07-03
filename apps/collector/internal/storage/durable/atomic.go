package durable

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Recovery struct {
	UsedBackup      bool
	QuarantinedPath string
}

func AtomicWrite(path string, content []byte, perm fs.FileMode) error {
	budgetMu.Lock()
	defer budgetMu.Unlock()
	if err := checkReplaceLocked(path, int64(len(content))); err != nil {
		return err
	}
	return atomicWriteUnlocked(path, content, perm, true)
}

func atomicWriteUnlocked(path string, content []byte, perm fs.FileMode, rotateBackup bool) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	temporaryPath := temporary.Name()
	installed := false
	defer func() {
		_ = temporary.Close()
		if !installed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(perm); err != nil {
		return fmt.Errorf("set temporary permissions for %s: %w", path, err)
	}
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary file for %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}

	backup := path + ".bak"
	hadPrimary := false
	if _, err := os.Stat(path); err == nil {
		hadPrimary = true
		if rotateBackup {
			_ = os.Remove(backup)
			if err := os.Rename(path, backup); err != nil {
				return fmt.Errorf("rotate backup for %s: %w", path, err)
			}
		} else if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove replaced file %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if err := os.Rename(temporaryPath, path); err != nil {
		if rotateBackup && hadPrimary {
			_ = os.Rename(backup, path)
		}
		return fmt.Errorf("install %s: %w", path, err)
	}
	installed = true
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync directory for %s: %w", path, err)
	}
	return nil
}

func LoadJSONWithBackup[T any](path string, validate func(T) error) (T, Recovery, error) {
	primary, _, primaryErr := decodeJSONFile[T](path, validate)
	if primaryErr == nil {
		return primary, Recovery{}, nil
	}
	backupPath := path + ".bak"
	backup, backupContent, backupErr := decodeJSONFile[T](backupPath, validate)
	if backupErr != nil {
		var zero T
		if errors.Is(primaryErr, os.ErrNotExist) && errors.Is(backupErr, os.ErrNotExist) {
			return zero, Recovery{}, os.ErrNotExist
		}
		return zero, Recovery{}, fmt.Errorf("load %s (primary: %v, backup: %v)", path, primaryErr, backupErr)
	}

	recovery := Recovery{UsedBackup: true}
	if _, err := os.Stat(path); err == nil {
		quarantine := fmt.Sprintf("%s.corrupt-%s", path, time.Now().UTC().Format("20060102T150405.000000000Z"))
		if err := os.Rename(path, quarantine); err != nil {
			var zero T
			return zero, Recovery{}, fmt.Errorf("quarantine corrupt %s: %w", path, err)
		}
		recovery.QuarantinedPath = quarantine
	} else if !errors.Is(err, os.ErrNotExist) {
		var zero T
		return zero, Recovery{}, fmt.Errorf("stat corrupt %s: %w", path, err)
	}

	budgetMu.Lock()
	installErr := atomicWriteUnlocked(path, backupContent, 0o600, false)
	budgetMu.Unlock()
	if installErr != nil {
		var zero T
		return zero, Recovery{}, fmt.Errorf("restore %s from backup: %w", path, installErr)
	}
	return backup, recovery, nil
}

func decodeJSONFile[T any](path string, validate func(T) error) (T, []byte, error) {
	var value T
	content, err := os.ReadFile(path)
	if err != nil {
		return value, nil, err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return value, content, fmt.Errorf("%s is empty", path)
	}
	if err := json.Unmarshal(content, &value); err != nil {
		return value, content, fmt.Errorf("decode %s: %w", path, err)
	}
	if validate != nil {
		if err := validate(value); err != nil {
			return value, content, fmt.Errorf("validate %s: %w", path, err)
		}
	}
	return value, content, nil
}
