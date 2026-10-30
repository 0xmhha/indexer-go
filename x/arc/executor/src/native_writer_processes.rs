//! Owned-process SDK crash and reader-publication qualification, never a node.
use super::*;
use std::{
    collections::HashMap,
    io::{BufRead, BufReader},
    os::fd::AsRawFd,
    os::unix::process::ExitStatusExt,
    process::{Child, ChildStdin, Command, Stdio},
    sync::{
        Mutex,
        atomic::{AtomicBool, AtomicU64, Ordering},
        mpsc,
    },
    thread,
    time::{Duration, Instant},
};
use tracing::{
    Subscriber,
    field::{Field, Visit},
    span::{Attributes, Id, Record},
};

const ROLE: &str = "ARC_NATIVE_WRITER_TEST_ROLE";
const NATIVE: &str = "ARC_NATIVE_WRITER_TEST_NATIVE";
const PAUSE: &str = "ARC_NATIVE_WRITER_TEST_PAUSE";
static ARMED: AtomicBool = AtomicBool::new(false);
static VM_ARMED: AtomicBool = AtomicBool::new(false);
pub(super) fn vm_read_hook() {
    if VM_ARMED.load(Ordering::SeqCst) {
        pause("vm-basic");
    }
}
fn pause(tag: &str) -> ! {
    println!("\nPAUSED:{tag}");
    std::io::stdout().flush().unwrap();
    // Parent kills only its own typed Child handle. No destructors/atexit and
    // no chance to proceed to the next store before that SIGKILL.
    loop {
        thread::park();
    }
}
struct Trap {
    mode: String,
    next: AtomicU64,
    spans: Mutex<HashMap<u64, (usize, bool)>>,
}
impl Subscriber for Trap {
    fn enabled(&self, _: &tracing::Metadata<'_>) -> bool {
        true
    }
    fn new_span(&self, a: &Attributes<'_>) -> Id {
        struct Batch(bool);
        impl Visit for Batch {
            fn record_u64(&mut self, f: &Field, v: u64) {
                if f.name() == "batch_len" && v > 0 {
                    self.0 = true;
                }
            }
            fn record_debug(&mut self, _: &Field, _: &dyn std::fmt::Debug) {}
        }
        let mut batch = Batch(false);
        a.record(&mut batch);
        let tracked = a.metadata().target() == "providers::rocksdb"
            && a.metadata().name() == "commit_batch"
            && batch.0;
        if tracked && ARMED.load(Ordering::SeqCst) && self.mode == "rocks-before" {
            pause("rocks-before");
        }
        let n = self.next.fetch_add(1, Ordering::Relaxed);
        self.spans.lock().unwrap().insert(n, (1, tracked));
        Id::from_u64(n)
    }
    fn record(&self, _: &Id, _: &Record<'_>) {}
    fn record_follows_from(&self, _: &Id, _: &Id) {}
    fn event(&self, _: &tracing::Event<'_>) {}
    fn enter(&self, _: &Id) {}
    fn exit(&self, _: &Id) {}
    fn clone_span(&self, id: &Id) -> Id {
        self.spans
            .lock()
            .unwrap()
            .get_mut(&id.into_u64())
            .unwrap()
            .0 += 1;
        id.clone()
    }
    fn try_close(&self, id: Id) -> bool {
        let mut spans = self.spans.lock().unwrap();
        let entry = spans.get_mut(&id.into_u64()).unwrap();
        entry.0 -= 1;
        if entry.0 > 0 {
            return false;
        }
        let tracked = spans.remove(&id.into_u64()).unwrap().1;
        drop(spans);
        if tracked && ARMED.load(Ordering::SeqCst) && self.mode == "rocks-after" {
            pause("rocks-after");
        }
        true
    }
    fn max_level_hint(&self) -> Option<tracing::metadata::LevelFilter> {
        Some(tracing::metadata::LevelFilter::DEBUG)
    }
}

struct Process {
    child: Child,
    input: Option<ChildStdin>,
    lines: Option<mpsc::Receiver<String>>,
    output: Option<thread::JoinHandle<()>>,
    errors: Option<thread::JoinHandle<()>>,
    diagnostics: Arc<Mutex<Vec<u8>>>,
}
fn remember(diagnostics: &Mutex<Vec<u8>>, bytes: &[u8]) {
    let mut saved = diagnostics.lock().unwrap();
    let remaining = (16 << 10_usize) - saved.len();
    saved.extend_from_slice(&bytes[..remaining.min(bytes.len())]);
}
impl Process {
    fn start(path: &Path, role: &str, point: &str) -> Result<Self> {
        Self::inherited(path, role, point, None)
    }
    fn inherited(path: &Path, role: &str, point: &str, view: Option<&File>) -> Result<Self> {
        let flags = view.map(rustix::io::fcntl_getfd).transpose()?;
        if let (Some(view), Some(flags)) = (view, flags) {
            rustix::io::fcntl_setfd(view, flags & !rustix::io::FdFlags::CLOEXEC)?;
        }
        let spawned = Command::new(std::env::current_exe()?)
            .args([
                "--exact",
                "native_writer::processes::writer_process_child",
                "--nocapture",
                "--test-threads=1",
            ])
            .env_clear()
            .env(ROLE, role)
            .env(NATIVE, path)
            .env(PAUSE, point)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn();
        let restored = if let (Some(view), Some(flags)) = (view, flags) {
            rustix::io::fcntl_setfd(view, flags)
        } else {
            Ok(())
        };
        let mut child = spawned?;
        if let Err(error) = restored {
            let _ = child.kill();
            let _ = child.wait();
            return Err(error.into());
        }
        let input = child.stdin.take();
        let stdout = child.stdout.take().ok_or_else(|| eyre!("owned stdout"))?;
        let stderr = child.stderr.take().ok_or_else(|| eyre!("owned stderr"))?;
        let (tx, rx) = mpsc::sync_channel(16);
        let diagnostics = Arc::new(Mutex::new(Vec::new()));
        let captured_output = diagnostics.clone();
        let output = thread::spawn(move || {
            let mut reader = BufReader::new(stdout);
            loop {
                let mut line = String::new();
                match (&mut reader).take(4097).read_line(&mut line) {
                    Ok(0) | Err(_) => break,
                    Ok(_) => {
                        remember(&captured_output, line.as_bytes());
                        if line.len() > 4096 {
                            break;
                        }
                        if tx.send(line).is_err() {
                            break;
                        }
                    }
                }
            }
        });
        let captured_error = diagnostics.clone();
        let errors = thread::spawn(move || {
            let mut r = stderr;
            let mut buf = [0u8; 1024];
            while let Ok(n) = r.read(&mut buf) {
                if n == 0 {
                    break;
                }
                remember(&captured_error, &buf[..n]);
            }
        });
        Ok(Self {
            child,
            input,
            lines: Some(rx),
            output: Some(output),
            errors: Some(errors),
            diagnostics,
        })
    }
    fn expect(&mut self, tag: &str) -> Result<()> {
        let end = Instant::now() + Duration::from_secs(30);
        loop {
            let line = self
                .lines
                .as_ref()
                .unwrap()
                .recv_timeout(end.saturating_duration_since(Instant::now()));
            let line = match line {
                Ok(line) => line,
                Err(error) => {
                    // Drop the receiver before joining its bounded sender;
                    // killing a child alone cannot unblock a full channel.
                    drop(self.lines.take());
                    let _ = self.child.kill();
                    let status = self.child.wait();
                    for handle in [self.output.take(), self.errors.take()]
                        .into_iter()
                        .flatten()
                    {
                        let _ = handle.join();
                    }
                    return Err(eyre!(
                        "owned child expected {tag}: {error}; status {status:?}; diagnostics: {}",
                        String::from_utf8_lossy(&self.diagnostics.lock().unwrap())
                    ));
                }
            };
            if line.trim().ends_with(tag) {
                return Ok(());
            }
        }
    }
    fn frame(&mut self, frame: &Frame) -> Result<()> {
        self.input
            .as_mut()
            .unwrap()
            .write_all(&super::tests::encoded(frame)?)?;
        self.input.as_mut().unwrap().flush()?;
        Ok(())
    }
    fn line(&mut self, line: &str) -> Result<()> {
        writeln!(self.input.as_mut().unwrap(), "{line}")?;
        self.input.as_mut().unwrap().flush()?;
        Ok(())
    }
    fn kill(&mut self) -> Result<()> {
        self.child.kill()?;
        let status = self.child.wait()?;
        ensure!(
            status.signal() == Some(9),
            "owned child not killed at boundary"
        );
        Ok(())
    }
}

fn parent_shared(native: &Path, view: &File) -> Result<()> {
    let turn = lock_file(native, ".arc-native-import-turn")?;
    rustix::fs::flock(&turn, FlockOperation::LockExclusive)?;
    rustix::fs::flock(view, FlockOperation::LockShared)?;
    Ok(())
}
fn publication_wait(native: &Path) -> Result<()> {
    let turn = lock_file(native, ".arc-native-import-turn")?;
    let deadline = Instant::now() + Duration::from_secs(30);
    loop {
        match rustix::fs::flock(&turn, FlockOperation::NonBlockingLockExclusive) {
            Err(rustix::io::Errno::WOULDBLOCK) => return Ok(()),
            Err(e) => return Err(e.into()),
            Ok(()) => rustix::fs::flock(&turn, FlockOperation::Unlock)?,
        }
        ensure!(
            Instant::now() < deadline,
            "writer never reached publication wait"
        );
        thread::yield_now();
    }
}

#[test]
fn managed_inherited_process_refreshes_same_native_factory_and_replays_after_parent_unlock()
-> Result<()> {
    let (_work, path, frame) = super::tests::setup()?;
    let mut writer = Process::start(&path, "writer", "none")?;
    writer.expect("WRITER_READY")?;
    let view = lock_file(&path, ".arc-native-import-view")?;
    parent_shared(&path, &view)?;
    let mut reader = Process::inherited(
        &path,
        "managed-reader",
        &view.as_raw_fd().to_string(),
        Some(&view),
    )?;
    reader.expect("MANAGED_READY")?;
    reader.line("HEAD")?;
    reader.expect("MANAGED:0:1")?;
    writer.frame(&frame)?;
    writer.expect("EXECUTED")?;
    publication_wait(&path)?;
    rustix::fs::flock(&view, FlockOperation::Unlock)?;
    writer.expect("COMMITTED")?;
    parent_shared(&path, &view)?;
    reader.line("HEAD")?;
    reader.expect("MANAGED:1:2")?;
    reader.line("REPLAY")?;
    reader.expect("MANAGED_REPLAY:7:1:2")?;
    rustix::fs::flock(&view, FlockOperation::Unlock)?;
    reader.line("QUIT")?;
    reader.expect("MANAGED_BYE")?;
    ensure!(reader.child.wait()?.success(), "managed RO child failed");
    ensure!(writer.child.wait()?.success(), "native writer child failed");
    Ok(())
}
#[test]
fn managed_inherited_process_cancel_joins_before_parent_shared_view_release() -> Result<()> {
    let (_work, path, frame) = super::tests::setup()?;
    let mut writer = Process::start(&path, "writer", "none")?;
    writer.expect("WRITER_READY")?;
    let view = lock_file(&path, ".arc-native-import-view")?;
    parent_shared(&path, &view)?;
    let mut reader = Process::inherited(
        &path,
        "managed-reader",
        &view.as_raw_fd().to_string(),
        Some(&view),
    )?;
    reader.expect("MANAGED_READY")?;
    writer.frame(&frame)?;
    writer.expect("EXECUTED")?;
    publication_wait(&path)?;
    reader.line("PAUSE")?;
    reader.expect("PAUSED:managed-read")?;
    reader.kill()?;
    assert!(
        writer.child.try_wait()?.is_none(),
        "reader exit must not unlock parent's same OFD"
    );
    rustix::fs::flock(&view, FlockOperation::Unlock)?;
    writer.expect("COMMITTED")?;
    ensure!(writer.child.wait()?.success(), "native writer child failed");
    let mut resumed = Writer::open(&path)?;
    resumed.import(&frame)?;
    assert_eq!(resumed.executions(), 0);
    Ok(())
}
impl Drop for Process {
    fn drop(&mut self) {
        drop(self.input.take());
        drop(self.lines.take());
        let _ = self.child.kill();
        let _ = self.child.wait();
        if let Some(t) = self.output.take() {
            let _ = t.join();
        }
        if let Some(t) = self.errors.take() {
            let _ = t.join();
        }
    }
}

#[test]
fn writer_process_child() -> Result<()> {
    let Ok(role) = std::env::var(ROLE) else {
        return Ok(());
    };
    let path = PathBuf::from(std::env::var(NATIVE)?);
    let point = std::env::var(PAUSE)?;
    if role == "managed-reader" {
        use crate::managed_read::{self, InheritedView};
        use alloy_primitives::Sealable;
        let view = InheritedView::inherited_fixture(&path, point.parse()?)?;
        let (f, _scratch) = native::open_readonly(&path, TESTNET.clone(), false)?;
        managed_read::snapshot(&view, &f, None)?;
        println!("\nMANAGED_READY");
        std::io::stdout().flush()?;
        for command in std::io::stdin().lock().lines() {
            match command?.as_str() {
                "HEAD" => {
                    let proof = managed_read::snapshot(&view, &f, None)?;
                    println!(
                        "\nMANAGED:{}:{}",
                        proof.head.tip.height, proof.head.read_epoch
                    );
                }
                "REPLAY" => {
                    let before = managed_read::snapshot(&view, &f, Some(1))?;
                    let provider = f.provider()?;
                    let block = provider
                        .block_by_number(1)?
                        .ok_or_else(|| eyre!("managed block"))?;
                    let parent = provider
                        .header_by_number(0)?
                        .ok_or_else(|| eyre!("managed parent"))?
                        .seal_slow();
                    let receipts = provider
                        .receipts_by_block(1.into())?
                        .ok_or_else(|| eyre!("managed receipts"))?;
                    let state = provider.history_by_block_hash(parent.hash())?;
                    let config = ArcEvmConfig::new(EthEvmConfig::new_with_evm_factory(
                        TESTNET.clone(),
                        ArcEvmFactory::new(TESTNET.clone()),
                    ));
                    let verified = crate::replay(&config, &state, &parent, &block, &receipts, 7)?;
                    ensure!(
                        verified.result().r#ref.generation == 7
                            && verified.result().r#ref.hash == before.r#ref.hash
                            && verified.result().state_root == before.r#ref.root,
                        "managed second-pass/root/generation"
                    );
                    let after = managed_read::snapshot(&view, &f, Some(1))?;
                    ensure!(
                        serde_json::to_vec(&before)? == serde_json::to_vec(&after)?,
                        "complete managed replay view changed"
                    );
                    println!("\nMANAGED_REPLAY:7:1:{}", after.head.read_epoch);
                }
                "PAUSE" => {
                    let _provider = f.provider()?;
                    managed_read::snapshot(&view, &f, None)?;
                    pause("managed-read");
                }
                "QUIT" => break,
                _ => eyre::bail!("invalid managed fixture command"),
            }
            std::io::stdout().flush()?;
        }
        println!("\nMANAGED_BYE");
        std::io::stdout().flush()?;
        return Ok(());
    }
    if role == "transition" {
        use crate::native_epoch::{self, Phase};
        if point.starts_with("rocks-") {
            tracing::subscriber::set_global_default(Trap {
                mode: point.clone(),
                next: AtomicU64::new(1),
                spans: Mutex::new(HashMap::new()),
            })
            .map_err(|_| eyre!("owned transition subscriber"))?;
        }
        let request =
            serde_json::from_slice(&private_bytes(&path.join("transition-request.json"))?)?;
        native_epoch::transition_with_hook(&path, &request, &mut |phase| {
            if phase == Phase::Intent {
                ARMED.store(true, Ordering::SeqCst);
            }
            let name = match phase {
                Phase::Intent => "intent",
                Phase::Unwound => "unwound",
                Phase::Committed => "committed",
                Phase::Receipt => "receipt",
                Phase::Head => "head",
                Phase::Cleared => "cleared",
            };
            if point == name {
                pause(name);
            }
            Ok(())
        })?;
        println!("\nTRANSITION_COMPLETED");
        std::io::stdout().flush()?;
        return Ok(());
    }
    if role == "writer" {
        if point.starts_with("rocks-") {
            tracing::subscriber::set_global_default(Trap {
                mode: point.clone(),
                next: AtomicU64::new(1),
                spans: Mutex::new(HashMap::new()),
            })
            .map_err(|_| eyre!("owned trace subscriber"))?;
        }
        let mut writer = Writer::open(&path)?;
        println!("\nWRITER_READY");
        std::io::stdout().flush()?;
        let mut input = std::io::stdin().lock();
        let frame = Frame::read(&mut input)?.ok_or_else(|| eyre!("owned frame absent"))?;
        writer.import_with_hook(&frame, &mut |phase| {
            if phase == Phase::Executed && point == "none" {
                println!("\nEXECUTED");
                std::io::stdout().flush()?;
            }
            if phase == Phase::BeforeExecution && point == "vm-basic" {
                VM_ARMED.store(true, Ordering::SeqCst);
            }
            if phase == Phase::Saved {
                ARMED.store(true, Ordering::SeqCst);
            }
            let name = match phase {
                Phase::BeforeExecution => "before-execution",
                Phase::Executed => "executed",
                Phase::Intent => "intent",
                Phase::Saved => "saved",
                Phase::Committed => "committed",
                Phase::Receipt => "receipt",
                Phase::Head => "head",
                Phase::Cleared => "cleared",
            };
            if point == name {
                pause(name);
            }
            Ok(())
        })?;
        assert_eq!(
            writer.executions(),
            1,
            "ordinary native save must not invoke a third EVM execution"
        );
        println!("\nCOMMITTED");
        std::io::stdout().flush()?;
        return Ok(());
    }
    ensure!(
        role == "reader-hold" || role == "reader-once",
        "invalid owned test role"
    );
    println!("\nATTEMPT");
    std::io::stdout().flush()?;
    let run = lock_file(&path, ".execution-retention-lock")?;
    rustix::fs::flock(&run, FlockOperation::NonBlockingLockShared)?;
    let view = read_guard(&path)?;
    let (f, _scratch) = native::open_readonly(&path, TESTNET.clone(), false)?;
    let verify = || -> Result<u64> {
        let head: Head = artifact(&path.join(HEAD))?;
        let id: Identity = artifact(&path.join(IDENTITY))?;
        check_native(&f, &head, &id)?;
        check_completion(&path, &f, &head, &id)?;
        Ok(head.tip.height)
    };
    if role == "reader-once" {
        println!("\nREAD:{}", verify()?);
        std::io::stdout().flush()?;
        return Ok(());
    }
    ensure!(verify()? == 0, "expected initial retained view");
    println!("\nHELD:0");
    std::io::stdout().flush()?;
    let mut input = std::io::stdin().lock();
    let mut line = String::new();
    input.read_line(&mut line)?;
    ensure!(line.trim() == "release", "expected release");
    drop(view);
    println!("\nRELEASED");
    std::io::stdout().flush()?;
    line.clear();
    input.read_line(&mut line)?;
    ensure!(line.trim() == "read", "expected read");
    let _view = read_guard(&path)?;
    println!("\nREAD:{}", verify()?);
    std::io::stdout().flush()?;
    Ok(())
}

#[test]
fn writer_actual_process_killpoints_keep_partial_store_fences_and_exact_lost_reply() -> Result<()> {
    for point in [
        "before-execution",
        "vm-basic",
        "executed",
        "intent",
        "saved",
        "rocks-before",
        "rocks-after",
        "committed",
        "receipt",
        "head",
        "cleared",
    ] {
        let (_work, path, frame) = super::tests::setup()?;
        let mut child = Process::start(&path, "writer", point)?;
        child.expect("WRITER_READY")?;
        child.frame(&frame)?;
        child.expect(&format!("PAUSED:{point}"))?;
        child.kill()?;
        drop(child);
        if matches!(
            point,
            "before-execution" | "vm-basic" | "executed" | "cleared"
        ) {
            let mut writer = Writer::open(&path)?;
            let r = writer.import(&frame)?;
            assert_eq!(r.hash, frame.meta().hash);
            assert_eq!(writer.executions(), u64::from(point != "cleared"));
        } else {
            let pending = std::fs::read(path.join(native_import::PENDING))?;
            assert!(read_guard(&path).is_err(), "{point}");
            assert!(Writer::open(&path).is_err(), "{point}");
            assert!(native::check_import_fences(&path).is_err(), "{point}");
            assert_eq!(
                std::fs::read(path.join(native_import::PENDING))?,
                pending,
                "{point}: never auto-clear"
            );
        }
    }
    Ok(())
}

#[test]
fn transition_actual_sigkill_preserves_multistore_fence_until_exact_receipt_and_head() -> Result<()>
{
    use crate::{
        native_epoch::{self, Request, Rewind},
        wire::Ref,
    };
    for point in [
        "intent",
        "unwound",
        "rocks-before",
        "rocks-after",
        "committed",
        "receipt",
        "head",
        "cleared",
    ] {
        let (_work, path, frame) = super::tests::setup()?;
        let mut writer = Writer::open(&path)?;
        writer.import(&frame)?;
        let identity = writer.identity().clone();
        let previous = writer.head().clone();
        let request = Request {
            version: 1,
            identity: identity.clone(),
            previous: previous.clone(),
            marker: Rewind {
                request_generation: 7,
                from: Ref {
                    generation: 7,
                    height: previous.tip.height,
                    hash: previous.tip.hash,
                },
                target: Ref {
                    generation: 8,
                    height: identity.anchor.height,
                    hash: identity.anchor.hash,
                },
                from_root: previous.tip.root,
                root: identity.anchor.root,
            },
        };
        drop(writer);
        checkpoint::write_private(&path.join("transition-request.json"), |w| {
            serde_json::to_writer(w, &request)?;
            Ok(())
        })?;
        let mut child = Process::start(&path, "transition", point)?;
        child.expect(&format!("PAUSED:{point}"))?;
        child.kill()?;
        drop(child);
        if matches!(point, "head" | "cleared") {
            let receipt = native_epoch::transition(&path, &request)?;
            assert_eq!(receipt.head.import_epoch, 2);
            assert_eq!(receipt.head.tip, identity.anchor);
            assert!(!path.join(native_import::PENDING).exists());
            drop(Writer::open(&path)?);
        } else {
            let pending = std::fs::read(path.join(native_import::PENDING))?;
            assert!(
                native_epoch::transition(&path, &request).is_err(),
                "{point}: SDK state cannot replace completed multi-store proof"
            );
            assert!(Writer::open(&path).is_err());
            assert!(read_guard(&path).is_err());
            assert_eq!(std::fs::read(path.join(native_import::PENDING))?, pending);
        }
    }
    Ok(())
}

#[test]
fn writer_turnstile_blocks_new_readers_until_publication_and_existing_reader_refreshes()
-> Result<()> {
    let (_work, path, frame) = super::tests::setup()?;
    let mut writer = Process::start(&path, "writer", "none")?;
    writer.expect("WRITER_READY")?;
    let mut first = Process::start(&path, "reader-hold", "none")?;
    first.expect("HELD:0")?;
    writer.frame(&frame)?;
    // The initial read admission also owns the turnstile briefly. Only the
    // post-execution rendezvous makes its next acquisition a publication wait.
    writer.expect("EXECUTED")?;
    let turn = lock_file(&path, ".arc-native-import-turn")?;
    let deadline = Instant::now() + Duration::from_secs(30);
    loop {
        match rustix::fs::flock(&turn, FlockOperation::NonBlockingLockExclusive) {
            Err(rustix::io::Errno::WOULDBLOCK) => break,
            Err(e) => return Err(e.into()),
            Ok(()) => rustix::fs::flock(&turn, FlockOperation::Unlock)?,
        }
        ensure!(
            Instant::now() < deadline,
            "writer never reached publication wait"
        );
        thread::yield_now();
    }
    let mut late = Process::start(&path, "reader-once", "none")?;
    late.expect("ATTEMPT")?;
    first.line("release")?;
    first.expect("RELEASED")?;
    writer.expect("COMMITTED")?;
    late.expect("READ:1")?;
    first.line("read")?;
    first.expect("READ:1")?;
    Ok(())
}

#[test]
fn writer_process_failure_reports_expected_phase_and_bounded_child_error() -> Result<()> {
    let (_work, path, _frame) = super::tests::setup()?;
    let mut child = Process::start(&path, "invalid-role", "none")?;
    let error = child.expect("NEVER_READY").unwrap_err().to_string();
    assert!(
        error.contains("NEVER_READY"),
        "missing expected phase: {error}"
    );
    assert!(
        error.contains("invalid owned test role"),
        "missing child failure: {error}"
    );
    assert!(error.len() < 20 << 10, "unbounded child diagnostics");
    Ok(())
}
