package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/normalization"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
)

type counters struct {
	Files             int
	RawEvents         int
	HistoryEvents     int
	OrderEvents       int
	HistoryStored     int
	HistoryDuplicates int
	OrdersStored      int
	OrderDuplicates   int
	SkippedTopics     int
}

func main() {
	if err := run(); err != nil {
		log.Printf("reprocess failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	inputDirectory := flag.String("input-dir", "./data/raw", "directory containing raw-ingest-*.jsonl")
	outputDirectory := flag.String("output-dir", "./data/normalized", "directory for normalized JSONL")
	catalogDirectory := flag.String("catalog-dir", "./catalog", "directory containing items.txt and markets.json")
	flag.Parse()

	itemCatalog, err := catalog.Load(
		filepath.Join(*catalogDirectory, "items.txt"),
		filepath.Join(*catalogDirectory, "markets.json"),
	)
	if err != nil {
		return err
	}
	store, err := normalizedjsonl.NewStore(*outputDirectory)
	if err != nil {
		return err
	}
	service, err := normalization.NewService(itemCatalog, store)
	if err != nil {
		return err
	}

	paths, err := filepath.Glob(filepath.Join(*inputDirectory, "raw-ingest-*.jsonl"))
	if err != nil {
		return fmt.Errorf("find raw files: %w", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return fmt.Errorf("no raw-ingest-*.jsonl files found in %s", *inputDirectory)
	}

	ctx := context.Background()
	stats := counters{}
	for _, path := range paths {
		if err := reprocessFile(ctx, service, path, &stats); err != nil {
			return err
		}
		stats.Files++
	}

	fmt.Printf("Reprocessing completed\n")
	fmt.Printf("  Files: %d\n", stats.Files)
	fmt.Printf("  Raw events: %d\n", stats.RawEvents)
	fmt.Printf("  History: %d events, %d stored, %d duplicates\n", stats.HistoryEvents, stats.HistoryStored, stats.HistoryDuplicates)
	fmt.Printf("  Orders: %d events, %d stored snapshots, %d duplicates\n", stats.OrderEvents, stats.OrdersStored, stats.OrderDuplicates)
	fmt.Printf("  Other topics skipped: %d\n", stats.SkippedTopics)
	return nil
}

func reprocessFile(ctx context.Context, service *normalization.Service, path string, stats *counters) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 20<<20)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var event domain.RawIngestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode %s line %d: %w", path, lineNumber, err)
		}
		if err := event.Validate(); err != nil {
			return fmt.Errorf("validate %s line %d: %w", path, lineNumber, err)
		}
		stats.RawEvents++
		switch event.Topic {
		case "markethistories.ingest":
			var upload domain.MarketHistoriesUpload
			if err := json.Unmarshal(event.Payload, &upload); err != nil {
				return fmt.Errorf("decode history %s line %d: %w", path, lineNumber, err)
			}
			capture := domain.CapturedHistory{
				SchemaVersion: 1,
				Source:        event.Source,
				Server:        event.Server,
				CapturedAt:    event.ReceivedAt,
				Payload:       upload,
			}
			_, stored, err := service.CaptureHistory(ctx, capture)
			if err != nil {
				return fmt.Errorf("normalize history %s line %d: %w", path, lineNumber, err)
			}
			stats.HistoryEvents++
			if stored {
				stats.HistoryStored++
			} else {
				stats.HistoryDuplicates++
			}
		case "marketorders.ingest":
			var upload domain.MarketOrdersUpload
			if err := json.Unmarshal(event.Payload, &upload); err != nil {
				return fmt.Errorf("decode orders %s line %d: %w", path, lineNumber, err)
			}
			result, err := service.CaptureOrders(ctx, event.Source, event.Server, event.ReceivedAt, upload)
			if err != nil {
				return fmt.Errorf("normalize orders %s line %d: %w", path, lineNumber, err)
			}
			stats.OrderEvents++
			stats.OrdersStored += result.Stored
			stats.OrderDuplicates += result.Duplicates
		default:
			stats.SkippedTopics++
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}
