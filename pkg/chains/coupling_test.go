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

// chainNeutral lists packages (relative to pkg/) that must work for every
// chain. They may use pkg/chains and pkg/core but not a specific chain
// profile; chain behaviour reaches them through registries in pkg/chains.
var chainNeutral = []string{"fetch", "api", "source"}

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
