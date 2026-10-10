package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/stream"
)

// Service defines the notification service interface.
type Service interface {
	// Start starts the notification service.
	Start(ctx context.Context) error

	// Stop gracefully stops the notification service.
	Stop(ctx context.Context) error

	// Settings management
	CreateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error)
	UpdateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error)
	DeleteSetting(ctx context.Context, id string) error
	GetSetting(ctx context.Context, id string) (*NotificationSetting, error)
	ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error)

	// Notification operations
	GetNotification(ctx context.Context, id string) (*Notification, error)
	ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error)
	RetryNotification(ctx context.Context, id string) error
	CancelNotification(ctx context.Context, id string) error

	// Statistics
	GetStats(ctx context.Context, settingID string) (*NotificationStats, error)
	GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error)

	// Testing
	TestSetting(ctx context.Context, id string) (*DeliveryResult, error)
}

// SettingsFilter for listing notification settings.
type SettingsFilter struct {
	Types      []NotificationType
	EventTypes []EventType
	Enabled    *bool
	Limit      int
	Offset     int
}

// NotificationsFilter for listing notifications.
type NotificationsFilter struct {
	SettingID  string
	Status     []DeliveryStatus
	EventTypes []EventType
	FromTime   *time.Time
	ToTime     *time.Time
	Limit      int
	Offset     int
}

// Storage defines the notification storage interface.
type Storage interface {
	// Settings
	SaveSetting(ctx context.Context, setting *NotificationSetting) error
	GetSetting(ctx context.Context, id string) (*NotificationSetting, error)
	DeleteSetting(ctx context.Context, id string) error
	ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error)

	// Notifications
	SaveNotification(ctx context.Context, notification *Notification) error
	GetNotification(ctx context.Context, id string) (*Notification, error)
	UpdateNotificationStatus(ctx context.Context, id string, status DeliveryStatus, err string) error
	ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error)
	GetPendingNotifications(ctx context.Context, limit int) ([]*Notification, error)

	// UpdateNotification stores a notification's delivery state (status,
	// error, retry count, next retry, sent time) and moves its status and
	// pending index entries.
	UpdateNotification(ctx context.Context, notification *Notification) error

	// History
	SaveDeliveryHistory(ctx context.Context, history *DeliveryHistory) error
	GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error)

	// Stats
	GetStats(ctx context.Context, settingID string) (*NotificationStats, error)
	IncrementStats(ctx context.Context, settingID string, success bool, deliveryMs int64) error

	// Cleanup
	CleanupOldHistory(ctx context.Context, before time.Time) (int64, error)
}

// Handler defines the interface for notification delivery handlers.
type Handler interface {
	Type() NotificationType
	Deliver(ctx context.Context, notification *Notification, setting *NotificationSetting) (*DeliveryResult, error)
	Validate(setting *NotificationSetting) error
}

// NotificationService implements the Service interface.
type NotificationService struct {
	config   *Config
	storage  Storage
	eventBus *events.EventBus
	logger   *zap.Logger

	handlers map[NotificationType]Handler
	queue    chan *Notification

	mu       sync.RWMutex
	settings map[string]*NotificationSetting
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc
	// workCtx is the context of deliveries: Stop cancels ctx first, so no
	// new work starts, lets deliveries under way finish and record their
	// status, and cancels workCtx only when its own deadline passes (a
	// delivery cut off before its status is recorded is sent again later).
	workCtx    context.Context
	workCancel context.CancelFunc
	wg         sync.WaitGroup

	eventSub *events.Subscription

	// stream is the change stream the service consumes as group (R3-5);
	// nil consumes the event bus instead.
	stream      stream.Bus
	streamGroup string

	// inflight are the notifications in the queue or being delivered, so
	// the retry processor does not queue them again.
	inflightMu sync.Mutex
	inflight   map[string]bool

	// nonTokenTransfer are contracts whose Transfer logs are not token
	// transfers (a native coin exposed as a token contract).
	nonTokenTransfer map[common.Address]bool
}

// SetNonTokenTransferContracts names contracts whose Transfer logs are not
// token transfers, such as a native coin exposed as a token contract.
func (s *NotificationService) SetNonTokenTransferContracts(addrs ...common.Address) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nonTokenTransfer = make(map[common.Address]bool, len(addrs))
	for _, a := range addrs {
		s.nonTokenTransfer[a] = true
	}
}

