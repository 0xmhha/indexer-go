use super::*;
use alloy_consensus::{BlockBody, Header, SignableTransaction, TxLegacy, TxReceipt, proofs};
use alloy_primitives::{Address, B256, Bytes, Sealable, Signature, TxKind, U256, keccak256};
use alloy_trie::{TrieAccount, root};
use arc_evm::{ArcEvmConfig, ArcEvmFactory};
use arc_execution_config::chainspec::LOCAL_DEV;
use eyre::Result;
use reth_ethereum::evm::EthEvmConfig;
use reth_ethereum_primitives::{Block, Receipt};
use reth_evm::{ConfigureEvm, execute::Executor};
use reth_primitives_traits::{Account, Bytecode, RecoveredBlock, SignerRecoverable};
use reth_revm::database::EvmStateProvider;
use reth_storage_api::errors::provider::ProviderResult;
use reth_trie::{HashedPostState, HashedStorage};
use std::collections::BTreeMap;

#[derive(Clone, Default)]
struct FullState {
    accounts: BTreeMap<Address, Account>,
    storage: BTreeMap<(Address, B256), U256>,
    code: BTreeMap<B256, Bytes>,
    hashes: BTreeMap<u64, B256>,
}

impl EvmStateProvider for FullState {
    fn basic_account(&self, address: &Address) -> ProviderResult<Option<Account>> {
        Ok(self.accounts.get(address).copied())
    }
    fn block_hash(&self, number: u64) -> ProviderResult<Option<B256>> {
        Ok(self.hashes.get(&number).copied())
    }
    fn bytecode_by_hash(&self, hash: &B256) -> ProviderResult<Option<Bytecode>> {
        Ok(self
            .code
            .get(hash)
            .map(|b| Bytecode(revm::bytecode::Bytecode::new_raw(b.clone()))))
    }
    fn storage(&self, address: Address, key: B256) -> ProviderResult<Option<U256>> {
        Ok(self.storage.get(&(address, key)).copied())
    }
}

// Test oracle rebuilds the full trie from genesis data and hashed updates. It
// does not use the journal builder or the native bundle-to-journal conversion.
impl ReplayState for FullState {
    fn storage_root(&self, address: Address, updates: HashedStorage) -> Result<B256> {
        let mut values: BTreeMap<B256, U256> = if updates.wiped {
            BTreeMap::new()
        } else {
            self.storage
                .iter()
                .filter(|((a, _), _)| *a == address)
                .map(|((_, slot), value)| (keccak256(slot), *value))
                .collect()
        };
        values.extend(updates.storage);
        Ok(root::storage_root(
            values.into_iter().filter(|(_, v)| !v.is_zero()),
        ))
    }
    fn state_root(&self, updates: HashedPostState) -> Result<B256> {
        let mut values: BTreeMap<B256, TrieAccount> = self
            .accounts
            .iter()
            .map(|(address, a)| {
                let storage_root = self.storage_root(
                    *address,
                    updates
                        .storages
                        .get(&keccak256(address))
                        .cloned()
                        .unwrap_or_default(),
                )?;
                Ok((
                    keccak256(address),
                    TrieAccount {
                        nonce: a.nonce,
                        balance: a.balance,
                        storage_root,
                        code_hash: a.get_bytecode_hash(),
                    },
                ))
            })
            .collect::<Result<_>>()?;
        for (hash, account) in updates.accounts {
            if let Some(a) = account {
                // All newly created addresses are known to this offline fixture.
                let address = self.accounts.keys().find(|a| keccak256(a) == hash);
                let storage_root = match address {
                    Some(a) => self.storage_root(
                        *a,
                        updates.storages.get(&hash).cloned().unwrap_or_default(),
                    )?,
                    None => root::storage_root_unsorted(
                        updates
                            .storages
                            .get(&hash)
                            .cloned()
                            .unwrap_or_default()
                            .storage
                            .into_iter()
                            .filter(|(_, v)| !v.is_zero()),
                    ),
                };
                values.insert(
                    hash,
                    TrieAccount {
                        nonce: a.nonce,
                        balance: a.balance,
                        storage_root,
                        code_hash: a.get_bytecode_hash(),
                    },
                );
            } else {
                values.remove(&hash);
            }
        }
        Ok(root::state_root(values))
    }
}

type Imported = (
    ArcEvmConfig,
    FullState,
    alloy_consensus::Sealed<Header>,
    Block,
    Vec<Receipt>,
);

fn imported() -> Result<Imported> {
    imported_case("empty")
}

#[test]
fn native_replay_exports_transaction_call_frames() -> Result<()> {
    let (config, state, parent, block, receipts) = imported_case("storage")?;
    let result = replay(&config, &state, &parent, &block, &receipts, 1)?;
    let metadata = serde_json::to_value(result.result())?;
    let calls = metadata
        .get("CallTraces")
        .expect("same replay must record call traces");
    assert_eq!(calls["Version"], 1);
    assert_eq!(calls["Frames"].as_array().unwrap().len(), 1);
    assert_eq!(calls["Frames"][0]["Parent"], -1);
    assert_eq!(calls["Frames"][0]["Type"], "CALL");
    Ok(())
}

#[test]
fn native_checkpoint_catalog_beyond_one_million() -> Result<()> {
    use crate::catalog::{Builder, Key};
    use std::os::unix::fs::PermissionsExt;
    let root = tempfile::tempdir()?;
    std::fs::set_permissions(root.path(), std::fs::Permissions::from_mode(0o700))?;
    let mut keys = Builder::new(root.path())?;
    for n in 0..1_000_001u64 {
        let mut address = [0u8; 20];
        address[12..].copy_from_slice(&n.to_be_bytes());
        keys.push(Key::account(Address::from(address)))?;
    }
    let keys = keys.finish()?;
    let mut reader = keys.reader()?;
    let mut count = 0;
    while reader.next_key()?.is_some() {
        count += 1;
    }
    assert_eq!(count, 1_000_001);
    Ok(())
}

