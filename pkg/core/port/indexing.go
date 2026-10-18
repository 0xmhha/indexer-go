package port

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

type IndexingUnit struct {
	Name       string                `json:"name"`
	State      FeatureState          `json:"state"`
	Checkpoint *model.UnitCheckpoint `json:"checkpoint"`
}
type IndexingPage struct {
	Descriptor *model.ProjectionDescriptor `json:"source"`
	Head       *model.BlockPoint           `json:"head"`
	Units      []IndexingUnit              `json:"units"`
	Next       string                      `json:"next"`
}

// IndexingReader returns one consistent, bounded projection DB snapshot.
// It is read-only and must work in API processes without opening a writer.
type IndexingReader interface {
	ReadIndexingPage(context.Context, string, int) (IndexingPage, error)
}
