package observability

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageUsageMeasuresConfiguredAreas(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "raw")
	normalized := filepath.Join(root, "normalized")
	if err := os.MkdirAll(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(normalized, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := map[string][]byte{
		filepath.Join(raw, "raw.jsonl"):             []byte("12345"),
		filepath.Join(normalized, "orders.jsonl"):   []byte("1234567"),
		filepath.Join(root, "market-state.json"):    []byte("123"),
		filepath.Join(root, "outbox", "state.json"): []byte("123456789"),
	}
	for path, content := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	usage := NewStorageUsage(StoragePaths{
		RawDirectory:        raw,
		NormalizedDirectory: normalized,
		DatabasePath:        filepath.Join(root, "market-state.json"),
		OutboxPath:          filepath.Join(root, "outbox", "state.json"),
		MaxBytes:            100,
	}, time.Second)
	snapshot := usage.Snapshot()
	if snapshot.RawBytes != 5 || snapshot.NormalizedBytes != 7 || snapshot.DatabaseBytes != 3 || snapshot.OutboxBytes != 9 || snapshot.TotalBytes != 24 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if snapshot.MaxBytes != 100 || len(snapshot.Errors) != 0 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}
