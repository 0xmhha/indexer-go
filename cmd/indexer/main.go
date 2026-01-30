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

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/logger"
	"github.com/0xmhha/indexer-go/pkg/adapters/detector"
	"github.com/0xmhha/indexer-go/pkg/adapters/factory"
	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/client"
	"github.com/0xmhha/indexer-go/pkg/compiler"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/multichain"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"github.com/0xmhha/indexer-go/pkg/verifier"
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
	config       *config.Config
	logger       *zap.Logger
	client       *client.Client
	chainAdapter chain.Adapter
	nodeInfo     *detector.NodeInfo
	storage      storage.Storage
	eventBus     *events.EventBus
	fetcher      *fetch.Fetcher
	apiServer    *api.Server
	rpcProxy     *rpcproxy.Proxy

	// Multi-chain support
	multichainManager *multichain.Manager

	// Notification system
	notificationService notifications.Service

	// Contract verification
	contractVerifier verifier.Verifier

	// Runtime flags
	enableGapMode    bool
	forceAdapterType string
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
	app, err := NewApp(cfg, log, flags.enableGapMode, flags.forceAdapterType)
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
	configFile       string
	showVersion      bool
	rpcEndpoint      string
	dbPath           string
	startHeight      uint64
	workers          int
	batchSize        int
	logLevel         string
	logFormat        string
	enableGapMode    bool
	clearData        bool
	enableAPI        bool
	apiHost          string
	apiPort          int
	enableGraphQL    bool
	enableJSONRPC    bool
	enableWebSocket  bool
	forceAdapterType string // Force specific adapter type: anvil, stableone, evm
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

	// Chain adapter flags
	flag.StringVar(&f.forceAdapterType, "adapter", "", "Force specific adapter type (anvil, stableone, evm). Auto-detected if empty")

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
	adapterInfo := "auto-detect"
	if flags.forceAdapterType != "" {
		adapterInfo = flags.forceAdapterType + " (forced)"
	}

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
		zap.String("adapter", adapterInfo),
	)
}

// NewApp creates and initializes a new application instance
func NewApp(cfg *config.Config, log *zap.Logger, enableGapMode bool, forceAdapterType string) (*App, error) {
	app := &App{
		config:           cfg,
		logger:           log,
		enableGapMode:    enableGapMode,
		forceAdapterType: forceAdapterType,
	}

	ctx := context.Background()

	// Initialize storage first (needed by both single and multi-chain modes)
	if err := app.initStorageOnly(ctx); err != nil {
		return nil, err
	}

	// Initialize EventBus (shared across all chains)
	app.initEventBus()

	// Initialize notification service if enabled
	if err := app.initNotificationService(); err != nil {
		return nil, fmt.Errorf("failed to initialize notification service: %w", err)
	}

	// Check if multichain mode is enabled
	if cfg.MultiChain.Enabled && len(cfg.MultiChain.Chains) > 0 {
		log.Info("Multi-chain mode enabled",
			zap.Int("configured_chains", len(cfg.MultiChain.Chains)),
		)

		if err := app.initMultiChainManager(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize multi-chain manager: %w", err)
		}
	} else {
		// Single chain mode (legacy)
		log.Info("Single-chain mode")

		// Initialize Ethereum client
		if err := app.initClient(); err != nil {
			return nil, err
		}

		// Test connection and get chain ID
		if err := app.testConnection(ctx); err != nil {
			return nil, err
		}

		// Complete storage initialization with client
		if err := app.completeStorageInit(ctx); err != nil {
			return nil, err
		}

		// Initialize fetcher
		app.initFetcher()
	}

	// Initialize API server if enabled
	if cfg.API.Enabled {
		if err := app.initAPIServer(); err != nil {
			return nil, err
		}
	}

	return app, nil
}

