# 원본 디스크 윈도우 저장소

`pkg/window`는 체인별 원본을 지정 디렉터리에 보관하는 저장소 패키지다. 원본 append와 fsync, 닫힌 segment의 디스크 색인, 재시작 복구, 읽기 lease, 삭제 승인 경계를 제공한다. 24시간 데이터 전체를 RAM 큐나 메모리 map으로 유지하지 않는다.

공통 저장소·DB capture coordinator·삭제 복구·세션 lifecycle과 공유 volume 예약을 구현했다. `window_storage`는 엄격하게 검증하며 `capture.RunConfiguredSession`이 지정 instance를 소비한다. [앱 feed 실행](FEED_APP.md)은 Arc의 검증된 header/body/receipt plan을 선택해 DB facts writer와 lifecycle을 연결한다. 상태/영구 객체의 앱 설정은 아직 지원하지 않는다. 실제 Arc peer/외장 mount·정책 운영·독립 CL certificate·24시간/450ms 검증은 남아 있다. [capture API](CAPTURE_STORAGE.md)와 [Feed 런타임](FEED_RUNTIME.md)을 함께 따른다.

## 저장 위치와 초기화

외장 디스크의 기존 mount 경로를 `Volume.Root`, 등록할 저장소 ID를 `Volume.ID`, 체인별 하위 디렉터리를 `Options.Path`로 지정한다. mount를 요구하는 것이 기본값이다. 아래 경로와 ID는 예시이며 실제 외장 디스크에 초기화를 실행한 것은 아니다.

```go
spec := window.VolumeSpec{
    Root: "/Volumes/IndexerData",
    ID:   "indexer-external-01",
    MaxBytes: 96 << 30, // 같은 volume에 있는 모든 raw window의 총 예산 예시
    MinFreeBytes: 20 << 30,
}
// 새 저장소를 운영자가 명시적으로 초기화할 때만 실행한다.
if err := window.InitializeVolume(spec); err != nil {
    return err
}
opts := window.Options{
    Volume:       spec,
    Path:         "/Volumes/IndexerData/indexer/window/arc",
    Reservation:  "arc-testnet", // Close/crash 후에도 유지하는 고정 이름
    Chain:        verifiedChainIdentity, // RPC/adapter에서 검증한 ID와 genesis hash
    MaxBytes:     32 << 30,              // 예시 예산이며 24시간 용량 보장이 아님
    MinFreeBytes: 20 << 30,
}
store, err := window.Create(opts) // 새 빈 디렉터리만 허용
if err != nil {
    return err
}
defer store.Close() // worker 종료와 모든 lease 반환이 먼저 완료되어야 한다
// 단독 재시작은 window.Open(opts). Capture 세션은 facts.OpenWindow(ctx, opts)로 DB를 먼저 확인한다.
// Open 실패를 Create로 자동 대체하지 않는다.
```

`InitializeVolume`은 존재하지 않는 root를 만들지 않는다. 같은 ID와 같은 예산/여유 조건의 초기화만 멱등적이며 기존 marker를 다른 값으로 바꾸지 않는다. `Create`는 하위 디렉터리를 초기화하고 비어 있지 않은 저장소를 덮어쓰지 않는다. `Open`은 등록된 volume과 기존 저장소를 요구한다. `Volume.AllowDirectory: true`는 로컬 디렉터리/시험용 명시적 선택이며 외장 디스크 누락을 우회하는 기본값이 아니다.

Linux/macOS의 mount, marker ID와 열린 디렉터리의 identity를 확인한다. 외장 경로가 사라지거나 다른 디렉터리로 교체되면 오류 상태로 전환하며 내부 디스크로 재생성하지 않는다. 재연결 뒤 기존 handle을 닫고 `Open`으로 복구한다. 경로 접근은 Go 1.24의 `os.Root`에 묶고, 외부 symlink·일반 파일이 아닌 입력·중복 writer를 거부한다.

Docker는 호스트 디렉터리를 `/data/feed` 등에 `--mount type=bind`로 연결하고 **컨테이너 내부 경로**를 지정한다. Linux에서는 같은 filesystem의 bind mount도 mountinfo로 판별한다. 설정은 컨테이너 내부의 volume root와 instance별 경로를 사용한다. 여러 chain의 앱 조립은 [feed 실행](FEED_APP.md)을 따른다. 호스트의 실제 외장 mount 운영 검증은 별도다.

## 입력과 작업 재개

`Append(ctx, envelope)`은 원본 파일을 fsync한 뒤 `Reference`를 반환한다. 같은 chain/hash/dataset의 같은 bytes·codec은 기존 참조를 반환하고 최초 수신 시각을 갱신하지 않는다. 다른 bytes·codec으로 덮어쓰지 않는다. payload의 체인별 실행/합의 검증은 adapter/profile이 수행하며 저장소 checksum은 이를 대신하지 않는다.

