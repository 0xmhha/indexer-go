# U10 실행 모드·node 조립 계약

상태: native context/feature·publication, config/preflight/control, DB-only baseline 잔액, app 복구/종료, feature 재시작, 공식 node adapter/settings/lifecycle의 기능 RED 후 focused race GREEN. 전체 통합 판정은 U11이다. 실제 node.Start·peer·성능 측정·노드 DB GC는 실행하지 않는다.

실행 모드의 balance.native는 Deps.NativeBalances를 사용한다. nil인 기존 모드는 profile accounting·RPC 초기화를 그대로 유지한다. Native provider가 있으면 PrepareBlock에서 accounting/RPC를 수행하지 않고 publication context에 묶인 거래별·Finalize 전후 잔액을 처리한다. 블록 높이/hash가 다른 context와 입력 없는 context는 오류이며 RPC/zero/latest로 대체하지 않는다.

입력은 ordinal/address 순서와 uint256 범위를 검증하며 context에 넣거나 반환할 때 BigInt를 복사한다. publication의 prepare와 handler가 같은 고정 블록 입력을 받는다. 새 legacy 잔액 record는 이미 account part가 검증한 Before로 초기화하고 이후 매 step의 Before와 기존 projection 값을 대조한다. 불일치는 publication 오류다. After-Before와 transaction hash로 이력을 남기며 Finalize는 zero tx hash다. 기존 RPC accounting을 다시 실행하거나 음수 오류를 RPC resync로 감추지 않는다.

이 변화는 기존 balance/history projection을 정확한 실행 상태로 갱신한다. 초기 state coverage를 자동으로 생성하는 기능은 아니다. 실행 mode 시작 전 anchor·covered state seed가 필요하며 전체 bootstrap/검증 도구는 E2다.

앱은 기존 RPC client/pool만 canonical/metadata에 사용한다. clear/reindex/gap 복구를 IO 전에 거절하고, 명시적으로 준비한 anchor와 baseline·chain ID·genesis를 검사한다. legacy backfill/Fetcher.Run을 실행하지 않는다. feature definition/checkpoint가 공개 높이와 다르면 자동 backfill 없이 거절한다. durable reorg는 API 생성 전, 다시 relay/listener 시작 전에 복구한다. Stop은 수신기와 소유 worker를 취소·join한 뒤 기존 fetcher/outbox와 DB를 닫는다.

PG 잔액 조회는 공개 snapshot에서 기존 feature projection을 우선 읽고, untouched account는 검증된 covered state로 조회한다. 공개 높이 밖이나 coverage 누락을 0/RPC로 보정하지 않는다. publication 내부는 기존 projection Before를 유지한다.

격리 모듈의 nodebackend는 공식 node/eth 조립과 canonical/header/body/stored receipt/StateCache를 Replay에 연결한다. 실제 서비스는 테스트하지 않는다. JSON config는 명시적 genesis·chain/network·anchor와 절대 경로를 요구한다. HTTP/WS/노드 IPC·discovery·mining은 시작하지 않고 explicit static peers만 설정한다. 실행부 socket은 별도 private execwire다.

OnReady는 인증된 indexer Hello와 startup backlog ACK gate 뒤에만 node.Start를 호출한다. 취소/오류는 자신이 연 node/spool만 닫는다. 준비된 노드 DB에서 anchor header/root가 없으면 fail closed이며 blind SetHead나 자동 bootstrap은 없다. StateScheme=path, StateHistory=0, TransactionHistory=0으로 E3의 검증된 보존 정책 전에는 history 자동 삭제를 활성화하지 않는다. E1의 spool 용량 상한과 노드 history 보존은 별도다.

실행 설정은 기본 nil/off다. enabled 시 PostgreSQL max_conns4·공유 RPC5·live HTTP canonical source·원래 anchor 높이/hash/root·bounded queue/timeout·single-chain full ingest를 요구한다. 다른 block source·RPC replay/record·API-only·finalized/confirmations와의 모호한 조합은 거절한다. clear/reindex/gap-recovery는 IO 전에 거절하며, 실행 control이 있는 PG는 legacy BeginBlock의 공개/fresh 우회를 차단한다. 이 검사들은 각각 RED 후 config/app/실제 PG race GREEN을 확인했다.

실행 수집 지원은 체인 이름 문자열로 판단하지 않는다. 공통 `chains.ExecutionProfile`/`SupportsExecution`과 EVM profile의 명시적 `WithExecutionJournals` 선언을 사용하며 현재 기본 제품 중 공식 producer가 있는 StableNet만 이를 켠다. 이름만 같은 generic profile은 거절한다. capability RED 후 profile/app/체인 분리 구조 검사를 통과했고 identifier 제한은 늘리지 않았다.
