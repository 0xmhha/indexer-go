# 명령행과 체인별 제품 구조 설계

이 문서는 명령행을 cobra로 바꾸고, 체인별 코드를 어댑터 구조로 나누어 체인별 바이너리를 빌드하는 리팩토링의 합의 내용을 적는다. 작업은 PR 다섯 개로 나누며, 각 PR의 상태는 6장에 적는다. 구현이 이 문서와 다르면 코드와 테스트가 기준이다.

## 1. 배경

### 명령행의 문제

리팩토링 전 indexer는 표준 `flag` 패키지로 플래그 21개를 `pkg/app`에서 직접 읽었다. 잘못 썼을 때의 안내가 부족했다.

- `indexer start --rpc x`처럼 위치 인자를 먼저 주면 그 뒤의 플래그를 모두 조용히 버렸다. 그다음 "RPC endpoint is required"라는 엉뚱한 오류를 냈다.
- 모르는 플래그를 주면 사용법 41줄을 쏟아 냈다. 비슷한 플래그를 제안하지 않았다.
- `--batch-size`의 도움말("0 = config.yaml 사용")과 실제 동작(0이면 검증 실패)이 달랐다. 이 플래그가 바꾸는 설정 이름은 `indexer.chunk_size`였다.
- `--clear-data`와 `--reindex`를 함께 주면 `--reindex`를 조용히 무시했다.
- 도움말에 설명, 예시, 설정 파일과 환경 변수의 관계, 문서 위치가 없었다.

### 설정 항목과 명령행의 차이

AST로 설정 필드가 어디서 읽히는지 추적했다. 말단 설정 필드는 281개이고, 실행 중 읽히는 것은 235개다. 그중 명령행으로 바꿀 수 있는 것은 13개뿐이었다. `node.role`, `database.driver`, `indexer.finality`, `rpc.fallback_endpoints` 같은 운영 설정은 환경 변수나 파일로만 바꿀 수 있다. 46개는 실행 중 읽히지 않는다. 대부분 연결되지 않은 Redis/Kafka 설정이다.

비밀 값(DSN, `api.keys`, SMTP 비밀번호)은 플래그로 만들지 않는다. 플래그는 프로세스 목록과 셸 기록에 남기 때문이다.

### 체인별 차이

EVM 체인은 블록, 거래, receipt, 상태 변화를 가져오는 큰 틀이 같다. 그러나 체인마다 헤더 형식, 해시와 서명 알고리즘, 지원하는 거래 타입, 하드포크와 EIP 지원 범위가 다르다. 그래서 가져온 데이터를 디코딩하고 해석하는 부분은 체인마다 바꿔 끼울 수 있어야 한다. 모든 체인의 코드를 코어 안에 직접 쓰지 않는다.

이 확장은 빌드 단계에서 고른다. Android 제품 빌드가 제품별 구성 파일로 넣을 모듈을 고르는 것과 같은 생각이다. Go에서는 제품별 main 패키지의 import 목록이 그 구성 파일 역할을 한다.

## 2. 합의한 원칙

1. `pkg/`와 `internal/`에는 코어 엔진만 둔다. 명령행, 명령행 플래그, 제품 조립 코드는 두지 않는다.
2. `cmd/`에는 조립 도구(`cmd/cli`, `cmd/flags`)와 기본 바이너리(`cmd/indexer`)만 둔다.
3. `pkg/chains`에는 코어 쪽 어댑터를 둔다. 어댑터 인터페이스와 등록 장치, generic EVM 기본 어댑터(`pkg/chains/evm`)다.
4. 체인별 파서와 확장 기능은 `x/<체인>`에 둔다(`x/stablenet`, `x/arc`). 이 코드는 어댑터 인터페이스를 구현해 코어에 연결된다. 체인 전용 바이너리도 여기에 둔다.
5. 바이너리 이름에 체인을 붙인다(`indexer`, `indexer-stablenet`, `indexer-arc`).
6. 리팩토링하면서 기존 테스트와 문서를 함께 고친다.
7. 외부 사용자 호환성은 고려하지 않는다. 바뀐 플래그와 명령에 옛 이름의 별칭을 두지 않는다.
8. 하드포크와 EIP 차이는 빌드 옵션으로 풀지 않는다. 같은 체인도 블록 높이에 따라 규칙이 바뀌기 때문이다. 빌드는 어떤 어댑터를 넣을지 고르고, 어댑터가 블록 높이를 보고 규칙을 정한다.
9. 빌드 태그와 파일 교체(`go build -overlay`)로 기능을 넣고 빼지 않는다. 태그 조합마다 다른 코드가 컴파일되어 시험하지 않은 바이너리가 생긴다. 바꿔 끼우는 것은 어댑터 옵션(`evm.With...`)과 profile을 감싸는 방식으로 한다.