`AppendBatch`/`AppendBatchWait`는 **같은 block identity의 서로 다른 dataset 최대3개**를 함께 기록한다. 모든 입력의 크기·identity·kind를 먼저 검사하고, 같은 segment의 원본 fsync를 공유한다. segment를 넘으면 이전 원본을 먼저 fsync한 뒤 기존 index/seal을 만들고 다음 segment를 기록한다. 파일 형식·checksum·quota·보관/삭제 권한은 같다. 잠금이 풀리거나 참조를 반환하기 전에 기록한 바이트를 durable하게 만들며, 용량 부족/취소 때도 완성된 prefix를 동기화한다. 실패는 참조/알림/ACK를 반환하지 않지만 그 durable prefix는 복구·재시도로 이어갈 수 있다. 독립 I/O/identity 오류를 단순 용량 대기로 숨기지 않는다. 여러 블록을 RAM에 쌓거나 fsync를 생략하는 방식은 아니다.

자동 교체는 색인 파일과 이름을 먼저 durable하게 만든다. 이어서 seal과 다음 segment 번호의 파일 내용을 각각 fsync·rename하고, **같은 부모 디렉터리 fsync**로 둘을 확정한 뒤에만 다음 raw 파일을 만든다. 색인→seal과 다음 번호→raw의 선행 보장은 유지한다. 공유 barrier 전 중단돼 seal/번호 중 일부만 남아도 기존 마지막 tail 복구 규칙으로 재개할 수 있다. 사용하지 않은 예약 번호는 건너뛸 수 있지만 기존 파일 번호는 재사용하지 않는다. 명시적 `Seal`/`Close`·segment/번호 한도에서는 다음 번호를 미리 예약하지 않는다. 파일 형식과 손상 판정·삭제 승인을 바꾸지 않는다.

`Lookup`으로 block identity와 dataset을 찾고 `Acquire`로 lease를 얻는다. lease는 읽기 버퍼와 GC 차단을 소유하므로 반드시 `Close`한다. `Envelope`가 반환한 bytes는 수정하거나 lease 종료 뒤 보유하지 않는다. 읽기 I/O는 append mutex 밖에서 수행한다. 디코딩과 worker가 보유하는 메모리는 별도 예산이 필요하다.

`Page(ctx, cursor, limit)`은 최대256개의 참조를 원본 append 순서로 반환한다. 참조가 작업을 마칠 때까지 보존된다는 뜻은 아니므로 처리 전에 lease를 얻는다. cursor는 store ID와 segment/offset이며 canonical block의 연속 완료 높이가 아니다. GC로 cursor가 사라졌으면 `ErrCursorExpired`로 명시하며, 후속 capture DB의 manifest/cursor를 통해 재개해야 한다.

## 파일과 한도

| 파일 | 역할 |
| --- | --- |
| `STORE.json` | format version, store/chain identity와 단조 증가 segment 번호 |
| `LOCK` | 프로세스 간 단일 writer 잠금 |
| `NNNN.raw` | header/metadata/payload checksum을 가진 append frame |
| `NNNN.idx` | 높이/hash/dataset 정렬의 고정 크기 참조 색인과 digest |
| `NNNN.seal` | 닫힌 segment의 크기·개수·범위·색인 digest |
| `NNNN.gc` | 승인된 삭제의 durable intent와 정확한 참조 해시. 중단 후 삭제를 마치고 회수 |
| `GC.completed.json` | 마지막 삭제의 store/segment/참조 해시. DB 잠금 복구를 위한 bounded 완료 기록 |

실제 segment 파일명은 20자리 번호를 사용한다. 정상 종료는 활성 segment의 색인을 닫는다. 비정상 종료에서는 마지막 미완성 frame만 잘라내고 내구성 있는 frame을 복구한다. 헤더 길이도 checksum으로 보호해 길이 손상을 미완성 쓰기로 오인하지 않는다. 완성된 frame이나 색인의 checksum 오류는 중단하며 손상된 자료를 빈 결과로 바꾸지 않는다.

기본 segment 목표256MiB, 최대4,096 record/segment, 최대4,096 segment다. 두 segment 한도 모두 적용되며 블록 수를 고정86,400으로 제한하지 않는다. 메모리는 활성 segment 참조와 bounded index page, 제한된 읽기 버퍼만 보유한다. 기본 payload 한도8MiB, 읽기 버퍼 예산64MiB, 동시 reader32다. 이 수치는 저장소 소유 버퍼의 한도이며 전체 Go RSS 보장이 아니다. 큰 실제 chain payload는 지원 형식과 실측을 확인해 명시적으로 조정해야 한다.