#[test]
fn native_checkpoint_full_root_and_missing_key_coverage() -> Result<()> {
    let (_, state, _, _, _) = imported()?;
    let parent = LOCAL_DEV.inner.genesis_header();
    let mut keys = checkpoint::Keys::new();
    for address in state.accounts.keys() {
        keys.entry(*address).or_default();
    }
    for (address, slot) in state.storage.keys() {
        keys.entry(*address).or_default().insert(*slot);
    }
    let mut stream = Vec::new();
    checkpoint::export(
        &mut stream,
        "1337",
        LOCAL_DEV.inner.genesis_hash(),
        parent,
        &state,
        &keys,
    )?;
    assert!(stream.ends_with(b"{\"Kind\":\"end\"}\n"));
    if let Ok(dir) = std::env::var("INDEXER_ARC_FIXTURE_DIR") {
        std::fs::create_dir_all(&dir)?;
        std::fs::write(
            std::path::Path::new(&dir).join("arc-native-checkpoint.ndjson"),
            &stream,
        )?;
    }
    let address = *keys.keys().next().unwrap();
    keys.remove(&address);
    assert!(
        checkpoint::export(
            Vec::new(),
            "1337",
            LOCAL_DEV.inner.genesis_hash(),
            parent,
            &state,
            &keys
        )
        .is_err()
    );
    Ok(())
}

