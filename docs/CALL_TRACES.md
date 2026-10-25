# 호출 trace 수집과 DB 조회

이 문서는 **RPC window-feed** trace 경로다. Arc·StableNet의 두 번째 native 실행에서 얻는 호출과 internal call은 [native 실행 trace](EXECUTION_CALL_TRACES.md)를 사용한다. 두 경로의 provenance·저장·완료점을 혼합하지 않는다.

window feed에 선택 `call_traces` worker를 연결했다. 이 worker는 저장된 블록의 정확한 hash로 RPC의 호출 trace를 얻고, 거래·receipt와 최상위 호출을 대조한 뒤 원천 DB에 보관한다. 이후 조회와 후처리는 DB 입력만 사용한다. 현재 Arc 모듈에는 디코더와 앱 연결을 제공하지만, 실제 공개 Arc RPC의 debug 활성화·tracer 형식·부모 상태 보존은 운영 환경에서 확인해야 한다.

## 자료의 의미와 검증 범위

입력은 [Geth callTracer](https://geth.ethereum.org/docs/developers/evm-tracing/built-in-tracers)의 거래별 호출 트리다. 각 frame의 호출 유형·주소·값·gas·input/output·오류를 보관한다. `Path: []`는 거래의 최상위 호출, `[0, 1]`은 첫 하위 호출의 두 번째 자식이다. 거래 순서와 각 자식 순서를 유지한 preorder로 페이지를 만든다. 최상위 호출도 포함하며 깊이는 path 길이다.

검증은 다음 두 범위를 구분한다.

- 저장된 header/body/receipt를 profile과 commitment로 다시 검증한다. trace 응답의 거래 수·순서·txHash, 최상위 from/to/value/input/gas/gasUsed·성공 여부, 생성 주소를 그 자료와 대조한다. 상태 대신 post-state root가 들어 있는 과거 receipt와 opaque 거래는 이 형식에서 거부한다.
- 하위 호출은 provider가 보고한 정보다. 부모가 revert하면 `AncestorReverted`와 `Reverted`에 표시하고 시도한 호출도 보존한다. `DELEGATECALL`의 value, `SELFDESTRUCT`, 생성 실패를 임의 잔액 이동으로 바꾸지 않는다. 값이 없으면 null이며 확인한0과 구분한다.

receipt/state root는 하위 호출 목록의 암호학적 증명이 아니다. provider가 그럴듯한 하위 호출을 누락하거나 다르게 보고했는지는 자체 EVM 실행 없이 증명할 수 없다. 결과의 `verification`은 `provider_reported_root_consistency`, `coverage`는 `reported_call_tree`다. 블록 전체 상태 변화·시스템 호출·native USDC 회계·포괄 internal transaction 완료를 뜻하지 않는다. [고정 Geth tracer 소스](https://github.com/ethereum/go-ethereum/blob/v1.16.5/eth/tracers/native/call.go)를 독립 형식 표본의 근거로 사용했다.

기존 `InternalTransaction` 테이블과 잔액/토큰 기능을 이 입력으로 자동 채우지 않는다. 기존 모델에는 호출 경로·부모 revert·이 provenance가 모두 담기지 않는다. base capture의 `datasets.traces`는 계속 unavailable이며 별도 `callTraces` 완료점만 표시한다.

## 선택 설정

[기존 앱 feed 설정](FEED_APP.md)의 `source.feed` 또는 per-chain `feed` 아래에 넣는다.

```yaml
call_traces:
  name: calls-v1
  provider: arc-public-pool  # URL/키가 아닌 운영자가 정한 RPC pool label
  start_height: 67000001    # source anchor보다 큰 명시적 시작 높이
  poll_interval: 50ms       # 기본50ms, 허용20ms..5s
```

이 항목이 없으면 worker는 실행하지 않는다. 운영 config.yaml은 변경하지 않았다. provider/name·시작 높이·format/profile은 stream 정의에 고정한다. provider와 profile은 짧은 label만 허용하고 URL/자격 증명을 넣지 않는다. profile은 모듈이 정하며 Arc는 `arc`다. proof와 trace stream은 서로 다른 이름을 사용한다. 같은 stream 이름으로 정의를 바꾸면 거부한다. 알 수 없는 필드와 `required`/`parent_checkpoint` 옵션은 지원하지 않는다.

RPC가 지원하지 않거나 과거 부모 상태가 없어 실패하면 해당 높이를 건너뛰지 않고 앱 task 집합을 취소/join한다. 빈 trace는 실제 거래0개인 블록에 대해 응답 `[]`를 검증한 경우에만 인정한다. 공개 provider가 이 기능을 제공한다고 추측하여 켜지 않는다.

## 수집과 영구 저장

worker는 canonical capture prefix를 durable 큐로 읽고 한 블록만 활성화한다. 네트워크 intake·base 검증/기존 writer·state proof·상태 후처리·요약 worker와 별도 task로 실행한다. epoch 변경은 기존 source supervisor의 reset을 기다린 뒤 자기 cursor를 재조정한다. source reorg 전 입력은 새 canonical 결과로 제공하지 않는다.

[debug_traceBlockByHash](https://github.com/ethereum/go-ethereum/blob/v1.16.5/eth/tracers/api.go)를 기존 chain client의 한 batch member로 요청한다. tracer는 고정 `callTracer`, `onlyTopCall:false`, `withLog:false`, `reexec:0`, timeout5s이며 임의 JS tracer/state override/latest/높이 fallback은 없다. RPC 요청 전체의 context 한도는15초다. provider의5s 해석과 별개로 indexer가 기다리는 시간을 제한한다.

신규 입력은 trace batch1회와 source commit의 fresh canonical1회가 필요하다. 실제 물리 HTTP 예약·failover는 기존 chain-wide5 HTTP/s pool을 공유한다. 기존 proof/capture/projection/요약의 요청도 이 예산을 사용한다. caller가 별도 client를 만들면 이 계약 밖이다. 450ms·2블록/s 추종이나 provider의 논리 method quota는 보장하지 않는다.

codec `evm.call-traces.geth.v1`의 immutable supplement는 exact capture identity·provider/format/profile·응답 result bytes를 보관한다. bundle·입력 receipt·연속 cursor·자체 input event를 같은 durable transaction에 기록하고 fresh 검사1회를 유지한다. 원본 result bytes는 base64로 보존하며 frame들은 조회 때 다시 검증·생성한다. SQL/table은 추가하지 않고 기존 bound-value capture 저장을 사용한다.

동일 input 재시도는 저장한 bytes를 재검증하고 fresh1회만 수행한다. 재시작은 마지막 완료 입력을 DB에서 검증한 뒤 다음 높이로 간다. 이미 확보한 trace는 provider가 과거 입력을 주지 않아도 조회할 수 있다. 아직 확보하지 못한 trace의 부모 상태가 원격에서 사라지면 디스크의 body/receipt만으로 복구할 수 없다.

이 supplement는 선택 입력이며 base plan·Through·기존 feature undo/outbox·raw ACK나 raw 삭제 권한을 변경하지 않는다. required proof/finality/최소24시간/lease gate는 그대로다. 포괄 trace를 원본 삭제의 필수 조건으로 선언하는 기능은 이 옵션에 포함하지 않는다.

## DB 전용 조회와 페이지 고정

`GET /indexing/status`의 `callTraces`에 exact descriptor·epoch·nullable 완료점·backlog·sequence와 위 검증/범위를 표시한다. source 상태와 같은 DB snapshot이며 multi-MiB trace bytes를 매 status 조회마다 읽지 않는다. 실제 소비와 재시작은 bytes를 다시 검증한다.

```text
GET /indexing/traces/blocks/{height}?limit=50
```

응답은 `epoch`, exact input `identity`, 수집 정의와 `selection` hash, `total`, `frames`, `next`를 반환한다. limit은1..256이고 `next`는 다음 frame의 offset이다. root도 하나의 frame이다. 다음 페이지는 첫 페이지의 세 값을 함께 고정한다.

```text
GET /indexing/traces/blocks/{height}?after=<next>&limit=50&epoch=<epoch>&hash=<identity.block.hash>&selection=<selection>
```

`after>0`인데 고정값이 없으면400, epoch/hash/selection이 바뀌면409다. 첫 페이지에도 고정값을 지정할 수 있다. client는409에서 해당 결과의 첫 페이지부터 다시 읽는다. source anchor/plan도 descriptor와 대조한다. 현재 완료 범위 밖은404, 기능 미설정은503, 완료를 주장한 입력의 누락/손상/I/O는500이며 내부 오류를 응답에 노출하지 않는다. 순차 페이지를 다른 fork/provider 정의와 섞지 않는다.

API는 read-only·no-store·RPC0이며 chain mount는 `/chains/{id}`다. 한 chain의 trace route는 동시에2개만 처리하고 초과 요청은429 `call_trace_busy`다. 실패·취소에서도 슬롯을 반환한다. 조회는 검증을 위해 bounded trace/source bytes를 읽으므로 일반 status보다 무겁다. 전역 주소/거래 검색과 별도 관계형 색인은 후속 작업이다.

라이브러리의 `evmtrace.ReadSnapshot/ReadPage`는 captured reader·profile·epoch·exact input·descriptor를 받는다. `capture.NewReader`는 DB/영구 객체 읽기만 제공하며 RPC나 write/ACK/삭제 메서드가 없다. 영구 객체는 호출자가 명시적으로 빌려 주며 누락 시 window/RPC로 대신하지 않는다. 현재 앱/API feed 구성은 inline source 데이터를 사용한다. `facts.Source.CallTraces`도 exact 정의의 DB-only page를 제공한다. Arc 라이브러리 factory는 `arcfeed.NewCallTraceSupplier`다.

## 한도와 계측

| 항목 | 한도 |
| --- | --- |
| 한 RPC result JSON | 4MiB |
| 입력 거래 | 4096개 |
| 전체 root 포함 frame | 16384개 |
| path 깊이 | 64 |
| frame input/output 각각 | 64KiB |
| error/revertReason 각각 | 4096bytes |
| 영구 supplement | 기존8MiB |

RPC pool의 물리 응답 body는 기존 최대64MiB를 먼저 적용하고, result byte 한도와 JSON 깊이/배열·object·토큰·중복 필드를 확인한 뒤 재귀 frame slice를 디코딩한다. 알 수 없는 opcode/필드·누락 root·중복 JSON key·잘못된 수치·한도 초과를 거부하고 부분 완료로 저장하지 않는다. 더 큰 블록/trace를 지원하려면 codec·객체/인덱스 설계를 확장해야 하며 잘라서 완료 처리하지 않는다.

선택 telemetry에는 `traceAcquire`, `traceBatch`, `traceVerify`, `traceCommit`을 추가했다. HTTP/limiter 대기는 기존 공용 계측에 포함한다. operation들은 중첩되므로 합산하지 않는다. [단계별 계측](FEED_TELEMETRY.md)의 측정 없음/재시작·실제 network head와의 구분을 유지한다.
