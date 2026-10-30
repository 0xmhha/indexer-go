//! Explicit managed maintenance under a trusted parent's inherited run EX.
//! Descriptor identity is not proof of lock mode or of an SQL maintenance lease.
use crate::{managed_read::ReadProof, native_writer as writer};
use eyre::{Result, ensure, eyre};
use serde::{Deserialize, Serialize};
use std::{
    fs::File,
    os::unix::fs::MetadataExt,
    path::{Path, PathBuf},
};

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Context {
    pub version: u8,
    pub original_data_dir: String,
    pub proof: ReadProof,
}

pub struct Managed {
    context: Context,
    native: PathBuf,
    descriptor: i32,
    directory: File,
    parent: File,
}
impl Managed {
    pub fn inherited(native: &Path, context: &Path, descriptor: i32) -> Result<Self> {
        ensure!(
            descriptor == 3,
            "managed maintenance requires inherited fd3"
        );
        Self::from_descriptor(native, context, descriptor)
    }
    fn from_descriptor(native: &Path, context: &Path, descriptor: i32) -> Result<Self> {
        ensure!(descriptor >= 3, "invalid inherited run descriptor");
        let bytes = writer::private_bytes(context)?;
        let context: Context = serde_json::from_slice(&bytes)?;
        ensure!(
            serde_json::to_vec(&context)? == bytes,
            "canonical managed context required"
        );
        let directory = writer::directory(native)?;
        let parent = writer::directory(
            native
                .parent()
                .ok_or_else(|| eyre!("native parent required"))?,
        )?;
        let owned = Self {
            context,
            native: native.into(),
            descriptor,
            directory,
            parent,
        };
        owned.check()?;
        Ok(owned)
    }
    pub fn check(&self) -> Result<()> {
        use arc_execution_config::chainspec::TESTNET;
        let c = &self.context;
        let original = Path::new(&c.original_data_dir);
        let parent = original
            .parent()
            .ok_or_else(|| eyre!("original parent required"))?;
        let name = original
            .file_name()
            .and_then(|s| s.to_str())
            .ok_or_else(|| eyre!("original name required"))?;
        ensure!(
            c.version == 1
                && original.is_absolute()
                && parent.canonicalize()? == parent
                && self.native.parent() == Some(parent),
            "managed original path identity"
        );
        let stage = parent.join(format!(".{name}.execution-retention-new"));
        ensure!(
            self.native == original || self.native == stage,
            "managed maintenance target is not original/candidate"
        );
        let lock = parent.join(format!(".{name}.execution-retention-lock"));
        crate::checkpoint::private_file(&lock)?;
        let expected = std::fs::symlink_metadata(lock)?;
        let probe = File::open(format!("/dev/fd/{}", self.descriptor))?;
        let inherited = probe.metadata()?;
        ensure!(
            inherited.is_file()
                && inherited.dev() == expected.dev()
                && inherited.ino() == expected.ino()
                && inherited.mode() & 0o777 == 0o600
                && inherited.uid() == rustix::process::geteuid().as_raw()
                && inherited.nlink() == 1,
            "inherited managed run descriptor identity"
        );
        for (path, pinned) in [
            (&self.native, &self.directory),
            (&parent.to_path_buf(), &self.parent),
        ] {
            let current = writer::directory(path)?.metadata()?;
            let pinned = pinned.metadata()?;
            ensure!(
                current.dev() == pinned.dev() && current.ino() == pinned.ino(),
                "managed directory replaced"
            );
        }
        crate::native::check_import_fences(&self.native)?;
        crate::native_import::require_clean(&self.native)?;
        let i: writer::Identity = writer::artifact(&self.native.join(writer::IDENTITY))?;
        let h: writer::Head = writer::artifact(&self.native.join(writer::HEAD))?;
        let (boot, digest) = writer::bootstrap_record(&self.native, &TESTNET)?;
        writer::validate_identity(&i, &boot, digest, &TESTNET)?;
        crate::native_epoch::valid_head(&h, &i)?;
        ensure!(
            i == c.proof.identity
                && h == c.proof.head
                && c.proof.version == 1
                && c.proof.r#ref == h.tip,
            "managed frozen owner/head mismatch"
        );
        if h.tip.height > i.anchor.height {
            let base = crate::native_epoch::receipt_context(&self.native, &i, &h, h.tip.height)?;
            let receipt: writer::ImportReceipt = writer::artifact(&writer::receipt_path(
                &self.native,
                base.import_epoch,
                h.tip.height,
            ))?;
            ensure!(
                c.proof.receipt.as_ref() == Some(&receipt),
                "managed receipt changed"
            );
        } else {
            ensure!(c.proof.receipt.is_none(), "anchor receipt is not a result");
        }
        Ok(())
    }
    pub fn verify(&self, f: &crate::native::Factory) -> Result<()> {
        self.check()?;
        let p = &self.context.proof;
        writer::check_native(f, &p.head, &p.identity)?;
        writer::check_completion(&self.native, f, &p.head, &p.identity)?;
        crate::retention::managed_floor(f, &self.native, &p.identity)?;
        Ok(())
    }
    pub fn request(&self, r: &crate::retention::Request) -> Result<()> {
        let p = &self.context.proof;
        ensure!(
            r.version == 1
                && r.chain.id == p.identity.chain_id.to_string()
                && r.chain.genesis == p.identity.genesis
                && r.anchor.generation > 0
                && r.anchor.height == p.identity.anchor.height
                && r.anchor.hash == p.identity.anchor.hash,
            "managed retention original anchor/chain mismatch"
        );
        if r.head.hash != alloy_primitives::B256::ZERO {
            r.validate()?;
            ensure!(
                r.head.height == p.head.tip.height
                    && r.head.hash == p.head.tip.hash
                    && r.head_root == p.head.tip.root
                    && r.ack.height == r.head.height
                    && r.ack.hash == r.head.hash
                    && r.tail == r.ack,
                "managed retention is not fully drained"
            );
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        checkpoint,
        native_writer::{self as writer, Writer},
    };
    use rustix::fs::{FlockOperation, flock};
    use std::os::fd::AsRawFd;

    #[test]
    fn managed_inherited_run_accepts_real_owner_and_preserves_legacy_refusal() -> Result<()> {
        let (work, path, frame) = writer::tests::setup()?;
        let path = path.canonicalize()?;
        let mut w = Writer::open(&path)?;
        let receipt = w.import(&frame)?;
        let proof = ReadProof {
            version: 1,
            identity: w.identity().clone(),
            head: w.head().clone(),
            r#ref: w.head().tip.clone(),
            receipt: Some(receipt),
        };
        drop(w);
        let context = work.path().join("managed.json");
        let expected = Context {
            version: 1,
            original_data_dir: path.to_str().unwrap().into(),
            proof,
        };
        checkpoint::write_private(&context, |out| {
            serde_json::to_writer(out, &expected)?;
            Ok(())
        })?;
        let run = writer::lock_file(&path, ".execution-retention-lock")?;
        flock(&run, FlockOperation::NonBlockingLockExclusive)?;
        assert!(crate::native::check_legacy_maintenance(&path).is_err());
        let managed = Managed::from_descriptor(&path, &context, run.as_raw_fd())?;
        let (f, _secondary) = crate::native::open_readonly(
            &path,
            arc_execution_config::chainspec::TESTNET.clone(),
            true,
        )?;
        managed.verify(&f)?;
        let wrong = checkpoint::write_private(&work.path().join("wrong-run"), |out| {
            out.write_all(b"x")?;
            Ok(())
        });
        wrong?;
        let wrong = File::open(work.path().join("wrong-run"))?;
        assert!(Managed::from_descriptor(&path, &context, wrong.as_raw_fd()).is_err());
        assert!(Managed::inherited(&path, &context, 4).is_err());
        Ok(())
    }

    #[test]
    fn managed_writer_rejects_advanced_floor_without_completed_cow_evidence() -> Result<()> {
        let (_work, path, frame) = writer::tests::setup()?;
        let mut w = Writer::open(&path)?;
        w.import(&frame)?;
        drop(w);
        let f =
            crate::native::open_writable(&path, arc_execution_config::chainspec::TESTNET.clone())?;
        crate::history::prune(&f, 1)?;
        assert_eq!(crate::history::floor(&f)?, 1);
        drop(f);
        assert!(
            Writer::open(&path).is_err(),
            "advanced floor was trusted without completed COW/seed proof"
        );
        Ok(())
    }
    fn frame_for(
        w: &Writer,
        block: &reth_chain_state::ExecutedBlock,
    ) -> Result<crate::native_import::Frame> {
        let h = block.recovered_block();
        let m = crate::native_import::Meta {
            version: 1,
            request_id: w.head().request_id + 1,
            chain_id: 5042002,
            genesis: w.identity().genesis,
            arc_revision: w.identity().arc_revision.clone(),
            reth_revision: w.identity().reth_revision.clone(),
            import_epoch: w.head().import_epoch,
            height: w.head().tip.height + 1,
            hash: h.hash(),
            parent: w.head().tip.hash,
            parent_root: w.head().tip.root,
        };
        let meta = serde_json::to_vec(&m)?;
        let encoded = alloy_rlp::encode(h.clone().into_block());
        let receipts = writer::encode_receipts(&block.execution_output.result.receipts);
        let mut bytes = b"AIN1".to_vec();
        for n in [meta.len(), encoded.len(), receipts.len()] {
            bytes.extend_from_slice(&(n as u32).to_be_bytes());
        }
        bytes.extend(meta);
        bytes.extend(encoded);
        bytes.extend(receipts);
        Ok(crate::native_import::Frame::read(bytes.as_slice())?.unwrap())
    }

    #[test]
    fn managed_cutoff_restarts_imports_and_rewinds_above_floor_without_moving_identity()
    -> Result<()> {
        use crate::{native, native_epoch, native_import, retention, wire::Ref};
        use alloy_primitives::B256;
        use reth_ethereum::provider::{HeaderProvider, SaveBlocksMode};
        use sha2::{Digest, Sha256};
        let (work, path, _frame) = writer::tests::setup()?;
        let path = path.canonicalize()?;
        let (_source, source, spec) = native_import::tests::native(false)?;
        let mut w = Writer::open(&path)?;
        let identity = w.identity().clone();
        let bootstrap = std::fs::read(path.join(".arc-native-bootstrap.json"))?;
        let mut receipt = None;
        for height in 0..132 {
            let block = native_import::tests::continued(&source, spec.clone(), height)?;
            receipt = Some(w.import(&frame_for(&w, &block)?)?);
            let p = source.provider_rw()?;
            p.save_blocks(vec![block], SaveBlocksMode::Full)?;
            p.commit()?;
        }
        let head = w.head().clone();
        let before_maint = native_epoch::Request {
            version: 1,
            identity: identity.clone(),
            previous: head.clone(),
            marker: native_epoch::Rewind {
                request_generation: 7,
                from: Ref {
                    generation: 7,
                    height: 132,
                    hash: head.tip.hash,
                },
                target: Ref {
                    generation: 8,
                    height: 0,
                    hash: identity.anchor.hash,
                },
                from_root: head.tip.root,
                root: identity.anchor.root,
            },
        };
        drop(w);
        let run = writer::lock_file(&path, ".execution-retention-lock")?;
        flock(&run, FlockOperation::NonBlockingLockExclusive)?;
        let context = Context {
            version: 1,
            original_data_dir: path.to_str().unwrap().into(),
            proof: ReadProof {
                version: 1,
                identity: identity.clone(),
                head: head.clone(),
                r#ref: head.tip.clone(),
                receipt,
            },
        };
        let context_file = work.path().join("managed.json");
        checkpoint::write_private(&context_file, |out| {
            serde_json::to_writer(out, &context)?;
            Ok(())
        })?;
        let ack = Ref {
            generation: 7,
            height: 132,
            hash: head.tip.hash,
        };
        let request = retention::Request {
            version: 1,
            chain: checkpoint::Chain {
                id: "5042002".into(),
                genesis: identity.genesis,
            },
            anchor: Ref {
                generation: 1,
                height: 0,
                hash: identity.anchor.hash,
            },
            ack: ack.clone(),
            tail: ack.clone(),
            head: Ref {
                generation: 0,
                height: 132,
                hash: head.tip.hash,
            },
            head_root: head.tip.root,
            source_digest: B256::repeat_byte(4),
            keep_blocks: 129,
            through: 3,
        };
        let permit = serde_json::json!({"Version":1,"Coverage":"full","BaselineDigest":B256::repeat_byte(5),"Token":B256::repeat_byte(6),"Request":request,"Visible":ack,"Root":head.tip.root});
        let raw = serde_json::value::RawValue::from_string(serde_json::to_string(&permit)?)?;
        let apply = retention::Apply {
            request,
            permit_digest: B256::from_slice(&Sha256::digest(raw.get().as_bytes())),
        };
        let stage = path
            .parent()
            .unwrap()
            .join(".native.execution-retention-new");
        std::fs::rename(&path, &stage)?; // owned SDK fixture; Go COW copy/swap is tested separately
        let marker = path.parent().unwrap().join(".native.execution-retention");
        writer::create(
            &marker,
            &serde_json::json!({"Version":1,"Permit":permit,"Phase":"copy","Original":{"Device":1,"Inode":2},"Replacement":{"Device":0,"Inode":0}}),
        )?;
        writer::create(
            &stage.join(".execution-retention-copy"),
            &serde_json::json!({"PermitDigest":apply.permit_digest}),
        )?;
        let managed = Managed::from_descriptor(&stage, &context_file, run.as_raw_fd())?;
        let f = native::open_writable(&stage, spec.clone())?;
        managed.verify(&f)?;
        managed.request(&apply.request)?;
        retention::candidate(&stage, &apply)?;
        retention::prune(&f, &spec, &stage, &apply, work.path())?;
        retention::verify(&f, &spec, &stage, &apply, work.path())?;
        assert!(
            retention::managed_floor(&f, &stage, &identity).is_err(),
            "native floor alone granted completed COW authority"
        );
        #[derive(Serialize)]
        #[serde(rename_all = "PascalCase")]
        struct Completed<'a> {
            version: u16,
            permit: &'a serde_json::value::RawValue,
        }
        writer::create(
            &stage.join(".execution-retention-floor"),
            &Completed {
                version: 1,
                permit: &raw,
            },
        )?;
        retention::managed_floor(&f, &stage, &identity)?;
        drop(f);
        drop(managed);
        std::fs::rename(&stage, &path)?;
        std::fs::remove_file(marker)?;
        drop(run);
        assert_eq!(
            std::fs::read(path.join(".arc-native-bootstrap.json"))?,
            bootstrap
        );
        let mut w = Writer::open(&path)?;
        assert_eq!(w.identity(), &identity);
        assert_eq!(w.head(), &head);
        let block = native_import::tests::continued(&source, spec.clone(), 132)?;
        w.import(&frame_for(&w, &block)?)?;
        let advanced = w.head().clone();
        drop(w);
        let w = Writer::open(&path)?;
        assert_eq!(w.head(), &advanced);
        drop(w);
        let mut too_deep = before_maint.clone();
        too_deep.previous = advanced.clone();
        too_deep.marker.from.height = 133;
        too_deep.marker.from.hash = advanced.tip.hash;
        too_deep.marker.from_root = advanced.tip.root;
        assert!(native_epoch::transition(&path, &too_deep).is_err());
        assert!(
            !path.join(native_import::PENDING).exists(),
            "below-floor rewind wrote an intent"
        );
        let target = source.provider()?.header_by_number(131)?.unwrap();
        too_deep.marker.target = Ref {
            generation: 8,
            height: 131,
            hash: target.hash_slow(),
        };
        too_deep.marker.root = target.state_root;
        native_epoch::transition(&path, &too_deep)?;
        let w = Writer::open(&path)?;
        assert_eq!(w.identity(), &identity);
        assert_eq!(w.head().tip.height, 131);
        assert_eq!(w.head().import_epoch, 2);
        Ok(())
    }
}
