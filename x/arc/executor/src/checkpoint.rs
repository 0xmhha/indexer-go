//! Full native state export. Key catalogs are candidates, never coverage proofs.
use crate::wire::{Account, Ref};
use alloy_consensus::Header;
use alloy_primitives::{Address, B256, keccak256};
use alloy_trie::{TrieAccount, root};
use base64::{Engine, engine::general_purpose::STANDARD};
use eyre::{Result, ensure};
use reth_revm::database::EvmStateProvider;
use serde::{Deserialize, Serialize};
use std::{
    collections::{BTreeMap, BTreeSet},
    io::{BufRead, BufReader, Read, Write},
    path::Path,
};

pub const MAX_KEYS: usize = 1_000_000;
pub const MAX_RECORD: usize = 2 << 20;
pub const MAX_RECORDS: usize = 64_000_000;
pub const MAX_BYTES: u64 = 64 << 30;
pub type Keys = BTreeMap<Address, BTreeSet<B256>>;

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Chain {
    #[serde(rename = "ID")]
    pub id: String,
    pub genesis: B256,
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Manifest {
    pub version: u8,
    pub mode: String,
    pub coverage: String,
    pub chain: Chain,
    pub r#ref: Ref,
    pub root: B256,
    pub header: String,
}
#[derive(Default, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
struct Record {
    kind: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    manifest: Option<Manifest>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    address: Option<Address>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    account: Option<Account>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    slot: Option<B256>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    value: Option<B256>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    code: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    proof: Option<Vec<String>>,
}
fn record(w: &mut impl Write, r: Record) -> Result<()> {
    let line = serde_json::to_vec(&r)?;
    ensure!(line.len() <= MAX_RECORD, "checkpoint record limit");
    w.write_all(&line)?;
    w.write_all(b"\n")?;
    Ok(())
}
pub fn validate_keys(keys: &Keys) -> Result<()> {
    let count = keys
        .values()
        .try_fold(keys.len(), |n, v| n.checked_add(v.len()));
    ensure!(
        count.is_some_and(|n| n <= MAX_KEYS),
        "checkpoint catalog exceeds one million address/slot keys"
    );
    Ok(())
}

pub fn export(
    mut w: impl Write,
    chain: &str,
    genesis: B256,
    header: &Header,
    state: &impl EvmStateProvider,
    keys: &Keys,
) -> Result<()> {
    validate_keys(keys)?;
    ensure!(
        chain.parse::<u64>().is_ok_and(|id| id > 0)
            && genesis != B256::ZERO
            && header.number < i64::MAX as u64
            && header.timestamp <= i64::MAX as u64
            && (header.number != 0 || header.hash_slow() == genesis),
        "checkpoint identity/range"
    );
    let manifest = Manifest {
        version: 1,
        mode: if header.number == 0 {
            "genesis"
        } else {
            "checkpoint"
        }
        .into(),
        coverage: "full".into(),
        chain: Chain {
            id: chain.into(),
            genesis,
        },
        r#ref: Ref {
            generation: 1,
            height: header.number,
            hash: header.hash_slow(),
        },
        root: header.state_root,
        header: STANDARD.encode(alloy_rlp::encode(header)),
    };
    record(
        &mut w,
        Record {
            kind: "manifest".into(),
            manifest: Some(manifest),
            ..Default::default()
        },
    )?;
    let ordered: BTreeMap<_, _> = keys
        .iter()
        .map(|(a, slots)| (keccak256(a), (*a, slots)))
        .collect();
    let mut accounts = Vec::new();
    for (hash, (address, slots)) in ordered {
        let Some(account) = state.basic_account(&address)? else {
            continue;
        };
        let mut storage = BTreeMap::new();
        for slot in slots {
            let value = state.storage(address, *slot)?.unwrap_or_default();
            if !value.is_zero() {
                storage.insert(keccak256(slot), (*slot, value));
            }
        }
        let storage_root = root::storage_root(storage.iter().map(|(h, (_, v))| (*h, *v)));
        let code_hash = account.get_bytecode_hash();
        let code = if code_hash == keccak256([]) {
            Vec::new()
        } else {
            state
                .bytecode_by_hash(&code_hash)?
                .ok_or_else(|| eyre::eyre!("native code unavailable"))?
                .original_bytes()
                .to_vec()
        };
        ensure!(
            code.len() <= 1 << 20 && keccak256(&code) == code_hash,
            "native code hash/limit"
        );
        accounts.push((
            hash,
            TrieAccount {
                nonce: account.nonce,
                balance: account.balance,
                storage_root,
                code_hash,
            },
        ));
        record(
            &mut w,
            Record {
                kind: "account".into(),
                address: Some(address),
                account: Some(Account {
                    exists: true,
                    nonce: account.nonce,
                    balance: B256::from(account.balance.to_be_bytes::<32>()),
                    code_hash,
                    storage_root,
                    extra: 0,
                }),
                code: Some(STANDARD.encode(code)),
                ..Default::default()
            },
        )?;
        for (_, (slot, value)) in storage {
            record(
                &mut w,
                Record {
                    kind: "storage".into(),
                    address: Some(address),
                    slot: Some(slot),
                    value: Some(B256::from(value.to_be_bytes::<32>())),
                    ..Default::default()
                },
            )?;
        }
    }
    ensure!(
        root::state_root(accounts) == header.state_root,
        "incomplete native key/history coverage or state root mismatch"
    );
    record(
        &mut w,
        Record {
            kind: "end".into(),
            ..Default::default()
        },
    )
}

