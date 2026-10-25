//! Bounded call-only observer; never stores opcode/memory/stack snapshots.
use crate::wire::{CallFrame, CallTraces};
use alloy_primitives::{Address, B256, U256};
use eyre::{Result, ensure};
use revm::{
    context::ContextTr,
    inspector::Inspector,
    interpreter::{
        CallInputs, CallOutcome, CallScheme, CreateInputs, CreateOutcome, Interpreter,
        interpreter_types::{Jumps, LoopControl},
    },
};
use std::{
    collections::BTreeMap,
    sync::{Arc, Mutex},
};

const MAX_FRAMES: usize = 4096;
const MAX_DEPTH: usize = 64;
const MAX_FIELD: usize = 64 << 10;
const MAX_BYTES: usize = 4 << 20;
// Pinned Arc transparent subcall completion runs after logical call_end.
const CALL_FROM: Address = alloy_primitives::address!("1800000000000000000000000000000000000003");

#[derive(Default)]
pub struct Calls {
    frames: Vec<CallFrame>,
    stack: Vec<usize>,
    bytes: usize,
    failed: bool,
    active: bool,
    index: u32,
    hash: B256,
    root: usize,
    pending_subcalls: BTreeMap<usize, usize>,
    max_depth: usize,
}
impl Calls {
    pub fn begin(&mut self, index: u32, hash: B256) -> Result<()> {
        ensure!(
            !self.active && self.stack.is_empty() && !self.failed,
            "call observer boundary"
        );
        self.active = true;
        self.index = index;
        self.hash = hash;
        self.root = self.frames.len();
        self.max_depth = 0;
        Ok(())
    }
    pub fn end(&mut self, gas: u64, used: u64, success: bool) -> Result<()> {
        ensure!(
            !self.failed && self.active && self.stack.is_empty(),
            "call observer incomplete/limit"
        );
        ensure!(
            self.frames.len() > self.root,
            "missing native transaction root"
        );
        let root = &mut self.frames[self.root];
        ensure!(
            root.parent == -1 && root.error.is_empty() == success,
            "call root status"
        );
        root.gas = gas;
        root.gas_used = used;
        self.active = false;
        Ok(())
    }
    pub fn finish_root(
        &mut self,
        mut expected: CallFrame,
        output: &[u8],
        error: String,
    ) -> Result<()> {
        ensure!(
            !self.failed && self.active && self.stack.is_empty() && self.frames.len() > self.root,
            "call observer root boundary"
        );
        ensure!(
            output.len() <= MAX_FIELD && error.len() <= 4096,
            "call root field limit"
        );
        let root = &self.frames[self.root];
        let different = root.r#type != expected.r#type
            || root.from != expected.from
            || root.value != expected.value
            || root.input != expected.input
            || expected.r#type == "CALL" && root.to != expected.to;
        if different {
            // Preserve both the transaction root and Arc's logical CallFrom
            // child. Do not rewrite an observed child into a physical call.
            ensure!(self.frames.len() < MAX_FRAMES, "call root frame limit");
            ensure!(self.max_depth < MAX_DEPTH, "wrapped call root depth limit");
            let size = expected.input.len() + output.len() + error.len();
            ensure!(self.bytes + size <= MAX_BYTES, "call root byte limit");
            expected.transaction_index = self.index;
            expected.transaction_hash = self.hash;
            expected.output = output.to_vec();
            expected.error = error;
            for f in self.frames.iter_mut().skip(self.root + 1) {
                if f.parent >= self.root as i32 {
                    f.parent += 1;
                }
            }
            self.frames[self.root].parent = self.root as i32;
            self.frames.insert(self.root, expected);
            self.bytes += size;
        } else {
            let size =
                self.bytes - root.output.len() - root.error.len() + output.len() + error.len();
            ensure!(size <= MAX_BYTES, "call root byte limit");
            let root = &mut self.frames[self.root];
            root.output = output.to_vec();
            if !error.is_empty() && (root.r#type == "CREATE" || root.r#type == "CREATE2") {
                root.to = None;
            }
            root.error = error;
            self.bytes = size;
        }
        Ok(())
    }
    pub fn take(&mut self) -> Result<CallTraces> {
        ensure!(
            !self.active && !self.failed && self.stack.is_empty(),
            "call observer unfinished"
        );
        Ok(CallTraces {
            version: 1,
            profile: "arc".into(),
            frames: std::mem::take(&mut self.frames),
        })
    }
    fn push(&mut self, mut f: CallFrame, leaf: bool) -> bool {
        if self.failed
            || self.frames.len() >= MAX_FRAMES
            || self.stack.len() > MAX_DEPTH
            || f.input.len() > MAX_FIELD
            || self.bytes + f.input.len() > MAX_BYTES
        {
            self.failed = true;
            return false;
        }
        f.transaction_index = self.index;
        f.transaction_hash = self.hash;
        f.parent = self.stack.last().map_or(-1, |p| *p as i32);
        self.max_depth = self.max_depth.max(self.stack.len());
        self.bytes += f.input.len();
        let n = self.frames.len();
        self.frames.push(f);
        if !leaf {
            self.stack.push(n);
        }
        true
    }
    fn end_frame(
        &mut self,
        output: &[u8],
        limit: u64,
        gas: u64,
        error: String,
        address: Option<Address>,
    ) {
        if self.failed {
            return;
        }
        let Some(n) = self.stack.pop() else {
            self.failed = true;
            return;
        };
        self.pending_subcalls.remove(&n);
        if output.len() > MAX_FIELD
            || error.len() > 4096
            || self.bytes + output.len() + error.len() > MAX_BYTES
        {
            self.failed = true;
            return;
        }
        self.bytes += output.len() + error.len();
        let f = &mut self.frames[n];
        f.output = output.to_vec();
        f.gas = limit;
        f.gas_used = gas;
        if f.r#type == "CREATE" || f.r#type == "CREATE2" {
            f.to = address;
        }
        f.error = error;
    }
}
pub fn root_frame(
    from: Address,
    to: Option<Address>,
    value: U256,
    gas: u64,
    input: &[u8],
) -> Result<CallFrame> {
    ensure!(input.len() <= MAX_FIELD, "call root input limit");
    Ok(frame(
        if to.is_some() { "CALL" } else { "CREATE" },
        from,
        to,
        Some(value),
        gas,
        input.to_vec(),
    ))
}
fn frame(
    kind: &str,
    from: Address,
    to: Option<Address>,
    value: Option<U256>,
    gas: u64,
    input: Vec<u8>,
) -> CallFrame {
    CallFrame {
        transaction_index: 0,
        transaction_hash: B256::ZERO,
        parent: -1,
        r#type: kind.into(),
        from,
        to,
        value: value.map(B256::from),
        gas,
        gas_used: 0,
        input,
        output: vec![],
        error: String::new(),
    }
}

