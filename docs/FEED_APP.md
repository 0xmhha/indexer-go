# 앱의 window feed 실행

`source.feed`는 `pkg/app`에서 수집·원천 DB·기존 projection writer를 함께 실행하는 명시적 선택이다. 현재 등록된 모듈은 `arc.eth68`이며 감사한 Arc 테스트넷5042002의 header/body/ETH68 receipt만 공급한다. full node·EVM·state trie를 실행하지 않는다. 실제 Arc peer, 외장 mount, 450ms 수집·24시간 운영은 아직 검증하지 않았다.

## 설정과 지원 범위

다음은 **입력값을 채워야 하는 설정 조각**이다. 실제 `config.yaml`은 변경하지 않았다. genesis/checkpoint·network/fork ID·static enode는 해당 네트워크에서 확인한 값을 사용한다. 첫 후처리 높이는 anchor+1이며 anchor 자체는 수집된 데이터가 아니다.

```yaml
rpc:
  endpoint: <public-rpc>
  rate_limit: 5
indexer:
  start_height: 67000001  # 아래 anchor 바로 다음 높이
  finality: head         # projection 대상은 DB의 연속 capture Through
features:
  balance.native:
    enabled: false
  token.metadata:
    enabled: false
source:
  feed:
    module: arc.eth68
    window: arc-testnet  # window_storage.windows의 고정 예약 이름
    consumer: arc-explorer-v1
    chain_id: "5042002"
    genesis_hash: <0x-32-byte-genesis>
    anchor_height: 67000000
    anchor_hash: <0x-32-byte-checkpoint>
    network_id: <verified-network-id>
    fork_hash: <8-hex-characters-without-0x>
    fork_next: 0
    key_file: /Volumes/IndexerData/identity/arc-peer.key
    peers:
      - enode://<public-key>@<host>:<port>
    listen_addr: "127.0.0.1:0"  # 외부 peer 연결을 받으려면 명시적 IP/port와 라우팅 필요
    initialize_volume: false
    create_window: false
    finality_authority: ""  # 비어 있으면 reorg 감지만; GC 허용 아님
    collect: false
    control_interval: 10s
```

[window-storage.yaml](examples/window-storage.yaml)을 `window_storage:` 아래에 넣고 실제 mount/ID/예산/경로로 바꾼다. `key_file`은 64자리 hex secp256k1 **peer identity**를 담는 소유자 전용 일반 파일(0600)이다. 키를 로그나 YAML에 넣지 않는다. 심볼릭 링크·공개 권한·잘못된 길이/형식은 거부한다. listener는 literal IP와 TCP port만 받는다.

새 빈 저장소의 최초 초기화에만 `initialize_volume: true`, `create_window: true`를 명시한다. root는 이미 존재해야 하고 mount 검사와 marker/ID 확인을 통과해야 한다. 앱은 성공한 window의 volume ID/root/path/reservation을 source DB에 동기 기록한다. 이후에는 같은 first-start flag가 남아 있어도 **기존 window만 Open**한다. 외장 디스크/원본이 사라졌을 때 재생성하지 않는다. 초기화 중 DB 기록 전에 실패해 이미 window가 만들어졌다면 원인을 확인한 뒤 두 flag를 false로 바꾸어 기존 window를 연다. marker/ledger/DB binding을 지워 우회하지 않는다. 초기화 후에는 두 flag를 false로 두는 것이 권장된다.

`finality_authority`는 URL/비밀번호가 아닌 고정 운영자 label이다. 이를 지정하면 같은 5 HTTP/s RPC pool의 finalized tag를 신뢰하는 감사된 Arc 정책을 사용한다. `collect: true`도 지정해야 최소24시간 보관·검증된 source facts·finality·삭제 fence 검사를 통과한 canonical 원본을 수거한다. label/정책 변경은 DB와 충돌하면 중단한다. 독립적인 CL certificate 검증은 아니다. 이전 fork 원본은 별도 종료 증거 없이 수거하지 않는다.

