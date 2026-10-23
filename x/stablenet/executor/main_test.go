package executor

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// The pinned core/sender_cacher.go creates this process-lifetime pool at
	// package initialization with no exported shutdown. Ignore only its exact
	// blocking function; executor callbacks and owned workers remain checked.
	goleak.VerifyTestMain(m, goleak.IgnoreTopFunction("github.com/ethereum/go-ethereum/core.(*txSenderCacher).cache"))
}
