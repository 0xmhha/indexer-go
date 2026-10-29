//! Owned-process SDK crash and reader-publication qualification, never a node.
use super::*;
use std::{
    collections::HashMap,
    io::{BufRead, BufReader},
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
}
impl Process {
    fn start(path: &Path, role: &str, point: &str) -> Result<Self> {
        let mut child = Command::new(std::env::current_exe()?)
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
            .spawn()?;
        let input = child.stdin.take();
        let stdout = child.stdout.take().ok_or_else(|| eyre!("owned stdout"))?;
        let stderr = child.stderr.take().ok_or_else(|| eyre!("owned stderr"))?;
        let (tx, rx) = mpsc::sync_channel(16);
        let output = thread::spawn(move || {
            let mut reader = BufReader::new(stdout);
            loop {
                let mut line = String::new();
                match (&mut reader).take(4097).read_line(&mut line) {
                    Ok(0) | Err(_) => break,
                    Ok(_) if line.len() > 4096 => break,
                    Ok(_) => {
                        if tx.send(line).is_err() {
                            break;
                        }
                    }
                }
            }
        });
        let errors = thread::spawn(move || {
            let mut r = stderr;
            let mut buf = [0u8; 1024];
            while r.read(&mut buf).is_ok_and(|n| n > 0) {}
        });
        Ok(Self {
            child,
            input,
            lines: Some(rx),
            output: Some(output),
            errors: Some(errors),
        })
    }
    fn expect(&mut self, tag: &str) -> Result<()> {
        let end = Instant::now() + Duration::from_secs(30);
        loop {
            let line = self
                .lines
                .as_ref()
                .unwrap()
                .recv_timeout(end.saturating_duration_since(Instant::now()))?;
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
fn writer_turnstile_blocks_new_readers_until_publication_and_existing_reader_refreshes()
-> Result<()> {
    let (_work, path, frame) = super::tests::setup()?;
    let mut writer = Process::start(&path, "writer", "none")?;
    writer.expect("WRITER_READY")?;
    let mut first = Process::start(&path, "reader-hold", "none")?;
    first.expect("HELD:0")?;
    writer.frame(&frame)?;
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