#[derive(Clone, Default)]
pub struct Observer(pub Arc<Mutex<Calls>>, bool);
impl<CTX: ContextTr> Inspector<CTX> for Observer {
    fn step(&mut self, interp: &mut Interpreter, _: &mut CTX) {
        let subcall = !interp.bytecode.is_end()
            && matches!(interp.bytecode.opcode(), 0xf1 | 0xf2 | 0xf4 | 0xfa)
            && interp
                .stack
                .peek(1)
                .is_ok_and(|to| Address::from_word(B256::from(to)) == CALL_FROM);
        // Ordinary opcode observation needs no mutex; only a CallFrom or an
        // outstanding completion enters the recorder's shared state.
        if !self.1 && !subcall {
            return;
        }
        let Ok(mut c) = self.0.lock() else { return };
        if !c.active || c.failed {
            self.1 = false;
            return;
        }
        let Some(parent) = c.stack.last().copied() else {
            self.1 = false;
            return;
        };
        if let Some(child) = c.pending_subcalls.remove(&parent)
            && child < c.frames.len()
            && c.frames[child].parent == parent as i32
        {
            match interp.stack.peek(0) {
                Ok(flag) if flag.is_zero() && c.frames[child].error.is_empty() => {
                    let error = "ArcCallFromCompletionReverted";
                    if c.bytes + error.len() > MAX_BYTES {
                        c.failed = true;
                        return;
                    }
                    c.frames[child].error = error.into();
                    c.bytes += error.len();
                }
                Err(_) => {
                    c.failed = true;
                    return;
                }
                _ => {}
            }
        }
        if subcall {
            let next = c.frames.len();
            c.pending_subcalls.insert(parent, next);
        }
        self.1 = !c.pending_subcalls.is_empty();
    }
    fn call(&mut self, ctx: &mut CTX, i: &mut CallInputs) -> Option<CallOutcome> {
        let Ok(mut c) = self.0.lock() else {
            return Some(CallOutcome::new_oog(
                i.gas_limit,
                i.return_memory_offset.clone(),
            ));
        };
        if !c.active {
            return None;
        }
        if i.input.len() > MAX_FIELD {
            c.failed = true;
        }
        let (kind, from, value) = match i.scheme {
            CallScheme::Call => ("CALL", i.caller, Some(i.value.get())),
            CallScheme::CallCode => ("CALLCODE", i.target_address, Some(i.value.get())),
            CallScheme::DelegateCall => ("DELEGATECALL", i.target_address, Some(i.value.get())),
            CallScheme::StaticCall => ("STATICCALL", i.caller, None),
        };
        if c.failed
            || !c.push(
                frame(
                    kind,
                    from,
                    Some(i.bytecode_address),
                    value,
                    i.gas_limit,
                    i.input.as_bytes(ctx).to_vec(),
                ),
                false,
            )
        {
            return Some(CallOutcome::new_oog(
                i.gas_limit,
                i.return_memory_offset.clone(),
            ));
        }
        None
    }
    fn call_end(&mut self, _: &mut CTX, _: &CallInputs, out: &mut CallOutcome) {
        if let Ok(mut c) = self.0.lock()
            && c.active
        {
            let error = if out.result.result.is_ok() {
                String::new()
            } else {
                format!("{:?}", out.result.result)
            };
            c.end_frame(
                out.output(),
                out.gas().limit(),
                out.gas().total_gas_spent(),
                error,
                None,
            );
        }
    }
    fn create(&mut self, _: &mut CTX, i: &mut CreateInputs) -> Option<CreateOutcome> {
        let Ok(mut c) = self.0.lock() else {
            return Some(CreateOutcome::new_oog(i.gas_limit()));
        };
        if !c.active {
            return None;
        }
        let kind = match i.scheme() {
            revm::context_interface::CreateScheme::Create => "CREATE",
            revm::context_interface::CreateScheme::Create2 { .. } => "CREATE2",
            _ => {
                c.failed = true;
                return Some(CreateOutcome::new_oog(i.gas_limit()));
            }
        };
        if i.init_code().len() > MAX_FIELD {
            c.failed = true;
        }
        if c.failed
            || !c.push(
                frame(
                    kind,
                    i.caller(),
                    None,
                    Some(i.value()),
                    i.gas_limit(),
                    i.init_code().to_vec(),
                ),
                false,
            )
        {
            return Some(CreateOutcome::new_oog(i.gas_limit()));
        }
        None
    }
    fn create_end(&mut self, _: &mut CTX, _: &CreateInputs, out: &mut CreateOutcome) {
        if let Ok(mut c) = self.0.lock()
            && c.active
        {
            let error = if out.result.result.is_ok() {
                String::new()
            } else {
                format!("{:?}", out.result.result)
            };
            c.end_frame(
                out.output(),
                out.gas().limit(),
                out.gas().total_gas_spent(),
                error,
                out.address,
            );
        }
    }
    fn selfdestruct(&mut self, from: Address, to: Address, value: U256) {
        if let Ok(mut c) = self.0.lock()
            && c.active
        {
            if c.stack.is_empty() {
                c.failed = true;
                return;
            }
            c.push(
                frame("SELFDESTRUCT", from, Some(to), Some(value), 0, vec![]),
                true,
            );
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn observer_limits_never_return_partial_success() -> Result<()> {
        for kind in ["field", "frames", "depth", "bytes"] {
            let mut c = Calls::default();
            c.begin(0, B256::repeat_byte(1))?;
            match kind {
                "field" => {
                    c.push(
                        frame(
                            "CALL",
                            Address::ZERO,
                            Some(Address::ZERO),
                            Some(U256::ZERO),
                            1,
                            vec![0; MAX_FIELD + 1],
                        ),
                        false,
                    );
                }
                "frames" => {
                    for _ in 0..=MAX_FRAMES {
                        c.push(
                            frame(
                                "CALL",
                                Address::ZERO,
                                Some(Address::ZERO),
                                Some(U256::ZERO),
                                1,
                                vec![],
                            ),
                            true,
                        );
                    }
                }
                "depth" => {
                    for _ in 0..MAX_DEPTH + 2 {
                        c.push(
                            frame(
                                "CALL",
                                Address::ZERO,
                                Some(Address::ZERO),
                                Some(U256::ZERO),
                                1,
                                vec![],
                            ),
                            false,
                        );
                    }
                }
                "bytes" => {
                    for _ in 0..66 {
                        c.push(
                            frame(
                                "CALL",
                                Address::ZERO,
                                Some(Address::ZERO),
                                Some(U256::ZERO),
                                1,
                                vec![0; MAX_FIELD],
                            ),
                            true,
                        );
                    }
                }
                _ => unreachable!(),
            }
            assert!(c.failed);
            assert!(c.end(1, 1, true).is_err());
            assert!(c.take().is_err());
        }
        Ok(())
    }
    #[test]
    fn wrapped_transaction_root_still_obeys_depth_limit() -> Result<()> {
        let mut c = Calls::default();
        c.begin(0, B256::repeat_byte(1))?;
        for _ in 0..=MAX_DEPTH {
            assert!(c.push(
                frame(
                    "CALL",
                    Address::ZERO,
                    Some(Address::ZERO),
                    Some(U256::ZERO),
                    1,
                    vec![]
                ),
                false
            ));
        }
        c.stack.clear();
        let physical = frame(
            "CALL",
            Address::repeat_byte(1),
            Some(Address::ZERO),
            Some(U256::ZERO),
            1,
            vec![],
        );
        assert!(c.finish_root(physical, &[], String::new()).is_err());
        assert!(c.take().is_err());
        Ok(())
    }
}
