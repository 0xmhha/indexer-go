# Arc 디스크 윈도우 peer 모듈

`modules/feeds/arc`는 현재 Go module 안에서 별도로 import하는 `arcfeed` 패키지다. 기존 geth RLPx 라이브러리로 실행 계층의 **ETH/68 history 요청**을 수행한다. Arc 실행/합의 바이너리, EVM, state trie, snapshot, Rust 빌드를 시작하지 않는다. 과거 ExEx 비교 코드는 `modules/arc-node-feed`에 별도로 남아 있다.

## 지원 범위

- Arc testnet `5042002`만 허용한다. peer는 명시적 enode 목록과 네트워크 identity key로 연결하고 discovery는 실행하지 않는다.
- `evmdata.BasePlan()`의 header RLP, body RLP, ETH/68 receipt RLP 전체를 받는다. body에는 거래가, receipt에는 로그가 포함된다. 별도 계정/스토리지/trace/blob dataset을 요구하면 연결과 디스크 초기화 전에 거부한다.
- genesis와 bootstrap checkpoint를 caller의 fresh canonical reader로 확인한다. status의 network/genesis/fork ID를 대조하고 요청 ID·높이·크기를 검사한다. 검증 worker는 profile의 블록 hash/거래 디코딩, transaction/uncle/withdrawal/receipt root, receipt 수·유형·gas/bloom과 parent 연속성을 확인한다.
- capture transaction 안의 fresh canonical 조회는 계속 수행한다. caller는 **기존 체인별5 HTTP/s 공유 pool**의 reader를 전달해야 한다. 이 모듈은 HTTP client나 우회 limiter를 만들지 않는다.
- ETH/68 실행 history를 순서대로 요청한다. CL의 Malachite gossip/certificate를 해석하거나 실행 결과·finality를 자체 증명하는 모듈은 아니다. 공급 감사와 자료별 한계는 [Arc 공급 감사](../../../docs/analysis/arc-peer-capabilities.md)를 참조한다.

## 호출 경계

실제 enode·network ID·fork ID는 운영자가 확인한 값을 전달한다. 샘플 public peer나 추측한 fork ID를 기본값으로 넣지 않는다. identity key는 이 peer의 네트워크 식별자이며 caller가 로컬에서 생성·보관한다. 거래 서명 키를 전달할 필요는 없다.

```go
// db: 이미 열린 Pebble/PostgreSQL CaptureDatabase
// fresh: 기존 공유 RPC pool의 uncached CanonicalReader
// chain/anchor: fresh 검증할 chain ID·genesis·bootstrap 높이/hash
// key, peers, auditedNetworkID, auditedForkID: 확인된 로컬/운영 입력
// layout: window.ReadLayout 또는 기존 window_storage에서 읽은 설정
peer := ethpeer.Config{
    Chain: chain, Checkpoint: anchor,
    NetworkID: auditedNetworkID, ForkID: auditedForkID,
    Key: key, Peers: peers,
}
err := arcfeed.Run(ctx, db, fresh, peer, capture.SessionConfig{
    Storage: layout, Instance: "arc-testnet",
    Plan: evmdata.BasePlan(), Anchor: anchor,
    // Create: true는 최초 빈 window 생성 때만 명시한다.
}, capture.PipelineOptions{QueueReferences: 256})
```

외장 volume의 `InitializeVolume`은 최초 명시적 작업이다. 경로·mount·identity·24시간 보관·공유 byte quota는 [window 설정](../../../docs/WINDOW_STORAGE.md)을 따른다. 예상 블록 수만으로 필요한 용량을 보장하지 않는다. restart는 `Create: false`로 기존 window와 같은 plan/checkpoint를 열며, 누락된 경로를 재생성하지 않는다. 기존 `cmd/indexer`에는 [source.feed 실행](../../../docs/FEED_APP.md)으로 DB-only writer와 lifecycle을 연결했다. 운영 config.yaml에는 활성화하지 않았고 실제 peer/mount 검증은 남아 있다. 선택적 finality/reorg/GC는 별도 `RunManaged`로 연결한다.

## 두 worker와 복구

`capture.PipelineTasks`의 수신 goroutine은 다음 블록의3개 encoded dataset을 P2P로 받아 `AppendWait`로 fsync한다. 별도 goroutine이 raw 참조를 모아 검증하고 source DB에 원자적으로 commit한다. 두 worker는 bounded 참조 channel을 공유한다. channel이 차면 알림을 합치며, 실제 작업은 디스크 window에 보존한다. 처리기는 Page로 계속 읽기 때문에 channel 크기보다 backlog가 커도 수신이 channel 뒤에서 멈추지 않는다. 디스크 quota는 별도 역압력을 건다.

