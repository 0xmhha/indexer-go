//! Offline full-state bootstrap. No network, node service or SQL startup.
use crate::{
    checkpoint::{self, FullRow, Manifest},
    native, native_import,
};
use alloy_consensus::{Header, ReceiptWithBloom, TxReceipt, proofs};
use alloy_primitives::{B256, U256, keccak256};
use alloy_rlp::Decodable;
use arc_execution_config::chainspec::ArcChainSpec;
use base64::{Engine, engine::general_purpose::STANDARD};
use eyre::{Result, ensure, eyre};
use reth_ethereum::{
    provider::{
        BlockHashReader, BlockNumReader, ProviderFactory, StaticFileProviderFactory,
        StaticFileSegment, StaticFileWriter,
        db::{init_db, mdbx::DatabaseArguments, tables, transaction::DbTxMut},
        providers::{RocksDBProvider, StaticFileProvider},
    },
    tasks::Runtime,
};
use reth_ethereum_primitives::{Block, Receipt};
use reth_primitives_traits::{Account, Bytecode, SignerRecoverable, StorageEntry};
use reth_prune_types::{PruneCheckpoint, PruneMode, PruneSegment};
use reth_stages_types::{StageCheckpoint, StageId};
use reth_storage_api::{
    BlockBodyWriter, BlockReader, PruneCheckpointWriter, ReceiptProvider, StageCheckpointWriter,
    StateRootProvider, TrieWriter,
};
use reth_trie::{StateRoot, StateRootProgress};
use reth_trie_db::{
    DatabaseHashedCursorFactory, DatabaseStateRoot, DatabaseTrieCursorFactory, PackedKeyAdapter,
};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    fs::{File, OpenOptions},
    io::{BufRead, BufReader, Read, Write},
    os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt},
    path::{Path, PathBuf},
    sync::Arc,
};

pub const RECEIPT: &str = ".arc-native-bootstrap.json";
pub const PENDING: &str = ".arc-native-bootstrap.pending";
pub struct Inputs<'a> {
    pub target: &'a Path,
    pub state: &'a Path,
    /// Independently trusted commitment; never inferred from state itself.
    pub expected: &'a Path,
    /// NDJSON {"Header":"base64 exact header RLP"}, contiguous 0..H.
    pub headers: &'a Path,
    pub block: &'a Path,
    pub receipts: &'a Path,
}
#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct ReceiptRecord {
    pub version: u8,
    pub arc_revision: String,
    pub reth_revision: String,
    pub anchor: Manifest,
    pub floor: u64,
    pub next: u64,
    pub state_digest: B256,
    pub headers_digest: Option<B256>,
    pub block_digest: Option<B256>,
    pub receipts_digest: Option<B256>,
}

