// Package arc implements Arc's USDC-native execution accounting.
package arc

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
)

const ID = "arc"

var NativeEmitter = common.HexToAddress("0xfffffffffffffffffffffffffffffffffffffffe")

func New() *evm.Profile {
	return evm.New(ID,
		evm.WithDetect(func(info chains.NodeInfo) bool { return info.ChainID == 5042 || info.ChainID == 5042002 }),
		evm.WithNativeAccounting(Accounting{}),
		evm.WithNativeCoinContract(NativeEmitter),
	)
}

func init() { chains.Register(New(), 100) }
