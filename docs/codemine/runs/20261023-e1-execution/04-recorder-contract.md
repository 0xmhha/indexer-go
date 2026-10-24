# U02 재실행 recorder 계약

상태: [PR #211](https://github.com/0xmhha/indexer-go/pull/211)에서 필수 CI·squash merge·main 동기화를 완료했다. 현재 계약과 후속 변경은 [체크리스트](checklist.md)를 따른다.

## 입력과 출력

별도 `x/stablenet/executor` 모듈에서 공식 StableNet 고정 commit을 import한다. 정상 import가 완료한 블록, 부모 header/state와 노드 receipt를 입력으로 받아, 2차 실행의 거래별·Finalize 변화를 U01 `execwire.Result`로 만든다. 2차 실행은 노드 DB에 commit하지 않는다. 공식 StateDB.Commit은 trie commit보다 code batch를 먼저 저장하므로 code batch도 실제 DB에 반영하지 않고 전체 commit을 거절하는 guard가 필요하다. 실행부는 PostgreSQL에 접근하지 않는다.

원본 블록의 state/transaction/receipt root와 gas를 실행 전에 값으로 보존한다. WBFT `Finalize`는 전달받은 header.Root를 바꾼다. 바뀐 header를 정답으로 삼으면 root 불일치를 숨길 수 있으므로 원본 값과 비교한다. 노드 receipt와 재실행 receipt의 consensus root가 모두 원본과 같아야 한다.

## 기록 경계

- 부모 상태를 감싼 `state.Database`로 열고, 각 거래의 `SetTxContext`·공식 `ApplyTransaction`·`IntermediateRoot`를 순서대로 처리한다. Finalize는 마지막 ordinal이다.
- 감싼 trie에서 변경 직전의 최초 값과 변경 후 최종 값을 모은다. reverted 변경은 StateDB에서 취소된 뒤 flush되므로 성공한 잔여 변화만 기록한다.
- nonce·balance·code/storage root·Extra·존재 여부, storage slot과 clear, code bytes를 보존한다. 삭제 후 재생성과 계정은 그대로인 storage-only 변경을 검사한다.
- 성공한 outer creation은 constructor 안에서 소멸해 전후가 모두 부재여도 실제 account witness를 보존한다. root 변화가 없다는 이유로 생성 증거를 버리지 않고 verified receipt와 연결한다. 실패 생성은 이에 포함하지 않는다.
- `CopyTrie`는 감싼 객체를 풀어 공식 cachingDB가 처리할 concrete trie를 전달하고 다시 감싼다. 복사본의 추측 실행이 원본 기록을 오염시키면 안 된다.
- DAO/beacon 등 transaction 이전의 실제 protocol 변경이 있는 구성은 원인을 잘못 귀속하지 않는다. 현재 StableNet 구성과 공식 processor를 확인해 정확히 기록하거나 지원하지 않는 조합을 명확하게 거절한다.
- EVM 실행 중 취소와 모든 종료 경로에서 소유한 callback·worker를 정리한다. 취소된 결과를 내보내지 않는다.

## 누락 검증

state root 일치만으로 export journal의 완전성을 주장하지 않는다.

제품 경로에서는 각 step의 시작 trie 복사본에 **내보낼 기록**을 적용해 실제 step root와 비교하는 방법을 사용한다. storage 기록을 별도로 적용해 account의 after storage root와 대조한 뒤 account 기록을 적용한다. account에 적힌 root만 복사하면 storage 기록 누락을 숨길 수 있으므로 두 단계를 분리한다. code 변화에는 해당 hash와 일치하는 bytes가 있어야 한다. clear·삭제도 같은 검증을 통과해야 한다.

기능 검사에서는 별도로 before/after trie를 양방향 순회하여 계정·storage의 변화와 기록을 대조한다. leaf의 hashed key를 기록 주소·slot의 Keccak와 연결할 수 있으므로 검증을 위해 preimage 역산에 의존하지 않는다. 제품 경로에서 전체 상태를 거래마다 순회하지 않는다. 성능 수치를 추정하거나 추가 측정하지 않는다.

fault 검사는 실제 trie 갱신을 계속한 상태에서 journal의 account·slot·code 기록만 버린다. 원본 EVM root가 맞더라도 기록 검증이 실패해야 한다. 거래1에서 바꾸고 거래2에서 원복한 경우도 거래별 기록을 남긴다. 한 거래 내부의 최종 순변화와 opcode별 이력을 혼동하지 않는다.

## 검사 범위와 다음 경계

실제 EVM·WBFT 라이브러리 fixture를 사용하되 `node.Start`, peer 접속, 성능 측정은 실행하지 않는다. 재실행 전후 backing DB의 durable write를 비교한다. fixture의 성공을 실제 peer의 블록 수락·운영 복구 증거로 확대하지 않는다.

U01의 chain/reference·ordinal·raw RLP·bounded transport 계약을 유지한다. U03은 spool·ACK, U04 이후는 상태 저장·공개 경계를 구현한다. DB 연결4개, writer7개의 DB 동시성2개, RPC5 HTTP/s와 최종 공개 직전 fresh는 E1 계획대로 유지한다.
