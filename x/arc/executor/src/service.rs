//! Bounded sequential stdin/stdout protocol. Errors terminate the adapter.
use eyre::{Result, ensure};
use serde::{Deserialize, Serialize};
use std::io::{Read, Write};

#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
#[serde(rename_all = "lowercase")]
#[repr(u8)]
pub enum Operation {
    Identity = 1,
    Head = 2,
    Canonical = 3,
    Replay = 4,
    ReplayProofs = 5,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    pub version: u8,
    pub id: u64,
    pub op: Operation,
    pub height: u64,
    pub generation: u64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub state_proofs: Option<crate::proofs::Selection>,
}

/// Managed response: separate bounded proof metadata and the unchanged payload.
pub fn serve_managed(
    input: impl Read,
    output: impl Write,
    mut handler: impl FnMut(Request) -> Result<(Vec<u8>, Vec<u8>)>,
) -> Result<()> {
    serve_inner(input, output, true, |r| {
        handler(r).map(|(proof, payload)| (Some(proof), payload))
    })
}

pub fn serve(
    input: impl Read,
    output: impl Write,
    mut handler: impl FnMut(Request) -> Result<Vec<u8>>,
) -> Result<()> {
    serve_inner(input, output, false, |r| handler(r).map(|p| (None, p)))
}
fn serve_inner(
    mut input: impl Read,
    mut output: impl Write,
    managed: bool,
    mut handler: impl FnMut(Request) -> Result<(Option<Vec<u8>>, Vec<u8>)>,
) -> Result<()> {
    let mut expected = 1_u64;
    loop {
        let mut length = [0_u8; 4];
        if input.read(&mut length[..1])? == 0 {
            return Ok(());
        }
        input.read_exact(&mut length[1..])?;
        let size = u32::from_be_bytes(length) as usize;
        ensure!(
            (1..=crate::proofs::MAX_SELECTION).contains(&size),
            "request length exceeds limit"
        );
        let mut bytes = vec![0; size];
        input.read_exact(&mut bytes)?;
        let request: Request = serde_json::from_slice(&bytes)?;
        ensure!(
            request.version == 1
                && request.id == expected
                && request.height <= i64::MAX as u64
                && request.generation <= i64::MAX as u64,
            "invalid version/id/range"
        );
        if matches!(request.op, Operation::ReplayProofs) {
            request
                .state_proofs
                .as_ref()
                .ok_or_else(|| eyre::eyre!("selection required"))?
                .normalize()?;
        } else {
            ensure!(
                size <= 1024 && request.state_proofs.is_none(),
                "unexpected selection/legacy size"
            );
        }
        match request.op {
            Operation::Identity | Operation::Head => ensure!(
                request.height == 0 && request.generation == 0,
                "unexpected identity/head arguments"
            ),
            Operation::Canonical => {
                ensure!(request.generation == 0, "unexpected canonical generation")
            }
            Operation::Replay | Operation::ReplayProofs => ensure!(
                request.height > 0 && request.generation > 0,
                "replay requires height/generation"
            ),
        }
        let op = request.op;
        let id = request.id;
        let (proof, payload) = handler(request)?;
        let limit = if matches!(op, Operation::Replay | Operation::ReplayProofs) {
            64 << 20
        } else {
            1024
        };
        ensure!(
            !payload.is_empty() && payload.len() <= limit,
            "response exceeds limit"
        );
        if matches!(op, Operation::Replay | Operation::ReplayProofs) {
            ensure!(
                payload.starts_with(b"EXR1"),
                "replay response requires verified EXR1"
            );
        }
        ensure!(
            proof
                .as_ref()
                .is_some_and(|p| !p.is_empty() && p.len() <= crate::native_import::MAX_META)
                == managed,
            "managed proof length exceeds limit"
        );
        output.write_all(if managed { b"AXM1" } else { b"AXS1" })?;
        output.write_all(&id.to_be_bytes())?;
        output.write_all(&[op as u8])?;
        if let Some(proof) = &proof {
            output.write_all(&(proof.len() as u32).to_be_bytes())?;
        }
        output.write_all(&(payload.len() as u32).to_be_bytes())?;
        if let Some(proof) = &proof {
            output.write_all(proof)?;
        }
        output.write_all(&payload)?;
        output.flush()?;
        expected = expected
            .checked_add(1)
            .ok_or_else(|| eyre::eyre!("request sequence overflow"))?;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;

    fn request(id: u64, op: Operation, height: u64, generation: u64) -> Vec<u8> {
        let json = serde_json::to_vec(&Request {
            version: 1,
            id,
            op,
            height,
            generation,
            state_proofs: None,
        })
        .unwrap();
        let mut bytes = (json.len() as u32).to_be_bytes().to_vec();
        bytes.extend(json);
        bytes
    }

    #[test]
    fn managed_service_frames_proof_separately_and_rejects_oversize_before_output() -> Result<()> {
        let mut input = request(1, Operation::Head, 0, 0);
        input.extend(request(2, Operation::Replay, 1, 7));
        let mut output = vec![];
        let proof = br#"{"Version":1}"#.to_vec();
        serve_managed(Cursor::new(input), &mut output, |r| {
            Ok((
                proof.clone(),
                if matches!(r.op, Operation::Replay) {
                    b"EXR1verified".to_vec()
                } else {
                    br#"{"height":1}"#.to_vec()
                },
            ))
        })?;
        let mut bytes = output.as_slice();
        for (id, op) in [(1_u64, 2_u8), (2, 4)] {
            let mut h = [0; 21];
            bytes.read_exact(&mut h)?;
            assert_eq!(
                &h[..4],
                b"AXM1",
                "managed mode must never downgrade to legacy framing"
            );
            assert_eq!(u64::from_be_bytes(h[4..12].try_into()?), id);
            assert_eq!(h[12], op);
            let m = u32::from_be_bytes(h[13..17].try_into()?) as usize;
            let p = u32::from_be_bytes(h[17..21].try_into()?) as usize;
            assert_eq!(&bytes[..m], proof);
            bytes = &bytes[m + p..];
        }
        assert!(bytes.is_empty());
        for proof in [vec![], vec![1; (16 << 10) + 1]] {
            let mut output = vec![];
            assert!(
                serve_managed(
                    Cursor::new(request(1, Operation::Head, 0, 0)),
                    &mut output,
                    |_| Ok((proof.clone(), vec![1]))
                )
                .is_err()
            );
            assert!(output.is_empty());
        }
        Ok(())
    }

    #[test]
    fn service_frames_ordered_responses_and_rejects_invalid_requests_before_handler() -> Result<()>
    {
        let mut input = request(1, Operation::Identity, 0, 0);
        input.extend(request(2, Operation::Replay, 7, 3));
        let mut out = Vec::new();
        serve(Cursor::new(input), &mut out, |r| {
            Ok(match r.op {
                Operation::Identity => br#"{"chain_id":"5042002"}"#.to_vec(),
                Operation::Replay => {
                    assert_eq!((r.height, r.generation), (7, 3));
                    b"EXR1verified".to_vec()
                }
                _ => unreachable!(),
            })
        })?;
        assert_eq!(&out[..4], b"AXS1");
        assert_eq!(u64::from_be_bytes(out[4..12].try_into()?), 1);
        assert_eq!(out[12], 1);
        let size = u32::from_be_bytes(out[13..17].try_into()?) as usize;
        assert_eq!(&out[17 + size..21 + size], b"AXS1");
        assert_eq!(out[29 + size], 4);

        for input in [
            request(2, Operation::Head, 0, 0),
            request(1, Operation::Replay, 0, 1),
            request(1, Operation::Replay, 1, 0),
            request(1, Operation::Head, 1, 0),
            request(1, Operation::Canonical, u64::MAX, 0),
            1025_u32.to_be_bytes().to_vec(),
            vec![0, 0],
        ] {
            let mut called = false;
            let mut out = Vec::new();
            assert!(
                serve(Cursor::new(input), &mut out, |_| {
                    called = true;
                    Ok(vec![])
                })
                .is_err()
            );
            assert!(!called && out.is_empty());
        }
        let mut out = Vec::new();
        assert!(
            serve(
                Cursor::new(request(1, Operation::Replay, 1, 1)),
                &mut out,
                |_| Ok(b"invalid".to_vec())
            )
            .is_err()
        );
        assert!(out.is_empty());
        Ok(())
    }
    #[test]
    fn proof_operation_requires_selection_and_keeps_legacy_scope_bounded() -> Result<()> {
        let selection = crate::proofs::Selection {
            targets: vec![crate::proofs::Target {
                address: alloy_primitives::Address::repeat_byte(1),
                code: true,
                slots: vec![alloy_primitives::B256::ZERO],
            }],
        };
        let framed = |r: &Request| -> Result<Vec<u8>> {
            let p = serde_json::to_vec(r)?;
            let mut b = (p.len() as u32).to_be_bytes().to_vec();
            b.extend(p);
            Ok(b)
        };
        let mut r = Request {
            version: 1,
            id: 1,
            op: Operation::ReplayProofs,
            height: 1,
            generation: 1,
            state_proofs: Some(selection),
        };
        let mut out = vec![];
        serve(Cursor::new(framed(&r)?), &mut out, |r| {
            assert!(r.state_proofs.is_some());
            Ok(b"EXR1verified".to_vec())
        })?;
        assert_eq!(out[12], 5);
        for op in [
            Operation::Identity,
            Operation::Head,
            Operation::Canonical,
            Operation::Replay,
        ] {
            r.op = op;
            let mut called = false;
            assert!(
                serve(Cursor::new(framed(&r)?), vec![], |_| {
                    called = true;
                    Ok(vec![1])
                })
                .is_err()
            );
            assert!(!called);
        }
        r.op = Operation::ReplayProofs;
        r.state_proofs = None;
        assert!(
            serve(Cursor::new(framed(&r)?), vec![], |_| panic!(
                "missing proof reached handler"
            ))
            .is_err()
        );
        Ok(())
    }
}
