# 기능과 API 확장

프로젝트가 indexer를 고치지 않고 자기 기능(처리기), 저장 키, GraphQL 조회, HTTP 경로를 붙이는 방법이다(refactoring plan R6-2). 예제는 `examples/receipts`(정산 컨트랙트의 영수증 조회, P07 재현과 인수 시험)다.

확장 지점은 두 갈래다.

| 바꾸려는 것 | 쓰는 패키지 | 문서 |
|---|---|---|
| 노드 감지, 블록·receipt 디코딩, 헤더 해시, 거래 타입, native 회계 | 어댑터: `pkg/chains`, `pkg/chains/evm` | [체인 커스터마이징 계약](CHAIN_CUSTOMIZATION.md) |
| 블록마다 실행하는 처리, 자기 저장 키, 선언한 표 읽기 | 기능: `pkg/feature`, `pkg/storage`(keyspace 등록), `pkg/features/records` | 이 문서 |
| GraphQL 조회·구독, HTTP 경로, 색인 진행 | `pkg/api`, `pkg/api/graphql`, `pkg/fetch` | 이 문서 |

이전의 `pkg/sdk`는 이 패키지들을 별칭으로 다시 내보내던 창구였다. 지금은 없어졌고, 위 패키지를 직접 쓴다. 호환성 약속은 없다. 쓰는 indexer 버전을 고정하고, 올릴 때 자기 시험을 돌린다.

## 1. 바이너리 구성

등록은 Go 패키지가 로드될 때 한다(`init`). YAML은 이미 링크된 기능을 켜고 설정할 뿐, 코드를 내려받거나 동적으로 로드하지 않는다. 그래서 프로젝트는 자기 바이너리를 만든다. main 패키지가 기능 패키지와 필요한 체인 bundle을 import하고 indexer 명령행(`cmd/cli`)의 `cli.Main`을 부른다. 설정, 명령행 플래그, 수명 관리는 indexer와 같다.

```go
package main

import (
	"os"

	"github.com/0xmhha/indexer-go/cmd/cli"
	_ "example.com/receipts" // 기능과 경로를 등록한다
)

func main() { os.Exit(cli.Main(cli.WithName("receipts-indexer"))) }
```

- 특정 체인용이면 그 체인의 bundle(`x/stablenet/bundle`, `x/arc/bundle`)도 import하고 `cli.WithChain`으로 다른 체인의 노드를 거부하게 한다. 저장소 안의 `x/<chain>/cmd/indexer-<chain>`이 이 모양이다.
- go.mod는 `github.com/0xmhha/indexer-go`를 require한다(저장소 안 예제는 `replace => ../..`).
- 알림 조건식이 쓰는 `cel-go`(v0.31.0)는 분리된 `google.golang.org/genproto/googleapis/api`를 쓴다. 프로젝트의 모듈 그래프에 분리 전의 `google.golang.org/genproto`(2023년 판)가 남아 있으면 `ambiguous import`가 난다. 이때는 `go get google.golang.org/genproto@v0.0.0-20240826202546-f6391c0de4c7` 뒤에 `go mod tidy`를 실행한다(`examples/receipts`도 이렇게 맞췄다).

## 2. 기능

기능은 `init`에서 등록하고, 색인되는 블록마다 그 블록의 저장 트랜잭션 안에서 호출된다. 그래서 기능이 쓴 것은 블록과 함께 commit되고, reorg로 블록이 되돌려지면 함께 되돌려진다.

```go
type totals struct{}

func (totals) Name() string           { return "receipts.totals" }
func (totals) Requires() []string     { return []string{"records"} }
func (totals) OrderIndependent() bool { return true } // 블록 순서와 무관하면 online backfill
func (totals) LogsOnly() bool         { return true } // 선언한 로그만 읽으면 declared 모드에서 실행

func (totals) Register(r feature.Registrar) error {
	kv, ok := r.Deps().Storage.(port.KV)
	if !ok {
		return errors.New("the storage has no key-value store")
	}
	r.OnBlock(feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		// b.Model(블록), b.Receipts(영수증과 로그). kv의 쓰기는 블록과 함께 commit된다.
		return nil
	}))
	return nil
}

func init() {
	feature.Register(totals{})
	storage.RegisterKeyspace("receipts.totals", storage.ChainData, "/x/receipts/") // 재색인 때 지울 키
}
```

