# MON-02/03 구현과 RED 조건

[MON-01 정본](../../../NATIVE_MONITORING_INTEGRATION.md)의 후속 계획이다.
각 slice는 별도 RED→GREEN·문서/체크리스트·PR·applicable CI·squash/main 단위로
진행한다. 이전 slice를 main에 반영한 뒤 다음으로 이동한다. 새 runtime 검사는
그때 실제 바뀐 코드에만 적용하며, 이 문서 작업에서는 실행하지 않았다.

## MON-02-A — 다음 권장: 순수 관측 모델과 qualification

변경 후보는 새 `internal/nativeobserve`의 DTO/metric definitions/scope binder/
qualification과 단위 테스트, 기존 budget/horizon predicate의 순수 공통 helper
추출(예: `internal/monitorpredicate`)이다. chain-bound v1 Evaluate에 가짜 identity를
넣지 않고 math를 재사용한다. CLI/보고 동작·App·Store·guard·schema·실제 설정은
바꾸지 않는다. worker/채널/파일/DB/RPC/알림을 시작하지 않는 순수 layer다.

완료 조건:

- metric enum/단위/resource kind·measurement definition·budget scope·series/run/
  source token·position family·획득/event 시각·quality를 구분한 typed 값.
- resource64/key1024 등 명시한 구현 상한 검증. 누락과 known0/음수·overflow,
  duplicate/conflicting resource binding을 분리하고 입력을 수정하지 않음.
- 현재/이전 component를 각각 qualify. 알려진 오류가 다른 미관측 값에 가려지지
  않음. 서로 다른 completion timestamp를 합친 가짜 current snapshot이 없음.
- Ddb/Dwal/Dpg/schema를 구분하고 database 전체 값을 per-chain schema budget에
  매핑하는 것을 거부. 같은 resource의 서로 다른 budget/definition은 conflict.
- capture generation과 receiver/native/raw epoch는 타입/명시적 binding으로 구분.
  미검증/lower-bound/truncated 값을 exact head/backlog로 변환하지 않음.

첫 RED는 **한 DB 전체 크기를 두 chain schema에 같은 per-chain metric으로
허용하거나, 오래된 WAL을 새 DB 시각으로 freshen하는 adapter를 거부해야 한다**는
행동 요구다. stub/기존 부재 경로가 실제 요구에 실패하고 컴파일되는 것을 먼저
보여준다. 순수 layer이므로 새 동시성200 캠페인을 붙이지 않는다.

## MON-02-B — 기존 owner 결과 tap과 통계 sampler 소유권

변경 후보: `internal/opsguard/pgstats.go`와 monitor의 result 경계,
`pkg/app/operations.go`/lifecycle, Arc round·SQL visibility·기존 telemetry의
비차단 observer hooks. MON-02-A가 정의한 read-only capability만 연결한다.

기존 sampler에서 필요한 공통 획득 부분을 추출해 capacity+observation이 함께
켜져도 정확히 하나만 돌아가게 한다. 관측 전용 mode도 같은 lifecycle/provider를
사용하고 별도 pool/poller를 만들지 않는다. opt-in stats deadline 상한2초와
round5초를 분리해 legacy default30초·stats 실패 비중단 의미를 유지한다.

RED 조건은 duplicate sampling/late result/store-close 경합이다. fake provider의
호출·동시 실행·cancel/return/close 순서를 채널로 제어하여 증명한다. 일반 pool
대기/SQL 시간을 합친 deadline, API1 미사용·pool4 유지, detach 후 late token
무효화, direct Shutdown/다중 체인 재시작을 검증한다. ACK seed는 progress0초가
아니며 ref/진행 시각을 독립적으로 검사한다. 실제 PG가 필요한 변경은 owned
fsync-on disposable fixture에서만 검증하고 운영 DB/노드에는 연결하지 않는다.

새 concurrent lifecycle/cache/queue가 생기면 그 새 사례에 한 번의
`GOMAXPROCS=1 -race -count=200`을 적용한다. 관련 Store 수정 시 API 예약·취소·
반납·fresh/fence 경계를 포함한다. 전혀 바뀌지 않은 native SDK/full matrix나
성능 캠페인을 반복하지 않는다.

## MON-02-C — 경보 reducer와 private checkpoint/queue

변경 후보: 새 `internal/nativealerts`의 pure reducer, bounded private checkpoint,
delivery queue/fake sink. source/row/block/transaction payload를 저장하지 않는다.
기존 SQL outbox/notifications 설정/GC journal을 알림 상태 저장소로 쓰지 않는다.

