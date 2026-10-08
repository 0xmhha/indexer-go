package main

import (
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/features/records"
)

// initRecords serves the declared tables (refactoring plan R6-1) when the
// records feature is enabled: the plan compiled from features.records is
// attached to the storage, where the GraphQL extension finds it.
func (a *App) initRecords() error {
	if !a.featureOverrides()[records.Name] {
		return nil
	}
	plan, err := records.Settings(a.config.FeatureSettings)
	if err != nil {
		return err
	}
	records.Attach(a.storage, plan)
	a.logger.Info("Declared tables", zap.Int("tables", len(plan.Tables)), zap.Int("contracts", len(plan.Addresses())))
	return nil
}
