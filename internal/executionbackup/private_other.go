//go:build !darwin && !linux

package executionbackup

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"os"
)

func openPrivate(string) (*os.File, error) { return nil, retention.ErrUnsafe }
func privateInfo(os.FileInfo) bool         { return false }
func publishReceipt(context.Context, string, retention.IndexerBackup) error {
	return retention.ErrUnsafe
}
