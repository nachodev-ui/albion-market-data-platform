package durable

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

func RecoverAndRefreshJSONBackup(path string) (Recovery, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if _, backupErr := os.Stat(path + ".bak"); errors.Is(backupErr, os.ErrNotExist) {
			return Recovery{}, nil
		}
		_, recovery, loadErr := LoadJSONWithBackup[json.RawMessage](path, nil)
		return recovery, loadErr
	}
	if err != nil {
		return Recovery{}, fmt.Errorf("read %s: %w", path, err)
	}
	if !json.Valid(content) {
		_, recovery, loadErr := LoadJSONWithBackup[json.RawMessage](path, nil)
		return recovery, loadErr
	}
	budgetMu.Lock()
	err = atomicWriteUnlocked(path+".bak", content, 0o600, false)
	budgetMu.Unlock()
	if err != nil {
		return Recovery{}, fmt.Errorf("refresh backup for %s: %w", path, err)
	}
	return Recovery{}, nil
}
