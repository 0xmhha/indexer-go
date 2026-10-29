//! Pinned native DB operations; never construct node services.

use crate::{
    catalog::{Builder, Key},
    checkpoint,
};
use arc_execution_config::chainspec::ArcChainSpec;
use eyre::{Result, ensure, eyre};
use reth_ethereum::{
    provider::{
        BlockHashReader, EthStorage, HeaderProvider, ProviderFactory,
        db::{DatabaseEnv, mdbx::DatabaseArguments, open_db},
        providers::{ProviderFactoryBuilder, ReadOnlyConfig, RocksDBProvider, StaticFileProvider},
    },
    tasks::Runtime,
};
use reth_ethereum_engine_primitives::EthPayloadTypes;
use reth_ethereum_primitives::EthPrimitives;
use reth_node_types::{AnyNodeTypes, NodeTypesWithDBAdapter};
use reth_storage_api::{
    BlockNumReader, ChangeSetReader, StorageChangeSetReader, StorageSettingsCache,
};
use std::{path::Path, sync::Arc};

pub type NativeTypes = AnyNodeTypes<EthPrimitives, ArcChainSpec, EthStorage, EthPayloadTypes>;

pub fn read_only_config(
    datadir: &std::path::Path,
    scratch: Option<&std::path::Path>,
) -> Result<(ReadOnlyConfig, tempfile::TempDir)> {
    #[cfg(unix)]
    {
        use std::os::unix::{fs::MetadataExt, fs::symlink};
        if let Some(parent) = scratch {
            let info = std::fs::symlink_metadata(parent)?;
            ensure!(
                parent.is_absolute()
                    && info.is_dir()
                    && info.mode() & 0o777 == 0o700
                    && info.uid() == rustix::process::geteuid().as_raw(),
                "scratch parent must be absolute, owned and private"
            );
        }
        let work = if let Some(parent) = scratch {
            tempfile::Builder::new()
                .prefix("secondary-")
                .tempdir_in(parent)?
        } else {
            tempfile::Builder::new()
                .prefix("arc-secondary-")
                .tempdir()?
        };
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(work.path(), std::fs::Permissions::from_mode(0o700))?;
        let native = datadir.canonicalize()?;
        let private = work.path().canonicalize()?;
        ensure!(
            !private.starts_with(&native) && !native.starts_with(&private),
            "native and scratch paths must be separate"
        );
        let primary = work.path().join("rocksdb-primary");
        // The pinned upstream secondary puts its LOG directory next to the
        // supplied primary path. Keep that lexical path in our private workdir;
        // the symlink target is opened read-only and is never removed by cleanup.
        symlink(native.join("rocksdb"), &primary)?;
        let mut config = ReadOnlyConfig::from_datadir(datadir);
        config.rocksdb_dir = primary;
        // provider() synchronizes on every new native transaction. Avoid the
        // upstream watcher retaining the factory and its secondary until exit.
        config.watch = false;
        Ok((config, work))
    }
    #[cfg(not(unix))]
    {
        let _ = (datadir, scratch);
        eyre::bail!("private native workspace requires Unix")
    }
}

