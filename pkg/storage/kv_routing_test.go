package storage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// directDBAllowed lists the functions that may use s.db for reads and writes
// directly. Everything else must go through s.kv(ctx) / s.newBatch(ctx) so a
// block transaction bound to ctx captures it. These functions run outside the
// ingest path (startup, reindex tooling, the exported Batch API) or implement
// the routing itself.
var directDBAllowed = map[string]bool{
	"kv":                   true, // routing
	"newBatch":             true, // routing
	"loadTransactionCount": true, // startup
	"DeleteByPrefix":       true, // reindex tooling
	"CountByPrefix":        true, // reindex tooling
	"NewBatch":             true, // exported Batch API, commits to the DB
}

var routedDBMethods = map[string]bool{
	"Get": true, "Set": true, "Delete": true, "NewIter": true,
	"NewBatch": true, "NewIndexedBatch": true, "Apply": true, "DeleteRange": true,
}

// TestStorageRoutesDBAccessThroughKV fails when a PebbleStorage method reads or
// writes s.db directly, which would bypass a bound block transaction and
// break per-block atomicity.
func TestStorageRoutesDBAccessThroughKV(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "pebble_backend.go" {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || directDBAllowed[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || !routedDBMethods[sel.Sel.Name] {
					return true
				}
				inner, ok := sel.X.(*ast.SelectorExpr)
				if !ok || inner.Sel.Name != "db" {
					return true
				}
				if id, ok := inner.X.(*ast.Ident); ok && id.Name == "s" {
					t.Errorf("%s: %s uses s.db.%s directly; use s.kv(ctx) or s.newBatch(ctx)",
						fset.Position(sel.Pos()), fn.Name.Name, sel.Sel.Name)
				}
				return true
			})
		}
	}
}