fn imported_case(case: &str) -> Result<Imported> {
    Ok(imported_with_end(case)?.0)
}
fn imported_with_end(case: &str) -> Result<(Imported, FullState)> {
    let spec = LOCAL_DEV.clone();
    let config = ArcEvmConfig::new(EthEvmConfig::new_with_evm_factory(
        spec.clone(),
        ArcEvmFactory::new(spec.clone()),
    ));
    let mut state = FullState::default();
    for (address, alloc) in &spec.inner.genesis.alloc {
        let code_hash = alloc.code.as_ref().filter(|b| !b.is_empty()).map(keccak256);
        if let Some(hash) = code_hash {
            state.code.insert(hash, alloc.code.clone().unwrap());
        }
        state.accounts.insert(
            *address,
            Account {
                nonce: alloc.nonce.unwrap_or_default(),
                balance: alloc.balance,
                bytecode_hash: code_hash,
            },
        );
        for (slot, value) in alloc.storage.clone().unwrap_or_default() {
            state
                .storage
                .insert((*address, slot), U256::from_be_bytes(value.0));
        }
    }
    let mut transactions = vec![];
    if case != "empty" {
        let recipient = if case == "call-from-direct" {
            subcall_address()
        } else if case.starts_with("call-from") {
            arc_execution_config::call_from::MEMO_ADDRESS
        } else {
            Address::repeat_byte(0x23)
        };
        let code: Option<Bytes> = match case {
            "call-from-oog" | "call-from-direct" => {
                let helper = Address::repeat_byte(0x24);
                let helper_code: Bytes =
                    vec![0x60, 9, 0x5f, 0x55, 0x61, 0x80, 0x00, 0x5f, 0xf3].into();
                let hash = keccak256(&helper_code);
                state.accounts.insert(
                    helper,
                    Account {
                        nonce: 1,
                        balance: U256::ZERO,
                        bytecode_hash: Some(hash),
                    },
                );
                state.code.insert(hash, helper_code);
                state.storage.insert((helper, B256::ZERO), U256::from(7));
                if case == "call-from-direct" {
                    None
                } else {
                    let mut code = vec![0x36, 0x5f, 0x5f, 0x37, 0x5f, 0x5f, 0x36, 0x5f, 0x5f, 0x73];
                    code.extend_from_slice(subcall_address().as_slice());
                    // The child can return 32KiB, but the CallFrom ABI-copy
                    // completion cannot fit. Retain the physical CALL flag.
                    code.extend([0x61, 0x38, 0xa4, 0xf1, 0x5f, 0x52, 0x60, 0x20, 0x5f, 0xf3]);
                    Some(code.into())
                }
            }
            "storage" => Some(vec![0x60, 9, 0x5f, 0x55, 0x00].into()),
            "revert" => Some(vec![0x60, 9, 0x5f, 0x55, 0x5f, 0x5f, 0xfd].into()),
            "calls" => {
                let helper = Address::repeat_byte(0x24);
                let doomed = Address::repeat_byte(0x25);
                let helper_code: Bytes = [call_code(0xfa, Address::repeat_byte(0x26)), vec![0]]
                    .concat()
                    .into();
                let doomed_code: Bytes = [vec![0x73], recipient.as_slice().to_vec(), vec![0xff]]
                    .concat()
                    .into();
                for (a, code) in [(helper, helper_code), (doomed, doomed_code)] {
                    let hash = keccak256(&code);
                    state.accounts.insert(
                        a,
                        Account {
                            nonce: 1,
                            balance: U256::from(2),
                            bytecode_hash: Some(hash),
                        },
                    );
                    state.code.insert(hash, code);
                }
                let mut code = vec![];
                for op in [0xf1, 0xf4, 0xf2, 0xfa] {
                    code.extend(call_code(op, helper));
                }
                // CREATE(empty init), CREATE2(empty init,salt0), then a child
                // SELFDESTRUCT. All succeed before the parent reverts.
                code.extend([
                    0x5f, 0x5f, 0x5f, 0xf0, 0x50, 0x5f, 0x5f, 0x5f, 0x5f, 0xf5, 0x50,
                ]);
                code.extend(call_code(0xf1, doomed));
                code.extend([0x5f, 0x5f, 0xfd]);
                Some(code.into())
            }
            _ => None,
        };
        let hash = code.as_ref().map(keccak256);
        state.accounts.insert(
            recipient,
            Account {
                nonce: 1,
                balance: U256::from(10),
                bytecode_hash: hash,
            },
        );
        if let Some(code) = code {
            state.code.insert(hash.unwrap(), code);
            state.storage.insert((recipient, B256::ZERO), U256::from(7));
        }
        let tx: reth_ethereum_primitives::TransactionSigned = TxLegacy {
            chain_id: Some(1337),
            nonce: 0,
            gas_price: 21_000_000_000,
            gas_limit: if case == "calls" || case.starts_with("call-from") {
                1_000_000
            } else {
                100_000
            },
            to: if case.starts_with("create") {
                TxKind::Create
            } else {
                TxKind::Call(recipient)
            },
            value: if case.starts_with("call-from") {
                U256::ZERO
            } else {
                U256::from(100)
            },
            input: match case {
                "call-from-oog" | "call-from-direct" => {
                    let mut data =
                        keccak256("callFrom(address,address,bytes)").as_slice()[..4].to_vec();
                    let sender = if case == "call-from-oog" {
                        recipient
                    } else {
                        Address::repeat_byte(0x44)
                    };
                    for word in [
                        sender.into_word(),
                        Address::repeat_byte(0x24).into_word(),
                        B256::from(U256::from(96)),
                        B256::ZERO,
                    ] {
                        data.extend_from_slice(word.as_slice());
                    }
                    data.into()
                }
                "create" => vec![0x5f, 0x5f, 0xf3].into(),
                "create-code" => {
                    vec![0x60, 1, 0x60, 12, 0x60, 0, 0x39, 0x60, 1, 0x60, 0, 0xf3, 0].into()
                }
                "create-gone" => {
                    let mut code = vec![0x73];
                    code.extend_from_slice(recipient.as_slice());
                    code.push(0xff);
                    code.into()
                }
                _ => Bytes::new(),
            },
        }
        .into_signed(Signature::test_signature())
        .into();
        let sender = tx.recover_signer()?;
        state.accounts.insert(
            sender,
            Account {
                nonce: 0,
                balance: U256::from(10).pow(U256::from(24)),
                bytecode_hash: None,
            },
        );
        transactions.push(tx);
    }
    let root = state.state_root(HashedPostState::default())?;
    let parent = Header {
        state_root: root,
        gas_limit: 30_000_000,
        ..Default::default()
    }
    .seal_slow();
    state.hashes.insert(0, parent.hash());
    let mut block = Block {
        header: Header {
            parent_hash: parent.hash(),
            number: 1,
            timestamp: 1,
            gas_limit: 30_000_000,
            base_fee_per_gas: Some(20_000_000_000),
            withdrawals_root: Some(alloy_trie::EMPTY_ROOT_HASH),
            blob_gas_used: Some(0),
            excess_blob_gas: Some(0),
            parent_beacon_block_root: Some(B256::ZERO),
            beneficiary: Address::repeat_byte(0x17),
            ..Default::default()
        },
        body: BlockBody {
            transactions,
            ommers: vec![],
            withdrawals: Some(Default::default()),
        },
    };
    let recovered = RecoveredBlock::try_recover(block.clone())?;
    let mut executor = config.executor(reth_revm::database::StateProviderDatabase::new(
        state.clone(),
    ));
    let output = executor.execute_one(&recovered)?;
    let db = executor.into_state();
    let hashed = HashedPostState::from_bundle_state::<reth_trie::KeccakKeyHasher>(
        db.bundle_state.state.iter(),
    );
    block.header.state_root = state.state_root(hashed)?;
    block.header.receipts_root = proofs::calculate_receipt_root(
        &output
            .receipts
            .iter()
            .map(|r| r.with_bloom_ref())
            .collect::<Vec<_>>(),
    );
    block.header.transactions_root = proofs::calculate_transaction_root(&block.body.transactions);
    block.header.gas_used = output.gas_used;
    for receipt in &output.receipts {
        block.header.logs_bloom |= receipt.bloom();
    }
    // Independent post-state oracle from the FIRST native execution, not EXR1 steps.
    let mut end = state.clone();
    for (address, a) in &db.bundle_state.state {
        if a.was_destroyed() || a.info.is_none() {
            end.storage.retain(|(owner, _), _| owner != address);
        }
        if let Some(info) = &a.info {
            end.accounts.insert(
                *address,
                Account {
                    nonce: info.nonce,
                    balance: info.balance,
                    bytecode_hash: (info.code_hash != keccak256([])).then_some(info.code_hash),
                },
            );
            for (slot, value) in &a.storage {
                end.storage
                    .insert((*address, B256::from(*slot)), value.present_value);
            }
        } else {
            end.accounts.remove(address);
        }
    }
    for (hash, code) in &db.bundle_state.contracts {
        end.code.insert(*hash, code.original_bytes());
    }
    assert_eq!(end.state_root(Default::default())?, block.header.state_root);
    Ok(((config, state, parent, block, output.receipts), end))
}

fn call_code(op: u8, to: Address) -> Vec<u8> {
    let mut code = vec![0x5f, 0x5f, 0x5f, 0x5f];
    if op == 0xf1 || op == 0xf2 {
        code.push(0x5f);
    }
    code.push(0x73);
    code.extend_from_slice(to.as_slice());
    code.extend([0x61, 0xff, 0xff, op, 0x50]);
    code
}

