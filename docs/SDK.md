# 사용자 처리기 SDK

프로젝트가 indexer를 고치지 않고 자기 처리기를 붙이는 방법이다(refactoring plan R6-2). 예제는 `examples/receipts`(정산 컨트랙트의 영수증 조회, P07)다.

## 1. 구조

처리기는 기능(feature)이다. `init`에서 등록하고, 색인되는 블록마다 그 블록의 저장 트랜잭션 안에서 호출된다. 그래서 처리기가 쓴 것은 블록과 함께 commit되고, reorg로 블록이 되돌려지면 함께 되돌려진다. 처리기는 GraphQL 조회와 HTTP 경로를 더할 수 있다.

프로젝트는 자기 바이너리를 만든다. main 패키지가 처리기 패키지를 import하고 `sdk.Main()`을 부른다. 설정, 명령행 플래그, 수명 관리는 indexer와 같다.

```go
package main

import (
	_ "example.com/receipts" // 처리기를 등록한다
	"github.com/0xmhha/indexer-go/pkg/sdk"
)

func main() { sdk.Main() }
```

go.mod는 `github.com/0xmhha/indexer-go`를 require한다(저장소 안 예제는 `replace => ../..`).

## 2. 처리기

```go
type totals struct{}

func (totals) Name() string           { return "receipts.totals" }
func (totals) Requires() []string     { return []string{"records"} }
func (totals) OrderIndependent() bool { return true } // 블록 순서와 무관하면 online backfill
func (totals) LogsOnly() bool         { return true } // 선언한 로그만 읽으면 declared 모드에서 실행

func (totals) Register(r sdk.Registrar) error {
	kv, err := sdk.KVOf(r.Deps().Storage)
	if err != nil {
		return err
	}
	r.OnBlock(sdk.BlockHandlerFunc(func(ctx context.Context, b *sdk.Block) error {
		// b.Model(블록), b.Receipts(영수증과 로그). kv의 쓰기는 블록과 함께 commit된다.
		return nil
	}))
	return nil
}

func init() {
	sdk.RegisterFeature(totals{})
	sdk.RegisterKeyspace("receipts.totals", "/x/receipts/") // 재색인 때 지울 키
}
```

- 켜기: `features.<name>.enabled: true`. 자기 설정 절은 `r.Deps().DecodeSettings(name, &settings)`로 읽는다(모르는 키는 시작 오류).
- `Deps`: `Storage`(필요한 포트를 type assertion으로, 자기 데이터는 `sdk.KVOf`), `Logger`, `Publish`(커밋 뒤 이벤트), `Contracts`·`BlockAt`(노드 조회).
- 블록 처리기가 오류를 내면 그 블록 전체가 취소된다. 같은 블록은 commit된 뒤 다시 처리되지 않는다.
- reorg: 쓴 데이터는 undo로 자동으로 되돌려진다. 내보낸 이벤트를 철회하려면 `r.OnRollback`으로 `sdk.RollbackHandler`를 등록한다.

## 3. 선언한 표 읽기

`features.records`로 선언한 표(docs/CONFIG.md "선언형 수집")의 레코드를 처리기와 API에서 쓴다.

- `sdk.DeclaredTables(deps)`: 등록할 때 표 정의를 얻는다. `Match(log)`로 로그가 어느 표인지, `Decode(log)`로 필드를 얻는다.
- `sdk.LookupRecords(ctx, store, table, map[string]string{...}, page)`: 선언된 키로 레코드를 찾는다. 가장 이른 것부터 돌려준다. 값은 저장할 때와 같이 정규화한다.

## 4. API

- `sdk.RegisterGraphQL(name, func(*sdk.GraphQLExtension))`: GraphQL 조회·구독을 더한다.
- `sdk.RegisterRoute(method, pattern, func(store sdk.Store, logger) http.Handler)`: GraphQL 옆에 HTTP 경로를 더한다. 상태 코드와 고정 JSON 형식이 필요할 때 쓴다. 경로 인자는 `sdk.URLParam(r, "name")`. 단일 체인 서버(declared 모드 포함)가 mount하고, 멀티체인 서버는 하지 않는다.

## 5. 시험

`pkg/testchain`이 결정적인 JSON-RPC 시험 체인을 준다(`testchain.NewServer`, 시나리오 `BuildReceipts` 등). 예제의 시험은 자기 바이너리를 빌드해 시험 체인을 상대로 설정 파일로 실행하고 HTTP로 확인한다(`examples/receipts/receipts_test.go`). `make test-examples`가 예제를 모두 시험한다.
