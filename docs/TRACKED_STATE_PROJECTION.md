# 지정 상태 후처리 worker

`feed.state_proofs.project_changes: true`는 검증된 부모/현재 proof를 소비하는 독립 상태 worker를 실행한다. 선택한 주소의 consensus balance·nonce·존재 여부·code hash·storage root와 **선택한 슬롯 값**을 현재값으로 보관하고 블록별 before/after 변경 내역을 저장한다. 원본 proof/header/code는 영구 source DB에 남는다. 현재값에 code bytes를 중복 저장하지 않으며 전체 계정 변경·모든 storage·native USDC 회계·임의 eth_call을 제공한다고 표시하지 않는다.

## 설정과 범위

```yaml
source:
  feed:
    # 나머지 peer/window/chain 설정은 FEED_APP.md를 따른다.
    state_proofs:
      name: watched-v1
      start_height: 67000001
      required: true        # 선택: 첫 intake 전에 고정하는 proof/GC 계약
      project_changes: true # 부모 checkpoint 모드를 함께 켠다.
      poll_interval: 50ms
      accounts:
        - address: "0x1111111111111111111111111111111111111111"
          code: true
          slots:
            - "0x0000000000000000000000000000000000000000000000000000000000000003"
```

멀티체인은 `multichain.chains[].feed.state_proofs`에 둔다. 운영 config.yaml은 자동 변경하지 않았다. 이름은 `tracked.<proof stream name>`, reducer 버전은1이다. descriptor에 proof stream의 exact name/codec/selection/digest/fromHeight/parent/required를 포함해 고정한다. 같은 이름의 범위·정의를 바꾸지 않는다.

이미 parent 모드로 보존한 proof stream이라면 worker를 나중에 켜고 명시한 범위부터 DB 자료를 재생할 수 있다. 기존 optional stream이 parent 없이 만들어졌다면 자동 모드 변경은 충돌이다. [proof 설정](STATE_PROOFS.md)에 따라 새 명시적 범위/이름 또는 별도 DB/migration이 필요하다. `required`의 기존 source 계약은 첫 intake 뒤 추가/변경할 수 없다. `project_changes`만 끄면 이 worker와 현재 API 노출을 중단하고 이전 자료는 유지한다. 이것은 required proof 공급/GC 계약을 제거하지 않는다.

## 입력·현재값·변경 기록

별도 goroutine이 captured/proof DB prefix를 durable 큐로 읽는다. 선택 시작 높이부터 순서대로 한 블록만 활성화한다. proof가 늦으면 DB poll로 기다리고, channel/RAM에 큰 상태 목록을 쌓지 않는다. 완료를 주장한 proof bytes의 누락·손상·I/O 오류는 대기로 숨기지 않고 task 집합을 취소/join한다. pruned 원격 상태를0/latest로 바꾸지 않는다.

각 입력은 같은 source transaction 안에서 다음을 처리한다.

1. 현재 source epoch·exact canonical capture identity·proof 연속 완료와 부모 관계를 검사한다.
2. 두 시점의 header hash와 MPT/account/storage/code를 순수 reducer에서 다시 검증한다. callback은 DB/RPC를 호출하지 않으므로 PostgreSQL pool1에서도 중첩 transaction이 없다.
3. 현재 저장된 값이 증명된 before와 정확히 일치하는지 검사하고 after를 구성한다.
4. **불변 output·자체 undo·가변 현재값·연속 cursor·apply event**를 한 durable transaction에 기록한다. 성공 전 완료점이나 이벤트를 올리지 않는다.
5. 실제 기록 직전 기존 chain-wide 공유 pool로 **fresh canonical 검사1회**를 수행한다. proof/state RPC 조회는 이 worker에 없다.

Pebble WAL+sync와 PostgreSQL fsync/synchronous_commit 계약, 값 파라미터 바인딩을 그대로 사용하며 새 SQL/table/의존성을 추가하지 않는다. 기존 feature의 전역 undo/outbox·feature tables·raw ACK는 변경하지 않는다. proof/current/input/행 자료는 모두 chain namespace로 격리된다.

output에는 exact input identity·descriptor digest·before block·before/after value digest·변경 내역/digest·해당 블록의 전체 **선택 범위 after 값**이 들어간다. 값이 같아 변경 배열이 비어 있어도 정확히 검증된 입력으로 완료점을 전진시킨다. 계정 생성/삭제는 Exists의 before/after로 구분하며, 슬롯 부재의0도 원래 proof로 증명된 값이다. storage root의 변경이 선택하지 않은 슬롯의 값을 열거해 주는 것은 아니다.

현재값128KiB, 변경 내역256KiB를 상한으로 고정한다. 주소8개·전체128슬롯이라는 proof selection 한도 안에서 code bytes를 중복하지 않는다. output/undo/head는 실제 JSON bytes와 SHA256 및 exact ownership을 확인한다. status는 작은 cursor/manifest metadata만 읽는다. 값 API가 모든 요청마다 MPT를 다시 읽는 것은 아니며, 입력 적용과 worker 재시작은 원본 proof를 재검증한다.

## 재시도·재시작·재분기

