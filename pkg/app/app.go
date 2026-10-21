// Package app is the indexer engine: wiring of storage, fetcher, features
// and API, and the lifecycle of one run. It links in the standard features
// and chain profiles. Start runs it with a validated configuration; the
// command line (cmd/cli) builds that configuration and calls Start.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/api"
	_ "github.com/0xmhha/indexer-go/pkg/api/indexing"
	"github.com/0xmhha/indexer-go/pkg/chains"
	_ "github.com/0xmhha/indexer-go/pkg/chains/evm" // generic EVM chain profile
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
	"github.com/0xmhha/indexer-go/pkg/opsguard"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/source/era"
	"github.com/0xmhha/indexer-go/pkg/source/nodefeed"
	"github.com/0xmhha/indexer-go/pkg/source/replay"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/startup"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/stream"
	"github.com/0xmhha/indexer-go/pkg/token"
	"github.com/0xmhha/indexer-go/pkg/verifier"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
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
	eraSource   *era.Source
	nodeFeed    *nodefeed.Source
	managedFeed *managedFeed

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

	// opsMonitor halts the process on capacity (operations.capacity); only
	// the root App owns one. rawObserver receives raw window usage.
	// statsDetach stops the monitor sampling this App's PostgreSQL store.
	opsMonitor  *opsguard.Monitor
	rawObserver func(window.Usage)
	statsDetach func()
	haltMu      sync.Mutex
	haltRun     context.CancelCauseFunc

	// Runtime flags
	enableGapMode    bool
	forceAdapterType string
	// checkChain refuses a node the binary is not built for (nil: any).
	checkChain ChainCheck
}

// NewApp creates and initializes a new application instance
func NewApp(cfg *config.Config, log *zap.Logger, enableGapMode bool, forceAdapterType string) (*App, error) {
	return newApp(cfg, log, StartOptions{GapRecovery: enableGapMode, Adapter: forceAdapterType})
}

func newApp(cfg *config.Config, log *zap.Logger, opts StartOptions) (_ *App, err error) {
	if err := preflightFeeds(cfg, opts.GapRecovery); err != nil {
		return nil, err
	}
	app := &App{
		config:           cfg,
		logger:           log,
		enableGapMode:    opts.GapRecovery,
		forceAdapterType: opts.Adapter,
		checkChain:       opts.CheckChain,
	}
	// A failed start releases what it opened (the database lock above all),
	// so the caller can retry in the same process.
	defer func() {
		if err != nil {
			app.closeAfterFailedStart()
		}
	}()
	if err := app.initOperations(); err != nil {
		return nil, err
	}

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
		Endpoint:      a.config.RPC.Endpoint,
		Timeout:       a.config.RPC.Timeout,
		Logger:        a.logger,
		Fallbacks:     a.config.RPC.FallbackEndpoints,
		RateLimit:     a.config.RPC.RateLimit,
		CoalesceReads: a.config.RPC.CoalesceReads,
	}
	if a.rpcEndpoint != nil {
		// The local record/replay endpoint already does the failover and
		// rate limiting (record) or needs none (replay).
		clientCfg.Fallbacks, clientCfg.RateLimit = nil, 0
		clientCfg.CoalesceReads = false
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
		if a.checkChain != nil {
			infoCtx, cancel := context.WithTimeout(ctx, a.config.RPC.Timeout)
			info, err := sourcerpc.NodeInfo(infoCtx, a.client.RPCClient())
			cancel()
			if err != nil {
				return fmt.Errorf("check the node's chain: %w", err)
			}
			if err := a.checkChain(info, a.profile); err != nil {
				return err
			}
		}
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
	if svc, ok := a.notificationService.(interface{ SetChainID(uint64) }); ok && chainID.IsUint64() {
		svc.SetChainID(chainID.Uint64())
	}
	return nil
}

