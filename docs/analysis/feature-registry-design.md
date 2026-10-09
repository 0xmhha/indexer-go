# 기능 레지스트리 상세 설계 (10/5)

> 10/9 검토: 인용한 `TestSystemContractsFeatureOff`는 `TestFeatureOffRemovesOnlyItsKeys`의 systemcontracts 하위 시험으로 바뀌었다. `pkg/resilience`, `LargeBlockProcessor`는 v0.1.0 뒤에 지워졌다. 그 언급은 당시 기록이다([plan-audit.md](plan-audit.md)).

이 문서는 refactoring-plan.md의 R2-5(처리기·기능 레지스트리, 플래그, 의존성 검증)와 R2-6(기존 기능을 기능 모듈로 이전)의 첫 단계를 설계한다. 체인 프로필 설계(chain-profile-design.md)에서 남긴 WBFT·시스템 컨트랙트 처리의 범용 수집기 분리도 여기서 다룬다.

---

## 1. 결론

블록 하나를 처리하는 일을 "기능(feature)"이라는 단위로 등록하게 한다. 수집기는 core 처리(블록·거래·receipt·로그 저장, 커서)만 직접 하고, 나머지는 켜진 기능의 처리기를 정해진 순서로 부른다. 기능은 이름, 의존 기능, 등록 함수를 가진다. 어떤 기능을 켤지는 체인 프로필의 기본값과 설정(`features.<name>.enabled`)으로 정한다. 켜진 기능이 꺼진 기능에 의존하면 시작을 멈춘다.

기존 처리기는 한 번에 하나씩 기능으로 옮긴다. 옮길 때마다 keyspace golden이 같아야 한다. 처음 옮길 기능은 `stablenet.wbft`이다. 범용 수집기에 남은 StableNet 전용 코드 중 하나이고, 다른 기능과 얽힌 데가 적다.

---

## 2. 지금의 구조와 문제

수집기(`pkg/fetch`)는 블록마다 다음 일을 코드 순서대로 직접 한다.

| 순서 | 처리 | 켜는 조건(지금) |
|---|---|---|
| 1 | 블록·거래 저장 | 항상 |
| 2 | WBFT 메타데이터 | adapter의 합의 종류가 WBFT이거나 adapter가 없을 때. 저장 계층이 `WBFTWriter`이면 |
| 3 | 주소 색인, 컨트랙트 생성, ERC-20/721 transfer, SetCode, UserOp, 모듈 | 저장 계층 인터페이스. UserOp·모듈은 `account_abstraction.enabled` |
| 4 | native 잔액 추적 | 저장 계층 인터페이스 |
| 5 | genesis 잔액·토큰 메타 초기화(블록 0) | 항상 |
| 6 | fee delegation 메타 | 저장 계층 인터페이스 |
| 7 | receipt 저장, 로그 색인 | 항상 |
| 8 | 시스템 컨트랙트 이벤트 | 저장 계층이 `SystemContractWriter`이면. 체인과 상관없이 항상 켜진다 |
| 9 | 이벤트 발행, 외부 처리기(토큰 메타) | 항상 |

문제는 셋이다.
- **켜고 끄는 방법이 처리기마다 다르다.** 저장 계층 인터페이스가 있는지로 켜는 것은 사실상 끌 수 없다는 뜻이다. Pebble이 모든 인터페이스를 구현하기 때문이다. StableNet이 아닌 체인에서도 시스템 컨트랙트 처리가 돈다.
- **체인 전용 코드가 범용 수집기 안에 있다.** WBFT와 시스템 컨트랙트 처리는 StableNet 전용인데 `pkg/fetch`에 있다.
- **의존 관계가 코드 순서에만 들어 있다.** 예를 들어 토큰 메타 색인은 주소 색인 단계에서 컨트랙트 생성을 감지해야 돈다. 이 관계를 아무도 확인하지 않는다.

---

## 3. 목표 구조

### 3.1 기능 계약 (`pkg/feature`)

