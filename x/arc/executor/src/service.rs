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
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    pub version: u8,
    pub id: u64,
    pub op: Operation,
    pub height: u64,
    pub generation: u64,
}

pub fn serve(
    mut input: impl Read,
    mut output: impl Write,
    mut handler: impl FnMut(Request) -> Result<Vec<u8>>,
) -> Result<()> {
    let mut expected = 1_u64;
    loop {
        let mut length = [0_u8; 4];
        if input.read(&mut length[..1])? == 0 {
            return Ok(());
        }
        input.read_exact(&mut length[1..])?;
        let size = u32::from_be_bytes(length) as usize;
        ensure!((1..=1024).contains(&size), "request length exceeds limit");
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
        match request.op {
            Operation::Identity | Operation::Head => ensure!(
                request.height == 0 && request.generation == 0,
                "unexpected identity/head arguments"
            ),
            Operation::Canonical => {
                ensure!(request.generation == 0, "unexpected canonical generation")
            }
            Operation::Replay => ensure!(
                request.height > 0 && request.generation > 0,
                "replay requires height/generation"
            ),
        }
        let op = request.op;
        let id = request.id;
        let payload = handler(request)?;
        let limit = if matches!(op, Operation::Replay) {
            64 << 20
        } else {
            1024
        };
        ensure!(
            !payload.is_empty() && payload.len() <= limit,
            "response exceeds limit"
        );
        if matches!(op, Operation::Replay) {
            ensure!(
                payload.starts_with(b"EXR1"),
                "replay response requires verified EXR1"
            );
        }
        output.write_all(b"AXS1")?;
        output.write_all(&id.to_be_bytes())?;
        output.write_all(&[op as u8])?;
        output.write_all(&(payload.len() as u32).to_be_bytes())?;
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
        })
        .unwrap();
        let mut bytes = (json.len() as u32).to_be_bytes().to_vec();
        bytes.extend(json);
        bytes
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
}
