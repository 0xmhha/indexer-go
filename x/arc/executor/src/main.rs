//! Read one already imported Arc block; never launch a node or modify its DB.
use alloy_primitives::Sealable;
use arc_evm::{ArcEvmConfig, ArcEvmFactory};
use arc_execution_config::chainspec::{ArcChainSpec, MAINNET, TESTNET};
use clap::{Parser, ValueEnum};
use eyre::{Result, ensure, eyre};
use indexer_arc_executor::{
    ARC_REVISION, Verified,
    native::{NativeTypes, read_only_config},
    replay,
    service::{self, Operation},
    write_result,
};
use reth_ethereum::{
    evm::EthEvmConfig,
    provider::{
        BlockHashReader, BlockReader, HeaderProvider, ReceiptProvider,
        providers::ProviderFactoryBuilder,
    },
    tasks::Runtime,
};

use reth_storage_api::BlockNumReader;
use std::{path::PathBuf, sync::Arc};

#[derive(Clone, Copy, Debug, ValueEnum)]
enum Chain {
    Mainnet,
    Testnet,
}

#[derive(Debug, Parser)]
#[command(
    about = "Replay an imported Arc block from a retained read-only native database; no node services"
)]
struct Args {
    #[arg(long)]
    chain: Chain,
    #[arg(long)]
    datadir: PathBuf,
    #[arg(long, required_unless_present = "serve", value_parser = clap::value_parser!(u64).range(1..=i64::MAX as u64))]
    height: Option<u64>,
    #[arg(long, required_unless_present = "serve", value_parser = clap::value_parser!(u64).range(1..=i64::MAX as u64))]
    generation: Option<u64>,
    #[arg(long, required_unless_present = "serve")]
    output: Option<PathBuf>,
    #[arg(long, conflicts_with_all = ["height", "generation", "output"])]
    serve: bool,
    #[arg(long)]
    scratch_dir: Option<PathBuf>,
}

fn main() -> Result<()> {
    produce(Args::parse())
}

