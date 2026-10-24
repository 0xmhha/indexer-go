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
        let recipient = Address::repeat_byte(0x23);
        let code: Option<Bytes> = match case {
            "storage" => Some(vec![0x60, 9, 0x5f, 0x55, 0x00].into()),
            "revert" => Some(vec![0x60, 9, 0x5f, 0x55, 0x5f, 0x5f, 0xfd].into()),
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
            gas_limit: 100_000,
            to: if case.starts_with("create") {
                TxKind::Create
            } else {
                TxKind::Call(recipient)
            },
            value: U256::from(100),
            input: match case {
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
    Ok((config, state, parent, block, output.receipts))
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
    for case in ["storage", "create-gone"] {
        let (config, state, parent, block, receipts) = imported_case(case)?;
        let result = replay(&config, &state, &parent, &block, &receipts, 1)?;
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
