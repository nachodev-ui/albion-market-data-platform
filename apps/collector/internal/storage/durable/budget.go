package durable

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	budgetMu   sync.Mutex
	budgetRoot string
	budgetMax  int64
)

var ErrStorageLimit = errors.New("local storage limit exceeded")

func ConfigureBudget(root string, maxBytes int64) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve storage root: %w", err)
	}
	budgetMu.Lock()
	defer budgetMu.Unlock()
	budgetRoot = filepath.Clean(absolute)
	budgetMax = maxBytes
	return nil
}

func DirectorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return total, err
}

func checkAppendLocked(path string, additional int64) error {
	return checkDeltaLocked(path, additional)
}

func checkReplaceLocked(path string, replacement int64) error {
	current := int64(0)
	if info, err := os.Stat(path); err == nil {
		current = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	delta := replacement - current
	if delta < 0 {
		delta = 0
	}
	return checkDeltaLocked(path, delta)
}

func checkDeltaLocked(path string, delta int64) error {
	if budgetMax <= 0 || budgetRoot == "" {
		return nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(budgetRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil
	}
	size, err := DirectorySize(budgetRoot)
	if err != nil {
		return fmt.Errorf("measure local storage: %w", err)
	}
	if size+delta > budgetMax {
		return fmt.Errorf("%w: current=%d projected=%d maximum=%d", ErrStorageLimit, size, size+delta, budgetMax)
	}
	return nil
}
