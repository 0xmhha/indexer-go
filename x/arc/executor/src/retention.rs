//! Conservative native-v2 payload retention. State/history/headers stay intact.
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
        for n in [r.through, r.tail.height, height] {
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
        ) {
            continue;
        }
        if range.end() > r.through
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
pub fn prune(f: &Factory, spec: &ArcChainSpec, datadir: &Path, a: &Apply) -> Result<()> {
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
    native::export_checkpoint(f, spec, a.request.through, seed, &next)?;
    let digest = digest_file(&next)?;
    if previous.exists() {
        std::fs::remove_file(&previous)?;
    }
    std::fs::rename(&next, &previous)?;
    std::fs::File::open(datadir)?.sync_all()?;
    let sf = f.static_file_provider();
    // No history pruning: preserve raw-key recovery and native reorg history.
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
    p.commit()?;
    let floor = Floor {
        version: 1,
        permit_digest: a.permit_digest,
        request: a.request.clone(),
        checkpoint_digest: digest,
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
    verify(f, spec, datadir, a)
}
pub fn verify(f: &Factory, spec: &ArcChainSpec, datadir: &Path, a: &Apply) -> Result<()> {
    a.request.validate()?;
    inspect(f, spec, &a.request)?;
    let path = datadir.join(FLOOR);
    checkpoint::private_file(&path)?;
    let floor: Floor = serde_json::from_reader(std::fs::File::open(path)?)?;
    ensure!(
        floor.version == 1
            && floor.permit_digest == a.permit_digest
            && serde_json::to_vec(&floor.request)? == serde_json::to_vec(&a.request)?,
        "native floor request/permit mismatch"
    );
    let snapshot = datadir.join(SNAPSHOT);
    ensure!(
        digest_file(&snapshot)? == floor.checkpoint_digest,
        "cutoff checkpoint digest differs"
    );
    let (m, keys) = checkpoint::seed(&snapshot, &a.request.chain.id, a.request.chain.genesis)?;
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
    checkpoint::export(
        std::io::sink(),
        &m.chain.id,
        m.chain.genesis,
        &h,
        &state,
        &keys,
    )?;
    ensure!(
        expired(f, datadir, &a.request)?.is_empty(),
        "eligible native payload jar remains"
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
        let dir = tempfile::tempdir()?;
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700))?;
        let db = init_db(dir.path().join("db"), Default::default())?;
        let sf = StaticFileProviderBuilder::read_write(dir.path().join("static_files"))
            .with_blocks_per_file(2)
            .build()?;
        let rocks = RocksDBProvider::builder(dir.path().join("rocksdb"))
            .with_default_tables()
            .build()?;
        let spec = TESTNET.clone();
        let f: Factory = ProviderFactory::new(db, spec.clone(), sf, rocks, Runtime::test())?;
        reth_db_common::init::init_genesis(&f)?;
        let p = f.provider_rw()?;
        let sf = f.static_file_provider();
        let genesis = spec.inner.genesis_header().clone();
        let mut parent = spec.inner.genesis_hash();
        for n in 1..=132 {
            let mut h = genesis.clone();
            h.number = n;
            h.timestamp = genesis.timestamp + n;
            h.parent_hash = parent;
            parent = h.hash_slow();
            p.tx_ref().put::<tables::HeaderNumbers>(parent, n)?;
            p.tx_ref().put::<tables::BlockBodyIndices>(
                n,
                StoredBlockBodyIndices {
                    first_tx_num: 0,
                    tx_count: 0,
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
                w.commit()?;
            }
            {
                let mut w = sf.get_writer(n, StaticFileSegment::AccountChangeSets)?;
                w.append_account_changeset(vec![], n)?;
                w.commit()?;
            }
            {
                let mut w = sf.get_writer(n, StaticFileSegment::StorageChangeSets)?;
                w.append_storage_changeset(vec![], n)?;
                w.commit()?;
            }
        }
        p.save_stage_checkpoint(StageId::Finish, StageCheckpoint::new(132))?;
        p.commit()?;
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
                hash: parent,
            },
            tail: Ref {
                generation: 1,
                height: 132,
                hash: parent,
            },
            head: Ref {
                generation: 0,
                height: 132,
                hash: parent,
            },
            head_root: genesis.state_root,
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
    fn native_v2_checkpoint_retention_reopens_with_current_state_and_window() -> Result<()> {
        let (dir, f, spec, a) = fixture()?;
        inspect(&f, &spec, &a.request)?;
        assert!(!expired(&f, dir.path(), &a.request)?.is_empty());
        prune(&f, &spec, dir.path(), &a)?;
        assert!(expired(&f, dir.path(), &a.request)?.is_empty());
        let before = a.request.head_root;
        let original_seed = digest_file(&dir.path().join(SNAPSHOT))?;
        drop(f);
        let (f, _work) = native::open_readonly(dir.path(), spec.clone(), true)?;
        verify(&f, &spec, dir.path(), &a)?;
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