commit 응답이 유실돼도 같은 입력의 retry는 기존 output/current/undo와 proof를 대조하고 fresh 검사만 다시 한다. apply event를 중복하지 않는다. 재시작은 마지막 완료 입력을 재검증하며 저장된 proof를 사용하므로 provider가 그 상태를 pruned한 뒤에도 재개할 수 있다.

source epoch가 바뀌면 이 worker의 현재 head에서 자신의 undo를 역순으로 적용한다. canonical prefix에 남은 exact input까지만 보존하며, 첫 입력 이전이면 부모 checkpoint의 값까지 되돌린다. 부모도 바뀌었거나 아직 새 prefix에 없다면 active head를 비우고 새 proof를 기다린다. old fork 출력/undo는 역사 자료로 보존하고 현재값으로 반환하지 않는다.

revert event들·복원한 head·새 epoch/cursor·reset event는 **한 transaction**으로 바뀐다. 도중 실패하면 일부 revert나 새 완료점이 남지 않는다. 최대128입력을 되돌리며 넘으면 ErrRebuildRequired로 중단한다. 128개만 일부 되돌린 상태를 완료로 게시하거나 과거 fork를 조용히 건너뛰지 않는다. epoch 재개도 최대64회다. fresh canonical 불일치는 기존 capture supervisor의 epoch 변경을 최대2분 기다린다. 모든 task는 취소/join 후 DB/client를 닫는다.

raw GC는 여전히 영구 source/proof 보존·선택 required 계약·24h/finality/lease/raw ACK를 따른다. 이 worker는 raw를 읽지 않으므로 처리 지연을 raw ACK로 위장하지 않는다. base Through·proof 완료·상태 후처리 완료·기존 feature/summary 완료는 서로 독립적이다.

## 조회 API와 이벤트 소비

모든 경로는 GET/read-only/no-store/RPC0회이며, 멀티체인은 `/chains/{id}` 아래에 mount한다.

| 경로 | 결과 |
| --- | --- |
| `/indexing/status` | 선택 `stateChanges`: descriptor·현재 epoch·nullable complete·captured Through 기준 backlog·sequence·status |
| `/indexing/state/current` | descriptor·epoch·exact active head와 선택 범위 현재값 |
| `/indexing/state/blocks/{height}` | 현재 epoch/canonical 완료 범위에 있는 output |
| `/indexing/state/events?after=0&limit=50` | 영구 apply/revert/reset event와 다음 sequence; limit1..256 |
| `/indexing/state/inputs/{height}/{hash}/{generation}` | event의 exact input에 해당하는 **역사 output**. 되돌린 fork도 반환하며 현재 완료를 뜻하지 않는다. |

status는 not_started/waiting/lagging/caught_up/stale를 구분한다. 현재 상태/블록 결과는 미완료404, epoch 재조정 중409, 미설정/미지원503, 잘못된 높이/hash/페이지400, I/O·손상500을 반환한다. 내부 경로·DB 비밀·원래 오류 문자열을 HTTP body에 노출하지 않는다. 이벤트의 after는 전체 sequence 이하이어야 한다. API 조회와 worker 설정 변경/다른 query snapshot 사이에는 시점 차이가 있을 수 있으므로 소비자는 epoch/identity/sequence를 확인한다.

이벤트는 이름/descriptor별로1부터 증가한다. apply는 해당 input의 after 값을 반영하고, revert는 같은 input의 changes.before로 되돌린다. reset은 유지된 입력/point를 알려 주며 input이 null이면 active 상태를 비운다. 첫 apply의 전체 선택 after 값으로 초기화할 수 있다. 외부 소비자는 event input의 역사 output을 가져와 **자기 DB 변경과 소비한 sequence를 같은 transaction**에 저장한다. 재전달은 (descriptor digest, sequence)로 중복 제거한다. 서버가 외부 sink transaction/ACK를 대신하지 않으며 delivery는 at least once다. 이벤트를 읽었다고 raw ACK·finality·source completion이 올라가지 않는다. 별도 보존/삭제 정책을 도입하기 전 output/undo/event는 자동 삭제하지 않는다.

라이브러리는 `evmstate.RunProjection`/`Projector`와 `capture.ReadStateProjectionHead/Output/Events/Input`을 사용한다. 기존 arbitrary feature handler를 이 source transaction에 연결하는 일반 병렬 writer API는 아니다.

## 검증과 남은 일

독립 Geth trie builder/prover로 balance/nonce/code/slot 변경·계정 삭제/생성을 구성해 before/after와 실제 두 DB 결과를 대조한다. partial apply5지점·partial reset5지점·uncertain commit·fresh 불일치·재시작·fork/역사 결과·128 undo 한도·누락 proof 오류·다섯 task join·read-only pool1 API를 검사한다. 환경별 결과는 작업 기록3.49와 `/private/tmp/indexer-state-projection-20261016/validation.json`에 남긴다.

추가 fresh 요청도 기존5 HTTP/s 예산을 공유한다. 이 opt-in worker를 켰다는 사실만으로 Arc2블록/s·450ms를 달성했다고 주장하지 않는다. 실제 Arc peer·공개 RPC proof 지원·외장 mount·24시간 처리량/보존·공급 지연의 운영 정책, native USDC 해석과 다른 stateful 기능·trace/blob/검색은 남아 있다.
