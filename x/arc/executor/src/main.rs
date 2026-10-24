//! Read one already imported Arc block; never launch a node or modify its DB.
use alloy_primitives::Sealable;
use arc_evm::{ArcEvmConfig, ArcEvmFactory};
use arc_execution_config::chainspec::{ArcChainSpec, MAINNET, TESTNET};
use clap::{Parser, ValueEnum};
use eyre::{Result, ensure, eyre};
use indexer_arc_executor::{replay, write_result};
use reth_ethereum::{
    evm::EthEvmConfig,
    provider::{
        BlockHashReader, BlockReader, EthStorage, HeaderProvider, ReceiptProvider,
        providers::{ProviderFactoryBuilder, ReadOnlyConfig},
    },
    tasks::Runtime,
};
use reth_ethereum_engine_primitives::EthPayloadTypes;
use reth_ethereum_primitives::EthPrimitives;
use reth_node_types::AnyNodeTypes;
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
    #[arg(long, value_parser = clap::value_parser!(u64).range(1..=i64::MAX as u64))]
    height: u64,
    #[arg(long, value_parser = clap::value_parser!(u64).range(1..=i64::MAX as u64))]
    generation: u64,
    #[arg(long)]
    output: PathBuf,
}

type NativeTypes = AnyNodeTypes<EthPrimitives, ArcChainSpec, EthStorage, EthPayloadTypes>;

fn main() -> Result<()> {
    produce(Args::parse())
}

fn produce(args: Args) -> Result<()> {
    ensure!(
        args.datadir.is_absolute() && args.output.is_absolute(),
        "native/output paths must be absolute"
    );
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
    let factory = ProviderFactoryBuilder::<NativeTypes>::default().open_read_only(
        spec.clone(),
        ReadOnlyConfig::from_datadir(&args.datadir),
        runtime,
    )?;
    // Header/body/receipts and the historical parent share one native RO view.
    // No latest-state fallback and no global prune/FinishedHeight calls.
    let provider = factory.provider()?;
    ensure!(
        provider.block_hash(0)? == Some(spec.inner.genesis_hash()),
        "native genesis differs from selected Arc chain"
    );
    let block = provider
        .block_by_number(args.height)?
        .ok_or_else(|| eyre!("retained block unavailable"))?;
    ensure!(
        provider.block_hash(args.height)? == Some(block.header.hash_slow()),
        "block is not canonical in native snapshot"
    );
    let parent = provider
        .sealed_header(args.height - 1)?
        .ok_or_else(|| eyre!("retained parent unavailable"))?;
    let parent = parent.header().clone().seal_slow();
    let receipts = provider
        .receipts_by_block(block.header.hash_slow().into())?
        .ok_or_else(|| eyre!("retained receipts unavailable"))?;
    let state = provider.history_by_block_hash(parent.hash())?;
    let config = ArcEvmConfig::new(EthEvmConfig::new_with_evm_factory(
        spec.clone(),
        ArcEvmFactory::new(spec.clone()),
    ));
    let result = replay(&config, &state, &parent, &block, &receipts, args.generation)?;
    // Native canonicality is useful early rejection. It does not replace the
    // indexer's final uncached RPC check and post-response generation fence.
    ensure!(
        factory.provider()?.block_hash(args.height)? == Some(result.result().r#ref.hash),
        "native canonical block changed during replay"
    );
    write_result(&args.output, &result)?;
    println!(
        "verified Arc {} block {} generation {}: {}",
        spec.inner.chain.id(),
        args.height,
        args.generation,
        args.output.display()
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn arguments_require_explicit_chain_paths_height_and_generation() {
        assert!(Args::try_parse_from(["arc-executor"]).is_err());
        assert!(Args::try_parse_from(["arc-executor", "--chain", "ethereum"]).is_err());
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
