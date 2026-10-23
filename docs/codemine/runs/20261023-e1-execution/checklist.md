# E1 구현 체크리스트

정본: [구현 계획](03-plan.md). 2회 실행·노드 소스 미수정·기존 pool4·RPC5 physical HTTP/s·최종 공개 직전 fresh를 유지한다. 실제 노드 서비스·peer·성능 측정·디스크 포맷·GC 활성화는 보류다.

## 현재

U08까지 기능 RED/GREEN을 마쳤다. durable marker·원자적 feature/core/state 복구·단계별 fault·128/129·retarget·queue 취소/join을 확인했으며, U04~U08의 기능·전체 race·동시성200회와 로컬 필수 검사를 완료하고 PR 통합 검증을 준비한다.

U04~U08은 같은 migration/publication/rewind 경계의 통합 PR로 검증한다. 기능 GREEN과 commit·CI·merge 완료를 구분한다. 마지막 merge는 #212(`ec40728`)이며 현재 통합 브랜치는 commit/PR·필수 CI를 기다린다. n=U08, n-1=U07 조회, n-2=U06 공개이며 [continuity](continuity.json)에 파일·계약·재개 지점을 유지한다.

| ID | 상태 | 요구 실패(RED) | 현재 GREEN·완료 근거 | PR·merge |
| --- | --- | --- | --- | --- |
| U00 | 완료 | 문서 작업, 해당 없음 | 계획·문서 링크·JSON, 필수 검사 | [#210](https://github.com/0xmhha/indexer-go/pull/210), `9f01b09` |
| U01 | 완료·입력 상한 후속 수정은 통합 중 | wire API stub6, raw/조립 상한2 | codec·framing·parts·취소/race·단일200회·필수 CI | [#210](https://github.com/0xmhha/indexer-go/pull/210), `9f01b09` |
| U02 | 완료 | recorder/Replay stub·clear/빈 code/조기 상한 | 실제 EVM/WBFT·Extra·fee payer·독립 trie oracle·commit 금지·race/200회·필수 CI | [#211](https://github.com/0xmhha/indexer-go/pull/211), `4525ac6` |
| U03 | 완료 | spool stub6·identity/temporary3·ACK quota2·종료 drain2 | durable spool/ACK·quota·복구·취소/join·race/200회·전체 회귀·필수 CI | [#212](https://github.com/0xmhha/indexer-go/pull/212), `ec40728` |
| U04 | 기능 GREEN·미커밋 | control3·account3·partition1·slot/time4·code3·core3·cursor1·creation2 | 실제 PG/race, 기존 port/undo/count/migration, control 단일200회. [계약](06-db-contract.md) | U04~U08 통합 예정 |
| U05 | 기능 GREEN·미커밋 | Prepare2·scheduler4·backend1·decoder/encoder 상한 각1 | race·단일 GOMAXPROCS=1/count=200, 입력 소유권/commitment/생성 증거. [계약](07-scheduler-contract.md) | U04~U08 통합 예정 |
| U06 | 기능 GREEN·미커밋 | publication3·feature2 | PG/race, all-seven/fresh/undo/atomicity/중복·post-response fence. [계약](08-publication-contract.md) | U04~U08 통합 예정 |
| U07 | 기능 GREEN·미커밋 | PG read3·state1, API snapshot2·state2·route/cache/chain 각1 | 모든 현재 TestExecution PG/race, API/race·기존 parser/limits/cache/종료 검사. [계약](09-read-contract.md) | U04~U08 통합 예정 |
| U08 | 기능 GREEN·미커밋 | marker/복구4·상태1·zero coverage1·Controller3·feature2·duplicate/fresh 각1 | PG/race, 단계별 원자성·128/129·retarget·실제 read fence·취소/join. [계약](10-rewind-contract.md) | U04~U08 통합 예정 |
| U09 | 대기 | 미실행 | IPC/exporter/canonical walk/spool generation 전환 | — |
| U10 | 대기 | 미실행 | node 조립·실행 모드·격리·config/lifecycle | — |
| U11 | 대기 | 미실행 | 실제 EVM→DB/API 오프라인 parity·장애/CI·문서 완료 | — |

## 검사 판정과 PR

- 기능 누락 때문에 실제 실행한 검사가 실패해야 RED다. 컴파일·캐시·환경 실패를 RED로 세지 않는다.
- 각 기능의 같은 검사를 GREEN으로 확인하고 현재 문서·체크리스트·최근 두 기능 context를 갱신한다.
- U04~U08 전체가 준비되면 root/wire/executor 전체 race, 실제 fsync-on PG, build/fmt/vet/lint, cache lifecycle, examples, 오프라인 운영 검사, secret 검사를 완료한다.
- 새 동시성 검사는 GOMAXPROCS=1/race/단일 count=200을 완료한다. 부분 실행 횟수를 합산하지 않는다.
- 영어 Conventional Commit·명시 경로 stage·draft PR 연결, 필수 lint/test/postgres/examples CI 모두 통과 후 ready·squash merge·main fast-forward 순서다.
- 성능이나 실제 운영을 오프라인 기능 GREEN으로 주장하지 않는다. 목적 없는 호스트 부하 확인·SLO 반복은 하지 않는다.

## 재현 가능한 근거

U01: execwire 모듈 `go test -race ./...`; concurrency `GOMAXPROCS=1 go test -race -count=200 -timeout=5m -run '^TestConn' ./...`. payload 소유권·SHA/경계·TTL·partial I/O·취소를 검증한다. SHA는 전송 무결성이며 EVM/합의 증명이 아니다.

U02: executor 모듈 `go test -race ./...`. 공식 고정 fork의 실제 EVM/WBFT 첫 실행과 recorder Replay를 대조하고, 별도 양방향 trie oracle 및 record-only omission fault를 검사한다. upstream 수정·두 번째 state commit·DAO/beacon prelude를 허용하지 않는다. sender-cache의 정확한 upstream 상주 함수만 goleak 예외이며 소유 worker는 제외하지 않는다.

U03: executor/spool race와 `GOMAXPROCS=1 go test -race -count=200 -timeout=5m -run '^TestConcurrentCancellationAndCloseJoin$' ./spool`. [spool 계약](05-spool-contract.md)을 따른다. #212 CI에서 발견한 실제 종료 outbox 누락은 accepted queue drain·publish/stop fence·relay-before-bus join으로 수정했고 두 RED 및 단일200회/전체 CI를 통과했다.

U04~U08: `scripts/test-postgres.sh ./pkg/storage/postgres -race -run '^TestExecution'`. 기능 테스트는 격리 schema와 fsync-on 임시 DB를 사용한다. U04의 before CAS·tombstone·clear·precision·creation 및 U06 공개·U07 snapshot이 같은 검사군에 포함된다. 실제 EVM 기반 end-to-end와 최종 모든 기능 판정은 U11이다.

U05: `GOMAXPROCS=1 go test -race ./pkg/execution -count=200 -timeout=10m`, `go test -race ./pkg/chainfeed/evmdata`. 성공 CREATE의 빈 runtime/생성자 소멸을 receipt 증거로 유지하며 실패 receipt는 제외한다. encoder가 decoder JSON token budget을 초과하는 결과를 만들던 추가 RED도 수정했다.

U06/U07: `go test -race ./pkg/fetch -run '^TestExecutionFeature'`; `go test -race ./pkg/api/... ./internal/api/rest -run '^TestExecution'`. 공개 전 SQL flush/undo, post-response generation, retry exact proof, request snapshot·큰 정수 정밀도·REST cache 차단을 검사한다. 새 root/API 및 실제 PG 동시성은 각각 단일 GOMAXPROCS=1/race/count=200을 통과했다. build/vet/lint/cache/examples/오프라인 운영과 실제 PG adapter suite·전체 root/wire/executor race를 통과했다. 최신 commit의 전체 PG 재검증은 필수 CI에서 완료한다.

각 merge의 영구 CI 결과는 위 PR에 있고, 구현 의도·현재 계약·검사 이름은 해당 문서와 테스트 소스에 있다. 임시 로그 경로만을 유일한 근거로 쓰지 않는다. 오래된 진행 문구·실험 결과는 Git history에서 확인한다.

전체 회귀에서 새 GraphQL 필드의 schema snapshot 누락과 stdlib-only wire 계약의 port allowlist 누락을 확인했다. 의도한 schema/method 목록을 갱신하고 wire의 외부 구현 의존을 금지하는 검사를 추가했다. 해당 focused race 및 전체 root/wire/executor race는 통과했다. 전체 PG는 기준 파일 갱신 전에 실행된 snapshot 한 건만 실패했고 나머지 adapter/app/API/bundle 검사는 통과했다. 같은 snapshot/method 검사를 최신 파일과 PG 검사 환경에서 다시 실행해 통과했다. 이 구분을 숨기지 않으며 최신 commit의 전체 PG는 PR CI로 확인한다.