fn subcall_address() -> Address {
    alloy_primitives::address!("1800000000000000000000000000000000000003")
}

#[test]
fn native_direct_subcall_keeps_outer_transaction_root() -> Result<()> {
    let (config, state, parent, block, receipts) = imported_case("call-from-direct")?;
    let result = replay(&config, &state, &parent, &block, &receipts, 1)?;
    let calls = result.result().call_traces.as_ref().unwrap();
    assert_eq!(
        calls.frames[0].from,
        block.body.transactions[0].recover_signer()?
    );
    assert_eq!(calls.frames[0].to, Some(subcall_address()));
    assert!(calls.frames[0].parent == -1 && !calls.frames[0].error.is_empty());
    assert!(calls.frames.len() > 1 && calls.frames[1].parent == 0);
    Ok(())
}

#[test]
fn native_subcall_completion_failure_is_not_reported_success() -> Result<()> {
    let (config, state, parent, block, receipts) = imported_case("call-from-oog")?;
    let result = replay(&config, &state, &parent, &block, &receipts, 1)?;
    let calls = result.result().call_traces.as_ref().unwrap();
    assert!(receipts[0].status());
    assert_eq!(
        calls.frames[0].output,
        vec![0; 32],
        "fixture must exercise a failed physical CALL"
    );
    assert_eq!(calls.frames.len(), 2);
    assert_eq!(
        calls.frames[1].output.len(),
        32768,
        "fixture child must have finished execution (gas={}, used={}, error={}, reason={})",
        calls.frames[1].gas,
        calls.frames[1].gas_used,
        calls.frames[1].error,
        String::from_utf8_lossy(&calls.frames[1].output)
    );
    assert_eq!(
        calls.frames[1].error, "ArcCallFromCompletionReverted",
        "completion rollback cannot be reported as a successful child"
    );
    assert!(
        result.result().steps.iter().all(|s| s
            .storage
            .iter()
            .all(|v| v.address != Address::repeat_byte(0x24))),
        "native journal must retain original child state"
    );
    Ok(())
}