// transferSig is the topic of ERC-20 and ERC-721 Transfer events.
var transferSig = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

// eventKinds returns the notification event types an event belongs to,
// most specific last: a transaction without a recipient is also a
// contract creation, and an ERC-20 (3 topics) or ERC-721 (4 topics)
// Transfer log, except from a SetNonTokenTransferContracts contract, also a
// token transfer.
func (s *NotificationService) eventKinds(event events.Event) []EventType {
	kinds := []EventType{s.convertEventType(event.Type())}
	switch e := event.(type) {
	case *events.TransactionEvent:
		if e.To == nil {
			kinds = append(kinds, EventTypeContractCreation)
		}
	case *events.LogEvent:
		if l := e.Log; l != nil && (len(l.Topics) == 3 || len(l.Topics) == 4) && l.Topics[0] == transferSig {
			s.mu.RLock()
			excluded := s.nonTokenTransfer[l.Address]
			s.mu.RUnlock()
			if !excluded {
				kinds = append(kinds, EventTypeTokenTransfer)
			}
		}
	}
	return kinds
}

// NewService creates a new notification service.
func NewService(
	config *Config,
	storage Storage,
	eventBus *events.EventBus,
	logger *zap.Logger,
) *NotificationService {
	if config == nil {
		config = DefaultConfig()
	}

	return &NotificationService{
		config:   config,
		storage:  storage,
		eventBus: eventBus,
		logger:   logger.Named("notifications"),
		handlers: make(map[NotificationType]Handler),
		queue:    make(chan *Notification, config.Queue.BufferSize),
		settings: make(map[string]*NotificationSetting),
		inflight: make(map[string]bool),
	}
}

// DefaultStreamGroup is the consumer group of the notification service.
const DefaultStreamGroup = "notifications"

// SetStream makes the service consume the change stream as group (empty:
// DefaultStreamGroup) instead of subscribing to the event bus (refactoring
// plan R3-5). Every event of an indexed block then leads to its
// notifications: they are stored, under an id derived from the setting and
// the event's sequence, before the service moves past the event, so a full
// queue or a restart loses none and a redelivered event creates none
// twice. A group that is new starts at the events committed from now on.
// Call it before Start.
func (s *NotificationService) SetStream(bus stream.Bus, group string) error {
	if group == "" {
		group = DefaultStreamGroup
	}
	if _, err := bus.Join(context.Background(), group, stream.StartLatest); err != nil {
		return fmt.Errorf("join change stream: %w", err)
	}
	s.stream, s.streamGroup = bus, group
	return nil
}

// RegisterHandler registers a notification handler.
func (s *NotificationService) RegisterHandler(handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[handler.Type()] = handler
	s.logger.Info("registered notification handler", zap.String("type", string(handler.Type())))
}

// Start starts the notification service.
func (s *NotificationService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("notification service already running")
	}
	s.running = true
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.workCtx, s.workCancel = context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Unlock()

	s.logger.Info("starting notification service")

	// Validate configuration
	if err := s.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Load existing settings
	if err := s.loadSettings(s.ctx); err != nil {
		s.logger.Warn("failed to load notification settings", zap.Error(err))
	}

	// Start workers
	for i := 0; i < s.config.Queue.Workers; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}

	// Consume the change stream, or subscribe to the event bus
	if s.stream != nil {
		s.wg.Add(1)
		go s.consumeStream()
	} else if s.eventBus != nil {
		if err := s.subscribeToEvents(); err != nil {
			s.logger.Error("failed to subscribe to events", zap.Error(err))
		}
	}

	// Start retry processor
	s.wg.Add(1)
	go s.retryProcessor()

	// Start cleanup processor
	s.wg.Add(1)
	go s.cleanupProcessor()

	s.logger.Info("notification service started",
		zap.Int("workers", s.config.Queue.Workers),
		zap.Int("queue_size", s.config.Queue.BufferSize),
	)

	return nil
}

