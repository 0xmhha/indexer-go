// Package bundle links in everything the Arc chain adds to the indexer: its
// chain profile and its source.feed module. A binary built for Arc imports
// it for its effects.
package bundle

import (
	_ "github.com/0xmhha/indexer-go/x/arc"         // chain profile
	_ "github.com/0xmhha/indexer-go/x/arc/feed"    // source.feed module
	_ "github.com/0xmhha/indexer-go/x/arc/ingress" // opt-in App-owned native input
)