#[test]
fn native_call_tree_keeps_nested_and_reverted_attempts() -> Result<()> {
    let (config, state, parent, block, receipts) = imported_case("calls")?;
    let result = replay(&config, &state, &parent, &block, &receipts, 1)?;
    let calls = result.result().call_traces.as_ref().unwrap();
    let kinds = calls
        .frames
        .iter()
        .map(|f| f.r#type.as_str())
        .collect::<Vec<_>>();
    assert_eq!(
        kinds,
        [
            "CALL",
            "CALL",
            "STATICCALL",
            "DELEGATECALL",
            "STATICCALL",
            "CALLCODE",
            "STATICCALL",
            "STATICCALL",
            "STATICCALL",
            "CREATE",
            "CREATE2",
            "CALL",
            "SELFDESTRUCT"
        ]
    );
    assert!(!calls.frames[0].error.is_empty());
    assert!(calls.frames[1..].iter().all(|f| f.error.is_empty()));
    assert_eq!(calls.frames[2].parent, 1);
    assert_eq!(calls.frames[4].parent, 3);
    assert_eq!(calls.frames[12].parent, 11);
    assert!(calls.frames[7].value.is_none());
    assert!(calls.frames[9].to.is_some());
    assert!(calls.frames[10].to.is_some());
    Ok(())
}

#[test]
fn arc_empty_block_records_native_system_changes() -> Result<()> {
    let (config, state, parent, block, receipts) = imported()?;
    let verified = replay(&config, &state, &parent, &block, &receipts, 1)?;
    let result = verified.result();
    assert_eq!(result.steps.first().unwrap().cause, wire::PRE_BLOCK);
    assert_eq!(result.steps.last().unwrap().cause, wire::FINALIZE);
    assert!(
        result
            .steps
            .last()
            .unwrap()
            .storage
            .iter()
            .any(|s| s.before != s.after),
        "Arc accounting must be recorded even without a transaction"
    );
    assert_eq!(result.state_root, block.header.state_root);
    assert!(
        result
            .steps
            .iter()
            .all(|s| s.tx_index == -1 && s.tx_hash.is_zero())
    );
    Ok(())
}

#[test]
fn arc_transactions_storage_reverts_and_creation_are_attributed() -> Result<()> {
    for case in [
        "transfer",
        "storage",
        "revert",
        "create",
        "create-code",
        "create-gone",
    ] {
        let (config, state, parent, block, receipts) = imported_case(case)?;
        let verified = replay(&config, &state, &parent, &block, &receipts, 1)?;
        let result = verified.result();
        assert_eq!(result.steps.len(), 3);
        let tx = &result.steps[1];
        assert_eq!(
            (tx.cause, tx.tx_index, tx.tx_hash),
            (wire::TRANSACTION, 0, *block.body.transactions[0].tx_hash())
        );
        let recipient = Address::repeat_byte(0x23);
        let writes: Vec<_> = tx
            .storage
            .iter()
            .filter(|s| s.address == recipient)
            .collect();
        if case == "storage" {
            assert!(
                writes.iter().any(|s| s.before == B256::from(U256::from(7))
                    && s.after == B256::from(U256::from(9)))
            );
        } else if case == "revert" {
            assert!(writes.is_empty(), "reverted writes must not survive");
        }
        if case.starts_with("create") {
            let created = block.body.transactions[0].recover_signer()?.create(0);
            assert!(
                tx.accounts.iter().any(|a| a.address == created
                    && !a.before.exists
                    && a.after.exists == (case != "create-gone")),
                "successful creation retains its witness even if it disappears"
            );
            if case == "create-code" {
                assert!(tx.code.iter().any(|c| c.bytes == [0]));
            }
        }
        assert!(!verified.encode()?.is_empty());
    }
    Ok(())
}

fn native_intermediate_roots(
    config: &ArcEvmConfig,
    state: &FullState,
    block: &Block,
) -> Result<Vec<B256>> {
    use reth_evm::{Evm, block::BlockExecutor};
    use revm::database::{State, states::bundle_state::BundleRetention};
    let mut db = State::builder()
        .with_database(reth_revm::database::StateProviderDatabase::new(
            state.clone(),
        ))
        .with_bundle_update()
        .build();
    let block = RecoveredBlock::try_recover(block.clone())?;
    let mut e = config.executor_for_block(&mut db, &block)?;
    let mut roots = vec![];
    e.apply_pre_execution_changes()?;
    e.evm_mut()
        .db_mut()
        .merge_transitions(BundleRetention::Reverts);
    roots.push(state.state_root(HashedPostState::from_bundle_state::<
        reth_trie::KeccakKeyHasher,
    >(e.evm().db().bundle_state.state.iter()))?);
    for tx in block.transactions_recovered() {
        e.execute_transaction(&tx)?;
        e.evm_mut()
            .db_mut()
            .merge_transitions(BundleRetention::Reverts);
        roots.push(state.state_root(HashedPostState::from_bundle_state::<
            reth_trie::KeccakKeyHasher,
        >(e.evm().db().bundle_state.state.iter()))?);
    }
    let (mut evm, _) = e.finish()?;
    evm.db_mut().merge_transitions(BundleRetention::Reverts);
    roots.push(state.state_root(HashedPostState::from_bundle_state::<
        reth_trie::KeccakKeyHasher,
    >(evm.db().bundle_state.state.iter()))?);
    Ok(roots)
}

#[test]
fn journal_omissions_and_false_before_values_fail_independent_roots() -> Result<()> {
    use crate::journal::Journal;
    for case in ["storage", "create-code"] {
        let (config, state, parent, block, receipts) = imported_case(case)?;
        let result = replay(&config, &state, &parent, &block, &receipts, 1)?;
        let roots = native_intermediate_roots(&config, &state, &block)?;
        for fault in ["account", "slot", "before", "code"] {
            if fault == "slot" && case != "storage" || fault == "code" && case != "create-code" {
                continue;
            }
            let mut journal = Journal::new(&state);
            journal.apply(&result.result().steps[0], roots[0])?;
            let mut broken = result.result().steps[1].clone();
            match fault {
                "account" => {
                    broken.accounts.clear();
                    broken.storage.clear();
                    broken.code.clear();
                }
                "slot" => broken.storage.clear(),
                "before" => broken.accounts[0].before.nonce += 1,
                "code" => broken.code.clear(),
                _ => unreachable!(),
            }
            assert!(
                journal.apply(&broken, roots[1]).is_err(),
                "{case}: {fault} must not be accepted"
            );
        }
    }
    Ok(())
}

#[test]
fn arc_replay_rejects_wrong_roots_parent_receipts_and_missing_code() -> Result<()> {
    let (config, state, parent, block, receipts) = imported_case("storage")?;
    assert!(replay(&config, &state, &parent, &block, &receipts, 0).is_err());
    assert!(replay(&config, &state, &parent, &block, &receipts, u64::MAX).is_err());
    let mut bad = block.clone();
    bad.header.state_root = B256::repeat_byte(1);
    assert!(replay(&config, &state, &parent, &bad, &receipts, 1).is_err());
    let mut bad = block.clone();
    bad.header.parent_hash = B256::repeat_byte(1);
    assert!(replay(&config, &state, &parent, &bad, &receipts, 1).is_err());
    let mut bad = block.clone();
    bad.header.gas_used += 1;
    assert!(replay(&config, &state, &parent, &bad, &receipts, 1).is_err());
    assert!(replay(&config, &state, &parent, &block, &[], 1).is_err());
    let mut bad_state = state.clone();
    bad_state.code.clear();
    assert!(replay(&config, &bad_state, &parent, &block, &receipts, 1).is_err());
    Ok(())
}

#[cfg(unix)]
#[test]
fn durable_result_never_overwrites_and_preserves_private_input() -> Result<()> {
    use std::{fs, os::unix::fs::PermissionsExt};
    let (config, state, parent, block, receipts) = imported()?;
    let result = replay(&config, &state, &parent, &block, &receipts, 1)?;
    let dir = tempfile::tempdir()?;
    fs::set_permissions(dir.path(), fs::Permissions::from_mode(0o700))?;
    let path = dir.path().join("block.exr");
    write_result(&path, &result)?;
    assert_eq!(fs::read(&path)?, result.encode()?);
    assert_eq!(fs::metadata(&path)?.permissions().mode() & 0o777, 0o600);
    assert!(write_result(&path, &result).is_err());
    assert_eq!(fs::read(&path)?, result.encode()?);
    fs::set_permissions(dir.path(), fs::Permissions::from_mode(0o755))?;
    assert!(write_result(&dir.path().join("other.exr"), &result).is_err());
    Ok(())
}

#[test]
fn frozen_arc_consumer_fixtures_are_reproducible() -> Result<()> {
    // Explicit generation only, never a normal test side effect on the repo.
    let Some(dir) = std::env::var_os("INDEXER_ARC_FIXTURE_DIR") else {
        return Ok(());
    };
    use base64::{Engine, engine::general_purpose::STANDARD};
    use serde_json::json;
    use std::{fs, path::PathBuf};
    let dir = PathBuf::from(dir);
    fs::create_dir_all(&dir)?;
    for case in [
        "state-proofs",
        "storage",
        "create-gone",
        "calls",
        "call-from-oog",
        "call-from-direct",
    ] {
        let ((config, state, parent, block, receipts), end) =
            imported_with_end(if case == "state-proofs" {
                "storage"
            } else {
                case
            })?;
        let mut result = replay(&config, &state, &parent, &block, &receipts, 1)?;
        if case == "state-proofs" {
            result = result.with_state_proofs(&end, &selected_scope())?;
        }
        let mut accounts = vec![];
        let mut storage = vec![];
        let mut code = vec![];
        for (address, a) in &state.accounts {
            let value = wire::Account {
                exists: true,
                nonce: a.nonce,
                balance: B256::from(a.balance),
                code_hash: a.get_bytecode_hash(),
                storage_root: state.storage_root(*address, HashedStorage::default())?,
                extra: 0,
            };
            accounts.push(wire::AccountChange {
                address: *address,
                before: value.clone(),
                after: value,
                baseline: true,
                storage_cleared: false,
            });
        }
        for ((address, slot), value) in &state.storage {
            storage.push(wire::StorageChange {
                address: *address,
                slot: *slot,
                before: B256::from(*value),
                after: B256::from(*value),
                baseline: true,
            });
        }
        for (hash, bytes) in &state.code {
            code.push(wire::Code {
                hash: *hash,
                bytes: bytes.to_vec(),
            });
        }
        let mut current = BTreeMap::new();
        let mut slots = BTreeMap::new();
        let mut new_code = state.code.clone();
        for step in &result.result().steps {
            for c in &step.code {
                new_code.insert(c.hash, c.bytes.clone().into());
            }
            for a in &step.accounts {
                current.insert(a.address, a.after.clone());
            }
            for s in &step.storage {
                slots.insert((s.address, s.slot), s.clone());
            }
        }
        let expected: Vec<_> = current
            .into_iter()
            .map(|(address, account)| {
                let bytes = new_code
                    .get(&account.code_hash)
                    .cloned()
                    .unwrap_or_default();
                json!({"Address": address, "Account": account, "Code": STANDARD.encode(bytes)})
            })
            .collect();
        let fixture = json!({
            "Version": 1, "Profile": "arc", "Producer": ARC_REVISION,
            "Chain": {"ID": "1337", "Genesis": parent.hash()},
            "ParentHeader": STANDARD.encode(alloy_rlp::encode(parent.inner())),
            "Baseline": {"Ref": {"Generation":1,"Height":0,"Hash":parent.hash()}, "Root": parent.state_root, "Accounts":accounts,"Storage":storage,"Code":code},
            "Payload": STANDARD.encode(result.encode()?), "Accounts": expected, "Storage": slots.into_values().collect::<Vec<_>>()
        });
        fs::write(
            dir.join(format!("arc-{case}.json")),
            serde_json::to_vec(&fixture)?,
        )?;
    }
    Ok(())
}

#[test]
fn native_checkpoint_storage_code_and_private_publication_fail_closed() -> Result<()> {
    let (_, mut state, _, _, _) = imported_case("storage")?;
    let h = Header {
        number: 42,
        state_root: state.state_root(Default::default())?,
        ..Default::default()
    };
    let mut keys = checkpoint::Keys::new();
    for a in state.accounts.keys() {
        keys.entry(*a).or_default();
    }
    for (a, s) in state.storage.keys() {
        keys.entry(*a).or_default().insert(*s);
    }
    let dir = tempfile::tempdir()?;
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700))?;
    let path = dir.path().join("full.ndjson");
    checkpoint::write_private(&path, |w| {
        checkpoint::export(w, "1337", LOCAL_DEV.inner.genesis_hash(), &h, &state, &keys)
    })?;
    let (m, recovered) = checkpoint::seed(&path, "1337", LOCAL_DEV.inner.genesis_hash())?;
    assert_eq!(m.r#ref.height, 42);
    assert_eq!(recovered, keys);
    let original = std::fs::read(&path)?;
    assert!(
        checkpoint::write_private(&path, |w| {
            w.write_all(b"replace")?;
            Ok(())
        })
        .is_err()
    );
    assert_eq!(std::fs::read(&path)?, original);
    let (a, s) = *state.storage.keys().next().unwrap();
    keys.get_mut(&a).unwrap().remove(&s);
    let missing = dir.path().join("missing.ndjson");
    assert!(
        checkpoint::write_private(&missing, |w| checkpoint::export(
            w,
            "1337",
            LOCAL_DEV.inner.genesis_hash(),
            &h,
            &state,
            &keys
        ))
        .is_err()
    );
    assert!(!missing.exists());
    keys.get_mut(&a).unwrap().insert(s);
    state.code.clear();
    assert!(
        checkpoint::export(
            Vec::new(),
            "1337",
            LOCAL_DEV.inner.genesis_hash(),
            &h,
            &state,
            &keys
        )
        .is_err()
    );
    let corrupt = dir.path().join("corrupt.ndjson");
    for bytes in [
        original[..original.len() - 15].to_vec(),
        [original.clone(), b"{}\n".to_vec()].concat(),
        vec![b'x'; checkpoint::MAX_RECORD + 1],
    ] {
        std::fs::write(&corrupt, bytes)?;
        std::fs::set_permissions(&corrupt, std::fs::Permissions::from_mode(0o600))?;
        assert!(checkpoint::seed(&corrupt, "1337", LOCAL_DEV.inner.genesis_hash()).is_err());
    }
    Ok(())
}

