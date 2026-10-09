package scan

import (
	"context"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/ethereum/go-ethereum"
)

type caller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, block any) ([]byte, error)
}

func handle(ctx context.Context, c caller, block any) {
	_ = time.Now()
	_ = rand.Intn(3)
	_ = os.Getenv("X")
	_, _ = http.Get("http://example.com")
	_, _ = c.CallContract(ctx, ethereum.CallMsg{}, nil)
	_, _ = c.CallContract(ctx, ethereum.CallMsg{}, block)
	_ = time.Unix(0, 0)
	//sdk:nondeterministic a route's cache, not a handler
	_ = time.Now()
	_ = time.Since(time.Time{}) //sdk:nondeterministic same line
}
