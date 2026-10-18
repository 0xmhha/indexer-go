//! Local execution feed. Only a durable indexer ACK releases pruning progress.
use alloy_primitives::{Address, B256, U256};
use eyre::{Result, eyre};
use serde::{Deserialize, Serialize};
use std::{collections::BTreeMap, path::PathBuf, sync::Arc, time::Duration};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{UnixListener, UnixStream},
    sync::{Mutex, Semaphore},
    task::JoinSet,
};

#[cfg(feature = "reth")]
pub mod exex;
pub const VERSION: u32 = 1;
pub const MAX_FRAME: usize = 32 << 20;
pub const MAX_REQUEST: usize = 64 << 10;

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Bundle {
    pub height: u64,
    pub hash: B256,
    pub parent_hash: B256,
    pub header: String,
    pub body: String,
    pub receipts: String,
}

pub trait Provider: Send + Sync + 'static {
    fn head(&self) -> Result<u64>;
    fn hash(&self, height: u64) -> Result<B256>;
    fn block(&self, height: u64) -> Result<Bundle>;
    fn balances(&self, hash: B256, addresses: &[Address]) -> Result<Vec<U256>>;
}

#[derive(Deserialize)]
pub struct Request {
    pub version: u32,
    pub chain_id: u64,
    pub session: String,
    pub kind: String,
    #[serde(default)]
    pub height: u64,
    #[serde(default)]
    pub hash: B256,
    #[serde(default)]
    pub addresses: Vec<Address>,
}
#[derive(Debug, Serialize, Deserialize)]
pub struct FeedError {
    pub code: String,
    pub message: String,
}
#[derive(Debug, Serialize, Deserialize)]
pub struct Response {
    pub version: u32,
    pub chain_id: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<FeedError>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub block: Option<Bundle>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub height: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub balances: Option<Vec<U256>>,
}

#[derive(Clone)]
pub struct Config {
    pub socket: PathBuf,
    pub chain_id: u64,
    pub pending_blocks: usize,
    pub pending_bytes: usize,
}
impl Config {
    pub fn new(socket: PathBuf, chain_id: u64) -> Self {
        Self {
            socket,
            chain_id,
            pending_blocks: 256,
            pending_bytes: 256 << 20,
        }
    }
}
struct Pending {
    bundle: Bundle,
    bytes: usize,
}
#[derive(Default)]
struct State {
    session: String,
    pending: BTreeMap<u64, Pending>,
    bytes: usize,
}

/// Owns only the socket inode it bound, never a pre-existing path.
struct SocketGuard {
    path: PathBuf,
    dev: u64,
    ino: u64,
}
impl Drop for SocketGuard {
    fn drop(&mut self) {
        use std::os::unix::fs::MetadataExt;
        if let Ok(meta) = std::fs::symlink_metadata(&self.path) {
            if meta.dev() == self.dev && meta.ino() == self.ino {
                let _ = std::fs::remove_file(&self.path);
            }
        }
    }
}

