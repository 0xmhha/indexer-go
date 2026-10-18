package arc

import "github.com/0xmhha/indexer-go/pkg/core/model"

const BlobAuditRevision = "6e764023ee6515fe70573e123ed2db912a7207b4"

// BlobCapability describes the pinned production pool's no_eip4844 policy.
// It is not a proof about every historical block or a consensus-rule change.
func BlobCapability() model.BlobCapability {
	return model.BlobCapability{Version: 1, Profile: ID, Status: model.BlobUnsupported, Reason: "eip4844_disabled_by_node_policy", Basis: "node_transaction_admission", Revision: BlobAuditRevision, SourceURL: "https://github.com/circlefin/arc-node/blob/" + BlobAuditRevision + "/crates/execution-txpool/src/pool.rs"}
}
