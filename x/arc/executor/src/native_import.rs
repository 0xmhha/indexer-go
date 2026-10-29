//! Bounded first-import inputs and crash fences, independent of E1 ACK authority.
//! Native execution and application lifecycle are introduced in later milestones.

use alloy_primitives::B256;
use arc_execution_config::chainspec::TESTNET;
use eyre::{Result, bail, ensure, eyre};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    io::{Read, Write},
    path::Path,
};

pub const RETH_REVISION: &str = "88505c7fcbfdebfd3b56d88c86b62e950043c6c4";
pub const MAX_META: usize = 16 << 10;
pub const MAX_BLOCK: usize = 10 << 20;
pub const MAX_RECEIPTS: usize = 10 << 20;
pub const PENDING: &str = ".arc-native-import.pending";

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Meta {
    pub version: u8,
    #[serde(rename = "RequestID")]
    pub request_id: u64,
    #[serde(rename = "ChainID")]
    pub chain_id: u64,
    pub genesis: B256,
    pub arc_revision: String,
    pub reth_revision: String,
    pub import_epoch: u64,
    pub height: u64,
    pub hash: B256,
    pub parent: B256,
    pub parent_root: B256,
}

#[derive(Debug)]
pub struct Frame {
    meta: Meta,
    block: Vec<u8>,
    receipts: Vec<u8>,
    digest: B256,
}

impl Frame {
    /// Decode one bounded frame. RLP contents are opaque here: this is not an
    /// execution, canonicality, publication, completion or ACK validation.
    pub fn read(mut input: impl Read) -> Result<Option<Self>> {
        let mut header = [0_u8; 16];
        if input.read(&mut header[..1])? == 0 {
            return Ok(None);
        }
        input
            .read_exact(&mut header[1..])
            .map_err(|_| eyre!("truncated import frame header"))?;
        ensure!(&header[..4] == b"AIN1", "invalid import frame version");
        let mut sizes = [0_usize; 3];
        for (slot, encoded) in sizes.iter_mut().zip(header[4..].as_chunks::<4>().0) {
            *slot = u32::from_be_bytes(*encoded) as usize;
        }
        ensure!(
            (1..=MAX_META).contains(&sizes[0])
                && (1..=MAX_BLOCK).contains(&sizes[1])
                && (1..=MAX_RECEIPTS).contains(&sizes[2]),
            "import frame size limit"
        );
        let mut metadata = vec![0; sizes[0]];
        input
            .read_exact(&mut metadata)
            .map_err(|_| eyre!("truncated import metadata"))?;
        let meta: Meta =
            serde_json::from_slice(&metadata).map_err(|_| eyre!("invalid import metadata"))?;
        ensure!(
            meta.version == 1
                && meta.chain_id == 5042002
                && meta.genesis == TESTNET.inner.genesis_hash()
                && meta.arc_revision == crate::ARC_REVISION
                && meta.reth_revision == RETH_REVISION,
            "import identity or pinned revision mismatch"
        );
        ensure!(
            [meta.request_id, meta.import_epoch, meta.height]
                .iter()
                .all(|n| (1..=i64::MAX as u64).contains(n))
                && !meta.hash.is_zero()
                && !meta.parent.is_zero()
                && !meta.parent_root.is_zero(),
            "invalid import reference"
        );
        let mut block = vec![0; sizes[1]];
        let mut receipts = vec![0; sizes[2]];
        input
            .read_exact(&mut block)
            .map_err(|_| eyre!("truncated import block"))?;
        input
            .read_exact(&mut receipts)
            .map_err(|_| eyre!("truncated import receipts"))?;
        let digest: [u8; 32] = Sha256::new()
            .chain_update(header)
            .chain_update(&metadata)
            .chain_update(&block)
            .chain_update(&receipts)
            .finalize()
            .into();
        Ok(Some(Self {
            meta,
            block,
            receipts,
            digest: B256::new(digest),
        }))
    }
    pub fn meta(&self) -> &Meta {
        &self.meta
    }
    pub fn block_rlp(&self) -> &[u8] {
        &self.block
    }
    pub fn receipts_rlp(&self) -> &[u8] {
        &self.receipts
    }
    pub const fn digest(&self) -> B256 {
        self.digest
    }
}

