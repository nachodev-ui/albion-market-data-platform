package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/httpapi"
	"albion-market-data/collector/internal/httpingest"
	"albion-market-data/collector/internal/normalization"
	"albion-market-data/collector/internal/observability"
	"albion-market-data/collector/internal/storage/composite"
	"albion-market-data/collector/internal/storage/localdb"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
	"albion-market-data/collector/internal/storage/rawjsonl"
	"albion-market-data/collector/internal/upstream"
)

func main() {
	if err := run(); err != nil {
		log.Printf("receiver failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	loadDotEnv()
	startedAt := time.Now().UTC()

	upstreamEnabledDefault := envBool("UPSTREAM_ENABLED", false)
	upstreamHistoryEnabledDefault := envBool("UPSTREAM_HISTORY_ENABLED", upstreamEnabledDefault)

	listenAddress := flag.String("listen", envString("COLLECTOR_LISTEN", "127.0.0.1:8787"), "local HTTP address to listen on")
	dataDirectory := flag.String("data-dir", envString("COLLECTOR_DATA_DIR", "./data"), "root directory for raw and normalized storage")
	catalogDirectory := flag.String("catalog-dir", envString("COLLECTOR_CATALOG_DIR", "./catalog"), "directory containing items.txt and markets.json")
	serverName := flag.String("server", envString("ALBION_SERVER", "west"), "Albion server: west, east or europe")
	environment := flag.String("environment", envString("APP_ENV", "development"), "runtime environment name")
	logColor := flag.String("log-color", envString("LOG_COLOR", "auto"), "log colors: auto, always or never")
	databasePath := flag.String("database", envString("LOCAL_DATABASE_PATH", ""), "embedded local database path; defaults to <data-dir>/database/market-state.json")
	upstreamEnabled := flag.Bool("upstream-enabled", upstreamEnabledDefault, "forward normalized current-price snapshots to the shared upstream API")
	upstreamHistoryEnabled := flag.Bool("upstream-history-enabled", upstreamHistoryEnabledDefault, "forward normalized market history to the shared upstream API")
	upstreamBaseURL := flag.String("upstream-base-url", envString("UPSTREAM_BASE_URL", ""), "shared upstream API base URL")
	upstreamToken := flag.String("upstream-token", envString("UPSTREAM_TOKEN", ""), "bearer token used for the shared upstream API")
	upstreamBatchSize := flag.Int("upstream-batch-size", envInt("UPSTREAM_BATCH_SIZE", 500), "maximum number of price entries per upstream batch")
	upstreamFlushInterval := flag.Duration("upstream-flush-interval", envDuration("UPSTREAM_FLUSH_INTERVAL", 250*time.Millisecond), "maximum time before flushing a price batch")
	upstreamQueueSize := flag.Int("upstream-queue-size", envInt("UPSTREAM_QUEUE_SIZE", 5000), "buffer size for queued upstream price entries")
	upstreamHistoryBatchSize := flag.Int("upstream-history-batch-size", envInt("UPSTREAM_HISTORY_BATCH_SIZE", 100), "maximum number of history captures per upstream batch")
	upstreamHistoryMaxBatchBuckets := flag.Int("upstream-history-max-batch-buckets", envInt("UPSTREAM_HISTORY_MAX_BATCH_BUCKETS", 100000), "maximum number of history buckets per upstream batch")
	upstreamHistoryFlushInterval := flag.Duration("upstream-history-flush-interval", envDuration("UPSTREAM_HISTORY_FLUSH_INTERVAL", 500*time.Millisecond), "maximum time before flushing a history batch")
	upstreamHistoryQueueSize := flag.Int("upstream-history-queue-size", envInt("UPSTREAM_HISTORY_QUEUE_SIZE", 1000), "buffer size for queued upstream history captures")
	upstreamTimeout := flag.Duration("upstream-timeout", envDuration("UPSTREAM_TIMEOUT", 5*time.Second), "timeout for each upstream HTTP request")
	upstreamRetryCount := flag.Int("upstream-retry-count", envInt("UPSTREAM_RETRY_COUNT", 3), "number of attempts for failed upstream batches")
	upstreamRetryDelay := flag.Duration("upstream-retry-delay", envDuration("UPSTREAM_RETRY_DELAY", 500*time.Millisecond), "base delay between upstream retries")
	upstreamGzip := flag.Bool("upstream-gzip", envBool("UPSTREAM_GZIP", false), "compress upstream price and history batches using gzip")
	upstreamOutboxPath := flag.String("upstream-outbox", envString("UPSTREAM_OUTBOX_PATH", ""), "persistent outbox state path; defaults to <data-dir>/outbox/state.json")
	upstreamMaxDeliveryAttempts := flag.Int("upstream-max-delivery-attempts", envInt("UPSTREAM_MAX_DELIVERY_ATTEMPTS", 12), "maximum persisted delivery attempts before dead-letter")
	upstreamMaxRetryDelay := flag.Duration("upstream-max-retry-delay", envDuration("UPSTREAM_MAX_RETRY_DELAY", 5*time.Minute), "maximum persistent retry backoff")
	flag.Parse()

	rawDirectory := filepath.Join(*dataDirectory, "raw")
	normalizedDirectory := filepath.Join(*dataDirectory, "normalized")
	if *databasePath == "" {
		*databasePath = filepath.Join(*dataDirectory, "database", "market-state.json")
	}
	if *upstreamOutboxPath == "" {
		*upstreamOutboxPath = filepath.Join(*dataDirectory, "outbox", "state.json")
	}

	itemCatalog, err := catalog.Load(
		filepath.Join(*catalogDirectory, "items.txt"),
		filepath.Join(*catalogDirectory, "markets.json"),
	)
	if err != nil {
		return err
	}
	rawStore, err := rawjsonl.NewStore(rawDirectory)
	if err != nil {
		return err
	}
	auditStore, err := normalizedjsonl.NewStore(normalizedDirectory)
	if err != nil {
		return err
	}
	database, err := localdb.New(*databasePath)
	if err != nil {
		return err
	}
	imported, err := database.ImportNormalizedDirectory(context.Background(), normalizedDirectory)
	if err != nil {
		return fmt.Errorf("bootstrap local database: %w", err)
	}
	normalizedStore, err := composite.New(auditStore, database)
	if err != nil {
		return err
	}
	normalizer, err := normalization.NewService(itemCatalog, normalizedStore)
	if err != nil {
		return err
	}

	logger := observability.NewLogger(os.Stdout, *logColor)

	var priceForwarder *upstream.Forwarder
	var historyForwarder *upstream.HistoryForwarder
	var persistentOutbox *upstream.Outbox
	if *upstreamEnabled || *upstreamHistoryEnabled {
		if strings.TrimSpace(*upstreamToken) == "" {
			return fmt.Errorf("UPSTREAM_TOKEN is required when upstream forwarding is enabled")
		}
		client, err := upstream.NewClient(*upstreamBaseURL, *upstreamToken, *upstreamTimeout, *upstreamGzip)
		if err != nil {
			return fmt.Errorf("configure upstream client: %w", err)
		}
		persistentOutbox, err = upstream.NewOutbox(*upstreamOutboxPath)
		if err != nil {
			return fmt.Errorf("configure persistent outbox: %w", err)
		}
		if *upstreamEnabled {
			priceForwarder, err = upstream.NewForwarderWithOutbox(
				client,
				logger,
				*serverName,
				persistentOutbox,
				*upstreamQueueSize,
				*upstreamBatchSize,
				*upstreamFlushInterval,
				*upstreamRetryCount,
				*upstreamRetryDelay,
				*upstreamMaxDeliveryAttempts,
				*upstreamMaxRetryDelay,
			)
			if err != nil {
				return fmt.Errorf("configure upstream price forwarder: %w", err)
			}
		}
		if *upstreamHistoryEnabled {
			historyForwarder, err = upstream.NewHistoryForwarderWithOutbox(
				client,
				logger,
				*serverName,
				persistentOutbox,
				*upstreamHistoryQueueSize,
				*upstreamHistoryBatchSize,
				*upstreamHistoryMaxBatchBuckets,
				*upstreamHistoryFlushInterval,
				*upstreamRetryCount,
				*upstreamRetryDelay,
				*upstreamMaxDeliveryAttempts,
				*upstreamMaxRetryDelay,
			)
			if err != nil {
				return fmt.Errorf("configure upstream history forwarder: %w", err)
			}
		}
	}

	ingestHandler, err := httpingest.NewHandler(*serverName, rawStore, normalizer, priceForwarder, historyForwarder, logger)
	if err != nil {
		return err
	}
	apiHandler, err := httpapi.NewHandler(database, itemCatalog, httpapi.StatusConfig{
		ServiceName:                   "albion-market-data-platform",
		Environment:                   *environment,
		StartedAt:                     startedAt,
		Forwarder:                     priceForwarder,
		ForwarderQueueCapacity:        *upstreamQueueSize,
		HistoryForwarder:              historyForwarder,
		HistoryForwarderQueueCapacity: *upstreamHistoryQueueSize,
	})
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", apiHandler)
	mux.Handle("/", ingestHandler)

	server := &http.Server{
		Addr:              *listenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	absoluteRawDirectory, _ := filepath.Abs(rawDirectory)
	absoluteNormalizedDirectory, _ := filepath.Abs(normalizedDirectory)
	absoluteDatabasePath, _ := filepath.Abs(*databasePath)
	logger.Event(
		observability.LevelOK,
		"receiver.started",
		observability.F("address", *listenAddress),
		observability.F("environment", *environment),
		observability.F("health", "http://"+*listenAddress+"/healthz"),
		observability.F("status", "http://"+*listenAddress+"/api/v1/status"),
		observability.F("raw_directory", absoluteRawDirectory),
		observability.F("normalized_directory", absoluteNormalizedDirectory),
		observability.F("database", absoluteDatabasePath),
		observability.F("histories_imported", imported.HistoryImported),
		observability.F("orders_imported", imported.OrdersImported),
		observability.F("color", *logColor),
	)
	if priceForwarder != nil {
		logger.Event(
			observability.LevelInfo,
			"upstream.price_configured",
			observability.F("base_url", *upstreamBaseURL),
			observability.F("batch_size", *upstreamBatchSize),
			observability.F("flush_interval", *upstreamFlushInterval),
			observability.F("queue_capacity", *upstreamQueueSize),
			observability.F("attempts_per_cycle", *upstreamRetryCount),
			observability.F("max_delivery_attempts", *upstreamMaxDeliveryAttempts),
			observability.F("max_retry_delay", *upstreamMaxRetryDelay),
			observability.F("outbox", *upstreamOutboxPath),
			observability.F("gzip", *upstreamGzip),
			observability.F("auth", "token"),
		)
	}
	if historyForwarder != nil {
		logger.Event(
			observability.LevelInfo,
			"upstream.history_configured",
			observability.F("base_url", *upstreamBaseURL),
			observability.F("batch_size", *upstreamHistoryBatchSize),
			observability.F("max_batch_buckets", *upstreamHistoryMaxBatchBuckets),
			observability.F("flush_interval", *upstreamHistoryFlushInterval),
			observability.F("queue_capacity", *upstreamHistoryQueueSize),
			observability.F("attempts_per_cycle", *upstreamRetryCount),
			observability.F("max_delivery_attempts", *upstreamMaxDeliveryAttempts),
			observability.F("max_retry_delay", *upstreamMaxRetryDelay),
			observability.F("outbox", *upstreamOutboxPath),
			observability.F("gzip", *upstreamGzip),
			observability.F("auth", "token"),
		)
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	forwarderContext := context.Background()
	if priceForwarder != nil {
		priceForwarder.Start(forwarderContext)
		defer priceForwarder.Stop()
	}
	if historyForwarder != nil {
		historyForwarder.Start(forwarderContext)
		defer historyForwarder.Stop()
	}

	serverError := make(chan error, 1)
	go func() {
		serverError <- server.ListenAndServe()
	}()

	select {
	case <-shutdownContext.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		logger.Event(observability.LevelInfo, "receiver.stopped")
		return nil
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	}
}

func loadDotEnv() {
	candidates := []string{".env"}

	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		candidates = append(
			candidates,
			filepath.Join(executableDir, ".env"),
			filepath.Join(executableDir, "..", ".env"),
		)
	}

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		if err := loadEnvFile(candidate); err == nil {
			return
		}
	}
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if value[0] == '\'' && value[len(value)-1] == '\'' {
				value = value[1 : len(value)-1]
			} else if value[0] == '"' && value[len(value)-1] == '"' {
				if unquoted, unquoteErr := strconv.Unquote(value); unquoteErr == nil {
					value = unquoted
				}
			}
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
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

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