#[test]
fn native_checkpoint_storage_spool_corruption_rejects_publication() -> Result<()> {
    use sha2::{Digest, Sha256};
    let original = [1u8; 64];
    let digest = B256::from_slice(&Sha256::digest(original));
    let mut corrupt = original;
    corrupt[63] ^= 1;
    let mut input = std::io::Cursor::new(corrupt);
    assert!(
        checkpoint::read_storage_spool(&mut input, 1, digest, |_| Ok(())).is_err(),
        "changed temporary storage values must not be published with the original root"
    );
    Ok(())
}

#[test]
fn native_checkpoint_streams_dense_account_seed_and_scratch_failures() -> Result<()> {
    use crate::catalog::{Builder, Key, Limits};
    use std::os::unix::fs::PermissionsExt;
    let (_, mut state, _, _, _) = imported_case("storage")?;
    let address = state.storage.keys().next().unwrap().0;
    for n in 1000..2024u64 {
        state.storage.insert(
            (address, B256::from(U256::from(n).to_be_bytes::<32>())),
            U256::from(n),
        );
    }
    state
        .storage
        .insert((address, B256::repeat_byte(0x45)), U256::ZERO);
    let header = Header {
        number: 42,
        state_root: state.state_root(Default::default())?,
        ..Default::default()
    };
    let root = tempfile::tempdir()?;
    std::fs::set_permissions(root.path(), std::fs::Permissions::from_mode(0o700))?;
    let limits = Limits {
        chunk: 17,
        fan_in: 2,
        inputs: 10_000,
        disk: 2 << 20,
    };
    let mut input = state
        .accounts
        .keys()
        .copied()
        .map(Key::account)
        .collect::<Vec<_>>();
    input.extend(state.storage.keys().map(|(a, s)| Key::storage(*a, *s)));
    // A storage-only candidate still identifies its account. Deleted accounts
    // and zero slots are candidates, never phantom output records.
    input.push(Key::storage(Address::repeat_byte(0x98), B256::ZERO));
    let mut catalog = Builder::with_limits(root.path(), limits)?;
    for k in &input {
        catalog.push(*k)?;
    }
    let catalog = catalog.finish()?;
    let path = root.path().join("dense.ndjson");
    checkpoint::write_private(&path, |w| {
        checkpoint::export_catalog(
            w,
            "1337",
            LOCAL_DEV.inner.genesis_hash(),
            &header,
            &state,
            &catalog,
        )
    })?;
    let mut seed = Builder::with_limits(root.path(), limits)?;
    let manifest = checkpoint::seed_into(&path, "1337", LOCAL_DEV.inner.genesis_hash(), &mut seed)?;
    assert_eq!(manifest.root, header.state_root);
    let mut again = Vec::new();
    checkpoint::export_catalog(
        &mut again,
        "1337",
        LOCAL_DEV.inner.genesis_hash(),
        &header,
        &state,
        &seed.finish()?,
    )?;
    assert_eq!(std::fs::read(&path)?, again);
    let mut quota = Builder::with_limits(
        root.path(),
        Limits {
            chunk: 32768,
            fan_in: 2,
            inputs: 10_000,
            disk: input.len() as u64 * 117 + 63,
        },
    )?;
    for k in &input {
        quota.push(*k)?;
    }
    let quota = quota.finish()?;
    let rejected = root.path().join("quota.ndjson");
    assert!(
        checkpoint::write_private(&rejected, |w| checkpoint::export_catalog(
            w,
            "1337",
            LOCAL_DEV.inner.genesis_hash(),
            &header,
            &state,
            &quota
        ))
        .is_err()
    );
    assert!(!rejected.exists());
    let corrupt = root.path().join("truncated.ndjson");
    std::fs::write(&corrupt, &again[..again.len() - 15])?;
    std::fs::set_permissions(&corrupt, std::fs::Permissions::from_mode(0o600))?;
    let mut poisoned = Builder::new(root.path())?;
    assert!(
        checkpoint::seed_into(
            &corrupt,
            "1337",
            LOCAL_DEV.inner.genesis_hash(),
            &mut poisoned
        )
        .is_err()
    );
    assert!(poisoned.finish().is_err());
    Ok(())
}