// initClient initializes the Ethereum client and detects node type
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

	// Create chain adapter using factory with auto-detection
	ctx := context.Background()
	factoryConfig := factory.DefaultConfig(a.config.RPC.Endpoint)
	factoryConfig.ForceAdapterType = a.forceAdapterType

	adapterFactory := factory.NewFactory(factoryConfig, a.logger)
	result, err := adapterFactory.Create(ctx)
	if err != nil {
		a.logger.Warn("Failed to create chain adapter, using generic EVM behavior",
			zap.Error(err),
		)
		// Continue without adapter - generic EVM behavior will be used
		return nil
	}

	a.chainAdapter = result.Adapter
	a.nodeInfo = result.NodeInfo

	a.logger.Info("Chain adapter initialized",
		zap.String("adapter_type", result.AdapterType),
		zap.String("node_type", string(result.NodeInfo.Type)),
		zap.Uint64("chain_id", result.NodeInfo.ChainID),
		zap.Bool("is_local", result.NodeInfo.IsLocal),
		zap.String("consensus_type", string(a.chainAdapter.Info().ConsensusType)),
	)

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

// initStorageOnly initializes only the base storage layer without genesis initialization
// This is used when multichain mode is enabled (each chain handles its own genesis)
func (a *App) initStorageOnly(ctx context.Context) error {
	storageConfig := storage.DefaultConfig(a.config.Database.Path)
	storageConfig.ReadOnly = false

	baseStore, err := storage.NewPebbleStorage(storageConfig)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}
	baseStore.SetLogger(a.logger)

	// For multichain mode, use base storage directly
	// For single chain mode, we'll wrap it with genesis initializer later
	a.storage = baseStore

	a.logger.Info("Base storage initialized",
		zap.String("path", a.config.Database.Path),
	)

	return nil
}

