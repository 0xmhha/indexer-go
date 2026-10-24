//go:build !darwin && !linux

package exporter

import (
	"github.com/0xmhha/indexer-go/x/stablenet/executor/spool"
	"os"
)

func sourceOwned(os.FileInfo) bool      { return false }
func sourcePeerUID(int) (uint32, error) { return 0, spool.ErrConflict }
