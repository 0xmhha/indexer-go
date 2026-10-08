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
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
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
	// ExtensionsOnly builds the schema from the extensions alone, without
	// the explorer's modules: the indexer keeps only declared data
	// (indexer.mode: declared, refactoring plan R6-1).
	ExtensionsOnly bool
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
	if opts == nil || !opts.ExtensionsOnly {
		builder = builder.WithModules(opts)
	}
	if opts != nil && opts.RPCProxy != nil {
		logger.Info("GraphQL RPC Proxy queries enabled")
	}
	if opts != nil && opts.NotificationService != nil {
		logger.Info("GraphQL Notification queries enabled")
	}
	if opts != nil && opts.ContractRegistrationService != nil {
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

// Validate returns why document does not validate against the served
// schema, nil when it does.
func (h *Handler) Validate(document string) []string {
	doc, err := parser.Parse(parser.ParseParams{Source: source.NewSource(&source.Source{Body: []byte(document)})})
	if err != nil {
		return []string{err.Error()}
	}
	res := graphql.ValidateDocument(&h.schema.schema, doc, nil)
	if res.IsValid {
		return nil
	}
	msgs := make([]string, len(res.Errors))
	for i, e := range res.Errors {
		msgs[i] = e.Message
	}
	return msgs
}

// Execute runs a GraphQL document against the served schema, without the
// request limits (for fixed documents such as the REST API's).
func (h *Handler) Execute(ctx context.Context, document string, variables map[string]interface{}) *graphql.Result {
	return graphql.Do(graphql.Params{
		Schema:         h.schema.schema,
		RequestString:  document,
		VariableValues: variables,
		Context:        ctx,
	})
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
