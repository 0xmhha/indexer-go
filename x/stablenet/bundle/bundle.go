// Package bundle links in everything the StableNet chain adds to the
// indexer: its chain profile, features, GraphQL and JSON-RPC extensions and
// startup steps. A binary built for StableNet imports it for its effects.
package bundle

import (
	_ "github.com/0xmhha/indexer-go/x/stablenet"                          // chain profile
	_ "github.com/0xmhha/indexer-go/x/stablenet/consensus/api"            // WBFT consensus API
	_ "github.com/0xmhha/indexer-go/x/stablenet/features/feedelegation"   // stablenet.fee_delegation feature
	_ "github.com/0xmhha/indexer-go/x/stablenet/features/systemcontracts" // stablenet.system_contracts feature
	_ "github.com/0xmhha/indexer-go/x/stablenet/features/wbft"            // stablenet.wbft feature
	_ "github.com/0xmhha/indexer-go/x/stablenet/feedelegation/api"        // fee delegation API
	_ "github.com/0xmhha/indexer-go/x/stablenet/systemcontracts"          // known tokens, verification startup step
	_ "github.com/0xmhha/indexer-go/x/stablenet/systemcontracts/api"      // system contract API
)
