# AA (EIP-4337) Backend Enhancement Tasks

> indexer-go 백엔드에서 Account Abstraction 기능 강화를 위해 필요한 작업 목록

## 배경

프론트엔드에서 Bundler/Paymaster 목록 페이지를 구현했으나, 현재 백엔드에는 "전체 Bundler 목록" / "전체 Paymaster 목록"을 반환하는 쿼리가 없음. 프론트엔드는 `recentUserOps(200)` → 고유 주소 추출 → 개별 `bundlerStats` 조회 방식으로 우회 중이나, 이는 **최근 활동한 주소만 표시**되는 한계가 있음.

## Task 1: `allBundlers` 목록 쿼리 추가

### 1-1. Storage Interface 확장 (`pkg/storage/userop.go`)

`UserOpIndexReader`에 추가:
```go
GetAllBundlerStats(ctx context.Context, limit, offset int) ([]*BundlerStats, error)
GetAllBundlerStatsCount(ctx context.Context) (int, error)
```

### 1-2. Pebble 구현 (`pkg/storage/pebble_userop.go`)

- `AABundlerStatsAllPrefix()` 로 prefix scan → 모든 BundlerStats 반환
- Pagination: offset/limit 적용
- Count: prefix 하위 엔트리 수 카운트

### 1-3. GraphQL Type (`pkg/api/graphql/types_userop.go`)

`BundlerStatsConnection` 타입 추가:
```graphql
type BundlerStatsConnection {
  nodes: [BundlerStats!]!
  totalCount: Int!
  pageInfo: PageInfo!
}
```

### 1-4. GraphQL Resolver (`pkg/api/graphql/resolvers_userop.go`)

`resolveAllBundlers` 함수 추가

### 1-5. Schema 등록 (`pkg/api/graphql/schema.go`)

`WithUserOpQueries()`에 `allBundlers` 쿼리 등록

---

## Task 2: `allPaymasters` 목록 쿼리 추가

Task 1과 동일 패턴으로:
- `GetAllPaymasterStats` / `GetAllPaymasterStatsCount`
- `PaymasterStatsConnection` 타입
- `resolveAllPaymasters` resolver
- `allPaymasters` 쿼리 등록

---

## Task 3: 프론트엔드 연동

백엔드 배포 후:
1. `lib/apollo/queries/aa.ts`에 `GET_ALL_BUNDLERS` / `GET_ALL_PAYMASTERS` 쿼리 추가
2. `useBundlerList.ts` / `usePaymasterList.ts`에서 새 쿼리 사용하도록 전환
3. Fallback: 새 쿼리 실패 시 기존 recentUserOps 기반 로직 유지

---

## 파일 변경 요약

| File | 변경 내용 |
|------|----------|
| `pkg/storage/userop.go` | Interface에 4개 메서드 추가 |
| `pkg/storage/pebble_userop.go` | 4개 메서드 구현 (prefix scan) |
| `pkg/api/graphql/types.go` | 변수 선언 2개 추가 |
| `pkg/api/graphql/types_userop.go` | Connection 타입 2개 추가 |
| `pkg/api/graphql/resolvers_userop.go` | Resolver 2개 추가 |
| `pkg/api/graphql/schema.go` | 쿼리 2개 등록 |

## GraphQL Schema (최종)

```graphql
# 새로 추가되는 쿼리
type Query {
  allBundlers(pagination: PaginationInput): BundlerStatsConnection!
  allPaymasters(pagination: PaginationInput): PaymasterStatsConnection!
}

type BundlerStatsConnection {
  nodes: [BundlerStats!]!
  totalCount: Int!
  pageInfo: PageInfo!
}

type PaymasterStatsConnection {
  nodes: [PaymasterStats!]!
  totalCount: Int!
  pageInfo: PageInfo!
}
```