RED 조건은 unknown을 정상 복귀로 보내거나 sequence gap/재시작으로 active
critical을 지우는 것이다. 즉시 critical, count+dwell warning, 별도 clear
threshold/count+dwell, repeated-state dedupe/cooldown을 fake clock으로 검사한다.
scope/series/policy/run reset, stale·역순, overflow와 backpressure, lost checkpoint,
atomic write/권한/link/size 거부, 전달 receipt 실패·재시작 중복을 포함한다.

private checkpoint는 alert/delivery 상태만 보존하고 rate 표본은 새 run에서
다시 수집한다. 파일을 읽지 못하거나 손상되면 state-lost/unknown이며 기존
resource가 복구됐다는 event를 만들지 않는다. persistence 실패로 input owner를
block하거나 durable source ACK/GC/COMMIT 결과를 변경하지 않는다.

## MON-02-D — opt-in App assembly와 sink 경계

변경 후보: disabled 기본 config/validation과 App assembly, 제한된 adapter.
`operations.observation`은 이때 실제 지원 필드가 된다. off일 때 이전 동작·
진단 CLI·default·기존 capacity config가 그대로인 것을 먼저 RED로 고정한다.

기본 sink는 disabled/fake다. 사용자 chain event pipeline을 운영 경보로 공유하지
않는다. 기존 transport를 공통화한다면 destination/private-network/redirect/
timeout/signature 규칙을 보존하고 별도 operator binding을 검증한다. 정의되지
않은 목적지·credential·권한을 가정하지 않는다. 실제 메시지 발송은 사용자에게
명시적으로 허용된 운영 범위가 있을 때만 활성화한다.

integration RED는 느린/실패 sink가 publisher를 막거나 background가 API reserved
context를 빌리는 경우다. payload redaction·bounded retries/dead-letter·stable
transition ID, no halt/Resume/GC/write capability, shutdown join을 fake sink와
소유 fixture로 확인한다. 외부 endpoint/운영 secret/실제 알림은 사용하지 않는다.

## MON-03 — Docker VM source와 observer health

별도 adapter로 probe interface·strict bounded result parser·fake process runner부터
구현한다. 현재 `docker_vm_free`는 container 실행이므로 가벼운 read-only getter로
등록하지 않는다. 실제 context/image/권한·관측법은 OPS-03/05 전까지 HOLD다.

RED 조건: 멈춘 probe가 반복 생성되거나, permission/daemon/malformed/timeout을
0 free/full 또는 healthy로 내보내는 경우. 동시에 하나의 owned 작업, context
cancel/kill/wait, 늦은 결과 fencing, stderr/credential redaction, bounded output,
byte 단위 정수 변환/overflow, missing heartbeat의 unknown/별도 경보를 검사한다.
기존 watcher lock/다른 프로젝트의 container·volume·Docker daemon은 종료하지 않는다.
수집 중 Docker pull/자동 provisioning·Compose actuation을 하지 않는다.

CLI 종료만으로 probe container가 제거됐다고 가정하지 않는다. 실제 container
방식은 job nonce/소유 label과 private cidfile로 해당 ID만 기록·확인하고, 취소 시
그 probe만 정리한다. ID/소유권 확인 실패·cleanup 실패는 unknown/pending으로
남겨 다음 probe 생성을 막고, 다른 container나 volume을 prune하지 않는다.

실제 mount 분리·권한·Docker/macOS 종료 보장과 장기 운전은 OPS-05/06에서만
입증한다. OS 호출이 끝나지 않아도 무조건 join된다는 보장은 만들지 않는다.

## 공통 검증과 인계

| 검증 | 적용 시점 |
| --- | --- |
| 행동 RED→GREEN·오류/취소/immutability | 각 코드 slice |
| build/fmt/vet/lint·focused race | 바뀐 Go package/연결 경계 |
| 새 concurrency CPU1/race/single200 | 실제로 새 concurrent logic을 추가한 slice만 |
| 실제 DB 경로 | SQL/connection/lifecycle 경계를 바꾼 경우 owned disposable fixture |
| 기존 scoped short CI | 모든 PR, 실제 exact head와 적용 job 확인 |
| SDK/full/performance | 변경으로 필요해진 별도 scope만. 성능/운영 HOLD는 유지 |
| 현재 상태/backlog/계획·체크리스트·검증 | 단계 전환과 중단/종료 전 |

각 slice 종료에 실제 source/head·결과·skip·다음 행동을 저장소에 남긴다.
운영 입력은 [운영 준비 현황](../../../OPERATIONS_READINESS.md), 전체 의존성은
[작업 ID 목록](../../../analysis/open-items.md)에서 관리한다.
