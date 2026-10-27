# 용량·backlog·복구 정책 초안

**상태: 정책 제안과 그 일부 구현. 파일시스템·PGDATA·raw 용량의 자동 감시와 중단·재개 잠금, 원인별 자동 재시작 억제를 구현했다(10/20, [구현 범위](#구현된-자동-감시와-중단)). 운영 적용·디스크 포맷·초기화·서비스 실행·추가 성능 측정은 보류한다.**

현재 입력·native COW 여유는 [운영 준비 현황](OPERATIONS_READINESS.md)을 따른다.

2026-10-19의 [운영 설정](OPERATIONS_DRAFT.md)과 [Docker 초안](../deployments/testnets/README.md)을 위한 기준이다. 아래 숫자는 실측 SLO나 디스크 안전 보장이 아니라 최초 운영 검토용 임계값이다. 현재 `config.yaml`·실행 코드·Compose·애플리케이션 YAML을 변경하지 않는다. 체인별 DB pool4개(체인별 App이1개인 정상 실행 예산16개), 공유 RPC5 HTTP/s, source/writer 각각의 커밋 직전 fresh·epoch·내구성·ACK·삭제 fence를 유지한다.

진행 기록·정상 종료·장애 분기와 GC/중단된 삭제의 복구는 [운영 절차 초안](OPERATIONS_RUNBOOK_DRAFT.md)에 정리했다. 새 자동 운영 도구를 제공하는 문서는 아니다.

## 현재 강제되는 조건과 정책으로 추가할 조건

| 구분 | 현재 구현 | 이번 초안에서 정하는 범위 |
| --- | --- | --- |
| raw 한도 | volume160GiB, Arc128GiB, 최소24h, filesystem 여유100GiB; 논리 예산이며 물리 예약 아님 | 여유 공간·증가 추세에 따른 조기 경고와 중단·재개 기준 |
| raw 압력 | `(FileBytes + IndexReserveBytes) / LimitBytes`80% warning·95% critical; 쓸 여유가 없으면 critical | critical을 정상 운영으로 방치하지 않는 대응 기준; 로그 자체는 중단 명령이 아님 |
| filesystem 압력 | 여유100GiB 위 margin이 volume160GiB의20%/5% 이하면 warning/critical | 이 초안에서는 각각 free132GiB/108GiB 이하에 해당. DB를 보호하기 위해 더 일찍 대응 |
| 수신 역압력 | `AppendWait`/`AppendBatchWait`는 용량 부족 시 같은 입력을 대기·재시도; 후처리는 계속 | 미처리 입력 폐기·ACK 위조·retention 축소 없이 원인 해소 |
| DB | PostgreSQL160GiB는 데이터·색인·WAL의 **계획 예산**, 자동 quota는 없음; operations guard의 PGDATA budget 초과 중단은 구현 | PGDATA 전체 증가 감시·원인 분리·예산 소진 전 운영 중단 |
| 삭제 | 현재 `collect: false`; 기본 상태에서 원본 자동 수거 없음 | 공간 부족을 이유로 GC를 켜지 않음. 검증된 GC의 별도 적용 전에는 완료 데이터도 계속 공간 차지 |
| 오류·재시작 | raw identity/I/O 오류는 세션 task 취소/join; 분류된 storage/capacity/integrity는 manager 자동 재시작 억제 | 디스크/무결성 사고 후 재개 점검과 실제 운영 검증이 필요. 현재 호스트 검사는 컨테이너 최초 기동 전 검사이고 manager 내부 재시작에 다시 실행되지 않음 |

코드 근거는 [Usage/용량 대기](../pkg/window/budget.go), [append](../pkg/window/store.go), [세션 수명](../pkg/capture/session.go), [압력 로그](../pkg/capture/config.go), [체인 재시작](../internal/multichain/manager.go)다. marker/ledger의 quota는 Open 때 대조하므로 YAML 숫자만 바꿔 온라인 증설하는 방식은 지원하지 않는다.

## 감시할 값과 수집 범위

아래 주기는 **일부 구현된 운영 감시 제안**이며 이번에 poller·스케줄·추가 DB 연결·RPC 조회를 만들거나 실행하지 않는다. 기록은 이 프로젝트 전용의 bounded 파일/별도 모니터링 저장소에 두고 DSN·peer key·원본 payload는 남기지 않는다. 같은 APFS에 장기 로그/백업을 무제한 저장하지 않는다.

Compose에는 이 프로젝트의 stdout/stderr 로그 회전(`local`,10m×최대3파일/서비스)을 명시했다. 이 로그·이미지·container writable layer는 Docker Desktop 저장 영역을 사용하므로 외장 APFS의 `F`와 별도 용량이다. `preflight.py`의 호스트 점검이 시작과 재개 전에 Docker VM 디스크의 여유를 한 번 읽는다(아래 절). 이 설정은 WAL·DB rows·향후 모니터 기록의 상한이 아니며 기존 container나 daemon 전역 설정에 적용하지 않았다.

| 값 | 제안 주기 | 얻는 곳과 주의점 |
| --- | --- | --- |
| `F`: APFS 가용 bytes, mount/UUID 일치 여부 | 30초 | 호스트의 해당 APFS와 pinned volume/physical-store identity. Docker VM 내부 free나 raw quota의 남은 양으로 대체하지 않음 |
| `Uraw`: (Arc raw 파일+색인 예약)/128GiB | 현재 세션10초; 로그는 severity 변경 때만 | `window.Usage` 기준. `VolumeAllocatedBytes`는 고정 예약 합계이므로 실제 사용량으로 계산하지 않음. 연속 수치 저장/export는 추가 구현 필요 |
| `Dpg`: PGDATA의 실제 할당량 추정 | 5분 | 이 프로젝트의 `indexer/postgres/16`만, 다른 filesystem을 따라가지 않는 bounded 검사. WAL·임시/공유 파일 포함. APFS snapshot/clone/metadata와 filesystem free가 정확히 대응하지 않을 수 있음 |
| `Ddb`: 전용 DB의 크기 | 60초 | `pg_database_size` 값. raw·cluster WAL·filesystem 전체 사용량을 포함하는 값으로 취급하지 않음 |
| `Dwal`: 보유 WAL 파일 bytes | 60초 | 관리용 `pg_ls_waldir()` 크기 합. 누적 WAL 생성 통계와 보유 크기를 구분. 권한/관측 부재는 unknown |
| 체인 schema별 table/index/TOAST 크기 | 5분 | `pg_total_relation_size`로 top-level 일반 table·materialized view를 집계. index/TOAST를 따로 더해 중복 계산하지 않음. 네 체인 합계와 WAL을 함께 확인 |
| source/feature 진행·오류 | 체인별30초, 직렬 조회 | `/chains/{id}/indexing/status`; pagination의 `next`가 있으면 끝까지 읽어 필수 unit 누락을 구분. DB-only/RPC0이며 기존 체인 pool4개 안에서 조회 |
| 증가율·backlog 추세 | 위 표본의15분/60분 구간 | 시간·단위·generation/runId와 관측 누락을 기록. 재시작/epoch 변경 전후 높이 차이를 처리량으로 계산하지 않음 |

DB 통계는 서버32개 연결 중 관리 예산에서 **동시에 최대1개의 단기 연결**로 직렬 수집하는 안이다. 앱 pool4개에 연결을 더하거나 별도 앱/API pool을 만들지 않는다. 제안 query timeout2초·전체 검사5초에서 취소/반납하고 다음 주기까지 기다리며 중첩 재시도를 하지 않는다. 권한은 통계에 필요한 범위만 별도 검토한다. `pg_ls_waldir`를 위해 앱 role에 superuser/`pg_monitor`를 추가하지 않는다. 향후 쿼리의 값은 파라미터 바인딩, schema/table 식별자는 검증·`pgx.Identifier`를 사용한다.

`Dpg`가160GiB 예산의 판정 기준이다. `Ddb`·schema 합·`Dwal`은 증가 원인을 설명하는 하위 관측이며 `Dpg`에 다시 더하지 않는다. PGDATA 표본이 오래됐거나 실패하면 예산 준수 여부는 unknown이다. filesystem `F`는 raw·DB·snapshot·다른 사용자의 소비를 모두 반영하므로 공간 고갈 판단의 최우선 값이다.

PostgreSQL의 [크기/디렉터리 함수와 권한](https://www.postgresql.org/docs/16/functions-admin.html), [disk-full 동작](https://www.postgresql.org/docs/16/disk-full.html)을 기준으로 한다. WAL 디스크 고갈은 서버 종료로 이어질 수 있으므로 raw의100GiB floor만으로 DB 보호가 구현됐다고 보지 않는다.

기존420GiB 합계(raw160 + DB160 + 여유100)는 runtime floor 기준이다. 두 예산을 모두 보수적으로 잡고 재개 여유150GiB 초과도 확보하려면470GiB 초과와 관리/파일시스템 여유가 필요하다. 이는 논리 예산을 합산한 계획일 뿐 preallocation이 아니며, 현재 exFAT의 과거 여유 수치를 APFS 운영 가능 공간으로 확정하지 않는다.

## backlog의 의미와 확인 가능한 범위

세 단계를 구분한다. 같은 chain·plan·현재 generation의 관측만 비교한다.

1. **수신→capture:** raw가 durable해도 source facts가 DB에 commit된 것은 아니다. status의 `source.factsThrough`를 연속 저장 완료점 `C`로 사용한다. raw 연속 수신 지점과 oldest-pending age는 `pipeline.runtime.rawIntake`로 볼 수 있다. 다만 검증 전 수신 기록이며 canonical 완료점이 아니다. 선택 runtime의 `observedHeader`는 관측 후보일 뿐 latest/완료점이 아니므로 `observedHeader.height - C`는 진단용 후보 격차로만 표시한다. canonical backlog나 수집 누락이 없다는 증거가 아니다.
2. **capture→writer:** `projectionHead`를 `P`로 두되 `projectionMatchesCapture: true`일 때만 `C-P`를 writer의 높이 격차로 쓴다. 두 DB snapshot을 조합한 값이므로 모순/변경 중이면 unknown으로 처리한다. head가 높은 것만으로 모든 feature가 연속 완료됐다고 하지 않는다.
3. **feature별 완료:** 활성 필수 unit의 현재 `epoch/status/completeThrough/gap`를 함께 본다. `gap`·stale·unreported·누락 unit은 완료로 간주하지 않는다. 선택 worker의 `completeThrough`도 별도이며 이번 base plan에는 proof/state/추가 projection worker가 없다.

`source.factsThrough`/cursor/age가 null이면0으로 바꾸지 않는다. anchor는 첫 처리 높이의 부모 기준일 뿐 source facts 완료로 표시하지 않는다. optional `proofBacklog/stateBacklog`를 base writer backlog로 재사용하지 않는다. 현재 YAML에는 `feed.telemetry`가 없어 runtime 계측이 꺼져 있다. 이 초안은 이를 켜지 않는다.

지연 시간은 향후 감시기가 **미처리를 처음 확인한 시각**부터 따로 기록하는 안이다. 이는 적체가 이어진 관측 시간이며 가장 오래된 개별 블록의 대기 시간이 아니다. `rawQueue` 표본은 완료된 작업의 대기 시간이고, process-local proof/state age는 선택 단계의 일부 입력만 관측하므로 현재 가장 오래된 base 미처리 입력의 나이로 대체할 수 없다. raw의 원래 `FirstReceived`에 기반한 oldest-pending age와 연속 raw intake cursor는 `feed.telemetry`를 켠 실행의 `pipeline.runtime.rawIntake`가 제공한다(10/20). 프로세스 안의 관측이므로 재시작 뒤에는 다시 전달된 입력부터 보인다. 상태 API 필드/한계는 [진행 상태 구현](../internal/indexingstatus/status.go), [pipeline 구현](../internal/indexingstatus/pipeline.go), [계측 설명](FEED_TELEMETRY.md)을 따른다.

진행률을 얻을 수 있다면 `λ=동일 epoch의 새 입력/초`, `μ=연속 durable 완료/초`, `B=같은 범위의 미완료 블록 수`로 구분한다. `B>0`이고 `μ>λ`일 때만 `B/(μ-λ)`를 지속 유입 중 예상 추격 시간으로 계산한다. `μ≤λ`이면 현 조건에서 유한한 추격 시간을 제시할 수 없다. 실제 λ가 없는데 Arc를2블록/s로 고정해 완료 시간을 약속하지 않는다. 일시적인 μ 또는450ms 수신 목표를 지속 운영 보장으로 사용하지 않는다.

## 경고·중단·재개 제안값

`1GiB=2^30 bytes`. 다음 기준은 자동 적용되지 않는다. **더 심각한 조건을 우선**하며 identity/무결성 오류에는 대기 시간을 두지 않는다. 일반 warning은 유효한 표본2회 연속, capacity 중단 기준은 유효한 표본1회부터 대응한다. 감시 자체 실패는 정상으로 판정하지 않는다.

| 대상 | 경고/대응 | 중단 기준 | 재개 기준 |
| --- | --- | --- | --- |
| filesystem free `F` | ≤150GiB: 소비 주체·DB/raw 증가를 확인하고 증설/중단 준비 | ≤120GiB 또는100GiB까지 예상 여유≤2시간: indexer 전체 정상 종료 요청. ≤110GiB·실제 ENOSPC/WAL I/O 오류는 긴급 분기 | >150GiB인 유효 표본3회(30초 간격); UUID·경로·권한 재확인. 여유 증가를 snapshot의 임시 수치만으로 승인하지 않음 |
| PGDATA `Dpg` | ≥128GiB(80%) warning; ≥144GiB(90%)는 계획 중단/증설 준비 | ≥160GiB 또는160GiB까지 예상 여유≤2시간: 이 프로젝트 indexer 종료. 물리 quota가 적용된 것은 아님 | <128GiB 또는 승인된 새 DB 용량 계획·저장 배치 확정. 영구 facts 삭제로 수치를 낮추지 않음 |
| Arc raw `Uraw` | ≥80% warning; 미래 자동 수거가 가능한지 별도 확인 | ≥95% 또는 raw 잔여 예상≤1시간: 이 프로젝트 indexer 종료를 요청. 실제 한도/추가 payload 부족에서는 기존 수신 역압력이 먼저 걸릴 수 있음 | ≤70%이고 append·seal 예약 여유 충족. 승인된 GC/별도 이전 없이 수동 segment 삭제 금지 |
| worker 지연 | 미처리 관측5분 또는10분 동안 backlog 증가: DB/RPC/fresh/잠금/입력 오류를 구분 | 알려진 미처리가 있는데 해당 연속 cursor가5분간 정지, 또는 미처리 관측30분: 심각 경보/복구 판단. **지연만으로 자동 종료하지 않음**; 아래 분기 적용 | backlog가0일 필요는 없음. 필수 입력/정확성·저장 여유를 확인하고 실제 durable cursor 진전이 관측되면 복구 상태로 관리 |
| 관측 부재 | 필수 감시값2회 누락: unknown 경고 | mount/identity 오류는 즉시 중단; `F`가60초 넘게 확인되지 않으면 indexer 정상 종료를 요청. `Dpg`가10분 넘게 확인되지 않아 예산을 판단할 수 없을 때도 종료 요청 | 정상 관측 복구와 해당 capacity 기준 충족. 재시작으로 경보 이력을 지우지 않음 |

증가 예측은 향후 운영 표본에서 산출한다. filesystem은 최근15분의 유효한1분 감소율 중 최대 양수와60분 평균 감소율 중 큰 값을 사용해 `(F-100GiB)/r`을 계산한다. DB는5분 간격의 인접 `Dpg` 증가분을 실제 경과 초로 나눈 최근15분 최대 양수와60분 평균 증가율 중 큰 값으로 `(160GiB-Dpg)/r`을 계산한다. 남은 bytes가0 이하면 이미 소진(예상0시간)으로 취급한다. 시계 역행·긴 누락·표본 부족은 unknown이며, `r=0`을 무한 안전으로 표기하지 않는다. 예측값이 없어도 절대 bytes/% 기준은 적용한다. 새 workload·큰 WAL/temp burst는 이 예상보다 먼저 공간을 쓸 수 있다.

raw 잔여 시간은 GC를 기대하지 않는 유입 bytes 기준으로 산출해야 한다. 현재 `Usage`의 순증감만으로 gross append rate를 알 수 없다. `collect: false`인 동안의 양수 증가 추정은 참고할 수 있지만, 수거가 켜진 뒤에는 append 누적 바이트 계측이 필요하다. `IndexReserveBytes`를 빼고 남은 양을 계산하며 raw 한도 밖의 약32GiB 예약 여유를 Arc가 자동 사용한다고 가정하지 않는다.

재개에는 모든 capacity 중단 사유의 해소가 필요하다. 유효한 예상값이 있으면 filesystem/DB 예산 여유6시간 초과·raw 여유3시간 초과도 요구하는 안이다. 증가율이 unknown이면 자동 재개하지 않고 bytes 기준과 복구 여유를 운영자가 판단한다. GC=false 상태에서 raw95%로 멈췄다면 worker가 모두 끝났어도 raw70% 재개선으로 저절로 내려가지 않는다. 별도 승인된 GC 적용 또는 안전한 이전/용량 재설계 없이는 중단 상태를 유지한다.

경계에서 경고와 재개가 동시에 참이 되지 않도록 filesystem은150GiB **초과**, 기존 DB 계획은128GiB **미만**으로 재개선을 구분했다. 이는 정책 문구의 정리이며 자동 controller 구현이 아니다. 또한 `max_unhealthy_duration: 5m`은 현재 manager의 중단 timer가 아니고, 초안에서는 해당 미사용 설정을 제외했다.

## 상황별 중단 범위와 복구

| 상황 | 운영 대응 초안 |
| --- | --- |
| writer만 지연, capture 계속 진행, DB·디스크 정상 | 영구 facts에서 후처리를 계속한다. API에는 뒤처짐/미확인 coverage를 표시하고 정상 최신 서비스로 판정하지 않는다. 새 worker/pool/RPC 예산을 늘리거나 fresh를 생략하지 않는다. 원본 삭제는 writer head가 아니라 기존 GC의 필수 source 저장·raw-only ACK·finality·lease/fence로 판단 |
| capture 지연 또는 raw quota 역압력 | 후처리가 진행 중이면 기존 capacity 대기를 유지할 수 있다. 현재 GC=false이므로 후처리 완료만으로 raw 공간이 회수되지는 않는다.95%/예측 중단선 이전에 복구·증설을 준비하고 도달하면 indexer 종료. 메모리 큐 크기 확대·오래된 입력 폐기로 해결하지 않음 |
| 필수 입력 누락/pruned·canonical/epoch 충돌 | strict 오류를 보존하고 영향 체인 복구를 판단한다. 필요한 과거 자료를 얻을 수 없으면 중단 상태 유지; latest/0 상태나 최신 anchor로 건너뛰지 않음. 일반 reorg는 기존 bounded 복구를 따르고 bootstrap/undo 범위 밖은 명시적 재구성 |
| 공유 APFS/DB 용량 중단선 | 같은 디스크와 PostgreSQL을 쓰는 **이 프로젝트 indexer 서비스 전체**를 종료해 네 체인의 새 쓰기를 멈춘다. 다른 프로젝트/디스크/volume은 건드리지 않는다. 정상 여유가 있으면 DB는 관리 점검을 위해 유지할 수 있음 |
| free≤110GiB·ENOSPC·WAL 쓰기 오류·디스크 제거/identity 불일치 | indexer를 즉시 중단하고 이 프로젝트 DB도 제어된 종료 대상으로 삼는다. 감지 전에 장치가 사라졌다면 정상 종료를 보장할 수 없으므로 강제/오류 종료로 기록하고 복구 점검. 자동 재연결·재초기화 금지 |

현재 공개된 운영 경로에는 수신만 pause/resume하거나 writer만 drain하는 제어가 없다. `StopChain`은 manager 라이브러리 기능이지 이 초안이 제공하는 운영 API가 아니다. capacity/identity의 강제 중단은 indexer 프로세스 전체 종료로 계획해 내부 auto-restart monitor도 함께 멈추게 한다. 오류 상태의 체인만 반복 재시작하도록 방치하지 않는다. 자동화하려면 원인별 재시작 억제와 capacity interlock이 필요하며 아직 구현하지 않았다.

종료는 먼저 indexer에 SIGTERM을 보내 task 취소/join과 DB·window close를 기다리고, DB 중단이 필요하면 그다음 PostgreSQL을 종료하는 순서다. 긴급 사고는 이 순서를 끝까지 수행하지 못할 수 있다. Compose 초안은 indexer에 SIGTERM·120초, PostgreSQL에 SIGINT(fast shutdown)·60초의 종료 유예를 둔다. Docker 기본10초가 지나면 SIGKILL이 되어 commit·fsync·최종 checkpoint를 끝내지 못하기 때문이다. `preflight.py`가 네 값을 검사한다. 실제 runtime join과 DB checkpoint가 그 안에 끝난다는 측정은 아직 없다. 종료 확인 없이 정상 종료로 표기하거나 `down -v`/광범위 prune으로 정리하지 않는다.

공간 회수는 이 프로젝트가 소유한 불필요한 로그/명시적 임시 산출물의 확인, 별도 증설/이전 계획 순으로 판단한다. PGDATA 파일·`pg_wal`·source facts·marker/ledger·미승인 raw를 직접 삭제하지 않는다. [일반 VACUUM](https://www.postgresql.org/docs/16/routine-vacuuming.html)은 대개 공간 재사용이며 즉시 filesystem 반환을 보장하지 않는다. `VACUUM FULL`은 새 복사본 공간과 배타 잠금이 필요하므로 용량 고갈 시 자동 응급 처치로 실행하지 않는다. autovacuum을 끄거나 fsync/synchronous_commit을 완화하지 않는다.

재개는 다음 조건을 모두 확인한 **명시적 운영 재개**다. 이번 초안 작성은 이를 승인하거나 실행하지 않는다.

1. 사고 원인과 종료 결과를 기록한다. 외장 APFS mount/volume·physical-store UUID·ownership과 free/DB/raw 재개 기준을 다시 확인한다. 디스크를 다시 꽂았다고 기존 handle을 재사용하지 않는다.
2. 같은 private 설정/DSN·DB/schema·volume marker/budget·store ID·peer key와 plan/anchor/binding을 확인한다. 기존 데이터와 PostgreSQL 복구 결과·미완료 삭제 fence가 일치해야 한다. 불일치/손상은 중단 유지하며 ledger/DB cursor를 재작성해 통과시키지 않는다.
3. DB durable source cursor·projection head·필수 unit의 연속 완료점을 따로 확인한다. 재시작은 기존 cursor에서 이어가며 미commit RAM 작업은 다시 검증한다. 원본/DB facts 중 필요한 입력이 없으면 재생 가능하다고 가정하지 않는다.
4. Arc peer가 누락 구간의 header/body/receipt를 제공하는지 확인하고, 다른3체인은 strict 부모 상태의 가용성을 확인한다. peer의 실제 과거 보존 범위는 미확정이다. source와 writer의 fresh를 같은5 HTTP/s로 계속 실행한다.
5. 모든 초기화 flag와 GC=false를 유지해 guarded 기동 절차로 재개한다. 단, GC=false여도 기존 durable 삭제 intent의 복구에서 정확한 과거 삭제가 완료될 수 있으므로 [해당 분기](OPERATIONS_RUNBOOK_DRAFT.md#7-중단된-삭제의-복구--gcfalse여도-별도-확인)를 먼저 확인한다. backlog를0으로 지우지 않고 정상 운영 감시에서 durable 진전과 여유를 확인한다. 관측 부재/동일 사고 재발 시 재시작을 반복하지 않는다. 성능·장기 측정은 별도 재개 요청 전 보류한다.

## 구현된 자동 감시와 중단

앱 설정의 `operations` 절이 이 초안의 중단·재개 기준을 프로세스 안에서 적용한다(`internal/opsguard`). 절이 없으면 아무것도 하지 않는다. 바이트 기준값에는 기본값이 없다. 배포마다 디스크와 예산이 다르기 때문이다. 테스트넷 초안 값은 [체인별 설정 초안](examples/testnets)에 있다. 체인 프로세스마다 같은 `operations` 절을 두고, halt latch는 체인별 상태 디렉터리에 둔다.

| 감시값 | 주기 | 경고 | 중단 |
| --- | --- | --- | --- |
| 파일시스템 여유 `F` | 30초 | `warn_free_bytes` 이하 2회 연속 | `stop_free_bytes` 이하 1회, 또는 floor까지 예상 2시간 이하 |
| PGDATA 할당량 `Dpg` | 5분 | 예산의 80% 이상 2회 연속, 90%는 준비 경고 | 예산 이상, 또는 예산까지 예상 2시간 이하 |
| raw 사용률 | 세션의 usage 보고마다(기본 10초) | 80% 이상 2회 연속 | 95% 이상, 또는 95%까지 예상 1시간 이하 |
| 관측 부재 | 위와 같음 | 표본 실패를 기록 | `F` 60초, `Dpg` 10분 넘게 유효 표본 없음 |
| DB 크기 `Ddb`, 보유 WAL `Dwal` | 60초(`postgres_stats_interval`) | 없음. journal 기록만 | 없음 |

`Ddb`는 `pg_database_size(current_database())`, `Dwal`은 `pg_ls_waldir()` 크기 합이다. PostgreSQL 저장소를 쓰는 App의 기존 연결 풀로 읽는다. 새 pool은 만들지 않고, 질의 하나는 30초로 제한한다. multichain에서는 체인들이 같은 database를 쓰므로 가장 최근에 열린 체인의 저장소 하나로 잰다. 중단 판정은 계속 `Dpg`만 쓴다. 통계를 읽지 못하면 journal에 unknown으로 남기고 처음 한 번 경고 로그를 낸다. 색인은 멈추지 않는다. 권한이 없어 실패한 것과 디스크가 찬 것을 구분할 수 없기 때문이다. `pg_ls_waldir()`는 superuser나 `pg_monitor`만 실행할 수 있다. Docker 초안의 `init-indexer.sql`은 `pg_monitor` 대신 이 함수의 EXECUTE만 준다. 이 SQL은 새 PGDATA를 초기화할 때만 실행되므로 이미 초기화한 DB에는 운영자가 같은 GRANT를 직접 실행해야 한다.

Docker VM 디스크는 컨테이너 안의 앱이 볼 수 없다. 이미지·container layer·서비스 로그가 이 디스크를 쓴다. 그래서 `preflight.py --check-host`(따라서 `--start`, `--resume`)가 다른 점검을 모두 통과한 뒤 일회용 container 하나로 `df -P -k /`를 실행해 VM 디스크의 여유를 읽는다. 검토한 indexer 이미지를 `--pull never`·network 없음·capability 없음·읽기 전용 root로 쓰고 끝나면 지운다. 여유가10GiB(`DOCKER_VM_MIN_FREE`, 최초 검토값) 이하이면 거부한다. 점검하는 순간의 값이며 실행 중에는 다시 재지 않는다.

긴급 분기에서 PostgreSQL까지 멈추는 일은 호스트의 `preflight.py --watch`가 맡는다. 컨테이너 안의 indexer는 PostgreSQL을 멈출 수 없기 때문이다. watch는 30초마다 세 가지를 본다. APFS mount·identity 점검이 실패하거나 100GiB 시작 하한 아래로 떨어졌는지, 여유가 110GiB 이하인지, 잠금 파일에 `stopDatabase`가 있는지다. 하나라도 맞으면 `compose stop indexer`, 이어서 `compose stop postgres`를 각자의 유예 안에서 실행하고 exit 3으로 끝난다. indexer 종료가 실패하거나 시간을 넘기면 PostgreSQL은 멈추지 않고 운영자에게 넘긴다. 아직 쓰는 중일 수 있는 indexer를 두고 DB만 멈추지 않기 위해서다. `down`·`rm`·volume 삭제는 하지 않고 아무것도 다시 시작하지 않는다.

`preflight.py --start`는 stack을 띄운 뒤 끝나지 않고 같은 감시를 이어 간다. 그래서 지원하는 시작 경로로 띄운 stack에는 늘 감시가 붙는다. 시작 전에 private 디렉터리의 `watch.lock`을 잡으므로 다른 감시가 이미 붙은 stack은 다시 시작하지 않는다. `--background`를 붙이면 감시가 터미널에서 떨어진 별도 session으로 돌고 `watch.log`에 기록한다. 터미널을 닫아도 감시는 남는다. 감시가 중단되면(exit 130) stack은 감시 없이 계속 돈다. 서비스가 `restart: "no"`라 재부팅 뒤에는 둘 다 올라오지 않고 `--start`로 다시 띄운다.

indexer는 잠금 파일을 쓸 때 `stopDatabase`를 함께 정한다. 저장소 장애(raw/객체 영역 제거·교체·I/O 오류)와 PostgreSQL의 `disk_full`(53100)·`io_error`(58030)이면 참이다. 두 SQLSTATE도 이제 각각 capacity·storage로 분류되어 multichain 자동 재시작에서 빠지고 프로세스 전체를 멈춘다. 용량 기준값 도달만으로는 거짓이다. 이 경우 DB는 점검을 위해 남긴다. PostgreSQL이 WAL 쓰기 실패로 스스로 멈춘 경우는 indexer에게 연결 끊김으로만 보여 구분하지 못한다. 그 상황은 watch의 여유 공간 점검이 잡는다.

예상 시간은 이 초안의 계산을 따른다. 최근 15분에서 1분 이상 간격의 가장 가파른 변화와 60분 평균 중 큰 쪽을 쓴다. 시계가 뒤로 가면 이력을 버린다. 변화가 없으면 예상은 unknown이며 무한으로 취급하지 않는다. raw의 증가 속도는 window를 연 뒤 새로 기록한 bytes(`Usage.AppendedBytes`)로 잰다. 수거가 꺼져 있으면 이것이 순증가와 같다. `Dpg`는 PGDATA를 du처럼 걷는다. 심볼릭 링크를 따라가지 않고 다른 파일시스템으로 넘어가지 않으며, 항목이 상한(기본 100만 개)을 넘거나 읽지 못하면 부분 합 대신 실패로 처리한다.

중단하면 먼저 `halt_file`에 잠금 파일을 기록한다. 첫 원인을 보존하기 위해 이미 있는 잠금은 덮어쓰지 않는다. 이어서 `App.Run`이 그 중단을 결과로 끝나고 프로세스는 오류로 종료한다. 잠금이 있으면 시작을 거부한다. 읽을 수 없는 잠금도 거부 사유다. 감시 표본과 상태 변화는 선택적으로 `journal_file`에 JSON 줄로 남긴다. 파일 하나는 최대 8MiB이고 회전본 하나만 둔다.

재개는 `indexer resume-check` 명령으로만 한다. 30초 간격의 표본 3개가 모두 `resume_free_bytes`를 넘고 `Dpg`가 예산의 80% 미만일 때만 잠금을 지운다. 끝나면 색인을 시작하지 않고 종료한다. 운영자가 그다음에 정상 시작한다. raw 원인의 중단과 무결성 중단은 이 명령으로 지우지 않는다. GC가 꺼져 있으면 raw 사용률은 내려가지 않고, 무결성 원인은 운영자가 직접 확인해야 하기 때문이다. 저장소 identity는 다음 시작에서 window Open이 다시 검사한다. Docker 초안에서는 `preflight.py --resume`이 호스트 점검을 반복한 뒤 DB 없이 이 명령만 실행한다.

실패의 원인도 분류한다. raw/객체 저장소 장애(`window.ErrUnavailable`, `ErrCorrupt`, 객체 영역 장애)는 storage, 용량 부족은 capacity, 불변 입력 충돌·finality 충돌·명시적 재구축 필요는 integrity다. multichain manager는 분류된 실패를 그 체인의 자동 재시작 대상에서 뺀다(`restartSuppressed`). storage와 capacity는 같은 디스크를 쓰는 모든 체인의 문제이므로 잠금을 쓰고 프로세스 전체를 끝낸다. integrity는 해당 체인만 멈추고 다른 체인은 계속한다. 명시적 StartChain은 억제를 해제한다. 단일 체인에서도 storage·capacity 오류로 끝나면 잠금을 쓴다. RPC 오류처럼 분류되지 않은 실패는 기존처럼 다시 시작한다.

아직 구현하지 않은 것은 다음과 같다.

- 실행 중 Docker VM 디스크의 지속 감시. 지금은 시작·재개 전 점검 한 번뿐이다.
- 감시 프로세스 자체가 죽었을 때 알리거나 다시 붙이는 장치. 재부팅 뒤 자동 시작(launchd 등)도 하지 않는다.
- 수신만 멈추는 pause/drain 모드. 10/20 검토에서 보류했다. 지금 종료로도 durable 진행점과 raw가 남아 재시작이 이어 받으므로 얻는 것이 작고, feed 세션의 수신·처리 동시 종료 약속을 바꿔야 해서 위험이 크다. 백업 직전 DB를 받은 블록까지 맞출 필요가 실제로 생기면 다시 판단한다.
- 실제 APFS·PGDATA에서의 동작 확인. 검증은 임시 디렉터리와 모의 probe 범위다.

## 복구 목표와 남은 작업

운영자 대응15분·사고 발생 후2시간 내 복구/장기 중단 방침 결정·worker 지연5분/30분 경보를 최초 제안으로 둔다. 이는 RTO2시간 달성 보장이 아니다. **허용 데이터 손실(RPO), 실제 RTO, 최대 peer 미연결 시간과 장기 DB 백업/증설 방침은 아직 확정하지 않았다.** 등급별 제안과 결정 항목은 [백업·이전과 복구 목표 초안](OPERATIONS_BACKUP_DRAFT.md)에 있다. durable commit까지 보존하는 것이 목표지만 장치 자체 손실은 별도 일관된 DB·source/identity 백업과 복원 검증이 필요하다.24시간 raw 보관은 백업·24시간 undo·원격 상태/peer 보존 보장이 아니다.

현재 구현 완료로 간주하지 않을 후속 순서는 다음과 같다.

1. PGDATA·filesystem·raw 감시와 bounded 기록, 누락 판정·경고 hysteresis는 구현했다(위 절). PostgreSQL 통계 `Ddb`/`Dwal`의 journal 기록도 구현했다. Docker VM 디스크는 시작·재개 전 점검만 구현했고 실행 중 감시는 남는다. 별도 앱 pool이나 새 RPC polling은 만들지 않는다.
2. raw 연속 intake·oldest-pending age·gross append bytes는 `pipeline.runtime.rawIntake`로 노출했다([계측](FEED_TELEMETRY.md)). 미관측은 null이다.
3. 원인별 자동 재시작 억제·중단 상태 보존·재개 interlock은 구현했다. Compose 종료 유예와 긴급 시 DB까지 멈추는 호스트 감시(`--start`가 이어서 실행, `--watch`, `--background`)는 구현했다. 단독 drain 모드는 보류했다.
4. [checkpoint/재시작/GC 운영 절차 초안](OPERATIONS_RUNBOOK_DRAFT.md)과 [읽기 전용 metadata 진단 도구](OPERATIONS_INSPECTION.md)는 작성했다. 진단은 모의 데이터 범위이며 백업 보관·이전의 실제 방식/RPO/RTO와 실제 복구/완전한 GC 승인 진단은 남아 있다. 운영 적용·실제 장애/복원 검증은 저장소·enode 확보와 실행 보류 해제 이후다.

이번 검토는 기존 코드·문서·공식 PostgreSQL 문서에 근거한 정적 검토다. 디스크 상태 재조회, DB 통계 쿼리, 컨테이너/서비스 실행, 실제 삭제·중단·재개, 성능 측정은 하지 않았다.