- 켜기: `features.<name>.enabled: true`. 자기 설정 절은 `r.Deps().DecodeSettings(name, &settings)`로 읽는다(모르는 키는 시작 오류).
- `feature.Deps`: `Storage`(필요한 포트를 type assertion으로 얻는다. 자기 데이터는 `port.KV`), `Logger`, `Publish`(커밋 뒤 이벤트), `Contracts`·`BlockAt`(노드 조회).
- 블록 처리기가 오류를 내면 그 블록 전체가 취소된다. 같은 블록은 commit된 뒤 다시 처리되지 않는다.
- reorg: 쓴 데이터는 undo로 자동으로 되돌려진다. 내보낸 이벤트를 철회하려면 `r.OnRollback`으로 `feature.RollbackHandler`를 등록한다.
- 설정이 부분(part)을 정의하는 기능은 `feature.PartRegistrar`(type assertion)로 부분마다 처리기를 등록한다. 더한 부분만 따로 backfill된다.
- 저장 키: `storage.RegisterKeyspace`로 등록한 prefix 아래는 그 기능의 것이다. indexer는 거기에 쓰지 않고, 재색인 때 지운다. indexer 자신의 키 형식은 바뀔 수 있으므로 port(`port.QueryStore`, `port.RecordReader`)로 읽는다.

## 3. 선언한 표 읽기

`features.records`로 선언한 표(docs/CONFIG.md "선언형 수집")의 레코드를 기능과 API에서 쓴다.

- `records.Settings(deps.DecodeSettings)`: 등록할 때 표 정의(`*declared.Plan`)를 얻는다. `Match(log)`로 로그가 어느 표인지, `Decode(log)`로 필드를 얻는다.
- `records.LookupRecords(ctx, store, table, map[string]string{...}, page)`: 선언된 키로 레코드를 찾는다. 가장 이른 것부터 돌려준다. 값은 저장할 때와 같이 정규화한다.

## 4. API

- `graphql.RegisterExtension(name, func(*graphql.Extension))`: GraphQL 조회·구독을 더한다.
- `api.RegisterRoute(api.Route{Method, Pattern, Handler})`: GraphQL 옆에 HTTP 경로를 더한다. `Handler`는 `func(store port.QueryStore, logger *zap.Logger) http.Handler`다. 상태 코드와 고정 JSON 형식이 필요할 때 쓴다. 경로 인자는 `api.URLParam(r, "name")`. 단일 체인 서버(declared 모드 포함)가 mount하고, 멀티체인 서버는 체인마다 `/chains/{id}/<pattern>`에 그 체인의 저장소로 mount한다. 경로 패턴에 `{id}` 인자가 있으면 체인 id와 겹치므로 멀티체인에서는 mount하지 않는다(경고 로그). `node.role: ingest`(health와 metrics만 제공)는 mount하지 않는다.
- PostgreSQL 실행 수집의 query는 공개 generation의 read snapshot 안에서 처리한다. GraphQL mutation과 `jsonrpc.RegisterMutationMethod(name, fn)`로 등록한 쓰기는 이 읽기 전용 transaction에서 제외한다. 기존 `jsonrpc.RegisterMethod`는 query 등록이며, DB에 쓰는 확장은 mutation으로 선언해야 한다. 일반 RPC 수집 모드의 읽기/쓰기 동작은 유지한다.
- 등록형 GET/HEAD route도 요청 단위 snapshot을 사용한다. 장시간 event/file stream은 `api.Route{Streaming: true, ...}`로 선언하여 연결 전체가 read transaction을 보유하지 않게 한다. stream에서 DB 조회가 필요하면 짧은 작업별로 `port.WithQuerySnapshot`을 사용한다. rewrite/restore 중에는 재시도 오류를 그대로 전달하고 임의0·없음으로 바꾸지 않는다.
- `fetch.IndexingOf(ctx, store)`: 색인한 블록, live loop가 마지막으로 본 노드의 목표 블록(finality 정책 기준), 그 차이(`Lag`). "아직 색인 전"과 "색인이 늦음"을 구분할 때 쓴다(예제의 503 `RPC_STALE`). API 전용 프로세스(`node.role api`)에서는 `Polled`가 false다.