pub fn build(input: &Inputs<'_>) -> Result<Manifest> {
    build_with_spec(input, arc_execution_config::chainspec::TESTNET.clone())
}
fn private_dir(path: &Path) -> Result<File> {
    ensure!(path.is_absolute(), "absolute private directory required");
    let fd = rustix::fs::open(
        path,
        rustix::fs::OFlags::DIRECTORY
            | rustix::fs::OFlags::NOFOLLOW
            | rustix::fs::OFlags::CLOEXEC
            | rustix::fs::OFlags::RDONLY,
        rustix::fs::Mode::empty(),
    )?;
    let f = File::from(fd);
    let m = f.metadata()?;
    ensure!(
        m.uid() == rustix::process::geteuid().as_raw() && m.mode() & 0o777 == 0o700,
        "owned private directory required"
    );
    Ok(f)
}
// Pin source through NOFOLLOW and copy into our private stage. All validation
// and native writes use only that copy. Original files are never modified.
fn copy_private(source: &Path, dest: &Path, limit: u64) -> Result<B256> {
    checkpoint::private_file(source)?;
    let mut f = OpenOptions::new()
        .read(true)
        .custom_flags(rustix::fs::OFlags::NOFOLLOW.bits() as i32)
        .open(source)?;
    let before = f.metadata()?;
    ensure!(
        before.is_file()
            && before.nlink() == 1
            && before.mode() & 0o777 == 0o600
            && before.uid() == rustix::process::geteuid().as_raw()
            && before.len() <= limit,
        "private input bounds/identity"
    );
    let mut out = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(dest)?;
    let mut hash = Sha256::new();
    let mut total = 0u64;
    let mut buf = [0u8; 64 << 10];
    loop {
        let n = f.read(&mut buf)?;
        if n == 0 {
            break;
        }
        total = total
            .checked_add(n as u64)
            .ok_or_else(|| eyre!("input overflow"))?;
        ensure!(total <= limit, "input byte limit");
        out.write_all(&buf[..n])?;
        hash.update(&buf[..n]);
    }
    let after = f.metadata()?;
    ensure!(
        total == before.len()
            && before.dev() == after.dev()
            && before.ino() == after.ino()
            && before.len() == after.len()
            && before.mtime() == after.mtime()
            && before.mtime_nsec() == after.mtime_nsec()
            && before.ctime() == after.ctime()
            && before.ctime_nsec() == after.ctime_nsec(),
        "input changed during copy"
    );
    out.sync_all()?;
    Ok(B256::from_slice(&hash.finalize()))
}
fn factory(stage: &Path, spec: Arc<ArcChainSpec>) -> Result<native::Factory> {
    let db = init_db(
        stage.join("db"),
        DatabaseArguments::default().with_exclusive(Some(true)),
    )?;
    let sf = StaticFileProvider::read_write(stage.join("static_files"))?;
    let rocks = RocksDBProvider::builder(stage.join("rocksdb"))
        .with_default_tables()
        .build()?;
    let f = ProviderFactory::new(db, spec, sf, rocks, Runtime::test())?;
    reth_db_common::init::init_genesis(&f)?;
    Ok(f)
}
fn flush(f: &native::Factory, rows: &mut Vec<FullRow>) -> Result<()> {
    let p = f.provider_rw()?;
    for row in rows.drain(..) {
        match row {
            FullRow::Account(address, a, code) => {
                p.tx_ref().put::<tables::HashedAccounts>(
                    keccak256(address),
                    Account {
                        nonce: a.nonce,
                        balance: U256::from_be_bytes(a.balance.0),
                        bytecode_hash: (a.code_hash != keccak256([])).then_some(a.code_hash),
                    },
                )?;
                if !code.is_empty() {
                    p.tx_ref().put::<tables::Bytecodes>(
                        a.code_hash,
                        Bytecode::new_raw_checked(code.into())?,
                    )?;
                }
            }
            FullRow::Storage(address, slot, value) => {
                p.tx_ref().put::<tables::HashedStorages>(
                    keccak256(address),
                    StorageEntry {
                        key: keccak256(slot),
                        value: U256::from_be_bytes(value.0),
                    },
                )?;
            }
        }
    }
    p.commit()?;
    Ok(())
}
fn root(f: &native::Factory) -> Result<B256> {
    type Calc<'a, TX> = StateRoot<
        DatabaseTrieCursorFactory<&'a TX, PackedKeyAdapter>,
        DatabaseHashedCursorFactory<&'a TX>,
    >;
    let mut progress = None;
    loop {
        let p = f.provider_rw()?;
        match Calc::from_tx(p.tx_ref())
            .with_intermediate_state(progress.take())
            .root_with_progress()?
        {
            StateRootProgress::Progress(state, _, updates) => {
                p.write_trie_updates(updates)?;
                progress = Some(*state);
                p.commit()?;
            }
            StateRootProgress::Complete(root, _, updates) => {
                p.write_trie_updates(updates)?;
                p.commit()?;
                return Ok(root);
            }
        }
    }
}
fn anchor_artifacts(
    f: &native::Factory,
    stage: &Path,
    spec: &ArcChainSpec,
    m: &Manifest,
) -> Result<()> {
    #[derive(Deserialize)]
    #[serde(rename_all = "PascalCase", deny_unknown_fields)]
    struct Row {
        header: String,
    }
    let mut input = BufReader::new(File::open(stage.join("headers.ndjson"))?);
    let mut parent = B256::ZERO;
    let sf = f.static_file_provider();
    for n in 0..=m.r#ref.height {
        let mut line = Vec::new();
        (&mut input)
            .take((checkpoint::MAX_RECORD + 1) as u64)
            .read_until(b'\n', &mut line)?;
        ensure!(
            !line.is_empty() && line.len() <= checkpoint::MAX_RECORD,
            "header archive missing/bounds"
        );
        let row: Row = serde_json::from_slice(&line)?;
        let raw = STANDARD.decode(row.header)?;
        let mut raw = raw.as_slice();
        let h = Header::decode(&mut raw)?;
        let hash = h.hash_slow();
        ensure!(
            raw.is_empty()
                && h.number == n
                && h.parent_hash == parent
                && (n != 0 || hash == spec.inner.genesis_hash()),
            "header archive chain mismatch"
        );
        if n > 0 {
            let p = f.provider_rw()?;
            p.tx_ref().put::<tables::HeaderNumbers>(hash, n)?;
            let mut w = sf.get_writer(n, StaticFileSegment::Headers)?;
            w.append_header(&h, &hash)?;
            w.commit()?;
            drop(w);
            p.commit()?;
        }
        parent = hash;
    }
    ensure!(
        input.fill_buf()?.is_empty() && parent == m.r#ref.hash,
        "header archive anchor/trailing bytes"
    );
    let raw = std::fs::read(stage.join("anchor.rlp"))?;
    let block: Block = alloy_rlp::decode_exact(&raw)?;
    let raw = std::fs::read(stage.join("anchor.receipts.rlp"))?;
    let receipts: Vec<ReceiptWithBloom<Receipt>> = alloy_rlp::decode_exact(&raw)?;
    ensure!(
        block.header.hash_slow() == m.r#ref.hash
            && block.header.state_root == m.root
            && proofs::calculate_transaction_root(&block.body.transactions)
                == block.header.transactions_root
            && proofs::calculate_ommers_root(&block.body.ommers) == block.header.ommers_hash
            && receipts.len() == block.body.transactions.len()
            && proofs::calculate_receipt_root(&receipts) == block.header.receipts_root,
        "anchor block/receipt commitments"
    );
    ensure!(
        match (&block.body.withdrawals, block.header.withdrawals_root) {
            (Some(w), Some(r)) => proofs::calculate_withdrawals_root(w) == r,
            (None, None) => true,
            _ => false,
        },
        "anchor withdrawals commitment"
    );
    let mut bloom = alloy_primitives::Bloom::ZERO;
    let mut previous_gas = 0;
    for (r, t) in receipts.iter().zip(&block.body.transactions) {
        use alloy_consensus::Typed2718;
        ensure!(
            r.receipt.bloom() == r.logs_bloom
                && r.receipt.ty() == t.ty()
                && r.receipt.cumulative_gas_used() >= previous_gas,
            "anchor receipt bloom/type/gas"
        );
        bloom |= r.logs_bloom;
        previous_gas = r.receipt.cumulative_gas_used();
    }
    ensure!(
        bloom == block.header.logs_bloom && previous_gas == block.header.gas_used,
        "anchor bloom/gas"
    );
    let n = m.r#ref.height;
    let p = f.provider_rw()?;
    let storage: reth_ethereum::provider::EthStorage = Default::default();
    storage.write_block_bodies(&*p, vec![(n, Some(&block.body))])?;
    p.tx_ref().put::<tables::BlockBodyIndices>(
        n,
        reth_ethereum::provider::db::models::StoredBlockBodyIndices {
            first_tx_num: 0,
            tx_count: block.body.transactions.len() as u64,
        },
    )?;
    if !block.body.transactions.is_empty() {
        p.tx_ref()
            .put::<tables::TransactionBlocks>((block.body.transactions.len() - 1) as u64, n)?;
        p.with_rocksdb_batch(|mut batch| {
            for (i, transaction) in block.body.transactions.iter().enumerate() {
                batch.put::<tables::TransactionHashNumbers>(*transaction.hash(), &(i as u64))?;
            }
            Ok(((), Some(batch.into_inner())))
        })?;
    }
    for segment in [
        StaticFileSegment::Transactions,
        StaticFileSegment::Receipts,
        StaticFileSegment::TransactionSenders,
    ] {
        // Keep static segment geometry contiguous without creating earlier body
        // indices, transactions or receipts. Those bodies remain unavailable.
        for height in 1..=n {
            let mut w = sf.get_writer(height, segment)?;
            w.increment_block(height)?;
            if height == n {
                for (i, t) in block.body.transactions.iter().enumerate() {
                    match segment {
                        StaticFileSegment::Transactions => w.append_transaction(i as u64, t)?,
                        StaticFileSegment::Receipts => {
                            w.append_receipt(i as u64, &receipts[i].receipt)?
                        }
                        StaticFileSegment::TransactionSenders => {
                            w.append_transaction_sender(i as u64, &t.recover_signer()?)?
                        }
                        _ => unreachable!(),
                    }
                }
            }
            w.commit()?;
        }
    }
    // Native-v2 appends reverts sequentially. These empty segments are only
    // physical positioning for H+1; the immutable floor rejects every pre-H
    // state query. They are not reconstructed pre-anchor execution history.
    for height in 1..=n {
        let mut w = sf.get_writer(height, StaticFileSegment::AccountChangeSets)?;
        w.append_account_changeset(Vec::new(), height)?;
        w.commit()?;
        drop(w);
        let mut w = sf.get_writer(height, StaticFileSegment::StorageChangeSets)?;
        w.append_storage_changeset(Vec::new(), height)?;
        w.commit()?;
    }
    for id in [StageId::Headers, StageId::Bodies, StageId::Finish] {
        p.save_stage_checkpoint(id, StageCheckpoint::new(n))?;
    }
    for segment in [PruneSegment::AccountHistory, PruneSegment::StorageHistory] {
        p.save_prune_checkpoint(
            segment,
            PruneCheckpoint {
                block_number: Some(n - 1),
                tx_number: None,
                prune_mode: PruneMode::Before(n),
            },
        )?;
    }
    p.commit()?;
    Ok(())
}
fn sync_tree(path: &Path) -> Result<()> {
    for entry in std::fs::read_dir(path)? {
        let entry = entry?;
        let m = entry.file_type()?;
        if m.is_dir() {
            sync_tree(&entry.path())?;
        } else {
            ensure!(m.is_file(), "unexpected staged object");
            File::open(entry.path())?.sync_all()?;
        }
    }
    File::open(path)?.sync_all()?;
    Ok(())
}
fn build_with_spec(input: &Inputs<'_>, spec: Arc<ArcChainSpec>) -> Result<Manifest> {
    ensure!(input.target.is_absolute(), "absolute new target required");
    let parent = input
        .target
        .parent()
        .ok_or_else(|| eyre!("target parent"))?;
    let parent_fd = private_dir(parent)?;
    let name = input
        .target
        .file_name()
        .ok_or_else(|| eyre!("target name"))?;
    ensure!(
        matches!(
            rustix::fs::statat(&parent_fd, name, rustix::fs::AtFlags::SYMLINK_NOFOLLOW),
            Err(rustix::io::Errno::NOENT)
        ),
        "target exists or cannot be inspected; never overwrite"
    );
    // Keep failed stages for explicit inspection; never clear a partial native
    // build by TTL, retry, latest-state substitution or automatic destructive reset.
    let temp = tempfile::Builder::new()
        .prefix(".arc-native-bootstrap-")
        .tempdir_in(parent)?;
    std::fs::set_permissions(temp.path(), std::fs::Permissions::from_mode(0o700))?;
    let stage: PathBuf = temp.keep();
    checkpoint::write_private(&stage.join(PENDING), |w| {
        w.write_all(b"incomplete native bootstrap\n")?;
        Ok(())
    })?;
    let state_digest = copy_private(
        input.state,
        &stage.join(".arc-retention-checkpoint.ndjson"),
        checkpoint::MAX_BYTES,
    )?;
    copy_private(
        input.expected,
        &stage.join("expected.json"),
        native_import::MAX_META as u64,
    )?;
    let expected = checkpoint::read_expected(File::open(stage.join("expected.json"))?)?;
    ensure!(
        expected.chain.id == spec.inner.chain.id().to_string()
            && expected.chain.genesis == spec.inner.genesis_hash(),
        "fixed Arc network identity"
    );
    let (headers_digest, block_digest, receipts_digest) = if expected.r#ref.height > 0 {
        (
            Some(copy_private(
                input.headers,
                &stage.join("headers.ndjson"),
                checkpoint::MAX_BYTES,
            )?),
            Some(copy_private(
                input.block,
                &stage.join("anchor.rlp"),
                native_import::MAX_BLOCK as u64,
            )?),
            Some(copy_private(
                input.receipts,
                &stage.join("anchor.receipts.rlp"),
                native_import::MAX_RECEIPTS as u64,
            )?),
        )
    } else {
        (None, None, None)
    };
    let f = factory(&stage, spec.clone())?;
    {
        let p = f.provider_rw()?;
        p.tx_ref().clear::<tables::HashedAccounts>()?;
        p.tx_ref().clear::<tables::HashedStorages>()?;
        p.tx_ref().clear::<tables::PackedAccountsTrie>()?;
        p.tx_ref().clear::<tables::PackedStoragesTrie>()?;
        p.commit()?;
    }
    let mut rows = Vec::new();
    let mut bytes = 0usize;
    let m = checkpoint::read_full(
        BufReader::new(File::open(stage.join(".arc-retention-checkpoint.ndjson"))?),
        &expected,
        |r| {
            bytes += match &r {
                FullRow::Account(_, _, code) => 256 + code.len(),
                FullRow::Storage(..) => 96,
            };
            rows.push(r);
            if rows.len() >= 256 || bytes >= 4 << 20 {
                flush(&f, &mut rows)?;
                bytes = 0;
            }
            Ok(())
        },
    )?;
    flush(&f, &mut rows)?;
    ensure!(root(&f)? == m.root, "persisted native full root mismatch");
    if m.r#ref.height > 0 {
        anchor_artifacts(&f, &stage, &spec, &m)?;
    }
    drop(f);
    {
        let (f, _scratch) = native::open_readonly(&stage, spec, true)?;
        ensure!(
            f.provider()?.best_block_number()? == m.r#ref.height
                && f.provider()?.block_hash(m.r#ref.height)? == Some(m.r#ref.hash)
                && f.latest()?.state_root(Default::default())? == m.root
                && crate::history::floor(&f)? == m.r#ref.height,
            "reopened bootstrap identity/root/floor mismatch"
        );
        if m.r#ref.height > 0 {
            let p = f.provider()?;
            let block = p
                .block(m.r#ref.height.into())?
                .ok_or_else(|| eyre!("reopened anchor body missing"))?;
            let receipts = p
                .receipts_by_block(m.r#ref.height.into())?
                .ok_or_else(|| eyre!("reopened anchor receipts missing"))?;
            ensure!(
                alloy_rlp::encode(&block) == std::fs::read(stage.join("anchor.rlp"))?
                    && receipts.len() == block.body.transactions.len()
                    && proofs::calculate_receipt_root(
                        &receipts
                            .iter()
                            .map(|r| r.with_bloom_ref())
                            .collect::<Vec<_>>()
                    ) == block.header.receipts_root,
                "reopened anchor body/receipt mismatch"
            );
        }
    }
    let receipt = ReceiptRecord {
        version: 1,
        arc_revision: crate::ARC_REVISION.into(),
        reth_revision: native_import::RETH_REVISION.into(),
        floor: m.r#ref.height,
        next: m.r#ref.height + 1,
        anchor: m.clone(),
        state_digest,
        headers_digest,
        block_digest,
        receipts_digest,
    };
    checkpoint::write_private(&stage.join(RECEIPT), |w| {
        serde_json::to_writer(w, &receipt)?;
        Ok(())
    })?;
    std::fs::remove_file(stage.join(PENDING))?;
    sync_tree(&stage)?;
    let current = private_dir(parent)?;
    ensure!(
        current.metadata()?.dev() == parent_fd.metadata()?.dev()
            && current.metadata()?.ino() == parent_fd.metadata()?.ino(),
        "target parent changed"
    );
    rustix::fs::renameat_with(
        &parent_fd,
        stage.file_name().ok_or_else(|| eyre!("stage name"))?,
        &parent_fd,
        name,
        rustix::fs::RenameFlags::NOREPLACE,
    )?;
    parent_fd.sync_all()?;
    Ok(m)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{native, native_import::tests::native as fixture};
    use reth_ethereum::provider::HeaderProvider;
    use reth_storage_api::StateRootProvider;
    use std::os::unix::fs::PermissionsExt;

    #[test]
    fn bootstrap_strictness_does_not_change_legacy_wire_decoding() -> Result<()> {
        let value = serde_json::json!({"Generation":1,"Height":0,"Hash":B256::repeat_byte(1),"LegacyExtension":true});
        let decoded: crate::wire::Ref = serde_json::from_value(value)?;
        assert_eq!(decoded.generation, 1);
        Ok(())
    }

    #[test]
    fn bootstrap_full_values_and_strict_stream_are_authenticated() -> Result<()> {
        let (_source, f, spec) = fixture(true)?;
        let work = tempfile::tempdir()?;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let path = work.path().join("full.ndjson");
        native::export_checkpoint(&f, &spec, 0, None, &path, work.path())?;
        let bytes = std::fs::read(path)?;
        let lines = bytes
            .split(|b| *b == b'\n')
            .filter(|s| !s.is_empty())
            .map(serde_json::from_slice)
            .collect::<std::result::Result<Vec<serde_json::Value>, _>>()?;
        let expected: Manifest = serde_json::from_value(lines[0]["Manifest"].clone())?;
        checkpoint::read_full(std::io::Cursor::new(&bytes), &expected, |_| Ok(()))?;
        let mut without_header = expected.clone();
        without_header.header.clear();
        checkpoint::read_full(std::io::Cursor::new(&bytes), &without_header, |_| Ok(()))?;
        let a = lines.iter().position(|r| r["Kind"] == "account").unwrap();
        let code = lines
            .iter()
            .position(|r| r["Kind"] == "account" && r["Code"] != "")
            .unwrap();
        let slot = lines.iter().position(|r| r["Kind"] == "storage").unwrap();
        for case in 0..17 {
            let mut rows = lines.clone();
            match case {
                0 => rows[a]["Account"]["Nonce"] = serde_json::json!(999),
                1 => rows[code]["Code"] = serde_json::json!("AA=="),
                2 => rows[slot]["Value"] = serde_json::to_value(B256::repeat_byte(3))?,
                3 => {
                    rows.remove(slot);
                }
                4 => {
                    rows.insert(a, rows[a].clone());
                }
                5 => rows[a]["Account"]["Unexpected"] = serde_json::json!(1),
                6 => rows[0]["Manifest"]["Ref"]["Unexpected"] = serde_json::json!(1),
                7 => rows[0]["Manifest"]["Coverage"] = serde_json::json!("partial"),
                8 => rows[slot]["Value"] = serde_json::to_value(B256::ZERO)?,
                9 => rows[a]["Proof"] = serde_json::json!([]),
                10 => {
                    rows.pop();
                }
                11 => rows.push(serde_json::json!({"Kind":"end"})),
                12 => rows[a]["Slot"] = serde_json::to_value(B256::ZERO)?,
                13 => {
                    rows[a].as_object_mut().unwrap().remove("Code");
                }
                14 => rows[a]["Account"]["Extra"] = serde_json::json!(1),
                15 => rows[a]["Account"]["Exists"] = serde_json::json!(false),
                _ => {
                    rows.remove(a);
                }
            }
            let mut raw = Vec::new();
            for row in rows {
                serde_json::to_writer(&mut raw, &row)?;
                raw.push(b'\n');
            }
            assert!(
                checkpoint::read_full(std::io::Cursor::new(raw), &expected, |_| Ok(())).is_err(),
                "case {case}"
            );
        }
        let mut bad = expected.clone();
        bad.root = B256::repeat_byte(8);
        assert!(checkpoint::read_full(std::io::Cursor::new(&bytes), &bad, |_| Ok(())).is_err());
        let duplicate = String::from_utf8(bytes)?.replacen(
            "\"Kind\":\"manifest\"",
            "\"Kind\":\"manifest\",\"Kind\":\"manifest\"",
            1,
        );
        assert!(
            checkpoint::read_full(std::io::Cursor::new(duplicate), &expected, |_| Ok(())).is_err()
        );
        Ok(())
    }

    #[test]
    fn bootstrap_invalid_state_stays_private_fenced_and_inputs_survive() -> Result<()> {
        let (_source, f, spec) = fixture(false)?;
        let work = tempfile::tempdir()?;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let state = work.path().join("state.ndjson");
        native::export_checkpoint(&f, &spec, 0, None, &state, work.path())?;
        let (mut m, _) = checkpoint::seed(&state, "5042002", spec.inner.genesis_hash())?;
        m.root = B256::repeat_byte(8);
        let expected = work.path().join("expected.json");
        checkpoint::write_private(&expected, |w| {
            serde_json::to_writer(w, &m)?;
            Ok(())
        })?;
        let before = std::fs::read(&state)?;
        let target = work.path().join("target");
        let absent = work.path().join("absent");
        let inputs = Inputs {
            target: &target,
            state: &state,
            expected: &expected,
            headers: &absent,
            block: &absent,
            receipts: &absent,
        };
        assert!(build(&inputs).is_err());
        assert!(!target.exists());
        let stages = std::fs::read_dir(work.path())?.collect::<std::result::Result<Vec<_>, _>>()?;
        let stage = stages
            .iter()
            .find(|p| {
                p.file_name()
                    .to_string_lossy()
                    .starts_with(".arc-native-bootstrap-")
            })
            .unwrap()
            .path();
        assert!(stage.join(PENDING).is_file());
        assert!(!stage.join(RECEIPT).exists());
        assert!(native::check_start(&stage).is_err());
        assert_eq!(std::fs::read(&state)?, before);
        std::os::unix::fs::symlink(&stage, &target)?;
        assert!(build(&inputs).is_err());
        assert!(target.is_symlink());
        assert_eq!(std::fs::read(&state)?, before);
        Ok(())
    }

    #[test]
    fn bootstrap_genesis_is_verified_reopened_and_never_overwrites() -> Result<()> {
        let (source, f, spec) = fixture(false)?;
        let work = tempfile::tempdir()?;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let state = work.path().join("state.ndjson");
        native::export_checkpoint(&f, &spec, 0, None, &state, work.path())?;
        let (m, _) = crate::checkpoint::seed(&state, "5042002", spec.inner.genesis_hash())?;
        let expected = work.path().join("expected.json");
        crate::checkpoint::write_private(&expected, |w| {
            serde_json::to_writer(w, &m)?;
            Ok(())
        })?;
        let target = work.path().join("new-native");
        let absent = work.path().join("unused-genesis-artifacts");
        let inputs = Inputs {
            target: &target,
            state: &state,
            expected: &expected,
            headers: &absent,
            block: &absent,
            receipts: &absent,
        };
        let result = build(&inputs)?;
        assert_eq!(result.r#ref, m.r#ref);
        let (g, scratch) = native::open_readonly(&target, spec.clone(), true)?;
        assert_eq!(
            g.provider()?.header_by_number(0)?.unwrap().state_root,
            m.root
        );
        assert_eq!(g.latest()?.state_root(Default::default())?, m.root);
        drop(g);
        drop(scratch);
        assert!(build(&inputs).is_err(), "existing target must survive");
        assert!(source.path().join("db").exists());
        Ok(())
    }

    #[test]
    fn bootstrap_checkpoint_preserves_code_slots_floor_and_executes_h_plus_one() -> Result<()> {
        checkpoint_at(1)?;
        checkpoint_at(3)
    }

    #[test]
    fn bootstrap_nonempty_anchor_withdrawals_round_trip() -> Result<()> {
        use reth_storage_api::BlockReader;
        // Storage-only synthetic authenticated anchor, not a claim that this
        // header has consensus/first-execution validity. Bootstrap trusts the
        // separately authenticated commitment instead of reexecuting the anchor.
        let (_source, f, spec) = fixture(false)?;
        let work = tempfile::tempdir()?;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let original = work.path().join("original.ndjson");
        native::export_checkpoint(&f, &spec, 0, None, &original, work.path())?;
        let (mut m, _) = checkpoint::seed(&original, "5042002", spec.inner.genesis_hash())?;
        let genesis = f.provider()?.header_by_number(0)?.unwrap();
        let withdrawals =
            alloy_eips::eip4895::Withdrawals::new(vec![alloy_eips::eip4895::Withdrawal {
                index: 0,
                validator_index: 0,
                address: alloy_primitives::Address::repeat_byte(8),
                amount: 1,
            }]);
        let header = Header {
            number: 1,
            parent_hash: genesis.hash_slow(),
            timestamp: genesis.timestamp + 1,
            withdrawals_root: Some(proofs::calculate_withdrawals_root(&withdrawals)),
            ..genesis.clone()
        };
        let block = Block {
            header: header.clone(),
            body: alloy_consensus::BlockBody {
                withdrawals: Some(withdrawals),
                ..Default::default()
            },
        };
        m.mode = "checkpoint".into();
        m.r#ref.height = 1;
        m.r#ref.hash = header.hash_slow();
        m.header = STANDARD.encode(alloy_rlp::encode(&header));
        let state = work.path().join("state.ndjson");
        checkpoint::write_private(&state, |w| {
            let raw = std::fs::read(original)?;
            let end = raw.iter().position(|b| *b == b'\n').unwrap() + 1;
            serde_json::to_writer(
                &mut *w,
                &serde_json::json!({"Kind":"manifest","Manifest":m}),
            )?;
            w.write_all(b"\n")?;
            w.write_all(&raw[end..])?;
            Ok(())
        })?;
        let expected = work.path().join("expected.json");
        checkpoint::write_private(&expected, |w| {
            serde_json::to_writer(w, &m)?;
            Ok(())
        })?;
        let headers = work.path().join("headers.ndjson");
        checkpoint::write_private(&headers, |w| {
            for h in [&genesis, &header] {
                serde_json::to_writer(
                    &mut *w,
                    &serde_json::json!({"Header":STANDARD.encode(alloy_rlp::encode(h))}),
                )?;
                w.write_all(b"\n")?;
            }
            Ok(())
        })?;
        let block_path = work.path().join("block.rlp");
        checkpoint::write_private(&block_path, |w| {
            w.write_all(&alloy_rlp::encode(&block))?;
            Ok(())
        })?;
        let receipts = work.path().join("receipts.rlp");
        checkpoint::write_private(&receipts, |w| {
            w.write_all(&[0xc0])?;
            Ok(())
        })?;
        let target = work.path().join("native");
        build(&Inputs {
            target: &target,
            state: &state,
            expected: &expected,
            headers: &headers,
            block: &block_path,
            receipts: &receipts,
        })?;
        let (g, _scratch) = native::open_readonly(&target, spec, true)?;
        assert_eq!(
            alloy_rlp::encode(g.provider()?.block(1.into())?.unwrap()),
            alloy_rlp::encode(block)
        );
        Ok(())
    }
    fn checkpoint_at(height: u64) -> Result<()> {
        use crate::native_import::tests::next;
        use alloy_eips::eip2718::Encodable2718;
        use reth_ethereum::provider::SaveBlocksMode;
        use reth_storage_api::{BlockBodyIndicesProvider, BlockHashReader, BlockReader};
        let (_source, f, spec) = fixture(true)?;
        let mut raw = Vec::new();
        let mut receipt_bytes = Vec::new();
        for parent in 0..height {
            let block = next(&f, spec.clone(), parent)?;
            if parent + 1 == height {
                raw = alloy_rlp::encode(block.recovered_block().clone().into_block());
                let mut payload = Vec::new();
                for r in &block.execution_output.result.receipts {
                    r.with_bloom_ref().network_encode(&mut payload);
                }
                alloy_rlp::Header {
                    list: true,
                    payload_length: payload.len(),
                }
                .encode(&mut receipt_bytes);
                receipt_bytes.extend(payload);
            }
            let p = f.provider_rw()?;
            p.save_blocks(vec![block], SaveBlocksMode::Full)?;
            p.commit()?;
        }
        let work = tempfile::tempdir()?;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let state = work.path().join("state.ndjson");
        native::export_checkpoint(&f, &spec, height, None, &state, work.path())?;
        let (m, _) = crate::checkpoint::seed(&state, "5042002", spec.inner.genesis_hash())?;
        let expected = work.path().join("expected.json");
        crate::checkpoint::write_private(&expected, |w| {
            serde_json::to_writer(w, &m)?;
            Ok(())
        })?;
        let headers = work.path().join("headers.ndjson");
        crate::checkpoint::write_private(&headers, |w| {
            for n in 0..=height {
                let h = f.provider()?.header_by_number(n)?.unwrap();
                serde_json::to_writer(
                    &mut *w,
                    &serde_json::json!({"Header":STANDARD.encode(alloy_rlp::encode(h))}),
                )?;
                w.write_all(b"\n")?;
            }
            Ok(())
        })?;
        let anchor = work.path().join("anchor.rlp");
        crate::checkpoint::write_private(&anchor, |w| {
            w.write_all(&raw)?;
            Ok(())
        })?;
        let receipts = work.path().join("receipts.rlp");
        crate::checkpoint::write_private(&receipts, |w| {
            w.write_all(&receipt_bytes)?;
            Ok(())
        })?;
        let target = work.path().join("native");
        let input = Inputs {
            target: &target,
            state: &state,
            expected: &expected,
            headers: &headers,
            block: &anchor,
            receipts: &receipts,
        };
        if height == 1 {
            let broken = work.path().join("broken-headers.ndjson");
            checkpoint::write_private(&broken, |w| {
                w.write_all(b"{\"Header\":\"wA==\"}\n")?;
                Ok(())
            })?;
            let bad_target = work.path().join("bad-native");
            let bad = Inputs {
                target: &bad_target,
                headers: &broken,
                ..input
            };
            assert!(build_with_spec(&bad, spec.clone()).is_err());
            assert!(!bad_target.exists());
            let empty = work.path().join("empty-receipts.rlp");
            checkpoint::write_private(&empty, |w| {
                w.write_all(&[0xc0])?;
                Ok(())
            })?;
            let bad = Inputs {
                target: &bad_target,
                receipts: &empty,
                ..input
            };
            assert!(build_with_spec(&bad, spec.clone()).is_err());
            assert!(!bad_target.exists());
        }
        build_with_spec(&input, spec.clone())?;
        let g = native::open_writable(&target, spec.clone())?;
        assert_eq!(crate::history::floor(&g)?, height);
        assert!(crate::history::require(&g, 0).is_err());
        assert_eq!(g.provider()?.block_hash(height)?, Some(m.r#ref.hash));
        assert!(g.provider()?.block_body_indices(height)?.is_some());
        assert_eq!(
            alloy_rlp::encode(g.provider()?.block(height.into())?.unwrap()),
            raw,
            "native anchor body must preserve the actual canonical bytes"
        );
        if height > 1 {
            assert!(g.provider()?.block_body_indices(height - 1)?.is_none());
        }
        let two = next(&g, spec.clone(), height)?;
        let expected_two = next(&f, spec.clone(), height)?;
        assert_eq!(
            two.recovered_block().hash(),
            expected_two.recovered_block().hash()
        );
        let p = g.provider_rw()?;
        p.save_blocks(vec![two], SaveBlocksMode::Full)?;
        p.commit()?;
        drop(g);
        let (g, _scratch) = native::open_readonly(&target, spec.clone(), true)?;
        assert_eq!(
            g.provider()?.block_hash(height + 1)?,
            Some(expected_two.recovered_block().hash())
        );
        assert_eq!(
            g.latest()?.state_root(Default::default())?,
            expected_two.recovered_block().header().state_root
        );
        let exported = work.path().join("reexport.ndjson");
        native::export_checkpoint(
            &g,
            &spec,
            height,
            Some(&target.join(".arc-retention-checkpoint.ndjson")),
            &exported,
            work.path(),
        )?;
        let (exported_manifest, _) =
            crate::checkpoint::seed(&exported, "5042002", m.chain.genesis)?;
        assert_eq!(exported_manifest, m);
        let record: ReceiptRecord = serde_json::from_reader(File::open(target.join(RECEIPT))?)?;
        assert_eq!(record.anchor.r#ref.height, height);
        assert_eq!(record.floor, height);
        assert_eq!(record.next, height + 1);
        assert_eq!(
            std::fs::read(target.join(".arc-retention-checkpoint.ndjson"))?,
            std::fs::read(state)?
        );
        Ok(())
    }
}
