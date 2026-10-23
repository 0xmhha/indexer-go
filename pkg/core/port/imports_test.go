package port_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal lists the packages of this module the ports may use:
// the chain-neutral model, ERC-4337 value types and the dependency-free
// execution protocol contracts shared with the isolated node module.
var allowedInternal = map[string]bool{
	"github.com/0xmhha/indexer-go/execwire":        true,
	"github.com/0xmhha/indexer-go/pkg/core/model":  true,
	"github.com/0xmhha/indexer-go/internal/userop": true,
}

// Allowing the wire contract must never admit the isolated node implementation
// or a storage dependency transitively into the ports.
func TestExecutionWireContractUsesOnlyStandardLibrary(t *testing.T) {
	err := filepath.WalkDir("../../../execwire", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if strings.Contains(strings.SplitN(p, "/", 2)[0], ".") {
				t.Errorf("execution wire contract %s imports implementation %s", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestPortsImportNoImplementation keeps the ports free of any storage
// implementation (refactoring plan R1-1): no Pebble, no pkg/storage, no
// chain profile; only the standard library, go-ethereum's basic types
// (common, not core/types: ports speak the chain-neutral model) and the
// allowed contract packages. execwire contains no storage, chain client or
// go-ethereum implementation; its separate module depends only on stdlib.
func TestPortsImportNoImplementation(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			switch {
			case !strings.Contains(strings.SplitN(p, "/", 2)[0], "."):
				// standard library
			case strings.HasPrefix(p, "github.com/ethereum/go-ethereum/") && !strings.Contains(p, "/ethdb") && p != "github.com/ethereum/go-ethereum/core/types":
			case allowedInternal[p]:
			default:
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
}