fn produce(mut args: Args) -> Result<()> {
    ensure!(
        args.datadir.is_absolute() && args.output.as_ref().is_none_or(|p| p.is_absolute()),
        "native/output paths must be absolute"
    );
    args.datadir = args.datadir.canonicalize()?;
    indexer_arc_executor::native::check_start(&args.datadir)?;
    for name in ["db", "rocksdb", "static_files"] {
        ensure!(
            args.datadir.join(name).is_dir(),
            "existing native layout required; no database is created"
        );
    }
    let spec: Arc<ArcChainSpec> = match args.chain {
        Chain::Mainnet => MAINNET.clone(),
        Chain::Testnet => TESTNET.clone(),
    };
    let runtime = Runtime::test();
    let (readonly, _workspace) = read_only_config(&args.datadir, args.scratch_dir.as_deref())?;
    let factory = ProviderFactoryBuilder::<NativeTypes>::default().open_read_only(
        spec.clone(),
        readonly,
        runtime,
    )?;
    ensure!(
        factory.provider()?.block_hash(0)? == Some(spec.inner.genesis_hash()),
        "native genesis differs from selected Arc chain"
    );
    let config = ArcEvmConfig::new(EthEvmConfig::new_with_evm_factory(
        spec.clone(),
        ArcEvmFactory::new(spec.clone()),
    ));
    let replay_block = |height: u64, generation: u64| -> Result<Verified> {
        indexer_arc_executor::history::require(&factory, height - 1)?;
        // Header/body/receipts and the historical parent share one native RO view.
        // No latest-state fallback and no global prune/FinishedHeight calls.
        let provider = factory.provider()?;
        ensure!(
            height <= provider.best_block_number()?,
            "block has not completed native import"
        );
        ensure!(
            provider.block_hash(0)? == Some(spec.inner.genesis_hash()),
            "native genesis differs from selected Arc chain"
        );
        let block = provider
            .block_by_number(height)?
            .ok_or_else(|| eyre!("retained block unavailable"))?;
        ensure!(
            provider.block_hash(height)? == Some(block.header.hash_slow()),
            "block is not canonical in native snapshot"
        );
        let parent = provider
            .sealed_header(height - 1)?
            .ok_or_else(|| eyre!("retained parent unavailable"))?;
        let parent = parent.header().clone().seal_slow();
        let receipts = provider
            .receipts_by_block(block.header.hash_slow().into())?
            .ok_or_else(|| eyre!("retained receipts unavailable"))?;
        let state = provider.history_by_block_hash(parent.hash())?;
        let result = replay(&config, &state, &parent, &block, &receipts, generation)?;
        // Native canonicality is useful early rejection. It does not replace the
        // indexer's final uncached RPC check and post-response generation fence.
        ensure!(
            factory.provider()?.block_hash(height)? == Some(result.result().r#ref.hash),
            "native canonical block changed during replay"
        );
        Ok(result)
    };
    if args.serve {
        return service::serve(
            std::io::stdin().lock(),
            std::io::stdout().lock(),
            |request| match request.op {
                Operation::Identity => Ok(serde_json::to_vec(
                    &serde_json::json!({"chain_id": spec.inner.chain.id().to_string(), "genesis": spec.inner.genesis_hash(), "revision": ARC_REVISION}),
                )?),
                Operation::Head => {
                    let provider = factory.provider()?;
                    let height = provider.best_block_number()?;
                    let hash = provider
                        .block_hash(height)?
                        .ok_or_else(|| eyre!("native head header unavailable"))?;
                    Ok(serde_json::to_vec(
                        &serde_json::json!({"height": height, "hash": hash}),
                    )?)
                }
                Operation::Canonical => {
                    let hash = factory
                        .provider()?
                        .block_hash(request.height)?
                        .ok_or_else(|| eyre!("retained canonical header unavailable"))?;
                    Ok(serde_json::to_vec(
                        &serde_json::json!({"height": request.height, "hash": hash}),
                    )?)
                }
                Operation::Replay => replay_block(request.height, request.generation)?.encode(),
            },
        );
    }
    let height = args.height.ok_or_else(|| eyre!("height required"))?;
    let generation = args
        .generation
        .ok_or_else(|| eyre!("generation required"))?;
    let output = args.output.ok_or_else(|| eyre!("output required"))?;
    let result = replay_block(height, generation)?;
    write_result(&output, &result)?;
    println!(
        "verified Arc {} block {} generation {}: {}",
        spec.inner.chain.id(),
        height,
        generation,
        output.display()
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn native_secondary_workspace_is_private_separate_and_removed_without_source_deletion()
    -> Result<()> {
        let native = tempfile::tempdir()?;
        std::fs::create_dir(native.path().join("rocksdb"))?;
        std::fs::write(native.path().join("rocksdb/KEEP"), b"source")?;
        let (config, work) = read_only_config(native.path(), None)?;
        let path = work.path().to_path_buf();
        assert!(!config.watch);
        assert_eq!(
            std::fs::read_link(&config.rocksdb_dir)?,
            native.path().canonicalize()?.join("rocksdb")
        );
        assert!(config.rocksdb_dir.starts_with(&path));
        drop(work);
        assert!(!path.exists());
        assert_eq!(
            std::fs::read(native.path().join("rocksdb/KEEP"))?,
            b"source"
        );
        Ok(())
    }
    #[test]
    fn arguments_require_explicit_chain_paths_height_and_generation() {
        assert!(Args::try_parse_from(["arc-executor"]).is_err());
        assert!(Args::try_parse_from(["arc-executor", "--chain", "ethereum"]).is_err());
        assert!(
            Args::try_parse_from([
                "arc-executor",
                "--chain",
                "testnet",
                "--datadir",
                "/missing",
                "--serve"
            ])
            .is_ok()
        );
        assert!(
            Args::try_parse_from([
                "arc-executor",
                "--chain",
                "testnet",
                "--datadir",
                "/missing",
                "--serve",
                "--height",
                "1"
            ])
            .is_err()
        );
        let args = Args::try_parse_from([
            "arc-executor",
            "--chain",
            "testnet",
            "--datadir",
            "/missing",
            "--output",
            "/private/block.exr",
            "--height",
            "1",
            "--generation",
            "1",
        ])
        .unwrap();
        assert!(
            produce(args).is_err(),
            "missing native layout must not be initialized"
        );
    }
}
