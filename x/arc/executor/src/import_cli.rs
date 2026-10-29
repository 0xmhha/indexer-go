//! Explicit owned Arc testnet first-execution writer; no node or RPC services.
use clap::Parser;
use std::path::PathBuf;

#[derive(Parser)]
#[command(
    about = "Owned Arc testnet native import on bounded stdin/stdout; no node, network or SQL"
)]
struct Args {
    #[arg(long)]
    datadir: PathBuf,
    #[arg(long, required = true)]
    serve: bool,
}
fn main() -> eyre::Result<()> {
    let args = Args::parse();
    indexer_arc_executor::native_writer::serve(
        std::io::stdin().lock(),
        std::io::stdout().lock(),
        &args.datadir,
    )
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn import_requires_explicit_serve_and_native_path() {
        assert!(Args::try_parse_from(["import"]).is_err());
        assert!(Args::try_parse_from(["import", "--datadir", "/private/native"]).is_err());
        assert!(
            Args::try_parse_from(["import", "--serve", "--datadir", "/private/native"]).is_ok()
        );
        assert!(
            Args::try_parse_from([
                "import",
                "--serve",
                "--datadir",
                "/private/native",
                "--rpc",
                "https://example.invalid"
            ])
            .is_err()
        );
    }
}
