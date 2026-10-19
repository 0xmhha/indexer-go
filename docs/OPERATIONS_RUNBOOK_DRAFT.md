# Checkpoint·재시작·GC 운영 절차 초안

**상태: 절차 초안. 디스크 포맷·초기화·실제 서비스 실행·삭제·장애 시험·추가 성능 측정은 보류한다.**

[운영 설정](OPERATIONS_DRAFT.md), [Docker 구성](../deployments/testnets/README.md), [용량·backlog 정책](OPERATIONS_CAPACITY_DRAFT.md)을 적용할 때 사용할 절차다. 이 문서는 실행 승인이나 새 운영 도구가 아니다. 체인별 DB pool4개(체인별 App이1개일 때16개), 체인별 공유 RPC5 HTTP/s, source와 writer 각각의 커밋 직전 fresh·응답 후 epoch·transaction fence, 내구성/ACK/최소24h 삭제 조건을 유지한다. 다른 프로젝트·기존 DB·디스크를 정리하거나 별도 full node를 시작하지 않는다.

현재 운영 초안 YAML의 `initialize_volume/create_window/collect`는 모두false이고 [호스트 검사기](../deployments/testnets/preflight.py)도 이를 강제한다. **`collect: false`는 새 GC 수거를 막는 설정이다. 이미 durable하게 승인된 삭제는 `OpenWindow`가 재시작 중 마칠 수 있다.** 앱 생성/DB open/window open은 읽기 전용 점검이 아니다. 실행 보류 중에는 이를 상태 조회 용도로 호출하지 않는다.

## 1. Checkpoint 기록과 변경 경계

여기서 checkpoint는 블록 높이·hash와 영구 진행/복구 정보를 뜻한다. PostgreSQL의 SQL `CHECKPOINT` 명령을 추가하거나 RAM 큐를 강제로 완료 처리하는 절차가 아니다. 운영 기록은 다음 항목을 같은 사건/변경 ID 아래 묶되 실제 저장 위치를 대체하지 않는다.

| 기록 항목 | 기록할 값과 근거 |
| --- | --- |
| 관측 구분 | 시각·timezone, 운영자/사건 ID, 기동 전·실행 중·종료 요청 전·종료 후 중 어느 관측인지. 요청 전 표본을 최종 commit이라고 이름 붙이지 않음 |
| 실행·설정 | 코드 revision/검토한 이미지 digest, 비밀을 제외한 설정 digest, 네 chain ID·각 DB/schema·feature 정의. DSN/비밀번호/peer private key 값은 기록하지 않음 |
| Arc 최초 anchor | 최초 `H0/hash`, `start_height=H0+1`, chain/genesis·network/fork·plan digest·projection consumer. 재시작마다 최신 anchor로 바꾸지 않음 |
| Arc source | DB generation `G`, 연속 완료 `source.factsThrough=C/hash`, source에 저장된 `finalized=F/hash`, 고정 finality policy/authority. observed header/latest 후보를 완료점으로 쓰지 않음 |
| writer·feature | `projectionHead=P/hash`, `projectionMatchesCapture`, 활성 필수 unit의 epoch·정의·`completeThrough/gap/status`, 설정된 worker의 개별 완료점. 최대 적용 높이 하나로 통합하지 않음 |
| 나머지3체인 | 각 저장소의 durable writer cursor/hash·활성 feature 완료/누락·오류. Arc의 generation/anchor/GC를 적용하지 않음. 일반 RPC 모드 status가 unreported이면 완료값을 만들어내지 않음 |
| 외장 원본 | 호스트 APFS VolumeUUID·physical-store UUID, 논리 volume ID, root/path/reservation·raw StoreID·용량/retention·DB binding. 물리 UUID와 논리 ID를 구분 |
| 삭제 복구 | DB `PendingDelete` 유무, 해당 token/generation·정확한 StoreID/segment·reference digest/개수, `.gc` 또는 `GC.completed.json`의 일치 여부. 요약 기록만으로 삭제를 새로 승인하지 않음 |
| 종료 결과 | exit/timeout/강제 종료·Close/DB/RPC 오류·남은 진행 차이·복구 필요 항목. checkpoint 조회 실패와 null은 unknown으로 보존 |