`MaxBytes`는 원본·색인·metadata의 논리 파일 크기와 색인 생성 예약을 제한한다. filesystem 실제 점유량과 다른 DB의 공간은 `MinFreeBytes` 여유 검사도 함께 적용한다. 한도 초과는 `ErrCapacity`, 읽기 예산/lease 충돌은 `ErrBusy`로 역압력을 전달한다. 오래된 미처리 입력을 버리지 않는다. `AppendWait`는 용량 부족 시 같은 immutable 입력을100ms 간격으로 재시도하며 취소할 수 있다. 대기 중 append/volume 잠금을 보유하지 않아 후처리와 승인된 GC가 진행할 수 있다. `WaitWritable`은 일반적인 여유 확인이며 특정 payload가 들어간다는 보장은 아니다. 부분 segment compaction은 남아 있다.

## 공유 volume 예약과 압력 보고

`Volume.MaxBytes > 0`이면 managed 모드다. `Reservation` 이름·상대 경로·chain/genesis·체인별 `MaxBytes`를 checksum 장부에 durable하게 예약한 뒤 raw 디렉터리를 생성한다. `STORE.json`의 임의 ID에 묶어 다른 저장소로 재사용하지 못하게 한다. 여러 프로세스/컨테이너도 같은 root 장부와 flock 파일을 공유해야 한다. 전체 예약 합계는 volume 예산에서 관리 파일용1MiB를 뺀 값 이하다. 최대256개 예약이며 장부는256KiB로 제한한다.

| root 파일 | 역할 |
| --- | --- |
| `.indexer-volume.json` | v2 managed ID·총 예산·여유 조건. 기존 standalone v1과 자동 전환하지 않음 |
| `.indexer-budget.json` | checksum·잠금 identity·고정 예약·store ID. atomic rename/fsync |
| `.indexer-budget.lock` | 프로세스 간 예약과 디스크 쓰기의 배타 잠금. 교체/소실 시 중단 |
| `.indexer-init.lock` | 명시적 초기화 직렬화 |

예약은 파일시스템 preallocation이 아니다. 처음 장부 생성 시 전체 예산과 여유 공간이 있는지 확인하고, 매 append/seal 때 chain/volume 중 큰 `MinFreeBytes`와 색인/관리 파일 여유를 검사한다. 잠금은 협력하는 window의 bounded 디스크 I/O만 직렬화하며 RPC·DB 조회·후처리 동안 보유하지 않는다. 다른 프로그램의 디스크 사용이나 filesystem block/metadata overhead까지 물리 공간을 보장하지는 않는다.

Close·crash·PID 만료로 예약을 반환하지 않는다. 디스크 원본이 남아 있기 때문이다. 실패한 최초 생성도 보수적으로 provisional 예약을 남길 수 있다. 이름/경로/chain/한도의 변경·기존 v1 전환·장부 소실 자동 복구·TTL 회수·온라인 resize/release 기능은 제공하지 않는다. 일치하는 기존 예약의 Open은 멱등적이고 새 예약을 만들지 않는다. 장부를 지우거나 수동으로 수정해 용량을 회수하는 절차는 안전한 운영 기능으로 지원하지 않는다.

`Usage(ctx)`는 실제 논리 파일 바이트·색인 예약·예약 합계·filesystem 여유·Writable을 반환한다. `(파일+색인 예약)/체인 한도`80%는 warning,95%는 critical이다. filesystem의 여유 floor 위 margin이 volume 예산의20%/5% 이하일 때도 경고/critical이며 쓸 여유가 없으면 critical이다. 고정 예약 합계가80%라는 이유만으로 디스크가 찼다고 보고하지 않는다. `RunConfiguredSession`은 기본10초마다 확인하고 severity가 바뀔 때만 로그를 남긴다. 공간 부족은 수신의 역압력이며 후처리는 계속한다. volume 소실/손상은 모든 worker를 취소하고 join한다.