## 5. 시험

`pkg/testchain`이 결정적인 JSON-RPC 시험 체인을 준다(`testchain.NewServer`, 시나리오 `BuildReceipts` 등, `DisableMethod`/`EnableMethod`로 RPC 장애 주입). 예제의 시험은 자기 바이너리를 빌드해 시험 체인을 상대로 설정 파일로 실행하고 HTTP로 확인한다(`examples/receipts/receipts_test.go`). `make test-examples`가 예제를 모두 시험한다.

## 6. 결정성 규약

같은 체인이면 어떻게 색인하든 처리기가 저장하는 값이 같아야 한다. 처음부터 색인할 때, 재색인할 때, 멈췄다 재시작할 때, 기능을 나중에 켜서 backfill할 때(순서 무관 기능은 live와 섞여 online으로), reorg 뒤 다시 색인할 때가 모두 같아야 한다. 그렇지 않으면 재색인한 DB와 운영 중인 DB가 서로 다른 답을 낸다.

처리기(`OnBlock`, `OnPart`, `OnRollback`에 등록한 것)는 다음을 지킨다.

1. 블록과 저장소만 읽는다. 값은 `b.Model`, `b.Receipts`, 그리고 `Deps.Storage`(같은 블록 트랜잭션)에서 얻는다. 시각이 필요하면 블록 시각(`b.Model.Time`)을 쓴다.
2. 시계, 난수, 환경 변수, 파일, 네트워크를 쓰지 않는다(`time.Now`, `math/rand`, `crypto/rand`, `os.Getenv`, `net/http` 등).
3. 노드를 읽을 때는 처리 중인 블록 번호를 넘긴다(`Contracts.CallContract(ctx, msg, number)`, `BalanceAt(ctx, addr, number)`). `nil`(latest)은 live 색인과 backfill에서 다른 값을 준다. 노드 오류는 오류로 돌려준다. 그러면 블록이 취소되고 다시 시도된다. 오류를 삼키고 기본값을 저장하면 노드 상태에 따라 결과가 달라진다.
4. 블록 사이의 상태를 메모리에 두지 않는다. 누적값은 KV에 쓴다(블록과 함께 commit되고 reorg 때 되돌려진다). 메모리 캐시는 KV에 있는 것의 사본일 때만 쓴다.
5. "지금 색인된 높이" 같은 live에서만 맞는 입력을 읽지 않는다. backfill에서는 이 값이 head다.
6. `OrderIndependent`를 선언한 기능은 블록을 어떤 순서로 처리해도 같은 결과를 내야 한다. 같은 블록을 두 번 처리하지는 않는다(실패한 블록은 commit되지 않고, backfill은 commit된 진행 다음부터 잇는다).

처리기 밖의 코드(HTTP 경로의 캐시, 배경 작업)는 시계를 써도 된다. 그런 줄에는 `//feature:nondeterministic <이유>`를 같은 줄이나 바로 윗줄에 단다.

### 확인

`pkg/feature/featuretest`가 두 가지를 확인한다.

- `featuretest.RequireDeterministic(t, featuretest.Check{...})`: 시험 체인을 네 가지로 색인하고 `Prefixes` 아래 키가 모두 같은지 본다. 네 가지는 처음부터, 새 DB에 다시, 절반에서 멈췄다 재시작, `Features`를 head까지 색인한 뒤 켜서 backfill이다. 시계·난수(두 번째), 메모리 상태(세 번째), 순서나 live 입력 의존(네 번째)이 걸린다. declared 모드에서 `records`는 끌 수 없으므로 `Requires`에 둔다(`Features`가 없으면 네 번째는 건너뛴다). reorg는 확인하지 않는다. 블록 트랜잭션으로 쓴 것은 무엇이든 undo가 되돌린다.
- `featuretest.RequireNoForbiddenUses(t, dirs...)`: 패키지 소스(시험 파일 제외)에서 2, 3번 위반을 찾는다. 문법만 읽으므로 다른 패키지 안의 호출은 따라가지 않는다.

