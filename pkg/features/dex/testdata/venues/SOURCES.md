# Venue ABI fixtures

The DEX feature decodes venue events and view calls from hard-coded signature
strings (`events.go`, `orderbook/reconcile.go`). These ABI files are the
contract side of that agreement; `venue_abi_test.go` and
`orderbook/venue_abi_test.go` fail when a signature string, its indexed-topic
count or the number of words read no longer matches them.

| Venue | Files | Source | How to regenerate |
|---|---|---|---|
| `uniswap_v3` | `IUniswapV3Factory.json`, `IUniswapV3Pool.json` | Uniswap `v3-core` `d8b1c63` (poc-contract `lib/v3-core`) | `solc --abi lib/v3-core/contracts/interfaces/IUniswapV3{Factory,Pool}.sol` |
| `uniswap_v2` | `IUniswapV2Factory.json`, `IUniswapV2Pair.json` | Uniswap `v2-core` `ee547b1` (poc-contract `lib/v2-core`) | `solc --abi lib/v2-core/contracts/interfaces/IUniswapV2{Factory,Pair}.sol` |
| `perp_orderbook` | `PerpetualEngine.json`, `OrderManager.json` | poc-contract `abi/perpetual` at `3f40bdf` | `./script/export-venue-abi.sh` in poc-contract, then copy `abi/perpetual/*.json` |

JSON is pretty-printed with sorted keys so diffs stay readable. When a
contract changes an event or a view the indexer reads, update the fixture and
the signature string in the same change.
