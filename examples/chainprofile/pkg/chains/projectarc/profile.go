// Package projectarc shows how a project customizes an existing EVM chain
// in its own module without changing the indexer core.
package projectarc

import (
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/arc"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
)

const ID = "project-arc"

// Profile embeds the concrete base profile so its optional accounting,
// native-coin and binary decoding interfaces remain available.
type Profile struct{ *evm.Profile }

var (
	_ chains.Profile               = (*Profile)(nil)
	_ chains.AccountingProfile     = (*Profile)(nil)
	_ chains.BinaryProfile         = (*Profile)(nil)
	_ chains.BlobCapabilityProfile = (*Profile)(nil)
)

// New retains Arc's header, transaction, receipt and native accounting rules.
// Only the project identity and detection scope change in this example.
func New() *Profile { return &Profile{Profile: arc.New()} }

func (*Profile) ID() string { return ID }

// Detect limits this project's profile to Arc testnet. Arc mainnet keeps the
// built-in profile; other EVM chains keep their own profiles.
func (*Profile) Detect(info chains.NodeInfo) bool { return info.ChainID == 5042002 }

func init() {
	// Built-in specific profiles have priority 100, generic EVM has 0.
	// Use a distinct id rather than trying to overwrite the built-in "arc".
	chains.Register(New(), 200)
	chains.RegisterAlias("project-arc-testnet", ID)
}
