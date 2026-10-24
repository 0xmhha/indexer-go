# U09 IPC·재전송 계약

상태: U09 기능 RED/GREEN과 focused race를 완료했다. 앱/node 조립(U10)과 실제 EVM→DB/API 전체 연결(U11), 최종 200회·필수 전체 회귀·통합 PR/CI는 아직 남아 있다. 실제 노드·peer·성능 측정·노드 DB GC는 보류한다.

spool v1을 읽을 수 있으며 generation 전환 뒤 manifest v2로 original anchor와 active base를 분리한다. 새 base는 기존 durable ACK 높이 이하이고 원래 anchor 이상이어야 한다. 호출자가 canonical hash를 확인한다. Hello는 forward ACK가 아니다.

전환 journal을 file fsync→rename→directory fsync로 기록한 뒤 구 generation의 미확인 파일을 격리하고 새 base를 저장한다. 마지막으로 journal을 정리한다. journal·격리·새 manifest·정리 단계의 fault 뒤 reopen이 전환을 멱등 재개한다. 미확인 결과를 삭제하지 않는다.

격리 원본은 새 generation으로 재인코딩하여 canonical hash가 같은 결과만 재사용한다. 같은 hash에 상충하는 내용은 거부한다. 격리 파일도 전체 byte/file quota에 포함한다. 공간이 없으면 중단하며 자동 삭제하지 않는다. 이전 generation의 ACK는 거부한다. 확인된 새 generation의 정확한 ACK만 active 결과를 정리한다.

검사: `go test -race ./spool`의 TestGenerationRebindPreservesUnackedCanonicalResults, TestGenerationRebindCannotPromoteHelloOrExceedRetainedQuota, TestGenerationRebindRecoversEveryDurableBoundary. fault 검사는 journal sync/rename/dir sync, 격리 rename, manifest sync/rename, journal 정리를 각각 실제로 실패시킨다.

송신자는 After(sender cursor)로 여러 durable 결과를 연속 전송할 수 있다. Tail은 수집 끝점이며 공개 ACK가 아니다. 순회는 ACK나 삭제를 수행하지 않으며 잘못된 sender hash를 거부한다. 실제 in-flight 수는 exporter의 별도 bounded window가 제한한다.

DB 재확인은 현재 generation과 공개 높이를 잠금으로 고정한 뒤 해당 높이의 공개 증거가 하나인지 확인한다. payload를 원래 증거 generation으로 정규화한 전체 SHA와 hash/parent/root가 일치해야 한다. fresh RPC 뒤 generation/visible/root를 다시 확인하고 transaction을 종료한다. 기능 표·outbox·공개 높이를 다시 쓰지 않는다. 미공개 높이는 확인 성공이 아니며 새 queue 작업으로 처리한다. marker·stale generation·내용 충돌·fresh 실패는 ACK하지 않는다.

수신 세션은 client Hello의 chain을 확인한 뒤 controller/공개 높이로 server Hello를 보낸다. bounded assembler가 완성한 payload를 profile로 검증하고 reference/digest를 대조한다. 새 결과는 bounded queue에, 공개 prefix는 정확한 확인 경로에 전달한다. 별도 ordered ACK worker는 publication Future 성공 뒤에만 보낸다. 잘못된 chain/reference·불완전 조각·취소는 ACK하지 않는다. reader/ACK/controller/취소 callback을 join한다. 실제 Unix peer 인증은 공개 listener에서 세션 진입 전에 적용한다.

Unix listener는 이미 있는 경로를 덮어쓰지 않는다. 소유한 0700 parent와 생성한 0600 socket을 사용하고 macOS LOCAL_PEERCRED/Linux SO_PEERCRED의 실제 상대 UID를 확인한다. 종료 시 inode가 같은 소유 socket만 정리하며 교체된 파일은 보존한다. 취소 callback·세션·controller를 join한다. 의미 오류는 중단하고, EOF·timeout·generation 전환은 재연결을 허용한다.

수신 pipeline 검사에서 첫 publication이 대기해도 다음 블록 parts가 진행되고 ACK는 높이 순서대로 오는 것을 확인했다. 잃어버린 ACK의 prefix 확인은 core/feature를 다시 적용하지 않는다. collector 범위 순회도 RED3 → 같은 exporter race GREEN을 완료했다. head 이벤트를 블록 하나로 해석하지 않고 durable tail 다음부터 canonical 범위를 걷는다. replay 전후 canonical hash를 확인하고 fsync spool 뒤에만 tail을 전진한다. retained canonical 결과가 있으면 부모 재실행 없이 재사용하며 missing parent·quota 오류는 그대로 중단한다.

