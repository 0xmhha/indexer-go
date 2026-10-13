# indexer-go

EVM 체인의 블록·거래·receipt·로그를 색인하고 API와 실시간 구독으로 제공하는 Go 프레임워크다. 체인별 디코딩·회계·기능은 별도 profile과 feature로 커스터마이징하며 공통 코어는 체인 중립 모델과 저장 포트를 사용한다.

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## 1. 현재 범위

- 내장 profile: `evm`(Ethereum 호환), `stablenet`(StableNet/WBFT·fee delegation), `arc`(USDC native 회계).
- Stable(Tendermint/CometBFT)은 현재 범위 밖이며 StableNet과 별개다.
- 저장소: Pebble 또는 PostgreSQL. 기능: 주소·토큰·AA 색인, 선택적인 DEX·집계·선언형 records와 프로젝트 SDK 처리기.
- API: GraphQL, JSON-RPC, REST, GraphQL 구독, 인증된 알림 stream. 멀티체인은 체인별 DB와 `/chains/{id}/` API를 사용한다.
- 현재 공개 테스트넷 표본은 StableNet·BSC·Sepolia·Arc에서 검증했다. 모든 체인·fork의 전체 회계, mainnet 또는 실제 동시 운영을 보장하는 결과는 아니다.

자세한 구조는 [아키텍처](docs/ARCHITECTURE.md), 검증 범위와 남은 일은 [작업 목록](docs/analysis/open-items.md)에 있다.

## 2. 빌드와 시작

Go 모듈은 `go 1.24.0`, toolchain `go1.24.9`다. 버전 정보를 포함하는 빌드는 Makefile을 사용한다.

```bash
go mod download
make build
./build/indexer-go --version
```

[빠른 시작](docs/QUICKSTART.md)에 따라 설정과 시작 높이를 정한다. 운영은 full node이며 archive가 없다. `balance.native`·`token.metadata`가 필요로 하는 과거 상태가 없으면 시작을 거부한다. 노드에 남아 있는 최근 상태, 시작 높이와 활성 기능을 확인해야 한다. 최근 시작한 DB에는 이전 블록의 색인이 없다.

현재 Arc profile은 발행 tag `v0.2.15` 이후 main에 추가됐다. 외부 프로젝트는 필요한 profile이 포함된 commit 또는 발행 버전을 고정한다. 기존 generic EVM 회계로 만든 Arc DB는 재색인이 필요하며 자동 보정하지 않는다.

## 3. 프로젝트 커스터마이징

- [체인 확장 계약](docs/CHAIN_CUSTOMIZATION.md): 감지·디코딩·native 회계·선택 인터페이스·binary·검증 책임.
- [SDK](docs/SDK.md): 프로젝트 feature, KV, API, 결정성 및 호환성 약속.
- [별도 profile 모듈](examples/chainprofile/README.md): Arc 규칙을 유지하는 프로젝트 테스트넷 profile.
- [receipts 모듈](examples/receipts/README.md): 선언형 records와 프로젝트 기능·HTTP API.

자기 바이너리에 등록 패키지를 연결한다. YAML이 새 코드를 동적으로 로드하지 않는다. `pkg/chains` 등 profile API는 SDK 호환성 약속 밖이며, 버전 고정과 업그레이드 검증이 필요하다.

## 4. 검증

```bash
gofmt -l $(git ls-files '*.go')
make build
make lint
make test-race
make test-postgres
make test-examples
```

SLO·알림·부하 경로를 바꾸면 `make test-slo`도 실행한다. PostgreSQL 검증은 전용 임시 컨테이너를 사용한다. 공개 RPC 검증은 opt-in이며 기본 시험·CI가 외부 RPC를 호출하지 않는다. 실행 방법은 [작업 기록](docs/analysis/work-log-2026-10.md)과 CLAUDE.md Testing에 있다.

PR과 main CI는 lint·test·postgres·examples를 실행한다. CI에는 race job이 없으므로 커밋 전 로컬 race 검증이 필요하다. 성능 측정은 환경과 입력 범위의 결과이며 운영 목표를 대신하지 않는다.

## 5. 문서와 작업 상태

[문서 목차](docs/README.md)에서 시작한다. API·설정·구조 설명은 각 문서에 두고, 이 README에는 중복된 전체 API 목록이나 배포 명령을 두지 않는다.

- [남은 작업](docs/analysis/open-items.md): Arc 회계 확대, 멀티체인 동시 검증, full node 운영 설정, 운영 성능과 별도 조건 항목.
- [결정·측정 기록](docs/analysis/work-log-2026-10.md): 완료 작업과 실패·측정 근거.
- [frontend 이전](docs/FRONTEND_MIGRATION.md): 별도 저장소의 후속 작업.

라이선스는 [Apache License 2.0](LICENSE)이다.
