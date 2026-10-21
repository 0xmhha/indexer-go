package chains_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// chainNeutral lists directories (relative to pkg/) that must work for
// every chain. They may use pkg/chains and pkg/core but not a specific
// chain profile; chain behaviour reaches them through registries in
// pkg/chains and in the packages themselves (API, storage keyspace, event
// codecs). The chain profiles are the chain-specific code outside this
// list; pkg/app links the generic EVM profile only.
var chainNeutral = []string{
	"chainfeed", "window", "capture", "progress", "projection", "indexingstatus", "feed",
	"fetch", "api", "source", "feature", "features", "storage", "events", "eventbus",
	"core", "types", "token", "abi", "client", "rpcproxy", "notifications",
	"multichain", "userop", "module", "verifier", "compiler", "price", "rpcpool",
	"../internal", "../cmd/cli", "../cmd/flags", "startup",
}

// TestChainNeutralPackagesDoNotImportProfiles keeps chain-specific code in
// the profiles (chain profile design, section 3).
func TestChainNeutralPackagesDoNotImportProfiles(t *testing.T) {
	const profiles = "github.com/0xmhha/indexer-go/pkg/chains/"
	fset := token.NewFileSet()
	for _, dir := range chainNeutral {
		root := filepath.Join("..", dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				if strings.HasPrefix(p, profiles) {
					t.Errorf("%s imports chain profile %s", path, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestOnlyProductsImportChains: chain code lives under x/<chain> and is
// linked in by the binaries built for it (x/<chain>/cmd), never by the
// engine, its packages or the indexer command; chains do not import each
// other. Tests may link chains in.
func TestOnlyProductsImportChains(t *testing.T) {
	const module = "github.com/0xmhha/indexer-go/"
	fset := token.NewFileSet()
	repo := filepath.Join("..", "..")
	for _, dir := range []string{"pkg", "internal", "cmd", "x"} {
		err := filepath.WalkDir(filepath.Join(repo, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(repo, path)
			rel = filepath.ToSlash(rel)
			if strings.Contains(rel, "/testdata/") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			own := ""
			if parts := strings.Split(rel, "/"); parts[0] == "x" {
				own = "x/" + parts[1]
			}
			for _, imp := range f.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				target, ok := strings.CutPrefix(p, module)
				if !ok || !strings.HasPrefix(target, "x/") {
					continue
				}
				chain := "x/" + strings.Split(target, "/")[1]
				if own == "" {
					t.Errorf("%s imports %s: only x/<chain> code and tests link chains in", rel, p)
				} else if chain != own {
					t.Errorf("%s imports %s: chains do not import each other", rel, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
