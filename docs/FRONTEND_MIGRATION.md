# indexer-frontend 수정 가이드

이 문서는 indexer-frontend가 지금 indexer-go API와 맞지 않는 곳과, 새로 생긴 API(REST, 구독 재동기화, 보안 제한)에 맞추는 방법을 정리한다. 기준은 indexer-go main(2026-10-08)과 indexer-frontend `e0f8100`(2026-03-31)이다.

- 1장은 지금 동작하지 않는 GraphQL 문서 26개를 고치는 방법이다. 가장 먼저 할 일이다.
- 2장은 폴링을 REST로 옮기는 방법이다. 선택 사항이지만 서버 부하를 줄인다.
- 3장은 구독, 4장은 보안 제한이 frontend에 주는 영향이다.
- 5장은 수정한 뒤 확인하는 방법이다.

---

## 1. 동작하지 않는 GraphQL 문서 26개

### 배경

indexer-go의 `TestFrontendDocuments`는 frontend 소스의 GraphQL 문서 137개를 서버 스키마로 검증한다. 검증 조건은 단일 체인 프로세스가 서비스하는 스키마이고, RPC proxy 쿼리가 포함된다. 이 중 26개가 실패한다. GraphQL은 문서에 모르는 필드나 인자가 하나라도 있으면 문서 전체를 거절하므로, 해당 화면은 데이터를 하나도 받지 못한다.

26개는 이번 리팩터링 때문에 깨진 것이 아니다. 리팩터링 직전 스키마(`2c0daaf^`)에서도 같은 이유로 실패했다. 원인은 셋이다.

- 2026년 4월 ERC-4337 색인을 다시 만들면서 API 이름이 바뀌었는데(`6f408a6`, `08b2341`), frontend는 예전 이름을 쓴다.
- indexer-go의 `docs/API.md` AA 절이 아직 예전 이름(`userOp`, `userOpsByBundler` 등)으로 적혀 있다. frontend는 이 문서를 따른 것으로 보인다.
- 동적 컨트랙트 등록 서비스는 어느 프로세스에도 연결되어 있지 않다.

수정 방법에 따라 네 묶음으로 나눈다.

| 묶음 | 뜻 | 문서 수 |
|---|---|---|
| A | frontend에서 이름과 필드만 바꾸면 된다 | 12 |
| B | 대응하는 쿼리는 있지만 frontend가 쓰는 필드 일부를 서버가 저장하지 않는다 | 5 |
| C | 서버 저장소에는 조회가 있지만 GraphQL로 열려 있지 않다. 백엔드 작업이 필요하다 | 4 |
| D | 서버가 아예 제공하지 않는 기능이다 | 5 |

아래에 적은 대체 쿼리는 모두 지금 스키마로 검증해 통과한 것이다.

### A. 이름과 필드만 바꾸면 되는 문서

UserOperation 필드는 다음처럼 바꾼다. 여러 문서에 공통으로 쓰인다.

| frontend가 쓰는 필드 | 지금 필드 | 비고 |
|---|---|---|
| `userOpHash` | `hash` | |
| `txHash` | `transactionHash` | |
| `success` | `status` | Boolean |
| `blockHash`, `blockNumber`, `sender`, `paymaster`, `nonce`, `actualGasCost`, `bundler`, `entryPoint`, `timestamp` | 같은 이름 | |
| `txIndex` | 없음 | 필요하면 `transaction(hash)`로 거래의 index를 읽는다 |
| `logIndex` | 없음 | `userLogsStartIndex`·`userLogsCount`는 UserOp가 남긴 로그 범위로, 뜻이 다르다 |
| `actualUserOpFeePerGas` | 없음 | `actualGasCost / gasUsed`로 계산한다 |