상태 공급이 없는 기본 plan에서는 모든 `feature.StateReader`와 `dex.orderbook`를 시작 전에 거부한다. `allow_pruned_state`로 우회하지 않는다. 현재 기본값 중 `balance.native`, `token.metadata`를 명시적으로 꺼야 한다. 기타 custom handler도 제공한 DB-only deps를 사용해야 하며 자체 RPC를 만들면 이 계약 밖이다. 라이브러리의 이미 초기화된 잔액 재생 가능성을 앱의 완전한 잔액 지원으로 확대하지 않는다. account/code/storage/trace/blob plan·영구 객체 설정·전역 source 검색은 후속 작업이다. [기능별 진행/coverage API와 선택 요약 worker](PROJECTION_PROGRESS.md)는 별도로 연결했다.

full ingest 전용이며 era, declared, 과거 ExEx IPC feed, RPC recording, gap recovery와 결합하지 않는다. 알려지지 않은 feed 필드/모듈, 잘못된 chain/plan/range, 공유 window 예약, state 기능과 5 이외 rate limit은 거부한다. 잘못된 새 구성을 발견했을 때 DB/RPC/volume에 접근하지 않도록 모든 enabled chain을 먼저 점검한다. 실제 RPC chain ID·genesis·anchor도 확인한 뒤 연결한다. 기존 RPC 모드는 `source.feed`가 없으면 그대로다. layout만 있고 소비할 feed가 없으면 계속 오류다.

## 시작·reorg·종료

원천과 projection은 같은 chain DB handle을 사용한다. 원천 namespace는 projection undo/reindex에서 보존된다. 기존 DB cursor가 anchor 이전이거나 anchor block을 포함한 별도 미수집 history이면 자동 접합하지 않고 별도 DB/명시적 재구성을 요구한다. 새로운 feature의 backfill이 anchor 이전을 요구해도 RPC로 채우지 않는다.

1. 등록 모듈·기능·저장 layout을 순수 검사하고 RPC identity를 확인한다. 동일 plan/anchor의 source DB를 초기화/검사하고 정책·외장 window·미완료 삭제를 확인한다.
2. fresh canonical 수집과 DB facts projection을 별도 task로 실행한다. 블록/receipt/상태의 projection RPC fallback은 없다. source commit과 projection commit 각각의 fresh 검사 및 기존 HTTP limiter를 유지한다.
3. capture Reset을 DB에서 관찰하면 projection을 취소한다. live goroutine·online backfill·writer·relay를 모두 join/close한 뒤 새 epoch Source/fetcher를 만든다. 기존 undo/outbox로 공통 조상까지 정리하고 재개한다. 수집하지 않은 bootstrap까지의 rollback은 fresh hash가 anchor와 같을 때만 허용한다. bootstrap 아래/undo 범위 밖은 재구성이 필요하다.
4. 재시작 때 마지막 durable block을 중복 검증하여 commit 직후 유실된 projection receipt를 복구한다. feature/outbox를 중복 반영하지 않는다. receipt는 raw ACK나 연속 완료 cursor가 아니다.
5. 한 task의 종료/오류는 상대 task를 취소하고 join한다. `Shutdown`은 이 작업이 끝난 뒤 DB/client를 닫는다. 한 App의 Run은 한 번만 허용하며 프로세스/manager 재시작은 새 App으로 기존 DB/window를 연다. capture 자동 reorg 복구는 기존 기본8회, projection 교체는 최대64회다.

## 여러 체인

멀티체인에서는 root `source.feed` 대신 `multichain.chains[].feed`에 같은 설정을 넣는다. root `window_storage`는 공유하고 각 feed는 서로 다른 예약을 사용한다. RPC endpoint·DB directory/SQL schema·feature override는 기존 per-chain 규칙을 따른다. 예를 들어 Arc entry만 `feed`를 선택하고 StableNet·BSC·Sepolia는 기존 RPC ingest를 계속 사용할 수 있다. 그 세 체인의 새로운 peer 공급 모듈은 구현됐다고 표시하지 않는다. health의 node 높이는 live RPC, projection 진행은 현재 fetcher의 DB head 기준이다.

로컬 검증은 이 혼합4체인 시작·Arc RPC 장애 동안 다른3체인 진행, 재시작·fresh 검사·bootstrap reorg·누락 원본 재생성 금지·종료 join을 포함한다. 실제 공개4테스트넷 동시 feed 운영이나 처리량 증거는 아니다.

