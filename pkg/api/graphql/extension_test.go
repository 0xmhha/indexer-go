package graphql

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

func init() {
	RegisterExtension("zz.test", func(e *Extension) {
		e.AddQuery("extensionEcho", &graphql.Field{
			Type: graphql.String,
			Args: graphql.FieldConfigArgument{"text": &graphql.ArgumentConfig{Type: graphql.String}},
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return p.Args["text"], nil
			},
		})
		e.AddSubscription("extensionReorgs", &graphql.Field{Type: graphql.String})
	})
	RegisterSubscription("extensionReorgs", SubscriptionSpec{
		EventType: events.EventTypeReorg,
		Payload: func(ev events.Event) (interface{}, bool) {
			r, ok := ev.(*events.ReorgEvent)
			if !ok {
				return nil, false
			}
			return fmt.Sprintf("reorg to %d", r.ForkNumber), true
		},
	})
}

func TestExtensionAddsQueryAndSubscription(t *testing.T) {
	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	h, err := NewHandler(st, zap.NewNop())
	require.NoError(t, err)
	res := h.ExecuteQuery(`{ extensionEcho(text: "hi") }`, nil)
	require.Empty(t, res.Errors)
	require.Equal(t, "hi", res.Data.(map[string]interface{})["extensionEcho"])

	bus := events.NewEventBus(100, 100)
	go bus.Run()
	defer bus.Stop()
	srv := httptest.NewServer(NewSubscriptionServer(bus, zap.NewNop(), true))
	defer srv.Close()
	conn := subscribeWS(t, srv.URL, "x", `subscription { extensionReorgs }`, nil)
	defer func() { _ = conn.Close() }()
	time.Sleep(100 * time.Millisecond)
	require.True(t, bus.Publish(&events.ReorgEvent{ForkNumber: 7, CreatedAt: time.Now()}))
	require.Equal(t, "reorg to 7", readNext(t, conn)["extensionReorgs"])
}
