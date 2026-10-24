# U04~U08 DB 통합 계약

상태: [PR #213](https://github.com/0xmhha/indexer-go/pull/213), merge `08098f2`에서 필수 CI 4개·squash/main 동기화를 완료했다. DB control·part·state 이력·core writer·publication·조회·rewind를 통합했다. 현재 작업은 [체크리스트](checklist.md)를 따른다.

## 초기화와 part 내구성

- 기존 pool의 MaxConns=4를 확인한다. pool을 추가하거나 연결 수를 늘리지 않는다.
- chain/genesis·원래 anchor/hash/root·partition 크기를 고정한다. 이미 저장된 anchor block의 hash/root가 일치해야 한다. legacy cursor가 다른 높이면 초기화를 거절한다.
- 초기화는 상태 baseline이나 앱 시작을 완료하지 않는다. 공개 높이는 anchor에 머물며 상태 coverage가 준비되지 않으면 상태 처리·실행 모드 시작을 거절한다.
- writer별 cursor와 `(writer,generation,height,hash,parent,digest)` part를 함께 commit한다. 연속 높이와 parent를 확인하고 정확한 중복만 재사용한다.
- 콜백은 해당 Store의 bound context를 동기적으로 사용한다. part는 feature undo를 기록하지 않으며 outbox·공개 높이·ACK·fresh RPC를 진행하지 않는다.
- PostgreSQL fsync=on을 요구하고 synchronous_commit=on을 transaction에 적용한다. 오류·취소는 part 자료와 cursor를 함께 rollback한다.
- schema별 advisory 공유 try-lock과 generation을 확인한다. rewind 독점 fence와 충돌하면 연결을 점유하며 기다리지 않고 재시도 오류를 반환한다. writer별 cursor row lock은 같은 writer의 중복 commit을 직렬화한다.
- 애플리케이션 값은 파라미터로 바인딩한다. writer 이름은 닫힌 목록이며 schema·partition 식별자는 검증/인용한다.

## 다음 기능의 경계

상태 이력은 anchor의 명시적인 baseline과 `(height,ordinal)` 변화를 보존한다. 기존 값과 Before의 CAS, 삭제와 slot clear, 알 수 없는 상태의 coverage 오류를 검사한다. 최신 값이 공개 높이보다 앞서면 이력에서 공개 시점의 값을 조회한다.

U05는7개 queue가 최대2개 part transaction을 공정하게 공유하도록 한다. U06만 같은 입력의7개 part와 feature·outbox·공개 높이를 하나의 transaction으로 결합하고 커밋 직전 새 fresh 검사와 응답 뒤 generation fence를 수행한다. U07은 모든 API 경로에 공개 시점 read fence를 설치한다. U08은 durable rewind marker·취소/join·rollback·재시작 복구를 연결한다. 각 기능을 RED/GREEN과 문서로 완료한 뒤 통합 전체 회귀·CI4개를 확인한다.

실제 노드·peer·성능 측정·디스크 포맷·노드 DB GC는 보류다. RPC5 HTTP/s 제한은 변경하지 않는다.

## 계정 baseline·이력의 현재 계약

`SeedExecution`은 선언한 anchor/hash/root와 정확히 일치하는 명시적 snapshot을 part 시작 전에 한 번 저장한다. 동일 digest만 멱등 재사용한다. 이 단계는 supplied covered keys의 상태 저장이며 전체 trie/초기 상태의 암호학적 검증 도구(E2)를 구현했다고 주장하지 않는다. 현재 accounts·storage baseline을 구현했으며 code baseline도 구현했다.

계정 nonce/Extra는 numeric20, 잔액은 numeric78과 uint256 범위를 쓰며 문자열/정수로 변환한다. Before 전체 값이 작업용 최신 값과 같아야 한다. 기존 계정이 누락되면 coverage 오류이고, 입력이 명시한 부재 Before의 새 계정은 구분해 허용한다. tombstone은 알려진 부재이며 알 수 없는 key의 임의0과 다르다. 거래→Finalize를 ordinal로 적용하고 partition된 불변 이력에 COPY한다. 최신 값은 같은 transaction에서 batch로 갱신한다.

`ExecutionAccountAt`은 요청 높이의 이력을 읽는 내부 상태 port다. 공개 상태 API는 [U07](09-read-contract.md)의 read snapshot과 공개 높이를 거친다. part scope 밖·다른 writer/reference/digest·만료한 callback context의 상태 쓰기는 거절한다. 중도 취소는 이력·최신 값·cursor를 함께 rollback한다. 삭제/재생성, 앞선 최신 값이 있어도 이전 이력 조회, precision·coverage를 검증했다. 이력 leaf partition은 명시한 parent의 coordinator 소유권을 따른다.

## slot·clear의 현재 계약

Storage writer는 account writer의 commit 순서에 의존하지 않고 자기 root metadata로 Before의 existence/root를 CAS한다. slot은 단계가 시작하기 전의 논리 값과 비교한다. **같은 단계의 clear를 적용한 뒤 Before를0과 비교하지 않는다.** 비교가 끝나면 clear proof와 slot After를 저장한다.

slot 최신 행은 삭제하지 않아도 된다. 높이·ordinal이 최근 clear보다 **엄격히 오래된** 값만0으로 취급한다. clear와 같은 ordinal의 After는 새 값이다. 이 규칙으로 예전 높이의 값과 clear 뒤의 새 값을 모두 조회한다. baseline empty root·명시적인 부재·clear는 알려진0의 근거이며, nonempty root의 미확인 slot은 coverage 오류다.

계정/slot 이력의 baseline block_time은 실제 anchor block의 timestamp를 보존한다. 처음 구현이 epoch0을 쓰는 결함도 expected1234/actual0 RED 후 수정했다. code/contract의 내부 저장 port는 구현했으며 공개 API는 [U07](09-read-contract.md)에 연결했다.

## code·contract의 현재 계약

코드는 Keccak hash를 검증해 내용 주소로 저장하며, 같은 hash의 다른 바이트를 덮어쓰지 않는다. 이전 계정의 code hash와 존재 상태를 contract writer 자신의 최신 값으로 CAS한다. account writer의 진행을 기다리지 않는다. 단계별 contract 이력을 보존하므로 생성 후 같은 블록에서 code가 사라져도 중간 코드와 이력은 남는다. 높이 조회는 해당 시점의 code/알려진 부재를 반환하고 미확인 주소는 coverage 오류다.

생성 높이·거래·creator는 검증된 성공(Status=1) 외부 생성 receipt/거래에서 만든 `ExecutionCreation`이 같은 tx index/hash의 생성 단계에 맞을 때만 기록한다. runtime code가 비어 있거나 생성자 안에서 계정이 사라져도 성공 receipt가 증명한 생성 정보는 보존한다. U05의 준비 단계가 이 모델을 조립한다. baseline 계약이나 임의 code 변화로 원래 생성 사실을 추정하지 않는다. `code_activated_height`는 새 코드의 관측 시점이며 `destroyed_height`는 code 비활성화 시점으로, opcode 실행 증거를 뜻하지 않는다. 전체 bootstrap/증명 도구는 E2다.

## 핵심 표 분리와 검증 범위

`ApplyExecutionCore`는 scoped writer에 맞춰 blocks·transactions·receipts·logs 중 자기 표만 쓴다. 로그·receipt가 먼저 저장돼도 다른 writer를 기다리지 않는다. 참조·높이·hash·tx/log 순서가 맞아야 한다. 블록/거래의 기존 저장 helper를 분리해 legacy `SetBlock`의 원자성은 유지한다. 이미 있는 다른 블록의 transaction hash를 앞선 미공개 높이로 이동시키지 않으며, 충돌 receipt/log도 덮어쓰지 않는다.

part의 현재 cursor generation과 같은 높이의 hash/parent/digest도 대조한다. 완료 receipt 하나로 손상되거나 미완료 rewind 상태의 cursor를 완료로 오판하지 않는다. U04 전체 TestExecution·기존 TestPortContracts·rollback·transaction-count·migration/undo가 실제 PostgreSQL/race GREEN이며 lint도 통과했다. 추가 성능 측정은 수행하지 않았다. 필수 전체 repository·CI 검사는 U04~U08 통합 PR 전에 수행한다.