| 파일 · 문서 | 지금 쓸 쿼리 | 바꿀 것 |
|---|---|---|
| `lib/apollo/queries/aa.ts` `GetUserOp` | `userOperation(hash: String!)` | 인자 `userOpHash` → `hash`. 위 필드 표대로 바꾼다 |
| `aa.ts` `GetUserOpsBySender` | `userOperationsBySender(sender, pagination)` | 반환은 같은 connection 모양(`nodes`, `totalCount`, `pageInfo`)이다. 필드 표대로 바꾼다 |
| `aa.ts` `GetUserOpCount` | `userOperationCount` | 이름만 바꾼다 |
| `aa.ts` `GetRecentUserOps` | `userOperations(pagination: {limit})` | `sender`를 주지 않으면 최근 UserOp를 준다(저장소 `GetRecentUserOps`). 반환은 connection이므로 `nodes { ... }` 안에 필드를 둔다 |
| `aa.ts` `GetUserOpRevert` | `userOperation(hash)` | `status`가 false이면 실패한 것이고, 실패 이유는 `revertReason`이다. `revertType`은 없다 |
| `aa.ts` `GetAllBundlers` | `bundlers(pagination)` | 노드 필드는 B의 `GetBundlerStats`를 따른다 |
| `aa.ts` `GetAllPaymasters` | `paymasters(pagination)` | 노드 필드는 B의 `GetPaymasterStats`를 따른다 |
| `lib/apollo/queries/module.ts` `GetListModuleStats` | `listModuleStats(pagination)` | `moduleType` 인자가 없다. 받은 노드의 `moduleType`으로 frontend에서 거른다. `installedModules(moduleType)`는 모듈 통계가 아니라 설치 기록 목록이다 |
| `module.ts` `GetModuleEventCount` | `moduleEventCount` | 인자가 없고 전체 설치 기록 수만 준다. 계정별 수는 `installedModules(account) { totalCount }`로 읽는다 |
| `lib/graphql/queries/address-indexing.ts` `GetERC20Transfer` | `erc20Transfer(transactionHash, logIndex)` | 인자 `txHash` → `transactionHash` |
| `address-indexing.ts` `GetERC721Transfer` | `erc721Transfer(transactionHash, logIndex)` | 인자 `txHash` → `transactionHash` |
| `lib/graphql/queries/rpcProxy.ts` `RPCProxyMetrics` | `rpcProxyMetrics` | `averageLatency` → `averageLatencyMs`(밀리초) |

예를 들어 `GetUserOp`는 다음처럼 바꾼다.

```graphql
query GetUserOp($hash: String!) {
  userOperation(hash: $hash) {
    hash
    transactionHash
    blockNumber
    blockHash
    sender
    paymaster
    nonce
    status
    actualGasCost
    gasUsed
    bundler
    entryPoint
    timestamp
  }
}
```

### B. 필드 일부를 서버가 저장하지 않는 문서

이 문서들은 대응하는 쿼리로 바꾸면 동작한다. 다만 frontend가 화면에 보여 주던 값 일부는 서버에 없다. 그 값을 화면에서 빼거나, 백엔드에 통계 추가를 요청해야 한다(백엔드 작업은 저장 형식 변경과 재색인이 필요하다).

| 파일 · 문서 | 지금 쓸 쿼리와 필드 | 서버에 없는 값 |
|---|---|---|
| `aa.ts` `GetBundlerStats` | `bundler(address) { address totalOps totalBundles }` | `successfulOps`, `failedOps`, `totalGasSponsored`, `lastActivityBlock`, `lastActivityTime` |
| `aa.ts` `GetPaymasterStats` | `paymaster(address) { address totalOps }` | 위와 같음 |
| `aa.ts` `GetAccountDeployment` | `userOperation(hash) { hash sender factory paymaster transactionHash blockNumber timestamp }`. `factory`가 있으면 그 UserOp가 계정을 만든 것이다. 계정 주소로 찾을 때는 `smartAccount(address) { factory creationOpHash creationTxHash creationTimestamp }` | `logIndex` |
| `aa.ts` `GetDeploymentsByFactory` | factory의 계정 수만 있다: `factory(address) { totalAccounts }` | factory별 배포 목록. 저장소에는 `GetUserOpsByFactory`가 있으므로 C 묶음과 같은 백엔드 작업으로 열 수 있다 |
| `lib/graphql/queries/system-contracts.ts` `GetDepositMintProposals` | `depositMintProposals(filter: { status, fromBlock, toBlock }) { proposalId to amount depositId status blockNumber transactionHash timestamp }`. `status`는 인자가 아니라 `filter` 안에 넣는다 | `requester`, `beneficiary`(`to`가 받는 주소다), `bankReference` |

