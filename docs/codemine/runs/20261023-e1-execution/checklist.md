# E1 구현 체크리스트

정본: [구현 계획](03-plan.md). 2회 실행·노드 소스 미수정·기존 pool 4개·RPC 5 HTTP/s·최종 공개 직전 fresh를 유지한다. 실제 노드 서비스와 추가 성능 측정은 보류다.

## 현재

- 진행 중: U01 로컬 GREEN 완료, 첫 PR 제출. U00 계획 문서를 함께 포함한다.
- 다음: 필수 CI·squash merge·main 동기화 후 U02 실행 recorder의 RED.
- 최근 완료 기능 n-1/n-2: 아직 없음.
- `03-plan.md`는 사용자의 2026-10-24 위임에 따라 작성했다. 이전 구현 gate의 잠금은 이 위임으로 해제한다.

| ID | 상태 | RED 근거 | GREEN·통합 근거 | PR·merge |
| --- | --- | --- | --- | --- |
| U00 | 로컬 완료 | 문서 작업, 해당 없음 | 문서 링크114·JSON·fmt·build·vet·lint·전체 race·PostgreSQL·cache lifecycle·examples·운영 초안 통과 | U01과 함께 제출 |
| U01 | 로컬 GREEN | API형 stub의 미구현 오류로6건 FAIL. raw RLP·조립 frame 상한 추가2건 FAIL | `make test-execwire`, wire lint, GOMAXPROCS=1/race/단일200회 통과. 별도 모듈 CI 연결 | 제출 중 |
| U02 | 대기 | 미실행 | 미구현 | — |
| U03 | 대기 | 미실행 | 미구현 | — |
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
- 다음 U02는 wire의 exact reference·ordinal·Finalize·before/after·raw RLP 계약을 재사용한다. 실제 EVM 검증과 upstream state commit 금지를 여기서 완료했다고 주장하지 않는다.