// Independent in-memory full-trie proof oracle. Production uses retained Reth providers.
fn trie_proof(mut entries: Vec<(B256, Vec<u8>)>, key: B256) -> (B256, Vec<Bytes>) {
    use alloy_trie::{HashBuilder, Nibbles, proof::ProofRetainer};
    let path = Nibbles::unpack(key);
    let mut builder = HashBuilder::default().with_proof_retainer(ProofRetainer::new(vec![path]));
    entries.sort_by_key(|(key, _)| *key);
    for (key, value) in entries {
        builder.add_leaf(Nibbles::unpack(key), &value);
    }
    let root = builder.root();
    let nodes = builder
        .take_proof_nodes()
        .matching_nodes_sorted(&path)
        .into_iter()
        .map(|(_, node)| node)
        .collect();
    (root, nodes)
}
impl crate::proofs::Source for FullState {
    fn selected_proof(&self, address: Address, slots: &[B256]) -> Result<reth_trie::AccountProof> {
        let mut entries = vec![];
        for (a, info) in &self.accounts {
            let account = TrieAccount {
                nonce: info.nonce,
                balance: info.balance,
                code_hash: info.get_bytecode_hash(),
                storage_root: self.storage_root(*a, Default::default())?,
            };
            entries.push((keccak256(a), alloy_rlp::encode(account)));
        }
        let (_, proof) = trie_proof(entries, keccak256(address));
        let storage_root = self.storage_root(address, Default::default())?;
        let mut storage_proofs = vec![];
        for slot in slots {
            let entries = self
                .storage
                .iter()
                .filter(|((a, _), v)| *a == address && !v.is_zero())
                .map(|((_, slot), v)| (keccak256(slot), alloy_rlp::encode(v)))
                .collect();
            let (_, nodes) = trie_proof(entries, keccak256(slot));
            let mut p = reth_trie::StorageProof::new(*slot);
            p.value = self
                .storage
                .get(&(address, *slot))
                .copied()
                .unwrap_or_default();
            p.proof = nodes;
            storage_proofs.push(p);
        }
        Ok(reth_trie::AccountProof {
            address,
            info: self.accounts.get(&address).copied(),
            proof,
            storage_root,
            storage_proofs,
        })
    }
    fn selected_code(&self, hash: B256) -> Result<Option<Bytes>> {
        Ok(self.code.get(&hash).cloned())
    }
}
fn selected_scope() -> crate::proofs::Selection {
    crate::proofs::Selection {
        targets: vec![
            crate::proofs::Target {
                address: Address::repeat_byte(0x23),
                code: true,
                slots: vec![B256::ZERO, B256::repeat_byte(1)],
            },
            crate::proofs::Target {
                address: Address::repeat_byte(0xee),
                code: true,
                slots: vec![B256::ZERO],
            },
        ],
    }
}
#[test]
fn native_replay_selected_proofs_preserve_roots_code_and_exact_scope() -> Result<()> {
    let ((config, state, parent, block, receipts), mut end) = imported_with_end("storage")?;
    let verified = replay(&config, &state, &parent, &block, &receipts, 1)?
        .with_state_proofs(&end, &selected_scope())?;
    let proof = verified.result().state_proof_bundle.as_ref().unwrap();
    assert_eq!((&*proof.profile, &*proof.provider), ("arc", "arc-native"));
    let bundle: serde_json::Value = serde_json::from_slice(&proof.response)?;
    assert_eq!(
        bundle["evidence"][0]["proof"]["storageProof"][0]["value"],
        "0x9"
    );
    assert_eq!(bundle["evidence"][1]["code"], "0x");
    assert!(!verified.encode()?.is_empty());
    assert!(crate::proofs::export(&state, block.header.state_root, &selected_scope()).is_err());
    end.code.clear();
    assert!(crate::proofs::export(&end, block.header.state_root, &selected_scope()).is_err());
    let mut bad = selected_scope();
    bad.targets.push(bad.targets[0].clone());
    assert!(bad.normalize().is_err());
    let mut bad = selected_scope();
    bad.targets[0].slots = vec![B256::ZERO; 129];
    assert!(bad.normalize().is_err());
    let mut bad = selected_scope();
    bad.targets[0].slots = vec![B256::ZERO; 2];
    assert!(bad.normalize().is_err());
    assert!(crate::proofs::Selection::parse(br#"{"targets":[],"targets":[]}"#).is_err());
    assert!(crate::proofs::Selection::parse(br#"{"targets":[],"unknown":0}"#).is_err());
    Ok(())
}

#[test]
fn native_proof_selection_files_and_malformed_source_fail_closed() -> Result<()> {
    use std::os::unix::fs::PermissionsExt;
    let dir = tempfile::tempdir()?;
    let path = dir.path().join("selection.json");
    let selection = selected_scope();
    std::fs::write(&path, serde_json::to_vec(&selection)?)?;
    std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600))?;
    assert_eq!(crate::proofs::read_selection(&path)?.targets.len(), 2);
    let link = dir.path().join("link");
    std::os::unix::fs::symlink(&path, &link)?;
    assert!(crate::proofs::read_selection(&link).is_err());
    std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o644))?;
    assert!(crate::proofs::read_selection(&path).is_err());
    std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600))?;
    std::fs::write(&path, vec![b' '; (16 << 10) + 1])?;
    assert!(crate::proofs::read_selection(&path).is_err());
    struct Fault<'a> {
        state: &'a FullState,
        fault: &'static str,
    }
    impl crate::proofs::Source for Fault<'_> {
        fn selected_proof(
            &self,
            address: Address,
            slots: &[B256],
        ) -> Result<reth_trie::AccountProof> {
            let mut proof = crate::proofs::Source::selected_proof(self.state, address, slots)?;
            match self.fault {
                "address" => proof.address = Address::ZERO,
                "slots" => {
                    proof.storage_proofs.pop();
                }
                "key" => proof.storage_proofs[0].key = B256::repeat_byte(9),
                "nodes" => proof.proof = vec![Bytes::from(vec![0x80]); 66],
                "node-size" => proof.proof = vec![Bytes::from(vec![0; 1025])],
                "damaged" => proof.proof = vec![Bytes::from(vec![0x80])],
                _ => {}
            }
            Ok(proof)
        }
        fn selected_code(&self, _: B256) -> Result<Option<Bytes>> {
            Ok(Some(Bytes::from(vec![0])))
        }
    }
    let ((_, _, _, block, _), end) = imported_with_end("storage")?;
    for fault in [
        "address",
        "slots",
        "key",
        "nodes",
        "node-size",
        "damaged",
        "code",
    ] {
        assert!(
            crate::proofs::export(
                &Fault { state: &end, fault },
                block.header.state_root,
                &selection
            )
            .is_err(),
            "{fault}"
        );
    }
    let mut no_code = selection;
    for target in &mut no_code.targets {
        target.code = false;
    }
    let bytes = crate::proofs::export(
        &Fault {
            state: &end,
            fault: "code",
        },
        block.header.state_root,
        &no_code,
    )?;
    let value: serde_json::Value = serde_json::from_slice(&bytes)?;
    assert!(value["evidence"][0].get("code").is_none());
    Ok(())
}