### C. 백엔드에 GraphQL 쿼리 추가가 필요한 문서

서버 저장소(`port.UserOpIndexReader`)에는 아래 조회가 이미 있다. GraphQL 필드만 추가하면 되므로 백엔드 작업은 작다. 추가하기 전까지 frontend에서는 이 화면을 숨기거나, 아래 대안을 쓴다.

| 파일 · 문서 | 저장소 조회 | 추가 전 대안 |
|---|---|---|
| `aa.ts` `GetUserOpsByBundler` | `GetUserOpsByBundler` | 없음 |
| `aa.ts` `GetUserOpsByPaymaster` | `GetUserOpsByPaymaster` | 없음 |
| `aa.ts` `GetUserOpsByTx` | `GetUserOpsByTx` | 없음 |
| `aa.ts` `GetUserOpsByBlock` | `GetUserOpsByBlock` | 없음 |

백엔드에 요청할 때 필드 이름은 A의 지금 이름(`userOperationsByBundler` 등, `UserOperationConnection` 반환)으로 맞추기를 권장한다. 그래야 A에서 고친 필드 목록을 그대로 쓸 수 있다.

### D. 서버가 제공하지 않는 기능

동적 컨트랙트 등록(`registerContract`, `unregisterContract`, `registeredContracts`, `registeredContract`)과 `dynamicContractEvents`는 indexer-go 코드에 있지만, 등록 서비스를 만드는 프로세스가 없어 어떤 서버도 서비스하지 않는다. 구독 `dynamicContractEvents`는 WebSocket 서버가 알아보는 구독 이름에도 없다.

| 파일 · 문서 |
|---|
| `system-contracts.ts` `RegisterContract`, `UnregisterContract`, `GetRegisteredContracts`, `GetRegisteredContract` |
| `system-contracts.ts` `DynamicContractEvents`(구독) |

frontend에서는 이 기능을 숨기는 것을 권장한다. 기능이 필요하면 백엔드에서 서비스 연결부터 결정해야 한다(등록 정보를 어디에 저장하고, API 프로세스와 색인 프로세스 중 누가 쓰는지).

---

## 2. 폴링을 REST로 옮기기 (선택)

### 왜 옮기나

frontend는 최근 블록·거래를 5초, 주소 잔액·거래를 10초, 통계를 30초마다 GraphQL로 다시 묻는다. GraphQL 요청은 POST라 브라우저와 CDN이 캐시하지 못하고, 서버는 매번 문서를 해석하고 실행한다. indexer-go는 이 조회들을 GET으로 제공한다. 응답에는 `ETag`와 `Cache-Control: public, max-age=1`이 붙는다. 같은 응답을 다시 받을 때는 304(본문 없음)로 끝나고, 앞단 캐시는 1초 동안 같은 응답을 재사용할 수 있다.

### 응답 모양

경로마다 서버가 고정된 GraphQL 문서를 같은 resolver로 실행한다. 그래서 응답 본문은 아래 표의 frontend 쿼리에 대한 GraphQL 응답과 같다(`{"data": {...}}`, resolver 오류가 있으면 `"errors"`). indexer-go의 `TestRESTMatchesFrontendQueries`가 색인한 데이터로 둘이 같은지 확인한다. 따라서 frontend는 요청 방법만 바꾸고 `data`를 읽는 코드는 그대로 둘 수 있다. 차이는 하나다. 주소별 거래의 `pageInfo`에는 `endCursor`가 더 있다.