Arc 진행은 `/chains/arc-testnet/indexing/status`의 DB-only snapshot을 참고하고 `next` pagination의 필수 unit을 빠뜨리지 않는다. source와 projection은 별도 DB snapshot이므로 generation/canonical 모순이 보이면 다시 확인할 대상으로 기록한다. `projectionMatchesCapture: true`도 모든 feature의 gap-free 완료나 삭제 승인이 아니다. projection receipt는 역사적 처리 ACK이며 raw ACK·연속 cursor·GC 허가가 아니다.

현재 HTTP status는 삭제 조건 전부를 노출하지 않는다. 별도 [읽기 전용 진단 명령](OPERATIONS_INSPECTION.md)은 source/writer/feature 진행·DB/window binding·pending 삭제와 정확한 raw intent/완료 기록·범위를 명시한 raw ACK를 조회한다. 앱/window writer를 열거나 삭제 복구를 호출하지 않는다. live lease는 unknown, payload/proof integrity는 not_checked이며 완전한 GC 승인 dry-run은 아니다. 실제 DB/외장 디스크 실행 검증은 보류다. DB 필드를 SQL로 수정하거나 startup을 조회 도구로 사용하지 않는다.

## 2. 최초 anchor 확정 절차 — 적용은 보류

1. APFS·용량·전용 DB/schema·private 설정·운영 이미지와 Arc EL enode/fork를 확정한다. 현재 enode/실제 peer fork/APFS UUID·시작 높이는 미확정이다. 확인된 genesis/network와 공개 소스 fork 후보의 구분은 [Arc identity 기록](analysis/arc-testnet-identity.md)을 따른다.
2. 실제 적용 직전에 기존 공유5 HTTP/s 경로로 Arc finalized `H0/hash`와 chain/genesis·canonical 일치를 검증한다. checkpoint를 기록하고 `anchor_height/hash`와 Arc `start_height=H0+1`을 함께 고정한다. 관측 시각이 오래되거나 검증에 실패하면 미확정 상태를 유지한다. `latest/safe`나 임의 confirmation 수로 finalized를 대체하지 않는다.
3. 나머지3체인은 각 시작 높이와 strict 기능에 필요한 부모 상태 가용성을 확인한다. 지금 `config.yaml`의 과거 높이를 새 DB 시작점으로 복사하지 않는다. pruned 상태를 최신값/0으로 바꾸지 않는다.
4. 새 빈 저장소의 최초 생성은 별도 승인된 provisioning 절차로만 수행한다. 현재 호스트 검사는 초기화 flagtrue를 거부하므로 이 초안으로 최초 생성하지 않는다. 기존 source DB는 같은 plan/anchor/소비자 집합으로만 재사용하며 변경은 별도 이전/재구성 대상이다.
5. 최초 binding 이후에는 같은 DB/schema·window·identity·anchor와 초기화 flagfalse로 유지한다. 생성 중 window만 남고 DB binding 기록 전 실패한 경우도 자동 재생성하지 않고 원인을 확인해 기존 저장소를 여는 분기로 판단한다.

anchor 자체의 원본이 수집됐다는 뜻은 아니다. anchor 이전의 feature backfill/깊은 rollback이 필요하면 현재 source 범위에 없는 입력을 읽을 수 있다고 가정하지 않는다.

## 3. 정상 종료

다음은 실행 보류 해제 후의 순서다. 이 문서 작성 중 signal·stop·SQL을 실행하지 않는다.

