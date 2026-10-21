# Arc node feed

An optional ExEx for Arc's Reth v2.2.0 SDK node. It exposes bounded local IPC acquisition, hash-bound native state and commit acknowledgments. The Go adapter is `pkg/source/nodefeed`; the shared wire schema is `pkg/nodefeed`.

See [the integration guide](../../../docs/NODE_FEED.md) for the audited Arc revision, hook installer, same-kernel socket placement, retention/ACK rules and validation limits.

```sh
cargo check --features reth --locked
cargo test --locked
```

The `fixture` binary is an offline interoperability test producer, not a full node. `install_arc.py` only patches the audited checkout with an optional hook. It does not start a node or configure networking. Keep `INDEXER_ARC_FEED_SOCKET` unset to retain stock Arc startup behavior.