// Stop gracefully stops the notification service.
func (s *NotificationService) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	s.logger.Info("stopping notification service")

	// Cancel context
	s.cancel()

	// Unsubscribe from events
	if s.eventSub != nil && s.eventBus != nil {
		s.eventBus.Unsubscribe(s.eventSub.ID)
	}

	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("notification service stopped gracefully")
	case <-ctx.Done():
		s.logger.Warn("notification service stop timed out; cancelling deliveries under way")
	}
	s.workCancel()

	return nil
}

// loadSettings loads all notification settings from storage.
//
// It replaces the settings held in memory, so settings another process
// created, changed or deleted (an API process, node.role api) take effect
// here; the retry processor calls it every settingsReload.
func (s *NotificationService) loadSettings(ctx context.Context) error {
	settings, err := s.storage.ListSettings(ctx, &SettingsFilter{Limit: 10000})
	if err != nil {
		return err
	}
	loaded := make(map[string]*NotificationSetting, len(settings))
	for _, setting := range settings {
		loaded[setting.ID] = setting
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(loaded) != len(s.settings) {
		s.logger.Info("loaded notification settings", zap.Int("count", len(loaded)))
	}
	s.settings = loaded
	return nil
}

// settingsReload is how often a running service reloads its settings from
// storage, where an API process may have changed them.
var settingsReload = 5 * time.Second

// subscribeToEvents subscribes to blockchain events.
func (s *NotificationService) subscribeToEvents() error {
	eventTypes := []events.EventType{
		events.EventTypeBlock,
		events.EventTypeTransaction,
		events.EventTypeLog,
		events.EventTypeReorg,
	}

	subID := events.SubscriptionID("notifications-" + uuid.New().String())
	sub := s.eventBus.Subscribe(subID, eventTypes, nil, s.config.Queue.BufferSize)
	if sub == nil {
		return fmt.Errorf("failed to subscribe to events")
	}

	s.eventSub = sub

	// Process events
	s.wg.Add(1)
	go s.processEvents()

	s.logger.Info("subscribed to blockchain events")
	return nil
}

// processEvents processes blockchain events and creates notifications.
func (s *NotificationService) processEvents() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case event, ok := <-s.eventSub.Channel:
			if !ok {
				return
			}
			s.handleEvent(event)
		}
	}
}

// handleEvent processes an event of the event bus.
func (s *NotificationService) handleEvent(event events.Event) {
	if err := s.notify(s.ctx, event); err != nil {
		s.logger.Error("failed to create notifications", zap.String("type", string(event.Type())), zap.Error(err))
	}
}

// streamTypes are the event types the service notifies of.
var streamTypes = map[events.EventType]bool{
	events.EventTypeBlock:       true,
	events.EventTypeTransaction: true,
	events.EventTypeLog:         true,
	events.EventTypeReorg:       true,
}

