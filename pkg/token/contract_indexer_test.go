package token

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// nodeError is an error the node answered (a JSON-RPC error).
type nodeError string

func (e nodeError) Error() string { return string(e) }
func (nodeError) ErrorCode() int  { return -32000 }

// nodeAtBlock is a node over mockEthClient that records the block of each
// read. With missing it keeps no state of past blocks; with down it
// answers nothing; latest reads see supply as the total supply.
type nodeAtBlock struct {
	*mockEthClient
	blocks  []interface{}
	missing bool
	down    error
	supply  *big.Int
}

func (n *nodeAtBlock) read(block interface{}) error {
	n.blocks = append(n.blocks, block)
	switch {
	case n.down != nil:
		return n.down
	case n.missing && block != nil:
		return nodeError("missing trie node 1f2e (path )")
	}
	return nil
}

func (n *nodeAtBlock) CodeAt(ctx context.Context, a common.Address, block interface{}) ([]byte, error) {
	if err := n.read(block); err != nil {
		return nil, err
	}
	return n.mockEthClient.CodeAt(ctx, a, block)
}

func (n *nodeAtBlock) CallContract(ctx context.Context, call ethereum.CallMsg, block interface{}) ([]byte, error) {
	if err := n.read(block); err != nil {
		return nil, err
	}
	if block == nil && n.supply != nil && fmt.Sprintf("%x", call.Data[:4]) == SelectorTotalSupply[2:] {
		return abiEncodeUint256(n.supply), nil
	}
	out, err := n.mockEthClient.CallContract(ctx, call, block)
	if err != nil {
		return nil, nodeError("execution reverted")
	}
	return out, nil
}

// portStore keeps token metadata in a map (TokenMetadataStore).
type portStore struct {
	port.TokenMetadataReader // unused methods
	tokens                   map[common.Address]*port.TokenMetadata
}

func newPortStore() *portStore { return &portStore{tokens: map[common.Address]*port.TokenMetadata{}} }

func (s *portStore) GetTokenMetadata(_ context.Context, a common.Address) (*port.TokenMetadata, error) {
	if m, ok := s.tokens[a]; ok {
		return m, nil
	}
	return nil, port.ErrNotFound
}

func (s *portStore) SaveTokenMetadata(_ context.Context, m *port.TokenMetadata) error {
	s.tokens[m.Address] = m
	return nil
}

func (s *portStore) DeleteTokenMetadata(_ context.Context, a common.Address) error {
	delete(s.tokens, a)
	return nil
}

func erc20Node(addr common.Address) *nodeAtBlock {
	mc := newMockEthClient()
	mc.codeAt[addr] = buildBytecodeWithSelectors([]string{
		SelectorTransfer, SelectorBalanceOf, SelectorTotalSupply, SelectorName, SelectorSymbol, SelectorDecimals,
	})
	mc.setCallResult(addr, SelectorName, abiEncodeString("TestToken"))
	mc.setCallResult(addr, SelectorSymbol, abiEncodeString("TT"))
	mc.setCallResult(addr, SelectorDecimals, abiEncodeUint8(18))
	mc.setCallResult(addr, SelectorTotalSupply, abiEncodeUint256(big.NewInt(1000)))
	return &nodeAtBlock{mockEthClient: mc, supply: big.NewInt(2000)}
}

// TestIndexContractReadsTheCreationBlock: token.metadata follows the
// determinism rules (docs/SDK.md): the contract is read as of its creation
// block, so a backfill stores what live indexing stored.
func TestIndexContractReadsTheCreationBlock(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000020")
	ctx := context.Background()

	t.Run("every read is at the block", func(t *testing.T) {
		node, store := erc20Node(addr), newPortStore()
		require.NoError(t, NewContractIndexer(node, store, zap.NewNop()).IndexContract(ctx, addr, 42, 1000))
		require.NotEmpty(t, node.blocks)
		for _, b := range node.blocks {
			assert.Equal(t, big.NewInt(42), b)
		}
		require.Contains(t, store.tokens, addr)
		assert.Equal(t, big.NewInt(1000), store.tokens[addr].TotalSupply, "the supply at the block, not the latest")
		assert.Equal(t, "TT", store.tokens[addr].Symbol)
	})
	t.Run("a node without the block's state is read at its latest state", func(t *testing.T) {
		node, store := erc20Node(addr), newPortStore()
		node.missing = true
		require.NoError(t, NewContractIndexer(node, store, zap.NewNop()).IndexContract(ctx, addr, 42, 1000))
		require.Contains(t, store.tokens, addr)
		assert.Equal(t, big.NewInt(2000), store.tokens[addr].TotalSupply)
	})
	t.Run("a node that does not answer fails the block", func(t *testing.T) {
		node, store := erc20Node(addr), newPortStore()
		node.down = errors.New("connection refused")
		err := NewContractIndexer(node, store, zap.NewNop()).IndexContract(ctx, addr, 42, 1000)
		require.ErrorContains(t, err, "connection refused")
		assert.Empty(t, store.tokens)
	})
	t.Run("reverted calls are answers: a contract that is no token is skipped", func(t *testing.T) {
		mc := newMockEthClient()
		mc.codeAt[addr] = []byte{0x60, 0x00}
		node, store := &nodeAtBlock{mockEthClient: mc}, newPortStore()
		require.NoError(t, NewContractIndexer(node, store, zap.NewNop()).IndexContract(ctx, addr, 42, 1000))
		assert.Empty(t, store.tokens)
	})
}
