package receipts_test

import (
	"testing"

	"github.com/0xmhha/indexer-go/examples/receipts"
	"github.com/0xmhha/indexer-go/pkg/sdk/sdktest"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestTotalsAreDeterministic follows docs/SDK.md "결정성 규약": the totals
// are the same however the chain is indexed, and the handler's source uses
// nothing the rules forbid.
func TestTotalsAreDeterministic(t *testing.T) {
	sdktest.RequireNoForbiddenUses(t, ".")

	sc := testchain.BuildReceipts()
	records := map[string]any{
		"sources": []map[string]any{{"name": "settlement", "address": sc.Settlement.Hex(), "events": []string{testchain.PaymentSettledSignature}}},
		"tables":  []map[string]any{{"name": "receipts", "source": "settlement", "event": "PaymentSettled", "keys": [][]string{{"merchant", "orderId"}}}},
	}
	sdktest.RequireDeterministic(t, sdktest.Check{
		Chain:    sc.Chain,
		Features: []string{receipts.TotalsName},
		Requires: []string{"records"},
		Settings: map[string]any{"records": records},
		Prefixes: []string{"/x/receipts/"},
		Declared: true,
		Finality: "finalized",
	})
}
