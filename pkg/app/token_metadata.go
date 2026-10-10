package app

import (
	"context"
	"errors"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/token"
)

// proxyTokenMetadata reads the metadata of tokens the store lacks
// (GetTokenBalances) from the node through the RPC proxy, so the API's
// node calls share its cache, rate limit and circuit breaker.
type proxyTokenMetadata struct {
	proxy  *rpcproxy.Proxy
	logger *zap.Logger
}

var _ port.TokenMetadataFetcher = (*proxyTokenMetadata)(nil)

// FetchTokenMetadata detects the token standard and reads the metadata.
// When a node call goes unanswered (rate limited, circuit open, timeout)
// it returns that error instead of the partial metadata, which the caller
// would otherwise store for good.
func (f *proxyTokenMetadata) FetchTokenMetadata(ctx context.Context, address common.Address) (*port.TokenMetadata, error) {
	client := &proxyTokenClient{proxy: f.proxy}
	md, err := token.NewStorageTokenMetadataFetcher(client, f.logger).FetchTokenMetadata(ctx, address)
	if err != nil {
		return nil, err
	}
	if err := client.unanswered(); err != nil {
		return nil, err
	}
	return md, nil
}

// proxyTokenClient is token.EthClient over the RPC proxy, remembering the
// first call the node did not answer. Errors the node answered (reverts,
// as for a method the contract lacks) are part of the metadata.
type proxyTokenClient struct {
	proxy  *rpcproxy.Proxy
	mu     sync.Mutex
	failed error
}

func (c *proxyTokenClient) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber interface{}) ([]byte, error) {
	block, err := blockArg(blockNumber)
	if err != nil {
		return nil, c.note(err)
	}
	data, err := c.proxy.Call(ctx, call, block)
	return data, c.note(err)
}

func (c *proxyTokenClient) CodeAt(ctx context.Context, contract common.Address, blockNumber interface{}) ([]byte, error) {
	block, err := blockArg(blockNumber)
	if err != nil {
		return nil, c.note(err)
	}
	resp, err := c.proxy.GetCode(ctx, &rpcproxy.CodeRequest{Address: contract, BlockNumber: block})
	if err != nil {
		return nil, c.note(err)
	}
	return resp.Code, nil
}

// note records err when the node did not answer it and returns it.
func (c *proxyTokenClient) note(err error) error {
	var answered rpc.Error
	if err == nil || errors.As(err, &answered) {
		return err
	}
	c.mu.Lock()
	if c.failed == nil {
		c.failed = err
	}
	c.mu.Unlock()
	return err
}

func (c *proxyTokenClient) unanswered() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failed
}

// blockArg converts the token package's block argument: nil (latest) or
// a *big.Int.
func blockArg(v interface{}) (*big.Int, error) {
	switch b := v.(type) {
	case nil:
		return nil, nil
	case *big.Int:
		return b, nil
	default:
		return nil, errors.New("token metadata: unsupported block argument")
	}
}
