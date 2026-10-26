//! Native-v2 COW retention. Full cutoff seed and protected state stay intact.
use crate::{
    checkpoint,
    native::{self, Factory},
    wire::Ref,
};
use alloy_primitives::B256;
use arc_execution_config::chainspec::ArcChainSpec;
use eyre::{Result, ensure, eyre};
use reth_ethereum::provider::{
    BlockHashReader, BlockReader, HeaderProvider, ReceiptProvider, StaticFileProviderFactory,
    StaticFileSegment,
};
use reth_prune_types::{PruneCheckpoint, PruneMode, PruneSegment};
use reth_storage_api::{
    BlockNumReader, PruneCheckpointReader, PruneCheckpointWriter, StateRootProvider,
    StorageSettingsCache,
};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    io::Read,
    path::{Path, PathBuf},
};

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Request {
    pub version: u8,
    pub chain: checkpoint::Chain,
    pub anchor: Ref,
    #[serde(rename = "ACK")]
    pub ack: Ref,
    pub tail: Ref,
    pub head: Ref,
    pub head_root: B256,
    pub source_digest: B256,
    pub keep_blocks: u64,
    pub through: u64,
}
impl Request {
    pub fn validate(&self) -> Result<()> {
        ensure!(
            self.version == 1
                && self.anchor.generation > 0
                && self.anchor.generation <= self.ack.generation
                && self.ack.generation == self.tail.generation
                && self.head.generation == 0,
            "retention generations/version"
        );
        ensure!(
            self.keep_blocks > 128
                && self.ack.height.checked_sub(self.keep_blocks) == Some(self.through)
                && self.through > self.anchor.height
                && self.head.height >= self.tail.height
                && self.tail.height >= self.ack.height
                && self.head.height < i64::MAX as u64,
            "retention window/range"
        );
        ensure!(
            self.source_digest != B256::ZERO
                && self.head_root != B256::ZERO
                && [&self.anchor, &self.ack, &self.tail, &self.head]
                    .iter()
                    .all(|r| r.hash != B256::ZERO),
            "retention commitments"
        );
        Ok(())
    }
}
#[derive(Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Apply {
    pub request: Request,
    pub permit_digest: B256,
}
#[derive(Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Observation {
    pub head: Ref,
    pub root: B256,
    pub anchor_root: B256,
}
#[derive(Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
struct Floor {
    version: u8,
    permit_digest: B256,
    request: Request,
    checkpoint_digest: B256,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    history_floor: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    lookup_checkpoint: Option<PruneCheckpoint>,
}
const SNAPSHOT: &str = ".arc-retention-checkpoint.ndjson";
const FLOOR: &str = ".arc-retention-native.json";

