package events

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Filter defines subscription filter conditions
type Filter struct {
	// Address filters - any address that matches will pass
	// Empty means no filtering on addresses
	Addresses []common.Address

	// FromAddresses filters transaction sender addresses
	// Empty means no filtering on from addresses
	FromAddresses []common.Address

	// ToAddresses filters transaction recipient addresses
	// Empty means no filtering on to addresses
	ToAddresses []common.Address

	// MinValue filters transactions by minimum value (inclusive)
	// Nil means no minimum value filtering
	MinValue *big.Int

	// MaxValue filters transactions by maximum value (inclusive)
	// Nil means no maximum value filtering
	MaxValue *big.Int

	// FromBlock filters events from this block number (inclusive)
	// 0 means no minimum block filtering
	FromBlock uint64

	// ToBlock filters events up to this block number (inclusive)
	// 0 means no maximum block filtering
	ToBlock uint64
}

// NewFilter creates a new empty filter
func NewFilter() *Filter {
	return &Filter{
		Addresses:     make([]common.Address, 0),
		FromAddresses: make([]common.Address, 0),
		ToAddresses:   make([]common.Address, 0),
	}
}

// Validate checks if the filter configuration is valid
func (f *Filter) Validate() error {
	// Check value range
	if f.MinValue != nil && f.MaxValue != nil {
		if f.MinValue.Cmp(f.MaxValue) > 0 {
			return fmt.Errorf("minValue (%s) cannot be greater than maxValue (%s)",
				f.MinValue.String(), f.MaxValue.String())
		}
	}

	// Check block range
	if f.FromBlock > 0 && f.ToBlock > 0 {
		if f.FromBlock > f.ToBlock {
			return fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)",
				f.FromBlock, f.ToBlock)
		}
	}

	// Check for negative values
	if f.MinValue != nil && f.MinValue.Sign() < 0 {
		return fmt.Errorf("minValue cannot be negative")
	}
	if f.MaxValue != nil && f.MaxValue.Sign() < 0 {
		return fmt.Errorf("maxValue cannot be negative")
	}

	return nil
}

// MatchBlock checks if a block event matches this filter
func (f *Filter) MatchBlock(event *BlockEvent) bool {
	// Check block number range
	if f.FromBlock > 0 && event.Number < f.FromBlock {
		return false
	}
	if f.ToBlock > 0 && event.Number > f.ToBlock {
		return false
	}

	return true
}

// MatchTransaction checks if a transaction event matches this filter
func (f *Filter) MatchTransaction(event *TransactionEvent) bool {
	// Check block number range
	if f.FromBlock > 0 && event.BlockNumber < f.FromBlock {
		return false
	}
	if f.ToBlock > 0 && event.BlockNumber > f.ToBlock {
		return false
	}

	// Check address filters (any address: from, to, or contract)
	if len(f.Addresses) > 0 {
		matched := false
		for _, addr := range f.Addresses {
			if event.From == addr {
				matched = true
				break
			}
			if event.To != nil && *event.To == addr {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check from address filter
	if len(f.FromAddresses) > 0 {
		matched := false
		for _, addr := range f.FromAddresses {
			if event.From == addr {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check to address filter
	if len(f.ToAddresses) > 0 {
		if event.To == nil {
			// Contract creation - no 'to' address
			return false
		}
		matched := false
		for _, addr := range f.ToAddresses {
			if *event.To == addr {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check value range
	if f.MinValue != nil || f.MaxValue != nil {
		// Convert event value string to big.Int
		eventValue := new(big.Int)
		eventValue.SetString(event.Value, 10)

		if f.MinValue != nil && eventValue.Cmp(f.MinValue) < 0 {
			return false
		}
		if f.MaxValue != nil && eventValue.Cmp(f.MaxValue) > 0 {
			return false
		}
	}

	return true
}

// Match checks if an event matches this filter
func (f *Filter) Match(event Event) bool {
	switch e := event.(type) {
	case *BlockEvent:
		return f.MatchBlock(e)
	case *TransactionEvent:
		return f.MatchTransaction(e)
	default:
		return false
	}
}

// IsEmpty returns true if the filter has no conditions set
func (f *Filter) IsEmpty() bool {
	return len(f.Addresses) == 0 &&
		len(f.FromAddresses) == 0 &&
		len(f.ToAddresses) == 0 &&
		f.MinValue == nil &&
		f.MaxValue == nil &&
		f.FromBlock == 0 &&
		f.ToBlock == 0
}

// Clone creates a deep copy of the filter
func (f *Filter) Clone() *Filter {
	clone := &Filter{
		Addresses:     make([]common.Address, len(f.Addresses)),
		FromAddresses: make([]common.Address, len(f.FromAddresses)),
		ToAddresses:   make([]common.Address, len(f.ToAddresses)),
		FromBlock:     f.FromBlock,
		ToBlock:       f.ToBlock,
	}

	copy(clone.Addresses, f.Addresses)
	copy(clone.FromAddresses, f.FromAddresses)
	copy(clone.ToAddresses, f.ToAddresses)

	if f.MinValue != nil {
		clone.MinValue = new(big.Int).Set(f.MinValue)
	}
	if f.MaxValue != nil {
		clone.MaxValue = new(big.Int).Set(f.MaxValue)
	}

	return clone
}
