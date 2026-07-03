package upstream

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutboxRecoversCorruptPrimaryFromBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox", "state.json")
	outbox, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.EnqueuePrice("west", PriceIngest{ItemKey: "T4_TEST", LocationID: 4002, Quality: 1}, 10); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Depth(PipelinePrices); got != 1 {
		t.Fatalf("depth=%d", got)
	}
}

func TestDeadLetterPersistsAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	outbox, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.EnqueuePrice("west", PriceIngest{ItemKey: "T4_TEST", LocationID: 4002, Quality: 1}, 10); err != nil {
		t.Fatal(err)
	}
	claimed, err := outbox.ClaimPriceBatch("west", 10)
	if err != nil || claimed == nil {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
	if err := outbox.DeadLetter(claimed.RequestID, 1, 401, "unauthorized"); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	letters := reopened.ListDeadLetters(PipelinePrices)
	if len(letters) != 1 || letters[0].RequestID != claimed.RequestID {
		t.Fatalf("letters=%+v", letters)
	}
}
