//! Explicit offline build into a NEW native path; never starts node services.
use clap::{Parser, ValueEnum};
use indexer_arc_executor::bootstrap::{self, Inputs};
use std::path::{Path, PathBuf};

#[derive(Clone, Copy, Debug, ValueEnum)]
enum Action {
    Build,
}
#[derive(Parser)]
#[command(
    about = "Build a private Arc testnet native DB from an independently authenticated full checkpoint; no network or node services"
)]
struct Args {
    #[arg(long)]
    action: Action,
    #[arg(long)]
    target: PathBuf,
    #[arg(long)]
    state: PathBuf,
    #[arg(long)]
    expected: PathBuf,
    #[arg(long)]
    headers: Option<PathBuf>,
    #[arg(long)]
    block: Option<PathBuf>,
    #[arg(long)]
    receipts: Option<PathBuf>,
}
fn main() -> eyre::Result<()> {
    let a = Args::parse();
    let m = bootstrap::build(&Inputs {
        target: &a.target,
        state: &a.state,
        expected: &a.expected,
        headers: a.headers.as_deref().unwrap_or(Path::new("")),
        block: a.block.as_deref().unwrap_or(Path::new("")),
        receipts: a.receipts.as_deref().unwrap_or(Path::new("")),
    })?;
    serde_json::to_writer(std::io::stdout().lock(), &m)?;
    Ok(())
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn bootstrap_requires_explicit_action_and_paths() {
        assert!(Args::try_parse_from(["bootstrap"]).is_err());
        assert!(
            Args::try_parse_from([
                "bootstrap",
                "--target",
                "/tmp/new",
                "--state",
                "/tmp/state",
                "--expected",
                "/tmp/expected"
            ])
            .is_err()
        );
        assert!(
            Args::try_parse_from([
                "bootstrap",
                "--action",
                "build",
                "--target",
                "/tmp/new",
                "--state",
                "/tmp/state",
                "--expected",
                "/tmp/expected"
            ])
            .is_ok()
        );
        assert!(Args::try_parse_from(["bootstrap", "--action", "run"]).is_err());
    }
}
