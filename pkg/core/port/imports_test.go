package port_test

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal lists the packages of this module the ports may use:
// the chain-neutral model and the ERC-4337 value types.
var allowedInternal = map[string]bool{
	"github.com/0xmhha/indexer-go/pkg/core/model": true,
	"github.com/0xmhha/indexer-go/pkg/userop":     true,
}

// TestPortsImportNoImplementation keeps the ports free of any storage
// implementation (refactoring plan R1-1): no Pebble, no pkg/storage, no
// chain profile; only the standard library, go-ethereum types and the
// allowed module packages.
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
			case strings.HasPrefix(p, "github.com/ethereum/go-ethereum/") && !strings.Contains(p, "/ethdb"):
			case allowedInternal[p]:
			default:
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
}
