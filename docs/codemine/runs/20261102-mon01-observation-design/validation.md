# MON-01 source review and validation

## 확인한 사실

소스 기준은 main5250f4d3753e5321aae9736991922345b55b75c1이다. 주요 producer와
소비자/권한 경계를 직접 읽어 [설계의 근거 표](../../../NATIVE_MONITORING_INTEGRATION.md#현재-코드에서-확인한-재사용-지점)에 연결했다.

- Store capacity SQL은 database/cluster 전체이며 체인 schema 크기가 아니다.
- stats는 기존 pool을 직접 사용한다. API 예약은 context 기반이며 background에
  그 context를 넘기면 경계가 달라진다. source latest-attach·30초 timeout·
  serial DB/WAL·journal-only failure 의미가 현재 코드에 있다.
- telemetry와 native/SQL metadata는 다른 일관성/시각/epoch 영역이다. pipeline
  NetworkHead는 현재 채워지지 않으며 raw intake truncation은 lower bound다.
- window Usage는 순수 메모리 getter가 아니고, source OnCommitted는 restart
  seed를 포함한다. observer는 원래 사건 시각을 새 sample time으로 바꾸면 안 된다.
- 기존 guard/latch/Resume, chain notifications, Docker emergency watcher가 있다.
  이들을 새 observer의 자동 actuation이나 공통 notification 권한으로 취급하지 않는다.

소스 근거는 실제 파일 링크와 주요 contract marker로 정적 대조했다.
실행 시 성능/동시성/운영 결과로 확대하지 않는다.

## 선택한 결정과 미검증 범위

per-component provenance/time와 resource-scope 계약, 기존 결과 tap, 순수 판정과
별도 alert state/sink, no-new-pool/no-new-node-RPC, reset/unknown/backpressure,
cancel/join과 구체적 MON-02/03 slices를 확정했다. 제안 namespace/module/cap은
현재 구현·운영값이 아니다. 실제 sampler 연결·새 DTO·hysteresis·sender·checkpoint·
Docker source는 아직 구현하지 않았다.

실제 DB 권한/리소스 매핑·APFS/peer/checkpoint/private 설정·운영 임계값과
latency·watcher 종료·전송/장애/복구는 미검증이며 HOLD다. 코드를 읽어 찾은
경합 가능성·lifecycle 요구는 재현된 운영 장애나 성능 원인으로 단정하지 않는다.

## 문서 검증과 릴리스

로컬 문서 검증은13개 문서/292개 상대 링크·anchor/오류0, 기존 CI policy24개,
14개 고유 작업 ID와 다음 MON-02-A 참조, 9개 source contract marker와
whitespace를 확인했다. staged secret0, reachable history15개는 이전 검토된
공개 fixture와 일치하며 미검토 항목0이다. private config hash/mode와 runtime/
workflow/config/default 소스는 변경하지 않았다.

compiler/DB/RPC/native/process/race200/성능 검사를 실행하지 않았다.
실제 exact-head docs-only CI·squash·main 동기화는 관련 PR/후속 인계의 실행된
식별자로 확인하며 이 문서로 미리 인증하지 않는다.

수용한 E1/first-import와 이전 두 맥락은 [SESSION_STATE](../../../SESSION_STATE.md)에
보존한다. 임시 로그는 보충 자료이며 설계·검증·다음 행동의 유일한 저장소가 아니다.
