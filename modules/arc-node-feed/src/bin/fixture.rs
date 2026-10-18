//! Offline interoperability fixture; not a chain-synchronizing node.
use alloy_primitives::{Address, B256, U256};
use eyre::{Result, eyre};
use indexer_arc_node_feed::{Bundle, Config, Provider};
use serde::Deserialize;
use std::{collections::HashMap, path::PathBuf};
#[derive(Deserialize)]
struct Fixture {
    chain_id: u64,
    blocks: Vec<Bundle>,
    states: HashMap<B256, HashMap<Address, U256>>,
    #[serde(default)]
    strict_state: bool,
}
impl Provider for Fixture {
    fn head(&self) -> Result<u64> {
        self.blocks
            .iter()
            .map(|b| b.height)
            .max()
            .ok_or_else(|| eyre!("empty fixture"))
    }
    fn hash(&self, n: u64) -> Result<B256> {
        self.blocks
            .iter()
            .find(|b| b.height == n)
            .map(|b| b.hash)
            .ok_or_else(|| eyre!("block not found: {n}"))
    }
    fn block(&self, n: u64) -> Result<Bundle> {
        self.blocks
            .iter()
            .find(|b| b.height == n)
            .cloned()
            .ok_or_else(|| eyre!("block not found: {n}"))
    }
    fn balances(&self, hash: B256, addresses: &[Address]) -> Result<Vec<U256>> {
        let state = self
            .states
            .get(&hash)
            .ok_or_else(|| eyre!("historical state unavailable"))?;
        addresses
            .iter()
            .map(|a| match state.get(a) {
                Some(value) => Ok(*value),
                None if self.strict_state => Err(eyre!("unrecorded state: {hash} {a}")),
                None => Ok(U256::ZERO),
            })
            .collect()
    }
}
#[tokio::main]
async fn main() -> Result<()> {
    let args: Vec<_> = std::env::args().collect();
    if args.len() != 3 {
        return Err(eyre!("usage: fixture SOCKET FIXTURE_JSON"));
    }
    let fixture: Fixture = serde_json::from_slice(&std::fs::read(&args[2])?)?;
    let config = Config::new(PathBuf::from(&args[1]), fixture.chain_id);
    tokio::select! { result=indexer_arc_node_feed::serve(fixture,config,|_,_|Ok(()))=>result, result=tokio::signal::ctrl_c()=>{result?;Ok(())} }
}
