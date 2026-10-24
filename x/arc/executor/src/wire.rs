//! Exact Go execwire metadata ordering and hex/base64 representation.
use alloy_primitives::{Address, B256};
use serde::{Deserialize, Serialize};

pub const TRANSACTION: u8 = 0;
pub const FINALIZE: u8 = 1;
pub const PRE_BLOCK: u8 = 4;

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct Ref {
    pub generation: u64,
    pub height: u64,
    pub hash: B256,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct Account {
    pub exists: bool,
    pub nonce: u64,
    pub balance: B256,
    pub code_hash: B256,
    pub storage_root: B256,
    pub extra: u64,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct AccountChange {
    pub address: Address,
    pub before: Account,
    pub after: Account,
    pub baseline: bool,
    pub storage_cleared: bool,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct StorageChange {
    pub address: Address,
    pub slot: B256,
    pub before: B256,
    pub after: B256,
    pub baseline: bool,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct Code {
    pub hash: B256,
    #[serde(serialize_with = "base64_bytes")]
    pub bytes: Vec<u8>,
}

fn base64_bytes<S: serde::Serializer>(bytes: &[u8], s: S) -> std::result::Result<S::Ok, S::Error> {
    use base64::Engine;
    s.serialize_str(&base64::engine::general_purpose::STANDARD.encode(bytes))
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct Step {
    pub ordinal: u32,
    pub cause: u8,
    pub tx_index: i32,
    pub tx_hash: B256,
    pub accounts: Vec<AccountChange>,
    pub storage: Vec<StorageChange>,
    pub code: Vec<Code>,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "PascalCase")]
pub struct Result {
    pub r#ref: Ref,
    pub parent: B256,
    pub time: u64,
    pub state_root: B256,
    pub receipts_root: B256,
    pub tx_count: u32,
    #[serde(skip)]
    pub block_rlp: Vec<u8>,
    #[serde(skip)]
    pub receipts_rlp: Vec<u8>,
    pub steps: Vec<Step>,
}
