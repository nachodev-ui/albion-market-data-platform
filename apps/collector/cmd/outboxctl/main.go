package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"albion-market-data/collector/internal/upstream"
)

func main() {
	if err := run(); err != nil {
		log.Printf("outboxctl failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("path", "./data/outbox/state.json", "persistent outbox state path")
	action := flag.String("action", "list", "action: list, requeue or purge")
	pipeline := flag.String("pipeline", "", "optional pipeline filter: prices or history")
	requestID := flag.String("request-id", "", "dead-letter request ID for requeue or purge")
	flag.Parse()

	var selected upstream.Pipeline
	switch strings.ToLower(strings.TrimSpace(*pipeline)) {
	case "":
	case "prices":
		selected = upstream.PipelinePrices
	case "history":
		selected = upstream.PipelineHistory
	default:
		return fmt.Errorf("pipeline must be prices or history")
	}

	outbox, err := upstream.NewOutbox(*path)
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(*action)) {
	case "list":
		content, err := json.MarshalIndent(outbox.ListDeadLetters(selected), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(content))
		return nil
	case "requeue":
		if strings.TrimSpace(*requestID) == "" {
			return fmt.Errorf("request-id is required for requeue")
		}
		if err := outbox.RequeueDeadLetter(strings.TrimSpace(*requestID)); err != nil {
			return err
		}
		fmt.Printf("requeued %s\n", strings.TrimSpace(*requestID))
		return nil
	case "purge":
		if strings.TrimSpace(*requestID) == "" {
			return fmt.Errorf("request-id is required for purge")
		}
		if err := outbox.PurgeDeadLetter(strings.TrimSpace(*requestID)); err != nil {
			return err
		}
		fmt.Printf("purged %s\n", strings.TrimSpace(*requestID))
		return nil
	default:
		return fmt.Errorf("action must be list, requeue or purge")
	}
}
