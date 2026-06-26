package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/normalization"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
)

func main() {
	if err := run(); err != nil {
		log.Printf("collector failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	inputPath := flag.String("input", "", "path to a captured-history JSON file")
	dataDirectory := flag.String("data-dir", "./data/test/normalized", "directory for normalized JSONL storage")
	catalogDirectory := flag.String("catalog-dir", "./catalog", "directory containing items.txt and markets.json")
	flag.Parse()

	if *inputPath == "" {
		return errors.New("missing required -input argument")
	}

	capture, err := readCapture(*inputPath)
	if err != nil {
		return err
	}
	itemCatalog, err := catalog.Load(
		filepath.Join(*catalogDirectory, "items.txt"),
		filepath.Join(*catalogDirectory, "markets.json"),
	)
	if err != nil {
		return err
	}
	store, err := normalizedjsonl.NewStore(*dataDirectory)
	if err != nil {
		return err
	}
	service, err := normalization.NewService(itemCatalog, store)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	normalized, stored, err := service.CaptureHistory(ctx, capture)
	if err != nil {
		return err
	}

	absoluteDataDirectory, _ := filepath.Abs(*dataDirectory)
	fmt.Printf("Normalized market history saved successfully\n")
	fmt.Printf("  Item: %s (%s)\n", normalized.Item.ID, normalized.Item.Name)
	fmt.Printf("  Albion ID: %d\n", normalized.Item.AlbionID)
	fmt.Printf("  Location: %s (%s)\n", normalized.Location.ID, normalized.Location.Name)
	fmt.Printf("  Quality: %d (%s)\n", normalized.Quality.ID, normalized.Quality.Name)
	fmt.Printf("  Period: %s\n", normalized.Period)
	fmt.Printf("  Active buckets: %d\n", normalized.Summary.ActiveBuckets)
	fmt.Printf("  Sold units: %d\n", normalized.Summary.SoldUnits)
	fmt.Printf("  Weighted average: %.3f silver\n", normalized.Summary.WeightedAverageUnitPrice)
	fmt.Printf("  Stored: %t\n", stored)
	fmt.Printf("  Data directory: %s\n", absoluteDataDirectory)
	return nil
}

func readCapture(path string) (domain.CapturedHistory, error) {
	file, err := os.Open(path)
	if err != nil {
		return domain.CapturedHistory{}, fmt.Errorf("open input: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 10<<20))
	decoder.DisallowUnknownFields()

	var capture domain.CapturedHistory
	if err := decoder.Decode(&capture); err != nil {
		return domain.CapturedHistory{}, fmt.Errorf("decode input: %w", err)
	}
	return capture, nil
}
