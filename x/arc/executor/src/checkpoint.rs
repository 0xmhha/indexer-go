//! Full native state export. Key catalogs are candidates, never coverage proofs.
use crate::catalog::{Builder, Catalog, Key};
use crate::wire::{Account, Ref};
use alloy_consensus::Header;
use alloy_primitives::{Address, B256, keccak256};
use alloy_trie::{HashBuilder, Nibbles, TrieAccount};
use base64::{Engine, engine::general_purpose::STANDARD};
use eyre::{Result, ensure};
use reth_revm::database::EvmStateProvider;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    io::{BufRead, BufReader, BufWriter, Read, Seek, SeekFrom, Write},
    path::Path,
};

#[cfg(test)]
use std::collections::{BTreeMap, BTreeSet};
#[cfg(test)]
pub const MAX_KEYS: usize = MAX_RECORDS - 2;
pub const MAX_RECORD: usize = 2 << 20;
pub const MAX_RECORDS: usize = 64_000_000;
pub const MAX_BYTES: u64 = 64 << 30;
#[cfg(test)]
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
struct Stream<W> {
    writer: W,
    records: usize,
    bytes: u64,
}
impl<W: Write> Stream<W> {
    fn record(&mut self, r: Record) -> Result<()> {
        let line = serde_json::to_vec(&r)?;
        ensure!(
            line.len() <= MAX_RECORD
                && self.records < MAX_RECORDS
                && (line.len() as u64 + 1) <= MAX_BYTES - self.bytes,
            "checkpoint output limit"
        );
        self.writer.write_all(&line)?;
        self.writer.write_all(b"\n")?;
        self.records += 1;
        self.bytes += line.len() as u64 + 1;
        Ok(())
    }
}

// Small in-memory fixture conveniences are deliberately absent from production.
#[cfg(test)]
pub fn validate_keys(keys: &Keys) -> Result<()> {
    let count = keys
        .values()
        .try_fold(keys.len(), |n, v| n.checked_add(v.len()));
    ensure!(count.is_some_and(|n| n <= MAX_KEYS), "checkpoint key limit");
    Ok(())
}
#[cfg(test)]
pub fn export(
    w: impl Write,
    chain: &str,
    genesis: B256,
    header: &Header,
    state: &impl EvmStateProvider,
    keys: &Keys,
) -> Result<()> {
    validate_keys(keys)?;
    let work = tempfile::tempdir()?;
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
    let mut b = Builder::new(work.path())?;
    for (address, slots) in keys {
        b.push(Key::account(*address))?;
        for slot in slots {
            b.push(Key::storage(*address, *slot))?;
        }
    }
    export_catalog(w, chain, genesis, header, state, &b.finish()?)
}

pub fn export_catalog(
    w: impl Write,
    chain: &str,
    genesis: B256,
    header: &Header,
    state: &impl EvmStateProvider,
    keys: &Catalog,
) -> Result<()> {
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
    let mut out = Stream {
        writer: w,
        records: 0,
        bytes: 0,
    };
    out.record(Record {
        kind: "manifest".into(),
        manifest: Some(manifest),
        ..Default::default()
    })?;
    let mut reader = keys.reader()?;
    let mut next = reader.next_key()?;
    let mut accounts = HashBuilder::default();
    let mut previous_address = None;
    while let Some(first) = next.take() {
        let address = first.address();
        let hash = keccak256(address);
        ensure!(
            previous_address.is_none_or(|p| p < hash),
            "catalog address hash collision/order"
        );
        previous_address = Some(hash);
        let account = state.basic_account(&address)?;
        let mut storage = HashBuilder::default();
        // The account record needs the completed storage root before its slots.
        // Use an anonymous private spool, never a per-account Vec or map.
        let mut spool = None;
        let mut slots = 0u64;
        let mut spool_digest = Sha256::new();
        let mut previous_slot = None;
        let mut current = first;
        loop {
            if let Some(slot) = current.slot() {
                let slot_hash = keccak256(slot);
                ensure!(
                    previous_slot.is_none_or(|p| p < slot_hash),
                    "catalog slot hash collision/order"
                );
                previous_slot = Some(slot_hash);
                if account.is_some() {
                    let value = state.storage(address, slot)?.unwrap_or_default();
                    if !value.is_zero() {
                        ensure!(
                            (slots + 1) * 64 <= keys.spare_bytes(),
                            "account spool scratch byte limit"
                        );
                        storage.add_leaf(
                            Nibbles::unpack(slot_hash),
                            alloy_rlp::encode_fixed_size(&value).as_ref(),
                        );
                        if spool.is_none() {
                            spool = Some(BufWriter::new(tempfile::tempfile_in(keys.workspace()?)?));
                        }
                        let file = spool.as_mut().expect("nonzero storage spool");
                        file.write_all(slot.as_slice())?;
                        file.write_all(&value.to_be_bytes::<32>())?;
                        spool_digest.update(slot.as_slice());
                        spool_digest.update(value.to_be_bytes::<32>());
                        slots += 1;
                    }
                }
            }
            next = reader.next_key()?;
            match next {
                Some(k) if k.address() == address => {
                    current = k;
                }
                _ => break,
            }
        }
        let Some(account) = account else {
            continue;
        };
        let storage_root = storage.root();
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
        accounts.add_leaf(
            Nibbles::unpack(hash),
            &alloy_rlp::encode(TrieAccount {
                nonce: account.nonce,
                balance: account.balance,
                storage_root,
                code_hash,
            }),
        );
        out.record(Record {
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
        })?;
        if let Some(mut spool) = spool {
            spool.flush()?;
            let mut spool = BufReader::new(spool.into_inner().map_err(|e| e.into_error())?);
            read_storage_spool(
                &mut spool,
                slots,
                B256::from_slice(&spool_digest.finalize()),
                |row| {
                    out.record(Record {
                        kind: "storage".into(),
                        address: Some(address),
                        slot: Some(B256::from_slice(&row[..32])),
                        value: Some(B256::from_slice(&row[32..])),
                        ..Default::default()
                    })
                },
            )?;
        }
    }
    ensure!(
        accounts.root() == header.state_root,
        "incomplete native key/history coverage or state root mismatch"
    );
    out.record(Record {
        kind: "end".into(),
        ..Default::default()
    })
}