## 3. 구조

```
cmd/
  cli/                         cobra 조립 도구: New(opts...), Main(opts...) int, 버전 변수, 종료 코드
  flags/                       플래그 묶음(Group)과 묶음의 합성(Set)
  indexer/main.go              기본 바이너리 indexer: generic EVM과 공통 기능
pkg/
  chains/                      어댑터 인터페이스와 등록 장치
    evm/                       generic EVM 기본 어댑터와 옵션
  app/                         엔진: app.Start, app.ResumeCheck, app.ValidateConfig
  features/...                 체인과 무관한 공통 기능
  startup/                     체인 패키지가 등록하는 시작 단계(StableNet 시스템 계약 검증 자료)
x/
  stablenet/                   이전의 pkg/chains/stablenet 전체
    bundle/                    이 체인의 부품을 모두 blank import하는 묶음
    cmd/indexer-stablenet/     제품 바이너리
  arc/                         지금의 pkg/chains/arc
    feed/                      지금의 modules/feeds/arc
    nodefeed/                  지금의 modules/arc-node-feed(Rust 크레이트)
    bundle/
    cmd/indexer-arc/
```

### 의존 방향

- `pkg/`, `internal/`, `cmd/`의 테스트가 아닌 코드는 `x/`를 import하지 않는다.
- `x/<체인>`끼리는 서로 import하지 않는다.
- `x/<체인>`은 `pkg/`의 공개 패키지만 쓴다.
- 같은 go.mod를 쓴다. 방향은 `pkg/chains/coupling_test.go`를 고쳐 강제한다.

`pkg/app`의 테스트 14개 파일은 StableNet이나 Arc 시나리오를 쓴다. 이 테스트는 `pkg/app` 내부 도우미에 기대므로 처음에는 `_test.go`에서만 `x/` import를 허용한다. `pkg/app/chains_test.go`가 두 bundle을 링크하고, 프로세스를 띄우는 시험은 두 bundle을 넣은 시험용 바이너리(`pkg/app/testdata/indexer-all`)를 빌드한다. `x/`가 `pkg/app`을 import하지 않으므로 순환은 생기지 않는다. 나중에 도우미를 `pkg/app/apptest`로 공개하고 체인 전용 테스트를 `x/<체인>`으로 옮긴다.

방향 규칙은 `pkg/chains/coupling_test.go`의 `TestOnlyProductsImportChains`가 강제한다. 체인 이름 개수 상한(`TestChainIdentifierLimits`)은 `x/`를 세지 않는다. 엔진이 체인 패키지에 직접 기대던 곳(StableNet 시스템 계약 검증 자료 준비)은 `pkg/startup`의 등록 단계로 바꿨다.

### 명령행 구조

- 루트 명령이 색인을 돌린다. 하위 명령은 `resume-check`, `version`, `help`다.
- `--config`, `--log-level`, `--log-format`은 모든 명령이 받는다. 나머지 플래그는 색인 명령만 받는다.
- 플래그 묶음은 `flags.Group`(`Name`, `AddFlags`)이다. 설정을 바꾸는 묶음은 `flags.Applier`(`ApplyTo`)도 구현한다. `flags.Set`이 묶음을 합성하고, 도움말을 묶음별로 쓴다.
- 설정 플래그는 `flags.Flag`로 선언한다. 이름, YAML 키, 환경 변수, 설명, 필드 접근 함수를 한곳에 둔다. 플래그를 하나 더할 때 고칠 곳은 이 선언 하나다.
- 설정 플래그는 명령행에 준 것만 설정에 반영한다. 기본값이 파일이나 환경 변수의 값을 바꾸지 않는다.
- 우선순위는 기본값 < 설정 파일 < 환경 변수 < 명령행 플래그다.
- 종료 코드는 성공 0, 실행 실패나 설정 오류 1, 명령행 오류 2다.
- 제품 main은 `cli.WithName`으로 이름을 붙인다. PR 2에서 `cli.WithChain`을 더한다.