엄격한 YAML 예시는 [window-storage.yaml](examples/window-storage.yaml)이다. `window.ReadLayout`은 이 standalone 형식을1MiB·단일 문서로 읽는다. 일반 앱 설정은 같은 구조를 `window_storage` 아래에 둔다. 파싱/검증은 경로를 만들거나 mount를 초기화하지 않는다. 기존 CLI 활성화와 실제 adapter 연결 범위는 [설정](CONFIG.md#원본-윈도우-저장-설정)을 따른다.

## 삭제 승인

24시간은 최소 보관 시간이고 segment의 가장 늦은 최초 수신 시각을 기준으로 적용한다. 읽는 중인 segment와 활성 segment는 삭제하지 않는다. 시계 역행이나 monotonic/wall clock의 큰 차이를 감지하면 GC를 막는다. 중복 수신·재시작으로 시간을 갱신하지 않는다.

`Collect`에는 DB coordinator가 제공하는 `DeletionGuard`가 필요하다. guard는 모든 record의 필수 plan 저장·영구 객체 내구성·finality/reorg 조건·raw-only consumer ACK를 검증하고, 현재 generation의 fence를 유지하면서 삭제 callback을 동기적으로 호출해야 한다. 테스트의 승인 oracle은 운영용 구현이 아니다. 단순 `CapturePlan.ValidateComplete` 성공이나 기존 처리 commit ACK로 이 승인을 대체하지 않는다.

`SealExpired`는 active segment의 최신 자료까지 retention을 지난 경우에만 seal한다. guard가 callback을 실행하지 않고 정확히 `ErrRetain`을 반환하면 그 segment를 보관하고 다른 후보를 계속 검사한다. joined 독립 오류나 callback 실행 뒤의 `ErrRetain`은 오류로 반환한다. 원천 DB guard의 orphan/ACK 대기에도 이 경계를 사용한다.

후보 선택 뒤 새 lease가 생겨도 삭제 직전에 다시 거부한다. 늦게 호출한 callback, callback을 호출하지 않은 승인, 무시된 삭제 오류를 처리하며 원천 저장 승인 없이 삭제하는 기본 구현을 제공하지 않는다. `pkg/capture.Store`가 DB 기반 guard를 구현한다. 필수 바이트 전체·finality·raw-only ACK를 확인하고 durable DB fence를 먼저 저장한다. 삭제 오류/중단 시 fence를 유지하며 정확한 raw 참조를 남긴다. `RecoverDeletion`은 GC/Close와 직렬화해 정확한 segment 삭제 또는 완료 기록을 확인한 후 DB 잠금을 해제한다. 파일 부재만으로 승인하지 않는다. 세션과 운영 연결 범위는 [capture 저장 문서](CAPTURE_STORAGE.md)를 따른다.

## 검증 범위

macOS race와 CPU1 경합50회, Linux Docker의 실제 bind/읽기 전용 mount를 검증했다. fsync 직후 게시 전 자식 프로세스를 종료해 복구하고, partial header/payload·checksum 손상·경로 교체·marker 변경·중복/충돌·byte/segment/read 한도·lease/GC 경합·GC 중단·cursor 만료를 시험한다.

공유 volume의 프로세스/고루틴 예약 경합·장부/잠금 손상·crash 유지·80/95% 압력·역압력과 GC 재개도 시험한다. 실제 외장 장치의 물리 분리·전원 손실, 86,400/172,800블록과 24시간 실운영, 데이터 공급 가능성·실제 chain 처리량은 후속 검증이다. capture transaction/DB guard의 단위·실DB 검증은 [별도 문서](CAPTURE_STORAGE.md)에 기록한다. 이 구현으로 Arc 속도 개선이나 운영 SLO 달성을 선언하지 않는다. 공개 RPC 5 HTTP/s·fresh canonical·기존 writer의 undo/outbox/SQL 바인딩은 유지한다.

## 영구 객체 영역

`window.Objects`는 raw segment와 별도의 `ObjectOptions.Path`/reservation을 쓰는 영구 저장 형식이다. 같은 volume guard/공유 예산을 재사용하며 raw retention/Collect가 영구 객체를 만료시키지 않는다. 스트리밍 검증·DB binding·orphan retirement와 복구는 [영구 원천 저장](PERMANENT_SOURCE_STORAGE.md)을 따른다. 기존 raw 저장소는 marker 형식을 유지하며 객체 저장소와 혼용하지 않는다.

## Go embedding 시계와 오프라인 검증

`Layout.Now`는 기존 `Options.Now`로 전달하는 선택적 Go 함수다. nil이면 기존
`time.Now`를 사용한다. 호출자는 동시 호출이 안전한 함수를 제공해야 한다.
YAML/JSON에 노출하지 않고 환경 변수·CLI binding도 없다. 순수 파싱·검증에서는
함수를 호출하지 않는다. 24시간 최소 보존, 삭제 permit·원본 증거·epoch·ACK
검사는 그대로 유지한다.

[N6 owned 통합 검사](EXECUTION_ARC_ACCEPTANCE.md)는 원본 window에만 제어 시계를
주어 25시간 경과를 표현한다. RPC/native/SQL 시계나 운영 파일을 바꾸지 않고,
정책을 짧게 하거나 실제 하루를 기다리지 않으면서 양의 원본 삭제를 검증한다.
운영자의 timestamp·fence 수정이나 GC 활성화를 허용하는 설정이 아니다.
