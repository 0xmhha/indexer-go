# E1 구현 계획

상태: 2026-10-24 사용자가 추천 결정과 구현·검증·PR·CI·squash merge를 위임했다. 이 계획을 기준으로 순서대로 구현한다. 현재 완료 여부는 [체크리스트](checklist.md), 재개 지점은 [연속성 기록](continuity.json)을 따른다.

## 1. 범위와 완료 판정

StableNet 실행부와 PostgreSQL indexer를 연결한다. 실행부는 별도 모듈에서 공식 `github.com/stable-net/go-stablenet`의 `v1.0.1-0.20261013052903-4d17eea018c8`을 import하고, 원본 프로젝트를 고치지 않는다. commit은 `4d17eea018c8dd579409db30bfd27de79c4f4121`로 고정한다. 1차 실행은 정상 블록 import, 2차 실행은 거래별·Finalize 변화 추출이다. 2차 실행은 노드 상태를 commit하지 않는다. 실행부는 PostgreSQL에 접근하지 않는다.

E1은 다음을 코드와 오프라인 기능 검사로 완료한다.

- 실행 결과의 결정성, state/receipt root, 거래별 전후 값과 블록 단위 변화.
- bounded wire·spool·queue, 높이/hash/generation에 묶인 멱등 재전송과 durable ACK.
- 표별 writer 7개와 기존 기능 writer, 상태 이력과 현재 값, 공개 높이.
- 일부 저장·취소·재시작·reorg·오류 시 일관성과 공개 차단.
- StableNet 전용 앱 연결, 기존 RPC 경로 호환, 모듈별 CI·문서.

실제 노드 서비스를 시작하거나 peer에 접속하지 않는다. 설치된 Anvil을 사용하는 선택 E2E도 이번 검사 환경의 PATH에서 제외하여 시작하지 않는다. 해당 skip을 보고하며 mock HTTP/IPC와 EVM 라이브러리 검사는 실행한다. 추가 성능 측정과 disk provisioning/GC 활성화도 보류한다. 라이브러리를 이용하는 결정적 EVM 기능 검사는 노드 서비스 운영과 구분한다. 녹화 자료를 준비하는 공개 RPC 읽기는 필요할 경우 체인별 공용 5 HTTP/s 예산 안에서만 한다. 속도 목표의 실측, 실제 peer·운영 복구는 검증 보류로 남기며 오프라인 성공을 그 증거로 사용하지 않는다.

## 2. 확정한 구현 계약

### 2.1 연결 4개와 writer 7개

writer 수는 goroutine/queue의 수이며 DB 연결을 계속 소유하는 수가 아니다. 기존 PostgreSQL pool의 `max_conns=4`를 늘리거나 별도 write pool을 만들지 않는다.

- API는 기존 pool의 읽기 예약 1개를 유지한다.
- 표별 저장 작업은 한 번에 최대 2개만 connection을 사용한다. writer는 DB 작업 직전에 permit을 받고 transaction 종료 후 즉시 반환한다.
- 나머지 여유는 공개 transaction과 짧은 관리 조회가 사용한다. API의 기존 유휴 연결 차용은 유지한다.
- permit을 가진 채 다른 writer·queue·RPC를 기다리지 않는다. 저장에 필요한 입력은 permit 전에 조립한다.
- 공개 transaction만 fresh RPC를 기다릴 수 있다. 이때 표별 작업 2개와 API 1개까지 합쳐도 pool 상한은 4다. 취소·오류는 transaction과 permit을 반환한다.
- 같은 writer의 높이는 연속으로 진행한다. writer 간 조합은 완료된 요청의 순서대로 공정하게 배치한다. worker 7개를 위해 연결 7개를 요구하는 기존 설계 문구는 이 계약으로 대체한다.

### 2.2 미공개 저장과 공개 commit

표별 writer의 commit은 **미공개 자료의 durable 저장**이다. canonical block의 최종 처리 완료나 source ACK가 아니다. 각 commit은 자기 행과 `(writer, generation, height, hash, digest)` 완료 기록을 함께 남긴다. highest height만 보고 연속 완료를 추정하지 않는다.

블록·거래·receipt·log는 해당 writer가 자기 표에 저장한다. 계정·storage·contract writer는 불변 이력과 작업용 최신 값을 저장한다. 기존 기능 writer는 7개 결과가 같은 generation·height·hash로 준비된 뒤 한 블록 transaction을 수행한다. 이 transaction에서 다음을 함께 commit한다.

1. 블록 입력을 사용한 기존 기능 표 갱신. 핵심 표의 중복 쓰기는 생략한다.
2. feature cursor, outbox와 공개 높이/hash.
3. 공개 직전의 새 canonical 검사와 응답 후 generation/hash fence 확인.

