// Package porttest checks storage implementations against the contracts of
// the storage ports in pkg/core/port (refactoring plan R1-4). A storage
// adapter's test calls Run with a constructor for empty stores:
//
//	func TestPortContracts(t *testing.T) {
//		porttest.Run(t, func(t *testing.T) any { return newEmptyStore(t) })
//	}
//
// Every port the store implements is checked. The ports every indexer
// storage needs (Reader, Writer, LogReader, LogWriter, BlockTransactor) are
// required. The contracts are written against the ports only, so a new
// backend (R4-2) passes the same tests as Pebble.
package porttest

import (
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// NewStore returns an empty store; it is called once per contract so that
// contracts do not see each other's data.
type NewStore func(t *testing.T) any

// contract checks one port. It is skipped when the store does not
// implement the port and required is false.
type contract struct {
	name     string
	required bool
	has      func(s any) bool
	run      func(t *testing.T, newStore NewStore)
}

func implements[P any](s any) bool {
	_, ok := s.(P)
	return ok
}

var contracts = []contract{
	{"BlockReaderWriter", true, implements[blockStore], testBlocks},
	{"Reader", true, implements[readerStore], testReader},
	{"Writer", true, implements[readerStore], testWriter},
	{"BlockTransactor", true, implements[txStore], testBlockTransactor},
	{"Logs", true, implements[logStore], testLogs},
	{"ABI", false, implements[abiStore], testABI},
	{"Search", false, implements[searchStore], testSearch},
	{"ContractVerification", false, implements[verificationStore], testContractVerification},
	{"TokenMetadata", false, implements[tokenMetadataStore], testTokenMetadata},
	{"Historical", false, implements[historicalStore], testHistorical},
	{"BalanceRecordChecker", false, implements[balanceRecordStore], testBalanceRecordChecker},
	{"AddressIndex", false, implements[addressIndexStore], testAddressIndex},
	{"TokenHolderIndex", false, implements[tokenHolderStore], testTokenHolderIndex},
	{"SetCodeIndex", false, implements[setCodeStore], testSetCodeIndex},
	{"UserOpIndex", false, implements[userOpStore], testUserOpIndex},
	{"ModuleIndex", false, implements[moduleStore], testModuleIndex},
	{"Orphans", false, implements[orphanStore], testOrphans},
	{"FeatureState", false, implements[port.FeatureStateStore], testFeatureState},
	{"KV", false, implements[port.KV], testKV},
}

// Run checks a store against every port contract it implements.
func Run(t *testing.T, newStore NewStore) {
	probe := newStore(t)
	for _, c := range contracts {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if !c.has(probe) {
				if c.required {
					t.Fatalf("%T does not implement the required %s ports", probe, c.name)
				}
				t.Skipf("%T does not implement the %s ports", probe, c.name)
			}
			c.run(t, newStore)
		})
	}
}

// The port sets each contract needs from a store.
type (
	blockStore interface {
		port.BlockReader
		port.BlockWriter
	}
	readerStore interface {
		port.Reader
		port.Writer
	}
	txStore interface {
		readerStore
		port.BlockTransactor
	}
	logStore interface {
		readerStore
		port.LogReader
		port.LogWriter
	}
	abiStore interface {
		port.ABIReader
		port.ABIWriter
	}
	searchStore interface {
		readerStore
		port.SearchReader
	}
	verificationStore interface {
		port.ContractVerificationReader
		port.ContractVerificationWriter
	}
	tokenMetadataStore interface {
		port.TokenMetadataReader
		port.TokenMetadataWriter
	}
	historicalStore interface {
		readerStore
		port.HistoricalReader
		port.HistoricalWriter
	}
	balanceRecordStore interface {
		port.HistoricalWriter
		port.BalanceRecordChecker
	}
	addressIndexStore interface {
		readerStore
		port.AddressIndexReader
		port.AddressIndexWriter
	}
	tokenHolderStore interface {
		port.TokenHolderIndexReader
		port.TokenHolderIndexWriter
	}
	setCodeStore interface {
		port.SetCodeIndexReader
		port.SetCodeIndexWriter
	}
	userOpStore interface {
		port.UserOpIndexReader
		port.UserOpIndexWriter
	}
	moduleStore interface {
		port.ModuleIndexReader
		port.ModuleIndexWriter
	}
	orphanStore interface {
		txStore
		port.OrphanReader
		port.Rollbacker
	}
)

// open returns a new empty store as S.
func open[S any](t *testing.T, newStore NewStore) S {
	t.Helper()
	s, ok := newStore(t).(S)
	if !ok {
		var zero S
		t.Fatalf("store does not implement %T", &zero)
	}
	return s
}
