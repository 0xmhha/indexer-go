package nodebackend

import (
	"go.uber.org/goleak"
	"testing"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.IgnoreTopFunction("github.com/ethereum/go-ethereum/core.(*txSenderCacher).cache"))
}
