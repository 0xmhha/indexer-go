package storage

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// Header extra data captured from a local go-stablenet (Gstable v1.1.0,
// commit 740526d) network with three validators. Absent seals and epoch info
// are encoded as empty values; decoding them used to fail for every header,
// so no WBFT data was ever stored.
const (
	liveGenesisExtra = "0xf8fa808080c0c080c0c086191a20322000f8e9f84ed994e56e6aac8be827db4c47aae0aa345929eeb35d76831cfde0d9949fb501b60a7ecc0e9d129e2e614300914bc69cbf831cfde0d99486840420cb11ab2ffdd9229471a1ba1e004ec785831cfde0c3800102f893b09976b3836ccc64e7aa279b7aaa92a0d68f1ad62e3ca51da963f49974ef75f18f7aaab9b806b3c5dbc26bf53ccf74d039b0b3cda4545df534ea48738b9f6aa88208206b0ee26285592deee68c169604167026288ed249b3cfb0c1103e250ed93b72b0867acc9facf80a4a7d6691ca7cf9da1331449543879d13d5a1e6bbe6081a865e5c91399b13d129bb4b6519b35b328239"
	liveBlock5Extra  = "0xf90202a0d983010100846765746888676f312e32352e328664617277696e000000000000b841d5bb0f1d045194f2c84488a9e35fe1457212a39c0a95d224b1438701a58619624bb5e0dafdb64803403ec5f0e06d12dbf4551a039f521534d9b9b60086bb055b0080f86307b8608bf66454995eb63575e4314c47849daae74dae982b85bede6c9532ce99eba1bd8f150d030117a9da225e68169a49b37a08e6dc389397d03ee41dd0925df00f7a818a284107e1d69827dc3fb25b8cceac912cbfe70fcb7ca3f41a8c33580608e4f86307b860b4bb24e2c4646cfd44a961d91e6de6689bf2b60ae23109e280321a7daf5daa79fdfd327f2c9c32c208a2f84df227884e03be70c5a9310ad13399bd8c24896fc1f923b0def8ac29e980cb51ba99fcd847cc544fedfe0b2696af42afca774c06fb80f86307b86081032ece62f7e6918b46dbd735d17342dfa4c9d4e15978aaa6c592521823511b3182b4141d3d4640c1e8a59527a22bf50ccbc23717b4d7f47aa3d4e5bf4fb757c8bf1cbff57031569c117bea5ebf15b8e2d54ffed86f38e17a7a13169c5ad890f86307b860aaed7b4fd0ce74b71edb5a0437159b019a971590bfb57a922fb7a5a711b4ba117367998400c6cdd5e0b1b30eeda9d1130cb1803d203228ead397b6ba395709adeea84e62ab2cb2cbb8735fddc3d6a23172ee6e57c9de8ac9347bcb374465b97886191a20322000c0"
)

func TestParseWBFTExtraLiveHeaders(t *testing.T) {
	genesis, err := ParseWBFTExtra(&types.Header{Number: big.NewInt(0), Extra: hexutil.MustDecode(liveGenesisExtra)})
	require.NoError(t, err)
	require.NotNil(t, genesis.EpochInfo, "genesis carries the first epoch")
	require.Len(t, genesis.EpochInfo.Validators, 3)
	require.Nil(t, genesis.CommittedSeal)

	block, err := ParseWBFTExtra(&types.Header{Number: big.NewInt(5), Extra: hexutil.MustDecode(liveBlock5Extra)})
	require.NoError(t, err)
	require.NotNil(t, block.CommittedSeal)
	require.NotNil(t, block.PreparedSeal)
	require.Nil(t, block.EpochInfo, "regular blocks carry no epoch info")
}