// consumeStream consumes the change stream until the service stops,
// starting again after errors.
func (s *NotificationService) consumeStream() {
	defer s.wg.Done()
	for {
		err := s.stream.Consume(s.ctx, s.streamGroup, s.handleBatch)
		if s.ctx.Err() != nil {
			return
		}
		s.logger.Error("change stream consumption stopped; retrying", zap.Error(err))
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// handleBatch creates the notifications of a batch of change stream
// entries. An error leaves the batch to be delivered again; notifications
// it created already are recognized by their ids.
func (s *NotificationService) handleBatch(ctx context.Context, batch []port.OutboxEntry) error {
	for _, entry := range batch {
		if !streamTypes[events.EventType(entry.Type)] {
			continue
		}
		event, err := events.UnmarshalEvent(events.EventType(entry.Type), entry.Data)
		if err != nil {
			s.logger.Error("undecodable change stream entry skipped", zap.Uint64("seq", entry.Seq), zap.Error(err))
			continue
		}
		if sq, ok := event.(events.Sequenced); ok {
			sq.SetSequence(entry.Seq)
		}
		if err := s.notify(ctx, event); err != nil {
			return fmt.Errorf("notifications of sequence %d: %w", entry.Seq, err)
		}
	}
	return nil
}

// notify creates, stores and queues the notifications of an event. A
// notification of a sequenced event that exists already (the event was
// delivered again) is not created again.
func (s *NotificationService) notify(ctx context.Context, event events.Event) error {
	s.mu.RLock()
	settings := make([]*NotificationSetting, 0, len(s.settings))
	for _, setting := range s.settings {
		if setting.Enabled {
			settings = append(settings, setting)
		}
	}
	s.mu.RUnlock()

	kinds := s.eventKinds(event)
	for _, setting := range settings {
		kind, ok := s.shouldNotify(setting, event, kinds)
		if !ok {
			continue
		}
		notification := s.createNotification(setting, event, kind)
		if notification == nil {
			continue
		}
		if events.SequenceOf(event) != 0 {
			existing, err := s.storage.GetNotification(ctx, notification.ID)
			if err != nil {
				return err
			}
			if existing != nil {
				continue
			}
		}
		if err := s.storage.SaveNotification(ctx, notification); err != nil {
			return err
		}
		s.enqueueNotification(notification)
	}
	return nil
}

// shouldNotify checks if a setting should be notified for an event of the
// given kinds (eventKinds) and returns the kind it is notified as: the most
// specific one the setting subscribes to.
func (s *NotificationService) shouldNotify(setting *NotificationSetting, event events.Event, kinds []EventType) (EventType, bool) {
	var eventType EventType
	found := false
	for i := len(kinds) - 1; i >= 0 && !found; i-- {
		for _, et := range setting.EventTypes {
			if et == kinds[i] {
				eventType, found = kinds[i], true
				break
			}
		}
	}
	if !found {
		return "", false
	}

	// Apply filters if present
	if setting.Filter != nil {
		return eventType, s.matchesFilter(setting.Filter, event)
	}

	return eventType, true
}

// convertEventType converts internal event type to notification event type.
func (s *NotificationService) convertEventType(eventType events.EventType) EventType {
	switch eventType {
	case events.EventTypeBlock:
		return EventTypeBlock
	case events.EventTypeTransaction:
		return EventTypeTransaction
	case events.EventTypeLog:
		return EventTypeLog
	case events.EventTypeReorg:
		return EventTypeReorg
	default:
		return EventType(eventType)
	}
}

// matchesFilter checks if an event matches the notification filter.
func (s *NotificationService) matchesFilter(filter *NotifyFilter, event events.Event) bool {
	// If no filter specified, accept all events
	if filter == nil {
		return true
	}

	switch e := event.(type) {
	case *events.TransactionEvent:
		return s.matchesTransactionFilter(filter, e)
	case *events.LogEvent:
		return s.matchesLogFilter(filter, e)
	case *events.BlockEvent:
		// Block events pass through if no specific address filter
		// or if miner address matches
		if len(filter.Addresses) > 0 && e.Block != nil {
			miner := e.Block.Coinbase()
			return containsAddress(filter.Addresses, miner)
		}
		return true
	default:
		return true
	}
}

// matchesTransactionFilter checks if a transaction event matches the filter.
func (s *NotificationService) matchesTransactionFilter(filter *NotifyFilter, e *events.TransactionEvent) bool {
	// Check address filter
	if len(filter.Addresses) > 0 {
		matchedAddress := false
		// Check if From or To address matches any filter address
		if containsAddress(filter.Addresses, e.From) {
			matchedAddress = true
		}
		if e.To != nil && containsAddress(filter.Addresses, *e.To) {
			matchedAddress = true
		}
		if !matchedAddress {
			return false
		}
	}

	// Check minimum value filter
	if filter.MinValue != nil && *filter.MinValue != "" {
		minValue, ok := new(big.Int).SetString(*filter.MinValue, 10)
		if ok {
			txValue, valueOk := new(big.Int).SetString(e.Value, 10)
			if valueOk && txValue.Cmp(minValue) < 0 {
				return false
			}
		}
	}

	return true
}

// matchesLogFilter checks if a log event matches the filter.
func (s *NotificationService) matchesLogFilter(filter *NotifyFilter, e *events.LogEvent) bool {
	if e.Log == nil {
		return false
	}

	// Check address filter (log contract address)
	if len(filter.Addresses) > 0 {
		if !containsAddress(filter.Addresses, e.Log.Address) {
			return false
		}
	}

	// Check topics filter
	if len(filter.Topics) > 0 {
		if !matchesTopics(filter.Topics, e.Log.Topics) {
			return false
		}
	}

	return matchesEvent(filter, e.Log.Topics) && matchesParticipants(filter, e.Log.Topics)
}

// containsAddress checks if an address is in the list.
func containsAddress(addresses []common.Address, addr common.Address) bool {
	for _, a := range addresses {
		if a == addr {
			return true
		}
	}
	return false
}

// matchesTopics checks if log topics match the filter topics.
// Filter topics use the same format as eth_getLogs:
// - Empty array at position matches any topic
// - Non-empty array at position must match one of the values
func matchesTopics(filterTopics [][]common.Hash, logTopics []common.Hash) bool {
	for i, topicOptions := range filterTopics {
		// If we've exhausted log topics, no match
		if i >= len(logTopics) {
			// Unless filter topic is empty (wildcard)
			if len(topicOptions) > 0 {
				return false
			}
			continue
		}

		// Empty array means any topic matches
		if len(topicOptions) == 0 {
			continue
		}

		// Check if log topic matches any of the filter options
		matched := false
		for _, opt := range topicOptions {
			if opt == logTopics[i] {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// createNotification creates a notification from an event.
func (s *NotificationService) createNotification(setting *NotificationSetting, event events.Event, kind EventType) *Notification {
	payload, err := s.createPayload(event)
	if err != nil {
		s.logger.Error("failed to create notification payload", zap.Error(err))
		return nil
	}
	payload.EventType = kind
	if le, ok := event.(*events.LogEvent); ok {
		payload.Decoded = decodeFilterEvent(setting.Filter, le.Log)
	}

	id := uuid.New().String()
	if seq := events.SequenceOf(event); seq != 0 {
		// One notification per setting and change stream event.
		id = uuid.NewSHA1(notificationIDSpace, []byte(fmt.Sprintf("%s/%d", setting.ID, seq))).String()
	}
	return &Notification{
		ID:         id,
		SettingID:  setting.ID,
		Type:       setting.Type,
		EventType:  kind,
		Payload:    payload,
		Status:     DeliveryStatusPending,
		RetryCount: 0,
		CreatedAt:  time.Now(),
	}
}

// notificationIDSpace is the UUID namespace of notification ids derived
// from a setting and a change stream sequence.
var notificationIDSpace = uuid.MustParse("6f3c1d52-9a43-4b8e-9d2a-0b7e5c1f4a10")

// createPayload creates an event payload.
func (s *NotificationService) createPayload(event events.Event) (*EventPayload, error) {
	var blockNumber uint64
	var blockHash common.Hash
	var chainID uint64 = 1 // Default chain ID

	// Extract block info based on event type
	switch e := event.(type) {
	case *events.BlockEvent:
		blockNumber = e.Number
		blockHash = e.Hash
	case *events.TransactionEvent:
		blockNumber = e.BlockNumber
		blockHash = e.BlockHash
	case *events.LogEvent:
		if e.Log != nil {
			blockNumber = e.Log.BlockNumber
			blockHash = e.Log.BlockHash
		}
	case *events.ReorgEvent:
		// The newest block both branches share.
		blockNumber = e.ForkNumber
		blockHash = e.ForkHash
	}

	// Serialize the event data
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	return &EventPayload{
		ChainID:     chainID,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		Timestamp:   event.Timestamp(),
		EventType:   s.convertEventType(event.Type()),
		Data:        data,
	}, nil
}

// enqueueNotification queues a stored notification for delivery unless it
// is queued or being delivered already. When the queue is full it stays
// stored as pending and the retry processor queues it later.
func (s *NotificationService) enqueueNotification(notification *Notification) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if s.inflight[notification.ID] {
		return
	}
	select {
	case s.queue <- notification:
		s.inflight[notification.ID] = true
	default:
		s.logger.Debug("notification queue full, delivery deferred",
			zap.String("notification_id", notification.ID))
	}
}

// delivered ends a notification's queued or delivering state.
func (s *NotificationService) delivered(id string) {
	s.inflightMu.Lock()
	delete(s.inflight, id)
	s.inflightMu.Unlock()
}

// worker processes notifications from the queue.
func (s *NotificationService) worker(id int) {
	defer s.wg.Done()
	s.logger.Debug("notification worker started", zap.Int("worker_id", id))

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Debug("notification worker stopping", zap.Int("worker_id", id))
			return
		case notification, ok := <-s.queue:
			if !ok {
				return
			}
			s.processNotification(notification)
			s.delivered(notification.ID)
		}
	}
}

// processNotification delivers a single notification.
func (s *NotificationService) processNotification(notification *Notification) {
	// The queued copy may be stale (the retry processor read it before an
	// earlier delivery finished): deliver what the storage holds, and
	// nothing that was sent or given up already.
	if current, err := s.storage.GetNotification(s.workCtx, notification.ID); err == nil && current != nil {
		if current.Status == DeliveryStatusSent || current.Status == DeliveryStatusFailed {
			return
		}
		notification = current
	}
	s.mu.RLock()
	setting := s.settings[notification.SettingID]
	s.mu.RUnlock()

	if setting == nil {
		s.logger.Warn("notification setting not found",
			zap.String("notification_id", notification.ID),
			zap.String("setting_id", notification.SettingID))
		s.giveUp(notification, "notification setting not found")
		return
	}

	handler, ok := s.handlers[notification.Type]
	if !ok {
		s.logger.Error("no handler for notification type",
			zap.String("type", string(notification.Type)))
		s.giveUp(notification, "no handler for notification type "+string(notification.Type))
		return
	}

	// Update status to sending
	notification.Status = DeliveryStatusRetrying
	if err := s.storage.UpdateNotificationStatus(s.workCtx, notification.ID, DeliveryStatusRetrying, ""); err != nil {
		s.logger.Warn("failed to update notification status to retrying",
			zap.String("notification_id", notification.ID),
			zap.Error(err))
	}

	// Deliver notification
	start := time.Now()
	result, err := handler.Deliver(s.workCtx, notification, setting)
	duration := time.Since(start).Milliseconds()

	// Record history
	history := &DeliveryHistory{
		NotificationID: notification.ID,
		SettingID:      setting.ID,
		Attempt:        notification.RetryCount + 1,
		Result:         result,
		Timestamp:      time.Now(),
	}
	if err := s.storage.SaveDeliveryHistory(s.workCtx, history); err != nil {
		s.logger.Warn("failed to save delivery history",
			zap.String("notification_id", notification.ID),
			zap.Error(err))
	}

	if err != nil || (result != nil && !result.Success) {
		s.handleDeliveryFailure(notification, result, err)
	} else {
		s.handleDeliverySuccess(notification, result, duration)
	}
}

// giveUp marks a notification that cannot be delivered as failed, so it
// is not retried.
func (s *NotificationService) giveUp(notification *Notification, reason string) {
	if err := s.storage.UpdateNotificationStatus(s.workCtx, notification.ID, DeliveryStatusFailed, reason); err != nil {
		s.logger.Warn("failed to update notification status to failed",
			zap.String("notification_id", notification.ID),
			zap.Error(err))
	}
}

// handleDeliverySuccess handles successful delivery.
func (s *NotificationService) handleDeliverySuccess(notification *Notification, result *DeliveryResult, durationMs int64) {
	now := time.Now()
	notification.Status = DeliveryStatusSent
	notification.SentAt = &now

	if err := s.storage.UpdateNotificationStatus(s.workCtx, notification.ID, DeliveryStatusSent, ""); err != nil {
		s.logger.Warn("failed to update notification status to sent",
			zap.String("notification_id", notification.ID),
			zap.Error(err))
	}
	if err := s.storage.IncrementStats(s.workCtx, notification.SettingID, true, durationMs); err != nil {
		s.logger.Warn("failed to increment notification stats",
			zap.String("setting_id", notification.SettingID),
			zap.Error(err))
	}

	s.logger.Debug("notification delivered",
		zap.String("notification_id", notification.ID),
		zap.Int64("duration_ms", durationMs))
}

// handleDeliveryFailure handles failed delivery.
func (s *NotificationService) handleDeliveryFailure(notification *Notification, result *DeliveryResult, err error) {
	notification.RetryCount++

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else if result != nil {
		errMsg = result.Error
	}
	notification.Error = errMsg

	if notification.RetryCount >= s.config.Retry.MaxAttempts {
		notification.Status = DeliveryStatusFailed
		notification.NextRetry = nil
		if stErr := s.storage.UpdateNotification(s.workCtx, notification); stErr != nil {
			s.logger.Warn("failed to update notification status to failed",
				zap.String("notification_id", notification.ID),
				zap.Error(stErr))
		}
		if stErr := s.storage.IncrementStats(s.workCtx, notification.SettingID, false, 0); stErr != nil {
			s.logger.Warn("failed to increment failure stats",
				zap.String("setting_id", notification.SettingID),
				zap.Error(stErr))
		}
		s.logger.Warn("notification delivery failed permanently",
			zap.String("notification_id", notification.ID),
			zap.Int("attempts", notification.RetryCount),
			zap.String("error", errMsg))
	} else {
		// Schedule retry
		delay := s.calculateRetryDelay(notification.RetryCount)
		nextRetry := time.Now().Add(delay)
		notification.NextRetry = &nextRetry
		notification.Status = DeliveryStatusRetrying
		// The retry count and time are stored with the status, so the
		// retry processor waits for them, also after a restart.
		if stErr := s.storage.UpdateNotification(s.workCtx, notification); stErr != nil {
			s.logger.Warn("failed to update notification status to retrying",
				zap.String("notification_id", notification.ID),
				zap.Error(stErr))
		}
		s.logger.Debug("scheduling notification retry",
			zap.String("notification_id", notification.ID),
			zap.Int("attempt", notification.RetryCount),
			zap.Duration("delay", delay))
	}
}

// calculateRetryDelay calculates the delay for a retry attempt.
func (s *NotificationService) calculateRetryDelay(attempt int) time.Duration {
	delay := s.config.Retry.InitialDelay
	for i := 1; i < attempt; i++ {
		delay = time.Duration(float64(delay) * s.config.Retry.Multiplier)
	}
	if delay > s.config.Retry.MaxDelay {
		delay = s.config.Retry.MaxDelay
	}
	return delay
}

// retryProcessor periodically checks for notifications to retry.
func (s *NotificationService) retryProcessor() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.Queue.FlushInterval)
	defer ticker.Stop()
	reload := time.NewTicker(settingsReload)
	defer reload.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processRetries()
		case <-reload.C:
			if err := s.loadSettings(s.ctx); err != nil && s.ctx.Err() == nil {
				s.logger.Warn("failed to reload notification settings", zap.Error(err))
			}
		}
	}
}