// initStorageOnly initializes only the base storage layer without genesis initialization
// This is used when multichain mode is enabled (each chain handles its own genesis)
func (a *App) initStorageOnly(ctx context.Context) error {
	apiProcess := a.config.NodeRole() == config.RoleAPI
	var writable []string
	if apiProcess && a.config.Notifications.Enabled {
		writable = notifications.KeyPrefixes() // the API process manages notification settings
	}
	baseStore, err := openStore(ctx, &a.config.Database, apiProcess, writable, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}
	if r, ok := baseStore.(interface{ SetOrphanRetention(uint64) }); ok {
		r.SetOrphanRetention(a.config.Indexer.OrphanRetention)
	}

	// For multichain mode, use base storage directly
	// For single chain mode, we'll wrap it with genesis initializer later
	a.storage = baseStore
	a.attachPostgresStats(a.opsMonitor)

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

	// Preparation steps of the linked-in chain packages (pkg/startup).
	startup.Run(ctx, startup.Env{Config: a.config, Storage: a.storage, Logger: a.logger}, func(name string, err error) {
		a.logger.Warn("Startup step failed", zap.String("step", name), zap.Error(err))
	})

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

	for _, label := range a.config.Notifications.OperatorLabels {
		if _, ok := a.config.API.Keys[label]; !ok {
			a.logger.Warn("notifications.operator_labels names a label without a key in api.keys", zap.String("label", label))
		}
	}
	if a.config.Notifications.AllowPrivateDestinations {
		a.logger.Warn("notifications.allow_private_destinations: webhooks may reach internal addresses (development only)")
	}

	// Convert config to notification service config
	notifConfig := &notifications.Config{
		Enabled:             a.config.Notifications.Enabled,
		OperatorLabels:      a.config.Notifications.OperatorLabels,
		MaxSettingsPerOwner: a.config.Notifications.MaxSettingsPerOwner,
		MaxStreamsPerOwner:  a.config.Notifications.MaxStreamsPerOwner,

		DestinationRateLimit: a.config.Notifications.DestinationRateLimit,
		DestinationBurst:     a.config.Notifications.DestinationBurst,
		Webhook: notifications.WebhookConfig{
			Enabled:         a.config.Notifications.Webhook.Enabled,
			Timeout:         a.config.Notifications.Webhook.Timeout,
			MaxRetries:      a.config.Notifications.Webhook.MaxRetries,
			MaxConcurrent:   a.config.Notifications.Webhook.MaxConcurrent,
			AllowedHosts:    a.config.Notifications.Webhook.AllowedHosts,
			SignatureHeader: a.config.Notifications.Webhook.SignatureHeader,

			VerifyDestinations: a.config.Notifications.Webhook.VerifyDestinations,

			AllowPrivateDestinations: a.config.Notifications.AllowPrivateDestinations,
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

			AllowPrivateDestinations: a.config.Notifications.AllowPrivateDestinations,
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
	if !ok || a.fetcher == nil {
		return nil
	}
	// Fast settings are evaluated as blocks are fetched, before the commit.
	a.fetcher.SetBlockTap(svc)
	if a.fetcher.Stream() == nil {
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
			NodeFeedSocket:    cc.NodeFeedSocket,
			Feed:              cc.Feed,
			CoalesceReads:     cc.CoalesceReads,
			ID:                cc.ID,
			Name:              cc.Name,
			RPCEndpoint:       cc.RPCEndpoint,
			WSEndpoint:        cc.WSEndpoint,
			FallbackEndpoints: cc.FallbackEndpoints,
			ChainID:           cc.ChainID,
			AdapterType:       cc.AdapterType,
			StartHeight:       cc.StartHeight,
			Enabled:           cc.Enabled,
			Workers:           orDefault(cc.Workers, a.config.Indexer.Workers),
			StatePrefetch:     cc.StatePrefetch,
			StateBatchSize:    cc.StateBatchSize,
			AcquisitionQueue:  cc.AcquisitionQueue,
			QueueBlocks:       cc.QueueBlocks,
			QueueMaxBytes:     cc.QueueMaxBytes,
			BatchSize:         orDefault(cc.BatchSize, a.config.Indexer.ChunkSize),
			RPCTimeout:        orDefault(cc.RPCTimeout, a.config.RPC.Timeout),
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
	manager.SetFailurePolicy(a.chainFailure)

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

// chainFeatureOverrides returns the feature sections configured on the
// chain id's entry.
func (a *App) chainFeatureOverrides(id string) map[string]config.FeatureConfig {
	for _, cc := range a.config.MultiChain.Chains {
		if cc.ID == id {
			return cc.Features
		}
	}
	return nil
}

// chainFeatures returns a chain's feature sections: the shared ones, each
// replaced by the chain's own section where it has one; a chain section
// without enabled keeps the shared enabled. The result is a new map, so
// chains never share or change the shared sections.
func chainFeatures(shared, chain map[string]config.FeatureConfig) map[string]config.FeatureConfig {
	out := make(map[string]config.FeatureConfig, len(shared)+len(chain))
	for name, fc := range shared {
		out[name] = fc
	}
	for name, fc := range chain {
		if fc.Enabled == nil {
			fc.Enabled = shared[name].Enabled
		}
		out[name] = fc
	}
	return out
}

// chainAppConfig returns the configuration of one chain's App: the shared
// settings with the chain's endpoints, database and indexing range. Settings
// that belong to the process (API server, contract verification, RPC
// archives) are off; the manager's App serves the API. Each chain runs its
// own notification service over its own database, so a setting watches
// the chain it was created under (/chains/{id}/).
func (a *App) chainAppConfig(cc *multichain.ChainConfig) *config.Config {
	cfg := *a.config
	// The root App owns capacity monitoring and the halt latch.
	cfg.Operations = opsguard.Config{}
	cfg.RPC.Endpoint = cc.RPCEndpoint
	if cc.CoalesceReads != nil {
		cfg.RPC.CoalesceReads = *cc.CoalesceReads
	}
	cfg.RPC.WSEndpoint = cc.WSEndpoint
	cfg.RPC.FallbackEndpoints = cc.FallbackEndpoints
	cfg.RPC.RecordDir = ""
	cfg.RPC.Timeout = orDefault(cc.RPCTimeout, a.config.RPC.Timeout)
	cfg.Source = config.SourceConfig{NodeFeedSocket: cc.NodeFeedSocket, Feed: cc.Feed}
	if cc.Feed == nil {
		cfg.WindowStorage = window.Layout{}
	}
	cfg.Database = chainDatabase(a.config.Database, cc.ID)
	cfg.Indexer.StartHeight = cc.StartHeight
	cfg.Indexer.Workers = orDefault(cc.Workers, a.config.Indexer.Workers)
	if cc.StatePrefetch != nil {
		cfg.Indexer.StatePrefetch = *cc.StatePrefetch
	}
	if cc.StateBatchSize != nil {
		cfg.Indexer.StateBatchSize = *cc.StateBatchSize
	}
	if cc.AcquisitionQueue != nil {
		cfg.Indexer.AcquisitionQueue = *cc.AcquisitionQueue
	}
	if cc.QueueBlocks != nil {
		cfg.Indexer.QueueBlocks = *cc.QueueBlocks
	}
	if cc.QueueMaxBytes != nil {
		cfg.Indexer.QueueMaxBytes = *cc.QueueMaxBytes
	}
	cfg.Indexer.ChunkSize = orDefault(cc.BatchSize, a.config.Indexer.ChunkSize)
	cfg.Features = chainFeatures(a.config.Features, a.chainFeatureOverrides(cc.ID))
	cfg.MultiChain = config.MultiChainConfig{}
	cfg.API.Enabled = false // the manager's App serves every chain's API, notifications included
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
	app, err := newApp(a.chainAppConfig(cc), a.logger.With(zap.String("chain", cc.ID)), StartOptions{GapRecovery: a.enableGapMode, Adapter: profile, CheckChain: a.checkChain})
	if err != nil {
		return nil, err
	}
	// Set before the manager runs the chain, which starts its sessions.
	app.rawObserver = a.rawObserver
	app.attachPostgresStats(a.opsMonitor)
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

// chainNotifications serves each running chain's notification service and
// stream connections to the API (api.ChainNotifications), besides its
// store (api.ChainStores).
type chainNotifications struct{ *multichain.Manager }

func (c chainNotifications) ChainNotifications(id string) (notifications.Service, *notifications.Streams, bool) {
	idx, ok := c.ChainIndexer(id)
	if !ok {
		return nil, nil, false
	}
	ci, ok := idx.(*chainIndexer)
	if !ok {
		return nil, nil, false
	}
	svc, ok := ci.app.notificationService.(*notifications.NotificationService)
	if !ok {
		return nil, nil, false
	}
	return svc, svc.Streams(), true
}

func (c *chainIndexer) Run(ctx context.Context) error { return c.app.Run(ctx) }
func (c *chainIndexer) Close()                        { c.app.Shutdown() }
func (c *chainIndexer) Store() port.QueryStore        { return c.app.storage }
func (c *chainIndexer) EventBus() *events.EventBus    { return c.app.eventBus }

func (c *chainIndexer) IndexedHeight(ctx context.Context) (uint64, error) {
	return c.app.storage.GetLatestHeight(ctx)
}

func (c *chainIndexer) NodeHeight(ctx context.Context) (uint64, error) {
	if c.app.managedFeed != nil {
		return c.app.profileSrc.Head(ctx)
	}
	n, _, err := c.app.fetcher.ObserveNodeHeight(ctx, 0)
	return n, err
}

func (c *chainIndexer) ObserveNodeHeight(ctx context.Context, maxAge time.Duration) (uint64, time.Duration, error) {
	if c.app.managedFeed != nil {
		start := time.Now()
		n, e := c.app.profileSrc.Head(ctx)
		return n, time.Since(start), e
	}
	return c.app.fetcher.ObserveNodeHeight(ctx, maxAge)
}

// initFetcher initializes the block fetcher
func (a *App) initFetcher(ctx context.Context) error {
	if a.config.Source.Feed != nil {
		return a.initManagedFeed(ctx)
	}
	// Real-time mode: Use shorter RetryDelay for batch_size=1
	retryDelay := time.Second * 5
	if a.config.Indexer.ChunkSize == 1 {
		retryDelay = time.Millisecond * 200
	}

	fetcherConfig := &fetch.Config{
		StatePrefetch:    a.config.Indexer.StatePrefetch,
		PipelineMetrics:  a.config.Indexer.PipelineMetrics,
		StateMaxBytes:    a.config.Indexer.StateMaxBytes,
		AcquisitionQueue: a.config.Indexer.AcquisitionQueue,
		QueueBlocks:      a.config.Indexer.QueueBlocks,
		QueueMaxBytes:    a.config.Indexer.QueueMaxBytes,
		StartHeight:      a.config.Indexer.StartHeight,
		BatchSize:        a.config.Indexer.ChunkSize,
		MaxRetries:       3,
		RetryDelay:       retryDelay,
		NumWorkers:       a.config.Indexer.Workers,
		RPCTimeout:       a.config.RPC.Timeout,
		PollInterval:     a.config.Indexer.PollInterval,
		Finality:         a.config.Indexer.Finality,
		Confirmations:    a.config.Indexer.Confirmations,
		NoOutbox:         !a.config.EventBus.Outbox,
		OutboxRetain:     a.config.EventBus.OutboxRetention,
		StreamGroup:      a.config.Node.ID,
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
	if socket := a.config.Source.NodeFeedSocket; socket != "" {
		if a.config.Source.EraDir != "" || a.config.DeclaredMode() || !a.config.Indexer.AcquisitionQueue || !a.config.Indexer.StatePrefetch {
			return fmt.Errorf("node-feed requires full queued state preparation without era archives")
		}
		id, err := a.client.GetChainID(ctx)
		if err != nil {
			return err
		}
		if !id.IsUint64() {
			return fmt.Errorf("node-feed chain id exceeds uint64")
		}
		feed, err := nodefeed.New(ctx, nodefeed.Config{Socket: socket, ChainID: id.Uint64(), Timeout: a.config.RPC.Timeout, Consumer: a.config.Node.ID + "|" + a.config.Database.Path + "|" + a.config.Database.Postgres.Schema}, src)
		if err != nil {
			return err
		}
		a.nodeFeed, blocks = feed, feed
		if resolver, ok := a.storage.(storage.GenesisBalanceConfigurer); ok {
			resolver.SetGenesisBalanceResolver(feed)
		}
	}
	a.fetcher.SetSource(blocks)
	a.fetcher.SetStateSource(src)
	a.logger.Info("Reading blocks through chain profile", zap.String("profile", profile.ID()))

	enabled, err := feature.Enabled(a.defaultFeatures(profile), a.featureOverrides())
	if err != nil {
		return err
	}
	if err := a.checkDeclaredFeatures(enabled); err != nil {
		return err
	}
	deps := feature.Deps{
		PrepareConcurrency: orDefault(a.config.Indexer.StateWorkers, 4),
		StateBatchSize:     orDefault(a.config.Indexer.StateBatchSize, 1),
		Storage:            a.storage,
		Logger:             a.logger,
		Profile:            profile,
		Publish:            a.fetcher.Publish,
		BalanceAt:          a.fetcher.BalanceAt,
		BalanceAtHash:      a.fetcher.BalanceAtHash,
		BalanceBatchAtHash: a.fetcher.BalanceBatchAtHash,
		BlockAt:            a.fetcher.BlockAt,
		Contracts:          token.NewEthClientAdapter(a.client.EthClient()),
		Settings:           a.config.FeatureSettings,
	}
	if a.nodeFeed != nil {
		deps.BalanceAt = a.nodeFeed.BalanceAt
		deps.BalanceAtHash = a.nodeFeed.BalanceAtHash
		deps.BalanceBatchAtHash = a.nodeFeed.BalanceBatchAtHash
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
	if err := a.checkNodeState(ctx, pipeline.Units(), pipeline.Features()); err != nil {
		return err
	}
	return a.fetcher.Recover(ctx, pipeline.Units(), backfill)
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
		// The measured cohosted pool4 policy keeps one short-read slot.
		// Other budgets and dedicated API/ingest roles retain their capacity.
		ReserveQueryReader:    a.config.NodeRole() == config.RoleAll && a.config.Database.Postgres.MaxConns == 4,
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
	if len(a.config.API.Keys) > 0 {
		apiConfig.APIKeys = make(map[string]string, len(a.config.API.Keys))
		for label, key := range a.config.API.Keys {
			apiConfig.APIKeys[key] = label
		}
	} else if a.config.Notifications.Enabled {
		a.logger.Warn("No api.keys: the notification API refuses every request")
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
		if a.config.Notifications.Enabled {
			serverOpts.Chains = chainNotifications{a.multichainManager}
		}
	}
	// The stream is served by the process that evaluates blocks: not by an
	// API process, whose ingest process creates the notifications.
	if svc, ok := a.notificationService.(*notifications.NotificationService); ok && a.config.NodeRole() != config.RoleAPI {
		serverOpts.NotificationStreams = svc.Streams()
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
	// GetTokenBalances reads the metadata of tokens not indexed yet from
	// the node through the proxy (its cache, rate limit, circuit breaker).
	a.storage.SetTokenMetadataFetcher(&proxyTokenMetadata{proxy: proxy, logger: a.logger})
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
		if cerr := solcCompiler.Close(); cerr != nil {
			a.logger.Warn("Failed to close Solidity compiler", zap.Error(cerr))
		}
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
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	a.haltMu.Lock()
	a.haltRun = cancel
	a.haltMu.Unlock()
	var monitor sync.WaitGroup
	if a.opsMonitor != nil {
		monitor.Add(1)
		go func() {
			defer monitor.Done()
			_ = a.opsMonitor.Run(ctx)
		}()
	}
	err := a.run(ctx)
	cause := context.Cause(ctx)
	cancel(nil)
	monitor.Wait()
	var h *opsguard.HaltError
	if errors.As(cause, &h) {
		return h
	}
	a.latchFailure("", err)
	return err
}

// run indexes until ctx ends.
func (a *App) run(ctx context.Context) error {
	a.logger.Info("Starting indexing...")

	// Start notification service if enabled. An API process only serves
	// the notification API: the indexing process delivers.
	if a.notificationService != nil && a.config.NodeRole() != config.RoleAPI {
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

	if a.managedFeed != nil {
		return a.managedFeed.Run(ctx)
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
	if a.managedFeed != nil {
		a.managedFeed.Stop()
	}

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
	a.closeOperations()
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
	if a.nodeFeed != nil {
		a.nodeFeed.Close()
	}
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

// validateConfig validates the configuration
func validateConfig(cfg *config.Config) error {
	if cfg.Indexer.QueueBlocks < 0 || cfg.Indexer.QueueBlocks > 65536 || cfg.Indexer.QueueMaxBytes < 0 {
		return fmt.Errorf("invalid acquisition queue limits")
	}
	if cfg.Indexer.AcquisitionQueue && !cfg.Indexer.StatePrefetch {
		return fmt.Errorf("acquisition_queue requires state_prefetch")
	}

	if cfg.Indexer.StateBatchSize < 0 || cfg.Indexer.StateBatchSize > 64 {
		return fmt.Errorf("state_batch_size must be between 1 and 64 (0 defaults to 1)")
	}
	if cfg.Indexer.StateWorkers < 0 {
		return fmt.Errorf("state workers must not be negative")
	}
	if cfg.Indexer.StateMaxBytes < 0 {
		return fmt.Errorf("state_max_bytes must not be negative")
	}
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
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("Failed to close database after reindex", zap.Error(err))
		}
	}()

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
	if a.managedFeed != nil {
		a.managedFeed.Stop()
	}
	a.closeOrderBook()
	a.closeOperations()
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
	if a.nodeFeed != nil {
		a.nodeFeed.Close()
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