선택 `feed.projections: [execution.summary.v1]`는 별도 DB-only block 요약 worker다. 자체 cursor/event/epoch를 소유하고 앱의 cancel/join에 포함된다. [완료 기록·HTTP API·예산과 제한](PROJECTION_PROGRESS.md)을 따른다. 기존 공유 undo를 사용하는 stateful feature의 병렬화 설정은 아니다.

선택 `feed.state_proofs`는 지정한 주소·슬롯·code의 자동 공급 task다. name·start_height·accounts를 명시하고 기존5 HTTP/s pool을 공유한다. proof 저장·입력 receipt·연속 cursor·자체 event는 한 durable transaction과 한 fresh 검사에 들어간다. DB prefix를 큐로 읽으며 미공급/pruned 높이를 건너뛰지 않는다. source epoch 변경 시 재조정하고 모든 다른 task와 함께 취소/join한다. `parent_checkpoint: true`는 첫 부모 상태를 먼저 보존하고 `required: true`는 첫 intake 이전에 필수 상태 완료/GC 계약을 고정한다. 앱은 raw Open 전에 계약을 검사하며 재시작 때 삭제/변경을 거부한다. [설정·status·내구성·필수 상태 경계](STATE_PROOFS.md)를 따른다. 이 설정만으로 balance.native/token.metadata/eth_call을 활성화하지 않는다.

선택 `state_proofs.project_changes: true`는 [상태 후처리 worker](TRACKED_STATE_PROJECTION.md)를 추가한다. parent 모드를 함께 켜고 자체 현재값·블록 변경·undo/cursor/event를 한 transaction으로 관리한다. 수신·원천 저장·기존 writer·선택 summary·proof 공급과 별도 task이며 종료 시 모두 join한다. `stateChanges`와 DB-only 상태 API는 기존 상태 기능 활성화와 독립적이다.

선택 `feed.telemetry: {receive_interval_target: 450ms}`는 수신/append/capture/proof/state/RPC 지연과 관측 가능한 queue age를 메모리에서 계측한다. `/indexing/status.pipeline`은 DB backlog와 실행 중 표본을 구분하고 재시작/다른 프로세스의 측정 없음은 null/unavailable로 표시한다. [단계 정의·운영 기준·한계](FEED_TELEMETRY.md)를 따른다.

선택 `feed.call_traces`는 provider의 hash-selected 호출 트리를 별도 task로 수집한다. 원본 응답/stream 완료를 원자 저장하고 [호출 trace API](CALL_TRACES.md)로 DB-only 조회한다. 실제 endpoint의 debug/부모 상태 지원이 필요하며 기존 내부거래/잔액 기능이나 base plan의 trace coverage를 자동 활성화하지 않는다.

고정 Arc 공식 노드는 blob 거래 admission을 비활성화한다. 앱은 profile/module의 code-owned audit를 descriptor에 기록하고 `/indexing/status`의 `source.blobCapability`로 DB-only 제공한다. 이는 수집 완료와 별개이며 과거 descriptor의 누락 metadata는 null이다. `feed.blobs` 활성화는 지원하지 않는다. [근거·라이브러리 차단·호환성](BLOB_INPUTS.md#arc-정책과-상태-조회)을 따른다.

선택 `feed.projections: [source.lookup.v1]`는 영구 원천 DB의 블록/거래 해시 색인과 DB-only 행 페이지를 연결한다. 별도 완료 cursor를 가지며 fresh 검사는 같은5 HTTP/s 예산을 사용한다. [원천 검색](SOURCE_QUERIES.md)의 범위·페이지 고정·용량/운영 제한을 따른다.

선택 `feed.projections: [source.search.v1]`는 거래 from/to·log 발행 주소/위치별 topic과 고정 블록 범위의 검색을 연결한다. hash lookup과 별도 cursor/root를 사용하며 각 fresh는 동일5 HTTP/s 예산을 소비한다. [원천 검색](SOURCE_QUERIES.md)의 opaque cursor·후보256/검증 블록16·행/동시성 한도를 따른다.
