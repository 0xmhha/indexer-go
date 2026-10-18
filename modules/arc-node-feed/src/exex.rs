//! Pinned Reth 2.2 adapter. ArcNode uses exactly EthPrimitives.
use crate::{Bundle, Config, Provider};
use alloy_consensus::{BlockHeader, TxReceipt};
use alloy_eips::eip2718::Encodable2718;
use alloy_primitives::{Address, B256, U256};
use base64::{Engine, engine::general_purpose::STANDARD};
use eyre::{Result, eyre};
use futures::StreamExt;
use reth_ethereum_primitives::EthPrimitives;
use reth_exex::{ExExContext, ExExEvent};
use reth_node_api::{FullNodeComponents, NodeTypes};
use reth_provider::{BlockReader, StateProviderFactory};
use std::{
    collections::BTreeMap,
    sync::{
        Arc, RwLock,
        atomic::{AtomicU64, Ordering},
    },
};

type HotCache = Arc<RwLock<BTreeMap<u64, Bundle>>>;

#[derive(Clone)]
struct NativeProvider<P> {
    provider: P,
    hot: HotCache,
}
impl<P> Provider for NativeProvider<P>
where
    P: BlockReader<
            Block = alloy_consensus::Block<reth_ethereum_primitives::TransactionSigned>,
            Receipt = reth_ethereum_primitives::Receipt,
        > + StateProviderFactory
        + Sync
        + 'static,
{
    fn head(&self) -> Result<u64> {
        Ok(self.provider.best_block_number()?)
    }
    fn hash(&self, height: u64) -> Result<B256> {
        self.provider
            .block_hash(height)?
            .ok_or_else(|| eyre!("block not found: {height}"))
    }
    fn block(&self, height: u64) -> Result<Bundle> {
        if let Some(bundle) = self
            .hot
            .read()
            .map_err(|_| eyre!("feed cache poisoned"))?
            .get(&height)
        {
            if self.provider.block_hash(height)? == Some(bundle.hash) {
                return Ok(bundle.clone());
            }
        }
        let block = self
            .provider
            .block_by_number(height)?
            .ok_or_else(|| eyre!("block not found: {height}"))?;
        let hash = block.header.hash_slow();
        let receipts = self
            .provider
            .receipts_by_block(hash.into())?
            .ok_or_else(|| eyre!("receipts unavailable: {height}"))?;
        let mut payload = Vec::new();
        for receipt in receipts {
            receipt.with_bloom_ref().network_encode(&mut payload);
        }
        let mut encoded = Vec::new();
        alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        }
        .encode(&mut encoded);
        encoded.extend_from_slice(&payload);
        Ok(Bundle {
            height,
            hash,
            parent_hash: block.header.parent_hash(),
            header: STANDARD.encode(alloy_rlp::encode(&block.header)),
            body: STANDARD.encode(alloy_rlp::encode(&block.body)),
            receipts: STANDARD.encode(encoded),
        })
    }
    fn balances(&self, hash: B256, addresses: &[Address]) -> Result<Vec<U256>> {
        let state = self.provider.state_by_block_hash(hash)?;
        addresses
            .iter()
            .map(|addr| Ok(state.account_balance(addr)?.unwrap_or(U256::ZERO)))
            .collect()
    }
}

pub async fn run<N>(mut ctx: ExExContext<N>, cfg: Config) -> Result<()>
where
    N: FullNodeComponents<Types: NodeTypes<Primitives = EthPrimitives>>,
{
    let events = ctx.events.clone();
    let hot: HotCache = Arc::default();
    let provider = NativeProvider {
        provider: ctx.provider().clone(),
        hot: hot.clone(),
    };
    let server = crate::serve(provider, cfg, move |number, hash| {
        events
            .send(ExExEvent::FinishedHeight(alloy_eips::BlockNumHash::new(
                number, hash,
            )))
            .map_err(|_| eyre!("ExEx manager closed"))
    });
    tokio::pin!(server);
    let epoch = Arc::new(AtomicU64::new(0));
    let cache_work = Arc::new(tokio::sync::Semaphore::new(1));
    let mut cache_tasks = tokio::task::JoinSet::new();
    loop {
        while let Some(result) = cache_tasks.try_join_next() {
            result??;
        }
        tokio::select! {
            result=&mut server => return result,
            result=cache_tasks.join_next(), if !cache_tasks.is_empty() => { if let Some(result)=result { result??; } },
            notification=ctx.notifications.next() => {
                // Delivery never advances pruning. Only a durable, hash-checked
                // indexer ACK sent through the server may emit FinishedHeight.
                match notification {
                    Some(Ok(notification)) => {
                        let generation=epoch.fetch_add(1,Ordering::SeqCst)+1;
                        if let Some(old)=notification.reverted_chain() {
                            if let Some(first)=old.blocks_iter().next() {
                                hot.write().map_err(|_|eyre!("feed cache poisoned"))?.retain(|height,_|*height<first.number());
                            }
                        }
                        if let Some(new)=notification.committed_chain() {
                            if let Ok(permit)=cache_work.clone().try_acquire_owned() {
                                let (hot,epoch)=(hot.clone(),epoch.clone());
                                cache_tasks.spawn_blocking(move || {
                                    let _permit=permit;
                                    let mut bundles=Vec::new();
                                    for (block,receipts) in new.blocks_and_receipts().skip(new.len().saturating_sub(8)) {
                                        let mut payload=Vec::new();
                                        for receipt in receipts { receipt.with_bloom_ref().network_encode(&mut payload); }
                                        let mut encoded=Vec::new();
                                        alloy_rlp::Header { list:true,payload_length:payload.len() }.encode(&mut encoded);
                                        encoded.extend_from_slice(&payload);
                                        let bundle=Bundle { height:block.number(),hash:block.hash(),parent_hash:block.parent_hash(),header:STANDARD.encode(alloy_rlp::encode(block.header())),body:STANDARD.encode(alloy_rlp::encode(block.body())),receipts:STANDARD.encode(encoded) };
                                        if bundle.header.len()+bundle.body.len()+bundle.receipts.len()<=crate::MAX_FRAME { bundles.push(bundle); }
                                    }
                                    let mut cache=hot.write().map_err(|_|eyre!("feed cache poisoned"))?;
                                    // A newer notification/reorg invalidates this materialization.
                                    if epoch.load(Ordering::SeqCst)!=generation { return Ok::<_,eyre::Report>(()); }
                                    for bundle in bundles { cache.insert(bundle.height,bundle); }
                                    while cache.len()>8 || cache.values().map(|b|b.header.len()+b.body.len()+b.receipts.len()).sum::<usize>()>crate::MAX_FRAME { cache.pop_first(); }
                                    Ok(())
                                });
                            }
                            // A busy materializer may skip priming; acquisition still reads
                            // canonical data from the same retained native provider.
                        }
                    },
                    Some(Err(error))=>return Err(error),None=>return Err(eyre!("ExEx notification stream closed"))
                }
            }
        }
    }
}