```go
// Feature is one optional part of indexing (refactoring plan 5.3).
type Feature interface {
	Name() string        // e.g. "stablenet.wbft"
	Requires() []string  // features that must also be enabled
	Register(r Registrar) error
}

// Registrar is what a feature can attach to. It grows as more of the
// pipeline becomes pluggable (API resolvers, metrics).
type Registrar interface {
	Deps() Deps                  // storage, logger, chain profile
	OnBlock(h BlockHandler)      // runs inside the block transaction
}

type BlockHandler interface {
	HandleBlock(ctx context.Context, b *Block) error
}
```

`feature.Block`은 수집기의 `fetchedBlock`을 공개한 것이다. 모델(블록, receipt)과 go-ethereum 보기를 함께 준다.

### 3.2 등록과 해석

- 기능은 `feature.Register(f)`로 전역 등록부에 넣는다. 기능 패키지의 `init`에서 부르고, `main.go`가 blank import로 연결한다. 체인 프로필과 같은 방식이다.
- 시작할 때 `feature.Resolve(enabled)`가 켤 기능 목록을 만든다. 순서는 의존 관계를 먼저 따르고, 의존 관계가 없는 기능끼리는 이름순이다. 그래서 실행 순서가 항상 같다.
- 켜진 기능의 `Requires`에 꺼졌거나 없는 기능이 있으면 오류로 멈춘다. 순환 의존도 오류다.

### 3.3 켤 기능 정하기

우선순위는 기존 설정 규칙(기본값 < YAML < 환경 변수 < CLI)을 따른다.

1. 체인 프로필의 `Features()`(StableNet은 `stablenet.system_contracts`, `stablenet.fee_delegation`, `stablenet.wbft`).
2. YAML `features: { <name>: { enabled: true|false } }`.
3. 환경 변수 `INDEXER_FEATURES=name1,-name2`(앞에 `-`를 붙이면 끈다).

아직 기능으로 옮기지 않은 처리기는 지금처럼 수집기 안에서 돈다. 옮길 때 그 처리기의 기본값을 지금과 같게 맞춘다. 그래서 옮기는 것만으로는 동작이 바뀌지 않는다.

### 3.4 실행 위치

기능 처리기는 블록 트랜잭션 안에서, core 저장 다음에 돈다. 저장 쓰기가 실패하면 블록 전체를 중단한다(원자 모드의 기존 규칙). 외부 조회(RPC, 토큰 메타) 실패는 기능 안에서 로그로 남기고 계속한다.

---

## 4. 단계와 검증

| 단계 | 내용 | 검증 |
|---|---|---|
| F1 | `pkg/feature`: 계약, 등록부, `Resolve`(순서, 의존 검증) | 단위 시험: 순서가 결정적이다, 꺼진 의존·없는 의존·순환을 거부한다 |
| F2 | 설정(`features`, `INDEXER_FEATURES`)과 시작 때 해석. 수집기가 켜진 기능의 처리기를 블록 트랜잭션 안에서 부른다 | 기능이 하나도 없으면 golden이 그대로다. 처리기 오류가 블록을 중단한다 |
| F3 | `stablenet.wbft`를 기능으로 옮긴다(`pkg/features/stablenet/wbft`). 수집기의 WBFT 코드를 지운다 | 가짜 체인·live golden이 같다. 기능을 끄면 WBFT 키가 생기지 않는다 |
| F4 | `stablenet.system_contracts`, `stablenet.fee_delegation` | 같은 방식. 시스템 컨트랙트 처리가 StableNet이 아닌 체인에서 돌지 않게 된다(동작 변경, golden에 반영) |
| F5 | 주소·잔액·토큰·AA 기능 | refactoring-plan 5.3의 순서를 따른다 |

---

## 5. 결정이 필요한 것

