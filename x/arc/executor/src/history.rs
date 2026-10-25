//! Offline history maintenance on an already sealed COW candidate only.
use crate::native::Factory;
use alloy_primitives::{Address, B256};
use eyre::{Result, ensure};
use reth_ethereum::provider::{
    RocksDBProviderFactory,
    db::{BlockNumberList, table::Table, tables},
    providers::RocksDBProvider,
};
use reth_prune_types::{PruneCheckpoint, PruneMode, PruneSegment};
use reth_storage_api::{PruneCheckpointReader, PruneCheckpointWriter, StorageSettingsCache};

const BATCH_ROWS: usize = 64;
const SHARD_ENTRIES: u64 = 2_000;
type Prefix = (Address, Option<B256>);

// Native shards are ordered lower-to-upper ranges with a final MAX sentinel.
// Removing a lower prefix leaves each surviving upper key and the sentinel
// unchanged. Stream rows instead of collecting all shards for a hot account.
fn index<T: Table<Value = BlockNumberList>>(
    rocks: &RocksDBProvider,
    through: u64,
    apply: bool,
    parts: impl Fn(&T::Key) -> (Prefix, u64),
) -> Result<()>
where
    T::Key: Clone,
{
    let mut previous = None;
    let mut last_value = None;
    let mut last_upper = None;
    let mut batch = apply.then(|| rocks.batch());
    for row in rocks.iter::<T>()? {
        let (key, blocks) = row?;
        let (prefix, upper) = parts(&key);
        ensure!(
            !blocks.is_empty() && blocks.len() <= SHARD_ENTRIES,
            "native history shard bounds"
        );
        let first = blocks.min().expect("nonempty shard");
        let last = blocks.max().expect("nonempty shard");
        if previous != Some(prefix) {
            ensure!(
                previous.is_none() || last_upper == Some(u64::MAX),
                "native history sentinel missing"
            );
            last_value = None;
        } else {
            ensure!(
                last_upper != Some(u64::MAX),
                "native history sentinel order"
            );
        }
        ensure!(
            last_value.is_none_or(|n| first > n) && (upper == u64::MAX || upper == last),
            "native history shard order/upper key"
        );
        previous = Some(prefix);
        last_value = Some(last);
        last_upper = Some(upper);
        if first <= through {
            ensure!(apply, "expired native history index remains");
            let keep = BlockNumberList::new_pre_sorted(blocks.iter().filter(|n| *n > through));
            let b = batch.as_mut().expect("writable maintenance batch");
            if keep.is_empty() {
                b.delete::<T>(key)?;
            } else {
                b.put::<T>(key, &keep)?;
            }
            if b.len() >= BATCH_ROWS {
                batch.take().expect("maintenance batch").commit()?;
                batch = Some(rocks.batch());
            }
        }
    }
    ensure!(
        previous.is_none() || last_upper == Some(u64::MAX),
        "native history final sentinel missing"
    );
    if let Some(b) = batch
        && !b.is_empty()
    {
        b.commit()?;
    }
    Ok(())
}
fn indices(rocks: &RocksDBProvider, through: u64, apply: bool) -> Result<()> {
    index::<tables::AccountsHistory>(rocks, through, apply, |k| {
        ((k.key, None), k.highest_block_number)
    })?;
    index::<tables::StoragesHistory>(rocks, through, apply, |k| {
        (
            (k.address, Some(k.sharded_key.key)),
            k.sharded_key.highest_block_number,
        )
    })
}
pub fn floor(f: &Factory) -> Result<u64> {
    let p = f.provider()?;
    let mut floor = 0;
    for segment in [PruneSegment::AccountHistory, PruneSegment::StorageHistory] {
        if let Some(n) = p
            .get_prune_checkpoint(segment)?
            .and_then(|c| c.block_number)
        {
            floor = floor.max(
                n.checked_add(1)
                    .ok_or_else(|| eyre::eyre!("native history floor overflow"))?,
            );
        }
    }
    Ok(floor)
}
pub fn require(f: &Factory, height: u64) -> Result<()> {
    ensure!(
        height >= floor(f)?,
        "requested state is below native history floor"
    );
    Ok(())
}
pub fn prune(f: &Factory, through: u64) -> Result<()> {
    ensure!(
        floor(f)? <= through,
        "native history floor would move backwards"
    );
    let p = f.provider()?;
    ensure!(
        p.cached_storage_settings().storage_v2,
        "native v2 history required"
    );
    let rocks = p.rocksdb_provider();
    drop(p);
    let pruned = through
        .checked_sub(1)
        .ok_or_else(|| eyre::eyre!("history cutoff must be positive"))?;
    indices(&rocks, pruned, true)?;
    let p = f.provider_rw()?;
    for segment in [PruneSegment::AccountHistory, PruneSegment::StorageHistory] {
        p.save_prune_checkpoint(
            segment,
            PruneCheckpoint {
                block_number: Some(pruned),
                tx_number: None,
                prune_mode: PruneMode::Before(through),
            },
        )?;
    }
    p.commit()?;
    Ok(())
}
pub fn verify(f: &Factory, through: u64) -> Result<()> {
    let pruned = through
        .checked_sub(1)
        .ok_or_else(|| eyre::eyre!("history cutoff must be positive"))?;
    let p = f.provider()?;
    for segment in [PruneSegment::AccountHistory, PruneSegment::StorageHistory] {
        ensure!(
            p.get_prune_checkpoint(segment)?
                .is_some_and(|c| c.block_number == Some(pruned)
                    && c.tx_number.is_none()
                    && c.prune_mode == PruneMode::Before(through)),
            "native history checkpoint differs"
        );
    }
    indices(&p.rocksdb_provider(), pruned, false)
}
pub fn compact(f: &Factory) -> Result<()> {
    f.provider()?.rocksdb_provider().flush_and_compact()?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use arc_execution_config::chainspec::TESTNET;
    use reth_ethereum::{
        provider::{
            ProviderFactory,
            db::init_db,
            providers::{RocksDBProvider, StaticFileProvider},
        },
        tasks::Runtime,
    };
    #[test]
    fn native_adapter_floor_rejects_old_parents_and_seeds() -> Result<()> {
        let dir = tempfile::tempdir()?;
        let db = init_db(dir.path().join("db"), Default::default())?;
        let sf = StaticFileProvider::read_write(dir.path().join("static_files"))?;
        let rocks = RocksDBProvider::builder(dir.path().join("rocksdb"))
            .with_default_tables()
            .build()?;
        let f: Factory = ProviderFactory::new(db, TESTNET.clone(), sf, rocks, Runtime::test())?;
        let p = f.provider_rw()?;
        p.save_prune_checkpoint(
            PruneSegment::AccountHistory,
            PruneCheckpoint {
                block_number: Some(2),
                tx_number: None,
                prune_mode: PruneMode::Before(3),
            },
        )?;
        p.commit()?;
        assert!(
            require(&f, 2).is_err(),
            "native adapters must reject state below the root-safe history floor"
        );
        require(&f, 3)?;
        Ok(())
    }
    #[test]
    fn shards_are_trimmed_in_bounded_batches_with_sentinels_preserved() -> Result<()> {
        use reth_ethereum::provider::db::models::{
            ShardedKey, storage_sharded_key::StorageShardedKey,
        };
        let dir = tempfile::tempdir()?;
        let rocks = RocksDBProvider::builder(dir.path().join("rocks"))
            .with_default_tables()
            .build()?;
        let a = Address::repeat_byte(0xa1);
        let slot = B256::repeat_byte(7);
        let mut batch = rocks.batch();
        batch.put::<tables::AccountsHistory>(
            ShardedKey::new(a, 2),
            &BlockNumberList::new_pre_sorted([1, 2]),
        )?;
        batch.put::<tables::AccountsHistory>(
            ShardedKey::last(a),
            &BlockNumberList::new_pre_sorted([3, 4, 8]),
        )?;
        batch.put::<tables::StoragesHistory>(
            StorageShardedKey::last(a, slot),
            &BlockNumberList::new_pre_sorted([1, 2, 5]),
        )?;
        for n in 0..130u64 {
            let mut addr = [0u8; 20];
            addr[12..].copy_from_slice(&n.to_be_bytes());
            batch.put::<tables::AccountsHistory>(
                ShardedKey::last(Address::from(addr)),
                &BlockNumberList::new_pre_sorted([1, 4]),
            )?;
        }
        batch.commit()?;
        indices(&rocks, 2, true)?;
        indices(&rocks, 2, false)?;
        assert!(
            rocks
                .get::<tables::AccountsHistory>(ShardedKey::new(a, 2))?
                .is_none()
        );
        assert_eq!(
            rocks
                .get::<tables::AccountsHistory>(ShardedKey::last(a))?
                .unwrap()
                .iter()
                .collect::<Vec<_>>(),
            vec![3, 4, 8]
        );
        assert_eq!(
            rocks
                .get::<tables::StoragesHistory>(StorageShardedKey::last(a, slot))?
                .unwrap()
                .iter()
                .collect::<Vec<_>>(),
            vec![5]
        );
        for row in rocks.iter::<tables::AccountsHistory>()? {
            let (_, v) = row?;
            assert!(v.min().unwrap() > 2);
        }
        Ok(())
    }
    #[test]
    fn corrupt_shards_are_not_repaired_or_accepted() -> Result<()> {
        use reth_ethereum::provider::db::models::ShardedKey;
        for damage in 0..4 {
            let dir = tempfile::tempdir()?;
            let rocks = RocksDBProvider::builder(dir.path().join("rocks"))
                .with_default_tables()
                .build()?;
            let a = Address::repeat_byte(0xab);
            let mut batch = rocks.batch();
            match damage {
                0 => batch.put::<tables::AccountsHistory>(
                    ShardedKey::new(a, 3),
                    &BlockNumberList::new_pre_sorted([1, 2]),
                )?,
                1 => batch.put::<tables::AccountsHistory>(
                    ShardedKey::new(a, 2),
                    &BlockNumberList::new_pre_sorted([1, 2]),
                )?,
                2 => {
                    batch.put::<tables::AccountsHistory>(
                        ShardedKey::new(a, 2),
                        &BlockNumberList::new_pre_sorted([1, 2]),
                    )?;
                    batch.put::<tables::AccountsHistory>(
                        ShardedKey::last(a),
                        &BlockNumberList::new_pre_sorted([2, 4]),
                    )?;
                }
                _ => batch.put::<tables::AccountsHistory>(
                    ShardedKey::last(a),
                    &BlockNumberList::new_pre_sorted(0..=2000),
                )?,
            }
            batch.commit()?;
            assert!(indices(&rocks, 0, true).is_err());
        }
        Ok(())
    }
}
