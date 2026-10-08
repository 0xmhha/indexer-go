package storage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// assemblyNames are the pkg/storage names that bind code to the Pebble
// implementation: the union of all ports and the implementation itself.
var assemblyNames = map[string]bool{
	"Storage":          true,
	"PebbleStorage":    true,
	"NewPebbleStorage": true,
}

// assemblyDirs may use assemblyNames outside tests: the binary wires the
// storage into every component, and the multi-chain manager creates one
// storage per chain.
var assemblyDirs = []string{"cmd/", "pkg/app/", "pkg/multichain/"}

// TestStorageAssemblyOnly keeps consumers on the ports (refactoring plan
// R1-1): outside pkg/storage, only assembly code names storage.Storage or
// the Pebble implementation; everything else takes pkg/core/port
// interfaces.
func TestStorageAssemblyOnly(t *testing.T) {
	const self = "github.com/0xmhha/indexer-go/pkg/storage"
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "pkg/storage" || rel == "tools" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		for _, dir := range assemblyDirs {
			if strings.HasPrefix(rel, dir) {
				return nil
			}
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		name := ""
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == self {
				name = "storage"
				if imp.Name != nil {
					name = imp.Name.Name
				}
			}
		}
		if name == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == name && assemblyNames[sel.Sel.Name] {
				t.Errorf("%s: uses storage.%s; take the pkg/core/port interfaces it needs", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestNoPortAliases keeps the ports in one place: pkg/storage declares no
// aliases of pkg/core/port names (they were removed when R1-1 moved every
// consumer to the port package).
func TestNoPortAliases(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	isPort := func(e ast.Expr) bool {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == "port"
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Assign.IsValid() && isPort(s.Type) {
						t.Errorf("%s: type %s aliases a port; use the port directly", fset.Position(s.Pos()), s.Name.Name)
					}
				case *ast.ValueSpec:
					for i, v := range s.Values {
						if isPort(v) {
							t.Errorf("%s: %s aliases a port; use the port directly", fset.Position(s.Pos()), s.Names[i].Name)
						}
					}
				}
			}
		}
	}
}
