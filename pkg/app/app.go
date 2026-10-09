// Package app is the indexer application: configuration, wiring of storage,
// fetcher, features and API, and the process lifecycle. It links in the
// standard features and chain profiles; Main runs it. A project adds its
// own handlers by importing their packages and calling Main from its own
// main package (refactoring plan R6-2).
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/internal/logger"
	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/chains"
	_ "github.com/0xmhha/indexer-go/pkg/chains/evm"                                // generic EVM chain profile
	_ "github.com/0xmhha/indexer-go/pkg/chains/stablenet"                          // StableNet chain profile
	_ "github.com/0xmhha/indexer-go/pkg/chains/stablenet/consensus/api"            // StableNet WBFT consensus API
	_ "github.com/0xmhha/indexer-go/pkg/chains/stablenet/features/feedelegation"   // stablenet.fee_delegation feature
	_ "github.com/0xmhha/indexer-go/pkg/chains/stablenet/features/systemcontracts" // stablenet.system_contracts feature
	_ "github.com/0xmhha/indexer-go/pkg/chains/stablenet/features/wbft"            // stablenet.wbft feature
	_ "github.com/0xmhha/indexer-go/pkg/chains/stablenet/feedelegation/api"        // StableNet fee delegation API
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
	_ "github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts/api" // StableNet system contract API
	"github.com/0xmhha/indexer-go/pkg/client"
	"github.com/0xmhha/indexer-go/pkg/compiler"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/aa"
	_ "github.com/0xmhha/indexer-go/pkg/features/address" // address.index feature
	_ "github.com/0xmhha/indexer-go/pkg/features/agg/api" // agg.candles, agg.timeseries and their GraphQL
	_ "github.com/0xmhha/indexer-go/pkg/features/balance" // balance.native feature
	_ "github.com/0xmhha/indexer-go/pkg/features/dex/api" // dex.pools, dex.trades, dex.orderbook and their GraphQL
	"github.com/0xmhha/indexer-go/pkg/features/dex/orderbook"
	"github.com/0xmhha/indexer-go/pkg/features/records"
	_ "github.com/0xmhha/indexer-go/pkg/features/records/api" // records (declared tables) and its GraphQL
	_ "github.com/0xmhha/indexer-go/pkg/features/token"       // token.transfers feature
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/multichain"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/source/era"
	"github.com/0xmhha/indexer-go/pkg/source/replay"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/stream"
	"github.com/0xmhha/indexer-go/pkg/token"
	"github.com/0xmhha/indexer-go/pkg/verifier"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

