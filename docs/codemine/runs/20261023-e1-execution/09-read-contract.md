# U07: public read boundary

상태: [PR #213](https://github.com/0xmhha/indexer-go/pull/213), merge `08098f2`에서 필수 CI 4개·squash/main 동기화를 완료했다. 공개 높이에 고정한 core/state/API snapshot과 취소·반납을 검증했다. 실행 중인 다른 API 프로세스에 hot-enable하지 않으며 읽기 전용 API도 새 모드 시작 뒤 다시 열어 control을 인식해야 한다. 현재 작업은 [체크리스트](checklist.md)를 따른다.

- core의 SELECT는 정적인 exec_visible_* view를 사용한다. block/tx/receipt/log의 hash·높이·목록·기간·존재 검사·receipt join을 공개 높이 이하로 제한한다. DML과 part/rewind 검증은 base table을 명시적으로 사용한다. SQL 문자열의 실행 시 치환이나 RLS 우회 가능성에 의존하지 않는다.
- latest height metadata는 control의 공개 높이를 따른다. transaction count는 실행 모드에서 공개 transaction만 센다. legacy mode의 기존 count/meta 경로는 유지한다.
- API read operation은 Repeatable Read/read-only transaction과 shared advisory try-lock을 사용한다. rewind marker가 있거나 독점 fence와 충돌하면 명시적 retry 오류를 반환한다. 알 수 없는 결과를 0/없음 성공으로 바꾸지 않는다.
- snapshot은 기존 예약/차용 admission을 바로 사용한다. 다른 연결을 먼저 얻는 probe를 추가하지 않는다. 동일 snapshot의 동시 GraphQL resolver는 rows의 수명까지 직렬화해 pgx connection-busy를 막는다. 취소·미소비 rows·callback 종료는 SQL/watch를 끝내고 transaction과 connection을 반납한다. 만료 context는 pool로 빠져나가지 않는다.
- GraphQL HTTP와 REST 고정 문서는 같은 AST/표준 validation/취소를 유지한다. query만 snapshot에 묶고 mutation은 쓰기 context를 유지한다. JSON-RPC query도 적용하며 notification/ABI mutation과 명시적인 RegisterMutationMethod는 제외한다. 등록형 GET/HEAD route도 snapshot을 사용한다. Streaming route는 명시적으로 제외한다.
- 실행 모드 REST는 응답 cache·singleflight·HTTP reuse를 우회하고 no-store로 응답한다. 이전 generation의 응답이나 취소와 분리한 context가 새 admission을 건너뛰지 않는다. 기존 모드의 REST cache는 유지한다.
- 체인별 handler도 같은 Store의 query reader를 예약/공유하며 종료 시 참조를 반납한다. pool 크기는 바꾸지 않는다. WebSocket 연결 전체를 transaction에 묶지 않는다. 구독은 공개 transaction이 기록한 outbox/event만 소비하며 part writer는 event를 발행하지 않는다.

## 공개 상태 API

GraphQL: executionAccount(address), executionStorage(address,slot), executionCode(address).
JSON-RPC: getExecutionAccount, getExecutionStorage, getExecutionCode.

각 값에 address/height/generation/blockHash/stateRoot를 포함한다. 정수는 decimal 문자열, code/slot/hash는 hex다. account의 exists·nonce·balance·extra·codeHash·storageRoot를 보존한다. 값은 공개 높이의 불변 이력에서 읽는다. 최신 행의 last_height 필터로 옛 값을 지우지 않는다. 알 수 없는 account/slot/code는 coverage 오류이며, 알려진 부재/empty root/clear의 0과 구분한다. 전체 상태 bootstrap이나 독립 trie proof API를 구현했다는 뜻은 아니다.

## 검증

실제 fsync-on PostgreSQL: 미공개 core 노출 RED1, snapshot 미구현 RED2, 공개 상태 미구현 RED1 → 같은 검사 race GREEN. 공개 도중 동일 snapshot의 이전 높이 유지, pool4 중 3개가 사용 중이어도 예약 1개로 진행, 쓰기 거절, abandoned rows 강제 종료, 활성 pg_sleep SQL 취소/join·반납을 검사했다. U04~U07 모든 TestExecution은 함께 PG/race GREEN이다.

API: GraphQL/JSON-RPC snapshot 누락 RED2, 상태 API 누락 RED2, 등록형 read route RED1, REST cache 재사용 RED1, chain reader context 누락 RED1 → 관련 기능/race GREEN. 기존 parse/validation/limits/cache/route/종료 검사를 함께 실행했다. chain 테스트는 mock의 미구현 높이 조회와 기존 Stop의 비멱등 호출을 유발한 helper를 수정했다. 제품의 WebSocket Stop 계약을 이 기능에서 바꾸지는 않았다. 새 동시성200회와 전체 필수 검사는 U04~U08 통합 PR 전에 완료한다. 노드 실행·성능 측정 없음.