| 결정 | 선택지 | 권장 | 권장안의 단점 |
|---|---|---|---|
| 기능 패키지 위치 | `pkg/chains/<체인>` 아래 / `pkg/features/<묶음>/<이름>` | `pkg/features/...` | 체인 전용 기능도 프로필과 다른 디렉터리에 있게 된다. 대신 프로필 패키지가 저장 계층을 import하지 않아 의존 방향이 단순하다(저장 계층 시험이 프로필을 import한다) |
| 꺼진 의존 처리 | 의존 기능을 자동으로 켠다 / 오류로 멈춘다 | 오류로 멈춘다 | 설정을 고쳐야 시작할 수 있다. 대신 켜지 않은 기능이 조용히 켜지지 않는다 |
| 이미 색인된 DB에서 기능을 켤 때 | 경고만 / 그 기능만 처음부터 다시 처리(backfill) | 이번 단계는 경고만 | 켜기 전 블록의 데이터가 빠진다. backfill은 R2-7에서 한다 |
| 시스템 컨트랙트 기능의 기본값 | 모든 체인에서 켬(지금 동작) / StableNet 프로필에서만 켬 | StableNet에서만 | 다른 체인에서 같은 주소에 컨트랙트가 있으면 그 이벤트가 더 이상 색인되지 않는다 |

## 6. 단점과 한계

- 등록부가 전역이다. 시험끼리 같은 이름을 등록하면 충돌하므로, 시험용 기능은 이름에 시험 이름을 붙여야 한다.
- 기능이 받는 `feature.Block`이 go-ethereum 보기를 포함한다. 처리기를 모델로 옮기면 보기를 지울 수 있지만, 그때까지는 공개 계약에 임시 필드가 들어 있다.
- F2~F5 동안은 일부 처리기는 기능으로, 일부는 수집기 안에서 돈다. 그동안은 실행 순서를 두 곳에서 봐야 한다.

---

## 7. 진행 기록

**결정(10/5).** 따로 지시가 없어 5절의 권장안 네 가지로 진행한다. 기능 패키지는 `pkg/features/...`에 두고, 꺼진 의존이 있으면 시작을 멈춘다. 이미 색인된 DB에서 기능을 켜면 경고만 하고, 시스템 컨트랙트 기능은 StableNet에서만 켠다.

### F1~F3 (10/5)

| 산출물 | 내용 |
|---|---|
| `pkg/feature` | `Feature`, `Registrar`, `BlockHandler`, 전역 등록부(`Register`), `Resolve`(의존 기능을 먼저, 나머지는 이름순. 등록되지 않은 이름, 꺼진 의존, 순환은 오류), `Build`/`Pipeline`(처리기를 순서대로 실행하고 첫 오류에서 멈춤), `Enabled`(프로필 기본값 + 설정 덮어쓰기. 아직 옮기지 않아 등록되지 않은 기본값은 건너뛰고, 설정에 쓴 이름은 등록되어 있어야 한다) |
| 설정 | `features: { <name>: { enabled: true\|false } }`, `INDEXER_FEATURES=name1,-name2` |
| 수집기 | `SetFeatures`. 켜진 기능의 처리기를 core 저장(블록·거래·receipt·로그) 다음에, 블록 트랜잭션 안에서 부른다. 원자 경로와 비원자 경로 모두. 처리기가 실패하면 그 블록은 저장되지 않는다. 기능이 발행한 이벤트는 commit 뒤에 전달된다(`Fetcher.Publish`) |
| `main.go` | 프로필을 항상 감지한다(`profile_source: false`여도 기본 기능을 정하려고). 켤 기능을 계산하고 처리기를 만들어 수집기에 건다. 시작 로그에 켜진 기능을 남긴다 |
| `pkg/features/stablenet/wbft` | 수집기의 WBFT 처리(`fetcher_consensus.go`)를 옮겼다. 저장 계층이 `WBFTWriter`가 아니면 등록할 때 오류다. 수집기에서 WBFT 코드와 adapter 합의 종류 검사를 지웠다. 이제 StableNet 프로필의 기본 기능일 때만 돈다 |
| 결합 시험 | `pkg/feature`도 특정 체인 프로필을 import하지 못한다 |

