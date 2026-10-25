//! Private, bounded external sort for candidate keys. Keys never prove coverage.
use alloy_primitives::{Address, B256, keccak256};
use eyre::{Result, ensure};
use sha2::{Digest, Sha256};
use std::{
    cmp::Reverse,
    collections::BinaryHeap,
    fs::{File, OpenOptions},
    io::{BufReader, BufWriter, Read, Write},
    os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt},
    path::{Path, PathBuf},
};

const KEY_BYTES: usize = 117;
pub const MAX_INPUTS: u64 = 64_000_000;
pub const MAX_DISK_BYTES: u64 = 16 << 30;
pub const CHUNK_KEYS: usize = 32_768;
pub const FAN_IN: usize = 16;

// Hash/raw-address/tag/hash/raw-slot; account markers precede their slots.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub struct Key([u8; KEY_BYTES]);
impl Key {
    pub fn account(address: Address) -> Self {
        let mut b = [0; KEY_BYTES];
        b[..32].copy_from_slice(keccak256(address).as_slice());
        b[32..52].copy_from_slice(address.as_slice());
        Self(b)
    }
    pub fn storage(address: Address, slot: B256) -> Self {
        let mut k = Self::account(address);
        k.0[52] = 1;
        k.0[53..85].copy_from_slice(keccak256(slot).as_slice());
        k.0[85..].copy_from_slice(slot.as_slice());
        k
    }
    pub fn address(self) -> Address {
        Address::from_slice(&self.0[32..52])
    }
    pub fn slot(self) -> Option<B256> {
        (self.0[52] == 1).then(|| B256::from_slice(&self.0[85..]))
    }
    fn validate(self) -> Result<()> {
        let expected = match self.0[52] {
            0 => Self::account(self.address()),
            1 => Self::storage(self.address(), B256::from_slice(&self.0[85..])),
            _ => eyre::bail!("invalid catalog tag"),
        };
        ensure!(self == expected, "catalog key commitment mismatch");
        Ok(())
    }
}

#[derive(Clone, Copy)]
pub(crate) struct Limits {
    pub chunk: usize,
    pub fan_in: usize,
    pub inputs: u64,
    pub disk: u64,
}
impl Default for Limits {
    fn default() -> Self {
        Self {
            chunk: CHUNK_KEYS,
            fan_in: FAN_IN,
            inputs: MAX_INPUTS,
            disk: MAX_DISK_BYTES,
        }
    }
}
#[derive(Default)]
struct Stats {
    buffer_peak: usize,
    readers_peak: usize,
    passes: usize,
}

pub fn private_dir(path: &Path) -> Result<()> {
    let m = std::fs::symlink_metadata(path)?;
    ensure!(
        path.is_absolute()
            && m.is_dir()
            && m.mode() & 0o777 == 0o700
            && m.uid() == rustix::process::geteuid().as_raw(),
        "owned private catalog parent required"
    );
    Ok(())
}
struct Workspace {
    path: PathBuf,
    device: u64,
    inode: u64,
}
impl Workspace {
    fn new(parent: &Path) -> Result<Self> {
        private_dir(parent)?;
        let path = tempfile::Builder::new()
            .prefix("arc-catalog-")
            .permissions(std::fs::Permissions::from_mode(0o700))
            .tempdir_in(parent)?
            .keep();
        let m = std::fs::symlink_metadata(&path)?;
        Ok(Self {
            path,
            device: m.dev(),
            inode: m.ino(),
        })
    }
    fn check(&self) -> Result<()> {
        private_dir(&self.path)?;
        let m = std::fs::symlink_metadata(&self.path)?;
        ensure!(
            m.dev() == self.device && m.ino() == self.inode,
            "catalog workspace replaced"
        );
        Ok(())
    }
}
impl Drop for Workspace {
    fn drop(&mut self) {
        if self.check().is_ok() {
            let _ = std::fs::remove_dir_all(&self.path);
        }
    }
}
#[derive(Clone)]
struct Run {
    path: PathBuf,
    count: u64,
    digest: B256,
    device: u64,
    inode: u64,
}
impl Run {
    fn bytes(&self) -> u64 {
        self.count * KEY_BYTES as u64
    }
}

