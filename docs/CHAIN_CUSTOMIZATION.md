# EVM 체인 커스터마이징 계약

프로젝트가 공통 코어를 고치지 않고 체인 규칙과 기능을 붙이는 방법이다. 현재 범위는 EVM 체인이며 Stable(Tendermint/CometBFT)은 제외한다. StableNet과 Arc의 고유 규칙은 각각 별도 profile에 있다. EVM RPC가 있다는 사실만으로 Ethereum의 헤더·거래·회계 규칙과 같다고 판단하지 않는다.

실행 가능한 별도 모듈은 [examples/chainprofile](../examples/chainprofile/README.md)이다. 기존 Arc profile을 프로젝트 이름으로 등록하고 감지 범위를 테스트넷으로 좁힌다. 기능·저장·API 확장은 [SDK](SDK.md)와 [receipts 예제](../examples/receipts/README.md)를 따른다.

## 1. 확장 위치와 책임

| 바꾸려는 것 | 확장 지점 | 책임 |
|---|---|---|
| 노드 감지, RPC 블록·receipt 디코딩, 기본 기능 | `chains.Profile`, `chains.Register` | 체인의 원래 식별자와 규칙을 보존해 `model`로 변환 |
| 거래 타입 | `evm.WithTxDecoder` | JSON 입력의 정식 해시·서명·sender 및 고유 필드 검증 |
| binary 거래, archive receipt의 gas price | `chains.BinaryProfile`, `evm.WithBinaryTxDecoder`, `evm.WithEffectiveGasPrice` | JSON과 같은 거래·receipt 의미를 복원 |
| 헤더 해시 | `evm.WithHeaderHasher` 또는 자체 profile | 체인의 실제 해시 규칙으로 계산하고 노드 값과 대조 |
| native balance 변화 | `chains.AccountingProfile`, `evm.WithNativeAccounting` | gas payer·수수료 분배·value·내부 이동·mint/burn·보상 등 지원 범위의 변화량 계산 |
| native coin의 토큰형 이벤트 | `chains.NativeCoinProfile`, `evm.WithNativeCoinContract` | native emitter를 ERC-20 색인과 토큰 알림에서 제외 |
| 모델의 체인 고유 필드 | `model.ExtKey`, `model.RegisterExtCodec` | typed accessor, 저장 codec과 round trip 제공 |
| 고유 이벤트·통계·설정·API | feature 및 SDK registry | 블록 트랜잭션 안에 저장하고 조회·구독을 별도 등록 |

내장 체인은 `pkg/chains/<chain>`에 둔다. 외부 프로젝트도 자기 모듈의 `pkg/chains/<chain>`에 둔다. 공통 fetch·storage·API에 체인 이름이나 chain id 조건문을 더하지 않는다. app/main은 등록 패키지를 import하는 조립 지점이다.

등록은 Go 패키지가 로드될 때 수행한다. YAML은 이미 연결된 profile과 feature를 선택·설정하며, 패키지를 다운로드하거나 동적으로 로드하지 않는다. 추가 패키지를 import한 자기 바이너리를 빌드한다.

## 2. Profile 선택과 감싸기

`chains.Register(profile, priority)`는 높은 priority부터 감지한다. 현재 내장 specific profile은 100, generic EVM은 0이다. 같은 priority면 id의 사전순이다. 프로젝트가 기존 체인을 커스터마이징할 때는 **새 id**와 더 높은 priority를 쓴다. 기존 id를 덮어쓰면 duplicate registration panic이 난다. alias는 `chains.RegisterAlias(alias, id)`로 등록한다.

감지 함수는 허용하려는 chain id/client 범위를 좁혀 쓴다. 모든 노드를 받는 높은 priority profile은 다른 체인의 규칙까지 바꾼다. registry에는 프로세스 전체에서 공유하는 profile이 들어가므로, 선택 이후 profile에 체인별 가변 상태를 저장하지 않는다.

단일 체인은 `--adapter <id 또는 alias>`로 등록된 profile을 지정할 수 있다. 빈 값이나 등록되지 않은 이름은 현재 자동 감지로 넘어간다. 오타가 강제 선택됐다고 생각하지 말고 실제 시작 로그의 profile을 확인한다. 명시적으로 선택된 profile의 `Detect`가 RPC chain id를 다시 제한해 주는 것은 아니다. 별도 체인의 RPC에 강제 지정하지 않는다.

예제의 핵심은 다음과 같다.

```go
type Profile struct{ *evm.Profile }

func New() *Profile { return &Profile{Profile: arc.New()} }
func (*Profile) ID() string { return "project-arc" }
func (*Profile) Detect(info chains.NodeInfo) bool {
    return info.ChainID == 5042002
}
```