// Seed state values are never used: only candidate raw keys survive. The final
// native/header root reconstruction proves full coverage, even for a false seed.
pub fn seed(path: &Path, chain: &str, genesis: B256) -> Result<(Manifest, Keys)> {
    private_file(path)?;
    let mut input = BufReader::new(std::fs::File::open(path)?);
    let mut keys = Keys::new();
    let mut manifest = None;
    let mut current = None;
    let mut previous_address = None;
    let mut previous_slot = None;
    let mut ended = false;
    let mut total = 0u64;
    let mut key_count = 0usize;
    for count in 0..MAX_RECORDS {
        let mut line = Vec::new();
        (&mut input)
            .take((MAX_RECORD + 1) as u64)
            .read_until(b'\n', &mut line)?;
        if line.is_empty() {
            ensure!(ended, "checkpoint missing end");
            return Ok((
                manifest.ok_or_else(|| eyre::eyre!("missing manifest"))?,
                keys,
            ));
        }
        total = total
            .checked_add(line.len() as u64)
            .ok_or_else(|| eyre::eyre!("seed bytes overflow"))?;
        ensure!(
            line.len() <= MAX_RECORD && total <= MAX_BYTES && !ended,
            "seed bounds or data after end"
        );
        let r: Record = serde_json::from_slice(&line)?;
        ensure!(
            r.proof.as_ref().is_none_or(|p| p.is_empty()),
            "partial seed proofs unsupported"
        );
        match r.kind.as_str() {
            "manifest" => {
                ensure!(count == 0, "duplicate manifest");
                let m = r.manifest.ok_or_else(|| eyre::eyre!("manifest required"))?;
                ensure!(
                    m.version == 1
                        && m.coverage == "full"
                        && (m.mode == "genesis" || m.mode == "checkpoint")
                        && m.chain.id == chain
                        && m.chain.genesis == genesis
                        && m.r#ref.generation == 1
                        && m.r#ref.height < i64::MAX as u64,
                    "seed identity/full coverage"
                );
                let bytes = STANDARD.decode(&m.header)?;
                let mut bytes = bytes.as_slice();
                let h = <Header as alloy_rlp::Decodable>::decode(&mut bytes)?;
                ensure!(
                    bytes.is_empty()
                        && h.number == m.r#ref.height
                        && h.hash_slow() == m.r#ref.hash
                        && h.state_root == m.root,
                    "seed header commitment"
                );
                manifest = Some(m);
            }
            "account" => {
                ensure!(
                    manifest.is_some() && r.manifest.is_none(),
                    "account before manifest"
                );
                let a = r.address.ok_or_else(|| eyre::eyre!("address required"))?;
                let hash = keccak256(a);
                ensure!(
                    r.account.as_ref().is_some_and(|v| v.exists && v.extra == 0)
                        && previous_address.is_none_or(|p| p < hash),
                    "seed account order/format"
                );
                keys.entry(a).or_default();
                key_count += 1;
                current = Some(a);
                previous_address = Some(hash);
                previous_slot = None;
            }
            "storage" => {
                let a = r.address.ok_or_else(|| eyre::eyre!("address required"))?;
                let slot = r.slot.ok_or_else(|| eyre::eyre!("slot required"))?;
                let hash = keccak256(slot);
                ensure!(
                    current == Some(a)
                        && previous_slot.is_none_or(|p| p < hash)
                        && r.value.is_some_and(|v| v != B256::ZERO),
                    "seed storage order/format"
                );
                keys.get_mut(&a)
                    .ok_or_else(|| eyre::eyre!("missing account"))?
                    .insert(slot);
                key_count += 1;
                previous_slot = Some(hash);
            }
            "end" => {
                ensure!(manifest.is_some(), "end before manifest");
                ended = true;
            }
            _ => eyre::bail!("unknown checkpoint record"),
        }
        ensure!(key_count <= MAX_KEYS, "checkpoint key limit");
    }
    eyre::bail!("checkpoint record limit")
}
pub fn private_file(path: &Path) -> Result<()> {
    use std::os::unix::fs::MetadataExt;
    let m = std::fs::symlink_metadata(path)?;
    ensure!(
        path.is_absolute()
            && m.is_file()
            && m.nlink() == 1
            && m.mode() & 0o777 == 0o600
            && m.uid() == rustix::process::geteuid().as_raw(),
        "owned private regular file required"
    );
    Ok(())
}
pub fn write_private(path: &Path, f: impl FnOnce(&mut dyn Write) -> Result<()>) -> Result<()> {
    use std::os::unix::fs::MetadataExt;
    let parent = path.parent().ok_or_else(|| eyre::eyre!("output parent"))?;
    let m = std::fs::symlink_metadata(parent)?;
    ensure!(
        path.is_absolute()
            && m.is_dir()
            && m.mode() & 0o777 == 0o700
            && m.uid() == rustix::process::geteuid().as_raw(),
        "private owned output parent required"
    );
    let mut temp = tempfile::NamedTempFile::new_in(parent)?;
    struct Bounded<'a> {
        w: &'a mut dyn Write,
        total: u64,
    }
    impl Write for Bounded<'_> {
        fn write(&mut self, b: &[u8]) -> std::io::Result<usize> {
            if b.len() as u64 > MAX_BYTES - self.total {
                return Err(std::io::Error::other("checkpoint byte limit"));
            }
            let n = self.w.write(b)?;
            self.total += n as u64;
            Ok(n)
        }
        fn flush(&mut self) -> std::io::Result<()> {
            self.w.flush()
        }
    }
    f(&mut Bounded {
        w: &mut temp,
        total: 0,
    })?;
    temp.as_file().sync_all()?;
    let current = std::fs::symlink_metadata(parent)?;
    ensure!(
        current.is_dir()
            && current.dev() == m.dev()
            && current.ino() == m.ino()
            && current.mode() & 0o777 == 0o700,
        "checkpoint output directory changed"
    );
    temp.persist_noclobber(path)?;
    std::fs::File::open(parent)?.sync_all()?;
    Ok(())
}