| frontend hook · 쿼리 | REST 경로 |
|---|---|
| `useBlocks` `GetBlocks` | `GET /v1/blocks?limit&offset&numberFrom&numberTo&miner` |
| `useTransactions` `GetTransactions` | `GET /v1/transactions?limit&offset&blockNumberFrom&blockNumberTo&from&to&type` |
| `useAddressBalance` `GetAddressBalance` | `GET /v1/addresses/{address}/balance?blockNumber` |
| `useAddressOverview` `GetAddressOverview` | `GET /v1/addresses/{address}/overview` |
| `useTokenBalances` `GetTokenBalances` | `GET /v1/addresses/{address}/tokens?tokenType` |
| `useAddressTransactions` `GetTransactionsByAddress` | `GET /v1/addresses/{address}/transactions?limit&offset&after` |
| `useStats` `GetTopMiners` | `GET /v1/stats/miners?limit&fromBlock&toBlock` |
| `useStats` `GetNetworkMetrics` | `GET /v1/stats/network?fromTime&toTime`(둘 다 필수, unix 초) |

multi-chain 모드에서는 `/chains/{id}/v1/...`이다.

### 인자 규칙

- `limit`은 1~100이다. 숫자(블록 번호, 시각)는 10진수 문자열, 주소는 `0x`로 시작하는 hex다. 규칙에 맞지 않으면 400과 `{"error": "..."}`가 온다.
- 다음 페이지: `blocks`와 `transactions`는 블록 범위로 읽으므로 `offset`만 쓴다. 주소별 거래는 앞 응답의 `pageInfo.endCursor`를 `after`로 넘기면, 목록 깊이와 상관없이 같은 비용으로 다음 페이지를 읽는다.
- 상태 코드: 요청한 값 자체를 못 얻으면 500, 값 일부만 실패하면 200에 `errors`가 함께 온다(이때는 캐시되지 않는다).

### 바꾸는 방법

Apollo `useQuery`의 `pollInterval` 대신, 같은 주기로 `fetch`하는 hook을 쓴다. 브라우저가 `ETag`와 `If-None-Match`를 알아서 처리하므로 frontend가 직접 다룰 필요는 없다.

```ts
// 예: GetBlocks 대신 REST. 반환 모양은 useQuery(GET_BLOCKS)의 data와 같다.
async function fetchBlocks(base: string, limit: number, offset: number) {
  const res = await fetch(`${base}/v1/blocks?limit=${limit}&offset=${offset}`)
  if (res.status === 429) throw new Error('rate limited') // Retry-After: 1
  const body = await res.json()
  if (!res.ok) throw new Error(body.error ?? body.errors?.[0]?.message)
  return body.data // { blocks: { nodes, totalCount, pageInfo } }
}
```

### 단점

- REST 경로는 frontend가 지금 묻는 필드만 준다. 필드를 더 보여 주려면 백엔드의 REST 문서(`pkg/api/rest/rest.go`)도 바꿔야 한다. GraphQL처럼 frontend에서 필드를 마음대로 고를 수는 없다.
- Apollo 캐시와 별도로 데이터를 들고 있게 되므로, 같은 데이터를 GraphQL로도 읽는 화면은 두 값이 잠깐 다를 수 있다.

---

## 3. 구독

WebSocket 서버(`/graphql/ws`)는 구독 문서를 스키마로 검증하지 않고 구독 이름과 변수만 읽는다. 그래서 `replayLast`처럼 스키마에 없는 변수도 동작한다. 다만 스키마 파일(`pkg/api/graphql/schema.graphql`)로 코드를 생성하면 이런 문서가 스키마와 맞지 않는다고 나온다.

끊겼다가 다시 연결할 때 이벤트를 빠짐없이 받으려면 `fromSequence`를 쓰는 것을 권장한다(indexer-go R3-4).

1. 처음 붙을 때 `{ streamSequence }`로 위치 S를 읽고, 화면 상태를 조회한 뒤, 구독 변수 `fromSequence: S+1`로 구독한다.
2. 각 "next" 메시지의 `extensions.sequence`를 기억한다.
3. 다시 연결할 때는 마지막으로 받은 sequence + 1로 구독한다.
4. 오류 코드별 처리:
   - `SLOW_SUBSCRIBER`: frontend가 이벤트를 늦게 읽어 서버가 연결을 끊은 것이다(close 1008). 오류의 `extensions.resumeFrom`부터 다시 구독한다.
   - `SEQUENCE_TOO_OLD`: 요청한 sequence가 서버에서 이미 지워졌다. 1번부터 다시 시작한다.
   - `RESUME_UNSUPPORTED`: 서버가 outbox를 끈 상태다. `replayLast`로 돌아간다.

