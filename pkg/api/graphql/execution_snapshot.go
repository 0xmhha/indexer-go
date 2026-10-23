package graphql

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/graphql-go/graphql/language/ast"
)

func validateExecutionDocument(schema *graphql.Schema, doc *ast.Document) graphql.ValidationResult {
	for _, definition := range doc.Definitions {
		if _, fragment := definition.(*ast.FragmentDefinition); fragment {
			result := graphql.ValidateDocument(schema, doc, []graphql.ValidationRuleFn{graphql.NoFragmentCyclesRule})
			if !result.IsValid {
				return result
			}
			break
		}
	}
	return graphql.ValidateDocument(schema, doc, nil)
}
func selectedReadOperation(doc *ast.Document, name string) bool {
	var selected *ast.OperationDefinition
	for _, definition := range doc.Definitions {
		op, ok := definition.(*ast.OperationDefinition)
		if !ok {
			continue
		}
		if name != "" {
			if op.Name != nil && op.Name.Value == name {
				selected = op
				break
			}
			continue
		}
		if selected != nil {
			return false
		}
		selected = op
	}
	return selected != nil && selected.Operation == "query"
}
func (h *Handler) executeParsed(ctx context.Context, schema graphql.Schema, doc *ast.Document, name string, args map[string]interface{}) *graphql.Result {
	var result *graphql.Result
	execute := func(ctx context.Context) error {
		result = graphql.Execute(graphql.ExecuteParams{Schema: schema, Root: (map[string]interface{})(nil), AST: doc, OperationName: name, Args: args, Context: ctx})
		return nil
	}
	if selectedReadOperation(doc, name) {
		if err := port.WithQuerySnapshot(ctx, h.schema.storage, execute); err != nil {
			return &graphql.Result{Errors: gqlerrors.FormatErrors(err)}
		}
	} else {
		_ = execute(ctx)
	}
	return result
}

// FreshSnapshotRequired prevents REST caches/singleflight from carrying data or
// a transaction context across an execution generation or request lifetime.
func (h *Handler) FreshSnapshotRequired() bool {
	policy, ok := h.schema.storage.(port.ExecutionSnapshotPolicy)
	return ok && policy.ExecutionSnapshotsEnabled()
}
