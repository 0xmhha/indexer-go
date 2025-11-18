package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xmhha/indexer-go/api"
	"github.com/0xmhha/indexer-go/client"
	"github.com/0xmhha/indexer-go/events"
	"github.com/0xmhha/indexer-go/fetch"
	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/logger"
	"github.com/0xmhha/indexer-go/storage"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

var (
	// Version information (injected at build time)
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	// Define command-line flags
	var (
		configFile    = flag.String("config", "", "Path to configuration file (YAML)")
		showVersion   = flag.Bool("version", false, "Show version information and exit")
		rpcEndpoint   = flag.String("rpc", "", "Ethereum RPC endpoint URL")
		dbPath        = flag.String("db", "", "Database path")
		startHeight   = flag.Uint64("start-height", 0, "Block height to start indexing from")
		workers       = flag.Int("workers", 100, "Number of concurrent workers")
		batchSize     = flag.Int("batch-size", 100, "Number of blocks per batch")
		logLevel      = flag.String("log-level", "", "Log level (debug, info, warn, error)")
		logFormat     = flag.String("log-format", "", "Log format (json, console)")
		enableGapMode = flag.Bool("gap-recovery", false, "Enable gap detection and recovery at startup")

		// API server flags
		enableAPI       = flag.Bool("api", false, "Enable API server")
		apiHost         = flag.String("api-host", "", "API server host")
		apiPort         = flag.Int("api-port", 0, "API server port")
		enableGraphQL   = flag.Bool("graphql", false, "Enable GraphQL API")
		enableJSONRPC   = flag.Bool("jsonrpc", false, "Enable JSON-RPC API")
		enableWebSocket = flag.Bool("websocket", false, "Enable WebSocket API")
	)

	flag.Parse()

	// Show version and exit if requested
	if *showVersion {
		fmt.Printf("indexer-go version %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", buildTime)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := loadConfig(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Override config with command-line flags
	applyFlags(cfg, *rpcEndpoint, *dbPath, *startHeight, *workers, *batchSize, *logLevel, *logFormat)
	applyAPIFlags(cfg, *enableAPI, *apiHost, *apiPort, *enableGraphQL, *enableJSONRPC, *enableWebSocket)

	// Validate configuration
	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log, err := initLogger(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	// Log startup information
	log.Info("Starting indexer",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("build_time", buildTime),
		zap.String("rpc_endpoint", cfg.RPC.Endpoint),
		zap.String("db_path", cfg.Database.Path),
		zap.Uint64("start_height", cfg.Indexer.StartHeight),
		zap.Int("workers", cfg.Indexer.Workers),
		zap.Int("batch_size", cfg.Indexer.ChunkSize),
		zap.Bool("gap_recovery", *enableGapMode),
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Initialize components
	log.Info("Initializing components...")

	// Initialize Ethereum client
	ethClient, err := client.NewClient(&client.Config{
		Endpoint: cfg.RPC.Endpoint,
		Timeout:  cfg.RPC.Timeout,
		Logger:   log,
	})
	if err != nil {
		log.Fatal("Failed to create Ethereum client", zap.Error(err))
	}
	defer ethClient.Close()

	log.Info("Connected to Ethereum node",
		zap.String("endpoint", cfg.RPC.Endpoint),
	)

	// Test connection
	chainID, err := ethClient.GetChainID(ctx)
	if err != nil {
		log.Fatal("Failed to get chain ID", zap.Error(err))
	}
	log.Info("Connected to chain",
		zap.String("chain_id", chainID.String()),
	)

	// Initialize storage
	storageConfig := storage.DefaultConfig(cfg.Database.Path)
	storageConfig.ReadOnly = false
	store, err := storage.NewPebbleStorage(storageConfig)
	if err != nil {
		log.Fatal("Failed to create storage", zap.Error(err))
	}
	store.SetLogger(log)
	defer func() {
		if err := store.Close(); err != nil {
			log.Error("Failed to close storage", zap.Error(err))
		}
	}()

	log.Info("Storage initialized",
		zap.String("path", cfg.Database.Path),
	)

	// Get latest indexed height
	latestHeight, err := store.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			log.Info("No blocks indexed yet, starting from configured height",
				zap.Uint64("start_height", cfg.Indexer.StartHeight),
			)
		} else {
			log.Warn("Failed to get latest indexed block",
				zap.Error(err),
			)
		}
	} else {
		log.Info("Resuming from latest indexed block",
			zap.Uint64("latest_height", latestHeight),
		)
	}

	// Initialize EventBus
	eventBus := events.NewEventBus(1000, 100)
	go eventBus.Run()
	defer eventBus.Stop()

	log.Info("EventBus initialized",
		zap.Int("publish_buffer", 1000),
		zap.Int("subscribe_buffer", 100),
	)

	// Initialize fetcher
	fetcherConfig := &fetch.Config{
		StartHeight: cfg.Indexer.StartHeight,
		BatchSize:   cfg.Indexer.ChunkSize,
		MaxRetries:  3,
		RetryDelay:  time.Second * 5,
		NumWorkers:  cfg.Indexer.Workers,
	}

	fetcher := fetch.NewFetcher(ethClient, store, fetcherConfig, log, eventBus)

	log.Info("Fetcher initialized, starting indexing...")

	// Initialize and start API server if enabled
	var apiServer *api.Server
	if cfg.API.Enabled {
		log.Info("Initializing API server...")

		apiConfig := &api.Config{
			Host:                  cfg.API.Host,
			Port:                  cfg.API.Port,
			ReadTimeout:           15 * time.Second,
			WriteTimeout:          15 * time.Second,
			IdleTimeout:           60 * time.Second,
			EnableCORS:            cfg.API.EnableCORS,
			AllowedOrigins:        cfg.API.AllowedOrigins,
			MaxHeaderBytes:        1 << 20, // 1 MB
			EnableGraphQL:         cfg.API.EnableGraphQL,
			EnableJSONRPC:         cfg.API.EnableJSONRPC,
			EnableWebSocket:       cfg.API.EnableWebSocket,
			GraphQLPath:           "/graphql",
			GraphQLPlaygroundPath: "/playground",
			JSONRPCPath:           "/rpc",
			WebSocketPath:         "/ws",
			ShutdownTimeout:       30 * time.Second,
		}

		var err error
		apiServer, err = api.NewServer(apiConfig, log, store)
		if err != nil {
			log.Fatal("Failed to create API server", zap.Error(err))
		}

		// Start API server in goroutine
		go func() {
			if err := apiServer.Start(); err != nil {
				log.Error("API server failed", zap.Error(err))
			}
		}()

		log.Info("API server started",
			zap.String("address", apiConfig.Address()),
			zap.Bool("graphql", apiConfig.EnableGraphQL),
			zap.Bool("jsonrpc", apiConfig.EnableJSONRPC),
			zap.Bool("websocket", apiConfig.EnableWebSocket),
		)
	}

	// Start fetcher in goroutine
	errChan := make(chan error, 1)
	go func() {
		if *enableGapMode {
			log.Info("Starting with gap recovery enabled")
			errChan <- fetcher.RunWithGapRecovery(ctx)
		} else {
			log.Info("Starting normal indexing mode")
			errChan <- fetcher.Run(ctx)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		log.Info("Received shutdown signal",
			zap.String("signal", sig.String()),
		)
		cancel() // Cancel context to stop fetcher
	case err := <-errChan:
		if err != nil && err != context.Canceled {
			log.Error("Fetcher stopped with error", zap.Error(err))
		}
	}

	// Wait a bit for graceful shutdown
	log.Info("Shutting down gracefully...")

	// Stop API server if it was started
	if apiServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := apiServer.Stop(shutdownCtx); err != nil {
			log.Error("Failed to stop API server gracefully", zap.Error(err))
		}
	}

	time.Sleep(time.Second * 2)

	// Get final statistics
	finalHeight, err := store.GetLatestHeight(ctx)
	if err == nil {
		log.Info("Final statistics",
			zap.Uint64("latest_height", finalHeight),
		)
	} else if !errors.Is(err, storage.ErrNotFound) {
		log.Warn("Failed to read final indexed height",
			zap.Error(err),
		)
	}

	log.Info("Indexer stopped")
}

// loadConfig loads configuration from file and environment variables
func loadConfig(configFile string) (*config.Config, error) {
	if err := loadDotEnv(); err != nil {
		return nil, err
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	return cfg, nil
}

// loadDotEnv loads environment variables from a .env file if it exists.
func loadDotEnv() error {
	info, err := os.Stat(".env")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to stat .env: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf(".env exists but is a directory")
	}
	if err := godotenv.Load(".env"); err != nil {
		return fmt.Errorf("failed to load .env: %w", err)
	}
	return nil
}

// applyFlags applies command-line flags to configuration
func applyFlags(cfg *config.Config, rpcEndpoint, dbPath string, startHeight uint64, workers, batchSize int, logLevel, logFormat string) {
	if rpcEndpoint != "" {
		cfg.RPC.Endpoint = rpcEndpoint
	}
	if dbPath != "" {
		cfg.Database.Path = dbPath
	}
	if startHeight > 0 {
		cfg.Indexer.StartHeight = startHeight
	}
	if workers > 0 {
		cfg.Indexer.Workers = workers
	}
	if batchSize > 0 {
		cfg.Indexer.ChunkSize = batchSize
	}
	if logLevel != "" {
		cfg.Log.Level = logLevel
	}
	if logFormat != "" {
		cfg.Log.Format = logFormat
	}
}

// applyAPIFlags applies API-related command-line flags to configuration
func applyAPIFlags(cfg *config.Config, enableAPI bool, apiHost string, apiPort int, enableGraphQL, enableJSONRPC, enableWebSocket bool) {
	if enableAPI {
		cfg.API.Enabled = true
	}
	if apiHost != "" {
		cfg.API.Host = apiHost
	}
	if apiPort > 0 {
		cfg.API.Port = apiPort
	}
	if enableGraphQL {
		cfg.API.EnableGraphQL = true
	}
	if enableJSONRPC {
		cfg.API.EnableJSONRPC = true
	}
	if enableWebSocket {
		cfg.API.EnableWebSocket = true
	}
}

// validateConfig validates the configuration
func validateConfig(cfg *config.Config) error {
	if cfg.RPC.Endpoint == "" {
		return fmt.Errorf("RPC endpoint is required (use --rpc or set INDEXER_RPC_ENDPOINT)")
	}
	if cfg.Database.Path == "" {
		return fmt.Errorf("database path is required (use --db or set INDEXER_DATABASE_PATH)")
	}
	if cfg.Indexer.Workers <= 0 {
		return fmt.Errorf("workers must be positive")
	}
	if cfg.Indexer.ChunkSize <= 0 {
		return fmt.Errorf("batch size must be positive")
	}
	return nil
}

// initLogger initializes the logger based on configuration
func initLogger(level, format string) (*zap.Logger, error) {
	if format == "json" || format == "production" {
		return logger.NewProduction()
	}

	// Default to development logger
	cfg := logger.Config{
		Level:       level,
		Encoding:    "console",
		Development: true,
	}
	return logger.NewWithConfig(&cfg)
}