공개 높이는 이 transaction으로만 전진한다. 이 commit 뒤에만 API·outbox가 해당 블록을 내보내고, 실행부에 `Committed(height, hash, generation)`를 보낸다. 공개 전 오류는 ACK하지 않는다.

### 2.3 공개 높이와 최신 값 조회

높이 `v`까지 공개됐으면 API가 읽는 블록·거래·receipt·log는 `height <= v`여야 한다. hash 조회, pagination, 통계·주소 색인에도 같은 경계를 적용한다. 실행 모드에서만 읽기 경계를 설치하며 기존 RPC/declared 모드의 SQL 의미는 바꾸지 않는다.

작업용 `state_accounts`, `state_storage`, `state_contracts`의 `last_height`만 필터링하지 않는다. 공개 높이보다 앞선 값이 이미 쓰였으면 **불변 이력에서 공개 시점의 값을 조회**한다. 계정 삭제와 storage clear를 tombstone으로 기록한다. clear 이전 슬롯을 현재 값으로 되살리지 않는다. 시작 상태가 없는 범위는 coverage 오류이며 임의로 0을 만들지 않는다.

여러 변화의 순서는 `(height, ordinal)`로 표현한다. transaction index와 hash는 별도 보존한다. baseline은 블록 실행 전, 거래 변화는 거래 순서, Finalize 변화는 마지막에 놓는다. Finalize의 `tx_index=-1`를 정렬 순서로 사용하지 않는다.

API 읽기는 공개 generation과 일관된 read transaction에 묶는다. 읽는 동안 reorg의 첫 fence와 충돌하지 않도록 rewind와 공통인 transaction-level advisory shared lock을 사용한다. read-only transaction에서 row lock을 요구하지 않는다. try-lock 실패는 연결을 점유하며 기다리지 않고 재시도 오류로 반환한다. rows를 닫거나 취소하면 lock과 연결을 반환한다. 새 조회가 rewinding 상태를 만나면 명시적 retryable 오류를 반환한다. 모든 결과를 0/없음으로 바꿔 성공시키지 않는다.

### 2.4 reorg와 재시작

정상 처리도 reorg도 control row와 generation으로 fence한다. 준비 중인 이전 generation의 작업이 새 가지에 commit하지 못하게 한다.

1. 진행 중 공개 commit과 읽기가 끝난 뒤 control row를 독점한다. rewind target과 새 generation을 durable 기록하고 새 공개를 막는다.
2. 수신/분배의 이전 generation 작업을 취소하고 join한다. 기존 데이터의 무조건 삭제나 TTL 해제를 하지 않는다.
3. 표마다 cursor와 이력을 되돌리고, 최신 값은 남은 이력 또는 대상 변화의 before 값에서 복원한다. 새 계정·storage clear·contract 소멸을 포함한다.
4. 기존 기능 표는 기존 undo로 되돌린다. 공개 높이/hash를 마지막 공통 부모로 옮기고 모든 writer의 결과를 확인한다.
5. rewind 기록을 없애고 조회·처리를 재개한다. 중간 종료 뒤에는 이 절차부터 이어간다.

128블록을 넘는 reorg, 부족한 undo/부모 상태, hash 불일치는 멈춘다. 일부 표만 바뀐 동안 조회를 허용하지 않는다. 최초 시작도 미완료 공개/rewind를 검사한 뒤 API를 연다. cursor의 높이만 같은 경우가 아니라 hash와 generation까지 같은 경우에만 멱등 완료로 인정한다.

### 2.5 fresh와 RPC 예산

미공개 저장에는 매번 새 HTTP 요청을 추가하지 않는다. 그 자료는 canonical 완료로 인정되지 않으며 epoch·hash에 묶인 입력이다. **공개 transaction 직전 한 번의 새 canonical 조회**가 공개·ACK의 기준이다. 캐시·이전 블록의 결과·이전 호출의 결과로 대신하지 않는다. RPC 응답 뒤에도 control generation을 다시 확인한다.

이름이 다른 개별 요청이더라도 기존 pool을 통해 체인별 5 HTTP/s를 공유한다. retries·failover·batch fallback도 물리 시도에 포함한다. token metadata는 E1에서 RPC를 유지한다. 실행부의 peer 통신은 HTTP RPC 호출로 세지 않지만 RPC를 우회 호출하는 별도 client를 만들지 않는다. 기존 facts 경로의 fresh·durability·ACK 검사는 유지한다.

### 2.6 실행 기록과 검증

2차 실행의 `state.Database`/trie wrapper는 before 값을 첫 변경 전에 읽고, 같은 거래 안에서는 최종 after 값과 묶는다. reverted 변경과 성공한 변경을 구분하며, fee payer·NativeManager·precompile·Finalize가 같은 상태 경계를 통과하는지 검사한다.