예제는 `examples/receipts/determinism_test.go`다. indexer의 내장 기능 패키지도 `TestBuiltInFeaturesFollowTheRules`로 검사한다.

내장 `token.metadata`는 토큰을 생성 블록 기준으로, `balance.native`는 처음 보는 계정의 잔액을 처리 블록 직전 기준으로 읽는다. 두 기능은 `feature.StateReader`다. v0.2.15부터 시작할 때 처리할 첫 블록 직전의 상태를 노드에 묻고, 노드가 그 상태가 없다고 답하면 시작을 거부한다(`pkg/app/node_state.go`). 첫 블록은 새 DB의 `indexer.start_height`, 재시작할 때의 다음 색인 블록, 또는 새로 켠 기능의 backfill 시작 블록이다. 블록 0부터 시작하면 상태 검사도 블록 0을 쓴다.

full node도 필요한 높이의 상태를 보관하면 시작할 수 있다. archive 노드가 없고 과거 상태가 지워졌다면, 상태가 남아 있는 높이 바로 다음 블록으로 `indexer.start_height`를 정하거나 해당 기능을 꺼야 한다. 최근 높이부터 시작한 DB에는 그 이전 블록의 색인 결과가 없다. 기능을 끈 DB에서 나중에 켜면 과거 높이부터 backfill할 수 있으므로 상태 검사를 다시 받는다. 노드 종류만으로 시작 가능 여부를 판단하지 말고, 실제 운영 설정과 시작 높이로 확인한다(`TestNodeStateCheckedAtStartup`).

`features.<name>.allow_pruned_state: false`(기본값)는 처리 중에도 적용된다. 필요한 잔액 조회나 토큰의 과거 상태 조회가 실패하면 블록 트랜잭션을 취소하고 live loop가 같은 높이를 재시도한다. 초기 잔액을 0으로 대체하거나 메타데이터를 latest로 읽지 않는다. 계약 자체의 호출 revert는 토큰 판별의 정상적인 응답으로 남긴다. `token.NewContractIndexer`도 기본적으로 과거 상태를 요구하며, 근사 처리가 필요하면 색인 시작 전에 `SetAllowPrunedState(true)`를 호출해야 한다.

`features.<name>.allow_pruned_state: true`는 시작 시 상태 검사를 건너뛰고 처리 중 기존 fallback을 명시적으로 허용한다: `token.metadata`는 latest 상태로 읽고 경고를 남기며 결정성이 깨질 수 있고, `balance.native`는 처음 보는 계정을 0에서 시작하거나 보정을 건너뛰므로 잔액이 틀릴 수 있다. 이 설정은 archive 상태를 복원하지 않는다. 시작 검사 통과는 처리 중 상태 보존을 보장하지 않으며 영구히 지워진 상태는 재시도로 돌아오지 않는다. 노드가 응답하지 않은 오류나 저장 오류는 설정과 관계없이 블록을 취소한다.

## 7. 상태 준비

상태 준비 API는 기존 handler/registrar 인터페이스를 바꾸지 않는 선택 인터페이스다. `feature.BlockPreparer.PrepareBlock`은 여러 블록에서 동시에 호출될 수 있으며 저장·publish·Block 변경 없이 `feature.PreparedHandler`를 반환한다. `Handler`만 기존 순서의 block transaction에서 실행된다. 입력은 block hash로 조회하며(`Deps.BalanceAtHash`, 선택적 `feature.HashContractReader`), `ReadState: true`면 writer가 canonical hash를 검증한다. `Bytes`로 유지하는 payload 크기를 계산해 보고한다. 채널 전달 후 입력을 수정하거나 다른 블록/fork에 재사용하지 않는다. `Deps.PrepareReads`는 `PrepareConcurrency`와 pipeline 공유 permit으로 작업 수를 제한한다. 기존 구현은 이 API 없이 그대로 작동하며 backfill은 기존 ordered handler를 호출한다.