## 4. 빌드와 시험

### Makefile

제품은 `CHAIN` 변수로 고른다. 비워 두면 기본 바이너리를 빌드한다.

| 대상 | 하는 일 |
|---|---|
| `make build` | `build/indexer` |
| `make build CHAIN=stablenet` | `build/indexer-stablenet` |
| `make build-all` | 기본 바이너리와 `x/` 아래 모든 체인 |
| `make test-short` | `go test -short ./...` |
| `make test` | `go test ./...` 전체 |
| `make test-chain CHAIN=arc` | `go test ./x/arc/...`와 그 제품 빌드 |
| `make test-race` | 지금과 같다. 커밋과 PR 전에 돌린다 |
| `make docker-build CHAIN=arc` | `indexer-arc` 이미지 |

없는 체인 이름을 주면 make가 바로 실패한다. 효과가 없던 `-tags release`는 지운다.

### CI와 test-short

`x/`가 같은 모듈에 있으므로 CI의 `go build ./...`와 `go test ./...`가 모든 제품과 체인 테스트를 이미 덮는다. 필수 CI 작업 4개는 그대로 둔다. 체인별 Docker 이미지는 배포할 때 빌드한다.

`test-short`는 로컬에서 빠르게 확인하는 용도다. 지금 `-short`로 건너뛰는 테스트는 6개뿐이다. 실제 프로세스를 띄우거나 강제 종료하는 테스트, 수백 블록 이상을 색인하는 부하 테스트, 반복 측정 테스트에 `testing.Short()` 검사를 더한다.

## 5. 제품을 잘못 붙이는 것 막기

바이너리 이름에 체인을 붙여도, 이름만으로 막지 못하는 경우가 있다. `indexer-stablenet`을 Arc 노드에 붙이면 Arc profile이 없으므로 generic EVM으로 감지되고, Arc의 native 잔액 규칙이 빠진 데이터가 쌓인다.

그래서 시작할 때 다음을 검사한다.

- 제품 main이 `cli.WithChain("stablenet")`처럼 자기 체인을 선언한다.
- 노드에서 감지한 profile이 그 체인 것이 아니면 시작을 거부한다. 오류에 노드의 client 버전과 chain id를 적는다.
- 기본 `indexer`는 generic EVM만 링크한다. 그래서 StableNet이나 Arc 노드도 generic EVM으로 감지해 잘못된 규칙으로 색인할 수 있다. 이를 막으려고 `cli.WithOtherProducts`로 다른 제품의 노드 판별 규칙을 받아, 해당 노드면 실행할 바이너리 이름을 적고 거부한다. 판별 규칙은 profile을 import하면 링크되어 버리므로 `cmd/indexer`에 데이터로 다시 적고, `cmd/indexer/main_test.go`가 각 profile의 `Detect`와 같은 답을 내는지 확인한다.
- 검사는 profile을 고른 직후 노드에 `web3_clientVersion`, `eth_chainId`를 한 번씩 더 묻는다. 멀티체인 모드에서는 체인마다 같은 검사를 한다.
- `--adapter`에 등록되지 않은 이름을 주면 자동 감지로 넘어가지 않고 오류를 낸다.
- `version`과 시작 로그에 제품 이름을 찍는다.

이 검사의 단점은 감지 범위 밖의 노드를 일부러 붙일 때도 거부한다는 점이다. 그때는 profile의 감지 범위를 코드로 넓혀야 한다.

## 6. PR 순서와 상태

