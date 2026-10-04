# 기능 레지스트리 상세 설계 (10/5)

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
