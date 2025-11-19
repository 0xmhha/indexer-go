package graphql

import (
	"encoding/json"
	"net/http"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/graphql-go/graphql"
	graphqlhandler "github.com/graphql-go/handler"
	"go.uber.org/zap"
)

// Handler handles GraphQL requests
type Handler struct {
	schema  *Schema
	handler *graphqlhandler.Handler
	logger  *zap.Logger
}

// NewHandler creates a new GraphQL handler
func NewHandler(store storage.Storage, logger *zap.Logger) (*Handler, error) {
	schema, err := NewSchema(store, logger)
	if err != nil {
		return nil, err
	}

	h := graphqlhandler.New(&graphqlhandler.Config{
		Schema:     &schema.schema,
		Pretty:     true,
		GraphiQL:   false,
		Playground: true,
	})

	return &Handler{
		schema:  schema,
		handler: h,
		logger:  logger,
	}, nil
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		w.Write([]byte(playgroundHTML))
	}
}

// ExecuteQuery executes a GraphQL query (for testing)
func (h *Handler) ExecuteQuery(query string, variables map[string]interface{}) *graphql.Result {
	params := graphql.Params{
		Schema:         h.schema.schema,
		RequestString:  query,
		VariableValues: variables,
	}
	return graphql.Do(params)
}

// ExecuteQueryJSON executes a GraphQL query and returns JSON (for testing)
func (h *Handler) ExecuteQueryJSON(query string, variables map[string]interface{}) ([]byte, error) {
	result := h.ExecuteQuery(query, variables)
	return json.Marshal(result)
}