// Seed state values are never used: only candidate raw keys survive. The final
// native/header root reconstruction proves full coverage, even for a false seed.
fn read_seed(
    path: &Path,
    chain: &str,
    genesis: B256,
    mut push: impl FnMut(Key) -> Result<()>,
) -> Result<Manifest> {
    private_file(path)?;
    let mut input = BufReader::new(std::fs::File::open(path)?);
    let mut manifest = None;
    let mut current = None;
    let mut previous_address = None;
    let mut previous_slot = None;
    let mut ended = false;
    let mut total = 0u64;
    let mut count = 0usize;
    loop {
        let mut line = Vec::new();
        (&mut input)
            .take((MAX_RECORD + 1) as u64)
            .read_until(b'\n', &mut line)?;
        if line.is_empty() {
            ensure!(ended, "checkpoint missing end");
            return manifest.ok_or_else(|| eyre::eyre!("missing manifest"));
        }
        total = total
            .checked_add(line.len() as u64)
            .ok_or_else(|| eyre::eyre!("seed bytes overflow"))?;
        ensure!(
            line.len() <= MAX_RECORD && total <= MAX_BYTES && !ended && count < MAX_RECORDS,
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
                push(Key::account(a))?;
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
                push(Key::storage(a, slot))?;
                previous_slot = Some(hash);
            }
            "end" => {
                ensure!(manifest.is_some(), "end before manifest");
                ended = true;
            }
            _ => eyre::bail!("unknown checkpoint record"),
        }
        count += 1;
    }
}
pub fn seed_into(
    path: &Path,
    chain: &str,
    genesis: B256,
    catalog: &mut Builder,
) -> Result<Manifest> {
    let result = read_seed(path, chain, genesis, |k| catalog.push(k));
    if result.is_err() {
        catalog.poison();
    }
    result
}
#[cfg(test)]
pub fn seed(path: &Path, chain: &str, genesis: B256) -> Result<(Manifest, Keys)> {
    let mut keys = Keys::new();
    let manifest = read_seed(path, chain, genesis, |k| {
        let slots = keys.entry(k.address()).or_default();
        if let Some(slot) = k.slot() {
            slots.insert(slot);
        }
        Ok(())
    })?;
    Ok((manifest, keys))
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

// The caller publishes only after this complete temporary stream succeeds.
pub(crate) fn read_storage_spool(
    input: &mut (impl Read + Seek),
    count: u64,
    digest: B256,
    mut emit: impl FnMut([u8; 64]) -> Result<()>,
) -> Result<()> {
    input.seek(SeekFrom::Start(0))?;
    let mut actual = Sha256::new();
    for _ in 0..count {
        let mut row = [0u8; 64];
        input.read_exact(&mut row)?;
        actual.update(row);
        emit(row)?;
    }
    let mut extra = [0];
    ensure!(
        input.read(&mut extra)? == 0 && B256::from_slice(&actual.finalize()) == digest,
        "account spool EOF/digest mismatch"
    );
    Ok(())
}
