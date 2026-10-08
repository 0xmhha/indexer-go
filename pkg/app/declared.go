package app

import (
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/records"
	"github.com/0xmhha/indexer-go/pkg/source"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
)

// The declared ingest mode (indexer.mode: declared, refactoring plan R6-1)
// reads only the headers and the logs of the tables declared in
// features.records, runs only the features that need nothing else and
// serves only their APIs.

// defaultFeatures returns the features on by default: the registered
// defaults and the profile's, none in the declared mode (the explorer
// features need whole blocks).
func (a *App) defaultFeatures(profile chains.Profile) []string {
	if a.config.DeclaredMode() {
		return nil
	}
	return append(feature.Defaults(), profile.Features()...)
}

// checkDeclaredFeatures requires the records feature in the declared mode
// and refuses features that need more than the declared logs.
func (a *App) checkDeclaredFeatures(enabled []string) error {
	if !a.config.DeclaredMode() {
		return nil
	}
	var others []string
	hasRecords := false
	for _, name := range enabled {
		if name == records.Name {
			hasRecords = true
		}
		if !feature.IsLogsOnly(name) {
			others = append(others, name)
		}
	}
	if !hasRecords {
		return fmt.Errorf("indexer.mode declared needs features.%s with the declared sources and tables", records.Name)
	}
	if len(others) > 0 {
		return fmt.Errorf("indexer.mode declared reads only the declared logs; these features need whole blocks: %s", strings.Join(others, ", "))
	}
	return nil
}

// declaredSource returns the source the fetcher reads: in the declared
// mode the headers and declared logs of src, otherwise blocks itself. It
// also starts indexing at the declared start block when indexer.start_height
// is not set.
func (a *App) declaredSource(src *sourcerpc.Source, blocks source.Source) (source.Source, error) {
	if !a.config.DeclaredMode() {
		return blocks, nil
	}
	plan, err := records.Settings(a.config.FeatureSettings)
	if err != nil {
		return nil, err
	}
	if a.config.Indexer.StartHeight == 0 {
		a.fetcher.SetStartHeight(plan.StartBlock())
	}
	a.fetcher.SetDeclared(true)
	a.logger.Info("Declared ingest mode: reading headers and declared logs only",
		zap.Int("contracts", len(plan.Addresses())), zap.Int("events", len(plan.Topics())), zap.Uint64("start_block", plan.StartBlock()))
	return sourcerpc.NewLogs(src, plan.Addresses(), plan.Topics()), nil
}
