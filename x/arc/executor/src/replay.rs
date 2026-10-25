use crate::{
    journal::{Attribution, Journal, MAX_CHANGES},
    wire,
};
use alloy_consensus::{Header, Sealed, Transaction, TxReceipt, proofs};
use alloy_eips::eip2718::Encodable2718;
use alloy_primitives::{Address, B256, KECCAK256_EMPTY, U256, keccak256};
use alloy_rlp::Encodable;
use arc_evm::ArcEvmConfig;
use eyre::{Result, ensure, eyre};
use reth_ethereum_primitives::{Block, Receipt};
use reth_evm::{ConfigureEvm, Evm, block::BlockExecutor};
use reth_primitives_traits::RecoveredBlock;
use reth_revm::database::EvmStateProvider;
use reth_storage_api::errors::provider::ProviderError;
use reth_storage_api::{StateProvider, StateRootProvider, StorageRootProvider};
use reth_trie::{HashedPostState, HashedStorage};
use revm::{
    Database,
    database::{State, states::bundle_state::BundleRetention},
    state::{AccountInfo, EvmState},
};
use std::{
    collections::BTreeSet,
    io::Write,
    sync::{Arc, Mutex},
};

/// Production implementations delegate roots to the exact historical native
/// state provider. An unavailable parent is an error, never latest-state fallback.
pub trait ReplayState: EvmStateProvider {
    fn state_root(&self, updates: HashedPostState) -> Result<B256>;
    fn storage_root(&self, address: Address, updates: HashedStorage) -> Result<B256>;
}

impl<T: StateProvider> ReplayState for T {
    fn state_root(&self, updates: HashedPostState) -> Result<B256> {
        Ok(StateRootProvider::state_root(self, updates)?)
    }
    fn storage_root(&self, address: Address, updates: HashedStorage) -> Result<B256> {
        Ok(StorageRootProvider::storage_root(self, address, updates)?)
    }
}

/// Constructible only after native and journal validation, never from wire input.
pub struct Verified(wire::Result);
impl Verified {
    pub fn result(&self) -> &wire::Result {
        &self.0
    }
    pub fn encode(&self) -> Result<Vec<u8>> {
        let mut meta = BoundedVec {
            data: vec![],
            limit: 16 << 20,
        };
        serde_json::to_writer(&mut meta, &self.0)?;
        let value: serde_json::Value = serde_json::from_slice(&meta.data)?;
        ensure!(tokens(&value) <= 262_144, "metadata token limit");
        let parts = [&meta.data, &self.0.block_rlp, &self.0.receipts_rlp];
        let total = 16 + parts.iter().map(|p| p.len()).sum::<usize>();
        ensure!(total <= 64 << 20, "result size limit");
        let mut out = Vec::with_capacity(total);
        out.extend_from_slice(b"EXR1");
        for p in parts {
            out.extend_from_slice(&(p.len() as u32).to_be_bytes());
        }
        for p in parts {
            out.extend_from_slice(p);
        }
        Ok(out)
    }
}

