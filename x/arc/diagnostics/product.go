// Package diagnostics binds the product's existing maintenance format without DB access.
package diagnostics

import (
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/nativediagnostics"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

func Product() nativediagnostics.Product {
	return nativediagnostics.Product{Name: "arc-native", InspectNative: nativestore.InspectMetadata, Managed: inspectManaged, ChainAllowed: func(c execwire.Chain) bool {
		if c.ID == "5042" {
			return true
		}
		var genesis execwire.Hash
		_ = genesis.UnmarshalText([]byte("0xe20e653af4441e8c6088e172b129d56420139824400477287b46e7101ae2bb1f"))
		return c.ID == "5042002" && c.Genesis == genesis
	}}
}
