use crate::Verified;
use eyre::Result;
use std::path::Path;

/// Publish a new private durable result without replacing an existing path.
#[cfg(unix)]
pub fn write_result(path: &Path, result: &Verified) -> Result<()> {
    use eyre::{ensure, eyre};
    use std::{
        fs::{self, File},
        io::Write,
        os::unix::fs::MetadataExt,
    };
    ensure!(path.is_absolute(), "result path must be absolute");
    let dir = path
        .parent()
        .ok_or_else(|| eyre!("result needs parent directory"))?;
    let info = fs::symlink_metadata(dir)?;
    ensure!(
        info.is_dir()
            && info.mode() & 0o777 == 0o700
            && info.uid() == rustix::process::geteuid().as_raw(),
        "result parent must be owned and private"
    );
    let mut tmp = tempfile::NamedTempFile::new_in(dir)?;
    let owned = tmp.as_file().metadata()?;
    ensure!(info.uid() == owned.uid(), "temporary file owner changed");
    ensure!(
        owned.mode() & 0o777 == 0o600,
        "result temporary file must be private"
    );
    tmp.write_all(&result.encode()?)?;
    tmp.as_file().sync_all()?;
    let current = fs::symlink_metadata(dir)?;
    ensure!(
        info.dev() == current.dev()
            && info.ino() == current.ino()
            && current.mode() & 0o777 == 0o700,
        "result directory changed"
    );
    tmp.persist_noclobber(path)?;
    File::open(dir)?.sync_all()?;
    Ok(())
}

#[cfg(not(unix))]
pub fn write_result(_path: &Path, _result: &Verified) -> Result<()> {
    eyre::bail!("private durable native output requires Unix")
}
