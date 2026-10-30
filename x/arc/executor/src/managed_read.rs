//! Explicit inherited read-view contract for the owned managed RO child.
use crate::native_writer::{Head, Identity, ImportReceipt, NativeRef};
use eyre::{Result, ensure, eyre};
use rustix::fs::{FlockOperation, flock};
use serde::{Deserialize, Serialize};
use std::{
    fs::File,
    os::unix::fs::MetadataExt,
    path::{Path, PathBuf},
};

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct ReadProof {
    pub version: u8,
    pub identity: Identity,
    pub head: Head,
    pub r#ref: NativeRef,
    pub receipt: Option<ImportReceipt>,
}
pub fn snapshot(
    view: &InheritedView,
    f: &crate::native::Factory,
    height: Option<u64>,
) -> Result<ReadProof> {
    use crate::native_writer as writer;
    use arc_execution_config::chainspec::TESTNET;
    use reth_ethereum::provider::HeaderProvider;
    view.check()?;
    let native = &view.native;
    let identity: Identity = writer::artifact(&native.join(writer::IDENTITY))?;
    let head: Head = writer::artifact(&native.join(writer::HEAD))?;
    let (boot, digest) = writer::bootstrap_record(native, &TESTNET)?;
    writer::validate_identity(&identity, &boot, digest, &TESTNET)?;
    crate::native_epoch::valid_head(&head, &identity)?;
    writer::check_native(f, &head, &identity)?;
    writer::check_completion(native, f, &head, &identity)?;
    // Bind even a head-only/canonical-anchor reply to the exact latest input.
    let current = if head.tip.height > identity.anchor.height {
        Some(checked_receipt(
            f,
            native,
            &identity,
            &head,
            head.tip.height,
        )?)
    } else {
        None
    };
    let height = height.unwrap_or(head.tip.height);
    ensure!(
        height <= head.tip.height,
        "managed reference exceeds completed native head"
    );
    let provider = f.provider()?;
    let header = provider
        .header_by_number(height)?
        .ok_or_else(|| eyre!("retained managed header unavailable"))?;
    let hash = alloy_primitives::Sealable::hash_slow(&header);
    let reference = NativeRef {
        height,
        hash,
        root: header.state_root,
    };
    let receipt = if height <= identity.anchor.height {
        None
    } else if height == head.tip.height {
        current
    } else {
        Some(checked_receipt(f, native, &identity, &head, height)?)
    };
    ensure!(
        writer::artifact::<Head>(&native.join(writer::HEAD))? == head
            && writer::artifact::<Identity>(&native.join(writer::IDENTITY))? == identity,
        "managed controls changed during snapshot"
    );
    view.check()?;
    Ok(ReadProof {
        version: 1,
        identity,
        head,
        r#ref: reference,
        receipt,
    })
}
fn checked_receipt(
    f: &crate::native::Factory,
    native: &Path,
    identity: &Identity,
    head: &Head,
    height: u64,
) -> Result<ImportReceipt> {
    use crate::{native_import, native_writer as writer};
    use alloy_primitives::B256;
    use reth_ethereum::provider::{BlockReader, HeaderProvider, ReceiptProvider};
    use sha2::{Digest, Sha256};
    let base = crate::native_epoch::receipt_context(native, identity, head, height)?;
    let r: ImportReceipt =
        writer::artifact(&writer::receipt_path(native, base.import_epoch, height))?;
    let provider = f.provider()?;
    let block = provider
        .block_by_number(height)?
        .ok_or_else(|| eyre!("retained managed block unavailable"))?;
    let parent = provider
        .header_by_number(height - 1)?
        .ok_or_else(|| eyre!("retained managed parent unavailable"))?;
    let receipts = provider
        .receipts_by_block(height.into())?
        .ok_or_else(|| eyre!("retained managed receipts unavailable"))?;
    ensure!(
        r.version == 1
            && r.identity == *identity
            && r.import_epoch == base.import_epoch
            && r.height == height
            && r.hash == block.header.hash_slow()
            && r.root == block.header.state_root
            && r.parent == block.header.parent_hash
            && r.parent_root == parent.state_root
            && r.receipts_root == block.header.receipts_root
            && r.request_id == height - base.tip.height
            && base.read_epoch.checked_add(r.request_id) == Some(r.read_epoch)
            && r.read_epoch <= head.read_epoch,
        "managed receipt/reference mismatch"
    );
    let meta = native_import::Meta {
        version: 1,
        request_id: r.request_id,
        chain_id: identity.chain_id,
        genesis: identity.genesis,
        arc_revision: identity.arc_revision.clone(),
        reth_revision: identity.reth_revision.clone(),
        import_epoch: r.import_epoch,
        height,
        hash: r.hash,
        parent: r.parent,
        parent_root: r.parent_root,
    };
    let encoded = serde_json::to_vec(&meta)?;
    let block = alloy_rlp::encode(block);
    let receipts = writer::encode_receipts(&receipts);
    ensure!(
        encoded.len() <= native_import::MAX_META
            && !block.is_empty()
            && block.len() <= native_import::MAX_BLOCK
            && !receipts.is_empty()
            && receipts.len() <= native_import::MAX_RECEIPTS,
        "managed original input limits"
    );
    let mut header = b"AIN1".to_vec();
    for n in [encoded.len(), block.len(), receipts.len()] {
        header.extend_from_slice(&(n as u32).to_be_bytes());
    }
    let mut digest = Sha256::new();
    for data in [
        header.as_slice(),
        encoded.as_slice(),
        block.as_slice(),
        receipts.as_slice(),
    ] {
        digest.update(data);
    }
    ensure!(
        r.input_digest == B256::from_slice(&digest.finalize()),
        "managed original input digest mismatch"
    );
    Ok(r)
}