state root 일치만으로 내보낸 기록의 무누락을 주장하지 않는다. **원래 trie 갱신은 실행하면서 기록만 버리는 fault**도 검사한다. 거래별 기록을 합친 결과와 별도의 양방향 trie 비교(계정·storage, 삭제 포함)를 대조한다. 전후 값이 현재 작업 상태와 다르면 저장하지 않는다. 노드가 보관한 receipt와 재실행 receipt root도 대조한다.

### 2.7 spool과 메모리 상한

wire는 version 1, 최대 8MiB frame이며 큰 블록 결과를 parts로 나눈다. 전체 블록·part 수·조립 시간·바이트에 별도 상한을 둔다. 길이를 검증한 뒤 할당한다.

spool은 fsync 뒤 전송하고 ACK와 같은 높이/hash/generation을 확인한 뒤 정리한다. 부분 파일·잘린 tail·디스크 부족·재전송·ACK 유실을 검사한다. 큐는 블록 수와 바이트를 함께 제한하며 active 작업도 계산한다. 입력을 삭제해서 공간을 만들지 않는다.

노드 DB의 자동 삭제는 E3이며 E1에 넣지 않는다. 실제 p2p 중단/재개와 pathdb의 되감기 동작은 구현한 코드와 검증 범위를 구분해 문서화한다.

## 3. 기능 단위와 순서

각 기능 단위는 소비자까지 연결하는 작은 PR로 마친다. 기능이 아닌 U00 계획 문서는 U01의 첫 PR에 함께 포함하여 문서 전용 CI 회전을 추가하지 않는다. RED는 해당 요구 때문에 실패해야 하며, 실패한 원본·명령·기준 commit을 보존한다. 구현 후 같은 검사를 GREEN으로 돌린다. 전체 기능을 끝까지 만든 뒤 테스트를 붙이는 방식을 쓰지 않는다.

| ID | 기능·파일·소비자 | RED / GREEN과 완료 기준 | 위험과 롤백 |
| --- | --- | --- | --- |
| U00 | 계획, 설계, checklist, continuity, 현재 작업 목록 | 문서 링크·계획 계약·상태 일치. 이 단위는 기능 구현이 아니다 | 구현 완료로 표시하지 않는다. 이전 문구는 Git history로 확인한다 |
| U01 | `execwire/` 별도 모듈. 실행부와 indexer가 소비 | deterministic codec, framing/parts, 제한·잘림·취소·잘못된 version/hash 거절. stdlib-only와 별도 CI 검사 | 할당·프로토콜 위험. 아직 앱에 연결하지 않는다 |
| U02 | `x/stablenet/executor/` 별도 모듈, 상태 recorder와 오프라인 EVM fixture | 2회 실행 parity, root·receipt, 거래/Finalize·삭제·revert·Extra·fee payer, 기록 누락 fault | go-stablenet API 가정이 틀리면 이 단위에서 해결한다. 노드 서비스는 시작하지 않는다 |
| U03 | 실행부 durable spool과 ACK | fsync 전 실패, 잘림, capacity, 잘못된 ACK, 중복 재연결, 재시작. 원본 보존 | spool 형식은 version으로 fence. ACK되지 않은 결과를 삭제하지 않는다 |
| U04 | execution control·migration 0014·상태 port | binding, 연속 cursor, before-value CAS, tombstone/baseline, 공개 이력 조회. 실제 임시 PostgreSQL 기능 검사 | 기존 모드 migration/undo 회귀. 새 설정은 기본 비활성 |
| U05 | `pkg/execution` 7개 queue와 DB 스케줄러 | 연결 최대 4, 표별 순서·공정성·취소·permit 반환, 느린 writer backpressure | deadlock·고갈. 실행 모드만 비활성화하면 기존 경로 사용 |
| U06 | 공개 transaction과 기존 feature writer 연결 | 일부 표만 완료하면 비공개, feature/visible/outbox 원자성, fresh 전후 오류·ACK 유실 | 기존 표 중복 쓰기·fresh 누락. RPC 모드와 parity 검사 |
| U07 | API read fence와 상태 조회 | hash/목록/통계/상태/구독에서 미공개 결과 차단. 최신 값·삭제·slot clear·취소 검사 | 필터만으로 옛 값이 사라지는 결함을 RED로 고정. 기존 모드는 동일 결과 |
| U08 | reorg·restart coordinator | 각 단계 중단 후 복구, stale generation, 128 경계, 조회 차단, 중복 hash | 불완전 rollback을 완료로 처리하지 않는다. 원본과 journal 보존 |
| U09 | IPC receiver·assembler와 실행부 exporter 연결 | part 누락/중복/순서/타임아웃, canonical 범위 걷기, 재연결·spool catchup, cancel/join | 이벤트 하나를 블록 하나로 추정하지 않는다. fake chain backend로 결정적 검사 |
| U10 | node 조립과 `pkg/app` 실행 모드 | chain/DB/pool/socket/config 거절·기본 off·root binary 격리, 기존 RPC 호환. build와 주입 backend 기능 검사 | 실제 node.Start/peer 운영은 보류. 초기 상태가 없으면 명확히 시작 거절 |
| U11 | 전체 오프라인 통합·CI·사용 문서 | 녹화/실제 EVM fixture 기반 DB/API parity, 장애 복구, 모든 새 모듈 build/vet/lint/race, 새 동시성200회, 필수 CI | 성능·실제 peer·운영 검증 보류를 완료로 포장하지 않는다 |

