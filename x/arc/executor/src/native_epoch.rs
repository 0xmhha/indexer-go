//! Explicit coordinator-bound native epoch change, never publication or ACK.
use crate::{
    native_writer::{self as writer, Head, Identity, NativeRef},
    wire::Ref,
};
use alloy_primitives::B256;
use eyre::{Result, ensure, eyre};
use reth_storage_api::BlockExecutionWriter;
use serde::{Deserialize, Serialize};
use std::{
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
};

pub const EPOCHS: &str = ".arc-native-import-epochs";
const MAX_LINEAGE: u64 = 128;

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Rewind {
    pub request_generation: u64,
    pub from: Ref,
    pub target: Ref,
    pub from_root: B256,
    pub root: B256,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Request {
    pub version: u8,
    pub identity: Identity,
    pub previous: Head,
    pub marker: Rewind,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Transition {
    pub version: u8,
    pub identity: Identity,
    pub previous: Head,
    pub marker: Rewind,
    pub head: Head,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
struct Intent {
    version: u8,
    transition: Transition,
}

pub fn path(native: &Path, epoch: u64) -> PathBuf {
    native.join(EPOCHS).join(format!("{epoch}.json"))
}
fn positive(n: u64) -> bool {
    n > 0 && n < i64::MAX as u64
}
fn absent(path: &Path) -> Result<()> {
    match std::fs::symlink_metadata(path) {
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(e.into()),
        Ok(_) => eyre::bail!("unexpected pending/future native artifact"),
    }
}
pub(crate) fn valid_head(h: &Head, i: &Identity) -> Result<()> {
    ensure!(
        h.version == 1
            && h.instance == i.instance
            && positive(h.import_epoch)
            && positive(h.read_epoch)
            && h.request_id < i64::MAX as u64
            && h.tip.height >= i.anchor.height
            && h.tip.height < i64::MAX as u64
            && h.tip.hash != B256::ZERO
            && h.tip.root != B256::ZERO,
        "native head range/identity"
    );
    Ok(())
}
fn valid_request(r: &Request) -> Result<()> {
    valid_head(&r.previous, &r.identity)?;
    let m = &r.marker;
    ensure!(
        r.version == 1
            && positive(m.request_generation)
            && positive(m.from.generation)
            && m.from.generation <= m.request_generation
            && m.target.generation == m.request_generation + 1
            && positive(m.target.generation)
            && m.from.height < i64::MAX as u64
            && m.target.height >= r.identity.anchor.height
            && m.target.height <= m.from.height
            && m.target.height <= r.previous.tip.height
            && m.from.hash != B256::ZERO
            && m.from_root != B256::ZERO
            && m.target.hash != B256::ZERO
            && m.root != B256::ZERO,
        "coordinator rewind marker/range"
    );
    Ok(())
}
fn expected(r: &Request) -> Result<Transition> {
    valid_request(r)?;
    let epoch = r
        .previous
        .import_epoch
        .checked_add(1)
        .filter(|n| *n <= MAX_LINEAGE)
        .ok_or_else(|| eyre!("native epoch lineage limit"))?;
    let read_epoch = r
        .previous
        .read_epoch
        .checked_add(1)
        .filter(|n| positive(*n))
        .ok_or_else(|| eyre!("native read epoch exhausted"))?;
    Ok(Transition {
        version: 1,
        identity: r.identity.clone(),
        previous: r.previous.clone(),
        marker: r.marker.clone(),
        head: Head {
            version: 1,
            instance: r.identity.instance,
            import_epoch: epoch,
            read_epoch,
            request_id: 0,
            tip: NativeRef {
                height: r.marker.target.height,
                hash: r.marker.target.hash,
                root: r.marker.root,
            },
        },
    })
}
fn validate_transition(t: &Transition, i: &Identity) -> Result<()> {
    ensure!(
        t.version == 1
            && &t.identity == i
            && *t
                == expected(&Request {
                    version: 1,
                    identity: i.clone(),
                    previous: t.previous.clone(),
                    marker: t.marker.clone()
                })?,
        "native epoch transition binding"
    );
    Ok(())
}
fn check_sequence(h: &Head, base: &Head, i: &Identity) -> Result<()> {
    valid_head(h, i)?;
    ensure!(
        h.import_epoch == base.import_epoch
            && h.tip.height >= base.tip.height
            && h.request_id == h.tip.height - base.tip.height
            && base.read_epoch.checked_add(h.request_id) == Some(h.read_epoch)
            && (h.request_id != 0 || h.tip == base.tip),
        "native epoch-relative sequence"
    );
    Ok(())
}
fn initial(i: &Identity) -> Head {
    Head {
        version: 1,
        instance: i.instance,
        import_epoch: 1,
        read_epoch: 1,
        request_id: 0,
        tip: i.anchor.clone(),
    }
}

/// Resolve original receipt epoch through immutable bounded rewind ancestry.
pub(crate) fn receipt_context(native: &Path, i: &Identity, h: &Head, height: u64) -> Result<Head> {
    ensure!(
        h.import_epoch <= MAX_LINEAGE && height <= h.tip.height,
        "native lineage/reference limit"
    );
    let mut cursor = h.clone();
    let mut selected = None;
    let mut newer_request = None;
    loop {
        if cursor.import_epoch == 1 {
            let base = initial(i);
            check_sequence(&cursor, &base, i)?;
            return Ok(selected.unwrap_or(base));
        }
        writer::directory(&native.join(EPOCHS))?;
        let t: Transition = writer::artifact(&path(native, cursor.import_epoch))?;
        validate_transition(&t, i)?;
        ensure!(
            newer_request.is_none_or(|g| t.marker.target.generation <= g),
            "coordinator generations decreased across native epochs"
        );
        newer_request = Some(t.marker.request_generation);
        check_sequence(&cursor, &t.head, i)?;
        if selected.is_none() && height > t.head.tip.height {
            selected = Some(t.head.clone());
        }
        cursor = t.previous;
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Phase {
    Intent,
    Unwound,
    Committed,
    Receipt,
    Head,
    Cleared,
}
pub fn transition(native: &Path, request: &Request) -> Result<Transition> {
    transition_with_hook(native, request, &mut |_| Ok(()))
}
pub fn serve_transition(
    mut input: impl std::io::Read,
    mut output: impl std::io::Write,
    native: &Path,
) -> Result<()> {
    let mut header = [0_u8; 8];
    input.read_exact(&mut header)?;
    let size = u32::from_be_bytes(header[4..].try_into()?) as usize;
    ensure!(
        &header[..4] == b"ANT1" && size > 0 && size <= crate::native_import::MAX_META,
        "bounded ANT1 transition required"
    );
    let mut bytes = vec![0; size];
    input.read_exact(&mut bytes)?;
    let request: Request = serde_json::from_slice(&bytes)?;
    ensure!(
        serde_json::to_vec(&request)? == bytes,
        "canonical exact transition required"
    );
    ensure!(
        input.read(&mut [0_u8; 1])? == 0,
        "single transition action required"
    );
    let result = transition(native, &request)?;
    let bytes = serde_json::to_vec(&result)?;
    ensure!(
        !bytes.is_empty() && bytes.len() <= crate::native_import::MAX_META,
        "bounded ATR1 reply required"
    );
    output.write_all(b"ATR1")?;
    output.write_all(&(bytes.len() as u32).to_be_bytes())?;
    output.write_all(&bytes)?;
    output.flush()?;
    Ok(())
}
pub(crate) fn transition_with_hook(
    native: &Path,
    request: &Request,
    hook: &mut impl FnMut(Phase) -> Result<()>,
) -> Result<Transition> {
    use arc_execution_config::chainspec::TESTNET;
    use reth_ethereum::provider::HeaderProvider;
    use rustix::fs::{FlockOperation, flock};
    let result = expected(request)?;
    let directory = writer::directory(native)?;
    writer::directory(native.parent().ok_or_else(|| eyre!("native parent"))?)?;
    let run = writer::lock_file(native, ".execution-retention-lock")?;
    flock(&run, FlockOperation::NonBlockingLockExclusive)?;
    let sole = writer::lock_file(native, ".arc-native-import-writer")?;
    flock(&sole, FlockOperation::NonBlockingLockExclusive)?;
    let _view = writer::write_guard(native)?;
    // The only allowed import pending here is this exact transition recovery.
    crate::native::check_retention_fences(native)?;
    absent(&native.join(crate::bootstrap::PENDING))?;
    let (boot, digest) = writer::bootstrap_record(native, &TESTNET)?;
    let identity: Identity = writer::artifact(&native.join(writer::IDENTITY))?;
    writer::validate_identity(&identity, &boot, digest, &TESTNET)?;
    ensure!(identity == request.identity, "native transition identity");
    let current: Head = writer::artifact(&native.join(writer::HEAD))?;
    let pending = native.join(crate::native_import::PENDING);
    let intent = Intent {
        version: 3,
        transition: result.clone(),
    };
    let has_pending = match std::fs::symlink_metadata(&pending) {
        Ok(_) => true,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => false,
        Err(e) => return Err(e.into()),
    };
    if has_pending {
        ensure!(
            writer::artifact::<Intent>(&pending)? == intent,
            "different or ambiguous native pending intent"
        );
    }
    if current == result.head {
        ensure!(
            writer::artifact::<Transition>(&path(native, result.head.import_epoch))? == result,
            "completed transition missing or mismatched"
        );
        let f = crate::native::open_writable(native, TESTNET.clone())?;
        writer::check_native(&f, &current, &identity)?;
        writer::check_completion(native, &f, &current, &identity)?;
        crate::retention::managed_floor(&f, native, &identity)?;
        if has_pending {
            std::fs::remove_file(pending)?;
            directory.sync_all()?;
        }
        return Ok(result);
    }
    ensure!(
        !has_pending,
        "partial native transition remains HOLD; completed receipt AND published head required"
    );
    ensure!(
        current == request.previous,
        "native transition previous head changed"
    );
    if current.import_epoch > 1 {
        let previous: Transition = writer::artifact(&path(native, current.import_epoch))?;
        validate_transition(&previous, &identity)?;
        ensure!(
            request.marker.request_generation >= previous.marker.target.generation,
            "completed coordinator marker cannot authorize another native epoch"
        );
    }
    let f = crate::native::open_writable(native, TESTNET.clone())?;
    writer::check_native(&f, &current, &identity)?;
    writer::check_completion(native, &f, &current, &identity)?;
    crate::retention::managed_floor(&f, native, &identity)?;
    crate::history::require(&f, result.head.tip.height)?;
    ensure!(
        f.provider()?
            .header_by_number(result.head.tip.height)?
            .is_some_and(
                |h| h.hash_slow() == result.head.tip.hash && h.state_root == result.head.tip.root
            ),
        "native target is not retained canonical state"
    );
    let epochs = native.join(EPOCHS);
    match std::fs::create_dir(&epochs) {
        Ok(()) => {
            std::fs::set_permissions(&epochs, std::fs::Permissions::from_mode(0o700))?;
            directory.sync_all()?;
        }
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {}
        Err(e) => return Err(e.into()),
    }
    writer::directory(&epochs)?;
    absent(&path(native, result.head.import_epoch))?;
    writer::create(&pending, &intent)?;
    hook(Phase::Intent)?;
    let p = f.provider_rw()?;
    p.remove_block_and_execution_above(result.head.tip.height)?;
    hook(Phase::Unwound)?;
    p.commit()?;
    hook(Phase::Committed)?;
    writer::check_native(&f, &result.head, &identity)?;
    crate::history::require(&f, result.head.tip.height)?;
    writer::create(&path(native, result.head.import_epoch), &result)?;
    hook(Phase::Receipt)?;
    writer::replace_head(&native.join(writer::HEAD), &current, &result.head)?;
    hook(Phase::Head)?;
    writer::check_completion(native, &f, &result.head, &identity)?;
    ensure!(
        writer::artifact::<Intent>(&pending)? == intent,
        "transition intent changed"
    );
    std::fs::remove_file(pending)?;
    directory.sync_all()?;
    hook(Phase::Cleared)?;
    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        native_import::{Frame, Meta},
        native_writer::{self, Writer},
    };
    fn request(w: &Writer) -> Request {
        let anchor = w.identity().anchor.clone();
        Request {
            version: 1,
            identity: w.identity().clone(),
            previous: w.head().clone(),
            marker: Rewind {
                request_generation: 7,
                from: Ref {
                    generation: 7,
                    height: w.head().tip.height,
                    hash: w.head().tip.hash,
                },
                target: Ref {
                    generation: 8,
                    height: anchor.height,
                    hash: anchor.hash,
                },
                from_root: w.head().tip.root,
                root: anchor.root,
            },
        }
    }
    #[test]
    fn transition_unwinds_actual_native_and_restarts_relative_sequence_without_moving_anchor()
    -> Result<()> {
        let (_work, path, frame) = native_writer::tests::setup()?;
        let mut w = Writer::open(&path)?;
        let old = w.import(&frame)?;
        let request = request(&w);
        drop(w);
        let result = transition(&path, &request)?;
        assert_eq!(result.head.import_epoch, 2);
        assert_eq!(result.head.read_epoch, 3);
        assert_eq!(result.head.request_id, 0);
        assert_eq!(result.head.tip, request.identity.anchor);
        assert_eq!(
            transition(&path, &request)?,
            result,
            "same SQL marker must not advance native epoch twice"
        );
        let mut w = Writer::open(&path)?;
        assert_eq!(w.identity(), &request.identity);
        assert!(
            w.import(&frame).is_err(),
            "old import epoch cannot be adopted"
        );
        let mut meta: Meta = frame.meta().clone();
        meta.import_epoch = 2;
        let bytes = serde_json::to_vec(&meta)?;
        let mut encoded = b"AIN1".to_vec();
        for n in [
            bytes.len(),
            frame.block_rlp().len(),
            frame.receipts_rlp().len(),
        ] {
            encoded.extend_from_slice(&(n as u32).to_be_bytes());
        }
        encoded.extend(bytes);
        encoded.extend(frame.block_rlp());
        encoded.extend(frame.receipts_rlp());
        let receipt = w.import(&Frame::read(encoded.as_slice())?.unwrap())?;
        assert_eq!(receipt.request_id, 1);
        assert_eq!(receipt.read_epoch, 4);
        assert_eq!(receipt.hash, old.hash);
        assert_eq!(w.executions(), 1);
        let stale = Request {
            version: 1,
            identity: request.identity.clone(),
            previous: w.head().clone(),
            marker: request.marker.clone(),
        };
        drop(w);
        assert!(
            transition(&path, &stale).is_err(),
            "an already completed SQL generation cannot authorize another native transition after imports advanced"
        );
        Ok(())
    }
    #[test]
    fn transition_requires_joined_writer_and_preserves_invalid_requests() -> Result<()> {
        let (_work, path, frame) = native_writer::tests::setup()?;
        let mut w = Writer::open(&path)?;
        w.import(&frame)?;
        let request = request(&w);
        assert!(
            transition(&path, &request).is_err(),
            "live writer must retain run ownership"
        );
        drop(w);
        for mut bad in [
            request.clone(),
            request.clone(),
            request.clone(),
            request.clone(),
        ]
        .into_iter()
        .enumerate()
        {
            match bad.0 {
                0 => bad.1.marker.target.generation = 7,
                1 => bad.1.marker.root = B256::repeat_byte(42),
                2 => bad.1.previous.read_epoch += 1,
                _ => bad.1.identity.instance = B256::repeat_byte(43),
            }
            assert!(transition(&path, &bad.1).is_err());
            assert!(
                !path.join(crate::native_import::PENDING).exists(),
                "invalid request must fail before native mutation"
            );
            let w = Writer::open(&path)?;
            assert_eq!(w.head(), &request.previous);
            drop(w);
        }
        Ok(())
    }
    #[test]
    fn transition_prefix_uses_original_epoch_receipt_and_rejects_missing_lineage() -> Result<()> {
        use arc_execution_config::chainspec::TESTNET;
        let (_work, path, frame) = native_writer::tests::setup()?;
        let mut w = Writer::open(&path)?;
        let receipt = w.import(&frame)?;
        let base = w.head().tip.clone();
        drop(w);
        // Expected block preparation is a fixture EVM pass outside the writer.
        let (f, scratch) = crate::native::open_readonly(&path, TESTNET.clone(), false)?;
        let executed = crate::native_import::tests::next(&f, TESTNET.clone(), 1)?;
        let block = alloy_rlp::encode(executed.recovered_block().clone().into_block());
        let receipts = writer::encode_receipts(&executed.execution_output.result.receipts);
        let meta = Meta {
            version: 1,
            request_id: 2,
            chain_id: 5042002,
            genesis: TESTNET.inner.genesis_hash(),
            arc_revision: crate::ARC_REVISION.into(),
            reth_revision: crate::native_import::RETH_REVISION.into(),
            import_epoch: 1,
            height: 2,
            hash: executed.recovered_block().hash(),
            parent: base.hash,
            parent_root: base.root,
        };
        let meta = serde_json::to_vec(&meta)?;
        let mut bytes = b"AIN1".to_vec();
        for n in [meta.len(), block.len(), receipts.len()] {
            bytes.extend_from_slice(&(n as u32).to_be_bytes());
        }
        bytes.extend(meta);
        bytes.extend(block);
        bytes.extend(receipts);
        drop(f);
        drop(scratch);
        let mut w = Writer::open(&path)?;
        w.import(&Frame::read(bytes.as_slice())?.unwrap())?;
        let mut request = request(&w);
        request.marker.target.height = 1;
        request.marker.target.hash = base.hash;
        request.marker.root = base.root;
        drop(w);
        let result = transition(&path, &request)?;
        assert_eq!(result.head.read_epoch, 4);
        assert_eq!(result.head.request_id, 0);
        let w = Writer::open(&path)?;
        assert_eq!(w.head().tip, base);
        assert_eq!(
            receipt_context(&path, w.identity(), w.head(), 1)?.import_epoch,
            1
        );
        assert_eq!(
            writer::artifact::<writer::ImportReceipt>(&writer::receipt_path(&path, 1, 1))?,
            receipt
        );
        drop(w);
        std::fs::remove_file(super::path(&path, 2))?;
        assert!(
            Writer::open(&path).is_err(),
            "a completed root cannot replace missing epoch lineage"
        );
        Ok(())
    }
    #[test]
    fn transition_interruption_preserves_ambiguous_stores_and_only_exact_completed_head_resumes()
    -> Result<()> {
        for phase in [
            Phase::Intent,
            Phase::Unwound,
            Phase::Committed,
            Phase::Receipt,
            Phase::Head,
            Phase::Cleared,
        ] {
            let (_work, path, frame) = native_writer::tests::setup()?;
            let mut w = Writer::open(&path)?;
            w.import(&frame)?;
            let request = request(&w);
            drop(w);
            assert!(
                transition_with_hook(&path, &request, &mut |at| {
                    if at == phase {
                        eyre::bail!("owned interruption")
                    }
                    Ok(())
                })
                .is_err()
            );
            let pending = path.join(crate::native_import::PENDING);
            if matches!(phase, Phase::Head | Phase::Cleared) {
                let completed = transition(&path, &request)?;
                assert_eq!(completed.head.import_epoch, 2);
                assert!(!pending.exists());
                drop(Writer::open(&path)?);
            } else {
                let before = std::fs::read(&pending)?;
                assert!(
                    transition(&path, &request).is_err(),
                    "ambiguous {phase:?} must remain HOLD even if MDBX best/root looks target"
                );
                assert_eq!(std::fs::read(&pending)?, before);
                assert!(Writer::open(&path).is_err());
            }
        }
        Ok(())
    }
    #[test]
    fn transition_rejects_dangling_bootstrap_pending_without_mutation() -> Result<()> {
        let (_work, path, frame) = native_writer::tests::setup()?;
        let mut w = Writer::open(&path)?;
        w.import(&frame)?;
        let request = request(&w);
        drop(w);
        let fence = path.join(crate::bootstrap::PENDING);
        std::os::unix::fs::symlink(path.join("absent"), &fence)?;
        assert!(
            transition(&path, &request).is_err(),
            "any pending object, including a broken symlink, must fence mutation"
        );
        assert!(std::fs::symlink_metadata(&fence)?.file_type().is_symlink());
        assert!(!path.join(crate::native_import::PENDING).exists());
        Ok(())
    }
    #[test]
    fn transition_protocol_is_bounded_canonical_single_action_and_exact_completed_reply()
    -> Result<()> {
        let (_work, path, frame) = native_writer::tests::setup()?;
        let mut w = Writer::open(&path)?;
        w.import(&frame)?;
        let request = request(&w);
        drop(w);
        let data = serde_json::to_vec(&request)?;
        let framed = |data: &[u8]| {
            let mut b = b"ANT1".to_vec();
            b.extend_from_slice(&(data.len() as u32).to_be_bytes());
            b.extend(data);
            b
        };
        for bad in [
            b"bad".to_vec(),
            framed(&vec![0; crate::native_import::MAX_META + 1]),
            framed(&[data.as_slice(), b" "].concat()),
            [framed(&data), vec![1]].concat(),
            framed(&data[..data.len() - 1]),
        ] {
            let mut output = vec![];
            assert!(serve_transition(bad.as_slice(), &mut output, &path).is_err());
            assert!(output.is_empty());
            assert!(!path.join(crate::native_import::PENDING).exists());
        }
        let mut output = vec![];
        serve_transition(framed(&data).as_slice(), &mut output, &path)?;
        assert_eq!(&output[..4], b"ATR1");
        let n = u32::from_be_bytes(output[4..8].try_into()?) as usize;
        assert_eq!(output.len(), 8 + n);
        assert!(n <= crate::native_import::MAX_META);
        let reply: Transition = serde_json::from_slice(&output[8..])?;
        assert_eq!(reply, expected(&request)?);
        Ok(())
    }
}
