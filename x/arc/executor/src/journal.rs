use crate::{ReplayState, wire};
use alloy_primitives::{Address, B256, KECCAK256_EMPTY, U256, keccak256};
use eyre::{Result, ensure, eyre};
use reth_primitives_traits::Account as NativeAccount;
use reth_trie::{HashedPostState, HashedStorage, KeccakKeyHasher};
use revm::database::BundleState;
use std::collections::{BTreeMap, BTreeSet};

pub(crate) const MAX_CHANGES: usize = 65_536;

pub(crate) struct Attribution {
    pub ordinal: u32,
    pub cause: u8,
    pub tx_index: i32,
    pub tx_hash: B256,
}

/// Reconstructs solely from wire changes, independently of the native bundle.
/// Each step is checked against the native intermediate root, so changes that
/// cancel out later in the block cannot disappear from transaction history.
pub(crate) struct Journal<'a, S> {
    state: &'a S,
    updates: HashedPostState,
    accounts: BTreeMap<Address, wire::Account>,
    changes: usize,
}

impl<'a, S: ReplayState> Journal<'a, S> {
    pub(crate) fn new(state: &'a S) -> Self {
        Self {
            state,
            updates: HashedPostState::default(),
            accounts: BTreeMap::new(),
            changes: 0,
        }
    }

    fn account(&self, address: Address) -> Result<wire::Account> {
        if let Some(a) = self.accounts.get(&address) {
            return Ok(a.clone());
        }
        let Some(a) = self.state.basic_account(&address)? else {
            return Ok(wire::Account::default());
        };
        Ok(account_value(
            &a,
            self.state.storage_root(address, HashedStorage::default())?,
        ))
    }

    fn slot(&self, address: Address, slot: B256) -> Result<B256> {
        if let Some(s) = self.updates.storages.get(&keccak256(address)) {
            if let Some(value) = s.storage.get(&keccak256(slot)) {
                return Ok(B256::from(*value));
            }
            if s.wiped {
                return Ok(B256::ZERO);
            }
        }
        Ok(B256::from(
            self.state.storage(address, slot)?.unwrap_or_default(),
        ))
    }

    pub(crate) fn apply(&mut self, step: &wire::Step, native_root: B256) -> Result<()> {
        let mut last = None;
        for a in &step.accounts {
            ensure!(
                last.is_none_or(|old| old < a.address) && !a.baseline,
                "account ordering/baseline"
            );
            last = Some(a.address);
            ensure!(
                self.account(a.address)? == a.before,
                "account before mismatch"
            );
            ensure!(
                a.after.extra == 0 && (a.after.exists || a.after == wire::Account::default()),
                "invalid Arc account"
            );
        }
        let mut last_slot = None;
        for slot in &step.storage {
            let key = (slot.address, slot.slot);
            ensure!(
                last_slot.is_none_or(|old| old < key) && !slot.baseline,
                "storage ordering/baseline"
            );
            last_slot = Some(key);
            ensure!(
                self.slot(slot.address, slot.slot)? == slot.before,
                "slot before mismatch"
            );
            ensure!(
                step.accounts
                    .binary_search_by_key(&slot.address, |a| a.address)
                    .is_ok(),
                "slot lacks account witness"
            );
        }
        let mut last_code = None;
        for code in &step.code {
            ensure!(
                last_code.is_none_or(|old| old < code.hash)
                    && !code.bytes.is_empty()
                    && code.bytes.len() <= 1 << 20
                    && keccak256(&code.bytes) == code.hash,
                "invalid code"
            );
            last_code = Some(code.hash);
        }
        // All before comparisons happen before installing any same-step clear.
        for a in &step.accounts {
            let hash = keccak256(a.address);
            if a.storage_cleared || !a.before.exists || !a.after.exists {
                self.updates.storages.insert(hash, HashedStorage::new(true));
            }
            let value = a.after.exists.then_some(NativeAccount {
                nonce: a.after.nonce,
                balance: U256::from_be_bytes(a.after.balance.0),
                bytecode_hash: (a.after.code_hash != KECCAK256_EMPTY).then_some(a.after.code_hash),
            });
            self.updates.accounts.insert(hash, value);
        }
        for slot in &step.storage {
            self.updates
                .storages
                .entry(keccak256(slot.address))
                .or_default()
                .storage
                .insert(keccak256(slot.slot), U256::from_be_bytes(slot.after.0));
        }
        for a in &step.accounts {
            if a.after.exists {
                ensure!(!a.after.code_hash.is_zero(), "missing code hash");
                let storage = self
                    .updates
                    .storages
                    .get(&keccak256(a.address))
                    .cloned()
                    .unwrap_or_default();
                ensure!(
                    self.state.storage_root(a.address, storage)? == a.after.storage_root,
                    "journal storage root mismatch"
                );
                if a.after.code_hash != KECCAK256_EMPTY && a.after.code_hash != a.before.code_hash {
                    ensure!(
                        step.code.iter().any(|c| c.hash == a.after.code_hash),
                        "new code is missing"
                    );
                }
            }
            self.accounts.insert(a.address, a.after.clone());
        }
        self.changes += step.accounts.len() + step.storage.len() + step.code.len();
        ensure!(self.changes <= MAX_CHANGES, "journal change limit");
        ensure!(
            self.state.state_root(self.updates.clone())? == native_root,
            "journal intermediate root mismatch"
        );
        Ok(())
    }

