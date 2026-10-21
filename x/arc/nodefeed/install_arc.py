#!/usr/bin/env python3
"""Apply the optional ExEx hook to the audited Arc checkout, never globally."""
import argparse
import json
import os
from pathlib import Path
import subprocess

ARC_REV = "6e764023ee6515fe70573e123ed2db912a7207b4"
parser = argparse.ArgumentParser()
parser.add_argument("--arc-checkout", type=Path, required=True)
args = parser.parse_args()
root = args.arc_checkout.resolve()
revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
if revision != ARC_REV:
    raise SystemExit(f"Arc revision is {revision}; audited revision {ARC_REV} is required")
module = Path(__file__).resolve().parent
manifest = root / "crates/node/Cargo.toml"
source = root / "crates/node/src/main.rs"
manifest_text, source_text = manifest.read_text(), source.read_text()
if "indexer-arc-node-feed =" not in manifest_text:
    path = os.path.relpath(module, manifest.parent)
    manifest_text = manifest_text.replace("[dependencies]\n", '[dependencies]\nindexer-arc-node-feed = { path = ' + json.dumps(path) + ', features = ["reth"] }\n', 1)
if '"indexer-arc-node-feed"' not in source_text:
    anchor = "            let handle = builder\n"
    assert source_text.count(anchor) == 1
    source_text = source_text.replace(anchor, '''            let feed_socket = std::env::var_os("INDEXER_ARC_FEED_SOCKET").map(std::path::PathBuf::from);
            let feed_chain_id = builder.config().chain.chain().id();
''' + anchor)
    anchor = "                .launch_with_debug_capabilities()"
    assert source_text.count(anchor) == 1
    source_text = source_text.replace(anchor, '''                .install_exex_if(feed_socket.is_some(), "indexer-arc-node-feed", move |ctx| {
                    let cfg = indexer_arc_node_feed::Config::new(feed_socket.expect("enabled feed socket"), feed_chain_id);
                    async move { Ok::<_, eyre::Report>(indexer_arc_node_feed::exex::run(ctx, cfg)) }
                })
''' + anchor)
manifest.write_text(manifest_text)
source.write_text(source_text)
print(f"Optional ExEx hook installed for Arc {revision}; set INDEXER_ARC_FEED_SOCKET to enable it")