Hello는 DB 공개 지점의 정보만 교환한다. 높이 차이만으로 ACK 또는 rewind를 추론하지 않는다. 명시적 Rewind는 현재 node canonical reference를 포함하고, 공개 RPC HashAt가 그 hash와 합치기 전에 generation을 전환하지 않는다. RPC 지연/실패는 원인을 가진 retryable canonical-lag 오류이며 원본을 보존한다. 이미 fresh가 검출한 publication 불일치는 기존 복구 경로로 처리한다. DB 복원이 source보다 과거 generation을 복원한 경우 자동 재사용하지 않고 E2의 명시적 복구가 필요하다.

송신 window 검사는 RED2 뒤 구현했다. sender와 ACK reader는 별도 worker이며 window만큼 여러 블록을 먼저 보낼 수 있다. ACK가 오기 전에는 원본을 삭제하지 않는다. 다음 slot은 정확한 ACK를 spool에 영속 반영한 뒤 반환한다. 잘못된/다른 순서 ACK·연결 단절·취소는 원본을 유지하고 두 worker와 IO 취소 callback을 join한다. 이 helper는 Client.Run의 실제 handshake/collector 재연결 수명에 연결되어 있다.

Client.Run은 첫 유효한 indexer Hello 뒤에만 OnReady를 호출한다. callback은 한 Run에서 한 번이며 재연결 때 node를 다시 시작하지 않는다. collector는 연결 단절·handshake 대기에도 유지되어 quota 안에서 durable 결과를 모은다. 재연결은 local durable ACK 다음부터 보내며, server Hello가 더 높아도 원본을 삭제하지 않는다. 상충하는 canonical reference는 명시적 Rewind를 요청하고 server generation이 실제로 전환된 뒤 collector를 join·rebind·재개한다.

송신 직전에도 local canonical hash를 확인한다. 낮아진 node head는 spool의 알려진 prefix/미확인 reference와 대조한다. 같은 prefix면 catch-up을 기다리고 상충하거나 확인할 수 없으면 canonical 복구를 요청한다. 재시작 handshake에서도 이 검사를 먼저 수행해 사라진 old tail을 무한 대기하지 않는다.

quota·부모 상태·잘못된 ACK 등 의미 오류는 owner를 취소하고 collector·송수신·IO callback을 join한 뒤 원인을 반환한다. node assembly는 반환 시 자신이 소유한 node를 닫아야 한다. spool은 호출자 소유이며 미확인 원본을 유지한다. 유휴 ACK reader도 연결을 읽으므로 데이터가 없을 때 peer 종료를 즉시 감지한다. exporter package 전체에 goleak 검사를 적용한다.

검사: collector RED3·transfer RED2·client lifetime RED3, 실제 유휴 EOF 누락 RED·short-head prefix RED2·재시작 short reorg RED1을 수정해 exporter/spool 전체 race GREEN을 확인했다. native Unix UID·unsafe path, replay/quota의 owner 종료, concurrent Run 거부를 검사했다. Root receiver의 Hello 추론 거부·RPC view 확인 전 generation 전환 금지 RED2와 fetch canonical-lag RED1도 같은 race GREEN이다. 200회와 전체 연결 검사는 마지막 통합 단위에서 완료한다.

가득 찬 spool의 정상 재시작은 기존 tail의 정확한 ACK를 먼저 완료한 뒤 node import·새 capture를 시작한다. 그렇지 않으면 재시작 직후 quota로 재차 중단될 수 있어 RED 후 수정했다. 저비용 durable Checkpoint 조회로 drain을 기다리고 OnReady는 once/atomic으로 중복 실행을 막는다. 보존 backlog보다 node header가 뒤처져 로컬 canonical 확인을 할 수 없으면 verified checkpoint recovery 오류로 중단한다. 이를 blind SetHead나 임의 삭제로 숨기지 않는다. orphan 격리 자료가 quota를 소진한 경우 자동 삭제하지 않으며 E3의 별도 정리 정책 또는 운영자 용량 조정이 필요하다.

수신 socket의 공개 경로는 bind 직후 노출하지 않는다. 같은 private 부모의 임시 디렉터리에서 listen·권한0600을 완료한 뒤 macOS/Linux의 no-replace rename으로 원자적으로 공개한다. 준비 중 권한/연결 상태를 producer가 잘못 관측하지 않으며, 공개 직전 다른 파일이 생겨도 덮어쓰지 않는다. 해당 공개 전 노출 RED와 실제 연결/소유 경로 보존 GREEN을 확인했다.