| PR | 내용 | 상태 |
|---|---|---|
| 1 | cobra와 pflag 추가, `go mod tidy`. `cmd/cli`, `cmd/flags`. `pkg/app`을 엔진만 남김. 오류 안내, 종료 코드, 묶음별 도움말, ldflags 경로, `--batch-size`를 `--chunk-size`로, `--resume-check`를 `resume-check` 명령으로, `sdk.Main` 제거 | 머지 대기 |
| 2 | `x/stablenet`, `x/arc` 이동. blank import를 bundle로 옮김. 제품 main, 제품 체인 검사, `--adapter` 미등록 이름 오류, Makefile `CHAIN`, Dockerfile, 배포 스크립트, coupling 테스트 | 제출 |
| 2a | 테스트넷 배포 초안을 체인별 프로세스로 나눔(Compose 서비스, entrypoint, `preflight.py`, 설정 초안) | 남음 |
| 3 | `pkg/sdk` 해체. `sdktest`를 `pkg/feature/featuretest`로. examples 갱신. SDK와 체인 커스터마이징 문서를 어댑터와 확장 문서로 다시 씀 | 남음 |
| 4 | 운영 플래그 추가(node-role, db-driver, finality, mode, rpc-fallback, rpc-ws, rpc-rate-limit, rpc-timeout, era-dir) | 남음 |
| 5 | `-short` 표시 보강과 `test-short` 기준 정리 | 남음 |

### PR 3에서 pkg/sdk가 나뉘는 방식

- 바이너리 시작은 `cmd/cli`로 간다(PR 1에서 끝냄).
- 어댑터 작성자는 `pkg/chains`, `pkg/chains/evm`을 직접 쓴다.
- 기능 작성자는 `pkg/feature`를 직접 쓴다.

별칭만 모아 둔 창구가 없어지면, 확장 코드가 만지면 안 되는 내부(writer, storage 구현)도 import할 수 있게 된다. 이것은 해당 패키지를 `internal/`로 옮겨 따로 막는다.

## 7. 멀티체인 모드와 체인별 프로세스

멀티체인 모드는 프로세스 하나가 여러 체인을 색인하는 기능이다(`multichain.enabled`). 한 프로세스가 여러 체인을 돌리려면 그 체인들의 어댑터가 모두 한 바이너리에 있어야 한다. 이런 바이너리를 따로 두지 않기로 했다. 체인별 바이너리를 체인마다 다른 프로세스로 돌린다.

이유는 다음과 같다.

- 체인별 등록 장치가 프로세스 전체에 하나다. StableNet이 등록한 known token(`0x…1000`)은 주소만 키로 쓰므로 같은 프로세스의 다른 체인에도 적용된다. StableNet GraphQL 확장도 모든 체인의 스키마에 붙는다. 바이너리에 자기 체인 어댑터만 넣으면 이 섞임이 생기지 않는다.
- 저장소·용량 장애는 halt latch를 쓰고 프로세스 전체를 멈춘다. 한 체인의 디스크 문제가 다른 체인 색인까지 멈추게 하지 않으려면 프로세스를 나눠야 한다.
- 체인별로 업그레이드하고 재시작할 수 있다.

비용도 있다. 테스트넷 초안의 Compose와 `preflight.py`는 indexer 서비스 하나를 전제로 하므로 다시 써야 한다. API 주소가 체인마다 생기고, 한 주소로 묶으려면 reverse proxy가 필요하다. 용량 감시와 halt latch가 프로세스마다 돈다.

멀티체인 모드 코드는 남겨 둔다. generic EVM 체인 여러 개를 `indexer` 한 프로세스로 돌리는 선택지로 쓸 수 있다. 테스트넷 초안은 PR 2a에서 나누며, 그 전까지 초안 설정(StableNet, Arc, generic 체인을 한 프로세스에)은 어떤 바이너리로도 시작할 수 없다. 운영 시작은 원래 보류 상태다.

남은 결정은 CI test 작업의 `-short` 전환 하나다. 바꾸면 PR 시간이 줄지만 빠진 테스트는 로컬 `make test-race`에서만 돈다. PR 5 전에 정한다.

## 8. 확인하지 않은 것

- Go 진영의 선례(Kubernetes의 옵션 합성, Caddy의 외부 빌드, geth의 short 테스트, Cosmos SDK의 `x/` 모듈 배치)는 알고 있는 내용으로 판단했다. 이번에 각 저장소를 열어 확인하지 않았다.
- 체인을 섞어 쓰는 구성은 테스트넷 초안(`docs/examples/config-testnets-windowed.draft.yaml`)에서 확인했다. 다른 외부 배포는 확인하지 않았다.
- frontend가 `/chains/<id>/` 경로에 기대는지 확인하지 않았다.
- `pkg/app` 테스트 중 체인 코드에 기대는 테스트 수(14개 파일)는 import 기준으로 셌다. 테스트 함수 단위로 세지 않았다.
