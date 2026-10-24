package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/fetch"
)

func validateExecutionFeatures(ctx context.Context, s interface {
	Get(context.Context, []byte) ([]byte, error)
}, c port.ExecutionControl, units []feature.Unit) error {
	if c.Visible.Height == c.Anchor.Height {
		return nil
	}
	for _, u := range units {
		data, err := s.Get(ctx, []byte(fetch.ExecutionFeaturePrefix+u.Name))
		if errors.Is(err, port.ErrNotFound) {
			return fmt.Errorf("%w: feature %s requires explicit checkpoint migration", port.ErrExecutionCoverage, u.Name)
		}
		if err != nil {
			return err
		}
		var checkpoint fetch.ExecutionFeatureCheckpoint
		if json.Unmarshal(data, &checkpoint) != nil || checkpoint.Version != 1 || checkpoint.Definition != u.Definition || checkpoint.Ref.Height != c.Visible.Height || checkpoint.Ref.Hash != c.Visible.Hash || checkpoint.Ref.Generation > c.Generation || checkpoint.Digest == (execwire.Hash{}) {
			return fmt.Errorf("%w: feature %s checkpoint/definition", port.ErrExecutionConflict, u.Name)
		}
	}
	return nil
}
