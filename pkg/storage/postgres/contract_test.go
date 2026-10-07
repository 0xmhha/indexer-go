package postgres

import (
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/port/porttest"
)

// TestPortContracts checks the adapter against the storage port contracts
// (refactoring plan R4-2): every port it implements must pass the same
// tests as the Pebble store.
func TestPortContracts(t *testing.T) {
	testDSN(t)
	porttest.Run(t, func(t *testing.T) any { return newTestStore(t) })
}
