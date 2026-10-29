//! Owned first execution and durable native publication. Never an E1 ACK.
use crate::{
    bootstrap, checkpoint, native,
    native_import::{self, Frame},
};
use alloy_consensus::{ReceiptWithBloom, TxReceipt, Typed2718, proofs};
use alloy_eips::eip2718::Encodable2718;
use alloy_primitives::B256;
use arc_evm::{ArcEvmConfig, ArcEvmFactory};
use arc_execution_config::chainspec::{ArcChainSpec, TESTNET};
use eyre::{Result, ensure, eyre};
use reth_chain_state::{ComputedTrieData, ExecutedBlock};
use reth_ethereum::{
    evm::EthEvmConfig,
    provider::{
        BlockHashReader, BlockReader, HeaderProvider, ProviderFactory, ReceiptProvider,
        SaveBlocksMode,
        db::{mdbx::DatabaseArguments, open_db},
        providers::{RocksDBProvider, StaticFileProvider},
    },
    tasks::Runtime,
};
use reth_ethereum_primitives::{Block, Receipt};
use reth_evm::{ConfigureEvm, execute::Executor};
use reth_primitives_traits::RecoveredBlock;
use reth_storage_api::{BlockNumReader, StateRootProvider, StorageSettingsCache};
use reth_trie::HashedPostState;
use rustix::fs::{FlockOperation, Mode, OFlags};
use serde::{Deserialize, Serialize, de::DeserializeOwned};
use sha2::{Digest, Sha256};
use std::{
    fs::File,
    io::{Read, Write},
    os::unix::fs::{MetadataExt, PermissionsExt},
    path::{Path, PathBuf},
    sync::Arc,
};

