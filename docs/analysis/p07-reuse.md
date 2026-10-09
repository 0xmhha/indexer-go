# indexer-go 재사용 분석 (10/3)

> 10/9: 이 문서의 결론(P07을 indexer-go 위에 만들지 않고 따로 유지)은 R6-3이 대체했다. P07은 `examples/receipts`에서 `pkg/sdk` 위에 다시 만들었고 인수 시험을 통과한다. 이 문서가 인용한 `pkg/resilience`, `fetchBlockAndReceiptsWithRetry` 등은 지금 없다.

> 이 문서는 nu-54v-dk-toy 저장소(NU-54V-DK 결제 프로젝트)에서 작성해 2026-10-03에 이 저장소로 옮겼다. 본문의 N25, N08 같은 번호는 그 저장소 설계 register의 결정 번호이고, P07-FR-01 같은 번호는 그 저장소 P07 indexer의 요구사항 번호다.

P07 최소 indexer는 직접 폴링 방식으로 만들었다. 하지만 nu-54v-dk-toy 저장소의 P07 기획(`docs/content/products/p07/plan.md`) 4절은 `indexer-go`를 먼저 재사용할 수 있는지 판단하라고 정한다. 이 문서가 그 판단을 대신한다. 순서는 셋이다. 먼저 `indexer-go`(백엔드)와 `indexer-ui`(프런트엔드)의 코드를 AST로 읽어 그래프로 만들고 구조를 본다. 다음으로 P07이 서비스로 돌아가는 데 필요한 기능을 정리한다. 마지막으로 그 기능마다 `indexer-go`에서 가져올 수 있는지, 가져오면 무엇이 함께 딸려 오는지를 적는다.

대상은 `stable-platform/indexer-go` 커밋 `5baff64`와 같은 디렉터리의 `indexer-ui`다. 구조 문제와 대용량 서비스 관점의 평가는 [indexer-go 구조 검토](structure-review.md)에 따로 있다. 그래프 도구와 분석 스크립트는 nu-54v-dk-toy 저장소의 `products/p10-platform/tools/astgraph`에 있다.

## 1. 결론

`indexer-go`를 P07의 수집·저장 엔진으로 바꾸는 것은 권장하지 않는다. 대신 작은 독립 부품 넷을 가져오고, 네 가지 구현 방식을 참고한다.

- **엔진을 바꾸지 않는 이유.** `indexer-go`는 체인 전체(모든 블록, 트랜잭션, receipt, 로그)를 PebbleDB에 색인하는 탐색기 백엔드다. 수집(`pkg/fetch`), 이벤트(`pkg/events`), 복원력(`pkg/resilience`)이 모두 저장소 `pkg/storage`(PebbleDB, 17,152줄)에 묶여 있다. 그래서 수집 기능 하나만 가져와도 수천 줄과 PebbleDB가 함께 온다. P07은 컨트랙트 하나의 이벤트 하나만 필요하다. 저장소는 기술 스택 결정(N25)대로 PostgreSQL이다.
- **가져올 부품.** HTTP 미들웨어, WebSocket hub, ABI decoder, webhook 알림이다. 넷 다 다른 패키지에 기대지 않는 200~400줄 크기다(4절).
- **참고할 방식.** RPC 재시도와 지수 backoff, Prometheus 지표, YAML 설정, zap 로거다.
- **indexer-ui.** `indexer-go`의 GraphQL 스키마에 맞춰 만들어져 있어 P07 API로는 돌릴 수 없다. 운영자용 체인 탐색기가 필요하면 `indexer-go`와 함께 별도로 띄운다(5절).

## 2. 방법: AST 그래프

`golang.org/x/tools/go/packages`로 모듈 전체를 타입 정보까지 읽어 그래프를 만들었다. 시험 파일은 뺐다. 대상의 `go.mod`를 바꾸지 않도록 `GOWORK=off GOFLAGS=-mod=readonly`로 읽었다.

| 그래프 | 수 |
|---|---|
| 노드 | 패키지 42, 함수 763, 메서드 1,737, 타입 614 |
| 간선 | 정적 호출 3,227, 타입 참조 4,438, 인터페이스 구현 156, 패키지 import 103, 외부 모듈 import 79 |
| 크기 | `cmd/indexer`가 끌어오는 코드 75,602줄(시험 제외) |

판단에 쓴 지표는 둘이다.
- **import 폐포.** 패키지 하나를 import하면 컴파일에 함께 들어오는 모듈 안 패키지와 외부 모듈이다.
- **선언 폐포.** 함수나 타입 하나에서 정적 호출과 타입 참조를 따라가면 닿는 선언 전체다. 그 기능만 떼어 복사할 때 딸려 오는 코드의 양이다.

## 3. indexer-go의 구조

import 폐포를 크기순으로 보면 층이 뚜렷하다. 아래는 그중 대표 행이다.

