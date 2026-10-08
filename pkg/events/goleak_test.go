package events

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's tests when a goroutine outlives them
// (refactoring plan R0-7, C1: goroutines that are never released).
func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
