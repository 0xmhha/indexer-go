package chains_test

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var updateIdentifierLimits = flag.Bool("update", false, "rewrite testdata/chain-identifiers.txt")

const identifierLimits = "testdata/chain-identifiers.txt"

// chainSpecific matches identifiers and string literals that name
// StableNet (go-stablenet) concepts: its consensus, transaction type, fee
// rules, system contracts and their governance.
var chainSpecific = regexp.MustCompile(`(?i)wbft|istanbul|stablenet|stableone|stable_one|feedelegat|feepayer|fee_payer|anzeon|systemcontract|system_contract|syscontract|nativecoin|govminter|govvalidator|govmaster|govcouncil|accountmanager|blacklist|epochinfo|gastip`)

// standardNames are Ethereum names that contain a chainSpecific word:
// GasTipCap is the EIP-1559 priority fee cap, not StableNet's gas tip.
var standardNames = regexp.MustCompile(`(?i)gastipcap`)

// isChainSpecific reports whether a name or string names a StableNet
// concept.
func isChainSpecific(s string) bool {
	return chainSpecific.MatchString(standardNames.ReplaceAllString(s, ""))
}

// TestChainIdentifierLimits counts chain-specific identifiers and strings
// in the production code of every package outside pkg/chains/<chain>/ and
// requires each count to equal its recorded limit: code that names a chain
// must move into the chain's package, behind a registry or interface the
// upper layers depend on (refactoring of chain-specific code, S0). When a
// count drops, lower the limit (run with -update).
func TestChainIdentifierLimits(t *testing.T) {
	root := filepath.Join("..", "..")
	counts := map[string]int{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case strings.HasPrefix(rel, ".") && rel != ".",
				rel == "tools", rel == "docs", rel == "e2e",
				rel == "pkg/testchain",
				strings.HasPrefix(rel, "pkg/chains/") && strings.Count(rel, "/") >= 2: // pkg/chains/<chain>/...
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(filepath.Dir(rel))
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if isChainSpecific(x.Name) {
					counts[pkg]++
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					if s, err := strconv.Unquote(x.Value); err == nil && isChainSpecific(s) {
						counts[pkg]++
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if *updateIdentifierLimits {
		var pkgs []string
		for p := range counts {
			pkgs = append(pkgs, p)
		}
		sort.Strings(pkgs)
		var b strings.Builder
		b.WriteString("# Chain-specific identifiers and strings per package outside pkg/chains/<chain>/ (TestChainIdentifierLimits).\n")
		for _, p := range pkgs {
			fmt.Fprintf(&b, "%s %d\n", p, counts[p])
		}
		if err := os.WriteFile(identifierLimits, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	limits := map[string]int{}
	f, err := os.Open(identifierLimits)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var p string
		var n int
		if _, err := fmt.Sscan(line, &p, &n); err != nil {
			t.Fatalf("bad line %q", line)
		}
		limits[p] = n
	}
	for p, n := range counts {
		switch limit := limits[p]; {
		case n > limit:
			t.Errorf("%s: %d chain-specific names, limit %d: move chain-specific code into pkg/chains/<chain>/", p, n, limit)
		case n < limit:
			t.Errorf("%s: %d chain-specific names, below limit %d: lower the limit (go test ./pkg/chains -run TestChainIdentifierLimits -update)", p, n, limit)
		}
	}
	for p, limit := range limits {
		if _, ok := counts[p]; !ok && limit > 0 {
			t.Errorf("%s: no chain-specific names left, below limit %d: lower the limit (-update)", p, limit)
		}
	}
}