**동작 변경.** 예전에는 adapter가 없거나 합의 종류가 WBFT이면 WBFT 처리가 돌았다. 지금은 `stablenet.wbft`가 켜져 있을 때만 돈다. 즉 StableNet 프로필로 감지되었거나 설정으로 켰을 때다. 일반 EVM 체인에서는 헤더 해석을 시도하다 경고를 남기던 일이 없어진다.

검증 결과는 다음과 같다.
- `pkg/feature` 단위 시험(race 포함): 순서가 입력 순서와 상관없이 같다. 꺼진 의존, 등록되지 않은 이름, 순환을 거부한다. 첫 오류에서 멈춘다. 이름이 겹치면 panic한다.
- `TestUnknownFeatureStopsStartup`: 등록되지 않은 기능을 설정하면 `NewApp`이 실패한다.
- `TestFeatureFailureAbortsBlock`: 블록 3에서 실패하는 시험용 기능을 켜면 수집이 그 오류로 멈추고, 커서는 2이며 블록 3은 저장되지 않는다.
- `TestWBFTFeatureOnNonWBFTChain`: 가짜 체인에서 `stablenet.wbft`를 켜도 저장 결과가 golden과 같다.
- keyspace·GraphQL golden, 기존 경로 동등성 시험이 그대로 통과한다. 전체 시험은 기존부터 불안정한 `pkg/resilience` 시험 하나를 빼고 통과한다.
- live StableNet(블록 0~19721): `TestLiveStableNet`에서 WBFT 키 20,283개가 기능 경로로 색인되고, 중단 후 재시작한 결과가 같다. `TestLiveStableNetIdentity`(블록 0~19091)도 통과한다.

### F4: 시스템 컨트랙트와 fee delegation (10/5)

| 산출물 | 내용 |
|---|---|
| `pkg/features/stablenet/systemcontracts` | `stablenet.system_contracts`. receipt마다 시스템 컨트랙트 이벤트를 색인한다(기존 파서를 그대로 쓴다). GovValidator의 `MemberAdded`·`MemberRemoved`를 검증자 집합 이벤트로 발행한다 |
| `pkg/features/stablenet/feedelegation` | `stablenet.fee_delegation`. 프로필이 붙인 fee payer 정보(`chains.FeeDelegationOf`)로 fee delegation 메타를 저장한다 |
| 수집기 | 시스템 컨트랙트 파서와 시스템 이벤트 감지(`detectSystemEvents*`)를 지웠다. 프로필 경로의 fee delegation 메타 생성을 지웠다. 기존 클라이언트 경로의 재조회(`processFeeDelegationMetadata`)는 S5까지 남는다 |
| 시험 | 가짜 체인은 일반 Geth 노드로 보고하지만 StableNet 시스템 컨트랙트 이벤트를 낸다. 그래서 golden 시험은 `stablenet.system_contracts`를 명시적으로 켠다. `TestSystemContractsFeatureOff`는 기능을 끄면 `/data/syscontracts/`·`/index/syscontracts/` 키만 빠지고 나머지는 golden과 같은지 확인한다 |

**동작 변경.**
- 시스템 컨트랙트 이벤트 색인이 StableNet 프로필로 감지되었거나 설정으로 켰을 때만 돈다. 예전에는 저장 계층이 지원하면 모든 체인에서 돌았다(5절 결정).
- 검증자 집합 이벤트의 블록 hash가 체인 값이다. 예전에는 go-ethereum 규칙으로 다시 계산해 StableNet에서 틀렸다(D16의 남은 누출).
- 검증자 집합 이벤트는 GovValidator 주소와 이벤트 서명으로 판단한다. 예전에는 adapter가 있으면 adapter의 시스템 컨트랙트 해석기를 썼다. StableNet에서는 같은 주소와 이벤트를 본다.
- 기존 비원자 경로의 범위 수집(`FetchRangeConcurrent`)은 시스템 컨트랙트 이벤트를 색인하지 않았다. 이제는 기능으로 돈다.