| 패키지 | 자체 줄 수 | import 폐포 줄 수 | 폐포 안 외부 모듈 | 뜻 |
|---|---|---|---|---|
| `pkg/client` | 357 | 357 | geth, zap | 독립. 배치 RPC가 있다 |
| `pkg/api/middleware` | 447 | 447 | x/time, zap | 독립 |
| `pkg/api/websocket` | 525 | 525 | gorilla/websocket, zap | 독립 |
| `pkg/abi` | 715 | 715 | geth | 독립 |
| `pkg/storage` | 17,152 | 18,432 | geth, **pebble**, zap | 허브. fan-in 13 |
| `pkg/resilience` | 1,158 | 19,590 | pebble 포함 | storage에 묶임 |
| `pkg/events` | 4,994 | 23,426 | pebble, prometheus 포함 | storage에 묶임 |
| `pkg/fetch` | 5,886 | 29,685 | pebble, prometheus 포함 | storage, events에 묶임 |
| `pkg/api/graphql` | 18,527 | 61,344 | graphql, pebble 외 | 탐색기 API |

`pkg/storage`가 중심이다. 13개 패키지가 이 패키지를 import하고, 이 패키지는 PebbleDB의 키 설계(블록, 트랜잭션, 주소, 토큰, UserOp, 로그 색인)를 모두 담고 있다. 그 위에 수집, 이벤트, API가 쌓이고, 외곽에 독립 유틸리티가 있다.

## 4. P07에 필요한 기능과 가져올 수 있는 것

P07의 지금 구현은 SRS의 P07-FR-01~06을 모두 만족한다.
- 정산 컨트랙트의 PaymentSettled만 수집한다.
- cursor로 재시작하고, 같은 로그는 한 번만 저장하고, (merchant, orderId)로 조회한다.
- 중복이면 표시하고, 수집이 뒤처지면 503을 돌려준다.

testnet에서 조회 지연 0.12초도 확인했다. 아래 표는 이 구현을 서비스로 운영하려면 더 있어야 하는 것과, 그것을 `indexer-go`에서 어떻게 얻을 수 있는지를 대응시킨 것이다.

| 필요 기능 | 지금 P07 | indexer-go의 대응 | 선언 폐포(딸려 오는 것) | 방식 |
|---|---|---|---|---|
| HTTP 보호: 요청 속도 제한, panic 복구, 요청 로그, API 키 | 없음 | `pkg/api/middleware` | 22개 선언, 278줄. 외부는 zap, x/time/rate | **가져온다** |
| 실시간 알림: 영수증이 생기면 키오스크·백오피스에 밀어 주기 | 없음(키오스크가 다시 묻는다) | `pkg/api/websocket` Hub, Server | 31개, 371줄. gorilla/websocket, zap | **가져온다**(쓸 때) |
| 다른 이벤트의 decode: CashOut, Deposit, Withdrawal 등 백오피스 대사용 | PaymentSettled 하나를 손으로 decode | `pkg/abi` Decoder | 12개, 210줄. geth만 | **가져온다**(이벤트를 늘릴 때) |
| 운영 알림: 수집이 멈추거나 뒤처질 때 webhook | 없음(healthz만) | `pkg/notifications` WebhookHandler, 서명 검증 | 17개, 235줄. zap | **가져온다** |
| RPC 재시도와 지수 backoff | 다음 폴링 주기에 같은 범위를 다시 읽는 것뿐 | `fetch.fetchBlockAndReceiptsWithRetry` | 메서드 하나지만 Fetcher가 storage에 묶여 있다 | **참고**(약 30줄 패턴) |
| 지표: cursor, lag, RPC 오류, 처리량 | 없음(healthz의 cursor·lag) | `pkg/fetch/metrics.go`, eventbus의 Prometheus 사용 | prometheus client | **참고** |
| 설정 파일과 로거 | 환경 변수, 표준 log | `internal/config`(YAML), `internal/logger`(zap, 151줄) | config 1,685줄 | **참고**. 로거만 가져오는 것도 가능 |
| 배치 RPC | 범위 단위 `eth_getLogs` 한 번이라 필요 없다 | `pkg/client` BatchGetReceipts | 23개, 236줄 | 지금은 불필요 |
| gap 감지와 복구 | 구조상 gap이 없다(로그와 cursor를 한 트랜잭션으로 저장) | `Fetcher.DetectGaps/FillGaps` | 211개, 5,997줄. storage, events 포함 | 불필요. 블록마다 저장하는 모델용이다 |
| 컨트랙트 등록과 ABI 기반 동적 이벤트 파싱 | 정산 컨트랙트 고정 | `events.ContractRegistrationService`, DynamicEventParser | 150개, 1,749줄. prometheus 포함 | 지금은 불필요. 이벤트가 여럿이 되면 `pkg/abi`로 충분하다 |
| 로그 색인 저장 | PostgreSQL 인덱스 `(merchant, order_id, block_number)` | `storage.PebbleStorage` 로그 함수 | 37개, 631줄. pebble | **가져올 수 없다**(N25와 다른 저장소) |
| 백오피스 조회: 가맹점·기간별 목록, 일/주/월 합계, 페이지 | 없음(단건 조회뿐) | GraphQL `logs(filter, pagination)`, 연결형 페이지 | 18,527줄 API 전체 | **참고**(페이지와 필터의 모양만). P05 백오피스 작업 때 PostgreSQL 질의로 만든다 |
| 재색인 명령: 지정 블록부터 다시 쌓기 | cursor를 지우면 배포 블록부터 다시 읽는다 | `start_height` 설정 | — | 지금 방식으로 충분 |
| RPC 여러 개 사이 failover | 없음 | 없음(엔드포인트 하나) | — | 해당 없음 |
| reorg 처리 | finalized만 읽어 필요 없다 | 블록 단위 처리 | — | 해당 없음 |