pub struct Builder {
    work: Workspace,
    limits: Limits,
    buffer: Vec<Key>,
    runs: Vec<Run>,
    inputs: u64,
    disk: u64,
    sequence: u64,
    poisoned: bool,
    stats: Stats,
}
impl Builder {
    pub fn new(parent: &Path) -> Result<Self> {
        Self::with_limits(parent, Limits::default())
    }
    pub(crate) fn with_limits(parent: &Path, limits: Limits) -> Result<Self> {
        ensure!(
            limits.chunk > 0
                && limits.chunk <= CHUNK_KEYS
                && limits.fan_in >= 2
                && limits.fan_in <= FAN_IN
                && limits.inputs <= MAX_INPUTS
                && limits.disk <= MAX_DISK_BYTES,
            "catalog limits"
        );
        Ok(Self {
            work: Workspace::new(parent)?,
            limits,
            buffer: Vec::with_capacity(limits.chunk),
            runs: Vec::new(),
            inputs: 0,
            disk: 0,
            sequence: 0,
            poisoned: false,
            stats: Stats::default(),
        })
    }
    pub(crate) fn poison(&mut self) {
        self.poisoned = true;
    }
    pub fn push(&mut self, key: Key) -> Result<()> {
        let result = self.push_inner(key);
        if result.is_err() {
            self.poison();
        }
        result
    }
    fn push_inner(&mut self, key: Key) -> Result<()> {
        ensure!(
            !self.poisoned && self.inputs < self.limits.inputs,
            "catalog input limit/poisoned"
        );
        self.inputs += 1;
        self.buffer.push(key);
        self.stats.buffer_peak = self.stats.buffer_peak.max(self.buffer.len());
        if self.buffer.len() == self.limits.chunk {
            self.flush()?;
        }
        Ok(())
    }
    fn write_run(&mut self, mut next: impl FnMut() -> Result<Option<Key>>) -> Result<Run> {
        self.work.check()?;
        let path = self.work.path.join(format!("run-{}", self.sequence));
        self.sequence += 1;
        let file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&path)?;
        let m = file.metadata()?;
        let mut writer = BufWriter::new(file);
        let mut digest = Sha256::new();
        let mut count = 0;
        let mut previous = None;
        while let Some(key) = next()? {
            if previous == Some(key) {
                continue;
            }
            ensure!(previous.is_none_or(|p| p < key), "catalog merge order");
            ensure!(
                (count + 1) * KEY_BYTES as u64 <= self.limits.disk - self.disk,
                "catalog scratch byte limit"
            );
            writer.write_all(&key.0)?;
            digest.update(key.0);
            previous = Some(key);
            count += 1;
        }
        writer.flush()?;
        let run = Run {
            path,
            count,
            digest: B256::from_slice(&digest.finalize()),
            device: m.dev(),
            inode: m.ino(),
        };
        self.disk += run.bytes();
        Ok(run)
    }
    fn flush(&mut self) -> Result<()> {
        let mut buffer = std::mem::take(&mut self.buffer);
        buffer.sort_unstable();
        buffer.dedup();
        let mut input = buffer.iter().copied();
        let run = self.write_run(|| Ok(input.next()))?;
        buffer.clear();
        self.buffer = buffer;
        self.runs.push(run);
        Ok(())
    }
    fn merge(&mut self, input: &[Run]) -> Result<Run> {
        self.stats.readers_peak = self.stats.readers_peak.max(input.len());
        let mut readers = input.iter().map(Reader::open).collect::<Result<Vec<_>>>()?;
        let mut heap = BinaryHeap::new();
        for (n, reader) in readers.iter_mut().enumerate() {
            if let Some(key) = reader.next_key()? {
                heap.push(Reverse((key, n)));
            }
        }
        let out = self.write_run(|| {
            let Some(Reverse((key, n))) = heap.pop() else {
                return Ok(None);
            };
            if let Some(next) = readers[n].next_key()? {
                heap.push(Reverse((next, n)));
            }
            Ok(Some(key))
        })?;
        drop(readers);
        for run in input {
            let m = std::fs::symlink_metadata(&run.path)?;
            ensure!(
                m.is_file() && m.dev() == run.device && m.ino() == run.inode,
                "catalog input replaced"
            );
            std::fs::remove_file(&run.path)?;
            self.disk -= run.bytes();
        }
        Ok(out)
    }
    pub fn finish(mut self) -> Result<Catalog> {
        ensure!(!self.poisoned, "catalog poisoned");
        self.work.check()?;
        if !self.buffer.is_empty() || self.runs.is_empty() {
            self.flush()?;
        }
        while self.runs.len() > 1 {
            self.stats.passes += 1;
            let old = std::mem::take(&mut self.runs);
            for batch in old.chunks(self.limits.fan_in) {
                let run = if batch.len() == 1 {
                    batch[0].clone()
                } else {
                    self.merge(batch)?
                };
                self.runs.push(run);
            }
        }
        let run = self.runs.pop().expect("one completed run");
        Ok(Catalog {
            work: self.work,
            run,
            spare: self.limits.disk - self.disk,
            _stats: self.stats,
        })
    }
}
pub struct Catalog {
    work: Workspace,
    run: Run,
    spare: u64,
    _stats: Stats,
}
impl Catalog {
    pub fn reader(&self) -> Result<Reader> {
        self.work.check()?;
        Reader::open(&self.run)
    }
    pub(crate) fn workspace(&self) -> Result<&Path> {
        self.work.check()?;
        Ok(&self.work.path)
    }
    pub(crate) fn spare_bytes(&self) -> u64 {
        self.spare
    }
}
pub struct Reader {
    file: BufReader<File>,
    run: Run,
    read: u64,
    digest: Sha256,
    previous: Option<Key>,
    ended: bool,
}
impl Reader {
    fn open(run: &Run) -> Result<Self> {
        let before = std::fs::symlink_metadata(&run.path)?;
        ensure!(
            before.is_file() && before.dev() == run.device && before.ino() == run.inode,
            "catalog path replaced"
        );
        let file: File = rustix::fs::open(
            &run.path,
            rustix::fs::OFlags::RDONLY
                | rustix::fs::OFlags::CLOEXEC
                | rustix::fs::OFlags::NOFOLLOW
                | rustix::fs::OFlags::NONBLOCK,
            rustix::fs::Mode::empty(),
        )?
        .into();
        let m = file.metadata()?;
        ensure!(
            before.is_file()
                && m.is_file()
                && m.dev() == run.device
                && m.ino() == run.inode
                && before.dev() == m.dev()
                && before.ino() == m.ino()
                && m.nlink() == 1
                && m.uid() == rustix::process::geteuid().as_raw()
                && m.mode() & 0o777 == 0o600
                && m.len() == run.bytes(),
            "catalog file identity/length"
        );
        Ok(Self {
            file: BufReader::new(file),
            run: run.clone(),
            read: 0,
            digest: Sha256::new(),
            previous: None,
            ended: false,
        })
    }
    pub fn next_key(&mut self) -> Result<Option<Key>> {
        if self.ended {
            return Ok(None);
        }
        if self.read == self.run.count {
            let mut tail = [0];
            ensure!(
                self.file.read(&mut tail)? == 0
                    && B256::from_slice(&self.digest.clone().finalize()) == self.run.digest,
                "catalog EOF/digest mismatch"
            );
            self.ended = true;
            return Ok(None);
        }
        let mut b = [0; KEY_BYTES];
        self.file.read_exact(&mut b)?;
        let key = Key(b);
        key.validate()?;
        ensure!(self.previous.is_none_or(|p| p < key), "catalog run order");
        self.previous = Some(key);
        self.read += 1;
        self.digest.update(b);
        Ok(Some(key))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn private_root() -> Result<tempfile::TempDir> {
        Ok(tempfile::Builder::new()
            .permissions(std::fs::Permissions::from_mode(0o700))
            .tempdir()?)
    }
    fn limits() -> Limits {
        Limits {
            chunk: 3,
            fan_in: 2,
            inputs: 100,
            disk: 1 << 20,
        }
    }
    fn key(n: u64) -> Key {
        let mut a = [0; 20];
        a[12..].copy_from_slice(&n.to_be_bytes());
        Key::account(Address::from(a))
    }
    fn all(c: &Catalog) -> Result<Vec<Key>> {
        let mut r = c.reader()?;
        let mut out = Vec::new();
        while let Some(k) = r.next_key()? {
            out.push(k);
        }
        Ok(out)
    }
    #[test]
    fn multipass_deduplicates_and_bounds_resources() -> Result<()> {
        let root = private_root()?;
        let mut b = Builder::with_limits(root.path(), limits())?;
        for n in (0..40).rev() {
            b.push(key(n % 23))?;
        }
        let c = b.finish()?;
        let mut expected = (0..23).map(key).collect::<Vec<_>>();
        expected.sort();
        assert_eq!(all(&c)?, expected);
        assert!(c._stats.passes >= 2 && c._stats.buffer_peak <= 3 && c._stats.readers_peak <= 2);
        let path = c.work.path.clone();
        drop(c);
        assert!(!path.exists());
        Ok(())
    }
    #[test]
    fn limits_poison_and_changed_workspaces_are_preserved() -> Result<()> {
        let root = private_root()?;
        for l in [
            Limits {
                inputs: 1,
                ..limits()
            },
            Limits {
                chunk: 2,
                disk: 117,
                ..limits()
            },
        ] {
            let mut b = Builder::with_limits(root.path(), l)?;
            let path = b.work.path.clone();
            b.push(key(1))?;
            assert!(b.push(key(2)).is_err());
            assert!(b.finish().is_err());
            assert!(!path.exists());
        }
        let b = Builder::new(root.path())?;
        let path = b.work.path.clone();
        std::fs::rename(&path, root.path().join("original"))?;
        std::fs::create_dir(&path)?;
        std::fs::write(path.join("foreign"), b"keep")?;
        assert!(b.finish().is_err());
        assert!(path.join("foreign").exists());
        Ok(())
    }
    #[test]
    fn corrupt_truncated_reordered_and_extra_runs_fail_closed() -> Result<()> {
        let root = private_root()?;
        for damage in 0..4 {
            let mut b = Builder::new(root.path())?;
            for n in 0..4 {
                b.push(key(n))?;
            }
            let c = b.finish()?;
            let mut bytes = std::fs::read(&c.run.path)?;
            match damage {
                0 => {
                    bytes.pop();
                }
                1 => {
                    bytes[0] ^= 1;
                }
                2 => {
                    let first = bytes[..KEY_BYTES].to_vec();
                    bytes.copy_within(KEY_BYTES..2 * KEY_BYTES, 0);
                    bytes[KEY_BYTES..2 * KEY_BYTES].copy_from_slice(&first);
                }
                _ => {
                    bytes.extend_from_slice(&key(99).0);
                }
            }
            std::fs::write(&c.run.path, bytes)?;
            assert!(all(&c).is_err());
        }
        Ok(())
    }
    #[test]
    fn same_length_valid_key_change_is_rejected_by_digest() -> Result<()> {
        let root = private_root()?;
        let mut b = Builder::new(root.path())?;
        for n in 0..4 {
            b.push(key(n))?;
        }
        let c = b.finish()?;
        let original = all(&c)?;
        let last = *original.last().unwrap();
        let replacement = (4..1000).map(key).find(|k| *k > last).unwrap();
        let mut bytes = std::fs::read(&c.run.path)?;
        bytes[3 * KEY_BYTES..].copy_from_slice(&replacement.0);
        std::fs::write(&c.run.path, bytes)?;
        let mut reader = c.reader()?;
        for _ in 0..4 {
            assert!(reader.next_key()?.is_some());
        }
        assert!(reader.next_key().is_err());
        Ok(())
    }
    #[test]
    fn nonprivate_and_linked_parents_are_rejected() -> Result<()> {
        let root = private_root()?;
        let public = root.path().join("public");
        std::fs::create_dir(&public)?;
        std::fs::set_permissions(&public, std::fs::Permissions::from_mode(0o755))?;
        assert!(Builder::new(&public).is_err());
        let link = root.path().join("link");
        std::os::unix::fs::symlink(root.path(), &link)?;
        assert!(Builder::new(&link).is_err());
        Ok(())
    }
}