fn private_directory(native: &Path) -> Result<std::fs::File> {
    use rustix::fs::{Mode, OFlags};
    use std::os::unix::fs::MetadataExt;
    ensure!(
        native.is_absolute(),
        "absolute private native directory required"
    );
    let fd = rustix::fs::open(
        native,
        OFlags::RDONLY | OFlags::DIRECTORY | OFlags::NOFOLLOW | OFlags::CLOEXEC,
        Mode::empty(),
    )
    .map_err(|_| eyre!("private native directory unavailable"))?;
    let file = std::fs::File::from(fd);
    let meta = file.metadata()?;
    ensure!(
        meta.is_dir()
            && meta.uid() == rustix::process::geteuid().as_raw()
            && meta.mode() & 0o777 == 0o700,
        "owned0700 native directory required"
    );
    Ok(file)
}

/// Publish a durable pending fence BEFORE native mutation. Errors preserve any
/// partial intent. There is deliberately no completion, deletion or ACK API.
pub fn write_pending(native: &Path, frame: &Frame) -> Result<()> {
    use rustix::fs::{Mode, OFlags};
    use std::os::unix::fs::PermissionsExt;
    #[derive(Serialize)]
    #[serde(rename_all = "PascalCase")]
    struct Pending<'a> {
        version: u8,
        meta: &'a Meta,
        digest: B256,
    }
    require_clean(native)?;
    crate::native::check_start(native)?;
    let dir = private_directory(native)?;
    let bytes = serde_json::to_vec(&Pending {
        version: 1,
        meta: frame.meta(),
        digest: frame.digest(),
    })?;
    ensure!(bytes.len() <= MAX_META, "import intent size limit");
    let fd = rustix::fs::openat(
        &dir,
        PENDING,
        OFlags::WRONLY | OFlags::CREATE | OFlags::EXCL | OFlags::NOFOLLOW | OFlags::CLOEXEC,
        Mode::RUSR | Mode::WUSR,
    )
    .map_err(|_| eyre!("native import intent already exists or is unavailable"))?;
    let mut file = std::fs::File::from(fd);
    file.set_permissions(std::fs::Permissions::from_mode(0o600))?;
    file.write_all(&bytes)?;
    file.sync_all()?;
    dir.sync_all()?;
    Ok(())
}