1. 계획 중단 사유·대상 project `indexer-testnets-draft`와 indexer 서비스를 기록한다. 마지막 관측 `C/P/필수 unit`, 용량·오류·진행 차이를 남긴다. 처리량을 측정하거나 backlog가0이 될 때까지 무기한 기다리지 않는다. 전용 drain/pause 운영 API는 현재 없다.
2. **이 프로젝트 indexer 전체에 SIGTERM**을 전달해 manager와 모든 체인의 취소를 요청한다. 체인만 오류 상태로 남겨 내부 auto-restart가 반복되게 하지 않는다. 다른 프로젝트의 서비스/프로세스는 중단 대상이 아니다.
3. 수신·capture·writer·relay·선택 task가 반환하고 하위 goroutine/lease가 정리되는지 확인한다. feed 세션은 관련 task를 join한 뒤 raw window를 Close하고, 앱은 pipeline 수명 종료 후 DB/client를 닫는다. 진행 중 transaction은 성공한 durable commit 또는 취소/rollback 결과로 판정한다. 원본 fsync/채널 알림만으로 source/writer ACK를 남기지 않는다.
4. process 종료와 Close/DB/독립 worker 오류·timeout 여부를 함께 기록한다. `stopped` 한 줄이나 정상 exit만으로 무오류 종료·backlog 완료를 선언하지 않는다. 마지막 완성된 raw/facts와 DB cursor는 남겨 재시작에 사용한다. 종료 직전 API가 사라졌으면 이전 표본을 최종 높이로 승격하지 않는다.
5. DB도 중단해야 하는 경우에만 indexer 종료 뒤 **이 프로젝트 PostgreSQL의 정상 종료 경로**를 사용하고 DB 종료 결과를 확인한다. indexer만 재시작하는 경우 DB를 유지할 수 있다. 종료 모드는 [PostgreSQL16 공식 설명](https://www.postgresql.org/docs/16/server-shutdown.html)을 따른다.
6. 제안 유예는 indexer120초/DB60초지만 현재 Compose에는 미적용이다. 시간 내 join/종료되지 않으면 정상 종료로 기록하지 않는다. 강제 종료가 필요했던 긴급 사건은 아래 장애 복구 대상으로 넘긴다. volume 삭제·광범위 prune·PID/LOCK 파일 수동 제거로 종료를 대신하지 않는다.

정상 종료는 수신된 모든 블록의 후처리 완료를 요구하지 않는다. 반대로 DB에 남은 완료점 이상을 운영 기록으로 올려서도 안 된다. 이 초안은 종료 상태를 영구 기록하거나 timeout을 강제하는 새 controller를 구현하지 않는다.

## 4. 재시작 전 확인과 진행 복구

| 단계 | 확인·동작 | 중단을 유지할 조건 |
| --- | --- | --- |
| 호스트 | 같은 외장 APFS mount/UUID·ownership·전용 경로·private 파일을 확인하고 [용량 정책](OPERATIONS_CAPACITY_DRAFT.md)의 재개선을 적용 | 다른 디스크/일반 폴더·낮은 여유·원인 미해소·관측 불가. 호스트 검사기의100GiB 최소선 통과가 정책의150GiB 초과 재개 승인과 같지 않음 |
| 설정·실행 | 같은 chain/plan/anchor·소비자·finality authority·DB/schema·window binding과 검토한 이미지, DB4/RPC5·strict 정책을 대조 | binding/정책 변경·미확정 enode/fork/checkpoint·필수 상태 계약 누락. flag를true로 바꾸거나 ledger를 지워 우회하지 않음 |
| DB | PostgreSQL이 정상 접속/복구 가능하고 기존 durable 기록과 내구성 설정이 유지되는지 확인 | DB 손상/복구 실패·원인 미확인 commit 오류·다른 시점의 source/window 조합 |
| pending 삭제 | 정확한 DB intent와 raw 기록을 먼저 확인. 승인된 복구 분기에서만 앱의 coordinated `OpenWindow` 경로 사용 | intent/reference/완료 기록 불일치·검증 불가. 이 단계의 Open은 삭제/복구 쓰기를 할 수 있으므로 읽기 전용 검사가 아님 |
| raw 복구 | 같은 window를 Open해 마지막 미완성 tail과 승인된 삭제를 복구하고 관련 오류를 확인 | 누락 원본 재생성 금지. 완성 frame/index checksum 오류·다른 StoreID·lease/lock/장부 오류는 중단 |
| source·writer | 앱은 기존 DB `C`와 generation에서 이어가며 writer는 자신의 durable 진행점에서 facts를 검증해 재개. 새 source/writer commit마다 기존 fresh/epoch 검사를 유지 | 미수집/손상 facts·stale 정의/epoch·bootstrap/undo 범위 밖의 복구 요구. 처음부터 최신 anchor로 점프하지 않음 |
| 재개 판정 | 정상 운영 감시에서 현재 generation의 durable 진전과 용량·오류를 확인. backlog는 그대로 보존 | 같은 장애 재발·예상과 다른 삭제·오류/관측 누락. 반복 restart로 정상 판정을 만들지 않음 |

원본 Open은 마지막 **미완성** frame을 정리할 수 있지만 완성 자료의 checksum 손상을 빈 tail로 처리하지 않는다. RAM 큐의 미commit 작업은 다시 수신/검증할 수 있어야 한다. peer가 누락 구간의 header/body/receipt를 보유하지 않거나 RPC 체인의 부모 상태가 이미 pruned됐으면 복구 가능성을 확정할 수 없다.24h 로컬 원본은 원격 자료24h 보존 보장이 아니다.

writer commit 직후 projection receipt 저장 전에 중단됐다면 기존 복구가 마지막 durable block을 다시 검증하고 ACK를 보완한다. feature/outbox를 두 번 적용하거나 raw ACK를 대신 기록하지 않는다. 단순히 ACK 기록이 없다는 이유로 source/projection DB cursor를 되돌리지 않는다.

## 5. 장애별 분기

| 장애 | 복구 판단 |
| --- | --- |
| 프로세스 강제 종료/호스트 재부팅 | 동일 저장소를 보존하고 DB 복구·window tail·pending intent·cursor를 순서대로 확인. 원래 anchor와 durable 진행점으로 재개 |
| raw quota/디스크 부족 | 기존 수신 역압력과 [capacity 중단·재개 기준](OPERATIONS_CAPACITY_DRAFT.md)을 따른다. GC=false에서는 worker가 끝나도 원본 공간이 줄지 않음. 미승인 삭제로 회수하지 않음 |
| 장치 제거/UUID·mount 변경 | 해당 indexer 전체와 필요 시 전용 DB를 중단 대상으로 기록하고 자동 재기동을 억제. 재연결 후 새 handle로 동일 identity를 확인. 내부 디스크 fallback/없는 원본 Create 금지 |
| RPC/peer 일시 장애 | 오류와 남은 durable 범위를 보존. 알려진 일반 통신 장애와 canonical 불일치를 구분하고 기존 limiter/retry 범위로만 재개. 별도 client로 요청 예산을 늘리지 않음 |
| 일반 captured suffix reorg | runtime의 bounded 공통 조상 조회→해당 epoch task 취소/join/Close→공통 조상 재확인·transaction fresh/Reset→새 generation 재개를 사용. writer도 기존 undo/outbox와 epoch 교체 규칙을 따름 |
| finalized 충돌·bootstrap/undo 밖·재개 한도 초과 | `ErrFinalityConflict/ErrRebuildRequired` 등을 보존해 자동 복구 중단. 별도 원인 조사와 새 저장소/명시적 범위의 재구성 계획이 필요. 기존 DB/원본은 보존 |
| checksum·plan·binding·필수 입력 불일치 | 손상/불일치 증거를 보존하고 재개하지 않음. empty/0/latest 대체·수동 cursor/fence 변경·필수 소비자 삭제로 통과시키지 않음 |

manager의 `auto_restart: true`는 용량/identity 사고의 재개 승인 기능이 아니다. 현재 별도 원인별 재시작 억제·영구 중단 상태·host watchdog은 미구현이다. 운영 적용 전에 이 공백을 해결해야 하며, 문서로 해결됐다고 표시하지 않는다. 독립 CL certificate 검증·orphans 자동 수거·무제한 undo도 현재 운영 경로에 포함하지 않는다.

`max_unhealthy_duration`의5m 값은 실제 중단 timer로 사용되지 않으므로 운영 초안에서 제외했다. worker 적체5분 경보와 같다고 해석하지 않는다. Docker 로그 회전과 읽기 전용 metadata 진단 도구는 있지만 capacity controller·종료 유예·실제 payload/proof/lease까지 검증하는 GC 승인 진단은 여전히 미구현이다.

`ChainInstance.Stop`은 timeout 때 이전 pipeline이 끝난 뒤 Close하도록 지연할 수 있다. 따라서 StatusStopped/Stop 반환만으로 이전 pool이 모두 반납됐다고 보지 않고 프로세스·자원 종료를 확인한 후 재기동한다. 체인별4개/정상 합계16개와 RPC5는 각 live App의 pool 예산이며 중복 App까지 묶는 전역 상한이 아니다. 이번에 중복 실행을 재현하거나 실제 예산 초과를 측정하지 않았다.

## 6. 새 원본 GC 전 확인 조건

아래 항목이 모두 충족될 때만 **정상 runtime/coordinator 경로의 새 삭제 후보**를 검토한다. 운영자가 체크한 표나 API snapshot은 실제 삭제 직전 DB guard를 대신하지 못한다.

| 확인 조건 | 실제 강제/확인 지점 |
| --- | --- |
| 정책·적용 범위 | Arc의 고정 trusted-RPC-finalized authority/ID가 DB와 일치. 이번 대상은 Arc raw window이며 영구 source facts·다른3체인 DB는 삭제 대상 아님 |
| 시간·segment | 정상 시계, sealed segment, segment의 가장 늦은 `FirstReceived`부터 최소24h 경과. active segment는 바로 삭제하지 않으며 중복/재시작으로 수신 시각을 갱신하지 않음 |
| 저장소 identity | 같은 volume/ledger/StoreID·정확한 segment/reference 집합. mount/lock/경로/검증 오류가 없어야 함 |
| 필수 원천 | 현재 plan의 encoded bytes·digest/codec/길이·정규화 rows와 필요한 영구 객체가 durable DB/저장소에서 재검증돼야 함. manifest/coverage 이름만 확인해서 승인하지 않음 |
| canonical·finality | 현 generation의 canonical 자료이며 검증된 finalized prefix 이내. finalized 부재·역행/충돌·이전 fork는 삭제 승인하지 않음 |
| 필수 추가 계약 | 기존 DB에 required supplement/부모 checkpoint 계약이 있다면 완료·원천 바이트까지 재검증. 현재 base plan에 proof/state worker를 새로 켜는 절차가 아님 |
| raw ACK·lease | 등록된 모든 raw-only consumer의 실제 durable `AckRaw`, 반환된 lease. writer head/projection receipt/계측 percentile을 raw ACK로 대체하지 않음 |
| 삭제 직전 fence | DB가 정확한 token/generation/reference의 삭제 intent를 durable하게 commit한 뒤에만 동기 삭제 callback. commit 실패/성공 불확실이면 새 삭제를 시작하지 않음 |
| 삭제 완료 | `.gc` intent·파일 unlink·directory fsync·정확한 완료 기록·DB fence 해제의 일치. 오류·중단은 fence를 유지하고 아래 복구로 넘김 |

현재 [제어 worker](../pkg/capture/control.go)는 정상 finality policy tick에서 `SealExpired/Collect`를 사용하고, [DB guard](../pkg/capture/gc.go)가 필수 facts/ACK를 transaction에서 재검증한다. [window GC](../pkg/window/gc.go)는 후보 선택 후 생긴 lease도 삭제 직전에 다시 확인한다. 정상 보관 대기(`ErrRetain`)와 독립 I/O/DB/손상 오류를 합쳐 같은 성공으로 처리하지 않는다.

finalized 조회가 null이면 새 GC를 보류한다. unsupported/malformed/통신 오류나 finalized 충돌은 해당 오류로 처리하고 latest/safe/depth로 대체하지 않는다. policy는 공개 RPC의 finalized를 신뢰하는 모드이며 독립 합의 증명을 검증한 것은 아니다. canonical과 이전 fork가 섞인 segment도 전체 참조가 통과해야 하므로 부분만 수동 삭제하지 않는다.

**향후 새 GC 활성화 순서:** 실행/삭제 보류 해제와 정책 승인을 받은 뒤, 먼저 GC=false 상태의 저장·종료·재시작·누락 거부·삭제 gate 검증 결과를 확인한다. 이어 host guard가 허용할 정확한 GC 전환 조건과 관측/실패 복구 도구를 별도 변경·검증하고, 같은 authority/plan/binding을 유지한 제한된 적용을 계획한다. 현재 검사기는 `collect: true`를 거부하므로 YAML 한 줄 변경이나 직접 Compose 실행으로 우회하지 않는다. 이 초안에서는 flag 변경·새 GC·실제 삭제를 전혀 수행하지 않았다.

## 7. 중단된 삭제의 복구 — GC=false여도 별도 확인

| 발견된 상태 | coordinated 복구 동작/거부 조건 |
| --- | --- |
| DB fence/intent 있음, segment가 그대로 있음 | 동일 token/generation/reference와 필수 facts를 재검증한 후 정확한 기존 삭제를 완료할 수 있음. 과거 허가를 새 segment에 재사용하지 않음 |
| DB intent와 일치하는 `.gc`, 일부 파일 삭제됨 | `OpenChecked`가 DB 승인을 먼저 확인하고 나머지 삭제를 마침. 다른 store/segment·intent 누락/변조는 거부 |
| raw 삭제 완료, DB fence 남음 | 정확한 `GC.completed.json`의 StoreID/segment/reference digest와 파일 집합 부재를 함께 확인한 뒤 같은 DB fence를 해제 |
| 파일만 없거나 intent/완료 기록 불일치 | 완료로 추정하지 않고 중단. TTL/PID/수동 DB UPDATE·`.gc`/LOCK/ledger 삭제로 fence를 지우지 않음 |

복구는 단일 writer lock/GC 직렬화 안에서 [DB intent 재검증](../pkg/capture/recovery.go)과 [raw 완료 기록 대조](../pkg/window/gc.go)를 연결하고 **수집/처리 task 시작 전에** 실행한다. 필요한 기록을 모르는 상태라면 startup으로 확인하려 하지 말고 읽기 전용 진단·일관된 증거 보존을 먼저 준비한다. `collect: false` 전환은 기존 허가의 취소나 삭제 복구의 read-only 모드가 아니다. 이전 fork 자료는 이 절차로 새 삭제 권한을 얻지 않는다.

## 8. 백업·이전과 완료 기록

장치 손실/재구성이 필요할 때는 같은 시점의 PostgreSQL 전체 cluster/WAL·raw/삭제 기록·marker/ledger·설정/identity의 연관을 보존해야 한다. 비밀 파일은 접근 제한된 별도 보관 정책을 사용한다. SQL dump만으로 raw 삭제 intent·filesystem identity까지 복원됐다고 간주하지 않는다. 일반 파일 복사 백업은 indexer를 멈추고 PostgreSQL까지 정상 종료한 일관된 상태를 기준으로 계획하며, 온라인 snapshot/base backup은 별도 설계·검증 대상이다. [PostgreSQL16 파일 백업 조건](https://www.postgresql.org/docs/16/backup-file.html)을 따른다.

복원은 원본을 덮어쓰지 않는 격리된 대상에서 검증하고 단일 writer와 DB/window binding을 확인하는 안이다. 새 디스크 UUID·경로·quota의 변경과 backup 정책/RPO/RTO는 별도 확정이 필요하다. 읽기 전용 metadata CLI는 모의 데이터로 검증했지만 완전한 GC 승인 dry-run·사건별 중단/재개 controller, 종료 유예, 실제 복원 검증은 아직 미구현/미검증이다.24h raw는 백업·24h undo·원격 상태 보존 보장이 아니다.

운영 기록의 완료 표시는 **정상 종료 / 복구 확인 후 재개 / 관측·복구 미확인으로 중단 유지** 중 하나로 남긴다. 실제 승인된 삭제가 있었다면 대상 reference digest/segment·DB fence/완료 결과도 연결한다. 프로세스 재실행·높이 증가·API health만으로 데이터 무결성/삭제 완료를 선언하지 않는다.

이번 작업은 [앱 lifecycle](../pkg/app/managed_feed.go), [source 초기화](../pkg/capture/store.go), [session](../pkg/capture/session.go), [runtime](../pkg/capture/runtime.go), [projection ACK](../pkg/source/facts/source.go)와 기존 문서를 대조한 정적 검토다. 관련 [앱 검사](../pkg/app/managed_feed_test.go)·[삭제 복구 검사](../pkg/capture/recovery_test.go)·[window 복구 검사](../pkg/window/recovery_test.go)는 근거 경로로 확인했으며 이번에 다시 실행하지 않았다. 실제 checkpoint 값·host/peer/DB 상태를 조회하거나 운영 절차를 실행한 결과가 아니다.
