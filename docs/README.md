# 문서 안내

현재 범위는 EVM 전용 프레임워크다. StableNet·Arc의 고유 규칙은 별도 profile에 두며 Stable은 제외한다. 구현 방법·운영 조건·남은 작업·과거 기록은 아래 문서에서 각각 확인한다.

## 1. 사용·확장 문서

| 목적 | 문서 |
|---|---|
| 빌드, 최근 높이에서 full node로 시작 | [빠른 시작](QUICKSTART.md) |
| 공통 코어, profile, writer, 저장소, stream 구조 | [아키텍처](ARCHITECTURE.md) |
| 설정·역할·멀티체인·알림 | [설정](CONFIG.md) |
| API 경로·조회·구독·현재 API 목록 | [API 안내](API.md) |
| 체인별 디코딩·회계·기능 커스터마이징 | [체인 확장 계약](CHAIN_CUSTOMIZATION.md) |
| 프로젝트 처리기·저장·API·결정성·호환성 | [SDK](SDK.md) |
| frontend의 GraphQL 수정·REST 이전 | [frontend 이전](FRONTEND_MIGRATION.md) |

별도 모듈 예제: [chainprofile](../examples/chainprofile/README.md), [receipts](../examples/receipts/README.md). 세부 필드·메서드는 [GraphQL snapshot](../pkg/app/testdata/api/graphql-schema.txt), [JSON-RPC snapshot](../pkg/app/testdata/api/jsonrpc-methods.txt)과 해당 등록 코드를 확인한다. Snapshot은 시험 구성의 API를 고정하며, 기능·체인·declared 모드에 따라 실제 제공 범위가 달라질 수 있다.

## 2. 작업 상태와 근거

- **현재 남은 작업:** [open-items](analysis/open-items.md). 완료 기준, 필요한 자료·결정, 보류와 범위 밖 항목을 관리한다.
- **결정·측정 기록:** [2026-10 작업 기록](analysis/work-log-2026-10.md). 당시 상태·실패·측정 환경과 후속 결정을 보존한다.
- **리팩토링 계획·10/9 완료 검토:** [계획](analysis/refactoring-plan.md), [검토](analysis/plan-audit.md).
- **설계 기록:** [chain profile](analysis/chain-profile-design.md), [feature registry](analysis/feature-registry-design.md), [reorg](analysis/reorg-design.md), [PostgreSQL undo](analysis/postgres-undo-design.md), [구독](analysis/subscriptions-design.md), [Phase 0](analysis/phase0-design.md).
- **AA 목록 API 제안:** [AA_BACKEND_TASKS](AA_BACKEND_TASKS.md). 승인된 실행 목록과 구분한다.

`analysis/`의 설계·측정·그래프는 작성 시점의 기록이다. 과거 경로·선택지·성능 수치가 현재 구현이나 운영 보장인 것은 아니다. 현 구조는 아키텍처와 코드, 남은 작업은 open-items를 기준으로 판단한다. 로컬 handoff는 세션 규칙·환경을 포함하는 인수인계 자료이며 커밋하지 않는다.

## 3. 갱신 원칙

- 현재 기능 설명은 사용 문서에, 실행할 일은 open-items에, 실험 근거는 작업 기록에 둔다.
- 미검증은 미검증으로 적는다. 순차 RPC 시험을 동시 운영이나 전체 회계 검증으로 확대 해석하지 않는다.
- 완료 항목을 실행 목록에 다시 올리지 않고, 보류·제외·승인 조건을 유지한다.
- API 이름·설정·파일 경로는 코드와 snapshot에 맞추고, 이동한 문서의 링크를 함께 갱신한다.
