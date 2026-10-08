package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/api/jsonrpc"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// API snapshots, built with the production wiring of this binary (chain
// packages register their API extensions when linked in).
const (
	schemaSnapshot  = "testdata/api/graphql-schema.txt"
	methodsSnapshot = "testdata/api/jsonrpc-methods.txt"
)

const introspection = `{ __schema {
  queryType { name } mutationType { name } subscriptionType { name }
  types { name kind
    fields(includeDeprecated: true) { name args { name type { ...T } } type { ...T } }
    inputFields { name type { ...T } }
    enumValues(includeDeprecated: true) { name }
  }
} }
fragment T on __Type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } }`

func typeRef(t map[string]any) string {
	if t == nil {
		return "?"
	}
	switch t["kind"] {
	case "NON_NULL":
		return typeRef(asMap(t["ofType"])) + "!"
	case "LIST":
		return "[" + typeRef(asMap(t["ofType"])) + "]"
	}
	return fmt.Sprint(t["name"])
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// TestSchemaSnapshot pins the GraphQL schema (types, fields, arguments,
// input fields, enum values) served by NewHandler, so moving code between
// packages cannot change the API unnoticed. Regenerate with -update when a
// change is intended.
func TestSchemaSnapshot(t *testing.T) {
	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	h, err := graphql.NewHandler(st, zap.NewNop())
	require.NoError(t, err)
	res := h.ExecuteQuery(introspection, nil)
	require.Empty(t, res.Errors)
	schema := asMap(asMap(res.Data)["__schema"])

	var lines []string
	for _, root := range []string{"queryType", "mutationType", "subscriptionType"} {
		if r := asMap(schema[root]); r != nil {
			lines = append(lines, fmt.Sprintf("schema %s: %v", root, r["name"]))
		}
	}
	for _, ty := range asList(schema["types"]) {
		tm := asMap(ty)
		name := fmt.Sprint(tm["name"])
		if strings.HasPrefix(name, "__") {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s", tm["kind"], name))
		for _, f := range asList(tm["fields"]) {
			fm := asMap(f)
			var args []string
			for _, a := range asList(fm["args"]) {
				am := asMap(a)
				args = append(args, fmt.Sprintf("%v: %s", am["name"], typeRef(asMap(am["type"]))))
			}
			sort.Strings(args) // graphql-go keeps arguments in a map
			lines = append(lines, fmt.Sprintf("%s.%v(%s): %s", name, fm["name"], strings.Join(args, ", "), typeRef(asMap(fm["type"]))))
		}
		for _, f := range asList(tm["inputFields"]) {
			fm := asMap(f)
			lines = append(lines, fmt.Sprintf("%s.%v: %s", name, fm["name"], typeRef(asMap(fm["type"]))))
		}
		for _, e := range asList(tm["enumValues"]) {
			lines = append(lines, fmt.Sprintf("%s = %v", name, asMap(e)["name"]))
		}
	}
	sort.Strings(lines)
	got := strings.Join(lines, "\n") + "\n"

	if *updateGolden {
		require.NoError(t, os.WriteFile(schemaSnapshot, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(schemaSnapshot)
	require.NoError(t, err, "missing snapshot; run with -update")
	require.Equal(t, string(want), got)
}

// TestMethodsStayServed calls every method listed in testdata/api/jsonrpc-methods.txt:
// none may answer "method not found", so moving handlers between packages
// cannot drop a method unnoticed. Add new methods to the list.
func TestMethodsStayServed(t *testing.T) {
	f, err := os.Open(methodsSnapshot)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	srv := jsonrpc.NewServer(st, zap.NewNop())

	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		method := strings.TrimSpace(sc.Text())
		if method == "" || strings.HasPrefix(method, "#") {
			continue
		}
		n++
		for _, params := range []string{"{}", "[]"} {
			_, rpcErr := srv.HandleMethodDirect(context.Background(), method, json.RawMessage(params))
			if rpcErr != nil {
				require.NotEqual(t, jsonrpc.MethodNotFound, rpcErr.Code, "%s is no longer served", method)
			}
		}
	}
	require.NoError(t, sc.Err())
	require.Greater(t, n, 0)
}
