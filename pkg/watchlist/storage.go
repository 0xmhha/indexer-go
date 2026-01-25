package watchlist

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// Storage key prefixes for watchlist data
const (
	// Data prefixes
	prefixWatchlist      = "/wl/"
	prefixWLAddr         = "/wl/addr/"         // Watched address data
	prefixWLChainAddrs   = "/wl/chain/"        // Addresses by chain
	prefixWLBloom        = "/wl/bloom/"        // Bloom filter per chain
	prefixWLSubscriber   = "/wl/sub/"          // Subscriber data
	prefixWLAddrSubs     = "/wl/addr_subs/"    // Subscribers per address
	prefixWLEvent        = "/wl/event/"        // Event data
	prefixWLEventIdx     = "/wl/eventidx/"     // Event index by address
	prefixWLAddrByAddr   = "/wl/idx/addr/"     // Index: address -> watch ID
	prefixWLStats        = "/wl/stats/"        // Address statistics
)

// Storage key functions

// WatchedAddressKey returns the key for storing a watched address
// Format: /wl/addr/{addressID}
func WatchedAddressKey(addressID string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixWLAddr, addressID))
}

// WatchedAddressKeyPrefix returns the prefix for all watched addresses
func WatchedAddressKeyPrefix() []byte {
	return []byte(prefixWLAddr)
}

// ChainAddressesKey returns the key for storing addresses watched on a chain
// Format: /wl/chain/{chainID}/{addressID}
func ChainAddressesKey(chainID, addressID string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixWLChainAddrs, chainID, addressID))
}

// ChainAddressesKeyPrefix returns the prefix for all addresses on a chain
// Format: /wl/chain/{chainID}/
func ChainAddressesKeyPrefix(chainID string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixWLChainAddrs, chainID))
}

// BloomFilterKey returns the key for storing a chain's bloom filter
// Format: /wl/bloom/{chainID}
func BloomFilterKey(chainID string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixWLBloom, chainID))
}

// SubscriberKey returns the key for storing a subscriber
// Format: /wl/sub/{subscriberID}
func SubscriberKey(subscriberID string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixWLSubscriber, subscriberID))
}

// SubscriberKeyPrefix returns the prefix for all subscribers
func SubscriberKeyPrefix() []byte {
	return []byte(prefixWLSubscriber)
}

// AddressSubscribersKey returns the key for storing subscribers of an address
// Format: /wl/addr_subs/{addressID}/{subscriberID}
func AddressSubscribersKey(addressID, subscriberID string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixWLAddrSubs, addressID, subscriberID))
}

// AddressSubscribersKeyPrefix returns the prefix for subscribers of an address
// Format: /wl/addr_subs/{addressID}/
func AddressSubscribersKeyPrefix(addressID string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixWLAddrSubs, addressID))
}

// WatchEventKey returns the key for storing a watch event
// Format: /wl/event/{chainID}/{blockNumber}/{txHash}/{logIndex}
func WatchEventKey(chainID string, blockNumber uint64, txHash common.Hash, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%s/%06d", prefixWLEvent, chainID, blockNumber, txHash.Hex(), logIndex))
}

// WatchEventKeyPrefix returns the prefix for all events
func WatchEventKeyPrefix() []byte {
	return []byte(prefixWLEvent)
}

// WatchEventChainKeyPrefix returns the prefix for events on a chain
// Format: /wl/event/{chainID}/
func WatchEventChainKeyPrefix(chainID string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixWLEvent, chainID))
}

// EventIndexKey returns the index key for events by watched address
// Format: /wl/eventidx/{addressID}/{timestamp}/{eventID}
func EventIndexKey(addressID string, timestamp int64, eventID string) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%s", prefixWLEventIdx, addressID, timestamp, eventID))
}

// EventIndexKeyPrefix returns the prefix for events by address
// Format: /wl/eventidx/{addressID}/
func EventIndexKeyPrefix(addressID string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixWLEventIdx, addressID))
}

// AddressByEthAddressKey returns the index key for looking up watch ID by ethereum address
// Format: /wl/idx/addr/{chainID}/{address}
func AddressByEthAddressKey(chainID string, address common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixWLAddrByAddr, chainID, address.Hex()))
}

// AddressByEthAddressKeyPrefix returns the prefix for address lookups by chain
// Format: /wl/idx/addr/{chainID}/
func AddressByEthAddressKeyPrefix(chainID string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixWLAddrByAddr, chainID))
}

// AddressStatsKey returns the key for storing address statistics
// Format: /wl/stats/{addressID}
func AddressStatsKey(addressID string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixWLStats, addressID))
}

// AddressStatsKeyPrefix returns the prefix for all address statistics
func AddressStatsKeyPrefix() []byte {
	return []byte(prefixWLStats)
}
