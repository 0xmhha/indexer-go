# U03 durable spool·ACK 계약

상태: PR #212에서 발견한 종료·ACK quota 보완과 최종 전체 로컬 회귀 완료. 수정 커밋·CI 갱신 대기. [E1 계획](03-plan.md)과 [체크리스트](checklist.md)를 따른다. 직전 두 기능은 U02 recorder(PR #211), U01 wire(PR #210)다.

실행부 모듈 안에 결과 spool을 둔다. root indexer나 PostgreSQL을 import하지 않는다. 실제 노드·peer·추가 성능 측정·디스크 포맷·노드 DB GC는 보류한다.

## 저장과 공개

- configured directory와 chain/genesis·최초 anchor identity를 검증하고 단일 process lock을 가진다. 다른 spool과 symlink를 허용하지 않는다.
- Result를 wire codec으로 검증하고 generation·연속 높이·parent·digest에 묶는다. 정확한 중복만 재사용한다.
- 같은 directory의 temporary file에 쓰고 file fsync→rename→directory fsync를 완료한 뒤 전송 대상으로 공개한다.
- frame·result 길이를 검증한 뒤 할당한다. 전송을 위해 읽을 때 전체 digest와 Result/reference를 대조한다.
- 전체 바이트·파일 수에 상한을 두며 pending뿐 아니라 metadata·임시·격리 파일도 사용량으로 센다. 상한을 피해 미확인 결과를 버리지 않는다.

## ACK와 복구

`Committed`의 chain·generation·height·hash·result digest가 일치할 때만 정리한다. Hello cursor만으로 결과를 삭제하지 않는다. ACK 유실 뒤에는 결과를 다시 보내고 indexer가 durable digest를 확인해 정확한 ACK를 다시 보내는 방식으로 연결한다.

ACK receipt를 먼저 fsync·seal하여 durable 증거로 남기고 결과를 삭제한다. 결과 삭제 directory sync와 checkpoint manifest 갱신을 마친 뒤 ACK receipt를 정리한다. 중간에 종료되면 receipt를 검증해 checkpoint와 삭제를 이어간다. 증거가 없는 파일을 삭제하지 않는다. 잘못된·앞선·다른 generation의 ACK는 checkpoint를 움직이지 않는다.

정확한 재전송은 멱등이다. 같은 reference에 다른 digest, 높이 gap, 잘못된 parent는 오류다. 알려진 미완료 임시 파일은 삭제하지 않고 quota 안에 보존·격리하며 원본 노드에서 재실행할 수 있다. 외부·손상된 sealed file은 fail-closed로 보존한다. Open은 chain/anchor identity 확인 전에 복구 쓰기를 수행하지 않는다.

새 generation으로 spool을 되감는 기능은 U08/U09의 durable rewind 확인과 연결한다. U03에서 Hello를 무조건 신뢰해 이전 generation 결과를 버리는 경로를 만들지 않는다.

## 검사

API형 stub으로 durable append/읽기, 정확한 ACK, ACK 유실 후 reopen, fsync·rename·delete 실패, 잘림·손상, quota, identity·lock·symlink와 취소를 RED로 고정한다. 각 장애 지점에서 원본이나 ACK 증거가 남는지 확인한다. 모든 파일은 임시 시험 directory에만 만든다. 새 동시성 검사는 GOMAXPROCS=1/race/단일200회로 완료한다.

구현 파일은 `x/stablenet/executor/spool/`이다. ACK 단계별10개 장애 복구, 손상·잘림·identity·forward ACK, 대기 취소·Close join 검사가 통과했다. ACK된 Append의 Size/이력 멱등과 초기 identity 재지정·알 수 없는 임시 파일은 추가 RED 후 수정했다. 초기 identity 파일이 미완료면 자동 재초기화하지 않는다. 단일 GOMAXPROCS=1/race/200회 동시성 검사가 통과했다.

ACK receipt 저장 실패 뒤에는 reopen이 필요하다. ACK 및 receipt 복구도 metadata reserve와 전체 retained 파일 budget을 통과해야 한다. 격리 자료가 예산을 소진하면 자동 삭제 대신 ErrFull로 중단한다. 이 두 경계는 추가 RED 후 전체 모듈 race/lint로 GREEN을 확인했다.