var (
	// Version information, injected at build time with
	// -X github.com/0xmhha/indexer-go/pkg/app.version=...
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

// App encapsulates all application components and lifecycle
type App struct {
	config *config.Config
	logger *zap.Logger
	client *client.Client
	// profile is the chain profile selected by --adapter or detected (nil if
	// detection failed, see profileErr); profileSrc reads blocks with it.
	profile    chains.Profile
	profileSrc *sourcerpc.Source
	profileErr error
	features   *feature.Pipeline // enabled features, in execution order
	storage    storage.Storage
	eventBus   *events.EventBus
	fetcher    *fetch.Fetcher
	apiServer  *api.Server
	rpcProxy   *rpcproxy.Proxy

	// RPC archive (pkg/source/replay): a local endpoint that records calls to
	// the node or replays a recorded archive.
	rpcEndpoint *replay.Endpoint
	rpcRecorder *replay.Writer
	rpcReplay   *replay.Server
	// eraSource serves history from era1 archives (source.era_dir).
	eraSource *era.Source

	// Multi-chain support
	multichainManager *multichain.Manager

	// Notification system
	notificationService notifications.Service

	// Contract verification
	contractVerifier verifier.Verifier

	// streamRelay feeds the event bus of an API process from the change
	// stream (node.role api, role.go).
	streamRelay *stream.Relay
	// orderBook keeps the DEX order books (dex.orderbook), nil when off.
	orderBook *orderbook.Service

	// Runtime flags
	enableGapMode    bool
	forceAdapterType string
}

// Main runs the indexer as its command line asks and exits the process on
// error. The indexer command calls it; a program that links in features or
// GraphQL extensions of its own (refactoring plan R6-2, pkg/sdk) imports
// them and calls it from its main.
func Main() {
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
	defer func() { _ = log.Sync() }()

	for _, msg := range cfg.UnsupportedSettings() {
		log.Warn("Unsupported setting ignored", zap.String("detail", msg))
	}

	// Log startup information
	logStartupInfo(log, cfg, flags)

	// Clear data folder if requested
	if flags.clearData {
		if err := clearDatabases(cfg, log); err != nil {
			return fmt.Errorf("failed to clear data folder: %w", err)
		}
	}

	// Reindex: clear blockchain data while preserving verification data
	if flags.reindex && !flags.clearData {
		if err := reindexDatabases(cfg, log); err != nil {
			return fmt.Errorf("failed to reindex data: %w", err)
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
	reindex          bool // Clear blockchain data only, preserving verification data
	enableAPI        bool
	apiHost          string
	apiPort          int
	enableGraphQL    bool
	enableJSONRPC    bool
	enableWebSocket  bool
	forceAdapterType string // chain profile id or alias (--adapter)

	// set records the flags given on the command line. Only those override
	// the configuration, so flag defaults never replace config values and
	// boolean flags can switch features off as well as on.
	set map[string]bool
}

// parseFlags parses the process command line.
func parseFlags() *Flags {
	f, err := parseFlagsFrom(os.Args[1:], flag.ExitOnError)
	if err != nil {
		// unreachable with ExitOnError
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return f
}

// parseFlagsFrom parses args with a fresh flag set (testable).
func parseFlagsFrom(args []string, onError flag.ErrorHandling) (*Flags, error) {
	f := &Flags{set: map[string]bool{}}
	fs := flag.NewFlagSet("indexer", onError)

	fs.StringVar(&f.configFile, "config", "config.yaml", "Path to configuration file (YAML)")
	fs.BoolVar(&f.showVersion, "version", false, "Show version information and exit")
	fs.StringVar(&f.rpcEndpoint, "rpc", "", "Ethereum RPC endpoint URL")
	fs.StringVar(&f.dbPath, "db", "", "Database path")
	fs.Uint64Var(&f.startHeight, "start-height", 0, "Block height to start indexing from")
	fs.IntVar(&f.workers, "workers", 100, "Number of concurrent workers")
	fs.IntVar(&f.batchSize, "batch-size", 0, "Number of blocks per batch (0 = use config.yaml)")
	fs.StringVar(&f.logLevel, "log-level", "", "Log level (debug, info, warn, error)")
	fs.StringVar(&f.logFormat, "log-format", "", "Log format (json, console)")
	fs.BoolVar(&f.enableGapMode, "gap-recovery", false, "Enable gap detection and recovery at startup")
	fs.BoolVar(&f.clearData, "clear-data", false, "Clear (delete) the data folder before starting")
	fs.BoolVar(&f.reindex, "reindex", false, "Clear blockchain data only, preserving verification data (ABIs, source code, verification status)")

	// API server flags
	fs.BoolVar(&f.enableAPI, "api", false, "Enable API server")
	fs.StringVar(&f.apiHost, "api-host", "", "API server host")
	fs.IntVar(&f.apiPort, "api-port", 0, "API server port")
	fs.BoolVar(&f.enableGraphQL, "graphql", false, "Enable GraphQL API")
	fs.BoolVar(&f.enableJSONRPC, "jsonrpc", false, "Enable JSON-RPC API")
	fs.BoolVar(&f.enableWebSocket, "websocket", false, "Enable WebSocket API")

	// Chain adapter flags
	fs.StringVar(&f.forceAdapterType, "adapter", "", "Chain profile id or alias (stablenet, stableone, evm). Detected from the node if empty or not a profile")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	fs.Visit(func(fl *flag.Flag) { f.set[fl.Name] = true })
	return f, nil
}

// loadAndValidateConfig loads configuration and applies flags
func loadAndValidateConfig(flags *Flags) (*config.Config, error) {
	// The default config path is optional: without --config and without
	// config.yaml, defaults, environment variables and flags are used.
	// An explicitly given --config must exist.
	configFile := flags.configFile
	if !flags.set["config"] {
		if _, statErr := os.Stat(configFile); errors.Is(statErr, os.ErrNotExist) {
			configFile = ""
		}
	}
	cfg, err := config.LoadUnvalidated(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Override config with flags given on the command line, then validate,
	// so that flags can supply values the file leaves out.
	applyFlags(cfg, flags)
	applyAPIFlags(cfg, flags)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// logStartupInfo logs startup information
func logStartupInfo(log *zap.Logger, cfg *config.Config, flags *Flags) {
	profileInfo := "auto-detect"
	if flags.forceAdapterType != "" {
		profileInfo = flags.forceAdapterType + " (forced)"
	}

	log.Info("Starting indexer",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("build_time", buildTime),
		zap.String("rpc_endpoint", cfg.RPC.Endpoint),
		zap.String("db_driver", cfg.Database.Driver),
		zap.String("db_path", cfg.Database.Path),
		zap.Uint64("start_height", cfg.Indexer.StartHeight),
		zap.Int("workers", cfg.Indexer.Workers),
		zap.Int("batch_size", cfg.Indexer.ChunkSize),
		zap.Bool("gap_recovery", flags.enableGapMode),
		zap.Bool("clear_data", flags.clearData),
		zap.Bool("reindex", flags.reindex),
		zap.String("chain_profile", profileInfo),
	)
}

// NewApp creates and initializes a new application instance
func NewApp(cfg *config.Config, log *zap.Logger, enableGapMode bool, forceAdapterType string) (_ *App, err error) {
	app := &App{
		config:           cfg,
		logger:           log,
		enableGapMode:    enableGapMode,
		forceAdapterType: forceAdapterType,
	}
	// A failed start releases what it opened (the database lock above all),
	// so the caller can retry in the same process.
	defer func() {
		if err != nil {
			app.closeAfterFailedStart()
		}
	}()

	ctx := context.Background()

	if cfg.MultiChainMode() {
		// Every chain runs its own pipeline into its own database
		// (<database.path>/chains/<id>, refactoring plan R2-8); this App
		// only manages them and serves their APIs.
		log.Info("Multi-chain mode enabled",
			zap.Int("configured_chains", len(cfg.MultiChain.Chains)),
		)
		if err := app.initMultiChainManager(); err != nil {
			return nil, fmt.Errorf("failed to initialize multi-chain manager: %w", err)
		}
	} else if cfg.NodeRole() == config.RoleAPI {
		log.Info("Single-chain mode", zap.String("role", config.RoleAPI))
		if err := app.initAPINode(ctx); err != nil {
			return nil, err
		}
	} else {
		log.Info("Single-chain mode", zap.String("role", cfg.NodeRole()))

		if err := app.startRPCArchive(); err != nil {
			return nil, err
		}

		// Initialize storage
		if err := app.initStorageOnly(ctx); err != nil {
			return nil, err
		}

		app.initEventBus()

		// Initialize notification service if enabled
		if err := app.initNotificationService(); err != nil {
			return nil, fmt.Errorf("failed to initialize notification service: %w", err)
		}

		// Initialize Ethereum client
		if err := app.initClient(); err != nil {
			return nil, err
		}
		app.configureNotificationsForChain()

		// Test connection and get chain ID
		if err := app.testConnection(ctx); err != nil {
			return nil, err
		}

		// Complete storage initialization with client
		if err := app.completeStorageInit(ctx); err != nil {
			return nil, err
		}

		// Initialize fetcher
		if err := app.initFetcher(ctx); err != nil {
			return nil, err
		}

		// Notifications consume the change stream (R3-5) when events are
		// recorded in the outbox.
		if err := app.streamNotifications(); err != nil {
			return nil, err
		}
	}
	if !cfg.MultiChainMode() {
		if err := app.initOrderBook(ctx); err != nil {
			return nil, err
		}
		if err := app.initRecords(); err != nil {
			return nil, err
		}
		if app.fetcher != nil {
			// Routes tell "not indexed yet" from "indexing is behind" (sdk.ProgressOf).
			fetch.AttachProgress(app.storage, app.fetcher)
		}
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
	clientCfg := &client.Config{
		Endpoint:  a.config.RPC.Endpoint,
		Timeout:   a.config.RPC.Timeout,
		Logger:    a.logger,
		Fallbacks: a.config.RPC.FallbackEndpoints,
		RateLimit: a.config.RPC.RateLimit,
	}
	if a.rpcEndpoint != nil {
		// The local record/replay endpoint already does the failover and
		// rate limiting (record) or needs none (replay).
		clientCfg.Fallbacks, clientCfg.RateLimit = nil, 0
	}
	ethClient, err := client.NewClient(clientCfg)
	if err != nil {
		return fmt.Errorf("failed to create Ethereum client: %w", err)
	}

	a.client = ethClient
	a.logger.Info("Connected to Ethereum node", zap.String("endpoint", a.config.RPC.Endpoint))

	// The chain profile: the one --adapter names, otherwise detected.
	ctx := context.Background()
	a.profileSrc, a.profileErr = sourcerpc.Select(ctx, a.client.RPCClient(), a.forceAdapterType)
	if a.profileErr == nil {
		a.profile = a.profileSrc.Profile()
		a.logger.Info("Chain profile selected", zap.String("profile", a.profile.ID()))
	}

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
	baseStore, err := openStore(ctx, &a.config.Database, a.config.NodeRole() == config.RoleAPI, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}
	if r, ok := baseStore.(interface{ SetOrphanRetention(uint64) }); ok {
		r.SetOrphanRetention(a.config.Indexer.OrphanRetention)
	}

	// For multichain mode, use base storage directly
	// For single chain mode, we'll wrap it with genesis initializer later
	a.storage = baseStore

	a.logger.Info("Base storage initialized",
		storeLocation(&a.config.Database),
	)

	return nil
}

// completeStorageInit completes storage initialization for single-chain mode
// This wraps storage with genesis initializer and runs additional setup
func (a *App) completeStorageInit(ctx context.Context) error {
	// Wrap storage with genesis initializer (needs client)
	if g, ok := a.storage.(storage.GenesisBalanceConfigurer); ok {
		g.SetGenesisBalanceResolver(a.client)
		a.logger.Info("Genesis balance auto-initialization enabled")
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
		if errors.Is(err, port.ErrNotFound) {
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
//
//nolint:unused
func (a *App) initStorage(ctx context.Context) error {
	if err := a.initStorageOnly(ctx); err != nil {
		return err
	}
	return a.completeStorageInit(ctx)
}

// initSystemContractVerifications initializes system contract verifications
func (a *App) initSystemContractVerifications(ctx context.Context) error {
	// Cast storage to required interfaces
	writer, ok := a.storage.(port.ContractVerificationWriter)
	if !ok {
		return fmt.Errorf("storage does not support contract verification writes")
	}

	reader, ok := a.storage.(port.ContractVerificationReader)
	if !ok {
		return fmt.Errorf("storage does not support contract verification reads")
	}

	config := &systemcontracts.SystemContractVerificationConfig{
		SourcePath:       a.config.SystemContracts.SourcePath,
		IncludeAbstracts: a.config.SystemContracts.IncludeAbstracts,
		Logger:           a.logger,
	}

	return systemcontracts.InitSystemContractVerifications(ctx, writer, reader, config)
}

// initEventBus initializes the event bus
func (a *App) initEventBus() {
	// A zero size (configuration built without defaults) would make the
	// publish channel unbuffered and drop every event.
	if a.config.EventBus.PublishBufferSize <= 0 {
		a.config.EventBus.PublishBufferSize = constants.DefaultEventBusPublishBuffer
	}
	if a.config.EventBus.SubscriberBufferSize <= 0 {
		a.config.EventBus.SubscriberBufferSize = constants.DefaultEventBusSubscriberBuffer
	}
	a.eventBus = events.NewEventBus(a.config.EventBus.PublishBufferSize, a.config.EventBus.SubscriberBufferSize)
	go a.eventBus.Run()

	a.logger.Info("EventBus initialized",
		zap.Int("publish_buffer", a.config.EventBus.PublishBufferSize),
		zap.Int("subscriber_buffer", a.config.EventBus.SubscriberBufferSize),
	)
}

// configureNotificationsForChain tells the notification service the chain
// profile's native coin contract, whose Transfer logs are not token
// transfers.
func (a *App) configureNotificationsForChain() {
	svc, ok := a.notificationService.(interface {
		SetNonTokenTransferContracts(...common.Address)
	})
	if !ok || a.profile == nil {
		return
	}
	if addr, ok := chains.NativeCoinContract(a.profile); ok {
		svc.SetNonTokenTransferContracts(addr)
	}
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

	// The notification service stores its data in the indexer database
	kvStore, ok := a.storage.(notifications.KeyValueStore)
	if !ok {
		return fmt.Errorf("storage does not support key-value access for notifications")
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

// streamNotifications makes the notification service consume the change
// stream as its own consumer group, so no event of an indexed block is
// lost before its notifications are stored. Without the outbox the service
// keeps its event bus subscription.
func (a *App) streamNotifications() error {
	svc, ok := a.notificationService.(*notifications.NotificationService)
	if !ok || a.fetcher == nil || a.fetcher.Stream() == nil {
		return nil
	}
	if err := svc.SetStream(a.fetcher.Stream(), notifications.DefaultStreamGroup); err != nil {
		return fmt.Errorf("notifications: %w", err)
	}
	return nil
}

// initMultiChainManager creates the manager of multichain mode. It starts
// every enabled chain with newChainIndexer.
func (a *App) initMultiChainManager() error {
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
			Workers:     orDefault(cc.Workers, a.config.Indexer.Workers),
			BatchSize:   orDefault(cc.BatchSize, a.config.Indexer.ChunkSize),
			RPCTimeout:  orDefault(cc.RPCTimeout, a.config.RPC.Timeout),
		})
	}

	managerConfig := &multichain.ManagerConfig{
		Enabled:              true,
		Chains:               chainConfigs,
		HealthCheckInterval:  a.config.MultiChain.HealthCheckInterval,
		MaxUnhealthyDuration: a.config.MultiChain.MaxUnhealthyDuration,
		AutoRestart:          a.config.MultiChain.AutoRestart,
		AutoRestartDelay:     a.config.MultiChain.AutoRestartDelay,
	}

	manager, err := multichain.NewManager(managerConfig, a.newChainIndexer, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create multi-chain manager: %w", err)
	}

	a.multichainManager = manager
	a.logger.Info("Multi-chain manager created",
		zap.Int("chains", len(chainConfigs)),
	)

	return nil
}

// orDefault returns v, or def when v is the zero value.
func orDefault[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// chainDBPath is the database of one chain in multichain mode.
func chainDBPath(root, chainID string) string {
	return filepath.Join(root, "chains", chainID)
}

// chainAppConfig returns the configuration of one chain's App: the shared
// settings with the chain's endpoints, database and indexing range. Settings
// that belong to the process (API server, notifications, contract
// verification, RPC archives) are off; the manager's App serves the API.
func (a *App) chainAppConfig(cc *multichain.ChainConfig) *config.Config {
	cfg := *a.config
	cfg.RPC.Endpoint = cc.RPCEndpoint
	cfg.RPC.WSEndpoint = cc.WSEndpoint
	cfg.RPC.FallbackEndpoints = nil
	cfg.RPC.RecordDir = ""
	cfg.RPC.Timeout = orDefault(cc.RPCTimeout, a.config.RPC.Timeout)
	cfg.Source = config.SourceConfig{}
	cfg.Database = chainDatabase(a.config.Database, cc.ID)
	cfg.Indexer.StartHeight = cc.StartHeight
	cfg.Indexer.Workers = orDefault(cc.Workers, a.config.Indexer.Workers)
	cfg.Indexer.ChunkSize = orDefault(cc.BatchSize, a.config.Indexer.ChunkSize)
	cfg.MultiChain = config.MultiChainConfig{}
	cfg.API.Enabled = false
	cfg.Notifications.Enabled = false
	cfg.Verifier.Enabled = false
	return &cfg
}

// newChainIndexer builds one chain of multichain mode
// (multichain.IndexerFactory): an App of its own over the chain's database,
// so chains never share storage keys (D4).
func (a *App) newChainIndexer(ctx context.Context, cc *multichain.ChainConfig) (multichain.Indexer, error) {
	profile := cc.AdapterType
	if profile == "auto" {
		profile = "" // detected from the node
	}
	app, err := NewApp(a.chainAppConfig(cc), a.logger.With(zap.String("chain", cc.ID)), a.enableGapMode, profile)
	if err != nil {
		return nil, err
	}
	if cc.ChainID != 0 {
		id, err := app.client.GetChainID(ctx)
		if err == nil && (!id.IsUint64() || id.Uint64() != cc.ChainID) {
			err = fmt.Errorf("node at %s serves chain id %s, configured chain_id is %d", cc.RPCEndpoint, id, cc.ChainID)
		}
		if err != nil {
			app.closeAfterFailedStart()
			return nil, fmt.Errorf("chain id check: %w", err)
		}
	}
	return &chainIndexer{app: app}, nil
}

// chainIndexer runs one chain's App for the multichain manager.
type chainIndexer struct {
	app *App
}

func (c *chainIndexer) Run(ctx context.Context) error { return c.app.Run(ctx) }
func (c *chainIndexer) Close()                        { c.app.Shutdown() }
func (c *chainIndexer) Store() port.QueryStore        { return c.app.storage }
func (c *chainIndexer) EventBus() *events.EventBus    { return c.app.eventBus }

func (c *chainIndexer) IndexedHeight(ctx context.Context) (uint64, error) {
	return c.app.storage.GetLatestHeight(ctx)
}

func (c *chainIndexer) NodeHeight(ctx context.Context) (uint64, error) {
	return c.app.client.GetLatestBlockNumber(ctx)
}

// initFetcher initializes the block fetcher
func (a *App) initFetcher(ctx context.Context) error {
	// Real-time mode: Use shorter RetryDelay for batch_size=1
	retryDelay := time.Second * 5
	if a.config.Indexer.ChunkSize == 1 {
		retryDelay = time.Millisecond * 200
	}

	fetcherConfig := &fetch.Config{
		StartHeight:   a.config.Indexer.StartHeight,
		BatchSize:     a.config.Indexer.ChunkSize,
		MaxRetries:    3,
		RetryDelay:    retryDelay,
		NumWorkers:    a.config.Indexer.Workers,
		RPCTimeout:    a.config.RPC.Timeout,
		PollInterval:  a.config.Indexer.PollInterval,
		Finality:      a.config.Indexer.Finality,
		Confirmations: a.config.Indexer.Confirmations,
		NoOutbox:      !a.config.EventBus.Outbox,
		OutboxRetain:  a.config.EventBus.OutboxRetention,
		StreamGroup:   a.config.Node.ID,
	}

	a.fetcher = fetch.NewFetcher(a.client, a.storage, fetcherConfig, a.logger, a.eventBus)
	a.logger.Info("Fetcher initialized",
		zap.Duration("retry_delay", retryDelay),
		zap.Int("batch_size", a.config.Indexer.ChunkSize),
	)

	// The chain profile decodes blocks and gives the default features.
	if a.profileErr != nil {
		return fmt.Errorf("detect chain profile: %w", a.profileErr)
	}
	profile, src := a.profile, a.profileSrc
	var blocks source.Source = src
	if dir := a.config.Source.EraDir; dir != "" {
		es, err := era.OpenDir(dir, profile)
		if err != nil {
			return fmt.Errorf("open era1 archives: %w", err)
		}
		a.eraSource = es
		chained := &source.Chained{First: es, Then: src}
		if err := chained.CheckJoin(ctx); err != nil {
			return fmt.Errorf("era1 archives in %s: %w", dir, err)
		}
		blocks = chained
		first, last := es.Range()
		a.logger.Info("Reading history from era1 archives", zap.String("dir", dir), zap.Uint64("first", first), zap.Uint64("last", last))
	}
	blocks, err := a.declaredSource(src, blocks)
	if err != nil {
		return err
	}
	a.fetcher.SetSource(blocks)
	a.logger.Info("Reading blocks through chain profile", zap.String("profile", profile.ID()))

	enabled, err := feature.Enabled(a.defaultFeatures(profile), a.featureOverrides())
	if err != nil {
		return err
	}
	if err := a.checkDeclaredFeatures(enabled); err != nil {
		return err
	}
	deps := feature.Deps{
		Storage:   a.storage,
		Logger:    a.logger,
		Profile:   profile,
		Publish:   a.fetcher.Publish,
		BalanceAt: a.fetcher.BalanceAt,
		BlockAt:   a.fetcher.BlockAt,
		Contracts: token.NewEthClientAdapter(a.client.EthClient()),
		Settings:  a.config.FeatureSettings,
	}
	pipeline, err := feature.Build(enabled, deps)
	if err != nil {
		return err
	}
	a.fetcher.SetFeatures(pipeline)
	a.features = pipeline
	a.logger.Info("Features enabled", zap.Strings("features", pipeline.Features()))

	// Old blocks are processed for newly enabled features before ingest
	// starts; events are not published for them.
	backfillDeps := deps
	backfillDeps.Publish = func(events.Event) bool { return true }
	backfill, err := feature.Build(enabled, backfillDeps)
	if err != nil {
		return err
	}
	if backfillCommitHook != nil {
		a.fetcher.SetBeforeCommitHook(backfillCommitHook)
		defer a.fetcher.SetBeforeCommitHook(nil)
	}
	if err := a.fetcher.CheckFinality(ctx); err != nil {
		return err
	}
	if err := a.fetcher.Recover(ctx, pipeline.Units(), backfill); err != nil {
		return err
	}

	a.setTokenMetadataFetcher()
	return nil
}

// setTokenMetadataFetcher lets GetTokenBalances fetch the metadata of tokens
// not indexed yet from the node.
func (a *App) setTokenMetadataFetcher() {
	tokenMetadataFetcher := token.NewStorageTokenMetadataFetcherFromEthClient(a.client.EthClient(), a.logger)
	if tokenMetadataFetcher != nil {
		a.storage.SetTokenMetadataFetcher(tokenMetadataFetcher)
		a.logger.Info("Token metadata fetcher configured for on-demand fetching")
	} else {
		a.logger.Warn("Failed to create token metadata fetcher - on-demand fetching will be disabled")
	}
}

// initAPIServer initializes the API server
func (a *App) initAPIServer() error {
	a.logger.Info("Initializing API server...")

	// Initialize RPC Proxy for contract call queries (single-chain mode;
	// multichain mode has no process-wide node)
	if a.client != nil {
		if err := a.initRPCProxy(); err != nil {
			a.logger.Warn("Failed to initialize RPC Proxy, contract call queries will be disabled", zap.Error(err))
		}
	}

	// Initialize Contract Verifier for Etherscan-compatible API; it stores
	// what it verifies, so an API process (read-only) has none.
	if a.config.NodeRole() != config.RoleAPI {
		if err := a.initContractVerifier(); err != nil {
			a.logger.Warn("Failed to initialize Contract Verifier, contract verification will be disabled", zap.Error(err))
		}
	}

	apiConfig := &api.Config{
		Host:                  a.config.API.Host,
		Port:                  a.config.API.Port,
		ReadTimeout:           constants.DefaultReadTimeout,
		WriteTimeout:          constants.DefaultWriteTimeout,
		IdleTimeout:           constants.DefaultIdleTimeout,
		EnableCORS:            a.config.API.EnableCORS,
		AllowedOrigins:        a.config.API.AllowedOrigins,
		TrustedProxies:        a.config.API.TrustedProxies,
		EnableRateLimit:       a.config.API.RateLimit.Enabled,
		RateLimitPerSecond:    a.config.API.RateLimit.PerSecond,
		RateLimitBurst:        a.config.API.RateLimit.Burst,
		DeclaredOnly:          a.config.DeclaredMode(),
		GraphQLMaxDepth:       a.config.API.GraphQL.MaxDepth,
		GraphQLMaxComplexity:  a.config.API.GraphQL.MaxComplexity,
		MaxHeaderBytes:        constants.DefaultMaxHeaderBytes,
		EnableGraphQL:         a.config.API.EnableGraphQL,
		EnableJSONRPC:         a.config.API.EnableJSONRPC,
		EnableWebSocket:       a.config.API.EnableWebSocket,
		EnableREST:            a.config.API.EnableREST,
		GraphQLPath:           constants.DefaultGraphQLPath,
		GraphQLPlaygroundPath: constants.DefaultGraphQLPlaygroundPath,
		JSONRPCPath:           constants.DefaultJSONRPCPath,
		WebSocketPath:         constants.DefaultWebSocketPath,
		ShutdownTimeout:       constants.DefaultShutdownTimeout,
	}
	apiConfig.EnableWebSocketKeepAlive = a.config.API.EnableWebSocketKeepAlive
	if a.config.NodeRole() == config.RoleIngest {
		// API processes serve the API (R4-1); this one serves health and
		// metrics.
		apiConfig.HealthOnly = true
		apiConfig.EnableGraphQL, apiConfig.EnableJSONRPC, apiConfig.EnableWebSocket = false, false, false
		apiConfig.EnableREST = false
	}
	apiConfig.DirectSubscriptions = !a.config.API.SubscriptionEngine
	apiConfig.StreamResume = a.config.EventBus.Outbox

	// Create API server with optional RPC Proxy, Notification Service, and Verifier
	serverOpts := &api.ServerOptions{
		RPCProxy:            a.rpcProxy,
		NotificationService: a.notificationService,
		Verifier:            a.contractVerifier,
	}
	var store port.QueryStore // nil in multichain mode: chains are served under /chains/{id}/
	if a.storage != nil {
		store = a.storage
	}
	if a.multichainManager != nil {
		serverOpts.Chains = a.multichainManager
	}
	apiServer, err := api.NewServerWithOptions(apiConfig, a.logger, store, serverOpts)
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

	if a.streamRelay != nil || a.fetcher == nil {
		return a.runAPINode(ctx)
	}

	// Single-chain mode (legacy)
	// Deliver the events committed before this start first.
	a.fetcher.StartRelay()
	if a.enableGapMode {
		if a.config.DeclaredMode() {
			return errors.New("--gap-recovery reads whole blocks: it does not apply to indexer.mode declared")
		}
		a.logger.Info("Starting with gap recovery enabled")
		return a.fetcher.RunWithGapRecovery(ctx)
	}

	a.followHeads(ctx)
	a.logger.Info("Starting normal indexing mode")
	return a.fetcher.Run(ctx)
}

// followHeads subscribes to newHeads (rpc.ws_endpoint) for the live loop
// until ctx ends.
func (a *App) followHeads(ctx context.Context) {
	if ws := a.config.RPC.WSEndpoint; ws != "" {
		a.fetcher.SetHeadNotifier(sourcerpc.SubscribeHeads(ctx, ws, a.logger))
		a.logger.Info("Following newHeads", zap.String("endpoint", ws))
	}
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
		if err := a.rpcProxy.Stop(); err != nil {
			a.logger.Error("Failed to stop RPC Proxy gracefully", zap.Error(err))
		}
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

	a.closeOrderBook()
	records.Detach(a.storage)
	fetch.DetachProgress(a.storage)

	// Stop EventBus
	if a.eventBus != nil {
		a.eventBus.Stop()
	}

	// Stop the writer after its queued commands, before closing storage
	if a.fetcher != nil {
		a.fetcher.Close()
	}

	// Close storage, after reading the final height
	if a.storage != nil {
		finalHeight, err := a.storage.GetLatestHeight(context.Background())
		if err == nil {
			a.logger.Info("Final statistics", zap.Uint64("latest_height", finalHeight))
		} else if !errors.Is(err, port.ErrNotFound) {
			a.logger.Warn("Failed to read final indexed height", zap.Error(err))
		}
		if err := a.storage.Close(); err != nil {
			a.logger.Error("Failed to close storage", zap.Error(err))
		}
	}

	// Close client (single-chain mode only)
	if a.client != nil {
		a.client.Close()
	}
	a.stopRPCArchive()
	a.closeEraSource()

	// Log final statistics
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
	}

	a.logger.Info("Application stopped")
}

// applyFlags applies command-line flags that were given explicitly.
func applyFlags(cfg *config.Config, f *Flags) {
	if f.set["rpc"] {
		cfg.RPC.Endpoint = f.rpcEndpoint
	}
	if f.set["db"] {
		cfg.Database.Path = f.dbPath
	}
	if f.set["start-height"] {
		cfg.Indexer.StartHeight = f.startHeight
	}
	if f.set["workers"] {
		cfg.Indexer.Workers = f.workers
	}
	if f.set["batch-size"] {
		cfg.Indexer.ChunkSize = f.batchSize
	}
	if f.set["log-level"] {
		cfg.Log.Level = f.logLevel
	}
	if f.set["log-format"] {
		cfg.Log.Format = f.logFormat
	}
}

// applyAPIFlags applies API-related command-line flags that were given
// explicitly. Boolean flags apply both ways (--api=false disables the API).
func applyAPIFlags(cfg *config.Config, f *Flags) {
	if f.set["api"] {
		cfg.API.Enabled = f.enableAPI
	}
	if f.set["api-host"] {
		cfg.API.Host = f.apiHost
	}
	if f.set["api-port"] {
		cfg.API.Port = f.apiPort
	}
	if f.set["graphql"] {
		cfg.API.EnableGraphQL = f.enableGraphQL
	}
	if f.set["jsonrpc"] {
		cfg.API.EnableJSONRPC = f.enableJSONRPC
	}
	if f.set["websocket"] {
		cfg.API.EnableWebSocket = f.enableWebSocket
	}
}

// validateConfig validates the configuration
func validateConfig(cfg *config.Config) error {
	if cfg.RPC.Endpoint == "" && !cfg.MultiChainMode() {
		return fmt.Errorf("RPC endpoint is required (use --rpc flag or set in config.yaml)")
	}
	if cfg.Database.Path == "" && !usesPostgres(&cfg.Database) {
		return fmt.Errorf("database path is required (use --db flag or set in config.yaml)")
	}
	if _, err := databases(cfg); err != nil {
		return err
	}
	if cfg.Indexer.Workers <= 0 {
		return fmt.Errorf("workers must be positive")
	}
	if cfg.Indexer.ChunkSize <= 0 {
		return fmt.Errorf("batch size must be positive")
	}
	if cfg.Database.ReadOnly {
		return fmt.Errorf("database.readonly is not supported: an API process opens the database read-only with node.role: %s", config.RoleAPI)
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

// reindexData clears blockchain data while preserving verification data (ABIs, source code, verification status)
func reindexData(path string, log *zap.Logger) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("Data folder does not exist, nothing to reindex", zap.String("path", path))
			return nil
		}
		return fmt.Errorf("failed to stat data folder: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("data path is not a directory: %s", path)
	}

	log.Warn("Reindexing: clearing blockchain data while preserving verification data", zap.String("path", path))

	// Open Pebble database
	storageConfig := storage.DefaultConfig(path)
	storageConfig.ReadOnly = false
	db, err := storage.NewPebbleStorage(storageConfig)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Every prefix storing chain data is deleted; user data (contract
	// verification) is preserved. Packages register their prefixes
	// (storage.RegisterKeyspace), so new data cannot be missed here.
	preservePrefixes := storage.PrefixesOf(storage.Preserved)
	deletePrefixes := storage.PrefixesOf(storage.ChainData)

	var deletedCount int64
	var preservedCount int64

	// Delete data with specified prefixes
	for _, prefix := range deletePrefixes {
		count, err := db.DeleteByPrefix([]byte(prefix))
		if err != nil {
			log.Warn("Failed to delete prefix", zap.String("prefix", prefix), zap.Error(err))
			continue
		}
		if count > 0 {
			log.Debug("Deleted keys with prefix", zap.String("prefix", prefix), zap.Int64("count", count))
			deletedCount += count
		}
	}

	// Log preserved prefixes info
	for _, prefix := range preservePrefixes {
		count, err := db.CountByPrefix([]byte(prefix))
		if err != nil {
			log.Warn("Failed to count preserved prefix", zap.String("prefix", prefix), zap.Error(err))
			continue
		}
		if count > 0 {
			log.Info("Preserved keys with prefix", zap.String("prefix", prefix), zap.Int64("count", count))
			preservedCount += count
		}
	}

	log.Info("Reindex completed",
		zap.Int64("deleted_keys", deletedCount),
		zap.Int64("preserved_keys", preservedCount),
		zap.String("path", path),
	)

	return nil
}

// featureOverrides returns the configured feature overrides. The older
// account_abstraction.enabled: false still turns off the ERC-4337 and
// ERC-7579 features unless the features section names them.
func (a *App) featureOverrides() map[string]bool {
	overrides := a.config.FeatureOverrides()
	if !a.config.AccountAbstraction.Enabled {
		for _, name := range []string{aa.ERC4337, aa.ERC7579} {
			if _, set := overrides[name]; !set {
				overrides[name] = false
			}
		}
	}
	return overrides
}

// closeAfterFailedStart releases the resources NewApp opened before failing.
func (a *App) closeAfterFailedStart() {
	a.closeOrderBook()
	records.Detach(a.storage)
	fetch.DetachProgress(a.storage)
	if a.eventBus != nil {
		a.eventBus.Stop()
	}
	if a.fetcher != nil {
		a.fetcher.Close()
	}
	if a.storage != nil {
		if err := a.storage.Close(); err != nil {
			a.logger.Error("Failed to close storage after failed start", zap.Error(err))
		}
	}
	if a.client != nil {
		a.client.Close()
	}
	a.stopRPCArchive()
	a.closeEraSource()
}

// closeEraSource closes the era1 archives.
func (a *App) closeEraSource() {
	if a.eraSource != nil {
		_ = a.eraSource.Close()
		a.eraSource = nil
	}
}

// startRPCArchive puts a local endpoint between the indexer and the node:
// a recording proxy (rpc.record_dir) or a replay of an archive
// (rpc.endpoint: replay:///dir). Every RPC user then goes through it.
func (a *App) startRPCArchive() error {
	if dir, ok := replay.ParseEndpoint(a.config.RPC.Endpoint); ok {
		archive, err := replay.Open(dir)
		if err != nil {
			return fmt.Errorf("open replay archive: %w", err)
		}
		a.rpcReplay = replay.NewServer(archive)
		ep, err := replay.Serve(a.rpcReplay)
		if err != nil {
			return err
		}
		a.rpcEndpoint = ep
		a.logger.Info("Replaying recorded RPC archive", zap.String("dir", dir),
			zap.Uint64("first", archive.Manifest().First), zap.Uint64("last", archive.Manifest().Last))
		a.config.RPC.Endpoint = ep.URL
		return nil
	}
	if a.config.RPC.RecordDir != "" {
		w, err := replay.NewWriter(a.config.RPC.RecordDir)
		if err != nil {
			return fmt.Errorf("open RPC record dir: %w", err)
		}
		pool, err := rpcpool.New(rpcpool.Config{
			Endpoints: append([]string{a.config.RPC.Endpoint}, a.config.RPC.FallbackEndpoints...),
			Timeout:   a.config.RPC.Timeout,
			RateLimit: a.config.RPC.RateLimit,
			Logger:    a.logger,
		})
		if err != nil {
			_ = w.Close()
			return err
		}
		ep, err := replay.Serve(replay.NewRecorderVia(pool.Primary(), pool, w))
		if err != nil {
			_ = w.Close()
			return err
		}
		a.rpcRecorder, a.rpcEndpoint = w, ep
		a.logger.Info("Recording RPC calls", zap.String("dir", a.config.RPC.RecordDir))
		a.config.RPC.Endpoint = ep.URL
	}
	return nil
}

// stopRPCArchive stops the local endpoint and finishes the recording.
func (a *App) stopRPCArchive() {
	if a.rpcEndpoint != nil {
		_ = a.rpcEndpoint.Close()
		a.rpcEndpoint = nil
	}
	if a.rpcRecorder != nil {
		if err := a.rpcRecorder.Close(); err != nil {
			a.logger.Error("Failed to finish RPC recording", zap.Error(err))
		}
		a.rpcRecorder = nil
	}
}

// backfillCommitHook is a fault-injection point for tests: it runs before
// each block commits during startup recovery (Fetcher.Recover).
var backfillCommitHook func(height uint64) error
