package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestSystemContractEventsArePublished indexes the StableNet scenario and
// requires its governance events on the event bus, where the
// systemContractEvents subscription reads them.
func TestSystemContractEventsArePublished(t *testing.T) {
	sc := testchain.BuildStableNet()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	app := startApp(t, srv, filepath.Join(t.TempDir(), "db"))
	t.Cleanup(app.Shutdown)

	sub := app.eventBus.Subscribe("system-contracts", []events.EventType{systemcontracts.EventTypeSystemContract}, nil, 1000)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))

	seen := map[systemcontracts.SystemContractEventType]bool{}
	require.Eventually(t, func() bool {
		for {
			select {
			case ev := <-sub.Channel:
				if e, ok := ev.(*systemcontracts.SystemContractEvent); ok {
					seen[e.EventName] = true
				}
			default:
				return seen[systemcontracts.SystemContractEventProposalCreated] &&
					seen[systemcontracts.SystemContractEventDepositMintProposed]
			}
		}
	}, 5*time.Second, 10*time.Millisecond, "published: %v", seen)
}
