# U11 전체 기능 통합 계약

상태: 실제 EVM/WBFT fixture와 PostgreSQL/API/IPC·ACK 유실·reorg·재시작, 최종 root/wire/executor 전체 race와 PostgreSQL 전체 회귀, build/fmt/vet/lint/예제/cache/오프라인 운영·secret 검사 GREEN. 새 receiver/runtime/exporter/node 및 실제 PG/IPC 각각 단일 GOMAXPROCS=1/race/count=200 GREEN. Linux receiver cross-build도 통과했다. PR/CI·squash/main 판정은 checklist의 연결 PR을 따른다. 실제 노드·peer·포맷·GC·성능 측정은 보류다.

## 실제 실행과 모듈 경계

`x/stablenet/executor/integration_fixture_test.go`가 고정 공식 fork의 첫 EVM/WBFT 실행과 recorder Replay를 수행한다. `testdata/execution`의 고정 wire payload를 매 정상 검사에서 다시 생성해 byte 비교한다. root는 fork를 import하지 않고 같은 payload를 읽는다. fixture 갱신은 테스트 전용 `INDEXER_UPDATE_EXECUTION_FIXTURES=1`의 명시적 opt-in이다. baseline과 expected account/code/storage는 첫 실행의 실제 parent/final trie에서 별도로 읽는다. Replay 전후 durable key-value dump가 같아야 한다.

범위는 EVM storage/log·revert·성공한 빈 runtime/생성자 소멸·실패 생성, WBFT native mint·fee payer·Finalize이며 원래 state/receipt root를 보존한다. 일반 이벤트·header/합의 seal admission은 노드 서비스 실행으로 검증하지 않았다. 여러 높이의 queue·canonical walk와 재연결은 별도 deterministic backend 계약 검사가 담당한다.

## 실제 DB와 앱

`TestExecutionFrozenEVMPostgresPublicationAndAPIParity`는 private 임시 fsync-on PostgreSQL pool4에 anchor/baseline을 명시적으로 준비한다. 같은 payload가 7개 part와 기존 native/address/token feature publication을 통과하고, 실제 final 상태·code·slot·native balance·GraphQL 공개 snapshot과 일치해야 한다. 중복 publication이 잔액 이력을 반복하지 않아야 한다.

`TestExecutionAppFrozenWBFTIPCReorgRestartAndLostACK`는 실제 `NewApp` 실행 모드와 native Unix socket을 사용한다. canonical 전용 mock HTTP만 사용하며 rate5를 유지한다. 정상 공개와 정확한 payload/digest ACK, 오래된 source checkpoint의 lost-ACK 재전송, outbox 무중복, 명시적 canonical Rewind, generation2의 새 실제 WBFT branch 처리, 종료/join과 DB reopen을 검사한다. 일반 StableNet 기능을 사용하되 fixture가 지원하지 않는 current token-metadata RPC는 비활성화한다. balance RPC는 허용하지 않는다. 이는 노드·peer 실행이나 처리량 측정이 아니다.

## 통합에서 고정한 결함과 수정

- 실제 계약 생성 payload에서 account/storage writer에 contract proof를 전달하던 오류: `TestStoreBackendCreationProofBelongsOnlyToContractWriter` RED → proof는 contract writer에만 전달한다.
- 성공한 constructor SELFDESTRUCT의 전후가 모두 부재여서 recorder가 witness를 버리던 오류: `TestReplayKeepsSuccessfulConstructorDestructionAccountWitness` RED → receipt의 성공한 outer creation에 한해 실제 parent/current account witness를 보존한다. DB에서 임의 baseline을 만들지 않는다.
- 기존 address feature가 실패 receipt와 zero contract address를 생성으로 기록하던 오류: `TestCreationRequiresSuccessfulReceiptAndNonzeroAddress` RED → 성공/nonzero만 생성 색인에 기록한다. 빈 runtime과 constructor 소멸은 성공 receipt를 유지한다.
- source.execution의 queue 기본값이 controller에 0으로 전달되던 오류: 실제 App/IPC 통합 실패 → 64블록/256MiB 기본값을 조립한다. 설정 값은 덮어쓰지 않는다.
- fresh가 감지한 reorg 복구 후 listener가 원래 fatal 오류로 종료하던 오류: `TestReceiverFreshDetectedReorgReturnsRetryableNewGeneration` RED → 성공한 복구는 새 Hello를 받는 retryable generation 전환으로 반환한다.
- 공통 앱에 체인 이름을 추가하던 구조 오류: 기존 `TestChainIdentifierLimits` 실패와 explicit producer capability RED → 선언을 `x/<chain>`의 profile에 두고 이름만 같은 generic profile을 거절한다. 구조 검사 제한을 늘리지 않는다.
- socket 초기화 도중 공개 경로가 보이던 경계: `TestReceiverSocketPathPublishedOnlyAfterReadyAndNeverReplaces` RED → private listen/chmod 후 no-replace rename으로 원자 공개한다. 테스트도 파일 존재가 아닌 실제 연결 성공을 기다린다.
- 종료의 joined cancellation 및 cleanup 오류 판정: command RED 두 경계 → 정상 취소만 정상 종료하고 실제 cleanup 실패는 보존한다.

표현 차이(nil/empty byte slice), 테스트 초기화와 macOS Unix socket 길이와 테스트의 bind/listen 준비 조건 문제는 제품 RED로 세지 않는다. 현재 fixture 비교는 정확한 bytes 의미와 짧은 자체 임시 socket 경로를 사용한다.

## 최종 검사와 보류

필수 root/wire/executor 전체 race, PostgreSQL 전체 회귀, build/fmt/vet/lint, astgraph, cache lifecycle, 예제, 오프라인 운영, secret 검사를 완료한 뒤 commit/PR한다. 새 receiver/runtime/exporter/node lifecycle과 실제 PG/IPC 통합은 단일 `GOMAXPROCS=1 -race -count=200`을 검증하고 CI에도 같은 범위를 등록한다. 부분 횟수는 합산하지 않는다.

기능 GREEN을 성능·운영 준비 완료로 확대하지 않는다. E2의 bootstrap/검증된 checkpoint·parent 복구, E3의 node history/원본 보존·삭제, E4의 기존 feature writer 분리, E5의 체인 확장, E6의 합의/finality 기반 RPC 의존 축소는 [남은 목록](../../../analysis/open-items.md)에 유지한다. n=U11, n-1=U10, n-2=U09 context와 E1 계획은 merge 후에도 보존한다.
