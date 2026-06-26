package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RawIngestEvent preserves exactly what an AoData-compatible client sent to
// the local HTTP receiver. Keeping the raw payload lets us reprocess data later
// without requiring the player to load the market screen again.
type RawIngestEvent struct {
	SchemaVersion int             `json:"schemaVersion"`
	Source        string          `json:"source"`
	Server        string          `json:"server"`
	Topic         string          `json:"topic"`
	ReceivedAt    time.Time       `json:"receivedAt"`
	Payload       json.RawMessage `json:"payload"`
}

func (e RawIngestEvent) Validate() error {
	if e.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must be 1, got %d", e.SchemaVersion)
	}
	if strings.TrimSpace(e.Source) == "" {
		return errors.New("source is required")
	}
	if e.Server != "west" && e.Server != "east" && e.Server != "europe" {
		return fmt.Errorf("unsupported server %q", e.Server)
	}
	if strings.TrimSpace(e.Topic) == "" {
		return errors.New("topic is required")
	}
	if e.ReceivedAt.IsZero() {
		return errors.New("receivedAt is required")
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) {
		return errors.New("payload must contain valid JSON")
	}
	return nil
}