// processRetries processes pending retries.
func (s *NotificationService) processRetries() {
	notifications, err := s.storage.GetPendingNotifications(s.ctx, s.config.Queue.BatchSize)
	if err != nil {
		s.logger.Error("failed to get pending notifications", zap.Error(err))
		return
	}

	// The storage returns the notifications that are due: never attempted
	// (left out of a full queue, or stored before a restart) or due for a
	// retry.
	for _, notification := range notifications {
		s.enqueueNotification(notification)
	}
}

// cleanupProcessor periodically cleans up old history.
func (s *NotificationService) cleanupProcessor() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			before := time.Now().Add(-s.config.Storage.HistoryRetention)
			count, err := s.storage.CleanupOldHistory(s.ctx, before)
			if err != nil {
				s.logger.Error("failed to cleanup old history", zap.Error(err))
			} else if count > 0 {
				s.logger.Info("cleaned up old notification history", zap.Int64("count", count))
			}
		}
	}
}

// CreateSetting creates a new notification setting.
func (s *NotificationService) CreateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error) {
	if setting.ID == "" {
		setting.ID = uuid.New().String()
	}
	setting.CreatedAt = time.Now()
	setting.UpdatedAt = time.Now()

	// Validate with handler
	handler, ok := s.handlers[setting.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported notification type: %s", setting.Type)
	}
	if err := validateFilter(setting.Filter); err != nil {
		return nil, fmt.Errorf("invalid setting: %w", err)
	}
	if err := handler.Validate(setting); err != nil {
		return nil, fmt.Errorf("invalid setting: %w", err)
	}

	if err := s.storage.SaveSetting(ctx, setting); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.settings[setting.ID] = setting
	s.mu.Unlock()

	s.logger.Info("created notification setting",
		zap.String("id", setting.ID),
		zap.String("type", string(setting.Type)))

	return setting, nil
}

