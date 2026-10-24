//go:build !darwin && !linux

package execution

import (
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"os"
)

func executionPeerUID(int) (uint32, error)        { return 0, port.ErrInvalidData }
func executionOwned(os.FileInfo) bool             { return false }
func publishExecutionSocket(string, string) error { return port.ErrInvalidData }
