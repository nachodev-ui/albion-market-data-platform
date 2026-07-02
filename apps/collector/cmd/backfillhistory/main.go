package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"albion-market-data/collector/internal/domain"
	"albion-market-data/collector/internal/secrets"
	"albion-market-data/collector/internal/upstream"
)

type options struct {
	inputDirectory   string
	baseURL          string
	token            string
	credentialSource string
	requireHTTPS     bool
	server           string
	from             time.Time
	to               time.Time
	batchSize        int
	maxBuckets       int
	requestsPerSec   float64
	timeout          time.Duration
	retryCount       int
	retryDelay       time.Duration
	useGzip          bool
	dryRun           bool
}

type counters struct {
	Files               int
	Lines               int
	Matched             int
	SkippedDate         int
	SkippedServer       int
	Invalid             int
	Batches             int
	Entries             int
	Buckets             int
	DuplicateBatches    int
	OriginalRowsTouched int64
	CurrentRowsTouched  int64
}

func main() {
	if err := run(); err != nil {
		log.Printf("history backfill failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	defaultFrom := today.AddDate(0, 0, -27).Format("2006-01-02")
	inputDirectory := flag.String("input-dir", "./data/normalized", "directory containing market-history-*.jsonl")
	baseURL := flag.String("base-url", envString("UPSTREAM_BASE_URL", "http://127.0.0.1:8080"), "central API base URL")
	token := flag.String("token", envString("UPSTREAM_TOKEN", ""), "central API bearer token; prefer --token-file")
	tokenFile := flag.String("token-file", envString("UPSTREAM_TOKEN_FILE", ""), "path to a file containing the central API bearer token")
	minimumTokenLength := flag.Int("min-token-length", envInt("UPSTREAM_MIN_TOKEN_LENGTH", 32), "minimum accepted bearer-token length")
	production := strings.EqualFold(envString("APP_ENV", "development"), "production")
	requireHTTPS := flag.Bool("require-https", envBool("UPSTREAM_REQUIRE_HTTPS", production), "require HTTPS for the central API")
	server := flag.String("server", "", "optional server filter: west, east or europe")
	fromText := flag.String("from", defaultFrom, "inclusive capture date in YYYY-MM-DD")
	toText := flag.String("to", today.Format("2006-01-02"), "inclusive capture date in YYYY-MM-DD")
	batchSize := flag.Int("batch-size", 100, "maximum history captures per request")
	maxBuckets := flag.Int("max-buckets", 100000, "maximum history buckets per request")
	requestsPerSec := flag.Float64("requests-per-second", 2, "maximum requests per second; 0 disables throttling")
	timeout := flag.Duration("timeout", 15*time.Second, "HTTP timeout per request")
	retryCount := flag.Int("retry-count", 3, "attempts per backfill batch")
	retryDelay := flag.Duration("retry-delay", 1*time.Second, "base delay between backfill retries")
	useGzip := flag.Bool("gzip", false, "compress requests using gzip")
	dryRun := flag.Bool("dry-run", false, "scan and validate without sending")
	flag.Parse()

	from, err := parseDate(*fromText)
	if err != nil {
		return fmt.Errorf("from: %w", err)
	}
	to, err := parseDate(*toText)
	if err != nil {
		return fmt.Errorf("to: %w", err)
	}
	if to.Before(from) {
		return fmt.Errorf("to must not be before from")
	}
	to = to.Add(24*time.Hour - time.Nanosecond)
	if *batchSize < 1 || *batchSize > 1000 {
		return fmt.Errorf("batch-size must be between 1 and 1000")
	}
	if *maxBuckets < 1 || *maxBuckets > 100000 {
		return fmt.Errorf("max-buckets must be between 1 and 100000")
	}
	if *retryCount < 1 || *retryCount > 20 {
		return fmt.Errorf("retry-count must be between 1 and 20")
	}
	if *retryDelay <= 0 {
		return fmt.Errorf("retry-delay must be greater than zero")
	}
	serverValue := strings.ToLower(strings.TrimSpace(*server))
	if serverValue != "" && serverValue != "west" && serverValue != "east" && serverValue != "europe" {
		return fmt.Errorf("server must be west, east or europe")
	}
	credentialValue := ""
	credentialSource := "disabled"
	if !*dryRun {
		credential, err := secrets.ResolveToken(secrets.ResolveOptions{
			Value:         *token,
			FilePath:      *tokenFile,
			MinimumLength: *minimumTokenLength,
			Production:    production,
		})
		if err != nil {
			return fmt.Errorf("configure central API credential: %w", err)
		}
		credentialValue = credential.Value()
		credentialSource = credential.Source()
	}

	opts := options{
		inputDirectory:   *inputDirectory,
		baseURL:          *baseURL,
		token:            credentialValue,
		credentialSource: credentialSource,
		requireHTTPS:     *requireHTTPS,
		server:           serverValue,
		from:             from,
		to:               to,
		batchSize:        *batchSize,
		maxBuckets:       *maxBuckets,
		requestsPerSec:   *requestsPerSec,
		timeout:          *timeout,
		retryCount:       *retryCount,
		retryDelay:       *retryDelay,
		useGzip:          *useGzip,
		dryRun:           *dryRun,
	}
	return execute(context.Background(), opts)
}

func execute(ctx context.Context, opts options) error {
	paths, err := filepath.Glob(filepath.Join(opts.inputDirectory, "market-history-*.jsonl"))
	if err != nil {
		return fmt.Errorf("find history files: %w", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return fmt.Errorf("no market-history-*.jsonl files found in %s", opts.inputDirectory)
	}

	var client *upstream.Client
	if !opts.dryRun {
		client, err = upstream.NewClientWithOptions(upstream.ClientOptions{
			BaseURL:      opts.baseURL,
			Token:        opts.token,
			Timeout:      opts.timeout,
			UseGzip:      opts.useGzip,
			RequireHTTPS: opts.requireHTTPS,
		})
		if err != nil {
			return err
		}
	}

	stats := counters{}
	entriesByServer := map[string][]upstream.HistoryIngest{}
	flush := func(server string, force bool) error {
		entries := entriesByServer[server]
		for len(entries) > 0 {
			count, buckets := batchBoundary(entries, opts.batchSize, opts.maxBuckets)
			if count == 0 {
				return fmt.Errorf("history entry exceeds max-buckets")
			}
			if !force && count == len(entries) && count < opts.batchSize && buckets < opts.maxBuckets {
				break
			}
			batch := append([]upstream.HistoryIngest(nil), entries[:count]...)
			entries = entries[count:]
			if err := sendBatch(ctx, client, opts, server, batch, &stats); err != nil {
				return err
			}
		}
		entriesByServer[server] = entries
		return nil
	}

	for _, path := range paths {
		if err := scanFile(path, opts, &stats, func(record domain.NormalizedHistory) error {
			entry, err := historyIngestFromNormalized(record)
			if err != nil {
				stats.Invalid++
				return fmt.Errorf("convert %s: %w", record.DedupeKey, err)
			}
			entriesByServer[record.Server] = append(entriesByServer[record.Server], entry)
			return flush(record.Server, false)
		}); err != nil {
			return err
		}
		stats.Files++
	}
	for server := range entriesByServer {
		if err := flush(server, true); err != nil {
			return err
		}
	}

	fmt.Printf("History backfill completed\n")
	fmt.Printf("  Mode: %s\n", map[bool]string{true: "dry-run", false: "send"}[opts.dryRun])
	fmt.Printf("  Credential source: %s, require HTTPS: %t\n", opts.credentialSource, opts.requireHTTPS)
	fmt.Printf("  Range: %s to %s\n", opts.from.Format("2006-01-02"), opts.to.Format("2006-01-02"))
	fmt.Printf("  Files: %d, lines: %d, matched: %d\n", stats.Files, stats.Lines, stats.Matched)
	fmt.Printf("  Batches: %d, entries: %d, buckets: %d\n", stats.Batches, stats.Entries, stats.Buckets)
	fmt.Printf("  Duplicate batches: %d\n", stats.DuplicateBatches)
	fmt.Printf("  Original rows touched: %d, current rows touched: %d\n", stats.OriginalRowsTouched, stats.CurrentRowsTouched)
	fmt.Printf("  Skipped by date: %d, skipped by server: %d, invalid: %d\n", stats.SkippedDate, stats.SkippedServer, stats.Invalid)
	return nil
}

func scanFile(path string, opts options, stats *counters, visit func(domain.NormalizedHistory) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 20<<20)
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		stats.Lines++
		var record domain.NormalizedHistory
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("decode %s line %d: %w", path, line, err)
		}
		if err := record.Validate(); err != nil {
			return fmt.Errorf("validate %s line %d: %w", path, line, err)
		}
		capturedAt := record.CapturedAt.UTC()
		if capturedAt.Before(opts.from) || capturedAt.After(opts.to) {
			stats.SkippedDate++
			continue
		}
		if opts.server != "" && record.Server != opts.server {
			stats.SkippedServer++
			continue
		}
		stats.Matched++
		if err := visit(record); err != nil {
			return fmt.Errorf("process %s line %d: %w", path, line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

func sendBatch(ctx context.Context, client *upstream.Client, opts options, server string, entries []upstream.HistoryIngest, stats *counters) error {
	payload := upstream.IngestHistoryRequest{
		Server:  server,
		Entries: entries,
	}
	payload.RequestID = deterministicRequestID(payload)
	buckets := historyBucketCount(entries)
	stats.Batches++
	stats.Entries += len(entries)
	stats.Buckets += buckets
	if opts.dryRun {
		fmt.Printf("dry-run batch=%s server=%s entries=%d buckets=%d\n", payload.RequestID, server, len(entries), buckets)
		return nil
	}

	var result upstream.HistorySendResult
	var err error
	for attempt := 1; attempt <= opts.retryCount; attempt++ {
		result, err = client.SendHistory(ctx, payload)
		if err == nil {
			break
		}
		if attempt == opts.retryCount {
			return fmt.Errorf("send batch %s after %d attempts: %w", payload.RequestID, attempt, err)
		}
		delay := time.Duration(attempt) * opts.retryDelay
		fmt.Printf("retry batch=%s attempt=%d retryIn=%s error=%v\n", payload.RequestID, attempt, delay, err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	originalRowsTouched, currentRowsTouched := rowsTouchedForAttempt(result.Response.Duplicate, result.Response.HistoryRowsTouched)
	if result.Response.Duplicate {
		stats.DuplicateBatches++
	}
	stats.OriginalRowsTouched += originalRowsTouched
	stats.CurrentRowsTouched += currentRowsTouched
	fmt.Printf("sent batch=%s server=%s entries=%d buckets=%d duplicate=%t originalRowsTouched=%d currentRowsTouched=%d status=%d\n",
		payload.RequestID, server, len(entries), buckets, result.Response.Duplicate, originalRowsTouched, currentRowsTouched, result.StatusCode)

	if opts.requestsPerSec > 0 {
		delay := time.Duration(float64(time.Second) / opts.requestsPerSec)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func rowsTouchedForAttempt(duplicate bool, reportedRowsTouched int64) (originalRowsTouched, currentRowsTouched int64) {
	if duplicate {
		return reportedRowsTouched, 0
	}
	return reportedRowsTouched, reportedRowsTouched
}

func batchBoundary(entries []upstream.HistoryIngest, maxEntries, maxBuckets int) (int, int) {
	count := 0
	buckets := 0
	for _, entry := range entries {
		entryBuckets := len(entry.History)
		if count > 0 && (count >= maxEntries || buckets+entryBuckets > maxBuckets) {
			break
		}
		if entryBuckets > maxBuckets {
			return 0, 0
		}
		count++
		buckets += entryBuckets
		if count >= maxEntries || buckets >= maxBuckets {
			break
		}
	}
	return count, buckets
}

func historyIngestFromNormalized(history domain.NormalizedHistory) (upstream.HistoryIngest, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(history.Location.ID), 10, 16)
	if err != nil || parsed <= 0 {
		return upstream.HistoryIngest{}, fmt.Errorf("location %q is not a positive numeric market identifier", history.Location.ID)
	}
	buckets := make([]upstream.HistoryBucketIngest, 0, len(history.History))
	for index, point := range history.History {
		if point.Timestamp.IsZero() || point.ItemCount < 0 {
			return upstream.HistoryIngest{}, fmt.Errorf("invalid bucket %d", index)
		}
		var average *int64
		if point.ItemCount > 0 && point.TotalSilver > 0 {
			value := point.TotalSilver / point.ItemCount
			if value > 0 {
				average = &value
			}
		}
		buckets = append(buckets, upstream.HistoryBucketIngest{
			Timestamp:        point.Timestamp.UTC(),
			ItemCount:        point.ItemCount,
			AverageUnitPrice: average,
		})
	}
	return upstream.HistoryIngest{
		ObservedAt: history.CapturedAt.UTC(),
		LocationID: int16(parsed),
		ItemKey:    history.Item.ID,
		Quality:    int16(history.Quality.ID),
		History:    buckets,
	}, nil
}

func deterministicRequestID(payload upstream.IngestHistoryRequest) string {
	payload.RequestID = ""
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	bytes := sum[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x50
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encodedHex := fmt.Sprintf("%x", bytes)
	return encodedHex[0:8] + "-" + encodedHex[8:12] + "-" + encodedHex[12:16] + "-" + encodedHex[16:20] + "-" + encodedHex[20:32]
}

func historyBucketCount(entries []upstream.HistoryIngest) int {
	total := 0
	for _, entry := range entries {
		total += len(entry.History)
	}
	return total
}

func parseDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("must use YYYY-MM-DD")
	}
	return parsed.UTC(), nil
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
