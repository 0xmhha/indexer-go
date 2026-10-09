# 사용자 처리기 SDK

프로젝트가 indexer를 고치지 않고 자기 처리기를 붙이는 방법이다(refactoring plan R6-2). 예제는 `examples/receipts`(정산 컨트랙트의 영수증 조회, P07 재현과 인수 시험)다.

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

- `sdk.ProgressOf(ctx, store)`: 색인한 블록, live loop가 마지막으로 본 노드의 목표 블록(finality 정책 기준), 그 차이(`Lag`). "아직 색인 전"과 "색인이 늦음"을 구분할 때 쓴다(예제의 503 `RPC_STALE`). API 전용 프로세스(`node.role api`)에서는 `Polled`가 false다.

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

처리기 밖의 코드(HTTP 경로의 캐시, 배경 작업)는 시계를 써도 된다. 그런 줄에는 `//sdk:nondeterministic <이유>`를 같은 줄이나 바로 윗줄에 단다.

### 확인

`pkg/sdk/sdktest`가 두 가지를 확인한다.

- `sdktest.RequireDeterministic(t, sdktest.Check{...})`: 시험 체인을 네 가지로 색인하고 `Prefixes` 아래 키가 모두 같은지 본다. 네 가지는 처음부터, 새 DB에 다시, 절반에서 멈췄다 재시작, `Features`를 head까지 색인한 뒤 켜서 backfill이다. 시계·난수(두 번째), 메모리 상태(세 번째), 순서나 live 입력 의존(네 번째)이 걸린다. declared 모드에서 `records`는 끌 수 없으므로 `Requires`에 둔다(`Features`가 없으면 네 번째는 건너뛴다). reorg는 확인하지 않는다. 블록 트랜잭션으로 쓴 것은 무엇이든 undo가 되돌린다.
- `sdktest.RequireNoForbiddenUses(t, dirs...)`: 패키지 소스(시험 파일 제외)에서 2, 3번 위반을 찾는다. 문법만 읽으므로 다른 패키지 안의 호출은 따라가지 않는다.

예제는 `examples/receipts/determinism_test.go`다. indexer의 내장 기능 패키지도 `TestBuiltInFeaturesFollowTheRules`로 검사한다.

내장 `token.metadata`는 토큰을 생성 블록 기준으로 읽는다. 노드가 그 블록의 상태를 보관하지 않으면(archive가 아닌 노드에서 오래된 블록을 backfill할 때) latest 상태로 읽고 경고를 남긴다. 이 경우만 결정성이 깨진다. 노드가 응답하지 않은 오류는 블록을 다시 시도하게 한다. 알려진 예외: `balance.native`는 처음 보는 계정의 잔액을 노드에서 못 읽으면 0에서 시작한다(3번의 오류 처리 위반).

## 7. 호환성 약속

프로젝트가 기대는 면은 `pkg/sdk`와 `pkg/sdk/sdktest`의 공개 식별자다. 별칭(alias)이 가리키는 indexer 타입의 공개 메서드와 필드도 여기에 들어간다. `pkg/testchain`은 시험용이라 약속에 넣지 않는다(바꿀 때 예제를 함께 고친다). 다른 `pkg/...`를 직접 import하면 약속 밖이다.

- 버전: 모듈 tag `vX.Y.Z`. v1 전에는 SDK를 깨는 변경을 minor 버전(`v0.Y.0`)에서만 하고, 이 문서의 "SDK 변경 기록"에 무엇이 바뀌었고 어떻게 고치는지 적는다. patch 버전은 깨지 않는다.
- 프로젝트가 구현하는 interface(`Feature`, `BlockHandler`, `RollbackHandler`, `OrderIndependent`, `LogsOnly`)에는 메서드를 더하지 않는다. 새 기능은 선택 interface로 더한다(`feature.PartRegistrar`처럼 type assertion으로 확인).
- 프로젝트가 쓰기만 하는 interface(`Registrar`, `Store`, `KV`, `RecordStore`)와 struct(`Deps`, `Block`, 모델 타입)에는 메서드와 필드를 더할 수 있다. 이것은 깨는 변경으로 보지 않는다. 시험용으로 이 interface를 직접 구현했다면 컴파일이 깨질 수 있으니, 구현체에 indexer 쪽 값을 embed한다.
- 저장 데이터: `RegisterKeyspace`로 등록한 prefix 아래는 프로젝트 것이다. indexer는 거기에 쓰지 않고, 재색인 때 지운다. indexer 자신의 키 형식은 약속에 들어가지 않으므로 port(`Store`, `RecordStore`)로 읽는다.
- 강제: `TestSDKSurface`가 공개 면 전체(`pkg/sdk/testdata/surface.txt`)를 고정한다. 바뀌면 시험이 실패하고, 검토한 뒤 `-update`로 다시 쓴다. `make test-examples`는 예제가 지금 SDK로 빌드되고 동작하는지 본다.

### SDK 변경 기록

- v0.1.0 이후(미발행): `pkg/sdk/sdktest` 추가. `feature.PartRegistrar` 추가(선택 interface, 기존 코드 영향 없음).