/// The callback must release a *parent* height, keeping the next block's
/// starting state available. No notification, acquisition or state read ACKs.
pub async fn serve<P: Provider>(
    provider: P,
    cfg: Config,
    finished: impl Fn(u64, B256) -> Result<()> + Send + Sync + 'static,
) -> Result<()> {
    use std::os::unix::fs::{MetadataExt, PermissionsExt};
    if cfg.pending_blocks == 0 || cfg.pending_bytes < MAX_FRAME || !cfg.socket.is_absolute() {
        return Err(eyre!("invalid feed configuration"));
    }
    let listener = UnixListener::bind(&cfg.socket)?;
    let meta = std::fs::symlink_metadata(&cfg.socket)?;
    let _guard = SocketGuard {
        path: cfg.socket.clone(),
        dev: meta.dev(),
        ino: meta.ino(),
    };
    std::fs::set_permissions(&cfg.socket, std::fs::Permissions::from_mode(0o600))?;
    let provider = Arc::new(provider);
    let finished = Arc::new(finished);
    let state = Arc::new(Mutex::new(State::default()));
    // Bounded accepted calls and provider work. ACK does not wait for a
    // cache-capacity permit, so a full cache cannot starve pruning progress.
    let slots = Arc::new(Semaphore::new(16));
    let work = Arc::new(Semaphore::new(4));
    let mut tasks = JoinSet::new();
    loop {
        while let Some(result) = tasks.try_join_next() {
            result?;
        }
        tokio::select! {
            accepted = listener.accept() => {
                let (stream, _) = accepted?;
                let permit = slots.clone().acquire_owned().await?;
                let (provider,state,finished,cfg,work)=(provider.clone(),state.clone(),finished.clone(),cfg.clone(),work.clone());
                tasks.spawn(async move {
                    let _permit=permit;
                    let _=tokio::time::timeout(Duration::from_secs(15), connection(stream,provider,cfg,state,finished,work)).await;
                });
            }
            result=tasks.join_next(), if !tasks.is_empty() => {
                if let Some(Err(error))=result { return Err(error.into()); }
            }
        }
    }
}

async fn connection<P: Provider, F: Fn(u64, B256) -> Result<()> + Send + Sync + 'static>(
    mut stream: UnixStream,
    provider: Arc<P>,
    cfg: Config,
    state: Arc<Mutex<State>>,
    finished: Arc<F>,
    work: Arc<Semaphore>,
) -> Result<()> {
    let length = stream.read_u32().await? as usize;
    if length == 0 || length > MAX_REQUEST {
        return Err(eyre!("request frame exceeds bound"));
    }
    let mut raw = vec![0; length];
    stream.read_exact(&mut raw).await?;
    let request: Request = serde_json::from_slice(&raw)?;
    let permit = work.acquire_owned().await?;
    let runtime = tokio::runtime::Handle::current();
    // Native provider/database reads and large encodings must not block the
    // node's async executor. A timed-out call retains its work permit until
    // the synchronous operation exits, bounding even stalled blocking work.
    let raw = tokio::task::spawn_blocking(move || {
        let _permit = permit;
        let mut response = Response {
            version: VERSION,
            chain_id: cfg.chain_id,
            error: None,
            block: None,
            height: None,
            balances: None,
        };
        if let Err(error) = runtime.block_on(handle(
            provider.as_ref(),
            &cfg,
            &state,
            finished.as_ref(),
            request,
            &mut response,
        )) {
            let message = format!("{error:#}");
            let code = if message.starts_with("feed backpressure:") {
                "backpressure"
            } else if message.starts_with("inactive session:") {
                "inactive_session"
            } else if message == "ACK has no acquired block" {
                "ack_not_acquired"
            } else if message.starts_with("block not found:") {
                "not_found"
            } else {
                "feed_error"
            };
            response.error = Some(FeedError {
                code: code.into(),
                message,
            });
        }
        let raw = serde_json::to_vec(&response)?;
        if raw.len() > MAX_FRAME {
            return Err(eyre!("response frame exceeds bound"));
        }
        Ok::<_, eyre::Report>(raw)
    })
    .await??;
    stream.write_u32(raw.len() as u32).await?;
    stream.write_all(&raw).await?;
    Ok(())
}