// completeStorageInit completes storage initialization for single-chain mode
// This wraps storage with genesis initializer and runs additional setup
func (a *App) completeStorageInit(ctx context.Context) error {
	// Wrap storage with genesis initializer (needs client)
	if pebbleStore, ok := a.storage.(*storage.PebbleStorage); ok {
		a.storage = storage.NewGenesisInitializingStorage(pebbleStore, a.client, a.logger)
		a.logger.Info("Storage wrapped with genesis auto-initialization")
	}

	// Initialize system contract verifications if enabled
	if a.config.SystemContracts.Enabled && a.config.SystemContracts.SourcePath != "" {
		if err := a.initSystemContractVerifications(ctx); err != nil {
			a.logger.Warn("Failed to initialize system contract verifications", zap.Error(err))
		}
	}

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

// initStorage initializes the storage layer (legacy method for compatibility)
func (a *App) initStorage(ctx context.Context) error {
	if err := a.initStorageOnly(ctx); err != nil {
		return err
	}
	return a.completeStorageInit(ctx)
}

// initSystemContractVerifications initializes system contract verifications
func (a *App) initSystemContractVerifications(ctx context.Context) error {
	// Cast storage to required interfaces
	writer, ok := a.storage.(storage.ContractVerificationWriter)
	if !ok {
		return fmt.Errorf("storage does not support contract verification writes")
	}

	reader, ok := a.storage.(storage.ContractVerificationReader)
	if !ok {
		return fmt.Errorf("storage does not support contract verification reads")
	}

	config := &storage.SystemContractVerificationConfig{
		SourcePath:       a.config.SystemContracts.SourcePath,
		IncludeAbstracts: a.config.SystemContracts.IncludeAbstracts,
		Logger:           a.logger,
	}

	return storage.InitSystemContractVerifications(ctx, writer, reader, config)
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

// initNotificationService initializes the notification service if enabled
func (a *App) initNotificationService() error {
	if !a.config.Notifications.Enabled {
		a.logger.Debug("Notification service disabled")
		return nil
	}

	// Convert config to notification service config
	notifConfig := &notifications.Config{
		Enabled: a.config.Notifications.Enabled,
		Webhook: notifications.WebhookConfig{
			Enabled:         a.config.Notifications.Webhook.Enabled,
			Timeout:         a.config.Notifications.Webhook.Timeout,
			MaxRetries:      a.config.Notifications.Webhook.MaxRetries,
			MaxConcurrent:   a.config.Notifications.Webhook.MaxConcurrent,
			AllowedHosts:    a.config.Notifications.Webhook.AllowedHosts,
			SignatureHeader: a.config.Notifications.Webhook.SignatureHeader,
		},
		Email: notifications.EmailConfig{
			Enabled:            a.config.Notifications.Email.Enabled,
			SMTPHost:           a.config.Notifications.Email.SMTPHost,
			SMTPPort:           a.config.Notifications.Email.SMTPPort,
			SMTPUsername:       a.config.Notifications.Email.SMTPUsername,
			SMTPPassword:       a.config.Notifications.Email.SMTPPassword,
			FromAddress:        a.config.Notifications.Email.FromAddress,
			FromName:           a.config.Notifications.Email.FromName,
			UseTLS:             a.config.Notifications.Email.UseTLS,
			MaxRecipients:      a.config.Notifications.Email.MaxRecipients,
			RateLimitPerMinute: a.config.Notifications.Email.RateLimitPerMinute,
		},
		Slack: notifications.SlackConfig{
			Enabled:            a.config.Notifications.Slack.Enabled,
			Timeout:            a.config.Notifications.Slack.Timeout,
			MaxRetries:         a.config.Notifications.Slack.MaxRetries,
			DefaultUsername:    a.config.Notifications.Slack.DefaultUsername,
			DefaultIconEmoji:   a.config.Notifications.Slack.DefaultIconEmoji,
			RateLimitPerMinute: a.config.Notifications.Slack.RateLimitPerMinute,
		},
		Retry: notifications.RetryConfig{
			MaxAttempts:  a.config.Notifications.Retry.MaxAttempts,
			InitialDelay: a.config.Notifications.Retry.InitialDelay,
			MaxDelay:     a.config.Notifications.Retry.MaxDelay,
			Multiplier:   a.config.Notifications.Retry.Multiplier,
		},
		Queue: notifications.QueueConfig{
			BufferSize:    a.config.Notifications.Queue.BufferSize,
			Workers:       a.config.Notifications.Queue.Workers,
			BatchSize:     a.config.Notifications.Queue.BatchSize,
			FlushInterval: a.config.Notifications.Queue.FlushInterval,
		},
		Storage: notifications.StorageConfig{
			HistoryRetention:        a.config.Notifications.Storage.HistoryRetention,
			MaxSettingsPerUser:      a.config.Notifications.Storage.MaxSettingsPerUser,
			MaxPendingNotifications: a.config.Notifications.Storage.MaxPendingNotifications,
		},
	}

	// Get KVStore from storage for notification persistence
	kvStore, ok := a.storage.(storage.KVStore)
	if !ok {
		return fmt.Errorf("storage does not implement KVStore interface")
	}

	// Create notification storage
	notifStorage := notifications.NewPebbleStorage(kvStore)

	// Create notification service
	service := notifications.NewService(notifConfig, notifStorage, a.eventBus, a.logger)

	// Register handlers
	if notifConfig.Webhook.Enabled {
		service.RegisterHandler(notifications.NewWebhookHandler(&notifConfig.Webhook, a.logger))
	}
	if notifConfig.Email.Enabled {
		service.RegisterHandler(notifications.NewEmailHandler(&notifConfig.Email, a.logger))
	}
	if notifConfig.Slack.Enabled {
		service.RegisterHandler(notifications.NewSlackHandler(&notifConfig.Slack, a.logger))
	}

	a.notificationService = service

	a.logger.Info("Notification service initialized",
		zap.Bool("webhook_enabled", notifConfig.Webhook.Enabled),
		zap.Bool("email_enabled", notifConfig.Email.Enabled),
		zap.Bool("slack_enabled", notifConfig.Slack.Enabled),
		zap.Int("worker_count", notifConfig.Queue.Workers),
	)

	return nil
}

// initMultiChainManager initializes the multi-chain manager
func (a *App) initMultiChainManager(ctx context.Context) error {
	// Convert config chains to multichain ChainConfigs
	chainConfigs := make([]multichain.ChainConfig, 0, len(a.config.MultiChain.Chains))
	for _, cc := range a.config.MultiChain.Chains {
		chainConfigs = append(chainConfigs, multichain.ChainConfig{
			ID:          cc.ID,
			Name:        cc.Name,
			RPCEndpoint: cc.RPCEndpoint,
			WSEndpoint:  cc.WSEndpoint,
			ChainID:     cc.ChainID,
			AdapterType: cc.AdapterType,
			StartHeight: cc.StartHeight,
			Enabled:     cc.Enabled,
			Workers:     a.config.Indexer.Workers,
			BatchSize:   a.config.Indexer.ChunkSize,
			RPCTimeout:  a.config.RPC.Timeout,
		})
	}

	managerConfig := &multichain.ManagerConfig{
		Enabled:             true,
		Chains:              chainConfigs,
		HealthCheckInterval: a.config.MultiChain.HealthCheckInterval,
		MaxUnhealthyDuration: a.config.MultiChain.MaxUnhealthyDuration,
		AutoRestart:         a.config.MultiChain.AutoRestart,
		AutoRestartDelay:    a.config.MultiChain.AutoRestartDelay,
	}

	manager, err := multichain.NewManager(managerConfig, a.storage, a.eventBus, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create multi-chain manager: %w", err)
	}

	a.multichainManager = manager
	a.logger.Info("Multi-chain manager created",
		zap.Int("chains", len(chainConfigs)),
	)

	return nil
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

	// Create fetcher with chain adapter if available
	if a.chainAdapter != nil {
		a.fetcher = fetch.NewFetcherWithAdapter(a.client, a.storage, fetcherConfig, a.logger, a.eventBus, a.chainAdapter)
		a.logger.Info("Fetcher initialized with chain adapter",
			zap.Duration("retry_delay", retryDelay),
			zap.Int("batch_size", a.config.Indexer.ChunkSize),
			zap.String("adapter_type", string(a.chainAdapter.Info().ChainType)),
			zap.String("consensus_type", string(a.chainAdapter.Info().ConsensusType)),
		)
	} else {
		a.fetcher = fetch.NewFetcher(a.client, a.storage, fetcherConfig, a.logger, a.eventBus)
		a.logger.Info("Fetcher initialized (generic EVM mode)",
			zap.Duration("retry_delay", retryDelay),
			zap.Int("batch_size", a.config.Indexer.ChunkSize),
		)
	}
}

// initAPIServer initializes the API server
func (a *App) initAPIServer() error {
	a.logger.Info("Initializing API server...")

	// Initialize RPC Proxy for contract call queries
	if err := a.initRPCProxy(); err != nil {
		a.logger.Warn("Failed to initialize RPC Proxy, contract call queries will be disabled", zap.Error(err))
	}

	// Initialize Contract Verifier for Etherscan-compatible API
	if err := a.initContractVerifier(); err != nil {
		a.logger.Warn("Failed to initialize Contract Verifier, contract verification will be disabled", zap.Error(err))
	}

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

	// Create API server with optional RPC Proxy, Notification Service, and Verifier
	serverOpts := &api.ServerOptions{
		RPCProxy:            a.rpcProxy,
		NotificationService: a.notificationService,
		Verifier:            a.contractVerifier,
	}
	apiServer, err := api.NewServerWithOptions(apiConfig, a.logger, a.storage, serverOpts)
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
		zap.Bool("rpc_proxy", a.rpcProxy != nil),
		zap.Bool("notifications", a.notificationService != nil),
		zap.Bool("verifier", a.contractVerifier != nil),
	)

	return nil
}

// initRPCProxy initializes the RPC Proxy for contract call queries
func (a *App) initRPCProxy() error {
	// Create RPC Proxy configuration
	proxyConfig := rpcproxy.DefaultConfig()

	// Get underlying eth and rpc clients from the indexer client
	ethClient := a.client.EthClient()
	rpcClient := a.client.RPCClient()

	// Create RPC Proxy
	proxy := rpcproxy.NewProxy(ethClient, rpcClient, a.storage, proxyConfig, a.logger)

	// Start the proxy (starts worker pool)
	if err := proxy.Start(); err != nil {
		return fmt.Errorf("failed to start RPC proxy: %w", err)
	}

	a.rpcProxy = proxy
	a.logger.Info("RPC Proxy initialized",
		zap.String("endpoint", a.config.RPC.Endpoint),
		zap.Int("workers", proxyConfig.Worker.NumWorkers),
	)

	return nil
}

// initContractVerifier initializes the contract verification service
func (a *App) initContractVerifier() error {
	if !a.config.Verifier.Enabled {
		a.logger.Debug("Contract verifier disabled")
		return nil
	}

	if a.client == nil {
		a.logger.Warn("Cannot initialize contract verifier: no client available")
		return nil
	}

	// Create compiler configuration
	compilerCfg := &compiler.Config{
		BinDir:             a.config.Verifier.SolcBinDir,
		CacheDir:           a.config.Verifier.SolcCacheDir,
		MaxCompilationTime: a.config.Verifier.MaxCompilationTime,
		CacheEnabled:       true,
		AutoDownload:       a.config.Verifier.AutoDownload,
	}

	// Create Solidity compiler
	solcCompiler, err := compiler.NewSolcCompiler(compilerCfg)
	if err != nil {
		return fmt.Errorf("failed to create Solidity compiler: %w", err)
	}

	// Create verifier configuration
	verifierCfg := verifier.DefaultConfig(solcCompiler, a.client.EthClient())
	verifierCfg.AllowMetadataVariance = a.config.Verifier.AllowMetadataVariance

	// Create contract verifier
	contractVerifier, err := verifier.NewContractVerifier(verifierCfg)
	if err != nil {
		solcCompiler.Close()
		return fmt.Errorf("failed to create contract verifier: %w", err)
	}

	a.contractVerifier = contractVerifier

	a.logger.Info("Contract verifier initialized",
		zap.String("solc_bin_dir", a.config.Verifier.SolcBinDir),
		zap.Bool("auto_download", a.config.Verifier.AutoDownload),
		zap.Bool("allow_metadata_variance", a.config.Verifier.AllowMetadataVariance),
	)

	return nil
}

// Run starts the application and blocks until context is cancelled
func (a *App) Run(ctx context.Context) error {
	a.logger.Info("Starting indexing...")

	// Start notification service if enabled
	if a.notificationService != nil {
		if err := a.notificationService.Start(ctx); err != nil {
			return fmt.Errorf("failed to start notification service: %w", err)
		}
		a.logger.Info("Notification service started")
	}

	// Multi-chain mode
	if a.multichainManager != nil {
		a.logger.Info("Starting multi-chain manager")
		if err := a.multichainManager.Start(ctx); err != nil {
			return fmt.Errorf("failed to start multi-chain manager: %w", err)
		}

		// Block until context is cancelled
		<-ctx.Done()
		return ctx.Err()
	}

	// Single-chain mode (legacy)
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop notification service
	if a.notificationService != nil {
		if err := a.notificationService.Stop(shutdownCtx); err != nil {
			a.logger.Error("Failed to stop notification service gracefully", zap.Error(err))
		}
		a.logger.Info("Notification service stopped")
	}

	// Stop API server
	if a.apiServer != nil {
		if err := a.apiServer.Stop(shutdownCtx); err != nil {
			a.logger.Error("Failed to stop API server gracefully", zap.Error(err))
		}
	}

	// Stop RPC Proxy
	if a.rpcProxy != nil {
		a.rpcProxy.Stop()
		a.logger.Info("RPC Proxy stopped")
	}

	// Close Contract Verifier
	if a.contractVerifier != nil {
		if err := a.contractVerifier.Close(); err != nil {
			a.logger.Error("Failed to close contract verifier", zap.Error(err))
		}
		a.logger.Info("Contract verifier stopped")
	}

	// Stop multi-chain manager if running
	if a.multichainManager != nil {
		if err := a.multichainManager.Stop(shutdownCtx); err != nil {
			a.logger.Error("Failed to stop multi-chain manager gracefully", zap.Error(err))
		}
		a.logger.Info("Multi-chain manager stopped")
	}

	// Stop EventBus
	if a.eventBus != nil {
		a.eventBus.Stop()
	}

	// Close chain adapter (single-chain mode only)
	if a.chainAdapter != nil {
		if err := a.chainAdapter.Close(); err != nil {
			a.logger.Error("Failed to close chain adapter", zap.Error(err))
		}
	}

	// Close storage
	if a.storage != nil {
		if err := a.storage.Close(); err != nil {
			a.logger.Error("Failed to close storage", zap.Error(err))
		}
	}

	// Close client (single-chain mode only)
	if a.client != nil {
		a.client.Close()
	}

	// Wait for graceful shutdown
	time.Sleep(time.Second * 2)

	// Log final statistics
	ctx := context.Background()
	if a.multichainManager != nil {
		// Multi-chain mode: log stats for each chain
		metrics := a.multichainManager.GetMetrics()
		for chainID, m := range metrics {
			a.logger.Info("Chain statistics",
				zap.String("chainId", chainID),
				zap.Uint64("blocksIndexed", m.BlocksIndexed),
				zap.Uint64("txsIndexed", m.TransactionsIndexed),
				zap.Uint64("logsIndexed", m.LogsIndexed),
			)
		}
	} else if a.storage != nil {
		// Single-chain mode
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
