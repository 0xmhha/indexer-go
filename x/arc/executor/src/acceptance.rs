//! Owned fixture construction only; this module never ships in a binary.
use crate::{
    checkpoint, native,
    native_import::tests::{continued, continued_variant, native as factory},
};
use alloy_primitives::{Bytes, Sealable};
use arc_evm::{ArcEvmConfig, ArcEvmFactory};
use eyre::{Result, ensure};
use reth_ethereum::evm::EthEvmConfig;
use reth_ethereum::provider::SaveBlocksMode;
use reth_storage_api::HeaderProvider;
use std::{os::unix::fs::PermissionsExt, path::Path};

fn write(root: &Path, name: &str, bytes: &[u8]) -> Result<()> {
    let p = root.join(name);
    ensure!(!p.exists(), "fixture never overwrites inputs");
    std::fs::write(&p, bytes)?;
    std::fs::set_permissions(p, std::fs::Permissions::from_mode(0o600))?;
    Ok(())
}
fn export_fixture(root: &Path, count: u64) -> Result<()> {
    ensure!(
        root.is_absolute() && (4..=131).contains(&count),
        "owned bounded fixture root"
    );
    std::fs::set_permissions(root, std::fs::Permissions::from_mode(0o700))?;
    let (_work, f, spec) = factory(false)?;
    let state = root.join("genesis.ndjson");
    native::export_checkpoint(&f, &spec, 0, None, &state, root)?;
    let (m, _) = checkpoint::seed(&state, "5042002", spec.inner.genesis_hash())?;
    write(root, "expected.json", &serde_json::to_vec(&m)?)?;
    write(
        root,
        "header-0.rlp",
        &alloy_rlp::encode(f.provider()?.header_by_number(0)?.unwrap()),
    )?;
    for height in 1..=count {
        if height == 3 {
            let alternate = continued_variant(
                &f,
                spec.clone(),
                height - 1,
                Bytes::from_static(b"N6-short"),
            )?;
            write(
                root,
                "alternate-3.rlp",
                &alloy_rlp::encode(alternate.recovered_block().clone().into_block()),
            )?;
        }
        let executed = continued(&f, spec.clone(), height - 1)?;
        let block = executed.recovered_block().clone().into_block();
        ensure!(
            block.body.transactions.is_empty(),
            "official system/empty fixture, no funded synthetic genesis"
        );
        write(
            root,
            &format!("block-{height}.rlp"),
            &alloy_rlp::encode(&block),
        )?;
        write(root, &format!("receipts-{height}.rlp"), &[0xc0])?;
        let p = f.provider_rw()?;
        p.save_blocks(vec![executed], SaveBlocksMode::Full)?;
        p.commit()?;
        if height == 3 {
            // Independent expected payload for lost-ACK retransmission. This
            // fixture construction replay precedes the counted App runtime.
            let provider = f.provider()?;
            let parent = provider
                .sealed_header(2)?
                .unwrap()
                .header()
                .clone()
                .seal_slow();
            let state = provider.history_by_block_hash(parent.hash())?;
            let config = ArcEvmConfig::new(EthEvmConfig::new_with_evm_factory(
                spec.clone(),
                ArcEvmFactory::new(spec.clone()),
            ));
            let result = crate::replay(&config, &state, &parent, &block, &[], 1)?;
            write(root, "result-3.exr", &result.encode()?)?;
        }
    }
    let (_deep, f, spec) = factory(false)?;
    for height in 1..=count {
        let executed =
            continued_variant(&f, spec.clone(), height - 1, Bytes::from_static(b"N6-deep"))?;
        write(
            root,
            &format!("deep-{height}.rlp"),
            &alloy_rlp::encode(executed.recovered_block().clone().into_block()),
        )?;
        let p = f.provider_rw()?;
        p.save_blocks(vec![executed], SaveBlocksMode::Full)?;
        p.commit()?;
    }
    write(
        root,
        "fixture.json",
        &serde_json::to_vec(
            &serde_json::json!({"Version":1,"Blocks":count,"Chain":m.chain,"Anchor":m.r#ref,"Root":m.root,"Scope":"owned-official-testnet-system-blocks-not-live-chain; fixture EVM generation excluded from runtime counts"}),
        )?,
    )?;
    Ok(())
}

#[test]
fn integrated_fixture_exports_official_checkpoint_and_two_branches() -> Result<()> {
    let work = tempfile::tempdir()?;
    export_fixture(work.path(), 4)?;
    for name in [
        "genesis.ndjson",
        "expected.json",
        "header-0.rlp",
        "block-1.rlp",
        "receipts-1.rlp",
        "block-4.rlp",
        "alternate-3.rlp",
        "deep-4.rlp",
    ] {
        assert!(
            work.path().join(name).is_file(),
            "missing integrated input {name}"
        );
    }
    if let Some(root) = std::env::var_os("INDEXER_ARC_INGRESS_FIXTURE_DIR") {
        let root = std::path::PathBuf::from(root);
        ensure!(!root.exists(), "selected exported fixture must be new");
        std::fs::create_dir(&root)?;
        export_fixture(&root, 131)?;
    }
    Ok(())
}