    pub(crate) fn capture(
        &mut self,
        bundle: &BundleState,
        cleared: &BTreeSet<Address>,
        forced: Option<Address>,
        at: Attribution,
    ) -> Result<wire::Step> {
        ensure!(
            bundle.state.len()
                + bundle
                    .state
                    .values()
                    .map(|a| a.storage.len())
                    .sum::<usize>()
                <= MAX_CHANGES,
            "native state limit"
        );
        let hashed = HashedPostState::from_bundle_state::<KeccakKeyHasher>(bundle.state.iter());
        let native_root = self.state.state_root(hashed.clone())?;
        let mut addresses: BTreeSet<_> = bundle.state.keys().copied().collect();
        addresses.extend(forced);
        let mut step = wire::Step {
            ordinal: at.ordinal,
            cause: at.cause,
            tx_index: at.tx_index,
            tx_hash: at.tx_hash,
            accounts: vec![],
            storage: vec![],
            code: vec![],
        };
        let mut codes = BTreeMap::new();
        for address in addresses {
            let before = self.account(address)?;
            let native = bundle.state.get(&address);
            let after = match native {
                Some(a) => match &a.info {
                    Some(info) => {
                        let value = NativeAccount {
                            nonce: info.nonce,
                            balance: info.balance,
                            bytecode_hash: (info.code_hash != KECCAK256_EMPTY)
                                .then_some(info.code_hash),
                        };
                        account_value(
                            &value,
                            self.state.storage_root(
                                address,
                                hashed
                                    .storages
                                    .get(&keccak256(address))
                                    .cloned()
                                    .unwrap_or_default(),
                            )?,
                        )
                    }
                    None => wire::Account::default(),
                },
                None => before.clone(),
            };
            let mut slots = BTreeSet::new();
            if let Some(a) = native {
                slots.extend(a.storage.keys().copied());
            }
            let start = step.storage.len();
            for slot in slots {
                let key = B256::from(slot);
                let old = self.slot(address, key)?;
                let value = native
                    .and_then(|a| a.storage_slot(slot))
                    .map(B256::from)
                    .unwrap_or(old);
                if old != value {
                    step.storage.push(wire::StorageChange {
                        address,
                        slot: key,
                        before: old,
                        after: value,
                        baseline: false,
                    });
                }
            }
            let wipe = cleared.contains(&address);
            if before != after || wipe || step.storage.len() > start || forced == Some(address) {
                if after.exists
                    && after.code_hash != KECCAK256_EMPTY
                    && after.code_hash != before.code_hash
                {
                    let code = bundle
                        .contracts
                        .get(&after.code_hash)
                        .ok_or_else(|| eyre!("new native code unavailable"))?
                        .original_bytes();
                    ensure!(!code.is_empty() && code.len() <= 1 << 20, "code size limit");
                    codes.insert(after.code_hash, code.to_vec());
                }
                step.accounts.push(wire::AccountChange {
                    address,
                    before,
                    after,
                    baseline: false,
                    storage_cleared: wipe,
                });
            }
        }
        step.code = codes
            .into_iter()
            .map(|(hash, bytes)| wire::Code { hash, bytes })
            .collect();
        self.apply(&step, native_root)?;
        Ok(step)
    }
}

fn account_value(a: &NativeAccount, storage_root: B256) -> wire::Account {
    wire::Account {
        exists: true,
        nonce: a.nonce,
        balance: B256::from(a.balance),
        code_hash: a.get_bytecode_hash(),
        storage_root,
        extra: 0,
    }
}
