package graphql

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/stream"
	"github.com/graphql-go/graphql"
	graphqlhandler "github.com/graphql-go/handler"
	"go.uber.org/zap"
)

// Handler handles GraphQL requests
type Handler struct {
	schema  *Schema
	handler *graphqlhandler.Handler
	logger  *zap.Logger
	limits  Limits
}

// HandlerOptions contains optional configuration for the GraphQL handler
type HandlerOptions struct {
	RPCProxy                    *rpcproxy.Proxy
	NotificationService         notifications.Service
	ContractRegistrationService *events.ContractRegistrationService
	// Stream is the outbox of the change stream, for the streamSequence
	// query; nil when events are not kept.
	Stream stream.Outbox
	// Limits bound the depth and complexity of requests.
	Limits Limits
}

// NewHandler creates a new GraphQL handler
func NewHandler(store port.QueryStore, logger *zap.Logger) (*Handler, error) {
	return NewHandlerWithOptions(store, logger, nil)
}

// NewHandlerWithOptions creates a new GraphQL handler with optional configurations
func NewHandlerWithOptions(store port.QueryStore, logger *zap.Logger, opts *HandlerOptions) (*Handler, error) {
	builder := NewSchemaBuilder(store, logger)
	if opts != nil {
		builder.schema.stream = opts.Stream
	}
	builder = builder.
		WithCoreQueries().
		WithHistoricalQueries().
		WithAnalyticsQueries().
		WithAddressIndexingQueries().
		WithSetCodeQueries().
		WithModuleQueries().
		WithUserOpQueries().
		WithTokenMetadataQueries().
		WithTokenHolderQueries().
		WithReorgQueries().
		WithSubscriptions().
		WithMutations()

	// Add RPC Proxy queries if proxy is provided
	if opts != nil && opts.RPCProxy != nil {
		builder = builder.WithRPCProxy(opts.RPCProxy).WithRPCProxyQueries()
		logger.Info("GraphQL RPC Proxy queries enabled")
	}

	// Add notification queries if notification service is provided
	if opts != nil && opts.NotificationService != nil {
		builder = builder.WithNotificationService(opts.NotificationService).WithNotificationQueries()
		logger.Info("GraphQL Notification queries enabled")
	}

	// Add dynamic contract queries if contract registration service is provided
	if opts != nil && opts.ContractRegistrationService != nil {
		builder = builder.WithContractRegistrationService(opts.ContractRegistrationService).WithDynamicContractQueries()
		logger.Info("GraphQL Dynamic Contract queries enabled")
	}

	schema, err := builder.Build()
	if err != nil {
		return nil, err
	}

	h := graphqlhandler.New(&graphqlhandler.Config{
		Schema:     &schema.schema,
		Pretty:     true,
		GraphiQL:   false,
		Playground: true,
	})

	handler := &Handler{
		schema:  schema,
		handler: h,
		logger:  logger,
	}
	if opts != nil {
		handler.limits = opts.Limits
	}
	return handler, nil
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.limits.enabled() && h.checkLimits(w, r) {
		return
	}
	h.handler.ServeHTTP(w, r)
}

// PlaygroundHandler returns a handler for GraphQL playground
func (h *Handler) PlaygroundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		playgroundHTML := `
<!DOCTYPE html>
<html>
<head>
  <title>GraphQL Playground</title>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/css/index.css" />
  <link rel="shortcut icon" href="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/favicon.png" />
  <script src="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/js/middleware.js"></script>
</head>
<body>
  <div id="root"></div>
  <script>
    window.addEventListener('load', function (event) {
      GraphQLPlayground.init(document.getElementById('root'), {
        endpoint: '/graphql',
        settings: {
          'request.credentials': 'same-origin',
        },
      })
    })
  </script>
</body>
</html>
`
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(playgroundHTML))
	}
}

// ExecuteQuery executes a GraphQL query (for testing)
func (h *Handler) ExecuteQuery(query string, variables map[string]interface{}) *graphql.Result {
	params := graphql.Params{
		Schema:         h.schema.schema,
		RequestString:  query,
		VariableValues: variables,
		Context:        context.Background(),
	}
	return graphql.Do(params)
}

// ExecuteQueryJSON executes a GraphQL query and returns JSON (for testing)
func (h *Handler) ExecuteQueryJSON(query string, variables map[string]interface{}) ([]byte, error) {
	result := h.ExecuteQuery(query, variables)
	return json.Marshal(result)
}
