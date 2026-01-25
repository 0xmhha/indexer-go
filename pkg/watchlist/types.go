package watchlist

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// WatchEventType represents the type of event that triggered a notification
type WatchEventType string

const (
	// Transaction events
	WatchEventTypeTxFrom WatchEventType = "tx_from" // TX where watched address is sender
	WatchEventTypeTxTo   WatchEventType = "tx_to"   // TX where watched address is recipient

	// Token events
	WatchEventTypeERC20Transfer  WatchEventType = "erc20_transfer"  // ERC20 Transfer involving watched address
	WatchEventTypeERC721Transfer WatchEventType = "erc721_transfer" // ERC721 Transfer involving watched address

	// Log events
	WatchEventTypeLog WatchEventType = "log" // Log emitted by watched address (contract)
)

// WatchFilter defines which events to track for a watched address
type WatchFilter struct {
	TxFrom   bool   `json:"txFrom"`             // Watch transactions where address is sender
	TxTo     bool   `json:"txTo"`               // Watch transactions where address is recipient
	ERC20    bool   `json:"erc20"`              // Watch ERC20 Transfer events involving address
	ERC721   bool   `json:"erc721"`             // Watch ERC721 Transfer events involving address
	Logs     bool   `json:"logs"`               // Watch all logs emitted by address (if contract)
	MinValue string `json:"minValue,omitempty"` // Minimum TX value filter (wei string)
}

// DefaultWatchFilter returns a filter that watches everything
func DefaultWatchFilter() *WatchFilter {
	return &WatchFilter{
		TxFrom: true,
		TxTo:   true,
		ERC20:  true,
		ERC721: true,
		Logs:   false, // Disabled by default (can be noisy for active contracts)
	}
}

// WatchRequest is the request to watch an address
type WatchRequest struct {
	Address common.Address `json:"address"`
	ChainID string         `json:"chainId"`
	Label   string         `json:"label,omitempty"`
	Filter  *WatchFilter   `json:"filter"`
}

// WatchedAddress represents a watched address with its configuration
type WatchedAddress struct {
	ID        string         `json:"id"`        // Unique identifier (uuid)
	Address   common.Address `json:"address"`   // Ethereum address being watched
	ChainID   string         `json:"chainId"`   // Chain this address is watched on
	Label     string         `json:"label"`     // User-provided label
	Filter    *WatchFilter   `json:"filter"`    // Filter configuration
	CreatedAt time.Time      `json:"createdAt"` // When the watch was created
	UpdatedAt time.Time      `json:"updatedAt"` // Last update time
	Stats     *WatchStats    `json:"stats"`     // Aggregated statistics
}

// WatchStats contains aggregated statistics for a watched address
type WatchStats struct {
	TotalEvents     uint64    `json:"totalEvents"`     // Total events received
	TxFromCount     uint64    `json:"txFromCount"`     // Count of tx_from events
	TxToCount       uint64    `json:"txToCount"`       // Count of tx_to events
	ERC20Count      uint64    `json:"erc20Count"`      // Count of ERC20 events
	ERC721Count     uint64    `json:"erc721Count"`     // Count of ERC721 events
	LogCount        uint64    `json:"logCount"`        // Count of log events
	LastEventAt     time.Time `json:"lastEventAt"`     // Time of last event
	LastBlockNumber uint64    `json:"lastBlockNumber"` // Block number of last event
}

// WatchEvent represents an event that matched a watched address
type WatchEvent struct {
	ID          string         `json:"id"`                    // Unique event identifier
	AddressID   string         `json:"addressId"`             // ID of the watched address
	Address     common.Address `json:"address"`               // The watched address
	ChainID     string         `json:"chainId"`               // Chain ID
	EventType   WatchEventType `json:"eventType"`             // Type of event
	BlockNumber uint64         `json:"blockNumber"`           // Block number
	BlockHash   common.Hash    `json:"blockHash"`             // Block hash
	TxHash      common.Hash    `json:"txHash"`                // Transaction hash
	TxIndex     uint           `json:"txIndex"`               // Transaction index in block
	LogIndex    *uint          `json:"logIndex,omitempty"`    // Log index (for log events)
	Data        interface{}    `json:"data"`                  // Event-specific data
	Timestamp   time.Time      `json:"timestamp"`             // Event timestamp
	Value       string         `json:"value,omitempty"`       // Transaction value (wei)
	From        common.Address `json:"from,omitempty"`        // From address (for transfers)
	To          common.Address `json:"to,omitempty"`          // To address (for transfers)
	TokenID     string         `json:"tokenId,omitempty"`     // Token ID (for ERC721)
	TokenAmount string         `json:"tokenAmount,omitempty"` // Token amount (for ERC20)
}

// Subscriber represents a subscriber to watch events
type Subscriber struct {
	ID           string    `json:"id"`           // Unique subscriber ID
	AddressID    string    `json:"addressId"`    // Address being subscribed to
	WebSocketID  string    `json:"websocketId"`  // WebSocket connection ID
	CreatedAt    time.Time `json:"createdAt"`    // When subscription was created
	LastDelivery time.Time `json:"lastDelivery"` // Last event delivery time
}

// ListFilter is used for filtering watched addresses in list queries
type ListFilter struct {
	ChainID string `json:"chainId,omitempty"` // Filter by chain ID
	Limit   int    `json:"limit,omitempty"`   // Max results (default 100)
	Offset  int    `json:"offset,omitempty"`  // Pagination offset
}

// TxEventData contains transaction-specific event data
type TxEventData struct {
	From     common.Address `json:"from"`
	To       common.Address `json:"to"`
	Value    string         `json:"value"`    // Wei value as string
	GasUsed  uint64         `json:"gasUsed"`
	GasPrice string         `json:"gasPrice"` // Wei value as string
	Nonce    uint64         `json:"nonce"`
	Input    string         `json:"input,omitempty"` // Input data (hex)
}

// ERC20EventData contains ERC20 transfer-specific event data
type ERC20EventData struct {
	Token   common.Address `json:"token"`
	From    common.Address `json:"from"`
	To      common.Address `json:"to"`
	Amount  string         `json:"amount"` // Token amount as string
	Decimal uint8          `json:"decimal,omitempty"`
	Symbol  string         `json:"symbol,omitempty"`
}

// ERC721EventData contains ERC721 transfer-specific event data
type ERC721EventData struct {
	Token   common.Address `json:"token"`
	From    common.Address `json:"from"`
	To      common.Address `json:"to"`
	TokenID string         `json:"tokenId"`
}

// LogEventData contains log-specific event data
type LogEventData struct {
	Address common.Address `json:"address"`
	Topics  []common.Hash  `json:"topics"`
	Data    string         `json:"data"` // Hex-encoded data
}