// UpdateSetting updates an existing notification setting.
func (s *NotificationService) UpdateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error) {
	existing, err := s.storage.GetSetting(ctx, setting.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("setting not found: %s", setting.ID)
	}

	setting.CreatedAt = existing.CreatedAt
	setting.UpdatedAt = time.Now()
	if setting.Owner == "" {
		setting.Owner = existing.Owner // an update does not change the owner
	}

	// Validate with handler
	handler, ok := s.handlers[setting.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported notification type: %s", setting.Type)
	}
	if err := validateFilter(setting.Filter); err != nil {
		return nil, fmt.Errorf("invalid setting: %w", err)
	}
	if err := handler.Validate(setting); err != nil {
		return nil, fmt.Errorf("invalid setting: %w", err)
	}

	if err := s.storage.SaveSetting(ctx, setting); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.settings[setting.ID] = setting
	s.mu.Unlock()

	s.logger.Info("updated notification setting",
		zap.String("id", setting.ID))

	return setting, nil
}

// DeleteSetting deletes a notification setting.
func (s *NotificationService) DeleteSetting(ctx context.Context, id string) error {
	if err := s.storage.DeleteSetting(ctx, id); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.settings, id)
	s.mu.Unlock()

	s.logger.Info("deleted notification setting", zap.String("id", id))
	return nil
}

