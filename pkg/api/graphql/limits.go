package graphql

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"

	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
	graphqlhandler "github.com/graphql-go/handler"
)

// Limits bound what one GraphQL request may ask for; a request over a
// limit is refused before any resolver runs (api.graphql.max_depth,
// api.graphql.max_complexity). 0 turns a limit off.
type Limits struct {
	// MaxDepth is the deepest field nesting allowed.
	MaxDepth int
	// MaxComplexity is the highest complexity allowed (see QueryCost).
	MaxComplexity int
}

func (l Limits) enabled() bool { return l.MaxDepth > 0 || l.MaxComplexity > 0 }

// maxListMultiplier caps the page size QueryCost reads from an argument.
const maxListMultiplier = 1000

// QueryCost returns the depth and the complexity of the costliest operation
// of doc.
//
// Depth counts nested fields (fragments add none). Complexity counts every
// field a request may resolve: a field costs 1 plus its selections, and the
// selections of a field asking for a page (a pagination: {limit} argument,
// or a limit or first argument) count once per row of the page. A request
// for 100 blocks with 3 fields each costs 1 + 100 * 3 = 301. variables
// gives the values of page sizes passed as variables.
func QueryCost(doc *ast.Document, variables map[string]interface{}) (depth, complexity int) {
	c := costWalker{fragments: map[string]*ast.FragmentDefinition{}, active: map[string]bool{}, variables: variables}
	for _, def := range doc.Definitions {
		if f, ok := def.(*ast.FragmentDefinition); ok && f.Name != nil {
			c.fragments[f.Name.Value] = f
		}
	}
	for _, def := range doc.Definitions {
		if op, ok := def.(*ast.OperationDefinition); ok {
			d, n := c.selections(op.SelectionSet)
			depth, complexity = max(depth, d), max(complexity, n)
		}
	}
	return depth, complexity
}

type costWalker struct {
	fragments map[string]*ast.FragmentDefinition
	active    map[string]bool // fragments being expanded (cycles are left to validation)
	variables map[string]interface{}
}

func (c *costWalker) selections(set *ast.SelectionSet) (depth, complexity int) {
	if set == nil {
		return 0, 0
	}
	for _, sel := range set.Selections {
		var d, n int
		switch s := sel.(type) {
		case *ast.Field:
			d, n = c.selections(s.SelectionSet)
			d++
			n = saturatingAdd(1, saturatingMul(c.pageSize(s), n))
		case *ast.InlineFragment:
			d, n = c.selections(s.SelectionSet)
		case *ast.FragmentSpread:
			name := s.Name.Value
			f, ok := c.fragments[name]
			if !ok || c.active[name] {
				continue
			}
			c.active[name] = true
			d, n = c.selections(f.SelectionSet)
			delete(c.active, name)
		}
		depth = max(depth, d)
		complexity = saturatingAdd(complexity, n)
	}
	return depth, complexity
}

// pageSize returns how many rows field asks for: the limit of its
// pagination argument, or its limit or first argument; 1 without one.
func (c *costWalker) pageSize(field *ast.Field) int {
	for _, arg := range field.Arguments {
		switch arg.Name.Value {
		case "limit", "first":
			if n, ok := c.intValue(arg.Value); ok {
				return clampPage(n)
			}
		case "pagination":
			if n, ok := c.objectField(arg.Value, "limit"); ok {
				return clampPage(n)
			}
		}
	}
	return 1
}

func clampPage(n int) int { return min(max(n, 1), maxListMultiplier) }

// objectField returns the integer field name of an object value given
// inline or as a variable.
func (c *costWalker) objectField(v ast.Value, name string) (int, bool) {
	switch o := v.(type) {
	case *ast.ObjectValue:
		for _, f := range o.Fields {
			if f.Name.Value == name {
				return c.intValue(f.Value)
			}
		}
	case *ast.Variable:
		if m, ok := c.variables[o.Name.Value].(map[string]interface{}); ok {
			return number(m[name])
		}
	}
	return 0, false
}

func (c *costWalker) intValue(v ast.Value) (int, bool) {
	switch x := v.(type) {
	case *ast.IntValue:
		var n int
		if _, err := fmt.Sscan(x.Value, &n); err == nil {
			return n, true
		}
		return maxListMultiplier, true // too large to read
	case *ast.Variable:
		return number(c.variables[x.Name.Value])
	}
	return 0, false
}

// number reads a JSON number of the request's variables.
func number(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n > maxListMultiplier {
			return maxListMultiplier, true
		}
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i > maxListMultiplier {
			return maxListMultiplier, true
		}
		return int(i), true
	}
	return 0, false
}

func saturatingAdd(a, b int) int {
	if a > math.MaxInt32-b {
		return math.MaxInt32
	}
	return a + b
}

func saturatingMul(a, b int) int {
	if a != 0 && b > math.MaxInt32/a {
		return math.MaxInt32
	}
	return a * b
}

// limitError is the GraphQL error of a request over a limit.
type limitError struct {
	Message    string                 `json:"message"`
	Extensions map[string]interface{} `json:"extensions"`
}

// checkLimits answers a request over the limits with an error and reports
// whether it did. It reads the request the way the GraphQL handler does and
// leaves the body for it. Requests that do not parse are left to the
// handler, which reports the syntax error.
func (h *Handler) checkLimits(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			http.Error(w, "failed to read the request", http.StatusBadRequest)
			return true
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		peek := r.Clone(r.Context())
		peek.Body = io.NopCloser(bytes.NewReader(body))
		r = peek
	}
	opts := graphqlhandler.NewRequestOptions(r)
	if opts.Query == "" {
		return false
	}
	doc, err := parser.Parse(parser.ParseParams{Source: source.NewSource(&source.Source{Body: []byte(opts.Query)})})
	if err != nil {
		return false
	}
	depth, complexity := QueryCost(doc, opts.Variables)
	var e *limitError
	switch {
	case h.limits.MaxDepth > 0 && depth > h.limits.MaxDepth:
		e = &limitError{
			Message:    fmt.Sprintf("query depth %d exceeds the limit %d", depth, h.limits.MaxDepth),
			Extensions: map[string]interface{}{"code": "QUERY_TOO_DEEP", "depth": depth, "limit": h.limits.MaxDepth},
		}
	case h.limits.MaxComplexity > 0 && complexity > h.limits.MaxComplexity:
		e = &limitError{
			Message:    fmt.Sprintf("query complexity %d exceeds the limit %d", complexity, h.limits.MaxComplexity),
			Extensions: map[string]interface{}{"code": "QUERY_TOO_COMPLEX", "complexity": complexity, "limit": h.limits.MaxComplexity},
		}
	default:
		return false
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"errors": []*limitError{e}})
	return true
}
