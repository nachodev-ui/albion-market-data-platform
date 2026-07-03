package durable

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendJSONLinesWritesCompleteBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "batch.jsonl")
	values := []map[string]int{{"id": 1}, {"id": 2}, {"id": 3}}
	if err := AppendJSONLines(path, values); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	seen := 0
	for scanner.Scan() {
		var value map[string]int
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		seen++
		if value["id"] != seen {
			t.Fatalf("line %d id=%d", seen, value["id"])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != len(values) {
		t.Fatalf("lines=%d want=%d", seen, len(values))
	}
}

func TestAppendJSONLinesEmptyBatchDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := AppendJSONLines(path, []map[string]int(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat err=%v", err)
	}
}