pub fn replay<S: ReplayState>(
    config: &ArcEvmConfig,
    state: &S,
    parent: &Sealed<Header>,
    block: &Block,
    receipts: &[Receipt],
    generation: u64,
) -> Result<Verified> {
    let h = &block.header;
    ensure!(
        generation > 0
            && generation <= i64::MAX as u64
            && h.number > 0
            && h.number <= i64::MAX as u64
            && h.timestamp <= i64::MAX as u64
            && parent.number.checked_add(1) == Some(h.number)
            && parent.hash() == h.parent_hash
            && parent.hash() == parent.inner().hash_slow(),
        "invalid parent/ref"
    );
    ensure!(
        block.body.transactions.len() <= MAX_CHANGES
            && receipts.len() == block.body.transactions.len(),
        "transaction/receipt count"
    );
    ensure!(
        block.length() <= 64 << 20
            && proofs::calculate_transaction_root(&block.body.transactions) == h.transactions_root
            && proofs::calculate_ommers_root(&block.body.ommers) == h.ommers_hash,
        "body commitment"
    );
    ensure!(
        match (&block.body.withdrawals, h.withdrawals_root) {
            (Some(w), Some(root)) => proofs::calculate_withdrawals_root(w) == root,
            (None, None) => true,
            _ => false,
        },
        "withdrawals commitment"
    );
    ensure!(
        state.state_root(HashedPostState::default())? == parent.state_root,
        "parent state root mismatch"
    );
    let expected_receipts = receipts
        .iter()
        .map(|r| r.with_bloom_ref())
        .collect::<Vec<_>>();
    let mut bloom = alloy_primitives::Bloom::ZERO;
    let mut receipt_bytes = 0;
    let mut log_count = 0;
    for r in &expected_receipts {
        bloom |= r.logs_bloom;
        receipt_bytes += r.network_len();
        log_count += r.logs().len();
        ensure!(
            receipt_bytes <= 64 << 20 && log_count <= 262_144,
            "receipt resource limit"
        );
    }
    ensure!(bloom == h.logs_bloom, "receipt bloom mismatch");
    ensure!(
        proofs::calculate_receipt_root(&expected_receipts) == h.receipts_root,
        "stored receipt root mismatch"
    );
    let recovered = RecoveredBlock::try_recover(block.clone())?;
    let mut db = State::builder()
        .with_database(StrictDB(state))
        .with_bundle_update()
        .build();
    let observer = crate::calls::Observer::default();
    let calls = observer.0.clone();
    let evm = config.evm_with_env_and_inspector(&mut db, config.evm_env(h)?, observer);
    let mut executor = config.create_executor(evm, config.context_for_block(&recovered)?);
    let cleared: Arc<Mutex<BTreeSet<Address>>> = Arc::default();
    let tracking = cleared.clone();
    executor.set_state_hook(Some(Box::new(move |_, changes: &EvmState| {
        // This is a committed-state hook, not an opcode trace: reverted inner
        // calls do not create deletion markers. Poison is checked by take_clear.
        if let Ok(mut set) = tracking.lock() {
            for (address, account) in changes {
                if account.is_selfdestructed() {
                    set.insert(*address);
                }
            }
        }
    })));
    let mut journal = Journal::new(state);
    let mut steps = Vec::with_capacity(receipts.len() + 2);
    executor.apply_pre_execution_changes()?;
    executor
        .evm_mut()
        .db_mut()
        .merge_transitions(BundleRetention::Reverts);
    steps.push(journal.capture(
        &executor.evm().db().bundle_state,
        &take_clear(&cleared)?,
        None,
        Attribution {
            ordinal: 0,
            cause: wire::PRE_BLOCK,
            tx_index: -1,
            tx_hash: B256::ZERO,
        },
    )?);
    for (index, tx) in recovered.transactions_recovered().enumerate() {
        calls
            .lock()
            .map_err(|_| eyre!("call observer poisoned"))?
            .begin(index as u32, *tx.tx_hash())?;
        let previous = if index == 0 {
            0
        } else {
            receipts[index - 1].cumulative_gas_used()
        };
        let used = receipts[index]
            .cumulative_gas_used()
            .checked_sub(previous)
            .ok_or_else(|| eyre!("receipt gas order"))?;
        let physical =
            crate::calls::root_frame(tx.signer(), tx.to(), tx.value(), tx.gas_limit(), tx.input())?;
        let mut trace_result = None;
        executor.execute_transaction_with_result_closure(&tx, |out| {
            trace_result = Some((|| -> Result<()> {
                let native = &out.result.result;
                ensure!(
                    native.is_success() == receipts[index].status(),
                    "native call result/receipt status"
                );
                let error = match native {
                    revm::context::result::ExecutionResult::Success { .. } => String::new(),
                    revm::context::result::ExecutionResult::Revert { .. } => "Revert".into(),
                    revm::context::result::ExecutionResult::Halt { reason, .. } => {
                        format!("{reason:?}")
                    }
                };
                let mut c = calls.lock().map_err(|_| eyre!("call observer poisoned"))?;
                c.finish_root(physical, native.output().map_or(&[], |b| b.as_ref()), error)?;
                c.end(tx.gas_limit(), used, native.is_success())
            })());
        })?;
        trace_result.ok_or_else(|| eyre!("missing native call result"))??;
        let forced = if tx.to().is_none() && executor.receipts().last().is_some_and(|r| r.status())
        {
            Some(tx.signer().create(tx.nonce()))
        } else {
            None
        };
        executor
            .evm_mut()
            .db_mut()
            .merge_transitions(BundleRetention::Reverts);
        steps.push(journal.capture(
            &executor.evm().db().bundle_state,
            &take_clear(&cleared)?,
            forced,
            Attribution {
                ordinal: steps.len() as u32,
                cause: wire::TRANSACTION,
                tx_index: index as i32,
                tx_hash: *tx.tx_hash(),
            },
        )?);
    }
    let (mut evm, output) = executor.finish()?;
    evm.db_mut().merge_transitions(BundleRetention::Reverts);
    steps.push(journal.capture(
        &evm.db().bundle_state,
        &take_clear(&cleared)?,
        None,
        Attribution {
            ordinal: steps.len() as u32,
            cause: wire::FINALIZE,
            tx_index: -1,
            tx_hash: B256::ZERO,
        },
    )?);
    ensure!(
        output.gas_used == h.gas_used
            && output.receipts == receipts
            && state.state_root(HashedPostState::from_bundle_state::<
                reth_trie::KeccakKeyHasher,
            >(evm.db().bundle_state.state.iter()))?
                == h.state_root,
        "native replay state/receipts/gas mismatch"
    );
    let mut encoded = vec![];
    for receipt in expected_receipts {
        receipt.network_encode(&mut encoded);
    }
    let mut receipts_rlp = vec![];
    alloy_rlp::Header {
        list: true,
        payload_length: encoded.len(),
    }
    .encode(&mut receipts_rlp);
    receipts_rlp.extend_from_slice(&encoded);
    let verified = Verified(wire::Result {
        r#ref: wire::Ref {
            generation,
            height: h.number,
            hash: h.hash_slow(),
        },
        parent: h.parent_hash,
        time: h.timestamp,
        state_root: h.state_root,
        receipts_root: h.receipts_root,
        tx_count: receipts.len() as u32,
        block_rlp: alloy_rlp::encode(block),
        receipts_rlp,
        steps,
        call_traces: Some(
            calls
                .lock()
                .map_err(|_| eyre!("call observer poisoned"))?
                .take()?,
        ),
    });
    // No result escapes without all transport bounds and completeness checks.
    verified.encode()?;
    Ok(verified)
}

