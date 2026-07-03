package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupVerifyRestore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	out := filepath.Join(t.TempDir(), "backups")
	os.MkdirAll(filepath.Join(root, "database"), 0o755)
	os.WriteFile(filepath.Join(root, "database", "state.json"), []byte(`{"ok":true}`), 0o600)
	path, manifest, err := Create(root, out, time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("manifest=%+v", manifest)
	}
	if _, err := Verify(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	report, err := Restore(path, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 1 {
		t.Fatalf("report=%+v", report)
	}
	content, err := os.ReadFile(filepath.Join(target, "database", "state.json"))
	if err != nil || string(content) != `{"ok":true}` {
		t.Fatalf("content=%q err=%v", content, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	file.Write([]byte("tamper"))
	file.Close()
	if _, err := Verify(path); err == nil {
		t.Fatal("tampered backup unexpectedly verified")
	}
}
