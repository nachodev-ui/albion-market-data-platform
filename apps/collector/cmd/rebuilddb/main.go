package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"albion-market-data/collector/internal/storage/localdb"
)

func main() {
	if err := run(); err != nil {
		log.Printf("rebuild database failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	normalizedDirectory := flag.String("normalized-dir", "./data/normalized", "directory containing normalized JSONL files")
	databasePath := flag.String("database", "./data/database/market-state.json", "embedded local database path")
	reset := flag.Bool("reset", false, "remove the existing projection before importing")
	flag.Parse()

	if *reset {
		if err := os.Remove(*databasePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove existing local database: %w", err)
		}
	}

	store, err := localdb.New(*databasePath)
	if err != nil {
		return err
	}
	result, err := store.ImportNormalizedDirectory(context.Background(), *normalizedDirectory)
	if err != nil {
		return err
	}
	stats := store.Stats()

	fmt.Printf("Local database ready\n")
	fmt.Printf("  Database: %s\n", *databasePath)
	fmt.Printf("  Imported now: %d histories, %d orders\n", result.HistoryImported, result.OrdersImported)
	fmt.Printf("  Persisted total: %d histories, %d orders\n", stats.HistorySnapshots, stats.OrderSnapshots)
	return nil
}