`Deps.StateBatchSize`와 선택적 `Deps.BalanceBatchAtHash(ctx, addresses, hash)` 및 `feature.HashContractBatchReader.CallContractBatchAtHash(ctx, address, calldata, hash)`는 batch 지원을 추가한다. envelope 성공 시 반환값은 입력 순서의 값 배열과 같은 길이의 멤버 오류 배열이다. envelope 오류가 있으면 값 배열은 nil일 수 있다. 호출자는 envelope 성공만으로 모든 값을 사용하지 말고 각 멤버의 오류를 확인한다. hash를 latest로 바꾸거나 미응답 잔액을 0으로 만들지 않는다. 기존 계약 reader 인터페이스에는 메서드를 추가하지 않았다. 공유 permit을 이미 가진 작업에서 `PrepareReads`를 다시 호출하면 교착될 수 있으므로 개별 fallback은 현재 permit 안에서 실행한다.

멀티체인의 기존 `multichain.Indexer` 인터페이스는 유지한다. 선택적 `multichain.NodeHeightObserver`는 `ObserveNodeHeight(ctx, maxAge)`로 raw head와 원래 RPC latency를 함께 제공한다. `maxAge=0`은 독립적인 fresh RPC이며, 양수는 지정한 freshness 범위에서 관측을 공유할 수 있다. production 구현은 최대 1초로 제한한다. 이 인터페이스가 없는 외부 Indexer는 기존 NodeHeight health 조회를 사용한다. finalized/confirmations target을 raw head로 반환하거나 실패를 이전 성공으로 숨기지 않는다.

## 8. 이전 SDK에서 옮기기

| 이전 | 지금 |
|---|---|
| `sdk.Main()` | `os.Exit(cli.Main(cli.WithName(...)))` (`cmd/cli`) |
| `sdk.RegisterFeature`, `sdk.Feature`, `sdk.Registrar`, `sdk.Deps`, `sdk.Block`, `sdk.BlockHandlerFunc` | `feature.Register`, `feature.Feature`, `feature.Registrar`, `feature.Deps`, `feature.Block`, `feature.BlockHandlerFunc` |
| `sdk.KVOf(s)` | `s.(port.KV)` |
| `sdk.RegisterKeyspace(owner, prefixes...)` | `storage.RegisterKeyspace(owner, storage.ChainData, prefixes...)` |
| `sdk.DeclaredTables(deps)`, `sdk.LookupRecords` | `records.Settings(deps.DecodeSettings)`, `records.LookupRecords` |
| `sdk.ProgressOf`, `sdk.Progress` | `fetch.IndexingOf`, `fetch.Indexing` |
| `sdk.RegisterGraphQL`, `sdk.RegisterRoute`, `sdk.URLParam` | `graphql.RegisterExtension`, `api.RegisterRoute(api.Route{...})`, `api.URLParam` |
| `sdk.Store`, `sdk.KV`, `sdk.Page`, `sdk.ErrNotFound` | `port.QueryStore`, `port.KV`, `port.Page`, `port.ErrNotFound` |
| `pkg/sdk/sdktest` | `pkg/feature/featuretest` |
| `//sdk:nondeterministic` | `//feature:nondeterministic` |

엔진만 쓰는 패키지(멀티체인 manager, 알림, RPC proxy, 용량 감시, API 내부, 내장 기능 구현 등)는 `internal/` 아래에 있어 다른 모듈이 import할 수 없다. 확장 지점(`pkg/feature`, `pkg/api`, `pkg/api/graphql`, `pkg/api/jsonrpc`, `pkg/chains`, `pkg/core`, `pkg/storage`, `pkg/fetch`, `pkg/features/records`, `pkg/events`, 체인 모듈용 `pkg/capture`·`pkg/window`·`pkg/chainfeed`·`pkg/source`, 시험용 `pkg/testchain`·`pkg/app/apptest`·`pkg/feature/featuretest`)은 `pkg/`에 남는다. `pkg/fetch`와 `pkg/storage`에는 writer와 저장소 구현도 함께 있으므로, 확장 코드는 이 문서가 든 함수만 쓴다.