fn take_clear(set: &Mutex<BTreeSet<Address>>) -> Result<BTreeSet<Address>> {
    Ok(std::mem::take(
        &mut *set.lock().map_err(|_| eyre!("state hook poisoned"))?,
    ))
}

struct StrictDB<'a, S>(&'a S);
impl<S> std::fmt::Debug for StrictDB<'_, S> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("StrictDB")
    }
}
impl<S: ReplayState> Database for StrictDB<'_, S> {
    type Error = ProviderError;
    fn basic(&mut self, address: Address) -> std::result::Result<Option<AccountInfo>, Self::Error> {
        Ok(self.0.basic_account(&address)?.map(Into::into))
    }
    fn storage(&mut self, address: Address, key: U256) -> std::result::Result<U256, Self::Error> {
        Ok(self
            .0
            .storage(address, B256::from(key))?
            .unwrap_or_default())
    }
    fn block_hash(&mut self, number: u64) -> std::result::Result<B256, Self::Error> {
        self.0.block_hash(number)?.ok_or_else(|| {
            ProviderError::other(std::io::Error::other("retained block hash unavailable"))
        })
    }
    fn code_by_hash(
        &mut self,
        hash: B256,
    ) -> std::result::Result<revm::bytecode::Bytecode, Self::Error> {
        if hash == KECCAK256_EMPTY {
            return Ok(revm::bytecode::Bytecode::default());
        }
        let code = self.0.bytecode_by_hash(&hash)?.ok_or_else(|| {
            ProviderError::other(std::io::Error::other("retained bytecode unavailable"))
        })?;
        if keccak256(code.original_bytes()) != hash {
            return Err(ProviderError::other(std::io::Error::other(
                "retained bytecode hash mismatch",
            )));
        }
        Ok(code.0)
    }
}

struct BoundedVec {
    data: Vec<u8>,
    limit: usize,
}
impl Write for BoundedVec {
    fn write(&mut self, data: &[u8]) -> std::io::Result<usize> {
        if data.len() > self.limit.saturating_sub(self.data.len()) {
            return Err(std::io::Error::other("metadata size limit"));
        }
        self.data.extend_from_slice(data);
        Ok(data.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}
fn tokens(v: &serde_json::Value) -> usize {
    match v {
        serde_json::Value::Object(map) => 2 + map.values().map(|v| 1 + tokens(v)).sum::<usize>(),
        serde_json::Value::Array(items) => 2 + items.iter().map(tokens).sum::<usize>(),
        _ => 1,
    }
}
