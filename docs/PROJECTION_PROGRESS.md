# 기능별 완료·독립 worker·coverage API

8번의 첫 단계로 facts 기반 기능별 완료 기록, 별도 transaction/cursor/event를 소유하는 블록 요약 worker와 DB-only HTTP API를 구현했다. 기존 잔액·토큰·AA 등의 writer는 하나이며 전역 높이 undo/outbox를 유지한다. 지정 account/code/storage의 proof 공급과 [독립 상태 worker](TRACKED_STATE_PROJECTION.md)는 구현했다. trace/blob와 기존 stateful 기능들의 독립화는 후속 단계다.

## 기존 feature의 완료 기록

facts Source가 `source.ProjectionReporter`로 실제 input identity·현재 epoch·bootstrap을 제공한다. writer는 실제로 실행한 feature/part의 definition·chain·plan digest·epoch와 처리 구간/end hash를 `/meta/indexing/units/`에 기록한다. 이 기록은 해당 block의 feature 데이터·cursor·outbox와 **같은 transaction 및 undo**에 포함된다. 실패/rollback은 함께 사라진다. RPC/era/replay의 기존 writer는 이 새로운 완료 기록을 만들지 않으며 status는 `unreported`로 반환한다.

최고 처리 높이를 연속 완료로 사용하지 않는다. 예를 들어1,3을 처리했다면 두 구간이고 연속 완료는1이다.2를 채워야1~3으로 합쳐진다. 구간은 최대128개이며 이를 넘는 작업은 commit하지 않는다. 실제 feed 앱은 anchor+1부터 순서대로 처리하므로 보통 한 구간이다. 라이브러리의 임의 순서 쓰기를 새로운 독립 undo 지원으로 확대하지 않는다.

동기/online backfill은 실행한 part만 기록한다. definition이 바뀌면 그 정의의 기록을 새로 시작한다. 중복 block은 새로운 기능을 실행했다고 표시하지 않는다. Source epoch가 바뀐 오래된 기록은 API에서 `stale`이고 새 앱 supervisor의 undo/재생 이후에만 현재 완료로 돌아온다. 기존 `FeatureState.Gap`도 반영하여 미완료 구간을 덮지 않는다. 데이터가 오래되어 기록이 없다면 기존 최고 높이로 추정하지 않는다. 필요한 경우 source 범위 내의 명시적 재생/재구성을 거쳐야 한다.

`feature_progress` 단계는 이 metadata의 준비/저장 시간과 SQL flush를 측정한다. 해당 단계의 성공은 아직 transaction commit이나 ACK가 아니다. `database_commit`, `source_validate`, `source_ack`와 함께 읽는다.

## 독립 실행의 소유권

첫 worker는 `execution.summary.v1`이다. 검증된 encoded DB 입력을 동일 binary profile로 재검증한 후 block별 transaction/receipt/성공·실패/log/contract creation 수와 gas used를 저장한다. raw window나 node의 block/receipt/state로 fallback하지 않는다. 불변 block 결과 전용이며 기존 feature 저장소를 변경하는 handler를 연결하는 API가 아니다.

```yaml
source:
  feed:
    # 나머지 항목은 FEED_APP.md의 검증된 Arc 설정
    projections:
      - execution.summary.v1
```

멀티체인에서는 같은 목록을 해당 `multichain.chains[].feed.projections`에 둔다. 기본값은 빈 목록이다. 모르는 이름과 중복은 IO 전에 거부한다. 현재 공급 모듈은 Arc 테스트넷 execution plan뿐이다.

worker는 기존 writer와 별도 goroutine에서 DB의 연속 capture prefix를 읽는다. 입력 해석/계산은 transaction 밖에서 수행하고, `capture.Store.CommitWorker`가 다음 항목을 자체 source namespace transaction으로 동기 commit한다.

- exact input/definition에 묶인 최대64KiB JSON 불변 결과
- worker의 연속 완료 cursor와 generation
- worker 자체 sequence와 `output` event

commit 바로 전 source generation·canonical identity·parent 연속성·**fresh RPC hash**를 확인한다. 불확실 commit의 동일 재시도는 event를 중복하지 않으며 다른 결과는 충돌이다. 실패 transaction은 결과·cursor·event를 모두 남기지 않는다. 이 기록은 raw ACK/GC 허가나 기존 projection outbox가 아니다. 이벤트를 외부로 보내거나 기존 notifications stream에 섞지 않는다.

reorg 세대가 바뀌면 이전 계산을 버리고 새 Source를 만든다. 현재 canonical identity에 대한 과거 불변 결과가 있는 prefix를 최대128높이의 뒤쪽 탐색으로 찾고, own `reset` event와 함께 그 위치까지 cursor를 조정한다. 찾지 못하면 명시적 bootstrap부터 source DB를 재생한다. 원본 window가 아니라 영구 원천 입력을 사용한다. 이전 fork 결과는 explicit identity의 라이브러리 조회로만 읽으며 현재 HTTP API는 제공하지 않는다. definition 변경은 새 worker 이름/version으로 다룬다. 무제한 자동 재시작이나 임의 최신 높이 점프는 없다.

앱의 task 집합은 수집·기존 projection·선택 worker를 함께 시작하고, 하나의 종료/실패 시 모두 취소/join한다. DB/client는 그 뒤 닫는다. 각 worker의 transaction은 독립적이지만 저장소의 capture 쓰기 fence/디스크 commit과 HTTP limiter는 공유한다. 선택 worker의 추가 fresh 검사는 같은5 HTTP/s 예산을 소비하므로 처리량 향상이나 Arc450ms 목표 달성을 뜻하지 않는다. 성능은9번에서 동일 입력으로 비교한다.

