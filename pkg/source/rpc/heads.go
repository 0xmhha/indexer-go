package rpc

import (
	"context"
	"encoding/json"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

// headRetry is how long SubscribeHeads waits before reconnecting.
const headRetry = 2 * time.Second

// SubscribeHeads subscribes to the node's newHeads over a WebSocket
// endpoint and signals the returned channel for every new head. Signals
// coalesce: the channel holds at most one pending signal, so a slow reader
// polls once for several heads. The subscription reconnects after errors
// until ctx ends; the channel is closed then. It is a hint for the live
// loop, which keeps polling, so a lost connection only adds latency.
func SubscribeHeads(ctx context.Context, wsURL string, logger *zap.Logger) <-chan struct{} {
	if logger == nil {
		logger = zap.NewNop()
	}
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		for ctx.Err() == nil {
			if err := followHeads(ctx, wsURL, out); err != nil && ctx.Err() == nil {
				logger.Warn("newHeads subscription failed, retrying", zap.String("endpoint", wsURL), zap.Error(err))
			}
			select {
			case <-ctx.Done():
			case <-time.After(headRetry):
			}
		}
	}()
	return out
}

// followHeads runs one subscription until it fails or ctx ends.
func followHeads(ctx context.Context, wsURL string, out chan<- struct{}) error {
	c, err := gethrpc.DialWebsocket(ctx, wsURL, "")
	if err != nil {
		return err
	}
	defer c.Close()
	heads := make(chan json.RawMessage, 16)
	sub, err := c.EthSubscribe(ctx, heads, "newHeads")
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-sub.Err():
			return err
		case <-heads:
			select {
			case out <- struct{}{}:
			default: // a signal is already pending
			}
		}
	}
}
