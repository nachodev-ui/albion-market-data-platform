package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"albion-market-data/collector/internal/catalog"
	"albion-market-data/collector/internal/httpapi"
	"albion-market-data/collector/internal/httpingest"
	"albion-market-data/collector/internal/normalization"
	"albion-market-data/collector/internal/observability"
	"albion-market-data/collector/internal/secrets"
	"albion-market-data/collector/internal/storage/composite"
	"albion-market-data/collector/internal/storage/localdb"
	"albion-market-data/collector/internal/storage/normalizedjsonl"
	"albion-market-data/collector/internal/storage/rawjsonl"
	"albion-market-data/collector/internal/upstream"
)

func run() error {
	loadDotEnv()
	startedAt := time.Now().UTC()

	upstreamEnabledDefault := envBool("UPSTREAM_ENABLED", false)
	upstreamHistoryEnabledDefault := envBool("UPSTREAM_HISTORY_ENABLED", upstreamEnabledDefault)
	upstreamRequireHTTPSDefault := strings.EqualFold(envString("APP_ENV", "development"), "production")

	listenAddress := flag.String("listen", envString("COLLECTOR_LISTEN", "127.0.0.1:8787"), "local HTTP address to listen on")
	allowRemoteListen := flag.Bool("allow-remote-listen", envBool("COLLECTOR_ALLOW_REMOTE", false), "allow the receiver HTTP server to bind to a non-loopback interface")
	maxHeaderBytes := flag.Int("max-header-bytes", envInt("COLLECTOR_MAX_HEADER_BYTES", 64<<10), "maximum HTTP request-header bytes")
	allowedOriginsText := flag.String("allowed-origins", envString("LOCAL_API_ALLOWED_ORIGINS", "http://127.0.0.1:5173,http://localhost:5173"), "comma-separated browser origins allowed to read the local API")
	dataDirectory := flag.String("data-dir", envString("COLLECTOR_DATA_DIR", "./data"), "root directory for raw and normalized storage")
	catalogDirectory := flag.String("catalog-dir", envString("COLLECTOR_CATALOG_DIR", "./catalog"), "directory containing items.txt and markets.json")
	serverName := flag.String("server", envString("ALBION_SERVER", "west"), "Albion server: west, east or europe")
	environment := flag.String("environment", envString("APP_ENV", "development"), "runtime environment name")
	logColor := flag.String("log-color", envString("LOG_COLOR", "auto"), "log colors: auto, always or never")
	databasePath := flag.String("database", envString("LOCAL_DATABASE_PATH", ""), "embedded local database path; defaults to <data-dir>/database/market-state.json")
	upstreamEnabled := flag.Bool("upstream-enabled", upstreamEnabledDefault, "forward normalized current-price snapshots to the shared upstream API")
	upstreamHistoryEnabled := flag.Bool("upstream-history-enabled", upstreamHistoryEnabledDefault, "forward normalized market history to the shared upstream API")
	upstreamBaseURL := flag.String("upstream-base-url", envString("UPSTREAM_BASE_URL", ""), "shared upstream API base URL")
	upstreamToken := flag.String("upstream-token", envString("UPSTREAM_TOKEN", ""), "bearer token used for the shared upstream API; prefer --upstream-token-file")
	upstreamTokenFile := flag.String("upstream-token-file", envString("UPSTREAM_TOKEN_FILE", ""), "path to a file containing the upstream bearer token")
	upstreamMinTokenLength := flag.Int("upstream-min-token-length", envInt("UPSTREAM_MIN_TOKEN_LENGTH", 32), "minimum accepted bearer-token length")
	upstreamRequireHTTPS := flag.Bool("upstream-require-https", envBool("UPSTREAM_REQUIRE_HTTPS", upstreamRequireHTTPSDefault), "require HTTPS for upstream requests")
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

	if err := validateListenAddress(*listenAddress, *allowRemoteListen); err != nil {
		return err
	}
	if *maxHeaderBytes < 1024 || *maxHeaderBytes > 1<<20 {
		return fmt.Errorf("COLLECTOR_MAX_HEADER_BYTES must be between 1024 and 1048576")
	}
	allowedOrigins, err := parseAllowedOrigins(*allowedOriginsText)
	if err != nil {
		return fmt.Errorf("configure local API origins: %w", err)
	}

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
	upstreamCredentialSource := "disabled"
	if *upstreamEnabled || *upstreamHistoryEnabled {
		credential, err := secrets.ResolveToken(secrets.ResolveOptions{
			Value:         *upstreamToken,
			FilePath:      *upstreamTokenFile,
			MinimumLength: *upstreamMinTokenLength,
			Production:    strings.EqualFold(*environment, "production"),
		})
		if err != nil {
			return fmt.Errorf("configure upstream credential: %w", err)
		}
		upstreamCredentialSource = credential.Source()
		client, err := upstream.NewClientWithOptions(upstream.ClientOptions{
			BaseURL:      *upstreamBaseURL,
			Token:        credential.Value(),
			Timeout:      *upstreamTimeout,
			UseGzip:      *upstreamGzip,
			RequireHTTPS: *upstreamRequireHTTPS,
		})
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
		AllowedOrigins:                allowedOrigins,
	})
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle(httpapi.RouteHealth, apiHandler)
	mux.Handle(httpapi.RouteReady, apiHandler)
	mux.Handle("/api/v1/", apiHandler)
	mux.Handle("/", ingestHandler)

	server := &http.Server{
		Addr:              *listenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    *maxHeaderBytes,
	}

	absoluteRawDirectory, _ := filepath.Abs(rawDirectory)
	absoluteNormalizedDirectory, _ := filepath.Abs(normalizedDirectory)
	absoluteDatabasePath, _ := filepath.Abs(*databasePath)
	logger.Event(
		observability.LevelOK,
		"receiver.started",
		observability.F("address", *listenAddress),
		observability.F("environment", *environment),
		observability.F("health", "http://"+*listenAddress+httpapi.RouteHealth),
		observability.F("readiness", "http://"+*listenAddress+httpapi.RouteReady),
		observability.F("status", "http://"+*listenAddress+httpapi.RouteStatus),
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
			observability.F("credential_source", upstreamCredentialSource),
			observability.F("require_https", *upstreamRequireHTTPS),
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
			observability.F("credential_source", upstreamCredentialSource),
			observability.F("require_https", *upstreamRequireHTTPS),
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
