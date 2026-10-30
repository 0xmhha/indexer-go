//! Arc-specific second-pass execution. No node services are started here.

/// The source identity is part of the adapter's compatibility boundary.
pub const ARC_REVISION: &str = "6e764023ee6515fe70573e123ed2db912a7207b4";

mod calls;
mod journal;
mod output;
mod replay;
pub mod wire;
pub use output::write_result;
pub use replay::{ReplayState, Verified, replay};
pub mod catalog;
pub mod checkpoint;
pub mod service;

#[cfg(test)]
mod tests;

pub mod bootstrap;
pub mod history;
pub mod managed_read;
pub mod native;
pub mod native_epoch;
pub mod native_import;
pub mod native_writer;

pub mod retention;

pub mod proofs;
