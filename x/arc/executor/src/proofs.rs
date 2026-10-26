//! Selected standard-MPT evidence from one exact retained block-end view.
use alloy_primitives::{Address, B256, Bytes, U256, keccak256};
use alloy_trie::EMPTY_ROOT_HASH;
use eyre::{Result, ensure, eyre};
use reth_storage_api::{StateProofProvider, StateProvider};
use reth_trie::{AccountProof, TrieInput};
use serde::{Deserialize, Serialize};

pub const MAX_SELECTION: usize = 16 << 10;
const MAX_RESPONSE: usize = 8 << 20;
#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Selection {
    pub targets: Vec<Target>,
}
#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Target {
    pub address: Address,
    pub code: bool,
    pub slots: Vec<B256>,
}
impl Selection {
    pub fn normalize(&self) -> Result<Self> {
        ensure!(
            !self.targets.is_empty() && self.targets.len() <= 8,
            "selected account limit"
        );
        let mut out = self.clone();
        out.targets.sort_by_key(|t| t.address);
        ensure!(
            out.targets.windows(2).all(|w| w[0].address != w[1].address),
            "duplicate account"
        );
        let mut slots = 0;
        for target in &mut out.targets {
            slots += target.slots.len();
            ensure!(slots <= 128, "selected slot limit");
            target.slots.sort();
            ensure!(
                target.slots.windows(2).all(|w| w[0] != w[1]),
                "duplicate slot"
            );
        }
        ensure!(
            serde_json::to_vec(&out)?.len() <= MAX_SELECTION,
            "selection byte limit"
        );
        Ok(out)
    }
    pub fn parse(bytes: &[u8]) -> Result<Self> {
        ensure!(
            !bytes.is_empty() && bytes.len() <= MAX_SELECTION,
            "selection byte limit"
        );
        serde_json::from_slice::<Self>(bytes)?.normalize()
    }
}

// Production delegates to pinned Reth's native trie/history implementation.
// The same contract allows independent full-trie offline test oracles.
pub trait Source {
    fn selected_proof(&self, address: Address, slots: &[B256]) -> Result<AccountProof>;
    fn selected_code(&self, hash: B256) -> Result<Option<Bytes>>;
}
impl<T: StateProvider + ?Sized> Source for T {
    fn selected_proof(&self, address: Address, slots: &[B256]) -> Result<AccountProof> {
        Ok(StateProofProvider::proof(
            self,
            TrieInput::default(),
            address,
            slots,
        )?)
    }
    fn selected_code(&self, hash: B256) -> Result<Option<Bytes>> {
        Ok(self
            .bytecode_by_hash(&hash)?
            .map(|code| code.original_bytes()))
    }
}
#[derive(Serialize)]
struct Bundle {
    version: u32,
    selection: Selection,
    evidence: Vec<Evidence>,
}
#[derive(Serialize)]
struct Evidence {
    proof: Proof,
    #[serde(skip_serializing_if = "Option::is_none")]
    code: Option<Bytes>,
}
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct Proof {
    address: Address,
    account_proof: Vec<Bytes>,
    balance: U256,
    code_hash: B256,
    nonce: String,
    storage_hash: B256,
    storage_proof: Vec<Slot>,
}
#[derive(Serialize)]
struct Slot {
    key: B256,
    value: U256,
    proof: Vec<Bytes>,
}
fn nodes(nodes: &mut Vec<Bytes>, root: B256) -> Result<()> {
    ensure!(
        nodes.len() <= 65 && nodes.iter().all(|n| !n.is_empty() && n.len() <= 1024),
        "proof node limit"
    );
    // Reth represents an empty trie with either [] or the RLP empty string.
    // Only the proven empty root has this canonical zero-node Go encoding.
    if root == EMPTY_ROOT_HASH {
        ensure!(
            nodes.is_empty() || nodes.len() == 1 && nodes[0].as_ref() == [0x80],
            "invalid empty trie proof"
        );
        nodes.clear();
    }
    Ok(())
}
pub fn export(
    state: &(impl Source + ?Sized),
    root: B256,
    selection: &Selection,
) -> Result<Vec<u8>> {
    let selection = selection.normalize()?;
    let mut evidence = Vec::with_capacity(selection.targets.len());
    for target in &selection.targets {
        let mut native = state.selected_proof(target.address, &target.slots)?;
        ensure!(
            native.address == target.address && native.storage_proofs.len() == target.slots.len(),
            "native proof scope differs"
        );
        ensure!(
            native.info.is_some() || native.storage_root == EMPTY_ROOT_HASH,
            "absent account storage differs"
        );
        for (proof, slot) in native.storage_proofs.iter().zip(&target.slots) {
            ensure!(proof.key == *slot, "native slot order differs");
        }
        nodes(&mut native.proof, root)?;
        for proof in &mut native.storage_proofs {
            nodes(&mut proof.proof, native.storage_root)?;
        }
        native
            .verify(root)
            .map_err(|_| eyre!("native proof root differs"))?;
        let info = native.info.unwrap_or_default();
        let code_hash = info.get_bytecode_hash();
        let code = if target.code {
            let bytes = if code_hash == keccak256([]) {
                Bytes::new()
            } else {
                state
                    .selected_code(code_hash)?
                    .ok_or_else(|| eyre!("selected native code unavailable"))?
            };
            ensure!(
                bytes.len() <= 256 << 10 && keccak256(&bytes) == code_hash,
                "selected code hash/limit"
            );
            Some(bytes)
        } else {
            None
        };
        let proof = Proof {
            address: target.address,
            account_proof: native.proof,
            balance: info.balance,
            code_hash,
            nonce: format!("0x{:x}", info.nonce),
            storage_hash: native.storage_root,
            storage_proof: native
                .storage_proofs
                .into_iter()
                .map(|p| Slot {
                    key: p.key,
                    value: p.value,
                    proof: p.proof,
                })
                .collect(),
        };
        ensure!(
            serde_json::to_vec(&proof)?.len() <= 2 << 20,
            "account proof byte limit"
        );
        evidence.push(Evidence { proof, code });
    }
    let bytes = serde_json::to_vec(&Bundle {
        version: 1,
        selection,
        evidence,
    })?;
    ensure!(bytes.len() <= MAX_RESPONSE, "proof bundle limit");
    Ok(bytes)
}

pub fn read_selection(path: &std::path::Path) -> Result<Selection> {
    use std::{
        io::Read,
        os::unix::fs::{MetadataExt, OpenOptionsExt},
    };
    crate::checkpoint::private_file(path)?;
    let before = std::fs::symlink_metadata(path)?;
    let file = std::fs::OpenOptions::new()
        .read(true)
        .custom_flags(rustix::fs::OFlags::NOFOLLOW.bits() as i32)
        .open(path)?;
    let opened = file.metadata()?;
    ensure!(
        before.dev() == opened.dev()
            && before.ino() == opened.ino()
            && opened.len() <= MAX_SELECTION as u64,
        "selection file changed/limit"
    );
    let mut bytes = Vec::new();
    file.take(MAX_SELECTION as u64 + 1)
        .read_to_end(&mut bytes)?;
    crate::checkpoint::private_file(path)?;
    let after = std::fs::symlink_metadata(path)?;
    ensure!(
        before.dev() == after.dev()
            && before.ino() == after.ino()
            && before.len() == bytes.len() as u64
            && before.len() == after.len()
            && before.mtime() == after.mtime()
            && before.mtime_nsec() == after.mtime_nsec(),
        "selection file changed"
    );
    Selection::parse(&bytes)
}
