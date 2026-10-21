package cli

import (
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/app"
	"github.com/0xmhha/indexer-go/pkg/chains"
)

// Product names another binary and the nodes it is built for.
type Product struct {
	Binary string
	// Matches reports whether a node belongs to that binary's chain.
	Matches func(chains.NodeInfo) bool
}

// WithChain builds the binary for one chain: it refuses a node whose
// selected profile is not chain (a profile id).
func WithChain(chain string) Option { return func(c *cli) { c.chain = chain } }

// WithOtherProducts lists the binaries built for other chains: a node one of
// them matches is refused with the name of the binary to run instead.
func WithOtherProducts(ps ...Product) Option {
	return func(c *cli) { c.others = append(c.others, ps...) }
}

// chainCheck returns the check of the node's chain, nil if the binary
// accepts any chain.
func (c *cli) chainCheck() app.ChainCheck {
	if c.chain == "" && len(c.others) == 0 {
		return nil
	}
	return func(info chains.NodeInfo, p chains.Profile) error {
		for _, o := range c.others {
			if o.Matches(info) {
				return fmt.Errorf("the node (%q, chain id %d) is a chain %s is built for; %s does not decode it correctly: run %s",
					info.ClientVersion, info.ChainID, o.Binary, c.name, o.Binary)
			}
		}
		if c.chain != "" && p.ID() != c.chain {
			return fmt.Errorf("%s indexes %s chains, but the node (%q, chain id %d) selected profile %q: run the indexer built for this chain",
				c.name, c.chain, info.ClientVersion, info.ChainID, p.ID())
		}
		return nil
	}
}