/// Any pending object, including a symlink, directory or corrupt partial file,
/// blocks admission. Its age or a visible native header cannot grant recovery.
pub fn require_clean(native: &Path) -> Result<()> {
    let dir = private_directory(native)?;
    match rustix::fs::statat(&dir, PENDING, rustix::fs::AtFlags::SYMLINK_NOFOLLOW) {
        Err(rustix::io::Errno::NOENT) => Ok(()),
        _ => bail!("native import pending; exact explicit recovery required"),
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use alloy_consensus::{BlockBody, Header, SignableTransaction, TxLegacy, TxReceipt, proofs};
    use alloy_primitives::{Address, B256, Sealable, Signature, TxKind, U256};
    use arc_evm::{ArcEvmConfig, ArcEvmFactory};
    use arc_execution_config::chainspec::{ArcChainSpec, TESTNET};
    use reth_chain_state::{ComputedTrieData, ExecutedBlock};
    use reth_ethereum::{
        evm::EthEvmConfig,
        provider::{
            BlockHashReader, HeaderProvider, ProviderFactory, SaveBlocksMode,
            db::{init_db, mdbx::DatabaseArguments},
            providers::{RocksDBProvider, StaticFileProvider},
        },
        tasks::Runtime,
    };
    use reth_ethereum_primitives::{Block, TransactionSigned};
    use reth_evm::{ConfigureEvm, execute::Executor};
    use reth_primitives_traits::{RecoveredBlock, SignerRecoverable};
    use reth_storage_api::{
        BlockExecutionWriter, BlockNumReader, BlockReader, ChangeSetReader, ReceiptProvider,
        StorageChangeSetReader, StorageSettingsCache,
    };
    use reth_trie::HashedPostState;
    use sha2::{Digest, Sha256};
    use std::{
        io::{BufRead, BufReader, Cursor},
        os::unix::fs::PermissionsExt,
        process::{Child, ChildStdin, Command, Stdio},
        sync::{Arc, mpsc},
        thread,
        time::Duration,
    };

    fn meta() -> Meta {
        Meta {
            version: 1,
            request_id: 1,
            chain_id: 5042002,
            genesis: TESTNET.inner.genesis_hash(),
            arc_revision: crate::ARC_REVISION.into(),
            reth_revision: RETH_REVISION.into(),
            import_epoch: 1,
            height: 1,
            hash: B256::repeat_byte(2),
            parent: TESTNET.inner.genesis_hash(),
            parent_root: TESTNET.inner.genesis_header().state_root,
        }
    }
    fn encoded(meta: &[u8], block: &[u8], receipts: &[u8]) -> Vec<u8> {
        let mut out = b"AIN1".to_vec();
        for n in [meta.len(), block.len(), receipts.len()] {
            out.extend_from_slice(&(n as u32).to_be_bytes());
        }
        out.extend_from_slice(meta);
        out.extend_from_slice(block);
        out.extend_from_slice(receipts);
        out
    }
    fn valid() -> Vec<u8> {
        encoded(&serde_json::to_vec(&meta()).unwrap(), &[0xc0], &[0xc0])
    }

    #[test]
    fn import_frame_exact_bytes_identity_and_clean_eof() -> Result<()> {
        let bytes = valid();
        let f = Frame::read(bytes.as_slice())?.expect("one frame");
        assert_eq!(f.meta(), &meta());
        assert_eq!(f.block_rlp(), &[0xc0]);
        assert_eq!(f.receipts_rlp(), &[0xc0]);
        assert_eq!(f.digest(), B256::from_slice(&Sha256::digest(&bytes)));
        assert!(Frame::read(&[][..])?.is_none());
        let mut combined = Cursor::new([bytes.clone(), bytes].concat());
        assert!(Frame::read(&mut combined)?.is_some());
        assert!(Frame::read(&mut combined)?.is_some());
        assert!(Frame::read(&mut combined)?.is_none());
        Ok(())
    }

    #[test]
    fn import_frame_rejects_bounds_truncation_unknowns_and_foreign_identity() {
        let good = valid();
        for size in 1..good.len() {
            assert!(Frame::read(&good[..size]).is_err(), "prefix {size}");
        }
        for (offset, n) in [
            (4, MAX_META + 1),
            (8, MAX_BLOCK + 1),
            (12, MAX_RECEIPTS + 1),
            (4, 0),
            (8, 0),
            (12, 0),
        ] {
            let mut bad = good[..16].to_vec();
            bad[offset..offset + 4].copy_from_slice(&(n as u32).to_be_bytes());
            assert!(Frame::read(bad.as_slice()).is_err());
        }
        let mut bad = good.clone();
        bad[0] = b'X';
        assert!(Frame::read(bad.as_slice()).is_err());
        let json = serde_json::to_string(&meta()).unwrap();
        for extra in [",\"Unknown\":1}", ",\"Version\":1}"] {
            let bad = format!("{}{}", &json[..json.len() - 1], extra);
            assert!(Frame::read(encoded(bad.as_bytes(), &[0xc0], &[0xc0]).as_slice()).is_err());
        }
        for change in 0..16 {
            let mut m = meta();
            match change {
                0 => m.version = 2,
                1 => m.chain_id = 1337,
                2 => m.genesis = B256::ZERO,
                3 => m.arc_revision = "other".into(),
                4 => m.reth_revision = "other".into(),
                5 => m.request_id = 0,
                6 => m.import_epoch = 0,
                7 => m.height = 0,
                8 => m.height = u64::MAX,
                9 => m.request_id = u64::MAX,
                10 => m.import_epoch = u64::MAX,
                11 => m.hash = B256::ZERO,
                12 => m.parent = B256::ZERO,
                13 => m.parent_root = B256::ZERO,
                14 => m.genesis = B256::repeat_byte(1),
                _ => m.chain_id = 5042,
            }
            assert!(
                Frame::read(encoded(&serde_json::to_vec(&m).unwrap(), &[0xc0], &[0xc0]).as_slice())
                    .is_err()
            );
        }
    }

    #[test]
    fn import_pending_is_durable_exclusive_and_never_an_ack() -> Result<()> {
        let dir = tempfile::tempdir()?;
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700))?;
        require_clean(dir.path())?;
        let f = Frame::read(valid().as_slice())?.unwrap();
        write_pending(dir.path(), &f)?;
        assert!(require_clean(dir.path()).is_err());
        assert!(
            write_pending(dir.path(), &f).is_err(),
            "pending intent must never overwrite"
        );
        let bytes = std::fs::read(dir.path().join(PENDING))?;
        let value: serde_json::Value = serde_json::from_slice(&bytes)?;
        assert_eq!(value["Digest"], serde_json::to_value(f.digest())?);
        assert_eq!(value["Meta"], serde_json::to_value(f.meta())?);
        assert_eq!(
            std::fs::metadata(dir.path().join(PENDING))?
                .permissions()
                .mode()
                & 0o777,
            0o600
        );
        assert!(
            require_clean(dir.path()).is_err(),
            "restart must retain the fence"
        );
        Ok(())
    }

    #[test]
    fn import_pending_rejects_unsafe_paths_and_preserves_foreign_files() -> Result<()> {
        use std::os::unix::fs::symlink;
        let dir = tempfile::tempdir()?;
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700))?;
        let frame = Frame::read(valid().as_slice())?.unwrap();
        assert!(require_clean(Path::new(".")).is_err());
        let link = dir.path().join("native-link");
        symlink(dir.path(), &link)?;
        assert!(require_clean(&link).is_err());
        assert!(write_pending(&link, &frame).is_err());
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o755))?;
        assert!(require_clean(dir.path()).is_err());
        assert!(write_pending(dir.path(), &frame).is_err());
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700))?;
        let target = dir.path().join("sentinel");
        std::fs::write(&target, b"preserve")?;
        symlink(&target, dir.path().join(PENDING))?;
        assert!(require_clean(dir.path()).is_err());
        assert!(write_pending(dir.path(), &frame).is_err());
        assert_eq!(std::fs::read(&target)?, b"preserve");
        Ok(())
    }

    #[test]
    fn import_pending_preserves_existing_retention_fence() -> Result<()> {
        let dir = tempfile::tempdir()?;
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700))?;
        let existing = dir.path().join(".execution-retention-pending");
        std::fs::write(&existing, b"existing exact restore fence")?;
        let frame = Frame::read(valid().as_slice())?.unwrap();
        assert!(write_pending(dir.path(), &frame).is_err());
        assert_eq!(std::fs::read(existing)?, b"existing exact restore fence");
        assert!(!dir.path().join(PENDING).exists());
        Ok(())
    }

    const SDK_READER: &str = "ARC_NATIVE_IMPORT_SDK_READER";
    const CODE: &[u8] = &[0x60, 9, 0x5f, 0x55, 0x00];
    fn recipient() -> Address {
        Address::repeat_byte(0x42)
    }
    fn transaction(spec: &ArcChainSpec) -> TransactionSigned {
        TxLegacy {
            chain_id: Some(5042002),
            nonce: 0,
            gas_price: u128::from(spec.inner.genesis_header().base_fee_per_gas.unwrap_or(1)) + 1,
            gas_limit: 100_000,
            to: TxKind::Call(recipient()),
            value: U256::from(100),
            ..Default::default()
        }
        .into_signed(Signature::test_signature())
        .into()
    }

    pub(crate) fn native(
        funded: bool,
    ) -> Result<(tempfile::TempDir, crate::native::Factory, Arc<ArcChainSpec>)> {
        let dir = tempfile::tempdir()?;
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700))?;
        let db = init_db(
            dir.path().join("db"),
            DatabaseArguments::test().with_exclusive(Some(false)),
        )?;
        let sf = StaticFileProvider::read_write(dir.path().join("static_files"))?;
        let rocks = RocksDBProvider::builder(dir.path().join("rocksdb"))
            .with_default_tables()
            .build()?;
        // Synthetic genesis injection exists ONLY inside this test module. The
        // production frame decoder always requires the official testnet genesis.
        let spec = if funded {
            let tx = transaction(&TESTNET);
            let mut genesis = TESTNET.inner.genesis.clone();
            genesis
                .alloc
                .entry(tx.recover_signer()?)
                .or_default()
                .balance = U256::from(1_000_000_000_000_000_000_000_000_u128);
            let account = genesis.alloc.entry(recipient()).or_default();
            account.nonce = Some(1);
            account.balance = U256::from(10);
            account.code = Some(CODE.to_vec().into());
            account.storage = Some(std::collections::BTreeMap::from([(
                B256::ZERO,
                B256::from(U256::from(7)),
            )]));
            Arc::new(ArcChainSpec::from(genesis))
        } else {
            TESTNET.clone()
        };
        let f = ProviderFactory::new(db, spec.clone(), sf, rocks, Runtime::test())?;
        reth_db_common::init::init_genesis(&f)?;
        assert!(f.provider()?.cached_storage_settings().storage_v2);
        let genesis = dir.path().join("sdk-genesis.json");
        std::fs::write(&genesis, serde_json::to_vec(&spec.inner.genesis)?)?;
        std::fs::set_permissions(genesis, std::fs::Permissions::from_mode(0o600))?;
        Ok((dir, f, spec))
    }

    // ONE actual official Arc EVM run. Persisting the resulting SDK object below
    // cannot call this executor again: save_blocks receives the outcome, not an EVM.
    pub(crate) fn first(
        f: &crate::native::Factory,
        spec: Arc<ArcChainSpec>,
    ) -> Result<ExecutedBlock> {
        next(f, spec, 0)
    }
    pub(crate) fn next(
        f: &crate::native::Factory,
        spec: Arc<ArcChainSpec>,
        height: u64,
    ) -> Result<ExecutedBlock> {
        let parent = f.provider()?.header_by_number(height)?.unwrap();
        let mut block = Block {
            header: Header {
                number: height + 1,
                parent_hash: parent.hash_slow(),
                timestamp: parent.timestamp + 1,
                gas_limit: 30_000_000,
                parent_beacon_block_root: Some(B256::ZERO),
                ..parent.clone()
            },
            body: BlockBody {
                transactions: if height == 0
                    && spec
                        .inner
                        .genesis
                        .alloc
                        .get(&recipient())
                        .and_then(|a| a.code.as_ref())
                        .is_some_and(|b| b.as_ref() == CODE)
                {
                    vec![transaction(&spec)]
                } else {
                    vec![]
                },
                withdrawals: parent.withdrawals_root.map(|_| Default::default()),
                ..Default::default()
            },
        };
        let config = ArcEvmConfig::new(EthEvmConfig::new_with_evm_factory(
            spec.clone(),
            ArcEvmFactory::new(spec),
        ));
        let recovered = RecoveredBlock::try_recover(block.clone())?;
        let output = config
            .executor(reth_revm::database::StateProviderDatabase::new(f.latest()?))
            .execute(&recovered)?;
        let hashed = HashedPostState::from_bundle_state::<reth_trie::KeccakKeyHasher>(
            output.state.state.iter(),
        );
        let (root, updates) = f.latest()?.state_root_with_updates(hashed.clone())?;
        block.header.state_root = root;
        block.header.gas_used = output.result.gas_used;
        block.header.receipts_root = proofs::calculate_receipt_root(
            &output
                .result
                .receipts
                .iter()
                .map(|r| r.with_bloom_ref())
                .collect::<Vec<_>>(),
        );
        block.header.transactions_root =
            proofs::calculate_transaction_root(&block.body.transactions);
        block.header.logs_bloom = Default::default();
        for r in &output.result.receipts {
            block.header.logs_bloom |= r.bloom();
        }
        assert!(
            !output.state.state.is_empty(),
            "exercise actual native state, not headers only"
        );
        Ok(ExecutedBlock::new(
            Arc::new(RecoveredBlock::try_recover(block)?),
            Arc::new(output),
            ComputedTrieData::without_trie_input(
                Arc::new(hashed.into_sorted()),
                Arc::new(updates.into_sorted()),
            ),
        ))
    }

    struct ReaderChild {
        child: Child,
        input: Option<ChildStdin>,
        lines: Option<mpsc::Receiver<String>>,
        drain: Option<thread::JoinHandle<()>>,
    }
    impl ReaderChild {
        fn open(native: &Path) -> Result<Self> {
            Self::start(native, "READY")
        }
        fn start(native: &Path, expected: &str) -> Result<Self> {
            let mut child = Command::new(std::env::current_exe()?)
                .args([
                    "--exact",
                    "native_import::tests::import_sdk_reader_child",
                    "--nocapture",
                ])
                .env_clear()
                .env(SDK_READER, native)
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::inherit())
                .spawn()?;
            let input = child.stdin.take();
            let output = child.stdout.take().unwrap();
            let (tx, rx) = mpsc::sync_channel(64);
            let drain = thread::spawn(move || {
                for line in BufReader::new(output).lines() {
                    match line {
                        Ok(line) => {
                            if tx.send(line).is_err() {
                                break;
                            }
                        }
                        Err(_) => break,
                    }
                }
            });
            let mut owned = Self {
                child,
                input,
                lines: Some(rx),
                drain: Some(drain),
            };
            owned.expect(expected)?;
            Ok(owned)
        }
        fn expect(&mut self, label: &str) -> Result<()> {
            let expected = format!("ARC_IMPORT_READER:{label}");
            loop {
                let line = self
                    .lines
                    .as_ref()
                    .unwrap()
                    .recv_timeout(Duration::from_secs(30))
                    .map_err(|_| eyre!("owned native reader did not acknowledge {label}"))?;
                if line.contains(&expected) {
                    return Ok(());
                }
            }
        }
        fn command(&mut self, command: &str, label: &str) -> Result<()> {
            let input = self.input.as_mut().unwrap();
            writeln!(input, "{command}")?;
            input.flush()?;
            self.expect(label)
        }
        fn finish(mut self) -> Result<()> {
            self.command("QUIT", "BYE")?;
            ensure!(self.child.wait()?.success(), "native reader child failed");
            Ok(())
        }
        fn exited(mut self) -> Result<()> {
            self.input.take();
            ensure!(self.child.wait()?.success(), "native fence probe failed");
            Ok(())
        }
    }
    impl Drop for ReaderChild {
        fn drop(&mut self) {
            self.input.take();
            if !matches!(self.child.try_wait(), Ok(Some(_))) {
                let _ = self.child.kill();
            }
            let _ = self.child.wait();
            self.lines.take();
            if let Some(drain) = self.drain.take() {
                let _ = drain.join();
            }
        }
    }

    #[test]
    fn import_sdk_reader_child() -> Result<()> {
        let Some(native) = std::env::var_os(SDK_READER) else {
            return Ok(());
        };
        let native = std::path::PathBuf::from(native);
        if require_clean(&native).is_err() {
            println!("ARC_IMPORT_READER:FENCED_ON_START");
            std::io::stdout().flush()?;
            return Ok(());
        }
        let genesis: alloy_genesis::Genesis =
            serde_json::from_slice(&std::fs::read(native.join("sdk-genesis.json"))?)?;
        let spec = Arc::new(ArcChainSpec::from(genesis));
        let (ro, scratch) = crate::native::open_readonly(&native, spec.clone(), false)?;
        assert_eq!(
            ro.provider()?.block_hash(0)?,
            Some(spec.inner.genesis_hash())
        );
        assert_eq!(ro.provider()?.best_block_number()?, 0);
        println!("ARC_IMPORT_READER:READY");
        std::io::stdout().flush()?;
        for command in std::io::stdin().lock().lines() {
            let command = command?;
            if command != "QUIT" && require_clean(&native).is_err() {
                println!("ARC_IMPORT_READER:FENCED");
                std::io::stdout().flush()?;
                continue;
            }
            match command.as_str() {
                "READ" => {
                    let provider = ro.provider()?;
                    assert_eq!(provider.best_block_number()?, 1);
                    let block = provider.block(1.into())?.unwrap();
                    let receipts = provider.receipts_by_block(1.into())?.unwrap();
                    assert_eq!(receipts.len(), block.body.transactions.len());
                    assert_eq!(
                        ro.latest()?.state_root(Default::default())?,
                        block.header.state_root
                    );
                    let parent = provider.header_by_number(0)?.unwrap().seal_slow();
                    let state = provider.history_by_block_hash(parent.hash())?;
                    let config = ArcEvmConfig::new(EthEvmConfig::new_with_evm_factory(
                        spec.clone(),
                        ArcEvmFactory::new(spec.clone()),
                    ));
                    let verified = crate::replay(&config, &state, &parent, &block, &receipts, 1)?;
                    assert_eq!(verified.result().state_root, block.header.state_root);
                    drop(state);
                    drop(provider);
                    println!("ARC_IMPORT_READER:READ_OK");
                    std::io::stdout().flush()?;
                }
                "ZERO" => {
                    assert_eq!(ro.provider()?.best_block_number()?, 0);
                    assert_eq!(
                        ro.latest()?.state_root(Default::default())?,
                        spec.inner.genesis_header().state_root
                    );
                    println!("ARC_IMPORT_READER:ZERO_OK");
                    std::io::stdout().flush()?;
                }
                "QUIT" => break,
                _ => bail!("unknown SDK fixture command"),
            }
        }
        drop(ro);
        drop(scratch);
        println!("ARC_IMPORT_READER:BYE");
        std::io::stdout().flush()?;
        Ok(())
    }

    #[test]
    fn import_sdk_full_storage_reopen_readonly_refresh_and_second_replay() -> Result<()> {
        for funded in [false, true] {
            let (dir, f, spec) = native(funded)?;
            let mut reader = ReaderChild::open(dir.path())?;
            let executed = first(&f, spec.clone())?;
            let hash = executed.recovered_block().hash();
            let root = executed.recovered_block().header().state_root;
            assert_eq!(
                executed.execution_output.result.receipts.len(),
                usize::from(funded)
            );
            let p = f.provider_rw()?;
            p.save_blocks(vec![executed], SaveBlocksMode::Full)?;
            p.commit()?;
            assert_eq!(f.provider()?.block_hash(1)?, Some(hash));
            let accounts = f.provider()?.account_block_changeset(1)?;
            let storage = f.provider()?.storage_block_changeset(1)?;
            assert!(
                !accounts.is_empty() || !storage.is_empty(),
                "Full persistence must include actual state history"
            );
            if funded {
                assert!(
                    !accounts.is_empty(),
                    "transaction account changes must persist"
                );
            }
            assert_eq!(f.latest()?.state_root(Default::default())?, root);
            if funded {
                assert_eq!(
                    f.latest()?.storage(recipient(), B256::ZERO)?,
                    Some(U256::from(9))
                );
            }
            reader.command("READ", "READ_OK")?;
            let work = tempfile::tempdir()?;
            std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
            let exported = work.path().join("full.ndjson");
            crate::native::export_checkpoint(&f, &spec, 1, None, &exported, work.path())?;
            let (manifest, keys) =
                crate::checkpoint::seed(&exported, "5042002", spec.inner.genesis_hash())?;
            assert_eq!(manifest.r#ref.height, 1);
            assert_eq!(manifest.root, root);
            assert!(!keys.is_empty());
            let p = f.provider_rw()?;
            p.remove_block_and_execution_above(0)?;
            p.commit()?;
            assert_eq!(f.provider()?.best_block_number()?, 0);
            assert_eq!(
                f.latest()?.state_root(Default::default())?,
                spec.inner.genesis_header().state_root
            );
            reader.command("ZERO", "ZERO_OK")?;
            let repeated = first(&f, spec.clone())?;
            assert_eq!(repeated.recovered_block().hash(), hash);
            let p = f.provider_rw()?;
            p.save_blocks(vec![repeated], SaveBlocksMode::Full)?;
            p.commit()?;
            reader.command("READ", "READ_OK")?;
            reader.finish()?;
            drop(f);
            let (reopened, _scratch) = crate::native::open_readonly(dir.path(), spec, false)?;
            assert_eq!(reopened.provider()?.block_hash(1)?, Some(hash));
            assert_eq!(reopened.latest()?.state_root(Default::default())?, root);
        }
        Ok(())
    }

    #[test]
    fn import_sdk_interrupted_save_and_unwind_remain_fenced_across_process_restart() -> Result<()> {
        for phase in [
            "save-precommit",
            "save-committed",
            "unwind-precommit",
            "unwind-committed",
        ] {
            let (dir, f, spec) = native(false)?;
            let mut reader = ReaderChild::open(dir.path())?;
            if phase.starts_with("unwind") {
                let executed = first(&f, spec.clone())?;
                let p = f.provider_rw()?;
                p.save_blocks(vec![executed], SaveBlocksMode::Full)?;
                p.commit()?;
                reader.command("READ", "READ_OK")?;
            }
            let frame = Frame::read(valid().as_slice())?.unwrap();
            write_pending(dir.path(), &frame)?;
            let executed = if phase.starts_with("save") {
                Some(first(&f, spec.clone())?)
            } else {
                None
            };
            let p = f.provider_rw()?;
            if let Some(executed) = executed {
                p.save_blocks(vec![executed], SaveBlocksMode::Full)?;
            } else {
                p.remove_block_and_execution_above(0)?;
            }
            if phase.ends_with("committed") {
                p.commit()?;
            } else {
                drop(p);
            }
            // This is a controlled interrupted-transaction qualification. It is
            // not a power-loss/automatic multi-store repair proof (N2 owns that).
            assert!(require_clean(dir.path()).is_err(), "{phase}");
            reader.command("READ", "FENCED")?;
            reader.finish()?;
            drop(f);
            assert!(require_clean(dir.path()).is_err());
            ReaderChild::start(dir.path(), "FENCED_ON_START")?.exited()?;
            let pending = std::fs::read(dir.path().join(PENDING))?;
            assert!(!pending.is_empty(), "original intent must survive {phase}");
        }
        Ok(())
    }
}
