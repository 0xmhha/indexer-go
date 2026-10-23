# U06: atomic execution publication

현재 U04~U08 통합 브랜치의 기능 검사 결과다. 앱의 실행 모드 선택·IPC 연결은 U09/U10이다.

`CommitExecution`은 기존 pool4의 transaction 하나를 사용한다. shared advisory fence와 control row lock으로 generation·rewind를 검사하고, 같은 ref/parent/digest의 7개 durable part와 저장된 block/root를 확인한다. 명시적 anchor baseline이 없으면 거절한다.

후보 공개 높이는 transaction 내부에서만 전진한다. 기존 feature writer가 core 표를 다시 쓰지 않고 metadata/features/events를 처리한다. feature별 definition/ref/digest checkpoint는 `/meta/execution/units/`에 저장한다. 기존 facts checkpoint 형식을 실행 결과의 증명인 것처럼 사용하지 않는다.

feature checkpoint·outbox·latest/public height·완료 증명·undo를 모두 보내고 오류를 확인한 다음 fresh callback을 실행한다. 그 후에는 generation/후보 hash fence SELECT와 실제 COMMIT만 수행한다. 기존 blockTx.Commit의 뒤늦은 flush/undo 준비를 호출하지 않는다. fresh 실패/취소/SQL 오류는 모두 rollback되며 ACK하지 않는다.

`Fetcher.PublishExecution`은 기존 single feature writer에서 실행한다. 표별 저장 때문에 block이 이미 있어도 feature를 생략하지 않는다. core block/tx/receipt/log 중복 쓰기를 하지 않는다. feature 준비는 새 publication의 apply callback에서만 수행하므로 ACK 재시도에 오래된 상태 조회를 요구하지 않는다. publication transaction은 필요시 feature RPC를 기다릴 수 있다. part permit 2개는 이 대기에 사용하지 않는다. 추가 준비 자료에는 기존 state_max_bytes 상한을 적용한다.

fresh reader는 시작 전에 별도로 지정한 uncached RPC source여야 한다. 앱은 기존 체인별 5 physical HTTP/s pool을 주입한다. 이 기능은 별도 RPC client를 만들지 않는다. 원본에 실린 hash나 cache로 fresh를 대신하지 않는다. 대상은 불변 execution ref에 고정하며 feature가 model identity를 바꾸면 공개 전에 거절한다. 관련 추가 RED는 U08 통합에서 수정했다.

완료된 블록의 재전송은 exec_publications의 generation/height/hash/parent/digest/root 전체를 대조하고 새 fresh 검사를 수행한다. feature/outbox는 재실행하지 않는다. CommitExecution 성공 뒤에만 IPC 소유자가 Committed를 보낼 수 있다. ACK 전송 실패는 durable 완료를 취소하지 않으며 같은 증명으로 재시도한다.

검사: 실제 임시 fsync-on PostgreSQL에서 stub RED3 → 동일 검사 GREEN. 7개 미완료·fresh 실패·중복/충돌·visible 원자성·undo 선행을 확인했다. 추가 post-response generation 변경/queued SQL error 검사도 통과했다. feature 경로는 기존 core가 있을 때의 stub RED2 → race GREEN이며 core 중복 쓰기0·feature1회·outbox 중복0·fresh 실패 rollback을 확인했다. 실제 노드·추가 성능 측정 없음. API 전체 read fence는 U07이므로 아직 전체 앱 공개 완료로 보지 않는다.