pub const IDENTITY: &str = ".arc-native-import.identity.json";
pub const HEAD: &str = ".arc-native-import.head.json";
pub const RECEIPTS: &str = ".arc-native-import-receipts";
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct NativeRef {
    pub height: u64,
    pub hash: B256,
    pub root: B256,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Identity {
    pub version: u8,
    pub instance: B256,
    #[serde(rename = "ChainID")]
    pub chain_id: u64,
    pub genesis: B256,
    pub arc_revision: String,
    pub reth_revision: String,
    pub layout: String,
    pub bootstrap_digest: B256,
    pub anchor: NativeRef,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Head {
    pub version: u8,
    pub instance: B256,
    pub import_epoch: u64,
    pub read_epoch: u64,
    #[serde(rename = "RequestID")]
    pub request_id: u64,
    pub tip: NativeRef,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct ImportReceipt {
    pub version: u8,
    pub identity: Identity,
    #[serde(rename = "RequestID")]
    pub request_id: u64,
    pub import_epoch: u64,
    pub read_epoch: u64,
    pub height: u64,
    pub hash: B256,
    pub parent: B256,
    pub parent_root: B256,
    pub root: B256,
    pub receipts_root: B256,
    pub input_digest: B256,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
struct Intent {
    version: u8,
    identity: Identity,
    previous: Head,
    meta: native_import::Meta,
    digest: B256,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Phase {
    BeforeExecution,
    Executed,
    Intent,
    Saved,
    Committed,
    Receipt,
    Head,
    Cleared,
}

fn directory(path: &Path) -> Result<File> {
    ensure!(path.is_absolute(), "absolute native directory required");
    let file = File::from(rustix::fs::open(
        path,
        OFlags::RDONLY | OFlags::DIRECTORY | OFlags::NOFOLLOW | OFlags::CLOEXEC,
        Mode::empty(),
    )?);
    let m = file.metadata()?;
    ensure!(
        m.is_dir() && m.uid() == rustix::process::geteuid().as_raw() && m.mode() & 0o777 == 0o700,
        "owned0700 directory required"
    );
    Ok(file)
}
fn lock_file(native: &Path, suffix: &str) -> Result<File> {
    let parent = native.parent().ok_or_else(|| eyre!("native parent"))?;
    let parent_fd = directory(parent)?;
    let name = format!(
        ".{}{}",
        native
            .file_name()
            .and_then(|s| s.to_str())
            .ok_or_else(|| eyre!("native name"))?,
        suffix
    );
    let file = File::from(rustix::fs::openat(
        &parent_fd,
        name.as_str(),
        OFlags::RDWR | OFlags::CREATE | OFlags::NOFOLLOW | OFlags::CLOEXEC,
        Mode::RUSR | Mode::WUSR,
    )?);
    let m = file.metadata()?;
    ensure!(
        m.is_file()
            && m.uid() == rustix::process::geteuid().as_raw()
            && m.mode() & 0o777 == 0o600
            && m.nlink() == 1,
        "private single-link lock required"
    );
    Ok(file)
}
/// Turnstile prevents arriving readers overtaking a writer waiting for the view.
/// Go's managed collector must use these exact sibling names and acquisition order.
pub struct ReadGuard {
    _view: File,
    _run: File,
}
struct WriteGuard {
    _view: File,
    _turn: File,
}
fn gate(native: &Path, write: bool) -> Result<(File, File)> {
    let turn = lock_file(native, ".arc-native-import-turn")?;
    rustix::fs::flock(&turn, FlockOperation::LockExclusive)?;
    let view = lock_file(native, ".arc-native-import-view")?;
    rustix::fs::flock(
        &view,
        if write {
            FlockOperation::LockExclusive
        } else {
            FlockOperation::LockShared
        },
    )?;
    Ok((turn, view))
}
pub fn read_guard(native: &Path) -> Result<ReadGuard> {
    let run = lock_file(native, ".execution-retention-lock")?;
    rustix::fs::flock(&run, FlockOperation::NonBlockingLockShared)?;
    let (turn, view) = gate(native, false)?;
    drop(turn);
    native_import::require_clean(native)?;
    native::check_fences(native)?;
    Ok(ReadGuard {
        _view: view,
        _run: run,
    })
}
fn write_guard(native: &Path) -> Result<WriteGuard> {
    let (turn, view) = gate(native, true)?;
    Ok(WriteGuard {
        _view: view,
        _turn: turn,
    })
}
#[derive(Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
struct Sealed {
    data: Box<serde_json::value::RawValue>,
    digest: B256,
}
fn private_bytes(path: &Path) -> Result<Vec<u8>> {
    checkpoint::private_file(path)?;
    let file = File::from(rustix::fs::open(
        path,
        OFlags::RDONLY | OFlags::NOFOLLOW | OFlags::NONBLOCK | OFlags::CLOEXEC,
        Mode::empty(),
    )?);
    let m = file.metadata()?;
    ensure!(
        m.is_file()
            && m.uid() == rustix::process::geteuid().as_raw()
            && m.mode() & 0o777 == 0o600
            && m.nlink() == 1
            && m.len() <= native_import::MAX_META as u64,
        "private bounded artifact required"
    );
    let mut bytes = Vec::new();
    file.take((native_import::MAX_META + 1) as u64)
        .read_to_end(&mut bytes)?;
    ensure!(
        bytes.len() <= native_import::MAX_META,
        "artifact byte limit"
    );
    Ok(bytes)
}
fn canonical<T: DeserializeOwned + Serialize>(bytes: &[u8]) -> Result<T> {
    let value = serde_json::from_slice::<T>(bytes)?;
    ensure!(
        serde_json::to_vec(&value)? == bytes,
        "canonical exact artifact required"
    );
    Ok(value)
}
fn artifact<T: DeserializeOwned + Serialize>(path: &Path) -> Result<T> {
    let sealed: Sealed = canonical(&private_bytes(path)?)?;
    let data = sealed.data.get().as_bytes();
    ensure!(
        B256::from_slice(&Sha256::digest(data)) == sealed.digest,
        "native artifact checksum mismatch"
    );
    canonical(data)
}
fn sealed_bytes<T: Serialize>(value: &T) -> Result<Vec<u8>> {
    let data = serde_json::value::to_raw_value(value)?;
    let digest = B256::from_slice(&Sha256::digest(data.get().as_bytes()));
    let bytes = serde_json::to_vec(&Sealed { data, digest })?;
    ensure!(
        bytes.len() <= native_import::MAX_META,
        "native control byte limit"
    );
    Ok(bytes)
}
fn create<T: Serialize>(path: &Path, value: &T) -> Result<()> {
    let bytes = sealed_bytes(value)?;
    checkpoint::write_private(path, |w| {
        w.write_all(&bytes)?;
        Ok(())
    })
}
fn replace_head(path: &Path, old: &Head, new: &Head) -> Result<()> {
    ensure!(artifact::<Head>(path)? == *old, "native head changed");
    let parent = path.parent().ok_or_else(|| eyre!("head parent"))?;
    let mut temp = tempfile::NamedTempFile::new_in(parent)?;
    temp.write_all(&sealed_bytes(new)?)?;
    temp.as_file().sync_all()?;
    temp.persist(path)?;
    File::open(parent)?.sync_all()?;
    Ok(())
}
fn receipt_path(native: &Path, epoch: u64, height: u64) -> PathBuf {
    native.join(RECEIPTS).join(format!("{epoch}-{height}.json"))
}
fn live_factory(path: &Path, spec: Arc<ArcChainSpec>) -> Result<native::Factory> {
    native::layout(path)?;
    let db = open_db(
        path.join("db"),
        DatabaseArguments::default().with_exclusive(Some(false)),
    )?;
    let sf = StaticFileProvider::read_write(path.join("static_files"))?;
    let rocks = RocksDBProvider::builder(path.join("rocksdb"))
        .with_default_tables()
        .build()?;
    Ok(ProviderFactory::new(db, spec, sf, rocks, Runtime::test())?)
}
fn check_native(f: &native::Factory, head: &Head, identity: &Identity) -> Result<()> {
    let p = f.provider()?;
    ensure!(
        p.cached_storage_settings().storage_v2
            && p.block_hash(0)? == Some(identity.genesis)
            && p.best_block_number()? == head.tip.height
            && p.block_hash(head.tip.height)? == Some(head.tip.hash)
            && p.header_by_number(head.tip.height)?
                .is_some_and(|h| h.state_root == head.tip.root)
            && p.latest().state_root(Default::default())? == head.tip.root
            && crate::history::floor(f)? >= identity.anchor.height
            && crate::history::floor(f)? <= head.tip.height,
        "native identity/completed head/root/floor mismatch"
    );
    Ok(())
}
fn check_completion(
    path: &Path,
    f: &native::Factory,
    head: &Head,
    identity: &Identity,
) -> Result<()> {
    ensure!(
        head.import_epoch == 1
            && head.request_id == head.tip.height - identity.anchor.height
            && head.request_id.checked_add(1) == Some(head.read_epoch),
        "unsupported or inconsistent native sequence/epoch"
    );
    if head.tip == identity.anchor {
        return Ok(());
    }
    let r: ImportReceipt = artifact(&receipt_path(path, head.import_epoch, head.tip.height))?;
    let p = f.provider()?;
    let h = p
        .header_by_number(head.tip.height)?
        .ok_or_else(|| eyre!("completion header absent"))?;
    let parent = p
        .header_by_number(head.tip.height - 1)?
        .ok_or_else(|| eyre!("completion parent absent"))?;
    ensure!(
        r.version == 1
            && r.identity == *identity
            && r.request_id == head.request_id
            && r.import_epoch == head.import_epoch
            && r.read_epoch == head.read_epoch
            && r.height == head.tip.height
            && r.hash == head.tip.hash
            && r.root == head.tip.root
            && r.parent == h.parent_hash
            && r.parent_root == parent.state_root
            && r.receipts_root == h.receipts_root
            && r.input_digest != B256::ZERO,
        "native completed receipt/head mismatch"
    );
    Ok(())
}
fn validate_identity(
    i: &Identity,
    boot: &bootstrap::ReceiptRecord,
    digest: B256,
    spec: &ArcChainSpec,
) -> Result<()> {
    ensure!(
        i.version == 1
            && i.instance != B256::ZERO
            && i.chain_id == 5042002
            && i.genesis == spec.inner.genesis_hash()
            && i.arc_revision == crate::ARC_REVISION
            && i.reth_revision == native_import::RETH_REVISION
            && i.layout == "reth-native-v2"
            && i.bootstrap_digest == digest
            && i.anchor
                == NativeRef {
                    height: boot.anchor.r#ref.height,
                    hash: boot.anchor.r#ref.hash,
                    root: boot.anchor.root
                },
        "native identity/pins/original anchor mismatch"
    );
    Ok(())
}
fn decoded(frame: &Frame) -> Result<(Block, Vec<Receipt>)> {
    let block: Block = alloy_rlp::decode_exact(frame.block_rlp())?;
    let receipts: Vec<ReceiptWithBloom<Receipt>> = alloy_rlp::decode_exact(frame.receipts_rlp())?;
    let h = &block.header;
    let m = frame.meta();
    ensure!(
        h.number == m.height
            && h.hash_slow() == m.hash
            && h.parent_hash == m.parent
            && h.timestamp <= i64::MAX as u64
            && h.gas_used <= h.gas_limit
            && block.body.transactions.len() <= 262_144
            && proofs::calculate_transaction_root(&block.body.transactions) == h.transactions_root
            && proofs::calculate_ommers_root(&block.body.ommers) == h.ommers_hash
            && receipts.len() == block.body.transactions.len()
            && proofs::calculate_receipt_root(&receipts) == h.receipts_root,
        "native payload identity/body/receipts"
    );
    ensure!(
        match (&block.body.withdrawals, h.withdrawals_root) {
            (Some(w), Some(r)) => proofs::calculate_withdrawals_root(w) == r,
            (None, None) => true,
            _ => false,
        },
        "native withdrawal commitment"
    );
    let mut bloom = alloy_primitives::Bloom::ZERO;
    let mut gas = 0;
    for (r, t) in receipts.iter().zip(&block.body.transactions) {
        ensure!(
            r.receipt.bloom() == r.logs_bloom
                && r.receipt.ty() == t.ty()
                && r.receipt.cumulative_gas_used() >= gas,
            "native receipt bloom/type/gas"
        );
        gas = r.receipt.cumulative_gas_used();
        bloom |= r.logs_bloom;
    }
    ensure!(
        gas == h.gas_used && bloom == h.logs_bloom,
        "native cumulative gas/bloom"
    );
    Ok((block, receipts.into_iter().map(|r| r.receipt).collect()))
}
fn encode_receipts(receipts: &[Receipt]) -> Vec<u8> {
    let mut data = Vec::new();
    for r in receipts {
        r.with_bloom_ref().network_encode(&mut data);
    }
    let mut out = Vec::new();
    alloy_rlp::Header {
        list: true,
        payload_length: data.len(),
    }
    .encode(&mut out);
    out.extend(data);
    out
}

// A transparent test-only database wrapper pauses inside actual official EVM
// execution so the owning parent can SIGKILL its child without SDK patches.
#[cfg(test)]
#[derive(Debug)]
struct ObservedDB<D>(D);
#[cfg(test)]
impl<D: revm::Database> revm::Database for ObservedDB<D> {
    type Error = D::Error;
    fn basic(
        &mut self,
        address: alloy_primitives::Address,
    ) -> std::result::Result<Option<revm::state::AccountInfo>, Self::Error> {
        processes::vm_read_hook();
        self.0.basic(address)
    }
    fn code_by_hash(
        &mut self,
        hash: B256,
    ) -> std::result::Result<revm::bytecode::Bytecode, Self::Error> {
        self.0.code_by_hash(hash)
    }
    fn storage(
        &mut self,
        address: alloy_primitives::Address,
        key: alloy_primitives::U256,
    ) -> std::result::Result<alloy_primitives::U256, Self::Error> {
        self.0.storage(address, key)
    }
    fn block_hash(&mut self, number: u64) -> std::result::Result<B256, Self::Error> {
        self.0.block_hash(number)
    }
}

pub struct Writer {
    f: native::Factory,
    spec: Arc<ArcChainSpec>,
    path: PathBuf,
    directory: File,
    identity: Identity,
    head: Head,
    executions: u64,
    _sole: File,
    _run: File,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
pub struct Ready {
    pub version: u8,
    pub identity: Identity,
    pub head: Head,
}
fn reply(out: &mut impl Write, magic: &[u8; 4], id: u64, value: &impl Serialize) -> Result<()> {
    let bytes = serde_json::to_vec(value)?;
    ensure!(
        !bytes.is_empty() && bytes.len() <= native_import::MAX_META,
        "native response limit"
    );
    out.write_all(magic)?;
    out.write_all(&id.to_be_bytes())?;
    out.write_all(&(bytes.len() as u32).to_be_bytes())?;
    out.write_all(&bytes)?;
    out.flush()?;
    Ok(())
}
pub fn serve(mut input: impl Read, mut output: impl Write, native: &Path) -> Result<()> {
    let mut writer = Writer::open(native)?;
    reply(
        &mut output,
        b"AWH1",
        0,
        &Ready {
            version: 1,
            identity: writer.identity.clone(),
            head: writer.head.clone(),
        },
    )?;
    while let Some(frame) = Frame::read(&mut input)? {
        let receipt = writer.import(&frame)?;
        reply(&mut output, b"AWR1", frame.meta().request_id, &receipt)?;
    }
    Ok(())
}
impl Writer {
    #[cfg(test)]
    fn open_fixture(path: &Path, spec: Arc<ArcChainSpec>) -> Result<Self> {
        Self::open_inner(path, spec)
    }
    pub fn open(path: &Path) -> Result<Self> {
        Self::open_inner(path, TESTNET.clone())
    }
    fn open_inner(path: &Path, spec: Arc<ArcChainSpec>) -> Result<Self> {
        ensure!(
            spec.inner.chain.id() == 5042002,
            "only audited Arc testnet supported"
        );
        let dir = directory(path)?;
        let run = lock_file(path, ".execution-retention-lock")?;
        rustix::fs::flock(&run, FlockOperation::NonBlockingLockShared)?;
        let sole = lock_file(path, ".arc-native-import-writer")?;
        rustix::fs::flock(&sole, FlockOperation::NonBlockingLockExclusive)?;
        let guard = write_guard(path)?;
        native_import::require_clean(path)?;
        native::check_fences(path)?;
        let boot_bytes = private_bytes(&path.join(bootstrap::RECEIPT))?;
        let boot: bootstrap::ReceiptRecord = canonical(&boot_bytes)?;
        ensure!(
            boot.version == 1
                && boot.arc_revision == crate::ARC_REVISION
                && boot.reth_revision == native_import::RETH_REVISION
                && boot.anchor.version == 1
                && boot.anchor.coverage == "full"
                && boot.anchor.chain.id == "5042002"
                && boot.anchor.chain.genesis == spec.inner.genesis_hash()
                && boot.anchor.r#ref.generation == 1
                && boot.floor == boot.anchor.r#ref.height
                && boot.next
                    == boot
                        .floor
                        .checked_add(1)
                        .ok_or_else(|| eyre!("bootstrap height"))?,
            "verified native bootstrap required"
        );
        let boot_digest = B256::from_slice(&Sha256::digest(&boot_bytes));
        let f = live_factory(path, spec.clone())?;
        let identity_path = path.join(IDENTITY);
        let head_path = path.join(HEAD);
        let (identity, head) = match std::fs::symlink_metadata(&identity_path) {
            Ok(_) => {
                let i: Identity = artifact(&identity_path)?;
                let h: Head = artifact(&head_path)?;
                (i, h)
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                ensure!(
                    matches!(std::fs::symlink_metadata(&head_path),Err(e) if e.kind()==std::io::ErrorKind::NotFound)
                        && matches!(std::fs::symlink_metadata(path.join(RECEIPTS)),Err(e) if e.kind()==std::io::ErrorKind::NotFound),
                    "partial native initialization"
                );
                let mut random = [0u8; 32];
                getrandom::fill(&mut random)
                    .map_err(|_| eyre!("native instance entropy unavailable"))?;
                let i = Identity {
                    version: 1,
                    instance: B256::from(random),
                    chain_id: 5042002,
                    genesis: spec.inner.genesis_hash(),
                    arc_revision: crate::ARC_REVISION.into(),
                    reth_revision: native_import::RETH_REVISION.into(),
                    layout: "reth-native-v2".into(),
                    bootstrap_digest: boot_digest,
                    anchor: NativeRef {
                        height: boot.floor,
                        hash: boot.anchor.r#ref.hash,
                        root: boot.anchor.root,
                    },
                };
                let h = Head {
                    version: 1,
                    instance: i.instance,
                    import_epoch: 1,
                    read_epoch: 1,
                    request_id: 0,
                    tip: i.anchor.clone(),
                };
                check_native(&f, &h, &i)?;
                let init = serde_json::json!({"Version":1,"Operation":"initialize","Instance":i.instance,"BootstrapDigest":boot_digest});
                create(&path.join(native_import::PENDING), &init)?;
                create(&identity_path, &i)?;
                create(&head_path, &h)?;
                std::fs::create_dir(path.join(RECEIPTS))?;
                std::fs::set_permissions(
                    path.join(RECEIPTS),
                    std::fs::Permissions::from_mode(0o700),
                )?;
                ensure!(
                    artifact::<serde_json::Value>(&path.join(native_import::PENDING))? == init,
                    "native initialization intent changed"
                );
                std::fs::remove_file(path.join(native_import::PENDING))?;
                dir.sync_all()?;
                (i, h)
            }
            Err(e) => return Err(e.into()),
        };
        validate_identity(&identity, &boot, boot_digest, &spec)?;
        ensure!(
            head.version == 1
                && head.instance == identity.instance
                && head.import_epoch > 0
                && head.import_epoch < i64::MAX as u64
                && head.read_epoch > 0
                && head.read_epoch < i64::MAX as u64
                && head.request_id < i64::MAX as u64
                && head.tip.height >= identity.anchor.height
                && head.tip.height < i64::MAX as u64,
            "native head range/identity"
        );
        directory(&path.join(RECEIPTS))?;
        check_native(&f, &head, &identity)?;
        check_completion(path, &f, &head, &identity)?;
        drop(guard);
        Ok(Self {
            f,
            spec,
            path: path.into(),
            directory: dir,
            identity,
            head,
            executions: 0,
            _sole: sole,
            _run: run,
        })
    }
    pub fn identity(&self) -> &Identity {
        &self.identity
    }
    pub fn head(&self) -> &Head {
        &self.head
    }
    pub const fn executions(&self) -> u64 {
        self.executions
    }
    fn unchanged(&self) -> Result<()> {
        let current = directory(&self.path)?;
        ensure!(
            current.metadata()?.dev() == self.directory.metadata()?.dev()
                && current.metadata()?.ino() == self.directory.metadata()?.ino()
                && artifact::<Identity>(&self.path.join(IDENTITY))? == self.identity
                && artifact::<Head>(&self.path.join(HEAD))? == self.head,
            "native ownership/head changed"
        );
        check_completion(&self.path, &self.f, &self.head, &self.identity)?;
        Ok(())
    }
    pub fn import(&mut self, frame: &Frame) -> Result<ImportReceipt> {
        self.import_with_hook(frame, &mut |_| Ok(()))
    }
    fn import_with_hook(
        &mut self,
        frame: &Frame,
        hook: &mut impl FnMut(Phase) -> Result<()>,
    ) -> Result<ImportReceipt> {
        let m = frame.meta();
        ensure!(
            m.chain_id == self.identity.chain_id
                && m.genesis == self.identity.genesis
                && m.arc_revision == self.identity.arc_revision
                && m.reth_revision == self.identity.reth_revision
                && m.import_epoch == self.head.import_epoch,
            "native request identity/epoch"
        );
        let view = read_guard(&self.path)?;
        self.unchanged()?;
        check_native(&self.f, &self.head, &self.identity)?;
        if m.height <= self.head.tip.height {
            let r: ImportReceipt = artifact(&receipt_path(&self.path, m.import_epoch, m.height))?;
            ensure!(
                r.version == 1
                    && r.identity == self.identity
                    && r.request_id == m.request_id
                    && r.import_epoch == m.import_epoch
                    && r.height == m.height
                    && r.hash == m.hash
                    && r.parent == m.parent
                    && r.parent_root == m.parent_root
                    && r.input_digest == frame.digest()
                    && self.f.provider()?.block_hash(r.height)? == Some(r.hash),
                "native duplicate is not exact"
            );
            return Ok(r);
        }
        ensure!(
            self.head.tip.height.checked_add(1) == Some(m.height)
                && self.head.request_id.checked_add(1) == Some(m.request_id)
                && self.head.tip.hash == m.parent
                && self.head.tip.root == m.parent_root,
            "native parent/sequence mismatch"
        );
        let (block, receipts) = decoded(frame)?;
        hook(Phase::BeforeExecution)?;
        let executed = {
            let p = self.f.provider()?;
            let config = ArcEvmConfig::new(EthEvmConfig::new_with_evm_factory(
                self.spec.clone(),
                ArcEvmFactory::new(self.spec.clone()),
            ));
            let recovered = RecoveredBlock::try_recover(block.clone())?;
            self.executions = self
                .executions
                .checked_add(1)
                .ok_or_else(|| eyre!("execution counter overflow"))?;
            let db = reth_revm::database::StateProviderDatabase::new(p.latest());
            #[cfg(test)]
            let db = ObservedDB(db);
            let output = config.executor(db).execute(&recovered)?;
            let hashed = HashedPostState::from_bundle_state::<reth_trie::KeccakKeyHasher>(
                output.state.state.iter(),
            );
            let (root, updates) = p.latest().state_root_with_updates(hashed.clone())?;
            ensure!(
                root == block.header.state_root
                    && output.result.receipts == receipts
                    && output.result.gas_used == block.header.gas_used
                    && output.result.blob_gas_used == block.header.blob_gas_used.unwrap_or(0)
                    && match block.header.requests_hash {
                        Some(hash) => output.result.requests.requests_hash() == hash,
                        None => output.result.requests.is_empty(),
                    },
                "first Arc execution root/receipts/gas/requests mismatch"
            );
            ExecutedBlock::new(
                Arc::new(recovered),
                Arc::new(output),
                ComputedTrieData::without_trie_input(
                    Arc::new(hashed.into_sorted()),
                    Arc::new(updates.into_sorted()),
                ),
            )
        };
        hook(Phase::Executed)?;
        drop(view);
        let guard = write_guard(&self.path)?;
        native_import::require_clean(&self.path)?;
        native::check_fences(&self.path)?;
        self.unchanged()?;
        check_native(&self.f, &self.head, &self.identity)?;
        let intent = Intent {
            version: 2,
            identity: self.identity.clone(),
            previous: self.head.clone(),
            meta: m.clone(),
            digest: frame.digest(),
        };
        create(&self.path.join(native_import::PENDING), &intent)?;
        hook(Phase::Intent)?;
        let p = self.f.provider_rw()?;
        p.save_blocks(vec![executed], SaveBlocksMode::Full)?;
        hook(Phase::Saved)?;
        p.commit()?;
        hook(Phase::Committed)?;
        let new = Head {
            version: 1,
            instance: self.identity.instance,
            import_epoch: self.head.import_epoch,
            read_epoch: self
                .head
                .read_epoch
                .checked_add(1)
                .filter(|n| *n < i64::MAX as u64)
                .ok_or_else(|| eyre!("read epoch exhausted"))?,
            request_id: m.request_id,
            tip: NativeRef {
                height: m.height,
                hash: m.hash,
                root: block.header.state_root,
            },
        };
        check_native(&self.f, &new, &self.identity)?;
        {
            let p = self.f.provider()?;
            ensure!(
                p.block(m.height.into())?
                    .is_some_and(|b| alloy_rlp::encode(b) == frame.block_rlp())
                    && p.receipts_by_block(m.height.into())?
                        .is_some_and(|r| encode_receipts(&r) == frame.receipts_rlp()),
                "stored native payload differs from original input"
            );
        }
        let r = ImportReceipt {
            version: 1,
            identity: self.identity.clone(),
            request_id: m.request_id,
            import_epoch: m.import_epoch,
            read_epoch: new.read_epoch,
            height: m.height,
            hash: m.hash,
            parent: m.parent,
            parent_root: m.parent_root,
            root: block.header.state_root,
            receipts_root: block.header.receipts_root,
            input_digest: frame.digest(),
        };
        create(&receipt_path(&self.path, m.import_epoch, m.height), &r)?;
        hook(Phase::Receipt)?;
        replace_head(&self.path.join(HEAD), &self.head, &new)?;
        self.head = new;
        hook(Phase::Head)?;
        ensure!(
            artifact::<Intent>(&self.path.join(native_import::PENDING))? == intent,
            "native pending changed"
        );
        self.unchanged()?;
        std::fs::remove_file(self.path.join(native_import::PENDING))?;
        self.directory.sync_all()?;
        hook(Phase::Cleared)?;
        drop(guard);
        Ok(r)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        bootstrap, checkpoint, native,
        native_import::{
            Meta, RETH_REVISION,
            tests::{first, native as fixture},
        },
    };
    use arc_execution_config::chainspec::TESTNET;
    use std::{io::Write, os::unix::fs::PermissionsExt, path::PathBuf};

    pub(super) fn setup() -> Result<(tempfile::TempDir, PathBuf, Frame)> {
        let (_source, f, spec) = fixture(false)?;
        let work = tempfile::tempdir()?;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let state = work.path().join("state.ndjson");
        native::export_checkpoint(&f, &spec, 0, None, &state, work.path())?;
        let (m, _) = checkpoint::seed(&state, "5042002", spec.inner.genesis_hash())?;
        let expected = work.path().join("expected.json");
        checkpoint::write_private(&expected, |w| {
            serde_json::to_writer(w, &m)?;
            Ok(())
        })?;
        let path = work.path().join("native");
        let absent = work.path().join("unused");
        bootstrap::build(&bootstrap::Inputs {
            target: &path,
            state: &state,
            expected: &expected,
            headers: &absent,
            block: &absent,
            receipts: &absent,
        })?;
        let one = first(&f, spec)?;
        let block = alloy_rlp::encode(one.recovered_block().clone().into_block());
        let meta = Meta {
            version: 1,
            request_id: 1,
            chain_id: 5042002,
            genesis: TESTNET.inner.genesis_hash(),
            arc_revision: crate::ARC_REVISION.into(),
            reth_revision: RETH_REVISION.into(),
            import_epoch: 1,
            height: 1,
            hash: one.recovered_block().hash(),
            parent: m.r#ref.hash,
            parent_root: m.root,
        };
        let meta = serde_json::to_vec(&meta)?;
        let mut data = b"AIN1".to_vec();
        for n in [meta.len(), block.len(), 1] {
            data.write_all(&(n as u32).to_be_bytes())?;
        }
        data.extend(meta);
        data.extend(block);
        data.push(0xc0);
        Ok((work, path, Frame::read(data.as_slice())?.unwrap()))
    }

    #[test]
    fn writer_first_execution_persists_and_exact_duplicate_survives_restart() -> Result<()> {
        use reth_storage_api::{BlockHashReader, StateRootProvider};
        let (_work, path, frame) = setup()?;
        let mut writer = Writer::open(&path)?;
        assert_eq!(writer.executions(), 0);
        let receipt = writer.import(&frame)?;
        assert_eq!(writer.executions(), 1);
        assert_eq!(receipt.input_digest, frame.digest());
        assert_eq!(receipt.hash, frame.meta().hash);
        assert_eq!(receipt.height, 1);
        assert!(
            Writer::open(&path).is_err(),
            "sole writer must be exclusive"
        );
        drop(writer);
        let mut writer = Writer::open(&path)?;
        assert_eq!(
            writer.import(&frame)?,
            receipt,
            "exact duplicate must not execute again"
        );
        assert_eq!(writer.executions(), 0);
        drop(writer);
        let (f, _scratch) = native::open_readonly(&path, TESTNET.clone(), true)?;
        assert_eq!(f.provider()?.block_hash(1)?, Some(receipt.hash));
        assert_eq!(f.latest()?.state_root(Default::default())?, receipt.root);
        assert!(!path.join(crate::native_import::PENDING).exists());
        Ok(())
    }

    pub(super) fn encoded(frame: &Frame) -> Result<Vec<u8>> {
        let meta = serde_json::to_vec(frame.meta())?;
        let mut out = b"AIN1".to_vec();
        for n in [
            meta.len(),
            frame.block_rlp().len(),
            frame.receipts_rlp().len(),
        ] {
            out.write_all(&(n as u32).to_be_bytes())?;
        }
        out.extend(meta);
        out.extend(frame.block_rlp());
        out.extend(frame.receipts_rlp());
        Ok(out)
    }
    #[test]
    fn writer_protocol_binds_ready_and_committed_reply_to_the_exact_input() -> Result<()> {
        let (_work, path, frame) = setup()?;
        let input = encoded(&frame)?;
        let mut output = Vec::new();
        serve(input.as_slice(), &mut output, &path)?;
        let mut bytes = output.as_slice();
        fn message(bytes: &mut &[u8]) -> Result<([u8; 4], u64, Vec<u8>)> {
            let mut h = [0u8; 16];
            bytes.read_exact(&mut h)?;
            let mut magic = [0; 4];
            magic.copy_from_slice(&h[..4]);
            let id = u64::from_be_bytes(h[4..12].try_into()?);
            let n = u32::from_be_bytes(h[12..].try_into()?) as usize;
            assert!(n <= native_import::MAX_META);
            let mut data = vec![0; n];
            bytes.read_exact(&mut data)?;
            Ok((magic, id, data))
        }
        let (magic, id, data) = message(&mut bytes)?;
        assert_eq!(&magic, b"AWH1");
        assert_eq!(id, 0);
        let hello: serde_json::Value = serde_json::from_slice(&data)?;
        assert_eq!(hello["Head"]["Tip"]["Height"], 0);
        let (magic, id, data) = message(&mut bytes)?;
        assert_eq!(&magic, b"AWR1");
        assert_eq!(id, frame.meta().request_id);
        let receipt: ImportReceipt = serde_json::from_slice(&data)?;
        assert_eq!(receipt.input_digest, frame.digest());
        assert_eq!(receipt.hash, frame.meta().hash);
        assert!(bytes.is_empty());
        assert!(!path.join(native_import::PENDING).exists());
        Ok(())
    }
    #[test]
    fn writer_rejects_a_canonical_but_corrupted_head_token() -> Result<()> {
        let (_work, path, frame) = setup()?;
        let mut writer = Writer::open(&path)?;
        writer.import(&frame)?;
        drop(writer);
        let path_head = path.join(HEAD);
        let before = std::fs::read_to_string(&path_head)?;
        let changed = before.replacen("\"ReadEpoch\":2", "\"ReadEpoch\":3", 1);
        assert_ne!(before, changed);
        std::fs::write(&path_head, changed.as_bytes())?;
        assert!(
            Writer::open(&path).is_err(),
            "corrupted head token must fail closed even if root/height still match"
        );
        assert_eq!(std::fs::read_to_string(path_head)?, changed);
        Ok(())
    }
    #[test]
    fn writer_read_guard_excludes_native_maintenance_until_release() -> Result<()> {
        let (_work, path, _frame) = setup()?;
        let read = read_guard(&path)?;
        let maintenance = lock_file(&path, ".execution-retention-lock")?;
        assert!(
            rustix::fs::flock(&maintenance, FlockOperation::NonBlockingLockExclusive).is_err(),
            "a read guard must retain the existing maintenance run lease"
        );
        drop(read);
        rustix::fs::flock(&maintenance, FlockOperation::NonBlockingLockExclusive)?;
        Ok(())
    }
    #[test]
    fn writer_head_must_match_its_immutable_completed_receipt() -> Result<()> {
        let (_work, path, frame) = setup()?;
        let mut writer = Writer::open(&path)?;
        writer.import(&frame)?;
        drop(writer);
        let mut head: Head = artifact(&path.join(HEAD))?;
        head.read_epoch += 1;
        std::fs::write(path.join(HEAD), sealed_bytes(&head)?)?;
        assert!(
            Writer::open(&path).is_err(),
            "a valid checksum cannot substitute for matching completion evidence"
        );
        Ok(())
    }
    #[test]
    fn writer_transaction_code_storage_receipts_and_second_replay_match() -> Result<()> {
        use reth_storage_api::ChangeSetReader;
        let (_source, f, spec) = fixture(true)?;
        let work = tempfile::tempdir()?;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let state = work.path().join("state.ndjson");
        native::export_checkpoint(&f, &spec, 0, None, &state, work.path())?;
        let (m, _) = checkpoint::seed(&state, "5042002", spec.inner.genesis_hash())?;
        let expected = work.path().join("expected.json");
        checkpoint::write_private(&expected, |w| {
            serde_json::to_writer(w, &m)?;
            Ok(())
        })?;
        let path = work.path().join("native");
        let absent = work.path().join("unused");
        bootstrap::build_fixture(
            &bootstrap::Inputs {
                target: &path,
                state: &state,
                expected: &expected,
                headers: &absent,
                block: &absent,
                receipts: &absent,
            },
            spec.clone(),
        )?;
        let one = first(&f, spec.clone())?;
        let block = one.recovered_block().clone().into_block();
        let receipts = one.execution_output.result.receipts.clone();
        assert_eq!(receipts.len(), 1);
        let meta = Meta {
            version: 1,
            request_id: 1,
            chain_id: 5042002,
            genesis: spec.inner.genesis_hash(),
            arc_revision: crate::ARC_REVISION.into(),
            reth_revision: RETH_REVISION.into(),
            import_epoch: 1,
            height: 1,
            hash: block.header.hash_slow(),
            parent: m.r#ref.hash,
            parent_root: m.root,
        };
        let frame = Frame::fixture(meta, alloy_rlp::encode(&block), encode_receipts(&receipts))?;
        assert!(
            Frame::read(encoded(&frame)?.as_slice()).is_err(),
            "production decoder cannot accept a synthetic genesis"
        );
        assert!(
            Writer::open(&path).is_err(),
            "production writer cannot accept a synthetic native DB"
        );
        let mut writer = Writer::open_fixture(&path, spec.clone())?;
        let result = writer.import(&frame)?;
        assert_eq!(writer.executions(), 1);
        drop(writer);
        let (g, _scratch) = native::open_readonly(&path, spec.clone(), true)?;
        let p = g.provider()?;
        assert!(!p.account_block_changeset(1)?.is_empty());
        let parent = p.header_by_number(0)?.unwrap();
        let state = p.history_by_block_hash(parent.hash_slow())?;
        use alloy_primitives::Sealable;
        let config = ArcEvmConfig::new(EthEvmConfig::new_with_evm_factory(
            spec.clone(),
            ArcEvmFactory::new(spec),
        ));
        let verified = crate::replay(&config, &state, &parent.seal_slow(), &block, &receipts, 1)?;
        assert_eq!(verified.result().state_root, result.root);
        assert_eq!(verified.result().block_rlp, frame.block_rlp());
        assert_eq!(verified.result().receipts_rlp, frame.receipts_rlp());
        assert!(!verified.result().steps.is_empty());
        Ok(())
    }
    #[test]
    fn writer_rejects_wrong_epoch_parent_sequence_payload_and_evm_roots_before_publication()
    -> Result<()> {
        let (_work, path, frame) = setup()?;
        let mut writer = Writer::open(&path)?;
        let mut expected_executions = 0;
        for case in 0..10 {
            let mut meta = frame.meta().clone();
            let mut block: Block = alloy_rlp::decode_exact(frame.block_rlp())?;
            let mut receipts = frame.receipts_rlp().to_vec();
            match case {
                0 => meta.import_epoch += 1,
                1 => meta.request_id += 1,
                2 => meta.height += 1,
                3 => meta.parent = B256::repeat_byte(3),
                4 => meta.parent_root = B256::repeat_byte(4),
                5 => {
                    block.header.state_root = B256::repeat_byte(5);
                    meta.hash = block.header.hash_slow();
                    expected_executions += 1;
                }
                6 => {
                    block.header.receipts_root = B256::repeat_byte(6);
                    meta.hash = block.header.hash_slow();
                }
                7 => receipts = vec![0xc1, 0xc0],
                8 => {
                    block.header.gas_used = 1;
                    meta.hash = block.header.hash_slow();
                }
                _ => {
                    block.header.requests_hash = Some(B256::repeat_byte(9));
                    meta.hash = block.header.hash_slow();
                    expected_executions += 1;
                }
            }
            let bad = Frame::fixture(meta, alloy_rlp::encode(block), receipts)?;
            assert!(writer.import(&bad).is_err(), "case {case}");
            assert_eq!(writer.executions(), expected_executions, "case {case}");
            assert_eq!(writer.head().tip.height, 0);
            assert_eq!(writer.f.provider()?.best_block_number()?, 0);
            assert!(!path.join(native_import::PENDING).exists());
            assert!(!receipt_path(&path, 1, 1).exists());
        }
        let r = writer.import(&frame)?;
        expected_executions += 1;
        let wrong = Frame::fixture(
            frame.meta().clone(),
            frame.block_rlp().to_vec(),
            vec![0xc1, 0xc0],
        )?;
        assert!(
            writer.import(&wrong).is_err(),
            "duplicate must bind original bytes"
        );
        assert_eq!(writer.import(&frame)?, r);
        assert_eq!(writer.executions(), expected_executions);
        Ok(())
    }
    #[test]
    fn writer_truncated_or_overlarge_input_never_gets_a_committed_response() -> Result<()> {
        for oversized in [false, true] {
            let (_work, path, frame) = setup()?;
            let mut input = encoded(&frame)?;
            if oversized {
                input[4..8].copy_from_slice(&u32::MAX.to_be_bytes());
            } else {
                input.pop();
            }
            let mut output = Vec::new();
            assert!(serve(input.as_slice(), &mut output, &path).is_err());
            assert!(!output.windows(4).any(|v| v == b"AWR1"));
            let writer = Writer::open(&path)?;
            assert_eq!(writer.head.tip.height, 0);
            assert_eq!(writer.executions(), 0);
            assert!(!path.join(native_import::PENDING).exists());
        }
        Ok(())
    }
    #[test]
    fn writer_output_failure_after_commit_retries_exact_receipt_without_execution() -> Result<()> {
        struct Fail {
            left: usize,
        }
        impl Write for Fail {
            fn write(&mut self, b: &[u8]) -> std::io::Result<usize> {
                if self.left == 0 {
                    return Err(std::io::Error::other("owned output failure"));
                }
                let n = b.len().min(self.left);
                self.left -= n;
                Ok(n)
            }
            fn flush(&mut self) -> std::io::Result<()> {
                Ok(())
            }
        }
        let (_work, path, frame) = setup()?;
        let writer = Writer::open(&path)?;
        let hello = Ready {
            version: 1,
            identity: writer.identity.clone(),
            head: writer.head.clone(),
        };
        let mut fail = Fail {
            left: 16 + serde_json::to_vec(&hello)?.len() + 17,
        };
        drop(writer);
        assert!(serve(encoded(&frame)?.as_slice(), &mut fail, &path).is_err());
        assert!(!path.join(native_import::PENDING).exists());
        let mut writer = Writer::open(&path)?;
        assert_eq!(writer.head.tip.height, 1);
        assert_eq!(writer.import(&frame)?.input_digest, frame.digest());
        assert_eq!(writer.executions(), 0);
        Ok(())
    }
    #[test]
    fn writer_unknown_pending_objects_and_unmanaged_inputs_are_preserved() -> Result<()> {
        for mode in 0..3 {
            let (_work, path, _frame) = setup()?;
            let pending = path.join(native_import::PENDING);
            let sentinel = path.parent().unwrap().join("sentinel");
            std::fs::write(&sentinel, b"preserve")?;
            match mode {
                0 => std::fs::write(&pending, b"corrupt partial intent")?,
                1 => std::os::unix::fs::symlink(&sentinel, &pending)?,
                _ => std::fs::create_dir(&pending)?,
            }
            assert!(Writer::open(&path).is_err());
            assert!(read_guard(&path).is_err());
            assert!(std::fs::symlink_metadata(&pending).is_ok());
            assert!(!path.join(IDENTITY).exists());
            assert_eq!(std::fs::read(sentinel)?, b"preserve");
        }
        let (work, path, _) = setup()?;
        let missing = work.path().join("unmanaged");
        std::fs::create_dir(&missing)?;
        std::fs::set_permissions(&missing, std::fs::Permissions::from_mode(0o700))?;
        assert!(Writer::open(&missing).is_err());
        assert!(!missing.join("db").exists());
        let writer = Writer::open(&path)?;
        assert!(
            native::check_start(&path).is_err(),
            "managed native must not downgrade to legacy replay"
        );
        drop(writer);
        assert!(native::check_import_fences(&path).is_ok());
        Ok(())
    }
    #[test]
    fn writer_managed_database_cannot_enter_legacy_maintenance_without_its_owner() -> Result<()> {
        let (_work, path, _frame) = setup()?;
        let writer = Writer::open(&path)?;
        assert!(
            native::check_legacy_maintenance(&path).is_err(),
            "all legacy native commands must reject managed DBs"
        );
        drop(writer);
        assert!(
            native::check_legacy_maintenance(&path).is_err(),
            "stopping a writer is not a maintenance permit"
        );
        Ok(())
    }
}

#[cfg(test)]
#[path = "native_writer_processes.rs"]
mod processes;