// GetSetting returns a notification setting by ID.
func (s *NotificationService) GetSetting(ctx context.Context, id string) (*NotificationSetting, error) {
	return s.storage.GetSetting(ctx, id)
}

// ListSettings returns notification settings matching the filter.
func (s *NotificationService) ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error) {
	return s.storage.ListSettings(ctx, filter)
}

// GetNotification returns a notification by ID.
func (s *NotificationService) GetNotification(ctx context.Context, id string) (*Notification, error) {
	return s.storage.GetNotification(ctx, id)
}

// ListNotifications returns notifications matching the filter.
func (s *NotificationService) ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error) {
	return s.storage.ListNotifications(ctx, filter)
}

// RetryNotification retries a failed notification.
func (s *NotificationService) RetryNotification(ctx context.Context, id string) error {
	notification, err := s.storage.GetNotification(ctx, id)
	if err != nil {
		return err
	}
	if notification == nil {
		return fmt.Errorf("notification not found: %s", id)
	}

	notification.RetryCount = 0
	notification.Status = DeliveryStatusPending
	notification.NextRetry = nil
	notification.Error = ""
	// Stored as pending, the retry processor of the running service (in
	// another process for an API process) delivers it, also when the
	// queue is full.
	if err := s.storage.UpdateNotification(ctx, notification); err != nil {
		return err
	}
	s.mu.RLock()
	running := s.running
	s.mu.RUnlock()
	if running {
		s.enqueueNotification(notification)
	}
	return nil
}