/// Go holds SH on this SAME inherited descriptor through full response decode.
/// Rust validates fd identity but never acquires the turnstile or unlocks it.
/// Metadata alone is not proof that a caller held the kernel lock.
pub struct InheritedView {
    native: PathBuf,
    descriptor: i32,
    directory: File,
    parent: File,
    _run: File,
}
impl InheritedView {
    #[cfg(test)]
    pub(crate) fn inherited_fixture(native: &Path, fd: i32) -> Result<Self> {
        Self::from_descriptor(native, fd)
    }
    pub fn inherited(native: &Path, fd: i32) -> Result<Self> {
        ensure!(fd == 3, "managed reader requires inherited fd3");
        Self::from_descriptor(native, fd)
    }
    fn from_descriptor(native: &Path, descriptor: i32) -> Result<Self> {
        ensure!(descriptor >= 3, "invalid inherited descriptor");
        let directory = crate::native_writer::directory(native)?;
        let parent = crate::native_writer::directory(
            native
                .parent()
                .ok_or_else(|| eyre!("native parent required"))?,
        )?;
        Self::check_descriptor(native, descriptor)?;
        crate::native_import::require_clean(native)?;
        crate::native::check_fences(native)?;
        crate::checkpoint::private_file(&native.join(crate::native_writer::IDENTITY))?;
        let run = crate::native_writer::lock_file(native, ".execution-retention-lock")?;
        flock(&run, FlockOperation::NonBlockingLockShared)?;
        Ok(Self {
            native: native.into(),
            descriptor,
            directory,
            parent,
            _run: run,
        })
    }
    fn check_descriptor(native: &Path, descriptor: i32) -> Result<()> {
        let name = native
            .file_name()
            .and_then(|s| s.to_str())
            .ok_or_else(|| eyre!("native filename required"))?;
        let path = native
            .parent()
            .ok_or_else(|| eyre!("native parent required"))?
            .join(format!(".{name}.arc-native-import-view"));
        crate::checkpoint::private_file(&path)?;
        let expected = std::fs::symlink_metadata(&path)?;
        // Descriptor-path probing never takes ownership of fd3 or calls flock.
        // The inherited original remains open until process exit. The probe is
        // only metadata validation, not a replacement lock/open-file description.
        let probe = File::open(format!("/dev/fd/{descriptor}"))?;
        let actual = probe.metadata()?;
        ensure!(
            actual.is_file()
                && actual.dev() == expected.dev()
                && actual.ino() == expected.ino()
                && actual.mode() & 0o777 == 0o600
                && actual.uid() == rustix::process::geteuid().as_raw()
                && actual.nlink() == 1,
            "inherited managed view identity mismatch"
        );
        Ok(())
    }
    pub fn check(&self) -> Result<()> {
        for (path, pinned) in [
            (self.native.as_path(), &self.directory),
            (
                self.native
                    .parent()
                    .ok_or_else(|| eyre!("native parent required"))?,
                &self.parent,
            ),
        ] {
            let current = crate::native_writer::directory(path)?;
            let a = current.metadata()?;
            let b = pinned.metadata()?;
            ensure!(
                a.dev() == b.dev() && a.ino() == b.ino(),
                "managed native directory changed"
            );
        }
        Self::check_descriptor(&self.native, self.descriptor)?;
        crate::native_import::require_clean(&self.native)?;
        crate::native::check_fences(&self.native)?;
        crate::checkpoint::private_file(&self.native.join(crate::native_writer::IDENTITY))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rustix::fs::{FlockOperation, flock};
    use std::{
        fs::{File, OpenOptions},
        os::unix::{
            fs::{OpenOptionsExt, PermissionsExt},
            io::AsRawFd,
        },
        path::PathBuf,
    };

    #[test]
    fn snapshot_binds_actual_completed_native_root_and_original_input_digest() -> Result<()> {
        use crate::native_writer::Writer;
        use arc_execution_config::chainspec::TESTNET;
        use sha2::{Digest, Sha256};
        let (_work, path, frame) = crate::native_writer::tests::setup()?;
        let mut writer = Writer::open(&path)?;
        let identity = writer.identity().clone();
        let receipt = writer.import(&frame)?;
        drop(writer);
        let file = crate::native_writer::lock_file(&path, ".arc-native-import-view")?;
        flock(&file, FlockOperation::LockShared)?;
        let view = InheritedView::from_descriptor(&path, file.as_raw_fd())?;
        let (factory, _scratch) = crate::native::open_readonly(&path, TESTNET.clone(), false)?;
        let proof = snapshot(&view, &factory, Some(1))?;
        assert_eq!(proof.identity, identity);
        assert_eq!(proof.r#ref.hash, frame.meta().hash);
        assert_eq!(proof.receipt.as_ref(), Some(&receipt));
        assert_eq!(proof.head.read_epoch, 2);
        let anchor = snapshot(&view, &factory, Some(0))?;
        assert!(anchor.receipt.is_none());
        assert_eq!(anchor.r#ref, identity.anchor);
        let head_path = path.join(crate::native_writer::HEAD);
        for version in [false, true] {
            let mut changed = proof.head.clone();
            if version {
                changed.version = 2;
            } else {
                changed.instance.0[0] ^= 1;
            }
            overwrite_control(&head_path, &changed)?;
            assert_eq!(crate::native_writer::artifact::<Head>(&head_path)?, changed);
            assert!(
                snapshot(&view, &factory, Some(1)).is_err(),
                "checksummed current head must bind its managed version and native instance"
            );
            overwrite_control(&head_path, &proof.head)?;
        }
        let mut corrupt = receipt;
        corrupt.input_digest.0[0] ^= 1;
        let data = serde_json::to_vec(&corrupt)?;
        let digest = alloy_primitives::B256::from_slice(&Sha256::digest(&data));
        let sealed = format!(
            "{{\"Data\":{},\"Digest\":\"{}\"}}",
            std::str::from_utf8(&data)?,
            digest
        );
        std::fs::write(
            path.join(crate::native_writer::RECEIPTS).join("1-1.json"),
            sealed,
        )?;
        assert_eq!(
            crate::native_writer::artifact::<ImportReceipt>(
                &path.join(crate::native_writer::RECEIPTS).join("1-1.json")
            )?,
            corrupt
        );
        assert!(
            snapshot(&view, &factory, Some(1)).is_err(),
            "a checksummed but wrong AIN1 digest must not establish completion"
        );
        Ok(())
    }
    fn overwrite_control<T: serde::Serialize>(path: &Path, value: &T) -> Result<()> {
        use sha2::{Digest, Sha256};
        let data = serde_json::to_vec(value)?;
        let digest = alloy_primitives::B256::from_slice(&Sha256::digest(&data));
        let sealed = format!(
            "{{\"Data\":{},\"Digest\":\"{}\"}}",
            std::str::from_utf8(&data)?,
            digest
        );
        std::fs::write(path, sealed)?;
        Ok(())
    }

    fn fixture() -> Result<(tempfile::TempDir, PathBuf, File, PathBuf)> {
        let work = tempfile::tempdir()?;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let native = work.path().join("native");
        std::fs::create_dir(&native)?;
        std::fs::set_permissions(&native, std::fs::Permissions::from_mode(0o700))?;
        std::fs::write(native.join("KEEP"), b"native state remains")?;
        let identity = native.join(crate::native_writer::IDENTITY);
        std::fs::write(&identity, b"private fixture; snapshot validates content")?;
        std::fs::set_permissions(identity, std::fs::Permissions::from_mode(0o600))?;
        let path = work.path().join(".native.arc-native-import-view");
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&path)?;
        Ok((work, native, file, path))
    }
    #[test]
    fn inherited_view_borrows_without_nested_turn_or_unlocking_parent() -> Result<()> {
        let (work, native, file, path) = fixture()?;
        flock(&file, FlockOperation::LockShared)?;
        let turn = OpenOptions::new()
            .read(true)
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(work.path().join(".native.arc-native-import-turn"))?;
        flock(&turn, FlockOperation::LockExclusive)?;
        let view = InheritedView::from_descriptor(&native, file.as_raw_fd())?;
        view.check()?;
        drop(view);
        let probe = OpenOptions::new().read(true).write(true).open(&path)?;
        assert!(
            flock(&probe, FlockOperation::NonBlockingLockExclusive).is_err(),
            "Rust must not unlock the inherited parent OFD"
        );
        flock(&file, FlockOperation::Unlock)?;
        flock(&probe, FlockOperation::NonBlockingLockExclusive)?;
        assert_eq!(std::fs::read(native.join("KEEP"))?, b"native state remains");
        Ok(())
    }
    #[test]
    fn inherited_view_rejects_wrong_fd_pending_or_replaced_lock() -> Result<()> {
        let (_work, native, file, path) = fixture()?;
        assert!(InheritedView::inherited(&native, 4).is_err());
        assert!(InheritedView::from_descriptor(&native, i32::MAX).is_err());
        let view = InheritedView::from_descriptor(&native, file.as_raw_fd())?;
        let pending = native.join(crate::native_import::PENDING);
        std::fs::write(&pending, b"preserve")?;
        assert!(view.check().is_err());
        assert_eq!(std::fs::read(&pending)?, b"preserve");
        std::fs::remove_file(&pending)?;
        std::fs::rename(&path, path.with_extension("keep"))?;
        let _new = OpenOptions::new()
            .read(true)
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&path)?;
        assert!(view.check().is_err());
        assert_eq!(std::fs::read(native.join("KEEP"))?, b"native state remains");
        Ok(())
    }
}