**검증.** 전체 시험이 통과했다. live StableNet(블록 0~21432)에서 fee delegation 메타 6건이 기능 경로로 저장되고(`TestLiveStableNet`, `TestLiveStableNetIdentity`의 메타·fee payer 색인 확인), 중단 후 재시작한 결과가 같다.

**남긴 것.** 범용 수집기에 남은 StableNet 전용 코드는 기존 클라이언트 경로의 fee delegation 재조회와 large block 처리기의 fee payer 조회다. 둘 다 S5에서 기존 경로와 함께 지운다. 다음 단계(F5)는 주소·잔액·토큰·AA 기능이다.

### F5: 주소·잔액·토큰·AA (10/5)

| 기능 | 패키지 | 하는 일 | 기본값 |
|---|---|---|---|
| `address.index` | `pkg/features/address` | 거래를 송신자·수신자·fee payer 주소로 색인하고 컨트랙트 생성을 기록한다 | 모든 체인에서 켬 |
| `balance.native` | `pkg/features/balance` | native 잔액 이력. 처음 보는 계정은 노드 잔액으로 시작하고, 블록 0에서는 miner의 genesis 잔액을 기록한다 | 모든 체인에서 켬 |
| `token.transfers` | `pkg/features/token` | ERC-20·ERC-721 Transfer 색인 | 모든 체인에서 켬 |
| `aa.eip7702`, `aa.erc4337`, `aa.erc7579` | `pkg/features/aa` | SetCode 인가, UserOperation, 모듈. 처리기 본체는 아직 `pkg/fetch`에 있고 기능은 그것을 연결한다 | 모든 체인에서 켬. 예전 설정 `account_abstraction.enabled: false`는 `features`에 따로 쓰지 않았으면 `aa.erc4337`·`aa.erc7579`를 끈다 |

등록부에는 `DefaultOn`(모든 체인에서 기본으로 켜는 기능), `Deps.BalanceAt`(노드 잔액 조회), `Block.Transactions`(거래와 receipt 짝짓기), `DelegatedFeePayer`(fee payer 조회 공용 함수)를 더했다. 수집기에는 core 저장과 기존 경로의 fee delegation 재조회, 실행되지 않는 genesis 토큰 메타 초기화만 남았다.

**바뀐 동작.**
- 저장 쓰기 실패가 기존 비원자 경로에서도 블록을 중단한다. 예전에는 비원자 경로에서는 로그만 남겼다.
- 기존 비원자 경로의 대형 블록 병렬 처리(`LargeBlockProcessor`)를 쓰지 않는다. 이 처리기는 주소·transfer 색인을 따로 구현한 사본이라, 기능과 함께 돌면 같은 거래를 두 번 색인한다. 원자 경로는 원래 이 처리기를 쓰지 않았다. 타입과 시험은 S5까지 남긴다.

**옮기지 않은 것.** 운영 코드에서 `SetTokenIndexer`를 부르는 곳이 없어서, 컨트랙트 생성 때 토큰 메타를 색인하는 코드와 genesis 토큰 메타 초기화는 실제로 돈 적이 없다. 토큰 메타는 따로 등록된 토큰 블록 처리기(`AddBlockProcessor`)가 맡는다. 그래서 이 경로는 기능으로 옮기지 않고 기록만 남긴다. 토큰 메타 기능(`token.metadata`)은 블록 처리기 연결 방식과 함께 정리한다.

**새로 찾은 결함(D20).** 기능을 하나씩 꺼 보니, `address.index`를 끄면 잔액 이력 키가 바뀌고 `balance.native`를 끄면 주소 색인 키가 바뀌었다. 두 색인이 주소별 순번 카운터 하나를 함께 썼기 때문이다. 이대로면 기능을 나중에 켜서 그 기능만 처리(backfill)했을 때 처음부터 켠 DB와 결과가 달라진다. 카운터를 키 묶음별로 나눴다. 재시작할 때는 각 묶음의 키에서만 순번을 복원한다. keyspace golden에서는 주소 색인 19개, 잔액 이력 19개의 키 번호가 바뀌었고, 값의 모음과 키 수는 그대로다.