// CancelNotification cancels a pending notification.
func (s *NotificationService) CancelNotification(ctx context.Context, id string) error {
	return s.storage.UpdateNotificationStatus(ctx, id, DeliveryStatusCancelled, "cancelled by user")
}

// GetStats returns statistics for a notification setting.
func (s *NotificationService) GetStats(ctx context.Context, settingID string) (*NotificationStats, error) {
	return s.storage.GetStats(ctx, settingID)
}

// GetDeliveryHistory returns delivery history for a notification.
func (s *NotificationService) GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error) {
	return s.storage.GetDeliveryHistory(ctx, notificationID)
}

// TestSetting tests a notification setting with a sample event.
func (s *NotificationService) TestSetting(ctx context.Context, id string) (*DeliveryResult, error) {
	setting, err := s.storage.GetSetting(ctx, id)
	if err != nil {
		return nil, err
	}
	if setting == nil {
		return nil, fmt.Errorf("setting not found: %s", id)
	}

	handler, ok := s.handlers[setting.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported notification type: %s", setting.Type)
	}

	// Create a test notification
	testNotification := &Notification{
		ID:        uuid.New().String(),
		SettingID: setting.ID,
		Type:      setting.Type,
		EventType: EventTypeBlock,
		Payload: &EventPayload{
			ChainID:     1,
			BlockNumber: 12345678,
			BlockHash:   [32]byte{0x01, 0x02, 0x03},
			Timestamp:   time.Now(),
			EventType:   EventTypeBlock,
			Data:        json.RawMessage(`{"test": true, "message": "This is a test notification"}`),
		},
		Status:    DeliveryStatusPending,
		CreatedAt: time.Now(),
	}

	return handler.Deliver(ctx, testNotification, setting)
}
