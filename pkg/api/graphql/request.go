package graphql

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
	graphqlhandler "github.com/graphql-go/handler"
)

// SchemaBuilder installs schema fields, not graphql-go executor extensions.
// Use the library's parser, full validator and cancellable executor directly,
// sharing one request-local AST. REST's fixed-document Execute path is unchanged.
func (h *Handler) serveRequest(w http.ResponseWriter, r *http.Request) {
	finish := requestPhase(r.Context(), "request_prepare")
	opts, err := requestOptions(r, h.limits.enabled())
	finishRequestPhase(finish)
	if err != nil {
		finish = requestPhase(r.Context(), "response")
		http.Error(w, "failed to read the request", http.StatusBadRequest)
		finishRequestPhase(finish)
		return
	}
	finish = requestPhase(r.Context(), "parse")
	doc, err := parser.Parse(parser.ParseParams{Source: source.NewSource(&source.Source{
		Body: []byte(opts.Query), Name: "GraphQL request",
	})})
	finishRequestPhase(finish)
	if err != nil {
		h.writeResult(w, r, &graphql.Result{Errors: gqlerrors.FormatErrors(err)})
		return
	}
	if h.limits.enabled() {
		finish = requestPhase(r.Context(), "limits")
		limit := h.checkDocumentLimits(doc, opts.Variables)
		finishRequestPhase(finish)
		if limit != nil {
			finish = requestPhase(r.Context(), "response")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"errors": []*limitError{limit}})
			finishRequestPhase(finish)
			return
		}
	}
	finish = requestPhase(r.Context(), "validation")
	// Match Do's request-local schema value, rather than passing the shared
	// handler's schema pointer into the validator's internal helper caches.
	schema := h.schema.schema
	validated := validateExecutionDocument(&schema, doc)
	finishRequestPhase(finish)
	if !validated.IsValid {
		h.writeResult(w, r, &graphql.Result{Errors: validated.Errors})
		return
	}
	finish = requestPhase(r.Context(), "execution")
	result := h.executeParsed(r.Context(), schema, doc, opts.OperationName, opts.Variables)
	finishRequestPhase(finish)
	h.writeResult(w, r, result)
}

func (h *Handler) writeResult(w http.ResponseWriter, r *http.Request, result *graphql.Result) {
	finish := requestPhase(r.Context(), "response")
	defer finishRequestPhase(finish)
	accept := r.Header.Get("Accept")
	_, raw := r.URL.Query()["raw"]
	if !raw && !strings.Contains(accept, "application/json") && strings.Contains(accept, "text/html") {
		// Preserve HTML negotiation without running Do (and therefore parsing
		// and executing the user's document) a second time.
		renderPlayground(w, r, r.URL.Path, "ws://"+r.Host+"/subscriptions")
		return
	}
	w.Header().Add("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	value, _ := json.Marshal(result)
	_, _ = w.Write(value)
}

// Keep the pinned adapter's GET/JSON/form/application-graphql/variables-string
// decoding, called exactly once. The old enabled-limit path returned HTTP400
// for body read failures, even when URL options took precedence. Track/drain
// unused bytes to preserve that rule without copying/replaying the body.
func requestOptions(r *http.Request, checkReadError bool) (*graphqlhandler.RequestOptions, error) {
	if !checkReadError || r.Body == nil {
		return graphqlhandler.NewRequestOptions(r), nil
	}
	body := &requestBody{ReadCloser: r.Body}
	r.Body = body
	defer func() { _ = body.Close() }()
	opts := graphqlhandler.NewRequestOptions(r)
	if !body.eof && body.err == nil {
		_, _ = io.Copy(io.Discard, body)
	}
	return opts, body.err
}

type requestBody struct {
	io.ReadCloser
	err error
	eof bool
}

func (r *requestBody) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err == io.EOF {
		r.eof = true
	} else if err != nil {
		r.err = err
	}
	return n, err
}