async fn handle<P: Provider>(
    provider: &P,
    cfg: &Config,
    state: &Mutex<State>,
    finished: &(impl Fn(u64, B256) -> Result<()> + Send + Sync),
    request: Request,
    response: &mut Response,
) -> Result<()> {
    if request.version != VERSION || request.chain_id != cfg.chain_id || request.session.len() != 32
    {
        return Err(eyre!("protocol, chain or session mismatch"));
    }
    if request.kind == "hello" {
        if request.hash != provider.hash(0)? {
            return Err(eyre!("genesis mismatch"));
        }
        let mut state = state.lock().await;
        // One logical database per socket/ExEx ID. A process restart uses the
        // same stable consumer identity; another database cannot steal retention.
        if !state.session.is_empty() && state.session != request.session {
            return Err(eyre!("feed already belongs to another consumer"));
        }
        state.session = request.session;
        response.height = Some(provider.head()?);
        return Ok(());
    }
    let mut state = state.lock().await;
    if state.session != request.session {
        return Err(eyre!("inactive session: reconnect with hello"));
    }
    match request.kind.as_str() {
        "head" => response.height = Some(provider.head()?),
        "block" => {
            let canonical = provider.hash(request.height)?;
            if state
                .pending
                .get(&request.height)
                .is_some_and(|p| p.bundle.hash != canonical)
            {
                let removed = state.pending.split_off(&request.height);
                state.bytes -= removed.values().map(|p| p.bytes).sum::<usize>();
            }
            if let Some(pending) = state.pending.get(&request.height) {
                response.block = Some(pending.bundle.clone());
            } else {
                if state.pending.len() >= cfg.pending_blocks {
                    return Err(eyre!("feed backpressure: pending block bound"));
                }
                let bundle = provider.block(request.height)?;
                if bundle.hash != canonical || bundle.height != request.height {
                    return Err(eyre!("block changed during acquisition"));
                }
                let bytes = serde_json::to_vec(&bundle)?.len();
                if bytes + 1024 > MAX_FRAME || bytes > cfg.pending_bytes {
                    return Err(eyre!("block payload exceeds bound"));
                }
                if state.bytes + bytes > cfg.pending_bytes {
                    return Err(eyre!("feed backpressure: pending byte bound"));
                }
                state.bytes += bytes;
                state.pending.insert(
                    request.height,
                    Pending {
                        bundle: bundle.clone(),
                        bytes,
                    },
                );
                response.block = Some(bundle);
            }
        }
        "balance" => {
            if request.addresses.is_empty() || request.addresses.len() > 64 {
                return Err(eyre!("balance batch outside 1..64"));
            }
            let balances = provider.balances(request.hash, &request.addresses)?;
            if balances.len() != request.addresses.len() {
                return Err(eyre!("incomplete state reply"));
            }
            response.balances = Some(balances);
        }
        "ack" => {
            let pending = state
                .pending
                .get(&request.height)
                .ok_or_else(|| eyre!("ACK has no acquired block"))?;
            if pending.bundle.hash != request.hash || provider.hash(request.height)? != request.hash
            {
                return Err(eyre!("noncanonical ACK"));
            }
            if request.height > 0 {
                finished(request.height - 1, pending.bundle.parent_hash)?;
            }
            let higher = state.pending.split_off(&(request.height.saturating_add(1)));
            state.pending = higher;
            state.bytes = state.pending.values().map(|p| p.bytes).sum();
        }
        _ => return Err(eyre!("unsupported feed request")),
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::{
        Mutex as StdMutex,
        atomic::{AtomicU8, Ordering},
    };
    struct Fake {
        epoch: AtomicU8,
    }
    impl Provider for Fake {
        fn head(&self) -> Result<u64> {
            Ok(2)
        }
        fn hash(&self, n: u64) -> Result<B256> {
            Ok(B256::with_last_byte(
                n as u8
                    + if n > 0 {
                        self.epoch.load(Ordering::SeqCst)
                    } else {
                        0
                    },
            ))
        }
        fn block(&self, n: u64) -> Result<Bundle> {
            Ok(Bundle {
                height: n,
                hash: self.hash(n)?,
                parent_hash: self.hash(n.saturating_sub(1))?,
                header: "AA==".into(),
                body: "AA==".into(),
                receipts: "AA==".into(),
            })
        }
        fn balances(&self, _: B256, _: &[Address]) -> Result<Vec<U256>> {
            Err(eyre!("state unavailable"))
        }
    }
    fn request(kind: &str, height: u64, hash: B256) -> Request {
        Request {
            version: VERSION,
            chain_id: 42,
            session: "a".repeat(32),
            kind: kind.into(),
            height,
            hash,
            addresses: vec![Address::ZERO],
        }
    }
    fn response() -> Response {
        Response {
            version: VERSION,
            chain_id: 42,
            error: None,
            block: None,
            height: None,
            balances: None,
        }
    }
    #[tokio::test]
    async fn acquisition_never_releases_pruning_and_ack_is_hash_checked() {
        let provider = Fake {
            epoch: AtomicU8::new(0),
        };
        let state = Mutex::new(State::default());
        let mut cfg = Config::new(PathBuf::from("/tmp/test-feed"), 42);
        cfg.pending_blocks = 1;
        let released = StdMutex::new(Vec::new());
        let finished = |n, h| {
            released.lock().unwrap().push((n, h));
            Ok(())
        };
        handle(
            &provider,
            &cfg,
            &state,
            &finished,
            request("hello", 0, B256::ZERO),
            &mut response(),
        )
        .await
        .unwrap();
        handle(
            &provider,
            &cfg,
            &state,
            &finished,
            request("block", 1, B256::ZERO),
            &mut response(),
        )
        .await
        .unwrap();
        assert!(released.lock().unwrap().is_empty());
        assert!(
            handle(
                &provider,
                &cfg,
                &state,
                &finished,
                request("block", 2, B256::ZERO),
                &mut response()
            )
            .await
            .is_err()
        );
        assert!(
            handle(
                &provider,
                &cfg,
                &state,
                &finished,
                request("ack", 1, B256::ZERO),
                &mut response()
            )
            .await
            .is_err()
        );
        assert!(
            handle(
                &provider,
                &cfg,
                &state,
                &finished,
                request("balance", 1, B256::ZERO),
                &mut response()
            )
            .await
            .is_err()
        );
        assert!(released.lock().unwrap().is_empty());
        handle(
            &provider,
            &cfg,
            &state,
            &finished,
            request("ack", 1, provider.hash(1).unwrap()),
            &mut response(),
        )
        .await
        .unwrap();
        assert_eq!(*released.lock().unwrap(), vec![(0, B256::ZERO)]);
        assert!(state.lock().await.pending.is_empty());
    }
    #[tokio::test]
    async fn fork_and_consumer_changes_cannot_ack_old_data() {
        let provider = Fake {
            epoch: AtomicU8::new(0),
        };
        let state = Mutex::new(State::default());
        let cfg = Config::new(PathBuf::from("/tmp/test-feed"), 42);
        let finished = |_, _| Ok(());
        handle(
            &provider,
            &cfg,
            &state,
            &finished,
            request("hello", 0, B256::ZERO),
            &mut response(),
        )
        .await
        .unwrap();
        handle(
            &provider,
            &cfg,
            &state,
            &finished,
            request("block", 1, B256::ZERO),
            &mut response(),
        )
        .await
        .unwrap();
        let old = provider.hash(1).unwrap();
        provider.epoch.store(10, Ordering::SeqCst);
        assert!(
            handle(
                &provider,
                &cfg,
                &state,
                &finished,
                request("ack", 1, old),
                &mut response()
            )
            .await
            .is_err()
        );
        handle(
            &provider,
            &cfg,
            &state,
            &finished,
            request("block", 1, B256::ZERO),
            &mut response(),
        )
        .await
        .unwrap();
        assert_eq!(
            state.lock().await.pending[&1].bundle.hash,
            provider.hash(1).unwrap()
        );
        let mut other = request("hello", 0, B256::ZERO);
        other.session = "b".repeat(32);
        assert!(
            handle(&provider, &cfg, &state, &finished, other, &mut response())
                .await
                .is_err()
        );
    }
}