`*evm.Profile`을 embed하면 `NativeAccounting`, `NativeCoinContract`, binary 디코딩 등 선택 인터페이스의 메서드도 남는다. `struct{ chains.Profile }`처럼 기본 인터페이스만 embed하면 선택 메서드가 가려질 수 있다. 이 경우 `AccountingOf`가 Ethereum 회계로 돌아가거나 native emitter가 토큰으로 분류될 수 있다. 다른 방식으로 감싸려면 필요한 선택 인터페이스를 명시적으로 위임하고 시험한다.

기존 profile을 재사용할 때는 decoder·header·receipt·회계 규칙을 함께 유지한다. 프로젝트 기능을 기본으로 켜려면 `Features()`를 override해 기존 `Features()`의 결과에 등록된 기능 이름을 더한다. 설정으로 끌 수 있으며, 필요한 선행 기능은 feature의 `Requires`로 선언한다. 체인별 feature 설정은 멀티체인의 각 체인 설정에 둔다.

새 규칙을 구성할 때는 `evm.New("새 id", options...)`를 쓸 수 있다. `WithDetect`, `WithFeatures`, `WithTxDecoder`, `WithHeaderHasher`, `WithNativeAccounting`, `WithNativeCoinContract`는 생성 시 조합하는 옵션이다. `arc.New()`·`stablenet.New()` 자체가 임의 옵션을 받는 factory는 아니다. 생성 옵션만으로 표현할 수 없는 규칙은 자체 profile/선택 인터페이스를 구현한다.

## 3. 디코딩과 검증 계약

- `Profile.DecodeBlock`은 full transaction RPC 입력을 중립 모델로 바꾼다. 저장·API·기능의 식별자는 `model.Block.Hash`, `model.Transaction.Hash`를 쓴다. Geth view에서 다시 계산한 해시는 체인별 헤더·거래에 맞지 않을 수 있다.
- 기본 EVM decoder는 헤더 해시와 알려진 거래의 해시·sender를 검사한다. `WithTxDecoder`로 교체한 타입은 **프로젝트 decoder가 그 검증을 책임진다.** 고유 타입을 표준 타입으로 바꿔 저장하지 않는다.
- 지원하지 않는 JSON 거래 타입은 현재 EVM 구현이 `Opaque`로 보존할 수 있다. 이 상태는 정식 해시·sender를 검증했다는 뜻이 아니다. 고유 타입을 지원한다고 선언하려면 별도 decoder와 변조 거부 시험이 필요하다.
- 해시 불일치를 없애려고 `WithHeaderHashCheck(false)`를 쓰는 것은 해시 검증을 생략하는 것이다. 현재 EVM 확장의 기본 방침은 실제 해시 규칙을 구현하는 것이다. custom hasher의 입력은 geth Header이므로 새 필드까지 자동 지원하지 않는다. 현재 `blockAccessListHash`·`slotNumber`가 있는 헤더는 generic EVM이 typed extension으로 보존하고, custom hasher는 자신의 대응이 구현되기 전까지 거부한다.
- 자체 필드는 한 패키지가 소유하는 `ExtKey`와 typed accessor로 접근한다. 영구 저장할 값은 이름이 고유한 codec을 등록한다. codec 이름은 저장 후 바꾸지 않으며 encode/decode 오류를 전파한다. codec이 없는 이전 바이너리의 읽기 호환성도 확인한다.

`BinaryProfile`은 선택 사항이다. 이를 구현한 profile만 era1 입력을 받는다. 고유 거래의 JSON decoder를 더했다면 binary decoder도 같은 규칙으로 구현하거나 해당 binary 입력을 명시적으로 거부한다. archive receipt는 실행 gasUsed·거래 위치·effective gas price 등을 복원해야 하며, JSON 경로와 같은 입력에서 대조한다. 현 EVM binary 구현은 blob gas price를 복원하지 않으므로 nil을 정확한 0으로 해석하지 않는다.

## 4. Native 회계와 상태

`NativeAccounting.NativeDeltas(ctx, block, receipts, env)`는 주소별 변화량을 돌려준다. `balance.native`가 이를 저장한다. 다음을 체인 규칙에 맞게 구분한다.

- sender와 gas payer, 성공/실패 시 value, 실제 gas 사용량과 가격.
- tip·base fee·blob fee의 수령자 또는 소각, 보상·withdrawal.
- top-level value와 내부 이동, native 이벤트와 ERC-20 이벤트의 중복.
- 블록·receipt 입력을 변경하지 않는 계산, 오류 전파와 결정성.

기본 `EthereumAccounting`은 블록·receipt에 보이는 top-level value, gas, withdrawal을 처리한다. trace가 필요한 내부 이동과 PoW 보상까지 완전한 잔액이라고 보장하지 않는다. Arc는 native system event와 자체 수수료 규칙을 쓴다. 다른 체인에 Arc 회계를 그대로 적용하지 않는다.

