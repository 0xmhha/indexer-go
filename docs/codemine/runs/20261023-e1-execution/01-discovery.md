# E1의 현재 코드 기반

이 문서는 E1이 바꾸는 기존 구조를 요약한다. 미결 인터뷰와 초기 선택지는 유지하지 않으며 Git history로 확인한다. 구현의 정본은 [최종 결정](../../../decisions/20261023-e1-design/decision.md), [설계](02-design.md), [구현 계획](03-plan.md)이다.

## 1. 기존 데이터 흐름

- RPC source, Arc capture/facts source, optional nodefeed는 `pkg/fetch`의 순차 writer로 모인다.
- `writer.go`가 block transaction을 열고 핵심 표·기능·cursor·outbox를 함께 commit한다. source ACK는 durable commit 뒤다.
- facts source의 `ValidateCommit`은 공개 전 새 canonical RPC와 응답 후 epoch를 확인한다. 준비 상태 검사는 별도로 BeginBlock 전에 있다. 모든 RPC source에 같은 hook이 자동 적용되는 것은 아니다.
- Arc `ethpeer`는 header/body/receipt history를 요청한다. 정상 EVM 실행부와 동일하지 않다.
- PostgreSQL은 migration0013까지다. E1의 실행 이력·표별 cursor·공개 높이·rewind 표는 새로 구현한다.

## 2. 재사용 경계

- `pkg/core/model` codec과 profile의 binary block/receipt 변환.
- `pkg/rpcpool`의 공유 HTTP 예산과 fresh 의미.
- `pkg/fetch`의 기존 기능 처리, undo/outbox와 DB 포트.
- PostgreSQL의 예약 API 연결, 값 바인딩, 취소·rows 반환 경로.
- window/capture의 durability·generation·ACK·삭제 fence는 기존 모드에서 유지한다.

## 3. 계획에서 먼저 검사할 의존

- StableNet WBFT handler는 부모 header를 `Deps.Blocks()`로 읽으며 storage miss 때 source로 간다. 핵심 표 의존이 없다고 가정하지 않는다.
- DEX orderbook의 latest-height 조회는 별도 service 경로다. 블록 handler의 의존과 섞지 않는다.
- receipt parity fixture는 model codec 바이트를 그대로 비교한다. 새 실행 구간에서도 같아야 하며 차이를 자동 예외로 두지 않는다.
- 실제 노드 실행에 쓰이는 go-stablenet은 별도 module로 고정한다. root module과 같은 binary에 두어 geth/pebble 버전을 섞지 않는다.

## 4. 검증 범위

기존 `tools/astgraph`는 운영 Go 코드의 static call·interface call·import·reference를 기록한다. 동적 주입이나 callback의 실제 호출을 그래프만으로 확정하지 않는다. 별도 모듈과 테스트·다른 build target을 구분한다. 생성 그래프는 저장소에 넣지 않는다.

실제 EVM 실행을 하지 않는 `pkg/testchain`의 대본 상태를 실행 정확성의 oracle로 사용하지 않는다. 실행부의 결정적 라이브러리 fixture와 독립 root/변화 대조를 먼저 만든다. 실제 노드 서비스와 속도 측정은 보류한다.