**검증.**
- `TestFeatureOffRemovesOnlyItsKeys`: 7개 기능(`address.index`, `balance.native`, `token.transfers`, `aa.*` 3개, `stablenet.system_contracts`)을 하나씩 끄면 그 기능의 키 접두어만 빠지고 나머지 키와 값은 기준 시나리오와 같다. 각 기능의 키가 시나리오에 실제로 있는지도 확인한다.
- `TestAddrSeqFamiliesAreIndependent`: 두 카운터가 서로를 밀지 않고, 각자의 최고 순번에서 다시 시작한다.
- keyspace golden(순번 변경 반영), GraphQL golden, 기존 경로와 클라이언트 경로 동등성, 재시작·재처리·gap·crash 시험이 통과했다.
- 전체 시험이 통과했다. live StableNet(블록 0~3000, 아래 상한)에서 `TestLiveStableNet`과 `TestLiveStableNetIdentity`가 통과했다.

**성능.** 처음 측정에서는 `BenchmarkIngest` 일반 블록의 원자 경로가 3.0~3.1초로, 이전(2.4~2.7초)보다 느렸다. 여러 기능이 블록마다 거래와 receipt 짝을 따로 만들고 있어서, 블록당 한 번만 계산하도록 `Block.Transactions`에 캐시를 넣었다. 그 뒤에는 2.55~2.73초로 이전과 같은 범위다. 로컬 체인 노드가 같은 기계에서 돌고 있어 측정 잡음이 크다.

**live 시험 범위 상한.** 로컬 체인이 계속 블록을 만들어 live 시험이 매번 느려졌다. 블록 25,122개까지 자란 체인에서 `TestLiveStableNet`이 시험 안의 5분 제한을 넘겼다. 그래서 두 live 시험은 기본으로 블록 3000까지만 색인한다. `INDEXER_LIVE_MAX_HEIGHT`로 바꿀 수 있고, 0이면 상한이 없다. fee delegation 거래(블록 25~33)는 이 범위 안에 있다.

---

## 8. 기능별 backfill 설계 (R2-7, 10/5)

### 8.1 문제

지금은 이미 색인된 DB에서 기능을 새로 켜면 켠 시점 이후 블록만 처리된다. 그 기능의 데이터에는 앞부분이 빠진다. 기능을 껐다가 다시 켜도 꺼져 있던 구간이 빠진다. 그리고 DB는 어떤 기능이 어느 구간을 처리했는지 기억하지 않아서, 이 빈 구간을 알아낼 방법이 없다.

### 8.2 설계

**기능 상태 기록.** 기능마다 `/meta/features/<name>`에 상태를 둔다.

| 상태 | 뜻 |
|---|---|
| `active` | 이 기능은 색인된 모든 블록을 처리했고, 앞으로 들어오는 블록도 처리한다 |
| `through=H` | 이 기능의 데이터는 블록 0~H까지만 완전하다(꺼졌거나 backfill 중) |

**시작할 때 맞추기.** 켤 기능 목록이 정해지면 저장된 상태와 비교한다.

| 경우 | 처리 |
|---|---|
| 빈 DB | 켜진 기능을 모두 `active`로 기록한다 |
| 데이터는 있는데 기능 상태가 하나도 없음(이 변경 전에 만든 DB) | 켜진 기능을 모두 `active`로 기록한다. 이전에는 기능이 수집기 안에서 늘 돌았으므로 처리했다고 본다 |
| 켜졌는데 상태가 없음 | 블록 0부터 커서까지 backfill한다 |
| 켜졌는데 `through=H` | H+1부터 커서까지 backfill한다 |
| 꺼졌는데 `active` | `through=커서`로 기록한다. 데이터는 지우지 않고 갱신만 멈춘다 |