`AccountingEnv`는 이전 블록 조회만 제공한다. 임의 상태 조회·trace를 제공하는 계약은 아니다. 새로운 체인의 정확한 회계에 그런 자료가 필요하면 자료 획득과 replay 계약을 먼저 설계한다. 현재 인터페이스가 이미 이를 제공한다고 가정하거나 공통 코어에 체인별 RPC 호출을 넣지 않는다.

운영은 full node이며 archive가 없다. 새 DB의 시작 높이와 새로 켠 기능의 backfill 높이에 필요한 상태가 실제로 남아 있어야 한다. `balance.native`·`token.metadata`의 시작 검사를 유지한다. `allow_pruned_state`는 과거 상태를 복원하지 않고, 잔액 또는 메타데이터 정확성을 떨어뜨릴 수 있다. 자세한 규칙은 [SDK 6장](SDK.md#6-결정성-규약)에 있다.

## 5. Feature·저장·API 계약

체인 규칙 계산은 profile/회계에, 선택적인 색인 결과와 조회는 feature에 둔다. 주소처럼 배포마다 달라지는 값은 feature 설정으로 읽는다. `Deps.Profile`과 선택 인터페이스로 필요한 능력을 확인하고, 기능이 필요한 능력을 얻지 못하면 등록을 실패시킨다.

처리기는 중립 모델과 저장 포트를 읽고, 받은 블록 context로 KV/포트에 쓴다. 이 쓰기는 블록과 commit되며 reorg에서 undo된다. 고유 prefix는 `sdk.RegisterKeyspace`로 선언한다. 시계·환경 변수·난수·외부 네트워크에 기대는 처리기를 만들지 않는다. 노드 상태 조회는 처리 높이를 지정하고 오류를 전파한다.

HTTP/GraphQL 조회는 SDK로 등록한다. JSON-RPC 고유 메서드와 이벤트 codec은 해당 registry에 등록한다. feature를 나중에 켜는 backfill, 설정 정의 변경, rollback 이벤트는 [SDK](SDK.md)와 feature 계약을 따른다. 등록된 API가 모든 체인에 같은 방식으로 mount될 수 있으므로, 기능이 꺼진 체인 또는 자료가 없는 경우의 응답도 정한다.

## 6. 검증과 호환성 범위

기존 [공개 RPC 시험](../pkg/app/live_rpc_test.go)은 opt-in이며 chain id/profile, 최근 시작, 블록·거래·receipt·로그 저장, API 해시, record/replay 일치를 검사한다. 모든 native 잔액·fork 실행·합의 증명까지 검사하는 시험은 아니다.

새 profile의 규칙에 맞춰 다음 증거를 마련한다.

1. 감지/명시 선택/alias와 다른 체인 선택이 바뀌지 않는지.
2. 실제 RPC fixture의 원래 해시·sender·receipt와 고유 필드, 변조 거부.
3. 선택 인터페이스가 wrapper에 남고, 기본 회계로 조용히 돌아가지 않는지.
4. 같은 기록의 JSON/binary(지원 시)·저장 codec·offline replay, Pebble/PostgreSQL 대조.
5. native 회계가 필요한 주소의 전후 노드 잔액과 일치하는지.
6. 기능의 재시작/backfill/reorg 결정성. SDK 처리기는 `sdktest`로 확인.

`examples/chainprofile`은 1~3의 로컬 회귀와 바이너리 빌드를 보여준다. 실제 노드·binary 입력·전체 회계·동시 운영까지 새로 확인한 예제는 아니다. 기존 Arc 테스트넷 기록은 [작업 기록 3.5](analysis/work-log-2026-10.md#35-1013-arcstable-추가와-공개-rpc-postgresql-검증)에 있다. mainnet과 모든 역사 구간은 미검증이다.

`pkg/sdk`·`pkg/sdk/sdktest`는 [SDK 7장](SDK.md#7-호환성-약속)의 호환성 약속을 따른다. 체인 profile에 쓰는 `pkg/chains`·`pkg/chains/evm` 및 다른 패키지는 그 약속 밖이다. 프로젝트는 모듈 버전을 고정하고 업그레이드 때 예제를 빌드·검증한다. 이 문서는 새 SDK 공개 면이나 버전 약속을 추가하지 않는다.

현재 Arc profile은 최신 발행 tag `v0.2.15` 이후 main에 추가됐다. 이 예제는 저장소 안의 `replace => ../..`로 검증한다. 저장소 밖으로 옮길 때는 Arc profile을 포함하는 commit 또는 향후 발행 버전을 고정해야 한다. 예제를 추가한다고 새 tag가 자동 발행되지는 않는다.