고정한 공식 모듈의 checksum 검증과 `core`, `state`, WBFT backend, `eth`, `node`의 별도 모듈 컴파일·실행 파일 링크를 Go 1.26.8에서 확인했다. 생성 파일은 실행하지 않았다. 실제 재실행 parity·노드 lifecycle은 이 준비 검사의 완료 범위가 아니며 U02·U10에서 각각 검사한다.

각 단위의 구체 파일명은 실제 계약에 맞춰 checklist에 고정한다. EVM 모듈·wire·indexer의 의존성을 섞거나 go-stablenet에 패치를 넣지 않는다. 준비된 결과를 신뢰할 수 없는 경우 다음 단계의 표를 만들기 전에 해결한다.

## 4. 검사와 PR 흐름

1. 요구를 검사로 작성하고 실패 원인을 기록한다. 기능 누락 때문에 실패한 RED와 환경/컴파일 실패를 구분한다.
2. 최소 구현 후 같은 검사를 GREEN으로 확인한다. 변경된 기능과 n-1/n-2 기능의 관련 계약도 확인한다.
3. 현재 문서·설정·체크리스트를 갱신한다. “현재” 문서에 오래된 수치·미결 문장·모순을 누적하지 않는다. 설계와 계획은 현재 정본을 유지한다.
4. 커밋 전에 build/fmt/vet/lint와 필수 race·cache lifecycle·PostgreSQL·examples를 완료한다. 성능/라이브 검사는 opt-in 상태를 유지한다. 새 동시성 검사는 GOMAXPROCS=1/race/단일 count=200을 완료한다.
5. 경로를 명시해 stage하고 staged/history secret 검사를 수행한다. 영어 Conventional Commit을 사용하며 `codex` 접두사·공동 저자 표기를 붙이지 않는다.
6. draft PR을 생성·연결하고 검사가 끝나면 ready로 바꾼다. 필수 lint/test/postgres/examples가 모두 통과해야 squash merge한다. 보호 규칙을 우회하지 않는다.
7. main을 fast-forward로 동기화한 뒤 다음 단위의 브랜치를 만든다. 다른 시스템의 변경과 충돌하면 보존·통합하고 미완료 작업을 덮어쓰지 않는다.

실패 시 같은 실행을 목적 없이 반복하지 않는다. 원인·수정·검사 범위를 기록하고 새 코드나 새 실패가 요구하는 검사만 반복한다. 다른 프로젝트의 프로세스·container·volume·캐시는 유지한다.

## 5. 연속성 기록

`checklist.md`는 각 기능의 계약·RED·GREEN·PR·merge·다음 단위를 한 곳에서 추적한다. `continuity.json`은 현재 단위 n, 직전 완료 n-1과 n-2를 유지한다. 각 항목은 파일·공개 계약·검사 근거·미해결 사항·merge commit을 포함한다.

재개할 때 이 계획, checklist, continuity를 먼저 읽고 Git 상태와 대조한다. 완료 항목을 반복 구현하지 않는다. 검사 로그는 commit/PR 또는 재현 가능한 명령으로 잇고, 사라질 수 있는 임시 경로만을 유일한 근거로 사용하지 않는다. 사용자 원문은 최신 결정의 근거로 유지하되 오래된 작업 상태를 복사하지 않는다.

## 6. E1 이후의 작업

- E2: 시작 모드·전체 상태 bootstrap·검증 도구. 준비되지 않은 빈 DB에서 head로 즉시 시작하는 운영 경로.
- E3: 노드 DB/원본의 보존·정리. canonical·reorg·ACK·lease·복구를 포함한 삭제 검증.
- E4: 기존 기능 표의 writer 분리와 중복 잔액 정리.
- E5: 체인별 실행부 확대. 비EVM 체인은 모델 결정을 별도로 한다.
- E6: 합의 검증에 근거한 canonical/finality 판정과 RPC 의존 축소.
- 별도 운영: 실제 peer·APFS·백업 복원·WAL 보관소·Docker VM 감시·장기 운전·속도 비교. E1 오프라인 완료와 분리한다.
