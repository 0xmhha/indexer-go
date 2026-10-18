# 지정 계정·code·storage 증명 공급

`pkg/chainfeed/evmstate`와 `arcfeed.NewStateSupplier`는 이미 수집한 블록에 대해 **명시적으로 선택한 주소·슬롯의 블록 종료 시점 상태**를 검증·보존한다. 앱은 선택 `feed.state_proofs`로 별도 수집 goroutine을 실행하고 `facts.Source.StateSnapshot`으로 후처리에서 읽는다. 전체 계정 변경 목록·모든 contract storage·EVM 실행·trace·blob을 공급하는 기능은 아니다. 앱의 state feature 거부와 기존3종 capture plan은 유지된다.

## 조회·검증과 보존

공급자는 기존 chain client의 `BatchCallContext`를 빌린다. 새 endpoint/client를 만들지 않는다. 선택한 모든 `eth_getProof`와 요청한 `eth_getCode`를 HTTP 배치 한 번으로 조회한다. 각 요청은 [EIP-1898](https://eips.ethereum.org/EIPS/eip-1898)의 `{blockHash, requireCanonical: true}`를 사용하며 높이/latest fallback은 없다. 공개 RPC의 proof/hash selector 지원 여부는 아직 실제 확인하지 않았다. batch/멤버 오류·pruned state·응답 누락은 전체 선택의 실패이며 일부만 완료했다고 저장하지 않는다.

[EIP-1186](https://eips.ethereum.org/EIPS/eip-1186)의 account/storage MPT proof를 검증한다. 기존 profile로 header/body/receipt를 재검증해 얻은 state root에 계정의 `keccak(address)` 경로를 대조하고, 증명된 계정의 storage root에 `keccak(32-byte slot)` 경로를 대조한다. RLP에서 nonce·balance·storage root·code hash·storage 값을 도출하고 모든 RPC 보고 값과 비교한다. code는 증명된 code hash와 keccak으로 대조한다. 없는 계정/슬롯·빈 code도 증명으로 확인하며, 누락된 조회를0으로 취급하지 않는다. Geth의 absent-account 응답에 있는 zero root/hash와 empty trie/code hash 표현을 구분하여 처리한다.

`Account.Balance`는 증명된 **consensus account 필드**다. 특정 체인의 RPC/native 자산 회계와 같다고 자동 가정하지 않는다. Arc의 native USDC 회계나 `balance.native`의 초기화/checkpoint로 이 값을 자동 연결하지 않는다. code·storage 몇 개가 있어도 임의 `eth_call`을 실행할 수 있는 EVM 상태 전체가 된 것은 아니다.

검증한 원본 proof·code·selection을 `evm.state-proof.v1` 불변 supplement로 저장한다. key는 chain namespace 안에서 exact capture identity·codec·selection digest에 묶인다. 동일 selection/identity의 다른 바이트는 덮어쓸 수 없다. `CommitSupplement`는 현재 epoch/canonical identity와 **별도 fresh canonical 조회**를 같은 동기 DB transaction 안에서 검사한다. Pebble은 WAL+sync, PostgreSQL은 기존 fsync/synchronous_commit 계약과 값 파라미터 바인딩을 사용한다. 새 SQL 문자열/테이블은 없다.

재시도에서 이미 저장한 값은 DB에서 읽어 다시 증명을 검증하고 fresh 검사를 재수행한다. 상태 조회는 반복하지 않으므로 그 사이 RPC 상태가 pruned돼도 재시도할 수 있다. 후처리 `Read`/`ReadSnapshot`/`StateSnapshot`은 RPC를 호출하지 않고 원본 proof와 code를 header commitment에 다시 대조한다. digest만 맞게 바꾼 잘못된 값도 거부한다. 오래된 epoch나 현재 canonical prefix 밖인 fork는 현재 결과로 제공하지 않는다. 새 epoch에 남은 canonical 조상의 exact input은 재사용할 수 있다.

## 앱 자동 수집 설정

`source.feed` 또는 `multichain.chains[].feed` 안에 다음을 넣는다. 아래 주소·슬롯은 형식 예시이며 실제 추적 대상으로 바꾼다. 운영 config.yaml은 자동 변경하지 않는다.

```yaml
state_proofs:
  name: watched-v1
  start_height: 67000001  # source anchor보다 큰 명시적 첫 높이
  poll_interval: 50ms    # 생략 시50ms, 허용20ms..5s
  parent_checkpoint: true # 선택: start_height-1 상태도 먼저 보존
  required: false         # true면 checkpoint 포함·필수 완료·raw GC 조건을 DB에 고정
  project_changes: false  # true면 독립 현재값·변경·undo/event worker도 실행
  accounts:
    - address: "0x1111111111111111111111111111111111111111"
      code: true
      slots:
        - "0x0000000000000000000000000000000000000000000000000000000000000003"
```

이름은 최대64자의 소문자/숫자/`.`/`_`/`-`다. 주소는 정확한20바이트, 슬롯은 정확한32바이트0x hex이며 중복은 거부한다. `code: false`/생략이면 account proof의 code hash는 검증하지만 code bytes를 수집했다고 표시하지 않는다. 시작 높이는 필수이고 latest로 자동 바꾸지 않는다. anchor 다음보다 나중을 지정하면 이전 높이의 상태는 이 scope의 coverage 밖이다. 미래 높이는 DB 수집이 도달할 때까지 기다린다. 현재 공급 capability를 선언한 모듈은 Arc testnet `arc.eth68`이다. 미지원 모듈·잘못된 설정·오타는 IO 전에 거부한다.

한 feed에 한 tracked stream을 설정한다. 이름·codec·정렬한 selection/digest·시작 높이·parent/required 모드는 원천 DB에 고정한다. 같은 이름의 범위를 바꿔 재시작하면 충돌이며 다른 명시적 이름이 필요하다. 이전 이름의 원천 자료는 보존한다. optional stream은 설정을 제거하면 자동 task와 현재 status 노출이 비활성화되고 이전 자료를 삭제하지 않는다. `required: true` stream은 설정 삭제·이름/범위/모드 변경을 재시작 오류로 거부한다.

별도 worker가 canonical capture prefix를 durable 작업 큐로 사용한다. 순서대로 한 블록만 처리하므로 backlog를 큰 RAM slice로 적재하지 않는다. P2P 수신·기존 projection·선택 요약 task는 독립적으로 진행한다. `CommitStreamSupplement`는 proof bundle·exact input receipt·연속 완료 cursor·자체 input event를 **한 transaction/한 fresh 검사**로 기록한다. partial proof나 channel 수신만으로 cursor를 올리지 않는다. 상태 RPC batch 오류/pruned는 해당 높이에서 앱 작업 집합을 취소·join하며 건너뛰지 않는다.

재시작은 마지막 완료 proof를 DB에서 재검증하고 다음 높이로 진행한다. commit 응답 유실 재시도도 입력/event/cursor를 중복하지 않는다. canonical 불일치는 기존 capture supervisor의 epoch reset을 최대2분 기다리고, 새 epoch에서는 exact canonical receipt가 있는 prefix만 최대128높이 뒤쪽 탐색으로 재사용한다. 없으면 설정 시작 높이부터 재생한다. DB에 이미 있는 proof는 RPC 재요청 없이 재사용하며 새 fork에 없는 상태는 새로 공급해야 한다. epoch 재개는 최대64회로 제한한다. 모든 task는 취소·join 뒤 DB/client를 닫는다.

## 부모 checkpoint와 필수 상태 완료

`parent_checkpoint: true`는 선택한 범위의 첫 높이 바로 전 블록 종료 상태를 먼저 공급한다. `required: true`는 이 옵션도 자동으로 켠다. 부모가 수집 범위 안이면 DB의 검증된 RLP header를 사용한다. 부모가 source bootstrap anchor이면 해당 hash의 `eth_getBlockByHash(..., false)` header와 모든 proof/code를 **같은 HTTP batch**로 얻고, profile의 header hash를 무조건 재계산한다. header/블록 번호·hash·state root와 exact selection의 MPT/code를 대조한다. anchor에 body/receipt/전체 capture manifest가 있다고 표시하지 않는다. 알려진 header suffix의 profile 규칙도 유지한다.

parent checkpoint bytes·cursor의 parent·checkpoint event는 한 durable transaction과 한 fresh 검사로 저장한다. 첫 input 완료 전에 이 checkpoint가 있어야 하며 proof/cursor commit에서 codec 검증을 수행한다. 재시작에서는 checkpoint와 마지막 proof를 DB에서 재검증한다. 부모가 바뀐 fork에서는 이전 checkpoint를 완료 근거로 쓰지 않고 새 canonical parent를 공급한다. 저장된 header+proof는 영구 source 자료이며 raw window 만료와 독립적이다. 부모 상태가 이미 pruned됐다면 task 집합을 취소/join하고 완료를 올리지 않는다.

필수 계약은 `State.requiredSupplement`에 고정하며, **첫 intake 이전(Through=anchor·source event 0·finalized/pending deletion 없음)**에만 추가할 수 있다. 이미 수집 중인 원천 DB를 자동 전환하지 않는다. 그런 DB에는 별도 DB 또는 명시적 migration 설계가 필요하다. 이 구현은 운영 DB/config를 전환하지 않았다. 앱/session은 raw Open이 과거 삭제를 복구하기 **전** 계약을 검사한다. 다른 Store handle도 DB에 고정한 계약을 적용한다. codec verifier가 없는 handle은 필수 상태 범위의 GC/복구를 승인할 수 없다.

필수 상태 완료는 fromHeight부터의 연속 proof cursor와 부모 checkpoint로 표현한다. 기존 base Through와 상태가 필요 없는 feature/summary worker는 별도로 계속 진행한다. downstream은 `facts.Source.StateTransition(ctx, height, descriptor)` 또는 `evmstate.ReadTransition`으로 exact selection의 before/after를 **RPC 없이** 읽는다. 첫 before는 checkpoint, 이후 before는 직전 블록의 검증된 proof다. `TransitionState`는 block/epoch/selection/accounts를 반환하며 bootstrap에 가짜 capture identity를 만들지 않는다. `Store.RequiredSupplementReady`는 현재 epoch/canonical 및 실제 필요한 proof bytes를 검증하는 명시적 완료 gate다. 임의 stateful handler를 자동 활성화하지 않는다.

## 상태 조회

기존 `GET /indexing/status`의 선택 `stateProofs`에 `descriptor`(name/codec/selection/digest/fromHeight), 현재 epoch, nullable `complete`(height/hash), 선택 `parent`(height/hash), `backlog`, sequence, status가 포함된다. 멀티체인은 `/chains/{id}/indexing/status`다. 완료 범위는 정확히 fromHeight부터 complete까지이며 네트워크 최신 높이가 아닌 **captured DB Through**에 비교한다.

- `checkpoint_pending`: parent 옵션이 있고 아직 부모 checkpoint가 없다.
- `waiting`: 아직 source가 시작 높이에 도달하지 않았다.
- `not_started`: 해당 source 범위가 있으나 첫 완료가 없다.
- `lagging`/`caught_up`: 설정 시작부터의 연속 완료가 captured prefix보다 뒤/같다.
- `stale`: source epoch가 바뀌었고 worker 재조정 전이다. 완료점은 null이다.

backlog는 범위 안의 미완료 captured 블록 수다. status는 동일 capture snapshot의 atomic receipt/parent canonical metadata를 확인하며 매 조회마다 최대8MiB proof를 읽지 않는다. 실제 snapshot read와 재시작은 proof bytes/commitment를 다시 검증한다. status는 RPC0회·read-only·no-store이고 PostgreSQL read-only/한 연결을 지원한다. 이 별도 tracked 상태점이 account_changes/storage_changes 전체 완료를 뜻하지 않으므로 기존 dataset 목록을 complete로 바꾸지 않는다. 값 조회는 아래 `StateSnapshot` library를 사용하며 새 상태값 HTTP endpoint는 추가하지 않았다.

## 라이브러리 조립 예

```go
// factsDB: Arc module이 Initialize/복구한 capture.Store
// chainClient: 기존 RPC client. CanonicalReader와 같은 chain-wide 5 HTTP/s
// pool을 사용해야 한다. supplier는 client/DB를 닫지 않는다.
supplier, err := arcfeed.NewStateSupplier(ctx, factsDB, chainClient.RPCClient())
if err != nil { return err }
state, err := factsDB.State(ctx)
if err != nil { return err }
id, err := factsDB.CanonicalAt(ctx, state.Generation, height)
if err != nil { return err }
selection := evmstate.Selection{Targets: []evmstate.Target{
    {Address: watchedAddress, Code: true, Slots: watchedSlots},
}}
snapshot, err := supplier.Acquire(ctx, state.Generation, id, selection)
if err != nil { return err } // 누락/pruned를 건너뛰지 않는다.
_ = snapshot

// 이후 다른 worker: 같은 Source epoch에서 DB-only 조회.
snapshot, err = blocks.StateSnapshot(ctx, height, selection)
if err != nil { return err }
```

일반 MPT EVM module은 해당 binary profile을 넣어 `evmstate.New`를 조립한다. Arc factory는 감사 대상 testnet5042002만 허용한다. `ReadSnapshot`은 RPC 의존성 없이 store/profile/epoch/identity를 받아 조회한다. 선택 주소·슬롯/code intent는 정렬한 canonical selection digest에 포함한다. 중복 주소/슬롯은 오류다. 저장된 selection과 정확히 같은 범위를 요청해야 하며, 다른 부분집합/상위집합을 저장했다고 추정하지 않는다.

## 한도와 예산

- selection당 최대8주소·총128슬롯, trie proof당 최대65개 node·node당1024bytes.
- proof JSON 응답당2MiB, code당256KiB, 저장할 bundle JSON당8MiB. cardinality/hex/RLP 정수/응답 크기를 검증한다.
- shared pool은 물리 HTTP 응답을 JSON decode 전 기본64MiB로 제한한다. `rpcpool.Config.MaxResponseBytes`로 더 작게 제한할 수 있다. 더 큰 값으로 hard cap을 제거할 수 없다. failover도 기존 같은 HTTP limiter를 사용한다.
- 새 snapshot은 상태 batch1회+fresh1회가 필요하다. 새 parent checkpoint도 batch1회+fresh1회이며 bootstrap header는 그 batch의 멤버다. checkpoint 복구와 transition 조회는 RPC0회다. 동일 snapshot 재시도는 fresh1회만 필요하다. capture/projection/다른 worker의 요청도 같은5 HTTP/s를 나눠 쓴다. 논리 batch member quota는 provider가 별도로 제한할 수 있다. 배치했다고2블록/s 추종이나450ms를 보장하지 않는다.
- `supplier.Metrics()`는 RPCBatch/Verify/Commit의 논리 횟수·실패·누적 wall time을 제공한다. RPCBatch에는 limiter 대기, Commit에는 fresh 조회와 durable commit이 포함된다. physical attempt는 pool의 Status에서 확인한다. 원자적 coverage snapshot이나 percentile로 해석하지 않는다.

proof는 RPC가 해당 상태를 보유할 때 **먼저** 획득해야 한다. 디스크에 body/receipt를24시간 보관해도 나중에 삭제된 원격 trie 상태가 복구되지는 않는다. 자동 worker는 명시한 범위를 순서대로 공급하며 미완료 높이를 latest로 건너뛰지 않는다. 같은5 HTTP/s를 나누므로 backlog가 늘 수 있고, 공개 provider의 실제 보존 범위·배치 member quota를 별도로 확인해야 한다. capture와 proof가 함께 생성률을 따라가는지는 운영 측정 대상이다.

## Capture plan·GC와의 경계

optional supplement는 기본 plan/digest·Through·FactsReady·projection/raw ACK를 바꾸지 않는다. **기본3종 plan의 완료가 상태 공급의 완료를 뜻하지 않는다.** source coverage의 account_changes/storage_changes는 계속 unavailable이다. 이들은 전체 변경 데이터셋이며 tracked selection의 두 시점 상태와는 다르다.

`required: true`에서 raw GC는 기존 24h 보존·finality·현재 canonical·base plan bytes·모든 raw consumer ACK·lease 조건에 다음을 추가한다. 범위의 부모 raw부터 checkpoint를 요구하며, fromHeight 이후 raw에는 current epoch의 연속 완료·정확한 input receipt·현재/직전 proof와 부모 checkpoint를 요구한다. 삭제 fence를 쓰는 **동일 source transaction 안에서 실제 bytes/digest와 header hash/MPT/code를 재검증**한다. proof와 receipt의 SHA를 함께 바꿔도 MPT 불일치를 거부한다. segment 내 같은 높이의 재검증은 metadata로 중복 제거한다. 새 SQL/table 없이 기존 bound-value capture storage를 사용한다.

상태 task가 늦는 것은 정상 GC 대기이며 삭제를 보류하고 수집을 계속한다. 완료를 주장한 bytes의 손상·I/O 오류·검증 실패는 숨기지 않는다. crash 후의 정확한 삭제 intent/receipt 복구도 같은 계약과 verifier를 적용한다. 요구 범위보다 전인 블록들은 상태 coverage 밖이며 base GC 계약을 따른다. proof cursor는 raw ACK·projection 완료·finality를 대신하지 않는다. 상태 proof는 permanent source DB에 남는다.

`project_changes: true`는 부모 checkpoint도 켜며 [지정 상태 후처리](TRACKED_STATE_PROJECTION.md)의 독립 현재값·변경·undo/event worker와 DB-only API를 실행한다. 첫 전용 stateful worker의 저장/undo 계약은 구현했다. 운영 지연/보존 기준·다른 stateful feature·native USDC 회계 연결은 후속 과제다. 실제 Arc enode·공개 RPC proof 지원·외장 mount·장기 보존/처리량은 미검증이다.

## 로컬 검증

독립 Geth trie builder/prover로 계정·존재/부재 슬롯·code가 있는 fixture를 만들고, 별도 HTTP oracle이 block hash/canonical selector·slot 순서·배치/응답 ID를 확인한다. 잘못된 balance/nonce/root/path/code/slot·누락 멤버·pruned/형식 오류·과대 입력·digest가 맞는 조작 자료를 검사한다. 두 DB의 내구성/재시도/재시작/epoch 경계, source 상태 조회,5 HTTP/s 물리 요청 간격과200회 commit/reset race를 검증한다. 상세 실행 결과와 제외 조건은 작업 기록3.46과 `/private/tmp/indexer-state-proof-20261015/validation.json`에 남긴다.

자동 task는 별도 empty-trie HTTP oracle·로컬 RLPx→window→capture→proof/cursor→기존 writer/status까지 두 DB에서 확인했다. richer trie/code/storage 원본은 위 Geth fixture의 실제 runtime 재개 시험으로 대조한다. partial write4지점·uncertain commit·scope 변경·retained canonical prefix·pruned 재시작·provider 실패의 전체 task 취소/join과 read-only pool1 status를 검사했다. source/cursor reset과 네 task join은 각각 CPU1/race/count200으로 확인한다. Linux 결과와 최종 증거는 작업 기록3.47 및 `/private/tmp/indexer-state-scheduler-20261016/validation.json`을 따른다.

부모 checkpoint/required 모드는 bootstrap header batch·richer MPT before/after·조작 proof/receipt·부분 checkpoint write3지점·uncertain commit·정상 GC 대기·정확한 삭제 복구·부모 reorg·pruned 재시작·설정 제거/변경 차단을 검증한다. 최종 환경별 결과는 작업 기록3.48과 `/private/tmp/indexer-state-required-20261016/validation.json`을 따른다.
