# E1 구현 체크리스트

정본: [구현 계획](03-plan.md). 2회 실행·노드 소스 미수정·기존 pool 4개·RPC 5 HTTP/s·최종 공개 직전 fresh를 유지한다. 실제 노드 서비스와 추가 성능 측정은 보류다.

## 현재

- 진행 중: U03 durable spool·ACK 로컬 검증 완료, PR·CI 준비. U02는 PR #211로 완료했다. U00·U01은 PR #210으로 완료했다.
- 다음: U03 검증·PR·CI·squash/main, 이후 U04 PostgreSQL 상태 포트.
- 최근 완료 기능 n-1: U02 recorder(`4525ac6`, PR #211), n-2: U01 wire(`9f01b09`, PR #210).
- `03-plan.md`는 사용자의 2026-10-24 위임에 따라 작성했다. 이전 구현 gate의 잠금은 이 위임으로 해제한다.

| ID | 상태 | RED 근거 | GREEN·통합 근거 | PR·merge |
| --- | --- | --- | --- | --- |
| U00 | 완료 | 문서 작업, 해당 없음 | 문서 링크114·JSON·fmt·build·vet·lint·전체 race·PostgreSQL·cache lifecycle·examples·운영 초안 통과 | [#210](https://github.com/0xmhha/indexer-go/pull/210), `9f01b09` |
| U01 | 완료 | API형 stub의 미구현 오류로6건 FAIL. raw RLP·조립 frame 상한 추가2건 FAIL | `make test-execwire`, wire lint, GOMAXPROCS=1/race/단일200회 통과. 별도 모듈 CI 연결 | [#210](https://github.com/0xmhha/indexer-go/pull/210), `9f01b09` |
| U02 | 완료 | recorder·Replay API stub, clear·빈 code·조기 상한 추가 RED | 실제 EVM/WBFT·fee payer·Extra·독립 양방향 대조, root/PG/모듈 회귀·동시성200회 통과 | [#211](https://github.com/0xmhha/indexer-go/pull/211), `4525ac6` |
| U03 | 검증 중 | stub6건 RED. ACK 재전송 identity1건·초기 identity/unknown temp2건 추가 RED | durable spool·ACK10단계 복구·quota·손상·취소/join GREEN, race·lint·단일200회 및 root/wire·PG 회귀와 영향 helper11건 검증 통과 | — |
| U04 | 대기 | 미실행 | 미구현 | — |
| U05 | 대기 | 미실행 | 미구현 | — |
| U06 | 대기 | 미실행 | 미구현 | — |
| U07 | 대기 | 미실행 | 미구현 | — |
| U08 | 대기 | 미실행 | 미구현 | — |
| U09 | 대기 | 미실행 | 미구현 | — |
| U10 | 대기 | 미실행 | 미구현 | — |
| U11 | 대기 | 미실행 | 미구현 | — |

## 완료 조건

- 각 기능의 RED와 수정 후 GREEN이 해당 요구를 검증한다.
- 사용자 요청으로 위임된 설계 결정을 계획에 기록하고 실행 코드와 일치시킨다.
- 미공개 자료, 공개 완료, source ACK를 구분한다.
- 실제 실행·성능·운영 검증을 오프라인 검사 완료로 표기하지 않는다.
- 각 PR의 필수 CI를 확인하고 squash merge한 뒤 main을 동기화한다.
- 기능마다 현재 문서와 continuity의 n/n-1/n-2를 갱신한다.

## U01의 현재 계약과 검증 근거

- 파일: `execwire/{wire,codec,result,parts,conn}.go`, 계약 검사2개 파일, 모듈 README. 기존 root Go 코드는 바꾸지 않았다.
- Version1, 8MiB frame, 64MiB result, 16MiB JSON metadata, 4096parts. 전체 declared bytes 예약과 first-part TTL로 조립을 제한한다. JSON은 typed slice 할당 전에 깊이·token 수를 검사한다.
- 원본 block/receipt RLP는 binary segment다. SHA-256은 전송 무결성이며 EVM root·canonical·durability 판정을 대신하지 않는다.
- 단일 reader/writer 방향 대기와 I/O 취소, deadline callback join, partial-frame 오류의 연결 폐기를 구현했다. assembler는 단일 소유자이며 disconnect에서 Reset해야 한다.
- RED 기준 main: `98c532404a8ae0d7fa8643de737dc53549c2eb2b`. 먼저 `go test -count=1 ./...`로 `TestResultDeterministicRoundTrip`, `TestFrameRoundTripAndRejectBeforeAllocation`, `TestPartsBoundedAssemblyDuplicatesAndExpiry`, `TestAssemblyRejectsConflictingMetadataAndWholeDigest`, `TestConnCancellationAndSubsequentFrame`, `TestJSONInputResourceBounds`가 `execwire: not implemented`로 실패했다. API형 stub이었고 컴파일 실패를 RED로 세지 않았다.
- 1차 GREEN 뒤 raw-segment 보존과 assembler의 직접 입력 frame 상한 검사를 추가했다. 각각 `source RLP was not preserved as raw segments`, `oversize frame: <nil>`로 실패한 뒤 binary segment 형식과 상한 검증을 수정하여 GREEN을 확인했다.
- 최종 명령: root `make test-execwire`; 모듈 `golangci-lint run ./...`; `GOMAXPROCS=1 go test -race -count=200 -timeout=5m -run '^TestConn' ./...`. 이 명령은 부분 실행을 합산하지 않았다.
- root 전체 회귀는 `make test-race 'GOTEST=go test -p=2 -parallel=1 -timeout=30m'`, 실제 임시 fsync-on PostgreSQL은 `GOFLAGS='-mod=readonly -p=2 -parallel=1' make test-postgres`로 통과했다. 기존1500블록 records 검사의 병렬 실행30초 실패는 단독 실행으로 분리했고 runner 병렬도만 낮췄다. 이를 E1 RED나 성능 증거로 쓰지 않는다.
- `make test-cache-lifecycle`, `make test-examples`, root/astgraph lint, build/vet, 운영 초안 Python·shell 검사도 통과했다. 선택 Anvil 검사는 해당 테스트 프로세스 PATH에서 Anvil을 제외해 skip했으며 실제 노드는 시작하지 않았다.
- `FuzzDecode`·`FuzzResult`의 제한된 기능 fuzz 검사와 잘림·정수 overflow·JSON 깊이/token 상한 검사도 통과했다. fuzz 처리량을 성능 증거로 사용하지 않는다.
- U02는 wire의 exact reference·ordinal·Finalize·before/after·raw RLP 계약을 재사용한다. 실제 EVM 검증과 upstream state commit 금지를 여기서 완료했다고 주장하지 않는다.

PR #210은 lint/test/postgres/examples 통과 후 `9f01b09493677cdfbd6dd40c861cf10d644b9bb8`로 squash merge했고 main을 fast-forward 동기화했다.

## U02 계약과 검증 근거

- `newRecorder`와 `Replay`를 API형 stub으로 두고 각각 `ErrNotImplemented` RED를 확인했다. 잘못된 EVM fixture의 root나 컴파일 오류는 RED로 세지 않았다.
- recorder: account/storage before-after, Extra, revert, 거래 간 원복, code와 삭제, 원본 갱신을 유지한 record-only omission(account/storage/code), CopyTrie 격리, code batch 포함 commit 금지와 backing DB 비기록이 GREEN이다.
- 삭제·재생성의 새 storage trie가 시작되는 clear와 빈 code 제거는 추가 RED 후 수정했다. 존재하는 재생성 계정 fixture에는 실제 CREATE처럼 nonce를 설정했다.
- 실제 EVM 첫 실행과 Replay의 state/receipt root, 원본 header 보존, header.Root를 덮어쓰는 Finalize 모형의 원본 root 거절, 잘못된 stored receipt와 취소가 GREEN이다.
- 실제 WBFT Finalize·fee payer·native mint와 NativeManager Extra, 독립 양방향 account/storage trie 대조, EVM 내부 취소/loop 중단·callback join, 생성 후 같은 블록에서 SELFDESTRUCT, 실제 EVM revert, path-mode parent와 missing-state fail-closed가 GREEN이다. WBFT fixture는 Applepie를 켰으며 seal admission을 검사했다고 주장하지 않는다. 실행부 build/vet/lint/race와 단일 GOMAXPROCS=1/race/200회, goleak 검사가 통과했다. upstream 패키지 초기화 sender-cache의 정확한 함수만 전역 상주 worker로 구분한다. root/wire 전체 race·PostgreSQL·build/vet/lint·astgraph lint·cache lifecycle·examples·오프라인 운영 검사와 staged/history secret 검사도 통과했다. 선택 Anvil은 테스트 PATH에서 제외했다. 실제 노드 서비스·peer·성능 측정은 실행하지 않았다.
- `Replay`는 DAO/beacon의 pre-transaction protocol 변경 조합을 명시적으로 거절한다. 현재 StableNet 구성의 지원 범위를 확인하고 runtime 구성에서 같은 경계를 검사해야 한다. 잘못된 transaction/Finalize 원인으로 기록하지 않는다.

- transaction 수65536 상한의 조기 차단도 RED(`ErrRootMismatch`로 늦게 거절) 후 `execwire.ErrLimit`로 먼저 거절하도록 보완했다.

- 고정 fork의 `core/sender_cacher.go`는 package init에서 `runtime.NumCPU()` 상주 worker를 만들고 종료 API를 제공하지 않는다. 첫 goleak 실패의 stack을 원본과 대조해 그 정확한 top 함수만 예외 처리했고, 수정 후 단일200회 전체 실행을 다시 통과했다. 소유 callback/worker를 일괄 제외하지 않았다.

PR #211은 필수 CI4개 통과 후 `4525ac6ddd630b60f9a2d93b4653228ef934817d`로 squash merge하고 main을 fast-forward 동기화했다.

## U03 계약과 검증 근거

- 기준 `4525ac6ddd630b60f9a2d93b4653228ef934817d`에서 API형 stub6건이 `ErrNotImplemented`로 RED였다. 컴파일/환경 실패는 RED로 세지 않았다.
- 추가 RED: ACK된 결과 Append가 원래 Size를 잃는 결함1건, 미완료 identity를 다른 체인으로 재초기화·unknown temporary를 임의 이동하는 결함2건. retained ACK history 재사용과 fail-closed identity/temporary 정책으로 수정했다.
- GREEN: file fsync→rename→directory fsync 뒤 공개; exact·contiguous ACK; ACK10단계 장애 복구; 다음 미확인 원본 보존; digest/length 손상 보존; quota와 단일 소유권·symlink 거절; 취소·Close join. `go test -race -count=1 ./...`와 spool `GOMAXPROCS=1 go test -race -count=200 -timeout=5m -run '^TestConcurrentCancellationAndCloseJoin$' ./spool` 통과.
- [spool 계약](05-spool-contract.md)과 모듈 README가 현재 API·복구·상한을 설명한다. 전체 회귀/CI 완료 전 완료로 표시하지 않는다.

- 전체 root/wire race는 통과했다. make 마지막 executor 단계는 기본 캐시의 fork lookup이 GOPROXY=off로 막혀 실행 전 실패했고, 전용 module cache로 executor 전체 race를 다시 통과했다. 제품 의존성이나 Go/RPC/DB 설정은 바꾸지 않았다.
- 전체 PostgreSQL 실행에서 어댑터·StableNet bundle은 통과했고 기존 `TestDeclaredBackfillReadsLogsFromTheNode/per_block`만1,500블록의 고정30초 대기에 실패했다. 같은 기능의 root race와 분리 PG/race는 통과했다. helper가 기존1분 context를 끝까지 사용하고 조기 Run 오류·마지막 높이를 보고하며 실패/성공 모두 cancel·join하도록 수정했다. 최초 app의 실패 경로 Shutdown도 추가했다. 기능 단언·생산 경로·RPC 예산은 바꾸지 않았다. 영향받는11개 기존 기능은 root와 실제 PG에서 race로 통과했다. 전체 CI는 PR에서 다시 실행한다.

- 최종 build/vet/lint(root·executor·astgraph), cache lifecycle, examples, 운영 Python·shell, JSON·문서 링크18개와 fmt 검사가 통과했다. 실제 노드·peer·추가 성능 측정은 보류했고 선택 Anvil은 검사 PATH에서 제외했다.