pub fn inspect(f: &Factory, spec: &ArcChainSpec, r: &Request) -> Result<Observation> {
    let p = f.provider()?;
    ensure!(
        p.cached_storage_settings().storage_v2
            && r.chain.id == spec.inner.chain.id().to_string()
            && r.chain.genesis == spec.inner.genesis_hash()
            && p.block_hash(0)? == Some(r.chain.genesis),
        "native v2 chain identity"
    );
    let height = p.best_block_number()?;
    let head = p
        .header_by_number(height)?
        .ok_or_else(|| eyre!("completed native head missing"))?;
    let anchor = p
        .header_by_number(r.anchor.height)?
        .ok_or_else(|| eyre!("anchor missing"))?;
    ensure!(
        p.block_hash(r.anchor.height)? == Some(r.anchor.hash),
        "anchor canonicality"
    );
    let o = Observation {
        head: Ref {
            generation: 0,
            height,
            hash: head.hash_slow(),
        },
        root: head.state_root,
        anchor_root: anchor.state_root,
    };
    if r.head.hash != B256::ZERO {
        r.validate()?;
        ensure!(
            o.head == r.head && o.root == r.head_root,
            "native head changed"
        );
        for reference in [&r.ack, &r.tail] {
            ensure!(
                p.block_hash(reference.height)? == Some(reference.hash),
                "native ACK/tail canonicality"
            );
        }
        ensure!(
            crate::history::floor(f)? <= r.through,
            "protected state is below native history floor"
        );
        for n in [
            r.through,
            r.through + 1,
            r.ack.height,
            r.tail.height,
            height,
        ] {
            let h = p
                .header_by_number(n)?
                .ok_or_else(|| eyre!("protected state header missing"))?;
            let state = p.history_by_block_hash(h.hash_slow())?;
            ensure!(
                state.state_root(Default::default())? == h.state_root,
                "protected exact native state unavailable"
            );
        }
        for n in [0, r.through + 1, r.tail.height, height] {
            ensure!(
                p.block_by_number(n)?.is_some() && p.receipts_by_block(n.into())?.is_some(),
                "protected native body/receipts unavailable"
            );
        }
    }
    Ok(o)
}
// Select by native jar metadata, not filename substring/rm globs. A jar's
// complete fixed range must expire, and the highest jar is always retained.
fn expired(f: &Factory, datadir: &Path, r: &Request) -> Result<Vec<(StaticFileSegment, u64)>> {
    let sf = f.static_file_provider();
    let mut out = Vec::new();
    for entry in std::fs::read_dir(datadir.join("static_files"))? {
        let path = entry?.path();
        let Some(name) = path.file_name().and_then(|n| n.to_str()) else {
            continue;
        };
        let Some((segment, range)) = StaticFileSegment::parse_filename(name) else {
            continue;
        };
        if !matches!(
            segment,
            StaticFileSegment::Transactions
                | StaticFileSegment::Receipts
                | StaticFileSegment::TransactionSenders
                | StaticFileSegment::AccountChangeSets
                | StaticFileSegment::StorageChangeSets
        ) {
            continue;
        }
        let history = matches!(
            segment,
            StaticFileSegment::AccountChangeSets | StaticFileSegment::StorageChangeSets
        );
        if (history && range.end() >= r.through)
            || range.end() > r.through
            || sf
                .get_highest_static_file_block(segment)
                .is_none_or(|h| range.end() >= h)
        {
            continue;
        }
        if range.start() <= r.anchor.height {
            continue;
        }
        let jar = sf
            .get_segment_provider_for_path(&path)?
            .ok_or_else(|| eyre!("native jar metadata missing"))?;
        ensure!(
            jar.user_header().expected_block_range() == range,
            "native jar range mismatch"
        );
        if let Some(actual) = jar.user_header().block_range() {
            ensure!(
                actual.start() >= range.start() && actual.end() <= range.end(),
                "native actual jar bounds"
            );
        }
        out.push((segment, range.end()));
    }
    out.sort();
    out.dedup();
    Ok(out)
}
pub fn digest_file(path: &Path) -> Result<B256> {
    checkpoint::private_file(path)?;
    let mut f = std::fs::File::open(path)?;
    let mut h = Sha256::new();
    let mut b = [0u8; 65536];
    let mut total = 0u64;
    loop {
        let n = f.read(&mut b)?;
        if n == 0 {
            break;
        }
        total += n as u64;
        ensure!(total <= checkpoint::MAX_BYTES, "checkpoint byte limit");
        h.update(&b[..n]);
    }
    Ok(B256::from_slice(&h.finalize()))
}
pub fn prune(
    f: &Factory,
    spec: &ArcChainSpec,
    datadir: &Path,
    a: &Apply,
    workspace: &Path,
) -> Result<()> {
    a.request.validate()?;
    ensure!(a.permit_digest != B256::ZERO, "permit digest missing");
    // Only a copy explicitly sealed by the Go COW coordinator is writable.
    let owner = datadir.join(".execution-retention-copy");
    checkpoint::private_file(&owner)?;
    let owner: serde_json::Value = serde_json::from_reader(std::fs::File::open(owner)?)?;
    ensure!(
        owner["Data"]["PermitDigest"] == serde_json::to_value(a.permit_digest)?,
        "copy owner permit differs"
    );
    inspect(f, spec, &a.request)?;
    let previous = datadir.join(SNAPSHOT);
    let seed = if previous.exists() {
        Some(previous.as_path())
    } else {
        None
    };
    let next = datadir.join(".arc-retention-checkpoint-new.ndjson");
    native::export_checkpoint(f, spec, a.request.through, seed, &next, workspace)?;
    let digest = digest_file(&next)?;
    if previous.exists() {
        std::fs::remove_file(&previous)?;
    }
    std::fs::rename(&next, &previous)?;
    std::fs::File::open(datadir)?.sync_all()?;
    let sf = f.static_file_provider();
    // The complete cutoff seed is durable before any history is removed.
    let lookup_checkpoint = prune_lookups(f, datadir, &a.request)?;
    crate::history::prune(f, a.request.through)?;
    for (segment, end) in expired(f, datadir, &a.request)? {
        sf.delete_jar(segment, end)?;
    }
    // The native resume path must not try to regenerate deliberately pruned
    // payloads. Conservative boundary jars and all pre-anchor payload stay.
    let p = f.provider_rw()?;
    for segment in [
        PruneSegment::Bodies,
        PruneSegment::Receipts,
        PruneSegment::SenderRecovery,
    ] {
        if p.get_prune_checkpoint(segment)?
            .is_some_and(|c| c.block_number.is_some_and(|n| n > a.request.through))
        {
            continue;
        }
        p.save_prune_checkpoint(
            segment,
            PruneCheckpoint {
                block_number: Some(a.request.through),
                tx_number: None,
                prune_mode: PruneMode::Before(a.request.through + 1),
            },
        )?;
    }
    if let Some(checkpoint) = lookup_checkpoint {
        let previous = p.get_prune_checkpoint(PruneSegment::TransactionLookup)?;
        if previous.is_none_or(|old| old.block_number <= checkpoint.block_number) {
            p.save_prune_checkpoint(PruneSegment::TransactionLookup, checkpoint)?;
        }
    }
    p.commit()?;
    crate::history::compact(f)?;
    let floor = Floor {
        version: 3,
        permit_digest: a.permit_digest,
        request: a.request.clone(),
        checkpoint_digest: digest,
        history_floor: Some(a.request.through),
        lookup_checkpoint: f
            .provider()?
            .get_prune_checkpoint(PruneSegment::TransactionLookup)?,
    };
    let floorpath = datadir.join(FLOOR);
    if floorpath.exists() {
        checkpoint::private_file(&floorpath)?;
        std::fs::remove_file(&floorpath)?;
    }
    checkpoint::write_private(&floorpath, |w| {
        serde_json::to_writer(w, &floor)?;
        Ok(())
    })?;
    verify(f, spec, datadir, a, workspace)
}
pub fn verify(
    f: &Factory,
    spec: &ArcChainSpec,
    datadir: &Path,
    a: &Apply,
    workspace: &Path,
) -> Result<()> {
    a.request.validate()?;
    inspect(f, spec, &a.request)?;
    let path = datadir.join(FLOOR);
    checkpoint::private_file(&path)?;
    let floor: Floor = serde_json::from_reader(std::fs::File::open(path)?)?;
    ensure!(
        ((floor.version == 1
            && floor.history_floor.is_none()
            && floor.lookup_checkpoint.is_none())
            || (floor.version == 2
                && floor.history_floor == Some(a.request.through)
                && floor.lookup_checkpoint.is_none())
            || (floor.version == 3 && floor.history_floor == Some(a.request.through)))
            && floor.permit_digest == a.permit_digest
            && serde_json::to_vec(&floor.request)? == serde_json::to_vec(&a.request)?,
        "native floor request/permit mismatch"
    );
    if let Some(through) = floor.history_floor {
        crate::history::verify(f, through)?;
    }
    if floor.version == 3 {
        ensure!(
            f.provider()?
                .get_prune_checkpoint(PruneSegment::TransactionLookup)?
                == floor.lookup_checkpoint,
            "native lookup checkpoint differs from exact floor"
        );
        if let Some(cp) = floor.lookup_checkpoint {
            ensure!(
                cp.block_number
                    .is_some_and(|n| n > a.request.anchor.height && n <= a.request.through)
                    && cp.tx_number.is_some(),
                "native lookup checkpoint boundary"
            );
        }
        verify_lookups(f)?;
    }
    let snapshot = datadir.join(SNAPSHOT);
    ensure!(
        digest_file(&snapshot)? == floor.checkpoint_digest,
        "cutoff checkpoint digest differs"
    );
    let mut keys = crate::catalog::Builder::new(workspace)?;
    let m = checkpoint::seed_into(
        &snapshot,
        &a.request.chain.id,
        a.request.chain.genesis,
        &mut keys,
    )?;
    let keys = keys.finish()?;
    ensure!(m.r#ref.height == a.request.through, "cutoff seed height");
    let p = f.provider()?;
    ensure!(
        p.block_hash(m.r#ref.height)? == Some(m.r#ref.hash),
        "cutoff seed canonicality"
    );
    let h = p
        .header_by_number(m.r#ref.height)?
        .ok_or_else(|| eyre!("cutoff header"))?;
    let state = p.history_by_block_hash(m.r#ref.hash)?;
    let mut regenerated = DigestWriter(Sha256::new());
    checkpoint::export_catalog(
        &mut regenerated,
        &m.chain.id,
        m.chain.genesis,
        &h,
        &state,
        &keys,
    )?;
    ensure!(
        B256::from_slice(&regenerated.0.finalize()) == floor.checkpoint_digest,
        "cutoff seed values differ from exact retained state"
    );
    let remaining = expired(f, datadir, &a.request)?;
    ensure!(
        !remaining
            .iter()
            .any(|(segment, _)| floor.history_floor.is_some()
                || !matches!(
                    segment,
                    StaticFileSegment::AccountChangeSets | StaticFileSegment::StorageChangeSets
                )),
        "eligible native retention jar remains"
    );
    Ok(())
}
#[derive(Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct BackupApply {
    pub apply: Apply,
    pub visible: Ref,
    pub root: B256,
}
pub fn verify_backup(
    f: &Factory,
    spec: &ArcChainSpec,
    datadir: &Path,
    b: &BackupApply,
    workspace: &Path,
) -> Result<()> {
    verify(f, spec, datadir, &b.apply, workspace)?;
    ensure!(
        b.visible.generation == b.apply.request.ack.generation
            && b.visible.height >= b.apply.request.through
            && b.visible.height <= b.apply.request.head.height,
        "backup consumer generation/floor/head mismatch"
    );
    let p = f.provider()?;
    let header = p
        .header_by_number(b.visible.height)?
        .ok_or_else(|| eyre!("backup consumer header missing"))?;
    ensure!(
        header.hash_slow() == b.visible.hash && header.state_root == b.root,
        "backup indexer/native publication mismatch"
    );
    let state = p.history_by_block_hash(b.visible.hash)?;
    ensure!(
        state.state_root(Default::default())? == b.root,
        "backup consumer exact retained root unavailable"
    );
    Ok(())
}

pub fn retained_seed(datadir: &Path) -> Option<PathBuf> {
    let p = datadir.join(SNAPSHOT);
    p.exists().then_some(p)
}

/// Internal helper accepts only the sealed COW candidate for the current permit.
/// A cached file can never authorize pruning the operational native path.
pub fn candidate(datadir: &Path, a: &Apply) -> Result<()> {
    #[derive(Deserialize)]
    #[serde(rename_all = "PascalCase", deny_unknown_fields)]
    struct Sealed {
        data: Box<serde_json::value::RawValue>,
        digest: B256,
    }
    #[derive(Deserialize)]
    #[serde(rename_all = "PascalCase", deny_unknown_fields)]
    struct Intent {
        version: u8,
        permit: Box<serde_json::value::RawValue>,
        phase: String,
        original: serde_json::Value,
        replacement: serde_json::Value,
    }
    let name = datadir
        .file_name()
        .and_then(|v| v.to_str())
        .ok_or_else(|| eyre!("candidate name"))?;
    ensure!(
        name.starts_with('.') && name.ends_with(".execution-retention-new"),
        "native pruning requires an internal COW candidate"
    );
    let marker = datadir
        .parent()
        .ok_or_else(|| eyre!("candidate parent"))?
        .join(
            name.strip_suffix("-new")
                .ok_or_else(|| eyre!("candidate suffix"))?,
        );
    checkpoint::private_file(&marker)?;
    let mut bytes = Vec::new();
    std::fs::File::open(marker)?
        .take((2 << 20) + 1)
        .read_to_end(&mut bytes)?;
    ensure!(bytes.len() <= 2 << 20, "intent limit");
    let sealed: Sealed = serde_json::from_slice(&bytes)?;
    ensure!(
        B256::from_slice(&Sha256::digest(sealed.data.get().as_bytes())) == sealed.digest,
        "intent checksum"
    );
    let intent: Intent = serde_json::from_str(sealed.data.get())?;
    ensure!(
        intent.version == 1
            && intent.phase == "copy"
            && intent.original.is_object()
            && intent.replacement.is_object()
            && B256::from_slice(&Sha256::digest(intent.permit.get().as_bytes())) == a.permit_digest,
        "candidate live-operation binding"
    );
    let permit: serde_json::Value = serde_json::from_str(intent.permit.get())?;
    let request: Request = serde_json::from_value(permit["Request"].clone())?;
    ensure!(
        permit["Version"] == 1
            && permit["Coverage"] == "full"
            && permit["BaselineDigest"] != serde_json::to_value(B256::ZERO)?
            && serde_json::to_vec(&request)? == serde_json::to_vec(&a.request)?,
        "candidate full permit/request binding"
    );
    Ok(())
}

// Lookups are pruned before their payload jars, exactly as in the pinned
// Reth v2 TransactionLookup/Bodies dependency. Do not clear a whole table.
fn prune_lookups(f: &Factory, datadir: &Path, r: &Request) -> Result<Option<PruneCheckpoint>> {
    use alloy_eips::eip2718::Encodable2718;
    use reth_ethereum::provider::{RocksDBProviderFactory, TransactionsProvider, db::tables};
    let rocks = f.rocksdb_provider();
    let previous = f
        .provider()?
        .get_prune_checkpoint(PruneSegment::TransactionLookup)?;
    ensure!(
        previous.is_none_or(|cp| cp.block_number.is_none_or(|n| n <= r.through)),
        "native lookup checkpoint exceeds protected window"
    );
    let mut checkpoint = None;
    for (segment, end) in expired(f, datadir, r)? {
        if segment != StaticFileSegment::Transactions {
            continue;
        }
        let sf = f.static_file_provider();
        let jar = sf.get_segment_provider_for_block(segment, end, None)?;
        if let Some(range) = jar.user_header().tx_range() {
            checkpoint = Some(PruneCheckpoint {
                block_number: Some(end),
                tx_number: Some(range.end()),
                prune_mode: PruneMode::Before(end + 1),
            });
            let p = f.provider()?;
            let mut start = range.start();
            while start <= range.end() {
                let last = start.saturating_add(63).min(range.end());
                let txs = p.transactions_by_tx_range(start..=last)?;
                ensure!(
                    txs.len() as u64 == last - start + 1,
                    "expired native transaction gap"
                );
                let mut batch = rocks.batch();
                for (offset, tx) in txs.iter().enumerate() {
                    let hash = tx.trie_hash();
                    let number = start + offset as u64;
                    ensure!(
                        rocks
                            .get::<tables::TransactionHashNumbers>(hash)?
                            .is_none_or(|n| n == number),
                        "foreign transaction lookup"
                    );
                    batch.delete::<tables::TransactionHashNumbers>(hash)?;
                }
                batch.commit()?;
                start = last
                    .checked_add(1)
                    .ok_or_else(|| eyre!("native transaction overflow"))?;
            }
        }
    }
    Ok(checkpoint)
}

struct DigestWriter(Sha256);
impl std::io::Write for DigestWriter {
    fn write(&mut self, bytes: &[u8]) -> std::io::Result<usize> {
        self.0.update(bytes);
        Ok(bytes.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}
fn verify_lookups(f: &Factory) -> Result<()> {
    use alloy_eips::eip2718::Encodable2718;
    use reth_ethereum::provider::{RocksDBProviderFactory, TransactionsProvider, db::tables};
    let p = f.provider()?;
    for row in f
        .rocksdb_provider()
        .iter::<tables::TransactionHashNumbers>()?
    {
        let (hash, number) = row?;
        let tx = p
            .transaction_by_id(number)?
            .ok_or_else(|| eyre!("lookup references missing native transaction"))?;
        ensure!(
            tx.trie_hash() == hash,
            "native transaction lookup identity mismatch"
        );
    }
    Ok(())
}

/// Physical reclaim is restricted to the same sealed COW candidate as pruning.
/// The caller must drop every Factory/provider/static/secondary handle first.
pub fn compact_mdbx(datadir: &Path, a: &Apply) -> Result<()> {
    candidate(datadir, a)?;
    checkpoint::private_file(&datadir.join(".execution-retention-copy"))?;
    compact_mdbx_copy(datadir)
}
fn compact_mdbx_copy(datadir: &Path) -> Result<()> {
    compact_mdbx_phases(datadir, |_| Ok(()))
}
fn compact_mdbx_phases(datadir: &Path, phase: impl Fn(&str) -> Result<()>) -> Result<()> {
    use reth_libmdbx::{Environment, EnvironmentFlags, Mode, ffi};
    use std::os::fd::AsRawFd;
    let db = datadir.join("db");
    let data = db.join("mdbx.dat");
    let info = std::fs::symlink_metadata(&data)?;
    ensure!(info.is_file(), "regular MDBX data file required");
    let mut builder = Environment::builder();
    builder.set_max_dbs(256);
    builder.set_flags(EnvironmentFlags {
        mode: Mode::ReadOnly,
        exclusive: true,
        ..Default::default()
    });
    // Exclusive open fails if a native owner still holds this candidate.
    let env = builder.open(&db)?;
    let output = tempfile::Builder::new()
        .prefix(".mdbx-compact-")
        .tempfile_in(&db)?;
    // SAFETY: the environment pointer stays inside the closure, and output owns
    // its writable file descriptor for the whole synchronous native copy call.
    let code = env.with_raw_env_ptr(|ptr| unsafe {
        ffi::mdbx_env_copy2fd(ptr, output.as_file().as_raw_fd(), ffi::MDBX_CP_COMPACT)
    });
    ensure!(code == 0, "native MDBX compact-copy failed: {code}");
    phase("mdbx_copied")?;
    output.as_file().sync_all()?;
    phase("mdbx_durable")?;
    drop(env);
    phase("mdbx_closed")?;
    // Replacement is inside the disposable candidate only. The Go COW journal
    // still protects the original and discards this candidate on interruption.
    output.persist(&data)?;
    phase("mdbx_replaced")?;
    std::fs::File::open(&db)?.sync_all()?;
    phase("mdbx_directory_durable")?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use arc_execution_config::chainspec::TESTNET;
    use reth_ethereum::{
        provider::{
            ProviderFactory,
            db::{init_db, models::StoredBlockBodyIndices, tables, transaction::DbTxMut},
            providers::{RocksDBProvider, StaticFileProviderBuilder, StaticFileWriter},
        },
        tasks::Runtime,
    };
    use reth_stages_types::{StageCheckpoint, StageId};
    use reth_storage_api::StageCheckpointWriter;
    use std::{os::unix::fs::PermissionsExt, sync::Arc};
    #[test]
    fn native_matched_backup_fixture_export() -> Result<()> {
        let (dir, f, spec, a) = fixture_mode(true)?;
        ensure!(
            std::fs::metadata(dir.path().join("db/mdbx.dat"))?.len() <= 64 * 1024 * 1024,
            "owned native fixture must use bounded native test geometry"
        );
        inspect(&f, &spec, &a.request)?;
        if let Ok(output) = std::env::var("INDEXER_ARC_NATIVE_FIXTURE_DIR") {
            let output = Path::new(&output);
            ensure!(
                output.is_absolute() && !output.exists(),
                "new owned native fixture output required"
            );
            std::fs::create_dir(output)?;
            std::fs::set_permissions(output, std::fs::Permissions::from_mode(0o700))?;
            let p = f.provider()?;
            let refs = (1..=a.request.head.height)
                .map(|height| {
                    Ok(Ref {
                        generation: 1,
                        height,
                        hash: p
                            .block_hash(height)?
                            .ok_or_else(|| eyre!("native fixture header"))?,
                    })
                })
                .collect::<Result<Vec<_>>>()?;
            let anchor_root = p.header_by_number(0)?.unwrap().state_root;
            drop(p);
            native::export_checkpoint(&f, &spec, 0, None, &output.join("genesis.ndjson"), output)?;
            let metadata = serde_json::json!({"Chain":a.request.chain,"Anchor":a.request.anchor,"AnchorRoot":anchor_root,"Head":a.request.head,"HeadRoot":a.request.head_root,"Refs":refs});
            drop(f);
            fn copy(source: &Path, dest: &Path) -> Result<()> {
                std::fs::create_dir(dest)?;
                std::fs::set_permissions(dest, std::fs::Permissions::from_mode(0o700))?;
                for item in std::fs::read_dir(source)? {
                    let item = item?;
                    if item.file_type()?.is_dir() {
                        copy(&item.path(), &dest.join(item.file_name()))?;
                    } else {
                        ensure!(
                            item.file_type()?.is_file(),
                            "native fixture links forbidden"
                        );
                        std::fs::copy(item.path(), dest.join(item.file_name()))?;
                        std::fs::set_permissions(
                            dest.join(item.file_name()),
                            std::fs::Permissions::from_mode(0o600),
                        )?;
                    }
                }
                Ok(())
            }
            copy(dir.path(), &output.join("native"))?;
            checkpoint::write_private(&output.join("fixture.json"), |w| {
                serde_json::to_writer(w, &metadata)?;
                Ok(())
            })?;
        }
        Ok(())
    }
    #[test]
    fn detached_native_snapshot_pending_intent_forbids_start() -> Result<()> {
        let dir = tempfile::tempdir()?;
        native::check_start(dir.path())?;
        std::fs::write(
            dir.path().join(".execution-retention-pending"),
            b"pending matched snapshot",
        )?;
        assert!(native::check_start(dir.path()).is_err());
        Ok(())
    }
    #[test]
    fn native_mdbx_interrupted_copy_phases_reopen_and_retry() -> Result<()> {
        use reth_ethereum::provider::db::{Database, transaction::DbTx};
        for interrupted in [
            "mdbx_copied",
            "mdbx_durable",
            "mdbx_closed",
            "mdbx_replaced",
            "mdbx_directory_durable",
        ] {
            let dir = tempfile::tempdir()?;
            let db = init_db(dir.path().join("db"), Default::default())?;
            let tx = db.tx_mut()?;
            tx.put::<tables::HeaderNumbers>(B256::repeat_byte(7), 132)?;
            tx.commit()?;
            drop(db);
            assert!(
                compact_mdbx_phases(dir.path(), |at| {
                    ensure!(at != interrupted, "injected durable-phase interruption");
                    Ok(())
                })
                .is_err()
            );
            let db = reth_ethereum::provider::db::open_db_read_only(
                dir.path().join("db"),
                Default::default(),
            )?;
            assert_eq!(
                db.tx()?
                    .get::<tables::HeaderNumbers>(B256::repeat_byte(7))?,
                Some(132)
            );
            drop(db);
            compact_mdbx_copy(dir.path())?;
            let db = reth_ethereum::provider::db::open_db_read_only(
                dir.path().join("db"),
                Default::default(),
            )?;
            assert_eq!(db.tx()?.entries::<tables::HeaderNumbers>()?, 1);
        }
        Ok(())
    }
    #[test]
    fn native_sealed_candidate_compact_reopen_and_matched_consumer() -> Result<()> {
        let (dir, f, spec, mut a) = fixture_mode(true)?;
        drop(f);
        let entries = std::fs::read_dir(dir.path())?.collect::<std::io::Result<Vec<_>>>()?;
        let stage = dir.path().join(".native.execution-retention-new");
        std::fs::create_dir(&stage)?;
        std::fs::set_permissions(&stage, std::fs::Permissions::from_mode(0o700))?;
        for item in entries {
            std::fs::rename(item.path(), stage.join(item.file_name()))?;
        }
        let permit = serde_json::json!({"Version":1,"Coverage":"full","BaselineDigest":B256::repeat_byte(2),"Token":B256::repeat_byte(3),"Request":a.request,"Visible":a.request.ack,"Root":a.request.head_root});
        a.permit_digest = B256::from_slice(&Sha256::digest(serde_json::to_vec(&permit)?));
        let intent = serde_json::json!({"Version":1,"Permit":permit,"Phase":"copy","Original":{"Device":1,"Inode":2},"Replacement":{"Device":0,"Inode":0}});
        let digest = B256::from_slice(&Sha256::digest(serde_json::to_vec(&intent)?));
        let marker = dir.path().join(".native.execution-retention");
        checkpoint::write_private(&marker, |w| {
            serde_json::to_writer(w, &serde_json::json!({"Data":intent,"Digest":digest}))?;
            Ok(())
        })?;
        std::fs::write(
            stage.join(".execution-retention-copy"),
            serde_json::to_vec(&serde_json::json!({"Data":{"PermitDigest":a.permit_digest}}))?,
        )?;
        let f = native::open_writable(&stage, spec.clone())?;
        candidate(&stage, &a)?;
        prune(&f, &spec, &stage, &a, dir.path())?;
        assert!(
            compact_mdbx(&stage, &a).is_err(),
            "open native handles must prevent physical replacement"
        );
        drop(f);
        compact_mdbx(&stage, &a)?;
        let (f, secondary) = native::open_readonly(&stage, spec.clone(), true)?;
        rich_state(&f, 3, Some(3), [0, 9])?;
        rich_state(&f, 134, Some(130), [17, 0])?;
        let provider = f.provider()?;
        let state = provider.history_by_block_number(3)?;
        let root = provider.header_by_number(3)?.unwrap().state_root;
        crate::proofs::export(
            &state,
            root,
            &crate::proofs::Selection {
                targets: vec![crate::proofs::Target {
                    address: alloy_primitives::Address::repeat_byte(0xa1),
                    code: true,
                    slots: vec![B256::repeat_byte(1), B256::repeat_byte(2)],
                }],
            },
        )?;
        drop(state);
        drop(provider);
        let mut b = BackupApply {
            visible: a.request.ack.clone(),
            root: a.request.head_root,
            apply: a,
        };
        verify_backup(&f, &spec, &stage, &b, dir.path())?;
        b.root = B256::repeat_byte(99);
        assert!(verify_backup(&f, &spec, &stage, &b, dir.path()).is_err());
        b.root = b.apply.request.head_root;
        b.visible.height = b.apply.request.through - 1;
        assert!(verify_backup(&f, &spec, &stage, &b, dir.path()).is_err());
        drop(f);
        drop(secondary);
        compact_mdbx(&stage, &b.apply)?;
        let (f, _secondary) = native::open_readonly(&stage, spec.clone(), true)?;
        verify(&f, &spec, &stage, &b.apply, dir.path())?;
        Ok(())
    }
    #[test]
    fn native_cutoff_seed_values_must_equal_retained_state() -> Result<()> {
        let (dir, f, spec, a) = fixture_mode(true)?;
        prune(&f, &spec, dir.path(), &a, dir.path())?;
        let snapshot = dir.path().join(SNAPSHOT);
        let text = std::fs::read_to_string(&snapshot)?;
        let mut changed = false;
        let mut output = String::new();
        for line in text.lines() {
            let mut row: serde_json::Value = serde_json::from_str(line)?;
            if row["Kind"] == "account" && !changed {
                row["Account"]["Nonce"] = serde_json::json!(999);
                changed = true;
            }
            output.push_str(&serde_json::to_string(&row)?);
            output.push('\n');
        }
        assert!(changed, "fixture must contain nonempty full cutoff state");
        // Change the payload and its outer checksum together. Key coverage and
        // canonical header alone must not accept false checkpoint values.
        std::fs::write(&snapshot, output)?;
        let floorpath = dir.path().join(FLOOR);
        let mut floor: Floor = serde_json::from_reader(std::fs::File::open(&floorpath)?)?;
        floor.checkpoint_digest = digest_file(&snapshot)?;
        std::fs::write(&floorpath, serde_json::to_vec(&floor)?)?;
        assert!(
            verify(&f, &spec, dir.path(), &a, dir.path()).is_err(),
            "cutoff payload differs from exact native state"
        );
        Ok(())
    }
    #[test]
    fn native_expired_transaction_lookup_removed_retained_lookup_preserved() -> Result<()> {
        use reth_ethereum::provider::{RocksDBProviderFactory, TransactionsProvider};
        let (dir, f, spec, a) = fixture_mode(true)?;
        let p = f.provider()?;
        let old = *p.transaction_by_id(1)?.unwrap().hash(); // block 2, expired jar 2..3
        let boundary = *p.transaction_by_id(3)?.unwrap().hash(); // block 4, retained
        let pre_anchor = *p.transaction_by_id(0)?.unwrap().hash(); // first jar
        let head = *p.transaction_by_id(133)?.unwrap().hash();
        drop(p);
        prune(&f, &spec, dir.path(), &a, dir.path())?;
        let rocks = f.rocksdb_provider();
        let cp = f
            .provider()?
            .get_prune_checkpoint(PruneSegment::TransactionLookup)?
            .expect("lookup resume checkpoint");
        assert_eq!(cp.block_number, Some(3));
        assert_eq!(
            cp.tx_number,
            Some(2),
            "native restart must resume after the exact pruned transaction"
        );
        assert_eq!(
            rocks.get::<tables::TransactionHashNumbers>(old)?,
            None,
            "obsolete lookup must be deleted before its body jar"
        );
        assert_eq!(
            rocks.get::<tables::TransactionHashNumbers>(boundary)?,
            Some(3)
        );
        assert_eq!(
            rocks.get::<tables::TransactionHashNumbers>(pre_anchor)?,
            Some(0)
        );
        assert_eq!(
            rocks.get::<tables::TransactionHashNumbers>(head)?,
            Some(133)
        );
        verify(&f, &spec, dir.path(), &a, dir.path())?;
        Ok(())
    }
    #[test]
    fn native_mdbx_compact_copy_reopens_and_reclaims_free_pages() -> Result<()> {
        use reth_ethereum::provider::db::{Database, transaction::DbTx};
        let dir = tempfile::tempdir()?;
        let db = init_db(dir.path().join("db"), Default::default())?;
        let tx = db.tx_mut()?;
        for n in 0..100_000u64 {
            tx.put::<tables::HeaderNumbers>(B256::from(alloy_primitives::U256::from(n)), n)?;
        }
        tx.commit()?;
        let tx = db.tx_mut()?;
        tx.clear::<tables::HeaderNumbers>()?;
        tx.put::<tables::HeaderNumbers>(B256::repeat_byte(9), 132)?;
        tx.commit()?;
        drop(db);
        let data = dir.path().join("db/mdbx.dat");
        use std::os::unix::fs::MetadataExt;
        let before = std::fs::metadata(&data)?.blocks();
        compact_mdbx_copy(dir.path())?;
        assert!(
            std::fs::metadata(&data)?.blocks() < before,
            "compact-copy must reclaim MDBX free pages"
        );
        let db = reth_ethereum::provider::db::open_db_read_only(
            dir.path().join("db"),
            Default::default(),
        )?;
        let tx = db.tx()?;
        assert_eq!(
            tx.get::<tables::HeaderNumbers>(B256::repeat_byte(9))?,
            Some(132)
        );
        assert_eq!(tx.entries::<tables::HeaderNumbers>()?, 1);
        Ok(())
    }
    #[test]
    fn native_candidate_requires_sealed_copy_intent_and_exact_permit() -> Result<()> {
        let root = tempfile::tempdir()?;
        let stage = root.path().join(".native.execution-retention-new");
        std::fs::create_dir(&stage)?;
        let marker = root.path().join(".native.execution-retention");
        let permit = serde_json::json!({"Version":1,"Coverage":"full","BaselineDigest":B256::repeat_byte(1),"Request":{"Version":1,"Chain":{"ID":"5042002","Genesis":B256::repeat_byte(2)},"Anchor":Ref{generation:1,height:0,hash:B256::repeat_byte(2)},"ACK":Ref{generation:1,height:132,hash:B256::repeat_byte(3)},"Tail":Ref{generation:1,height:132,hash:B256::repeat_byte(3)},"Head":Ref{generation:0,height:132,hash:B256::repeat_byte(3)},"HeadRoot":B256::repeat_byte(4),"SourceDigest":B256::repeat_byte(5),"KeepBlocks":129,"Through":3}});
        let pd = B256::from_slice(&Sha256::digest(serde_json::to_vec(&permit)?));
        let request: Request = serde_json::from_value(permit["Request"].clone())?;
        let mut a = Apply {
            request,
            permit_digest: pd,
        };
        let data = serde_json::json!({"Version":1,"Phase":"copy","Permit":permit,"Original":{"Device":1,"Inode":2},"Replacement":{"Device":0,"Inode":0}});
        let digest = B256::from_slice(&Sha256::digest(serde_json::to_vec(&data)?));
        std::fs::write(
            &marker,
            serde_json::to_vec(&serde_json::json!({"Data":data,"Digest":digest}))?,
        )?;
        std::fs::set_permissions(&marker, std::fs::Permissions::from_mode(0o600))?;
        candidate(&stage, &a)?;
        a.permit_digest = B256::repeat_byte(9);
        assert!(candidate(&stage, &a).is_err());
        assert!(candidate(root.path(), &a).is_err());
        Ok(())
    }
    #[test]
    fn native_writer_probe() -> Result<()> {
        if let Ok(path) = std::env::var("INDEXER_ARC_NATIVE_WRITER_PROBE") {
            let path = Path::new(&path).canonicalize()?;
            ensure!(
                path.starts_with(std::env::temp_dir().canonicalize()?)
                    && path.join(".execution-retention-copy").is_file(),
                "owned native fixture required"
            );
            use reth_ethereum::provider::db::{
                DatabaseEnv, DatabaseEnvKind, mdbx::DatabaseArguments,
            };
            let opened = DatabaseEnv::open(
                &path.join("db"),
                DatabaseEnvKind::RW,
                DatabaseArguments::default(),
            );
            assert!(opened.is_err(), "normal native writer was not excluded");
        }
        Ok(())
    }
    fn fixture() -> Result<(tempfile::TempDir, Factory, Arc<ArcChainSpec>, Apply)> {
        fixture_mode(false)
    }
    fn fixture_mode(rich: bool) -> Result<(tempfile::TempDir, Factory, Arc<ArcChainSpec>, Apply)> {
        use alloy_consensus::{SignableTransaction, TxLegacy};
        use alloy_primitives::{Address, U256, keccak256};
        use reth_ethereum::provider::db::models::{AccountBeforeTx, StorageBeforeTx};
        use reth_primitives_traits::Account;
        use reth_primitives_traits::SignerRecoverable;
        use reth_storage_api::{StateWriter, TrieWriter};
        use reth_trie::{HashedPostState, HashedStorage};
        let dir = tempfile::tempdir()?;
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700))?;
        // Keep owned functional fixtures on the pinned library's durable test
        // geometry. Production defaults grow by 4 GiB and exhaust CI COW
        // headroom despite this fixture containing only 134 small blocks.
        let db = init_db(
            dir.path().join("db"),
            reth_ethereum::provider::db::mdbx::DatabaseArguments::test(),
        )?;
        let sf = StaticFileProviderBuilder::read_write(dir.path().join("static_files"))
            .with_blocks_per_file(2)
            .build()?;
        let rocks = RocksDBProvider::builder(dir.path().join("rocksdb"))
            .with_default_tables()
            .build()?;
        let spec = TESTNET.clone();
        let f: Factory = ProviderFactory::new(db, spec.clone(), sf, rocks, Runtime::test())?;
        reth_db_common::init::init_genesis(&f)?;
        let sf = f.static_file_provider();
        let genesis = spec.inner.genesis_header().clone();
        let mut parent = spec.inner.genesis_hash();
        let total = if rich { 134 } else { 132 };
        let (a_addr, b_addr) = (Address::repeat_byte(0xa1), Address::repeat_byte(0xb2));
        let slots = [B256::repeat_byte(1), B256::repeat_byte(2)];
        let (mut a_info, mut b_info) = (None, None);
        let mut values = [U256::ZERO; 2];
        let mut last_root = genesis.state_root;
        let mut ack_hash = B256::ZERO;
        for n in 1..=total {
            let mut account_reverts = Vec::new();
            let mut storage_reverts = Vec::new();
            let mut post = HashedPostState::default();
            if rich && [1, 2, 3, 4, 5, 130].contains(&n) {
                let info = (n != 5).then_some(Account {
                    nonce: n,
                    balance: U256::from(n * 11),
                    bytecode_hash: Some(keccak256([0x60, 0x01, 0x00])),
                });
                account_reverts.push(AccountBeforeTx {
                    address: a_addr,
                    info: a_info,
                });
                post.accounts.insert(keccak256(a_addr), info);
                let next = match n {
                    1 => [5, 0],
                    2 => [7, 9],
                    3 => [0, 9],
                    4 => [13, 9],
                    5 => [0, 0],
                    _ => [17, 0],
                }
                .map(U256::from);
                let mut hashed = HashedStorage {
                    wiped: n == 5 || n == 130,
                    ..Default::default()
                };
                for i in 0..2 {
                    if values[i] != next[i] {
                        storage_reverts.push(StorageBeforeTx {
                            address: a_addr,
                            key: slots[i],
                            value: values[i],
                        });
                        hashed.storage.insert(keccak256(slots[i]), next[i]);
                    }
                }
                post.storages.insert(keccak256(a_addr), hashed);
                values = next;
                a_info = info;
            }
            if rich && [1, 2].contains(&n) {
                let info = (n == 1).then_some(Account {
                    nonce: 1,
                    balance: U256::from(8),
                    bytecode_hash: None,
                });
                account_reverts.push(AccountBeforeTx {
                    address: b_addr,
                    info: b_info,
                });
                post.accounts.insert(keccak256(b_addr), info);
                b_info = info;
            }
            let updates = if !account_reverts.is_empty() || !storage_reverts.is_empty() {
                let (root, updates) = f.latest()?.state_root_with_updates(post.clone())?;
                last_root = root;
                Some(updates)
            } else {
                None
            };
            let tx: reth_ethereum_primitives::TransactionSigned = TxLegacy {
                chain_id: Some(5042002),
                nonce: n,
                gas_limit: 21000,
                gas_price: 1,
                ..Default::default()
            }
            .into_signed(alloy_primitives::Signature::test_signature())
            .into();
            let tx_hash = *tx.hash();
            let p = f.provider_rw()?;
            p.tx_ref().put::<tables::TransactionBlocks>(n - 1, n)?;
            p.with_rocksdb_batch(|mut batch| {
                batch.put::<tables::TransactionHashNumbers>(tx_hash, &(n - 1))?;
                Ok(((), Some(batch.into_inner())))
            })?;
            if rich {
                p.tx_ref().put::<tables::Bytecodes>(
                    keccak256([0x60, 0x01, 0x00]),
                    reth_primitives_traits::Bytecode(revm::bytecode::Bytecode::new_raw(
                        vec![0x60, 0x01, 0x00].into(),
                    )),
                )?;
            }
            if let Some(updates) = updates {
                p.write_hashed_state(&post.into_sorted())?;
                p.write_trie_updates(updates)?;
                p.with_rocksdb_batch(|mut batch| {
                    for change in &account_reverts {
                        batch.append_account_history_shard(change.address, vec![n])?;
                    }
                    for change in &storage_reverts {
                        batch.append_storage_history_shard(change.address, change.key, vec![n])?;
                    }
                    Ok(((), Some(batch.into_inner())))
                })?;
            }
            let mut h = genesis.clone();
            h.state_root = last_root;
            h.number = n;
            h.timestamp = genesis.timestamp + n;
            h.parent_hash = parent;
            parent = h.hash_slow();
            p.tx_ref().put::<tables::HeaderNumbers>(parent, n)?;
            p.tx_ref().put::<tables::BlockBodyIndices>(
                n,
                StoredBlockBodyIndices {
                    first_tx_num: n - 1,
                    tx_count: 1,
                },
            )?;
            {
                let mut w = sf.get_writer(n, StaticFileSegment::Headers)?;
                w.append_header(&h, &parent)?;
                w.commit()?;
            }
            for segment in [
                StaticFileSegment::Transactions,
                StaticFileSegment::Receipts,
                StaticFileSegment::TransactionSenders,
            ] {
                let mut w = sf.get_writer(n, segment)?;
                w.increment_block(n)?;
                match segment {
                    StaticFileSegment::Transactions => w.append_transaction(n - 1, &tx)?,
                    StaticFileSegment::Receipts => w.append_receipt(n - 1, &Default::default())?,
                    StaticFileSegment::TransactionSenders => {
                        w.append_transaction_sender(n - 1, &tx.recover_signer()?)?
                    }
                    _ => unreachable!(),
                }
                w.commit()?;
            }
            {
                let mut w = sf.get_writer(n, StaticFileSegment::AccountChangeSets)?;
                w.append_account_changeset(account_reverts, n)?;
                w.commit()?;
            }
            {
                let mut w = sf.get_writer(n, StaticFileSegment::StorageChangeSets)?;
                w.append_storage_changeset(storage_reverts, n)?;
                w.commit()?;
            }
            p.save_stage_checkpoint(StageId::Finish, StageCheckpoint::new(n))?;
            p.commit()?;
            if n == 132 {
                ack_hash = parent;
            }
        }
        let r = Request {
            version: 1,
            chain: checkpoint::Chain {
                id: spec.inner.chain.id().to_string(),
                genesis: spec.inner.genesis_hash(),
            },
            anchor: Ref {
                generation: 1,
                height: 0,
                hash: spec.inner.genesis_hash(),
            },
            ack: Ref {
                generation: 1,
                height: 132,
                hash: ack_hash,
            },
            tail: Ref {
                generation: 1,
                height: total,
                hash: parent,
            },
            head: Ref {
                generation: 0,
                height: total,
                hash: parent,
            },
            head_root: last_root,
            source_digest: B256::repeat_byte(3),
            keep_blocks: 129,
            through: 3,
        };
        let a = Apply {
            request: r,
            permit_digest: B256::repeat_byte(4),
        };
        checkpoint::write_private(&dir.path().join(".execution-retention-copy"), |w| {
            serde_json::to_writer(
                w,
                &serde_json::json!({"Data":{"PermitDigest":a.permit_digest}}),
            )?;
            Ok(())
        })?;
        Ok((dir, f, spec, a))
    }
    #[test]
    fn native_history_cutoff_is_enforced_and_old_jars_reclaimed() -> Result<()> {
        let (dir, f, spec, a) = fixture()?;
        prune(&f, &spec, dir.path(), &a, dir.path())?;
        let p = f.provider()?;
        for segment in [PruneSegment::AccountHistory, PruneSegment::StorageHistory] {
            assert_eq!(
                p.get_prune_checkpoint(segment)?
                    .and_then(|c| c.block_number),
                Some(a.request.through - 1),
                "history cutoff must be durable"
            );
        }
        use reth_storage_api::AccountReader;
        let state = p.history_by_block_number(a.request.through - 2)?;
        assert!(
            state
                .basic_account(&alloy_primitives::Address::ZERO)
                .is_err(),
            "expired native state must not masquerade as zero/current state"
        );
        Ok(())
    }
    #[test]
    fn native_selected_proofs_bind_retained_block_end_and_absence() -> Result<()> {
        use crate::proofs::{Selection, Target};
        use alloy_primitives::Address;
        let (_dir, f, _spec, _apply) = fixture_mode(true)?;
        let selection = Selection {
            targets: vec![Target {
                address: Address::repeat_byte(0xa1),
                code: true,
                slots: vec![B256::repeat_byte(1), B256::repeat_byte(2)],
            }],
        };
        for height in [3, 4, 5, 129, 130] {
            let p = f.provider()?;
            let header = p.header_by_number(height)?.unwrap();
            let state = p.history_by_block_number(height)?;
            let bytes = crate::proofs::export(&state, header.state_root, &selection)?;
            let value: serde_json::Value = serde_json::from_slice(&bytes)?;
            assert_eq!(value["version"], 1);
            assert_eq!(
                value["evidence"][0]["proof"]["nonce"],
                format!(
                    "0x{:x}",
                    if height == 3 || height == 4 || height == 130 {
                        height
                    } else {
                        0
                    }
                )
            );
            assert!(crate::proofs::export(&state, B256::repeat_byte(9), &selection).is_err());
        }
        Ok(())
    }
    fn rich_state(f: &Factory, height: u64, nonce: Option<u64>, slots: [u64; 2]) -> Result<()> {
        use alloy_primitives::{Address, U256};
        let p = f.provider()?;
        let state = p.history_by_block_number(height)?;
        let address = Address::repeat_byte(0xa1);
        let account = state.basic_account(&address)?;
        assert_eq!(account.map(|a| a.nonce), nonce, "account at{height}");
        if let Some(n) = nonce {
            assert_eq!(account.unwrap().balance, U256::from(n * 11));
            assert!(
                state
                    .bytecode_by_hash(&alloy_primitives::keccak256([0x60, 0x01, 0x00]))?
                    .is_some(),
                "retained account bytecode unavailable"
            );
        }
        for (i, expected) in slots.iter().enumerate() {
            assert_eq!(
                state
                    .storage(address, B256::repeat_byte(i as u8 + 1))?
                    .unwrap_or_default(),
                U256::from(*expected),
                "slot{i} at{height}"
            );
        }
        assert!(state.basic_account(&Address::repeat_byte(0xb2))?.is_none());
        assert_eq!(
            state.state_root(Default::default())?,
            p.header_by_number(height)?.unwrap().state_root
        );
        Ok(())
    }
    #[test]
    fn native_nonempty_history_survives_consecutive_cutoffs_and_reopen() -> Result<()> {
        let (dir, f, spec, a) = fixture_mode(true)?;
        let work = tempfile::tempdir()?;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let old = work.path().join("genesis.ndjson");
        let before = work.path().join("before3.ndjson");
        native::export_checkpoint(&f, &spec, 0, None, &old, work.path())?;
        native::export_checkpoint(&f, &spec, 3, None, &before, work.path())?;
        rich_state(&f, 3, Some(3), [0, 9])?;
        rich_state(&f, 4, Some(4), [13, 9])?;
        rich_state(&f, 5, None, [0, 0])?;
        rich_state(&f, 129, None, [0, 0])?;
        rich_state(&f, 130, Some(130), [17, 0])?;
        prune(&f, &spec, dir.path(), &a, work.path())?;
        assert_eq!(
            std::fs::read(&before)?,
            std::fs::read(dir.path().join(SNAPSHOT))?
        );
        assert!(crate::history::require(&f, 2).is_err());
        drop(f);
        if let Ok(binary) = std::env::var("INDEXER_ARC_EXECUTOR_BINARY") {
            let output = work.path().join("old-parent.exr");
            let result = std::process::Command::new(binary)
                .args([
                    "--chain",
                    "testnet",
                    "--datadir",
                    dir.path().to_str().unwrap(),
                    "--height",
                    "3",
                    "--generation",
                    "1",
                    "--output",
                    output.to_str().unwrap(),
                    "--scratch-dir",
                    work.path().to_str().unwrap(),
                ])
                .output()?;
            assert!(!result.status.success());
            assert!(String::from_utf8_lossy(&result.stderr).contains("history floor"));
            assert!(!output.exists());
        }
        let (f, secondary) = native::open_readonly(dir.path(), spec.clone(), true)?;
        verify(&f, &spec, dir.path(), &a, work.path())?;
        rich_state(&f, 3, Some(3), [0, 9])?;
        rich_state(&f, 4, Some(4), [13, 9])?;
        rich_state(&f, 5, None, [0, 0])?;
        rich_state(&f, 129, None, [0, 0])?;
        rich_state(&f, 134, Some(130), [17, 0])?;
        let rejected = work.path().join("stale.ndjson");
        let e = native::export_checkpoint(&f, &spec, 3, Some(&old), &rejected, work.path())
            .unwrap_err();
        assert!(e.to_string().contains("history floor"));
        assert!(!rejected.exists());
        let retained = dir.path().join(SNAPSHOT);
        native::export_checkpoint(
            &f,
            &spec,
            134,
            Some(&retained),
            &work.path().join("head.ndjson"),
            work.path(),
        )?;
        drop(f);
        drop(secondary);
        let f = native::open_writable(dir.path(), spec.clone())?;
        let mut next = Apply {
            request: a.request.clone(),
            permit_digest: B256::repeat_byte(8),
        };
        next.request.ack.height = 133;
        next.request.ack.hash = f.provider()?.block_hash(133)?.unwrap();
        next.request.through = 4;
        assert!(
            expired(&f, dir.path(), &next.request)?
                .contains(&(StaticFileSegment::AccountChangeSets, 3))
        );
        std::fs::write(
            dir.path().join(".execution-retention-copy"),
            serde_json::to_vec(&serde_json::json!({"Data":{"PermitDigest":next.permit_digest}}))?,
        )?;
        prune(&f, &spec, dir.path(), &next, work.path())?;
        let digest = digest_file(&dir.path().join(SNAPSHOT))?;
        prune(&f, &spec, dir.path(), &next, work.path())?;
        assert_eq!(digest_file(&dir.path().join(SNAPSHOT))?, digest);
        assert!(crate::history::require(&f, 3).is_err());
        rich_state(&f, 4, Some(4), [13, 9])?;
        drop(f);
        let (f, _secondary) = native::open_readonly(dir.path(), spec.clone(), true)?;
        verify(&f, &spec, dir.path(), &next, work.path())?;
        rich_state(&f, 4, Some(4), [13, 9])?;
        rich_state(&f, 134, Some(130), [17, 0])?;
        Ok(())
    }
    #[test]
    fn native_v1_payload_floor_remains_readable() -> Result<()> {
        let (dir, f, spec, mut a) = fixture_mode(true)?;
        a.request.ack.height = 133;
        a.request.ack.hash = f.provider()?.block_hash(133)?.unwrap();
        a.request.through = 4;
        assert!(
            expired(&f, dir.path(), &a.request)?
                .contains(&(StaticFileSegment::AccountChangeSets, 3))
        );
        let snapshot = dir.path().join(SNAPSHOT);
        native::export_checkpoint(&f, &spec, a.request.through, None, &snapshot, dir.path())?;
        let floor = Floor {
            version: 1,
            permit_digest: a.permit_digest,
            request: a.request.clone(),
            checkpoint_digest: digest_file(&snapshot)?,
            history_floor: None,
            lookup_checkpoint: None,
        };
        for (segment, end) in expired(&f, dir.path(), &a.request)? {
            if matches!(
                segment,
                StaticFileSegment::Transactions
                    | StaticFileSegment::Receipts
                    | StaticFileSegment::TransactionSenders
            ) {
                f.static_file_provider().delete_jar(segment, end)?;
            }
        }
        checkpoint::write_private(&dir.path().join(FLOOR), |w| {
            serde_json::to_writer(w, &floor)?;
            Ok(())
        })?;
        verify(&f, &spec, dir.path(), &a, dir.path())?;
        Ok(())
    }
    #[test]
    fn native_v2_checkpoint_retention_reopens_with_current_state_and_window() -> Result<()> {
        let (dir, f, spec, a) = fixture()?;
        inspect(&f, &spec, &a.request)?;
        assert!(!expired(&f, dir.path(), &a.request)?.is_empty());
        prune(&f, &spec, dir.path(), &a, dir.path())?;
        assert!(expired(&f, dir.path(), &a.request)?.is_empty());
        let before = a.request.head_root;
        let original_seed = digest_file(&dir.path().join(SNAPSHOT))?;
        drop(f);
        let (f, _work) = native::open_readonly(dir.path(), spec.clone(), true)?;
        verify(&f, &spec, dir.path(), &a, dir.path())?;
        assert_eq!(inspect(&f, &spec, &a.request)?.root, before);
        assert_eq!(digest_file(&dir.path().join(SNAPSHOT))?, original_seed);
        let sf = f.static_file_provider();
        let _ = sf.get_segment_provider_for_block(StaticFileSegment::Transactions, 0, None)?;
        let _ = sf.get_segment_provider_for_block(StaticFileSegment::Transactions, 4, None)?;
        drop(sf);
        drop(f);
        if let Ok(binary) = std::env::var("INDEXER_ARC_MAINTENANCE_BINARY") {
            use std::{
                io::{BufRead, BufReader, Write},
                process::{Command, Stdio},
            };
            let args = [
                "--chain",
                "testnet",
                "--datadir",
                dir.path().to_str().unwrap(),
                "--operation",
            ];
            let run = |op: &str, body: &[u8]| -> Result<()> {
                let mut child = Command::new(&binary)
                    .args(args)
                    .arg(op)
                    .stdin(Stdio::piped())
                    .stdout(Stdio::piped())
                    .stderr(Stdio::piped())
                    .spawn()?;
                child.stdin.take().unwrap().write_all(body)?;
                let result = child.wait_with_output()?;
                ensure!(
                    result.status.success(),
                    "native CLI failed: {}",
                    String::from_utf8_lossy(&result.stderr)
                );
                Ok(())
            };
            let mut body = serde_json::to_vec(&a)?;
            body.push(b'\n');
            run("verify", &body)?;
            let mut child = Command::new(&binary)
                .args(args)
                .arg("hold")
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::piped())
                .spawn()?;
            let mut input = child.stdin.take().unwrap();
            serde_json::to_writer(&mut input, &a.request)?;
            input.write_all(b"\n")?;
            input.flush()?;
            let mut ready = String::new();
            BufReader::new(child.stdout.take().unwrap()).read_line(&mut ready)?;
            let observed: Observation = serde_json::from_str(&ready)?;
            assert_eq!(observed.head, a.request.head);
            assert!(
                child.try_wait()?.is_none(),
                "hold child exited before exclusion check"
            );
            let mut probe = Command::new(&binary)
                .args(args)
                .arg("inspect")
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::piped())
                .spawn()?;
            let mut request = serde_json::to_vec(&a.request)?;
            request.push(b'\n');
            probe.stdin.take().unwrap().write_all(&request)?;
            let result = probe.wait_with_output()?;
            assert!(
                !result.status.success(),
                "exclusive native maintenance allowed a separate helper: {}",
                String::from_utf8_lossy(&result.stdout)
            );
            let writer = Command::new(std::env::current_exe()?)
                .args([
                    "--exact",
                    "retention::tests::native_writer_probe",
                    "--nocapture",
                ])
                .env("INDEXER_ARC_NATIVE_WRITER_PROBE", dir.path())
                .output()?;
            assert!(
                writer.status.success(),
                "native exclusive hold allowed a normal writer: {}",
                String::from_utf8_lossy(&writer.stdout)
            );
            drop(input);
            assert!(child.wait()?.success());
        }
        Ok(())
    }
}