## HTTP 조회

단일 체인은 다음 경로를 사용하고, 여러 체인은 `/chains/{id}`를 앞에 붙인다. 앱은 API route를 등록한다. 앱을 쓰지 않는 직접 API 조립에서는 `internal/api/indexing`을 연결해야 한다. 기존 인증/요청 제한 middleware를 따른다.

- `GET /indexing/status?limit=50&after=<unit-name>`: 최대100 feature/part, 이름 순서의 keyset 페이지. 다음 페이지는 응답 `next`를 사용한다. `after`는 최대256바이트다.
- `GET /indexing/workers/execution.summary.v1/blocks/123`: 해당 worker의 현재 canonical 완료 prefix에 포함된 block 결과.

status는 `source`의 chain/genesis·generation·plan digest/version·anchor·연속 `factsThrough`·최종 확정 높이·dataset coverage, `projectionHead`, 기능별 `epoch`/`completeThrough`/`lastApplied`/gap·definition·상태, worker별 완료 cursor/sequence를 반환한다. `projectionHead`는 core writer가 기록한 최고 commit head이며 중간 누락이 없다는 독립 증거가 아니다. 연속 완료는 feature/worker의 `completeThrough`를 확인한다.

source의 범위는 anchor 다음부터 `factsThrough`까지다. 수집 전에는 `factsThrough: null`이다. dataset 목록은 그 범위에 적용할 plan의 선언이며, 빈 범위를 수집했다고 표시하지 않는다. 현재 header/body/receipt는 complete encoded, transaction/log는 complete 입력에서 파생되며 account/code/storage/trace/blob/state input은 `unavailable`이다. feature 완료는 해당 source 범위와 설정된 definition을 처리했다는 뜻이고 모든 EVM 상태가 공급됐다는 뜻이 아니다.

선택 [상태 증명 공급](STATE_PROOFS.md)의 `feed.state_proofs`는 자체 자동 task와 atomic 입력/연속 cursor/event를 소유한다. 이 API의 별도 `stateProofs`는 exact selection/fromHeight·epoch·완료 height/hash·backlog·sequence를 보고한다. 전체 account_changes/storage_changes 완료로 올리지 않는다. parent 옵션은 `checkpoint_pending`과 exact `parent` 높이/hash를 추가한다. `required: true`는 부모 checkpoint와 선택 범위의 필수 상태 완료/GC gate를 고정한다. base Through·state-free feature/summary의 완료점은 독립적이다. `project_changes`는 별도 `stateChanges` 완료점과 자체 head/output/undo/apply-revert-reset event를 가진 worker를 연결한다. 기존 stateful feature의 전역 undo를 공유하지 않는다.

완료 기록이 없으면 `unreported`, 세대/definition/hash가 맞지 않으면 `stale`, 연속 범위가 부족하면 `backfilling`/`lagging`, source prefix까지 끝나면 `caught_up`이다. 비활성 기능은 `disabled`이다. 미완료 높이/hash는0으로 채우지 않고 null을 쓴다. worker의 `not_started`/`waiting`도 완료를 주장하지 않는다. `projectionMatchesCapture`는 projection head와 capture snapshot의 canonical 일치 여부다.

조회는 projection DB의 일관된 snapshot을 닫은 뒤 capture DB의 일관된 snapshot에서 현재 generation/identity를 대조한다. 한 SQL 연결과 별도 read-only API process도 지원하며 node RPC를 호출하지 않는다. 이는 응답에 표시된 capture generation의 상태이며 live network tip에 대한 fresh 조회나 두 DB의 분산 transaction은 아니다. 페이지 사이의 generation 변화는 응답으로 확인한다. 응답은 `Cache-Control: no-store`다.

worker 결과는 epoch와 exact source identity를 포함한다. 미래/미완료/미설정 결과는404, epoch 재동기화 중에는409, 구현/공급이 없으면503이다. 잘못된 요청은400, 손상/DB 오류는 세부 내용을 감춘500이다. 값/페이지/worker key는 SQL 파라미터로 바인딩한다. 운영 RPC URL·키·window 경로·삭제 token을 응답에 포함하지 않는다.

## 남은 범위

추가 상태 공급자는 획득 시점·hash/proof 결합·보존 기간·coverage selection과5 HTTP/s 예산을 먼저 정해야 한다. ETH68 block/receipt만으로 전체 account/contract storage 변화나 internal trace/blob sidecar가 공급된다고 가정하지 않는다. 지정 상태 worker는 자체 undo·head·이벤트/consumer 계약을 추가했다. 다른 cross-block 가변 집계를 독립화하려면 각 결과의 별도 계약이 필요하다. 기존 height 전역 undo를 공유하는 feature들을 병렬 writer로 실행하지 않는다.

선택 `source.lookup.v1` worker는 source DB만 읽어 블록/거래 해시별 canonical prefix 색인을 만들고, `/indexing/source/*`에서 버전이 있는 header/body/transaction/receipt/log 행을 제공한다. 별도 cursor·immutable root·fresh/동일5 HTTP/s 예산과 reorg/page 고정은 [원천 검색](SOURCE_QUERIES.md)을 따른다. 주소/topic 전역 검색과 기존 projected API의 대체는 포함하지 않는다.

선택 `source.search.v1`은 주소/topic/높이의 ordered posting과 블록별 immutable root를 별도 cursor/event transaction에 기록한다. 기존 source.lookup.v1 형식을 바꾸지 않는다. DB-only 고정 범위·cursor 페이지와 예산/운영 한도는 [원천 검색](SOURCE_QUERIES.md)을 따른다.