`replayLast`는 서버가 메모리에 둔 최근 이벤트를 돌려줄 뿐이라, 끊긴 동안의 이벤트를 모두 받는다는 보장은 없다.

---

## 4. 보안 제한이 frontend에 주는 영향 (indexer-go R4-3)

| 제한 | frontend가 할 일 |
|---|---|
| rate limit(주소마다 초당 100, burst 200, 기본 켜짐). 넘으면 429와 `Retry-After: 1` | 429를 오류로 보이지 말고 잠시 뒤 다시 시도한다. Next.js 서버 라우트(`app/api/v1/...`)에서 색인기를 부르는 요청은 frontend 서버 주소 하나로 묶여 한도를 함께 쓴다. 이 경로의 요청이 많으면 운영 쪽에서 `api.rate_limit`을 올려야 한다 |
| GraphQL 깊이 15, 복잡도 5000. 넘으면 `extensions.code`가 `QUERY_TOO_DEEP` 또는 `QUERY_TOO_COMPLEX`인 오류 | 복잡도는 페이지 크기만큼 곱해 센다. 지금 frontend 쿼리는 100행 페이지로 물어도 최대 약 2,200이라 걸리지 않는다. 페이지 안에서 다시 페이지를 묻는 쿼리(블록 100개마다 거래 100개 등)를 새로 만들지 않는다 |
| CORS는 credentials를 허용하지 않는다 | frontend는 `credentials: 'same-origin'`이라 영향이 없다. 다른 출처로 쿠키를 보내는 요청을 새로 만들지 않는다 |
| 블록 범위 없는 `transactions`/`logs`가 색인으로 답하지 못하는 조건(트랜잭션 `type`만, 조건 없는 `logs`)이면 블록을 최대 10,000개만 읽는다. 페이지를 다 채우지 못하고 멈추면 connection의 `scannedThrough`에 마지막으로 읽은 블록이 온다. 범위를 10,000블록보다 길게 주면 트랜잭션은 최신 쪽, 로그는 오래된 쪽 10,001블록만 읽고 같은 필드로 알린다 | `scannedThrough`가 있으면 결과가 더 있을 수 있다는 뜻이다. 트랜잭션은 `blockNumberTo`를 그 아래로, 로그는 `blockNumberFrom`을 그 위로 주어 이어 묻는다. `from`/`to`(트랜잭션), `address`/`topics`(로그) 조건은 색인으로 답하므로 멈추지 않는다 |
| WebSocket은 Origin을 검사한다 | 운영에서 `api.allowed_origins`를 목록으로 두면 frontend 주소를 반드시 넣는다. 빠지면 구독 연결이 403으로 거절된다 |

---

## 5. 수정한 뒤 확인하기

1. indexer-go 저장소에서 frontend 문서를 다시 읽어 온다(frontend 체크아웃이 `../indexer-frontend`에 있다고 가정한다. 다른 곳이면 `INDEXER_FRONTEND_DIR`를 지정한다).

   ```bash
   go test ./pkg/app -run TestFrontendDocuments -update
   ```

2. 고친 문서는 시험이 "works now: remove it from known-invalid.txt"라고 알려 준다. `pkg/app/testdata/frontend/known-invalid.txt`에서 그 줄을 지운다.
3. 새로 깨진 문서가 있으면 "no longer works"와 서버의 검증 오류가 나온다.
4. 바뀐 `documents.json`과 `known-invalid.txt`를 indexer-go에 커밋한다. 이후 indexer-go 변경이 frontend 문서를 깨뜨리면 이 시험이 실패한다.

이 시험은 문서가 스키마에 맞는지까지만 확인한다. 화면이 값을 올바르게 그리는지는 frontend의 시험(vitest, playwright)으로 확인해야 한다.
