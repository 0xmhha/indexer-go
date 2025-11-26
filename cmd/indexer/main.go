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
	"go.uber.org/zap"
)

var (
	// Version information (injected at build time)
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

// App encapsulates all application components and lifecycle
type App struct {
	config    *config.Config
	logger    *zap.Logger
	client    *client.Client
	storage   storage.Storage
	eventBus  *events.EventBus
	fetcher   *fetch.Fetcher
	apiServer *api.Server

	// Runtime flags
	enableGapMode bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// run is the main entry point that orchestrates application lifecycle
func run() error {
	// Parse command-line flags
	flags := parseFlags()

	// Show version and exit if requested
	if flags.showVersion {
		fmt.Printf("indexer-go version %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", buildTime)
		return nil
	}

	// Load and validate configuration
	cfg, err := loadAndValidateConfig(flags)
	if err != nil {
		return err
	}

	// Initialize logger
	log, err := initLogger(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer log.Sync()

	// Log startup information
	logStartupInfo(log, cfg, flags)

	// Clear data folder if requested
	if flags.clearData {
		if err := clearDataFolder(cfg.Database.Path, log); err != nil {
			return fmt.Errorf("failed to clear data folder: %w", err)
		}
	}

	// Create and initialize application
	app, err := NewApp(cfg, log, flags.enableGapMode)
	if err != nil {
		return fmt.Errorf("failed to create application: %w", err)
	}
	defer app.Shutdown()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Run application
	errChan := make(chan error, 1)
	go func() {
		errChan <- app.Run(ctx)
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		log.Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()
	case err := <-errChan:
		if err != nil && err != context.Canceled {
			log.Error("Application stopped with error", zap.Error(err))
			return err
		}
	}

	log.Info("Shutting down gracefully...")
	return nil
}

// Flags holds all command-line flag values
type Flags struct {
	configFile      string
	showVersion     bool
	rpcEndpoint     string
	dbPath          string
	startHeight     uint64
	workers         int
	batchSize       int
	logLevel        string
	logFormat       string
	enableGapMode   bool
	clearData       bool
	enableAPI       bool
	apiHost         string
	apiPort         int
	enableGraphQL   bool
	enableJSONRPC   bool
	enableWebSocket bool
}

// parseFlags parses command-line flags
func parseFlags() *Flags {
	f := &Flags{}

	flag.StringVar(&f.configFile, "config", "config.yaml", "Path to configuration file (YAML)")
	flag.BoolVar(&f.showVersion, "version", false, "Show version information and exit")
	flag.StringVar(&f.rpcEndpoint, "rpc", "", "Ethereum RPC endpoint URL")
	flag.StringVar(&f.dbPath, "db", "", "Database path")
	flag.Uint64Var(&f.startHeight, "start-height", 0, "Block height to start indexing from")
	flag.IntVar(&f.workers, "workers", 100, "Number of concurrent workers")
	flag.IntVar(&f.batchSize, "batch-size", 0, "Number of blocks per batch (0 = use config.yaml)")
	flag.StringVar(&f.logLevel, "log-level", "", "Log level (debug, info, warn, error)")
	flag.StringVar(&f.logFormat, "log-format", "", "Log format (json, console)")
	flag.BoolVar(&f.enableGapMode, "gap-recovery", false, "Enable gap detection and recovery at startup")
	flag.BoolVar(&f.clearData, "clear-data", false, "Clear (delete) the data folder before starting")

	// API server flags
	flag.BoolVar(&f.enableAPI, "api", false, "Enable API server")
	flag.StringVar(&f.apiHost, "api-host", "", "API server host")
	flag.IntVar(&f.apiPort, "api-port", 0, "API server port")
	flag.BoolVar(&f.enableGraphQL, "graphql", false, "Enable GraphQL API")
	flag.BoolVar(&f.enableJSONRPC, "jsonrpc", false, "Enable JSON-RPC API")
	flag.BoolVar(&f.enableWebSocket, "websocket", false, "Enable WebSocket API")

	flag.Parse()
	return f
}

// loadAndValidateConfig loads configuration and applies flags
func loadAndValidateConfig(flags *Flags) (*config.Config, error) {
	cfg, err := loadConfig(flags.configFile)
	if err != nil {
		return nil, err
	}

	// Override config with command-line flags
	applyFlags(cfg, flags.rpcEndpoint, flags.dbPath, flags.startHeight, flags.workers, flags.batchSize, flags.logLevel, flags.logFormat)
	applyAPIFlags(cfg, flags.enableAPI, flags.apiHost, flags.apiPort, flags.enableGraphQL, flags.enableJSONRPC, flags.enableWebSocket)

	// Validate configuration
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// logStartupInfo logs startup information
func logStartupInfo(log *zap.Logger, cfg *config.Config, flags *Flags) {
	log.Info("Starting indexer",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("build_time", buildTime),
		zap.String("rpc_endpoint", cfg.RPC.Endpoint),
		zap.String("db_path", cfg.Database.Path),
		zap.Uint64("start_height", cfg.Indexer.StartHeight),
		zap.Int("workers", cfg.Indexer.Workers),
		zap.Int("batch_size", cfg.Indexer.ChunkSize),
		zap.Bool("gap_recovery", flags.enableGapMode),
		zap.Bool("clear_data", flags.clearData),
	)
}

// NewApp creates and initializes a new application instance
func NewApp(cfg *config.Config, log *zap.Logger, enableGapMode bool) (*App, error) {
	app := &App{
		config:        cfg,
		logger:        log,
		enableGapMode: enableGapMode,
	}

	ctx := context.Background()

	// Initialize Ethereum client
	if err := app.initClient(); err != nil {
		return nil, err
	}

	// Test connection and get chain ID
	if err := app.testConnection(ctx); err != nil {
		return nil, err
	}

	// Initialize storage
	if err := app.initStorage(ctx); err != nil {
		return nil, err
	}

	// Initialize EventBus
	app.initEventBus()

	// Initialize fetcher
	app.initFetcher()

	// Initialize API server if enabled
	if cfg.API.Enabled {
		if err := app.initAPIServer(); err != nil {
			return nil, err
		}
	}

	return app, nil
}

// initClient initializes the Ethereum client
func (a *App) initClient() error {
	ethClient, err := client.NewClient(&client.Config{
		Endpoint: a.config.RPC.Endpoint,
		Timeout:  a.config.RPC.Timeout,
		Logger:   a.logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create Ethereum client: %w", err)
	}

	a.client = ethClient
	a.logger.Info("Connected to Ethereum node", zap.String("endpoint", a.config.RPC.Endpoint))
	return nil
}

// testConnection tests the Ethereum client connection
func (a *App) testConnection(ctx context.Context) error {
	chainID, err := a.client.GetChainID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain ID: %w", err)
	}

	a.logger.Info("Connected to chain", zap.String("chain_id", chainID.String()))
	return nil
}

// initStorage initializes the storage layer
func (a *App) initStorage(ctx context.Context) error {
	storageConfig := storage.DefaultConfig(a.config.Database.Path)
	storageConfig.ReadOnly = false

	baseStore, err := storage.NewPebbleStorage(storageConfig)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}
	baseStore.SetLogger(a.logger)

	// Wrap storage with genesis initializer
	a.storage = storage.NewGenesisInitializingStorage(baseStore, a.client, a.logger)

	a.logger.Info("Storage initialized with genesis auto-initialization",
		zap.String("path", a.config.Database.Path),
	)

	// Log latest indexed height
	latestHeight, err := a.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			a.logger.Info("No blocks indexed yet, starting from configured height",
				zap.Uint64("start_height", a.config.Indexer.StartHeight),
			)
		} else {
			a.logger.Warn("Failed to get latest indexed block", zap.Error(err))
		}
	} else {
		a.logger.Info("Resuming from latest indexed block", zap.Uint64("latest_height", latestHeight))
	}

	return nil
}

// initEventBus initializes the event bus
func (a *App) initEventBus() {
	a.eventBus = events.NewEventBus(1000, 100)
	go a.eventBus.Run()

	a.logger.Info("EventBus initialized",
		zap.Int("publish_buffer", 1000),
		zap.Int("subscribe_buffer", 100),
	)
}

// initFetcher initializes the block fetcher
func (a *App) initFetcher() {
	// Real-time mode: Use shorter RetryDelay for batch_size=1
	retryDelay := time.Second * 5
	if a.config.Indexer.ChunkSize == 1 {
		retryDelay = time.Millisecond * 200
	}

	fetcherConfig := &fetch.Config{
		StartHeight: a.config.Indexer.StartHeight,
		BatchSize:   a.config.Indexer.ChunkSize,
		MaxRetries:  3,
		RetryDelay:  retryDelay,
		NumWorkers:  a.config.Indexer.Workers,
	}

	a.fetcher = fetch.NewFetcher(a.client, a.storage, fetcherConfig, a.logger, a.eventBus)

	a.logger.Info("Fetcher initialized",
		zap.Duration("retry_delay", retryDelay),
		zap.Int("batch_size", a.config.Indexer.ChunkSize),
	)
}

// initAPIServer initializes the API server
func (a *App) initAPIServer() error {
	a.logger.Info("Initializing API server...")

	apiConfig := &api.Config{
		Host:                  a.config.API.Host,
		Port:                  a.config.API.Port,
		ReadTimeout:           15 * time.Second,
		WriteTimeout:          15 * time.Second,
		IdleTimeout:           60 * time.Second,
		EnableCORS:            a.config.API.EnableCORS,
		AllowedOrigins:        a.config.API.AllowedOrigins,
		MaxHeaderBytes:        1 << 20,
		EnableGraphQL:         a.config.API.EnableGraphQL,
		EnableJSONRPC:         a.config.API.EnableJSONRPC,
		EnableWebSocket:       a.config.API.EnableWebSocket,
		GraphQLPath:           "/graphql",
		GraphQLPlaygroundPath: "/playground",
		JSONRPCPath:           "/rpc",
		WebSocketPath:         "/ws",
		ShutdownTimeout:       30 * time.Second,
	}

	apiServer, err := api.NewServer(apiConfig, a.logger, a.storage)
	if err != nil {
		return fmt.Errorf("failed to create API server: %w", err)
	}

	apiServer.SetEventBus(a.eventBus)
	a.apiServer = apiServer

	// Start API server in goroutine
	go func() {
		if err := a.apiServer.Start(); err != nil {
			a.logger.Error("API server failed", zap.Error(err))
		}
	}()

	a.logger.Info("API server started",
		zap.String("address", apiConfig.Address()),
		zap.Bool("graphql", apiConfig.EnableGraphQL),
		zap.Bool("jsonrpc", apiConfig.EnableJSONRPC),
		zap.Bool("websocket", apiConfig.EnableWebSocket),
	)

	return nil
}

// Run starts the application and blocks until context is cancelled
func (a *App) Run(ctx context.Context) error {
	a.logger.Info("Starting indexing...")

	if a.enableGapMode {
		a.logger.Info("Starting with gap recovery enabled")
		return a.fetcher.RunWithGapRecovery(ctx)
	}

	a.logger.Info("Starting normal indexing mode")
	return a.fetcher.Run(ctx)
}

// Shutdown gracefully shuts down all application components
func (a *App) Shutdown() {
	a.logger.Info("Shutting down application components...")

	// Stop API server
	if a.apiServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := a.apiServer.Stop(shutdownCtx); err != nil {
			a.logger.Error("Failed to stop API server gracefully", zap.Error(err))
		}
	}

	// Stop EventBus
	if a.eventBus != nil {
		a.eventBus.Stop()
	}

	// Close storage
	if a.storage != nil {
		if err := a.storage.Close(); err != nil {
			a.logger.Error("Failed to close storage", zap.Error(err))
		}
	}

	// Close client
	if a.client != nil {
		a.client.Close()
	}

	// Wait for graceful shutdown
	time.Sleep(time.Second * 2)

	// Log final statistics
	ctx := context.Background()
	if a.storage != nil {
		finalHeight, err := a.storage.GetLatestHeight(ctx)
		if err == nil {
			a.logger.Info("Final statistics", zap.Uint64("latest_height", finalHeight))
		} else if !errors.Is(err, storage.ErrNotFound) {
			a.logger.Warn("Failed to read final indexed height", zap.Error(err))
		}
	}

	a.logger.Info("Application stopped")
}

// loadConfig loads configuration from YAML file
func loadConfig(configFile string) (*config.Config, error) {
	cfg, err := config.Load(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	return cfg, nil
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
		return fmt.Errorf("RPC endpoint is required (use --rpc flag or set in config.yaml)")
	}
	if cfg.Database.Path == "" {
		return fmt.Errorf("database path is required (use --db flag or set in config.yaml)")
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

// clearDataFolder removes the data folder and all its contents
func clearDataFolder(path string, log *zap.Logger) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("Data folder does not exist, nothing to clear", zap.String("path", path))
			return nil
		}
		return fmt.Errorf("failed to stat data folder: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("data path is not a directory: %s", path)
	}

	log.Warn("Clearing data folder", zap.String("path", path))

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to remove data folder: %w", err)
	}

	log.Info("Data folder cleared successfully", zap.String("path", path))
	return nil
}
