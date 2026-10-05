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
	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/internal/logger"
	"github.com/0xmhha/indexer-go/pkg/adapters/detector"
	"github.com/0xmhha/indexer-go/pkg/adapters/factory"
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
	_ "github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts/api"      // StableNet system contract API
	"github.com/0xmhha/indexer-go/pkg/client"
	"github.com/0xmhha/indexer-go/pkg/compiler"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/aa"
	_ "github.com/0xmhha/indexer-go/pkg/features/address" // address.index feature
	_ "github.com/0xmhha/indexer-go/pkg/features/balance" // balance.native feature
	_ "github.com/0xmhha/indexer-go/pkg/features/token"   // token.transfers feature
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/multichain"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/source/era"
	"github.com/0xmhha/indexer-go/pkg/source/replay"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/token"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"github.com/0xmhha/indexer-go/pkg/verifier"
	"github.com/ethereum/go-ethereum/common"
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
	// profile is the chain profile selected by --adapter or detected (nil if
	// detection failed, see profileErr); profileSrc reads blocks with it.
	profile    chains.Profile
	profileSrc *sourcerpc.Source
	profileErr error
	features     *feature.Pipeline // enabled features, in execution order
	storage      storage.Storage
	eventBus     *events.EventBus
	fetcher      *fetch.Fetcher
	apiServer    *api.Server
	rpcProxy     *rpcproxy.Proxy

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
	defer func() { _ = log.Sync() }()

	for _, msg := range cfg.UnsupportedSettings() {
		log.Warn("Unsupported setting ignored", zap.String("detail", msg))
	}

	// Log startup information
	logStartupInfo(log, cfg, flags)

	// Clear data folder if requested
	if flags.clearData {
		if err := clearDataFolder(cfg.Database.Path, log); err != nil {
			return fmt.Errorf("failed to clear data folder: %w", err)
		}
	}

	// Reindex: clear blockchain data while preserving verification data
	if flags.reindex && !flags.clearData {
		if err := reindexData(cfg.Database.Path, log); err != nil {
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
	forceAdapterType string // Force specific adapter type: anvil, stableone, evm

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
	fs.StringVar(&f.forceAdapterType, "adapter", "", "Force specific adapter type (anvil, stableone, evm). Auto-detected if empty")

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
		zap.Bool("reindex", flags.reindex),
		zap.String("adapter", adapterInfo),
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

	if err := app.startRPCArchive(); err != nil {
		return nil, err
	}

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

	// The chain profile: the one --adapter names, otherwise detected. The
	// adapter factory follows it, so both agree on the chain.
	ctx := context.Background()
	a.profileSrc, a.profileErr = sourcerpc.Select(ctx, a.client.RPCClient(), a.forceAdapterType)
	if a.profileErr == nil {
		a.profile = a.profileSrc.Profile()
	}

	// Create chain adapter using factory with auto-detection
	factoryConfig := factory.DefaultConfig(a.config.RPC.Endpoint)
	factoryConfig.ForceAdapterType = a.forceAdapterType
	factoryConfig.Profile = a.profile

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
	baseStore.SetOrphanRetention(a.config.Indexer.OrphanRetention)

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
	writer, ok := a.storage.(storage.ContractVerificationWriter)
	if !ok {
		return fmt.Errorf("storage does not support contract verification writes")
	}

	reader, ok := a.storage.(storage.ContractVerificationReader)
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
		AtomicBlock:   a.config.Indexer.AtomicBlock,
		RPCTimeout:    a.config.RPC.Timeout,
		PollInterval:  a.config.Indexer.PollInterval,
		Finality:      a.config.Indexer.Finality,
		Confirmations: a.config.Indexer.Confirmations,
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

	// The chain profile decodes blocks (profile_source) and gives the
	// default features. Without profile_source a failed detection only
	// leaves the features to the configuration.
	profile, src := a.profile, a.profileSrc
	switch {
	case a.profileErr == nil:
	case a.config.Indexer.ProfileSource:
		return fmt.Errorf("detect chain profile: %w", a.profileErr)
	default:
		a.logger.Warn("Chain profile detection failed; using configured features only", zap.Error(a.profileErr))
	}
	if a.config.Indexer.ProfileSource {
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
		a.fetcher.SetSource(blocks)
		a.logger.Info("Reading blocks through chain profile", zap.String("profile", profile.ID()))
	}

	defaults := feature.Defaults()
	if profile != nil {
		defaults = append(defaults, profile.Features()...)
	}
	enabled, err := feature.Enabled(defaults, a.featureOverrides())
	if err != nil {
		return err
	}
	deps := feature.Deps{
		Storage:   a.storage,
		Logger:    a.logger,
		Profile:   profile,
		Publish:   a.fetcher.Publish,
		BalanceAt: a.fetcher.BalanceAt,
		BlockAt:   a.fetcher.BlockAt,
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
	if err := a.fetcher.Recover(ctx, pipeline.Features(), backfill); err != nil {
		return err
	}

	a.registerFeatureProcessors()

	// Add token block processor for automatic token metadata indexing
	tokenProcessor := token.NewBlockProcessorFromEthClient(a.client.EthClient(), a.storage, a.logger)
	a.fetcher.AddBlockProcessor(tokenProcessor)
	a.logger.Info("Token block processor added to fetcher")

	// Set up on-demand token metadata fetcher for storage
	// This allows GetTokenBalances to fetch metadata for tokens not yet indexed
	tokenMetadataFetcher := token.NewStorageTokenMetadataFetcherFromEthClient(a.client.EthClient(), a.logger)
	if tokenMetadataFetcher != nil {
		a.storage.SetTokenMetadataFetcher(tokenMetadataFetcher)
		a.logger.Info("Token metadata fetcher configured for on-demand fetching")
	} else {
		a.logger.Warn("Failed to create token metadata fetcher - on-demand fetching will be disabled")
	}
	return nil
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
		ReadTimeout:           constants.DefaultReadTimeout,
		WriteTimeout:          constants.DefaultWriteTimeout,
		IdleTimeout:           constants.DefaultIdleTimeout,
		EnableCORS:            a.config.API.EnableCORS,
		AllowedOrigins:        a.config.API.AllowedOrigins,
		MaxHeaderBytes:        constants.DefaultMaxHeaderBytes,
		EnableGraphQL:         a.config.API.EnableGraphQL,
		EnableJSONRPC:         a.config.API.EnableJSONRPC,
		EnableWebSocket:       a.config.API.EnableWebSocket,
		GraphQLPath:           constants.DefaultGraphQLPath,
		GraphQLPlaygroundPath: constants.DefaultGraphQLPlaygroundPath,
		JSONRPCPath:           constants.DefaultJSONRPCPath,
		WebSocketPath:         constants.DefaultWebSocketPath,
		ShutdownTimeout:       constants.DefaultShutdownTimeout,
	}
	apiConfig.EnableWebSocketKeepAlive = a.config.API.EnableWebSocketKeepAlive

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

	// Stop the writer after its queued commands, before closing storage
	if a.fetcher != nil {
		a.fetcher.Close()
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
	a.stopRPCArchive()
	a.closeEraSource()

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
	if cfg.MultiChain.Enabled && len(cfg.MultiChain.Chains) > 0 {
		return fmt.Errorf("multichain mode is disabled: all chains would share the same storage keys and overwrite each other (refactoring plan D4, R2-8)")
	}
	if cfg.Database.ReadOnly {
		return fmt.Errorf("database.readonly is not supported: the indexer must write; an API-only role is planned (refactoring plan R4-1)")
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

// The StableNet RPC client must satisfy the fetcher's fee delegation
// interface; this fails to compile if their metadata types drift apart.
var _ fetch.FeeDelegationClient = (*factory.EVMClient)(nil)

// registerFeatureProcessors connects the legacy client's fee delegation
// re-fetch. It is removed with the legacy client path; every other
// per-block processor is a feature (pkg/features).
func (a *App) registerFeatureProcessors() {
	// Fee delegation (type 0x16) exists only on StableNet nodes.
	if a.nodeInfo != nil && a.nodeInfo.Type == detector.NodeTypeStableOne {
		a.fetcher.SetFeeDelegationClient(factory.NewEVMClient(a.client.RPCClient()))
	}
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
	if a.eventBus != nil {
		a.eventBus.Stop()
	}
	if a.chainAdapter != nil {
		_ = a.chainAdapter.Close()
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
		ep, err := replay.Serve(replay.NewRecorder(a.config.RPC.Endpoint, w))
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