## 5. indexer-ui

`indexer-ui`는 Next.js 탐색기다. 블록, 트랜잭션, 주소, 컨트랙트, 시스템 컨트랙트, UserOp, bundler, paymaster, 합의(epoch) 화면이 있고, 모든 데이터를 `indexer-go`의 GraphQL과 GraphQL 구독(`logs`, `dynamicContractEvents` 등)으로 받는다. P07의 REST API(`/receipts`)는 이 스키마와 다르므로 그대로 붙지 않는다.

쓰는 방법은 둘이다.
- **운영자 탐색기로 따로 띄운다.** `indexer-go`를 `start_height`를 정산 컨트랙트 배포 블록으로 두고 띄우고, 그 앞에 `indexer-ui`를 둔다. P07은 그대로다. 장점은 결제 트랜잭션과 이벤트를 화면에서 바로 볼 수 있다는 것이다. 단점은 indexer가 둘이 되어 운영 부담이 늘고, 체인 전체를 색인하는 디스크와 시간이 든다는 것이다. 설정에 있던 호스팅 주소(`testnet-indexer.stablenet.io`)는 DNS에 없다(10/3 확인).
- **화면 구성만 참고한다.** P05 백오피스 웹(`products/p05-operations-backoffice/web`)을 만들 때 표, 상세, 검색 화면의 구성을 참고한다.

## 6. 권장과 그 단점

1. **지금 P07을 유지하고, 서비스 단계에서 미들웨어와 webhook 알림을 가져온다.**
   - 장점: register를 바꾸지 않는다. 가져오는 코드는 두 부품 합쳐 약 500줄이고, 외부 의존은 zap과 x/time 정도다.
   - 단점: 복사한 코드는 `indexer-go`의 수정을 자동으로 따라가지 않는다. 출처 커밋을 적어 두고 직접 맞춰야 한다.
2. **WebSocket hub와 ABI decoder는 필요한 기능이 생길 때 가져온다.** 실시간 영수증 알림이 필요할 때, 백오피스 대사에 이벤트를 늘릴 때다.
   - 단점: 그 전까지 키오스크는 영수증을 다시 묻는 방식(최대 10초)을 쓴다.
3. **가져오는 방식은 Go 모듈 import보다 복사를 권장한다.** [Mid]
   - 이유: `indexer-go` 모듈을 import하면 쓰지 않는 의존(pebble, kafka-go, redis, graphql)까지 go.sum과 모듈 그래프에 들어온다. 또 그 모듈의 Go 버전과 go-ethereum 버전(1.16.5, P07은 1.16.4)에 묶인다.
   - 단점: 복사본을 직접 관리해야 한다.

## 7. 라이선스

`indexer-go`와 `indexer-ui`의 LICENSE 파일은 Apache License 2.0이다. 다만 `indexer-go`의 README 배지는 MIT라고 적고 있어 두 표기가 다르다. 이 저장소도 Apache-2.0이므로 함께 쓸 수 있다. 코드를 복사할 때는 원본의 저작권 표시를 남기고, 바꾼 파일에는 바꿨다는 표시를 붙인다(Apache 2.0 4절).

## 8. 한계

- 정적 분석이다. 인터페이스를 거친 호출은 대상을 알 수 없어 호출 간선에 없다. 그래서 선언 폐포는 실제보다 작게 나올 수 있다. 다만 결론을 가른 차이(독립 부품 200~400줄 대 storage 묶음 수천~수만 줄)는 이 오차보다 크다.
- `indexer-go`를 testnet에 실제로 띄워 보지는 않았다. 5절의 첫째 방법을 고르면, StableNet 8283에서 Stable-One 어댑터가 동작하는지부터 확인해야 한다.
- `indexer-ui`는 데이터 접근 방식(GraphQL 질의와 구독)만 확인했다. 코드 그래프는 만들지 않았다(TypeScript 프로젝트다).
