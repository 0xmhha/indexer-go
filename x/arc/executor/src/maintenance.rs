//! Explicit offline native-library commands. No node startup or RPC clients.
use arc_execution_config::chainspec::{ArcChainSpec, MAINNET, TESTNET};
use clap::{Parser, ValueEnum};
use eyre::{Result, ensure, eyre};
use indexer_arc_executor::{checkpoint, native, retention};
use std::{
    io::{BufRead, Read, Write},
    path::PathBuf,
    sync::Arc,
};

#[derive(Clone, Copy, Debug, ValueEnum)]
enum Chain {
    Mainnet,
    Testnet,
}
#[derive(Clone, Copy, Debug, ValueEnum)]
enum Operation {
    Checkpoint,
    Inspect,
    Hold,
    Prune,
    Verify,
    MatchBackup,
}
#[derive(Parser)]
#[command(
    about = "Offline Arc native full checkpoint and supervised retention adapter; no node services"
)]
struct Args {
    #[arg(long)]
    chain: Chain,
    #[arg(long)]
    datadir: PathBuf,
    #[arg(long)]
    operation: Operation,
    #[arg(long)]
    height: Option<u64>,
    #[arg(long)]
    output: Option<PathBuf>,
    #[arg(long)]
    seed: Option<PathBuf>,
    #[arg(long)]
    scratch_dir: Option<PathBuf>,
}
fn read_input<T: serde::de::DeserializeOwned>(r: &mut impl BufRead) -> Result<T> {
    let mut line = Vec::new();
    r.take((2 << 20) + 1).read_until(b'\n', &mut line)?;
    ensure!(
        line.len() <= 2 << 20 && line.ends_with(b"\n"),
        "bounded native maintenance request required"
    );
    Ok(serde_json::from_slice(&line)?)
}
fn main() -> Result<()> {
    let mut a = Args::parse();
    a.datadir = a.datadir.canonicalize()?;
    native::check_legacy_maintenance(&a.datadir)?;
    native::layout(&a.datadir)?;
    let spec: Arc<ArcChainSpec> = match a.chain {
        Chain::Mainnet => MAINNET.clone(),
        Chain::Testnet => TESTNET.clone(),
    };
    let mut input = std::io::stdin().lock();
    match a.operation {
        Operation::Checkpoint => {
            native::check_start(&a.datadir)?;
            let height = a
                .height
                .ok_or_else(|| eyre!("explicit checkpoint height required"))?;
            let output = a
                .output
                .ok_or_else(|| eyre!("checkpoint output required"))?;
            let canonical = a.datadir.canonicalize()?;
            let parent = output
                .parent()
                .ok_or_else(|| eyre!("output parent"))?
                .canonicalize()?;
            ensure!(
                !parent.starts_with(&canonical) && !canonical.starts_with(&parent),
                "checkpoint output must be separate from native storage"
            );
            let (f, _work) =
                native::open_readonly_in(&a.datadir, spec.clone(), true, a.scratch_dir.as_deref())?;
            let retained = retention::retained_seed(&a.datadir);
            native::export_checkpoint(
                &f,
                &spec,
                height,
                a.seed.as_deref().or(retained.as_deref()),
                &output,
                a.scratch_dir.as_deref().unwrap_or(&parent),
            )?;
            println!("{{\"Status\":\"full_checkpoint_exported\"}}");
        }
        Operation::Inspect | Operation::Hold => {
            ensure!(
                a.height.is_none() && a.output.is_none() && a.seed.is_none(),
                "unexpected maintenance export arguments"
            );
            let r: retention::Request = read_input(&mut input)?;
            if matches!(a.operation, Operation::Inspect) {
                let mut extra = [0u8; 1];
                ensure!(input.read(&mut extra)? == 0, "extra maintenance input");
            }
            let (f, _work) =
                native::open_readonly_in(&a.datadir, spec.clone(), true, a.scratch_dir.as_deref())?;
            let observed = retention::inspect(&f, &spec, &r)?;
            serde_json::to_writer(std::io::stdout().lock(), &observed)?;
            println!();
            std::io::stdout().flush()?;
            if matches!(a.operation, Operation::Hold) {
                let mut extra = [0u8; 1];
                ensure!(input.read(&mut extra)? == 0, "extra hold input");
            }
        }
        Operation::MatchBackup => {
            ensure!(
                a.height.is_none() && a.output.is_none() && a.seed.is_none(),
                "unexpected backup arguments"
            );
            let b: retention::BackupApply = read_input(&mut input)?;
            let mut extra = [0u8; 1];
            ensure!(input.read(&mut extra)? == 0, "extra backup input");
            let (f, work) =
                native::open_readonly_in(&a.datadir, spec.clone(), true, a.scratch_dir.as_deref())?;
            retention::verify_backup(&f, &spec, &a.datadir, &b, work.path())?;
            println!("{{\"Status\":\"native_verified\"}}");
        }
        Operation::Prune | Operation::Verify => {
            ensure!(
                a.height.is_none() && a.output.is_none() && a.seed.is_none(),
                "unexpected maintenance export arguments"
            );
            let apply: retention::Apply = read_input(&mut input)?;
            apply.request.validate()?;
            let mut extra = [0u8; 1];
            ensure!(input.read(&mut extra)? == 0, "extra maintenance input");
            if matches!(a.operation, Operation::Prune) {
                retention::candidate(&a.datadir, &apply)?;
                checkpoint::private_file(&a.datadir.join(".execution-retention-copy"))?;
                let f = native::open_writable(&a.datadir, spec.clone())?;
                let work = if let Some(parent) = a.scratch_dir.as_deref() {
                    indexer_arc_executor::catalog::private_dir(parent)?;
                    tempfile::Builder::new()
                        .prefix("maintenance-sort-")
                        .tempdir_in(parent)?
                } else {
                    tempfile::tempdir()?
                };
                use std::os::unix::fs::PermissionsExt;
                std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
                retention::prune(&f, &spec, &a.datadir, &apply, work.path())?;
                drop(f);
                retention::compact_mdbx(&a.datadir, &apply)?;
                let (f, _secondary) = native::open_readonly_in(
                    &a.datadir,
                    spec.clone(),
                    true,
                    a.scratch_dir.as_deref(),
                )?;
                retention::verify(&f, &spec, &a.datadir, &apply, work.path())?;
            } else {
                let (f, _work) = native::open_readonly_in(
                    &a.datadir,
                    spec.clone(),
                    true,
                    a.scratch_dir.as_deref(),
                )?;
                retention::verify(&f, &spec, &a.datadir, &apply, _work.path())?;
            }
            println!("{{\"Status\":\"native_verified\"}}");
        }
    }
    Ok(())
}
