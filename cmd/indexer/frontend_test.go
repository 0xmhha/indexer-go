package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// indexer-frontend reads this service's GraphQL API (refactoring plan
// R4-4). testdata/frontend/documents.json holds the GraphQL documents of
// its sources; TestFrontendDocuments requires every query to validate
// against the served schema and every subscription to name one the
// WebSocket server serves, except those listed in known-invalid.txt, which
// were already broken when the list was made. Refresh the documents from a
// checkout of indexer-frontend (INDEXER_FRONTEND_DIR, default
// ../indexer-frontend) with
//
//	go test ./cmd/indexer -run TestFrontendDocuments -update

const (
	frontendDocuments = "testdata/frontend/documents.json"
	frontendKnown     = "testdata/frontend/known-invalid.txt"
)

// frontendDocument is one gql`...` template of indexer-frontend.
type frontendDocument struct {
	File     string `json:"file"`
	Document string `json:"document"`
}

// gqlTemplate matches gql`...`; interpolations (${...}) are removed: the
// sources interpolate no fragments.
var (
	gqlTemplate   = regexp.MustCompile("(?s)gql`(.*?)`")
	interpolation = regexp.MustCompile(`\$\{[^}]*\}`)
)

// readFrontendDocuments extracts the GraphQL documents of the indexer-frontend
// sources under dir, leaving out generated code, tests and dependencies.
func readFrontendDocuments(t *testing.T, dir string) []frontendDocument {
	t.Helper()
	var docs []frontendDocument
	for _, sub := range []string{"app", "components", "lib", "stores"} {
		root := filepath.Join(dir, sub)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if name == "node_modules" || name == "__tests__" {
					return filepath.SkipDir
				}
				return nil
			}
			source := strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx")
			if !source || strings.Contains(name, ".test.") || strings.Contains(name, ".spec.") || name == "generated.ts" {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, path)
			for _, m := range gqlTemplate.FindAllSubmatch(src, -1) {
				docs = append(docs, frontendDocument{File: filepath.ToSlash(rel), Document: interpolation.ReplaceAllString(string(m[1]), "")})
			}
			return nil
		})
		require.NoError(t, err)
	}
	require.NotEmpty(t, docs, "no GraphQL documents under %s", dir)
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].File < docs[j].File })
	return docs
}

// documentKey names a document in known-invalid.txt: its file and its
// operation names.
func documentKey(d frontendDocument) (key string, subscription bool) {
	doc, err := parser.Parse(parser.ParseParams{Source: d.Document})
	if err != nil {
		return d.File + " (unparsable)", false
	}
	var names []string
	for _, def := range doc.Definitions {
		if op, ok := def.(*ast.OperationDefinition); ok {
			if op.Operation == ast.OperationTypeSubscription {
				subscription = true
			}
			if op.Name != nil {
				names = append(names, op.Name.Value)
			}
		}
	}
	return d.File + " " + strings.Join(names, ","), subscription
}

func TestFrontendDocuments(t *testing.T) {
	if *updateGolden {
		dir := os.Getenv("INDEXER_FRONTEND_DIR")
		if dir == "" {
			dir = filepath.Join("..", "..", "..", "indexer-frontend")
		}
		out, err := json.MarshalIndent(readFrontendDocuments(t, dir), "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(frontendDocuments), 0o755))
		require.NoError(t, os.WriteFile(frontendDocuments, append(out, '\n'), 0o644))
	}

	raw, err := os.ReadFile(frontendDocuments)
	require.NoError(t, err)
	var docs []frontendDocument
	require.NoError(t, json.Unmarshal(raw, &docs))

	known := map[string]bool{}
	if raw, err := os.ReadFile(frontendKnown); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				known[line] = true
			}
		}
	}

	// The schema every optional query group adds to: what a node serving
	// the RPC proxy and dynamic contracts answers.
	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	h, err := graphql.NewHandlerWithOptions(st, zap.NewNop(), &graphql.HandlerOptions{
		RPCProxy:                    &rpcproxy.Proxy{},
		ContractRegistrationService: &events.ContractRegistrationService{},
	})
	require.NoError(t, err)

	var invalid []string
	for _, d := range docs {
		key, subscription := documentKey(d)
		var why string
		if subscription {
			// The WebSocket server reads subscriptions by name; it does not
			// validate them.
			if graphql.SubscriptionKind(d.Document) == "" {
				why = "no subscription the server serves"
			}
		} else if msgs := h.Validate(d.Document); msgs != nil {
			why = strings.Join(msgs, "; ")
		}
		switch {
		case why != "" && !known[key]:
			t.Errorf("indexer-frontend %s no longer works: %s", key, why)
		case why == "" && known[key]:
			t.Errorf("indexer-frontend %s works now: remove it from %s", key, frontendKnown)
		}
		if why != "" {
			invalid = append(invalid, key)
		}
	}
	t.Logf("%d documents, %d known invalid", len(docs), len(invalid))
}