재시작은 capture의 연속 완료 위치와 raw Page를 대조한다. 부분 저장 블록은 재요청하고 동일 참조를 중복 저장하지 않는다. peer가 head 아래의 header/body/receipt를 제공하지 않으면 `ErrHistoryGap`, commitment/프로토콜/canonical 불일치는 오류로 중단한다. 해당 블록을 건너뛰거나 완료 ACK하지 않는다. transport 단절은 static peer 재연결과 같은 높이 재요청으로 재개한다. 기존 `Run`은 fork 교체를 오류로 반환한다. 새 `RunManaged`는 전체 worker join 뒤 bounded 공통 조상 검증과 generation reset으로 재개한다.

DB commit을 마친 블록만 다른 peer에 제공한다. 기본 최근1024블록의 참조만 메모리에 두고 바이트는 원본 window의 짧은 lease로 읽는다. 모르는/이미 삭제된 자료는 빈 history 응답이다. 전체24시간을 모두 서비스하는 서버나 full node의 실행 검증을 선언하지 않는다. 종료는 stream·peer를 취소/join한 뒤 window를 닫는다. 기존 `Run`은 자동 GC/finality/ACK를 추가하지 않는다. `RunManaged`는 명시적인 Arc RPC-finalized 신뢰 모드, raw 소비자의 durable 완료/ACK,24시간·lease·DB fence 조건의 선택적 GC를 제공한다. 이전 fork 원본은 보관한다. 기존 projection 완료 ACK는 별도다. 자세한 API·신뢰 근거·오류/재개 범위는 [Feed 런타임](../../../docs/FEED_RUNTIME.md)을 따른다.

## 검증

선택 `NewStateSupplier`는 이미 수집한 블록의 지정 주소·슬롯을 기존 공유5 HTTP/s RPC에서 batch 조회하고 account/storage proof·code hash를 검증·보존한다. `facts.Source.StateSnapshot`은 이를 DB에서 재검증해 읽는다. Arc의 실제 public RPC proof 지원은 미확인이다. 선택 feed.state_proofs로 자동 task·선택 YAML·atomic 완료 cursor/event와 status를 연결했다. 기본3종 required plan·앱의 상태 feature 제한은 유지하며 부모 상태/checkpoint·필수 상태 plan/GC는 남아 있다. [상태 증명 공급](../../../docs/STATE_PROOFS.md)을 따른다.

```bash
go test -race ./modules/feeds/arc ./pkg/chainfeed/... ./pkg/capture
GOMAXPROCS=1 go test -race ./modules/feeds/arc \
  -run '^(TestArcIntakeContinuesWhileProcessorWaits|TestArcPartialRawRestart)$' \
  -count=200 -timeout=10m
```

독립 loopback RLPx oracle→지정 window→검증→Pebble/실제 PostgreSQL, 처리 정지 중 수신 지속, 부분 raw 재시작, 손상·누락·틀린 요청 ID·fresh mismatch 거부, 기존 제한 RPC pool을 검사한다. [기록 fixture](testdata/README.md)는 실제 Arc의 실행 입력 포맷을 고정한다. 실제 Arc peer 연결/재연결·보존 구간·450ms 처리량·24시간 운영은 아직 측정하지 않았다.

## 정규화 원천 행

기본3종 입력의 모든 profile/commitment 검증 뒤 header/body/transaction/receipt/log를 `capture.Input.Rows`로도 만든다. 기존 model codec으로 체인 확장을 보존하고, 행 digest/count와 encoded dataset·manifest·cursor·내부 이벤트를 같은 DB transaction에 저장한다. `capture.Store.Rows`는 raw window 없이 개별 행을 페이지로 조회한다. [영구 원천 저장](../../../docs/PERMANENT_SOURCE_STORAGE.md)을 참고한다. 계정/storage/trace/blob는 기존처럼 unsupported이며 plan을 넓히지 않았다.

## DB facts writer

`arcfeed.NewFactsSource(ctx, captureStore, liveCanonical, consumer)`는 Arc5042002의 같은 profile/native 회계를 고정하여 기존 fetch writer에 연결한다. block/receipt/state를 노드에서 다시 읽지 않으며, 현재 없는 계정/code/contract 입력은 coverage 오류다. [facts Source 조립·내구성·fresh·ACK·재분기 계약](../../../docs/SOURCE_FACTS.md)을 따른다. 앱의 opt-in lifecycle과 Source epoch 교체는 [source.feed 실행](../../../docs/FEED_APP.md)에 연결되어 있다.

선택 provider-reported 호출 트리는 `NewCallTraceSupplier`/앱 `call_traces`로 공급한다. same Arc profile·기존 shared5 HTTP/s client를 사용하고 거래 root/receipt를 대조하지만 하위 실행은 자체 증명하지 않는다. public Arc debug 활성화·형식/보존은 실제 확인 전이며 base plan의 trace dataset은 계속 unsupported다. [설정과 조회](../../../docs/CALL_TRACES.md)를 따른다.
