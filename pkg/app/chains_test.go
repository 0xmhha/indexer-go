package app

// The end-to-end tests run StableNet and Arc scenarios, so the test binary
// links in the chain bundles the indexer command leaves out.
import (
	_ "github.com/0xmhha/indexer-go/x/arc/bundle"
	_ "github.com/0xmhha/indexer-go/x/stablenet/bundle"
)