pub type Factory = ProviderFactory<NodeTypesWithDBAdapter<NativeTypes, DatabaseEnv>>;
pub fn open_readonly(
    datadir: &Path,
    spec: Arc<ArcChainSpec>,
    exclusive: bool,
) -> Result<(Factory, tempfile::TempDir)> {
    open_readonly_in(datadir, spec, exclusive, None)
}
pub fn open_readonly_in(
    datadir: &Path,
    spec: Arc<ArcChainSpec>,
    exclusive: bool,
    scratch: Option<&Path>,
) -> Result<(Factory, tempfile::TempDir)> {
    layout(datadir)?;
    let (mut c, work) = read_only_config(datadir, scratch)?;
    c.db_args = c.db_args.with_exclusive(Some(exclusive));
    if exclusive {
        c = c.disable_long_read_transaction_safety();
    }
    let f = ProviderFactoryBuilder::<NativeTypes>::default().open_read_only(
        spec,
        c,
        Runtime::test(),
    )?;
    Ok((f, work))
}
pub fn layout(path: &Path) -> Result<()> {
    ensure!(path.is_absolute(), "absolute native datadir required");
    for name in ["db", "rocksdb", "static_files"] {
        ensure!(
            std::fs::symlink_metadata(path.join(name))?.is_dir(),
            "existing native layout required"
        );
    }
    Ok(())
}
pub fn open_writable(datadir: &Path, spec: Arc<ArcChainSpec>) -> Result<Factory> {
    layout(datadir)?;
    let db = open_db(
        datadir.join("db"),
        DatabaseArguments::default().with_exclusive(Some(true)),
    )?;
    let static_files = StaticFileProvider::read_write(datadir.join("static_files"))?;
    let rocks = RocksDBProvider::builder(datadir.join("rocksdb"))
        .with_default_tables()
        .build()?;
    Ok(ProviderFactory::new(
        db,
        spec,
        static_files,
        rocks,
        Runtime::test(),
    )?)
}
// Collect raw key candidates from genesis/prior full export and native history.
// Reth v2 hashed current tables alone cannot reveal original addresses/slots.
pub fn export_checkpoint(
    f: &Factory,
    spec: &ArcChainSpec,
    height: u64,
    seed: Option<&Path>,
    output: &Path,
    workspace: &Path,
) -> Result<()> {
    crate::history::require(f, height)?;
    let p = f.provider()?;
    ensure!(
        p.cached_storage_settings().storage_v2,
        "only pinned native v2 layout supported"
    );
    ensure!(
        p.block_hash(0)? == Some(spec.inner.genesis_hash()) && height <= p.best_block_number()?,
        "native chain/completed import"
    );
    let header = p
        .header_by_number(height)?
        .ok_or_else(|| eyre!("retained checkpoint header unavailable"))?;
    let mut keys = Builder::new(workspace)?;
    let start = if let Some(seed) = seed {
        let m = checkpoint::seed_into(
            seed,
            &spec.inner.chain.id().to_string(),
            spec.inner.genesis_hash(),
            &mut keys,
        )?;
        ensure!(
            m.r#ref.height <= height && p.block_hash(m.r#ref.height)? == Some(m.r#ref.hash),
            "seed is not a canonical ancestor"
        );
        m.r#ref.height
    } else {
        for (a, v) in &spec.inner.genesis.alloc {
            keys.push(Key::account(*a))?;
            if let Some(storage) = &v.storage {
                for slot in storage.keys() {
                    keys.push(Key::storage(*a, *slot))?;
                }
            }
        }
        0
    };
    crate::history::require(f, start)?;
    for n in start + 1..=height {
        for c in p.account_block_changeset(n)? {
            keys.push(Key::account(c.address))?;
        }
        for c in p.storage_block_changeset(n)? {
            keys.push(Key::storage(c.address, c.key))?;
        }
    }
    let keys = keys.finish()?;
    let state = p.history_by_block_hash(header.hash_slow())?;
    checkpoint::write_private(output, |w| {
        checkpoint::export_catalog(
            w,
            &spec.inner.chain.id().to_string(),
            spec.inner.genesis_hash(),
            &header,
            &state,
            &keys,
        )?;
        ensure!(
            f.provider()?.block_hash(height)? == Some(header.hash_slow()),
            "checkpoint canonicality changed"
        );
        Ok(())
    })
}

pub fn check_start(datadir: &Path) -> Result<()> {
    match std::fs::symlink_metadata(datadir.join(crate::bootstrap::PENDING)) {
        Ok(_) => eyre::bail!("native bootstrap incomplete; explicit inspection required"),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
        Err(e) => return Err(e.into()),
    }
    match std::fs::symlink_metadata(datadir.join(".execution-retention-pending")) {
        Ok(_) => eyre::bail!(
            "matched native snapshot remains fenced; exact E3 restore activation required"
        ),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
        Err(e) => return Err(e.into()),
    }
    let name = datadir
        .file_name()
        .ok_or_else(|| eyre!("native directory name"))?
        .to_str()
        .ok_or_else(|| eyre!("native directory encoding"))?;
    let parent = datadir
        .parent()
        .ok_or_else(|| eyre!("native directory parent"))?;
    match std::fs::symlink_metadata(parent.join(format!(".{name}.execution-retention"))) {
        Ok(_) => eyre::bail!("native retention pending; exact recovery/release required"),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(e.into()),
    }
}