**backfill 실행.** 수집을 시작하기 전에, 해결된 기능 순서대로 하나씩 블록 순서로 처리한다. 블록마다 저장된 모델(블록, receipt)을 읽고 그 기능의 처리기만 블록 트랜잭션 안에서 부른다. 같은 트랜잭션 안에서 `through`를 그 블록으로 올린다. 그래서 중간에 멈춰도 마지막으로 commit된 블록 다음부터 이어 간다. backfill 중에는 이벤트를 내보내지 않는다. 끝나면 `active`로 바꾼다.

**수집 전에 동기로 하는 이유.** 잔액 이력처럼 블록 순서대로 누적해야 하는 기능이 있다. 수집과 backfill을 동시에 돌리면 뒤 블록의 기록이 앞 블록보다 먼저 쓰여 잔액이 틀린다. 단점은 backfill이 끝날 때까지 새 블록 수집이 멈춘다는 것이다.

### 8.3 한계

- 노드의 과거 상태를 읽는 기능(`balance.native`는 처음 보는 계정의 잔액을 직전 블록 기준으로 노드에서 읽는다)은 archive 노드가 아니면 정확히 backfill되지 않는다. 노드가 그 상태를 버렸으면 잔액을 0에서 시작한다(지금 수집과 같은 동작이고 경고를 남긴다).
- API는 아직 기능 상태(갱신 중지, backfill 중)를 알려 주지 않는다. 로그로만 남는다.
- 커서보다 뒤에서 기능을 켜는 일(특정 높이부터 켜기)은 다루지 않는다.

### 8.4 진행 기록 (10/5)

| 산출물 | 내용 |
|---|---|
| `pkg/storage/feature_state.go` | `/meta/features/<name>`에 `{active, through}`를 JSON으로 저장한다(`FeatureStateStore`) |
| `pkg/feature/state.go` | `Reconcile`: 8.2절 표대로 쓸 상태와 backfill 작업을 계산한다. `Pipeline.Only`: 기능 하나의 처리기만 남긴다 |
| `pkg/fetch/backfill.go` | `Fetcher.Backfill`: 저장된 모델(블록, receipt)을 읽어 블록마다 트랜잭션 하나로 처리기를 돌리고, 같은 트랜잭션에서 진행 높이를 기록한다. 노드에서는 블록을 읽지 않는다 |
| `main.go` | 처리기를 만든 뒤 상태를 맞추고, 필요한 backfill을 수집 전에 해결된 순서대로 돌린다. backfill 처리기는 이벤트를 내보내지 않는다. 꺼진 기능은 경고 로그와 함께 `through=커서`로 기록한다 |
| 시작 실패 정리 | `NewApp`이 중간에 실패하면 연 자원(DB 잠금, 이벤트 버스, adapter, 클라이언트)을 닫는다. 예전에는 DB 잠금이 남아 같은 프로세스에서 다시 열 수 없었다. backfill 중단 시험에서 드러났다 |

검증 결과는 다음과 같다.
- `TestReconcile`: 빈 DB, 상태 기록 이전 DB, 새로 켬·다시 켬·끔·완료 경우.
- `TestBackfillMatchesFromScratch`: 기능 7개 각각을 끈 채 기준 시나리오를 색인하고, 다시 켜서 시작하면 backfill 뒤 저장 결과가 처음부터 켠 golden과 키·값까지 같다.
- `TestBackfillAfterDisable`: 중간 높이에서 `token.transfers`와 `balance.native`를 껐다가 다시 켜면, 꺼져 있던 구간만 채워 golden과 같아진다. 잔액처럼 블록 순서에 의존하는 기능도 결과가 같다.
- `TestBackfillResumesAfterCrash`: backfill 중간 블록의 commit 직전에 실패시키면 시작이 실패한다. 다음 시작은 마지막으로 commit된 블록 다음부터 이어 가고, 결과는 golden과 같다.
- keyspace golden에 기능 상태 키 7개가 더해졌다. 전체 시험, race 검사, live StableNet 시험(블록 0~3000)이 통과했다.
