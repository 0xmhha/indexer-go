# E1 구현 체크리스트

정본: [구현 계획](03-plan.md). 2회 실행·노드 소스 미수정·기존 pool4·RPC5 physical HTTP/s·최종 공개 직전 fresh를 유지한다. 실제 노드 서비스·peer·성능 측정·디스크 포맷·GC 활성화는 보류다.

## 현재

U00~U08은 필수 CI·squash/main 동기화를 완료했다. U04~U08 통합은 [PR #213](https://github.com/0xmhha/indexer-go/pull/213), merge `08098f2`다. 최신 커밋의 전체 PostgreSQL 검사와 두 단일 200회 race 검사를 포함한 네 CI가 모두 통과했다.

U09~U11의 구현과 필수 로컬 검증을 완료했다. 실제 EVM/WBFT→PostgreSQL/API·IPC/ACK/reorg/restart, 최종 root/wire/executor 전체 race, 전체 PostgreSQL 회귀, build/fmt/vet/lint·예제·cache·오프라인 운영·secret 검사가 GREEN이다. 새 receiver/runtime/exporter/node lifecycle과 실제 PG/IPC 통합은 각각 단일 GOMAXPROCS=1/race/count=200을 통과했다. 실패한 부분 실행을 합산하지 않았다.

최종 통합 [PR #214](https://github.com/0xmhha/indexer-go/pull/214)의 최신 head에서 필수 CI4가 통과해야 ready·squash/main 동기화한다. 해당 PR이 CI·merge commit의 정본이며 기능별 계약과 로컬 검증 기록은 아래에 유지한다. n=U11, n-1=U10, n-2=U09의 계약·검사 근거는 continuity에 유지한다.

| ID | 상태 | 요구 실패(RED) | 현재 GREEN·완료 근거 | PR·merge |
| --- | --- | --- | --- | --- |
| U00 | 완료 | 문서 작업, 해당 없음 | 계획·문서 링크·JSON, 필수 검사 | [#210](https://github.com/0xmhha/indexer-go/pull/210), `9f01b09` |
| U01 | 완료 | wire API stub6, raw/조립 상한2 | codec·framing·parts·취소/race·단일200회·필수 CI | [#210](https://github.com/0xmhha/indexer-go/pull/210), `9f01b09` |
| U02 | 완료 | recorder/Replay stub·clear/빈 code/조기 상한 | 실제 EVM/WBFT·Extra·fee payer·독립 trie oracle·commit 금지·race/200회·필수 CI | [#211](https://github.com/0xmhha/indexer-go/pull/211), `4525ac6` |
| U03 | 완료 | spool stub6·identity/temporary3·ACK quota2·종료 drain2 | durable spool/ACK·quota·복구·취소/join·race/200회·전체 회귀·필수 CI | [#212](https://github.com/0xmhha/indexer-go/pull/212), `ec40728` |
| U04 | 완료 | control3·account3·partition1·slot/time4·code3·core3·cursor1·creation2 | 실제 PG/race, 기존 port/undo/count/migration, control 단일200회. [계약](06-db-contract.md) | [#213](https://github.com/0xmhha/indexer-go/pull/213), `08098f2` |
| U05 | 완료 | Prepare2·scheduler4·backend1·decoder/encoder 상한 각1 | race·단일 GOMAXPROCS=1/count=200, 입력 소유권/commitment/생성 증거. [계약](07-scheduler-contract.md) | [#213](https://github.com/0xmhha/indexer-go/pull/213), `08098f2` |
| U06 | 완료 | publication3·feature2 | PG/race, all-seven/fresh/undo/atomicity/중복·post-response fence. [계약](08-publication-contract.md) | [#213](https://github.com/0xmhha/indexer-go/pull/213), `08098f2` |
| U07 | 완료 | PG read3·state1, API snapshot2·state2·route/cache/chain 각1 | 모든 현재 TestExecution PG/race, API/race·기존 parser/limits/cache/종료 검사. [계약](09-read-contract.md) | [#213](https://github.com/0xmhha/indexer-go/pull/213), `08098f2` |
| U08 | 완료 | marker/복구4·상태1·zero coverage1·Controller3·feature2·duplicate/fresh 각1 | PG/race, 단계별 원자성·128/129·retarget·실제 read fence·취소/join. [계약](10-rewind-contract.md) | [#213](https://github.com/0xmhha/indexer-go/pull/213), `08098f2` |
| U09 | 구현·로컬검증 완료 | spool generation3·pipeline1·manifest gap1·PG confirm2·IPC2·fetch confirm1 | spool/PG/IPC·collector·재연결·short reorg focused race GREEN. [계약](11-ipc-contract.md) | [#214](https://github.com/0xmhha/indexer-go/pull/214), squash/main은 PR 상태 참조 |
| U10 | 구현·로컬검증 완료 | native4·binding1·config/preflight/control·balance/read·runtime/feature/node | native/config/app/실제 PG/nodebackend focused race GREEN. [계약](12-runtime-contract.md) | [#214](https://github.com/0xmhha/indexer-go/pull/214), squash/main은 PR 상태 참조 |
| U11 | 구현·로컬검증 완료 | creation routing/witness·실패 생성·fresh 재연결·종료·profile capability·socket 원자 공개 | 실제 EVM/WBFT frozen fixture·PG/API·IPC ACK 유실/reorg/restart race GREEN. [계약](13-integration-contract.md) | [#214](https://github.com/0xmhha/indexer-go/pull/214), squash/main은 PR 상태 참조 |

## 검사 판정과 PR

- 기능 누락 때문에 실제 실행한 검사가 실패해야 RED다. 컴파일·캐시·환경 실패를 RED로 세지 않는다.
- 각 기능의 같은 검사를 GREEN으로 확인하고 현재 문서·체크리스트·최근 두 기능 context를 갱신한다.
- 통합 단위가 준비되면 root/wire/executor 전체 race, 실제 fsync-on PG, build/fmt/vet/lint, cache lifecycle, examples, 오프라인 운영 검사, secret 검사를 완료한다.
- 새 동시성 검사는 GOMAXPROCS=1/race/단일 count=200을 완료한다. 부분 실행 횟수를 합산하지 않는다.
- 영어 Conventional Commit·명시 경로 stage·draft PR 연결, 필수 lint/test/postgres/examples CI 모두 통과 후 ready·squash merge·main fast-forward 순서다.
- 성능이나 실제 운영을 오프라인 기능 GREEN으로 주장하지 않는다. 목적 없는 호스트 부하 확인·SLO 반복은 하지 않는다.

## 재현 가능한 근거

U01: execwire 모듈 `go test -race ./...`; concurrency `GOMAXPROCS=1 go test -race -count=200 -timeout=5m -run '^TestConn' ./...`. payload 소유권·SHA/경계·TTL·partial I/O·취소를 검증한다. SHA는 전송 무결성이며 EVM/합의 증명이 아니다.

U02: executor 모듈 `go test -race ./...`. 공식 고정 fork의 실제 EVM/WBFT 첫 실행과 recorder Replay를 대조하고, 별도 양방향 trie oracle 및 record-only omission fault를 검사한다. upstream 수정·두 번째 state commit·DAO/beacon prelude를 허용하지 않는다. sender-cache의 정확한 upstream 상주 함수만 goleak 예외이며 소유 worker는 제외하지 않는다.

U03: executor/spool race와 `GOMAXPROCS=1 go test -race -count=200 -timeout=5m -run '^TestConcurrentCancellationAndCloseJoin$' ./spool`. [spool 계약](05-spool-contract.md)을 따른다. #212 CI에서 발견한 실제 종료 outbox 누락은 accepted queue drain·publish/stop fence·relay-before-bus join으로 수정했고 두 RED 및 단일200회/전체 CI를 통과했다.

U04~U08: `scripts/test-postgres.sh ./pkg/storage/postgres -race -run '^TestExecution'`. 기능 테스트는 격리 schema와 fsync-on 임시 DB를 사용한다. U04의 before CAS·tombstone·clear·precision·creation 및 U06 공개·U07 snapshot이 같은 검사군에 포함된다. 실제 EVM 기반 end-to-end와 최종 모든 기능 판정은 U11이다.

U05: `GOMAXPROCS=1 go test -race ./pkg/execution -count=200 -timeout=10m`, `go test -race ./pkg/chainfeed/evmdata`. 성공 CREATE의 빈 runtime/생성자 소멸을 receipt 증거로 유지하며 실패 receipt는 제외한다. encoder가 decoder JSON token budget을 초과하는 결과를 만들던 추가 RED도 수정했다.

U06/U07: `go test -race ./pkg/fetch -run '^TestExecutionFeature'`; `go test -race ./pkg/api/... ./internal/api/rest -run '^TestExecution'`. 공개 전 SQL flush/undo, post-response generation, retry exact proof, request snapshot·큰 정수 정밀도·REST cache 차단을 검사한다. 새 root/API 및 실제 PG 동시성은 각각 단일 GOMAXPROCS=1/race/count=200을 통과했다. build/vet/lint/cache/examples/오프라인 운영과 실제 PG adapter suite·전체 root/wire/executor race를 통과했다. 최신 commit의 전체 PG는 #213 필수 CI에서 통과했다.

각 merge의 영구 CI 결과는 위 PR에 있고, 구현 의도·현재 계약·검사 이름은 해당 문서와 테스트 소스에 있다. 임시 로그 경로만을 유일한 근거로 쓰지 않는다. 오래된 진행 문구·실험 결과는 Git history에서 확인한다.

GraphQL schema/method 기준 파일과 stdlib-only wire의 port 의존 경계 검사는 현재 기능과 일치한다. 최신 commit의 전체 PostgreSQL·root 기능·race200·lint/examples CI는 [#213](https://github.com/0xmhha/indexer-go/pull/213)에서 모두 통과했다. 과거 실패 원인과 수정 전 자료는 Git history에서 확인한다.
