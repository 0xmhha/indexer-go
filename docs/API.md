# API 안내

단일 체인의 full 모드와 `all` 역할을 기준으로 설명한다. 기능·체인·역할·declared 모드에 따라 제공 범위가 달라진다. 전체 필드와 인자는 [GraphQL snapshot](../pkg/app/testdata/api/graphql-schema.txt), 메서드 이름은 [JSON-RPC snapshot](../pkg/app/testdata/api/jsonrpc-methods.txt)에서 확인한다. Snapshot은 시험 구성의 API를 고정한 목록이며 기능 활성화나 실제 노드 지원까지 보장하지 않는다.

## 1. Endpoints

| 경로 | 용도 |
|---|---|
| `/graphql` | GraphQL 조회·조작 |
| `/playground` | GraphQL Playground와 introspection |
| `/graphql/ws` | GraphQL 구독 |
| `/rpc` | JSON-RPC |
| `/v1/...` | 자주 조회하는 경로의 REST |
| `/v1/subscriptions/stream` | API key 소유자의 알림 stream |
| `/api` | Etherscan 호환 API |
| `/ws` | 별도 hub. 현재 색인 파이프라인의 이벤트가 연결되지 않음 |
| `/health`, `/metrics` | 상태·Prometheus 지표 |

멀티체인은 `/chains/{id}/graphql`, `/chains/{id}/rpc`, `/chains/{id}/v1/...`처럼 체인별 경로를 사용한다. 체인 목록은 `GET /chains`다. 루트 탐색 API가 있다고 가정하지 않는다. `ingest` 역할의 HTTP는 health·metrics만 제공한다.

## 2. GraphQL API

높이·금액·해시·주소처럼 모델의 큰 숫자/hex 값은 해당 스키마의 String 형식을 따른다. 반환 필드와 인자는 snapshot 또는 실제 introspection으로 확인한다. 기본 블록 필드는 `number`, `timestamp`, `transactionCount`다.

```graphql
query {
  latestHeight
}
```

```graphql
query BlockAt($number: String!) {
  block(number: $number) {
    number
    hash
    parentHash
    timestamp
    transactionCount
    gasUsed
    baseFeePerGas
  }
}
```

```graphql
query TransactionByHash($hash: String!) {
  transaction(hash: $hash) {
    hash
    from
    to
    value
    blockNumber
  }
}
```

`block(height: ...)`, `Block.height`, `Block.time`, `Block.numTxs`는 현재 snapshot의 이름이 아니다. 최근 시작한 DB의 이전 블록이나 아직 색인하지 않은 값이 항상 존재한다고 가정하지 않는다.

### Pagination

`PaginationInput`은 `limit`, `offset`, `after`를 제공한다. Cursor가 지원되는 목록에서는 반환된 `pageInfo.endCursor`를 다음 `after`로 보낸다. Cursor는 불투명한 값이며 클라이언트가 새로 만들지 않는다. 각 목록의 필터·순서·범위 제한은 해당 resolver와 [설정](CONFIG.md)을 확인한다.

```graphql
query AddressTransactions($address: String!, $after: String) {
  transactionsByAddress(address: $address, pagination: {limit: 20, after: $after}) {
    nodes { hash blockNumber from to value }
    pageInfo { hasNextPage endCursor }
    totalCount
  }
}
```

넓은 범위 또는 색인이 없는 조건은 scan 상한과 `scannedThrough`로 조회한 범위를 알릴 수 있다. DEX·집계·records·AA·StableNet 전용 조회는 기능 설정과 저장된 자료가 필요하다. 프로젝트 조회는 [확장 방법](EXTENDING.md)으로 추가한다.

## 3. REST

대표 경로는 다음과 같다.

- `GET /v1/blocks`
- `GET /v1/transactions`
- `GET /v1/addresses/{address}/balance`
- `GET /v1/addresses/{address}/overview`
- `GET /v1/addresses/{address}/tokens`
- `GET /v1/addresses/{address}/transactions`
- `GET /v1/stats/miners`, `GET /v1/stats/network`

고정된 GraphQL 문서를 같은 resolver로 실행하며 응답은 `data`/`errors` 구조다. 성공 응답의 ETag·Cache-Control과 1초 cache를 사용한다. 상세 인자·범위·오류 동작은 [설정의 REST 설명](CONFIG.md)과 `internal/api/rest`를 확인한다. frontend 이전 경로는 [이전 안내](FRONTEND_MIGRATION.md)에 있다.

## 4. JSON-RPC API

```bash
curl -s http://127.0.0.1:8080/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"getLatestHeight","params":{},"id":1}'
```

프로젝트 메서드 이름과 Ethereum 호환 메서드 일부를 제공한다. 모든 Ethereum RPC가 구현돼 있다고 가정하지 않는다. 예를 들어 현재 목록에는 `getLatestHeight`, `getBlock`, `getTxResult`, `getTxReceipt`, `eth_getLogs`, filter 메서드가 있고 `eth_subscribe`는 없다. 인자·반환 형식은 `pkg/api/jsonrpc`와 체인별 method 등록 코드를 확인한다.

## 5. WebSocket·구독·알림

색인 이벤트 구독은 `/graphql/ws`를 사용한다. `graphql-transport-ws` 프로토콜을 사용할 수 있으며 연결은 `connection_init` 뒤 `subscribe`로 구독한다. 예를 들면 다음 GraphQL 문서다.

```graphql
subscription {
  newBlock { number hash timestamp transactionCount }
}
```

Outbox가 켜져 있으면 `extensions.sequence`와 재개 변수 `fromSequence`로 재동기화할 수 있다. `fromSequence`는 구독 처리기의 재개 변수이며 위 schema snapshot의 필드 인자로 나타나지 않는다. Queue overflow·`SLOW_SUBSCRIBER`·`SEQUENCE_TOO_OLD` 처리와 snapshot 절차는 [CONFIG](CONFIG.md)와 [구독 설계](analysis/subscriptions-design.md)에 있다.

API key 알림은 `/v1/subscriptions/stream`을 사용한다. 설정은 만든 key의 소유이며 조건식·fast/durable·webhook challenge·채널 설정은 [Notifications 설정](CONFIG.md#notifications)을 따른다. Fast 경로는 commit 전 평가이고 durable 경로는 outbox 소비다. 일반 GraphQL 구독과 같은 전달 보장이라고 가정하지 않는다.

`/ws` hub의 프로토콜은 존재하지만 현재 수집 파이프라인이 그 hub로 이벤트를 보내지 않는다. 이를 새 블록의 운영 구독 경로로 안내하지 않는다.

## 6. 접근과 검증 범위

알림 설정 API는 `api.keys`의 key가 필요하다. Key 소유권·rate limit·trusted proxy·CORS·GraphQL depth/complexity·WebSocket Origin 검사 등 접근 조건은 [설정](CONFIG.md)에 있다.

API snapshot과 로컬 시험은 이름·계약·회귀를 검증한다. 실제 노드의 전체 데이터·내부 이동·native 잔액 정확성 또는 모든 체인의 동시 운영을 보장하지 않는다. 미검증 영역과 후속 작업은 [open-items](analysis/open-items.md), 실제 공개 RPC 표본은 [작업 기록](analysis/work-log-2026-10.md)에 있다.
