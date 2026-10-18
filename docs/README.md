# 문서 안내

현재 범위는 EVM 전용 프레임워크다. StableNet·Arc의 고유 규칙은 별도 profile에 두며 Stable은 제외한다. 구현 방법·운영 조건·남은 작업·과거 기록은 아래 문서에서 각각 확인한다.

## 1. 사용·확장 문서

| 목적 | 문서 |
|---|---|
| 빌드, 최근 높이에서 full node로 시작 | [빠른 시작](QUICKSTART.md) |
| 공통 코어, profile, writer, 저장소, stream 구조 | [아키텍처](ARCHITECTURE.md) |
| 설정·역할·멀티체인·알림 | [설정](CONFIG.md) |
| 24시간 원본 윈도우·원천 DB·체인별 수집 모듈·후처리 설계 | [윈도우 수집 설계](analysis/windowed-feed-design.md) |
| 구현된 원본 저장소·외장 경로·공유 예산/설정·압력과 삭제 승인 | [디스크 윈도우 저장소](WINDOW_STORAGE.md) |
| 원천 저장 transaction·generation·DB 삭제 guard와 현재 한계 | [원천 DB 저장](CAPTURE_STORAGE.md) |
| 영구 객체 경로·내구성·orphan 정리와 정규화 행/페이지 조회 | [영구 원천 저장](PERMANENT_SOURCE_STORAGE.md) |
| Arc ETH/68 peer·수신/검증 worker·지원 자료와 실행 경계 | [Arc peer 모듈](../modules/feeds/arc/README.md), [공급 감사](analysis/arc-peer-capabilities.md) |
| finality 정책·reorg 재개·raw 소비자 ACK·선택적 GC | [Feed 런타임](FEED_RUNTIME.md) |
| 원천 DB의 해시·주소/topic/범위 색인·fork 고정 페이지 API | [원천 검색](SOURCE_QUERIES.md) |
| 원천 DB를 기존 writer에 연결·strict coverage·처리 receipt | [DB facts 후처리](SOURCE_FACTS.md) |
| 지정 계정·code·storage의 hash/proof 공급·DB-only snapshot과 제한 | [상태 증명 공급](STATE_PROOFS.md) |
| provider 호출 trace 수집·영구 저장·fork 고정 DB 페이지 | [호출 trace](CALL_TRACES.md) |
| 전체 blob의 KZG 검증·공급 worker·영구 저장/DB-only 페이지 | [Blob 입력](BLOB_INPUTS.md) |
| 수신·proof·상태 후처리/RPC의 지연·적체와 운영 기준 | [단계별 계측](FEED_TELEMETRY.md) |
| 지정 상태의 현재값·블록 변경·독립 undo/event·조회 API | [지정 상태 후처리](TRACKED_STATE_PROJECTION.md) |
| 과거 Arc ExEx 비교 구현·IPC·ACK와 실행 설정 | [Node-feed](NODE_FEED.md) |
| Arc 연속 수집·처리 큐·지연 계측·복구 범위 | [수집 큐](analysis/arc-acquisition-queue.md) |
| API 경로·조회·구독·현재 API 목록 | [API 안내](API.md) |
| 체인별 디코딩·회계·기능 커스터마이징 | [체인 확장 계약](CHAIN_CUSTOMIZATION.md) |
| 프로젝트 처리기·저장·API·결정성·호환성 | [SDK](SDK.md) |
| frontend의 GraphQL 수정·REST 이전 | [frontend 이전](FRONTEND_MIGRATION.md) |

별도 모듈 예제: [chainprofile](../examples/chainprofile/README.md), [receipts](../examples/receipts/README.md). 세부 필드·메서드는 [GraphQL snapshot](../pkg/app/testdata/api/graphql-schema.txt), [JSON-RPC snapshot](../pkg/app/testdata/api/jsonrpc-methods.txt)과 해당 등록 코드를 확인한다. Snapshot은 시험 구성의 API를 고정하며, 기능·체인·declared 모드에 따라 실제 제공 범위가 달라질 수 있다.

## 2. 작업 상태와 근거

- **현재 수집 설계:** [24시간 윈도우와 원천 DB](analysis/windowed-feed-design.md). 체인별 네트워크 모듈·완전성·내구성·후처리·삭제 조건과 구현 순서를 정의한다.
- **이전 노드 feed 검토:** [node-backed acquisition options](analysis/node-feed-options.md). full node/ExEx 검토의 과거 기록이며 현재 개발 방향은 윈도우 수집 설계를 따른다.
- **현재 남은 작업:** [open-items](analysis/open-items.md). 1~3번의 Arc 기본 구현/검증 범위와 실제 네트워크 조건, Arc4번 runtime의 신뢰/운영 범위와 다음5번부터의 구현 순서, 운영 환경/별도 조건과 보류 항목을 구분한다. R03/R04/R07과 윈도우 설계를 함께 추적한다.
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

- [앱 feed 실행](FEED_APP.md): source.feed 설정, Arc 실행 plan, epoch/undo/종료와 혼합 멀티체인.
- [기능별 완료·독립 worker·coverage API](PROJECTION_PROGRESS.md): scoped 완료 높이/hash, epoch/gap, 자체 transaction과 HTTP 조회.
