# 영구 객체와 정규화 원천 저장

`pkg/window.Objects`와 `pkg/capture`의 라이브러리 API다. Arc ETH/68 검증기는 header·body·transaction·receipt·log를 정규화한다. 계정/계약 버전·storage 변화·trace·blob를 공급하거나 검증하는 기능은 아직 없으며, 객체를 저장할 수 있다는 이유로 그 coverage를 완료 처리하지 않는다. DB-only full-ingest writer는 [facts Source](SOURCE_FACTS.md)에 연결했다. 기존 CLI 활성화·운영 supervisor는 후속 단계다.

## 저장 위치와 내구성

`ObjectOptions`는 `Volume`, `Path`, `Reservation`, `Chain`, `MaxBytes`, `MinFreeBytes`, `MaxObjectBytes`, `MaxObjects`를 명시한다. `Path`는 초기화한 volume 아래의 별도 영구 디렉터리다. raw window와 같은 managed v2 volume에 두면 서로 겹치지 않는 경로와 별도 reservation을 지정한다. 예산은 staging을 포함하며 Close/프로세스 종료 후에도 예약을 반환하지 않는다. 실제 mount·marker·pinned directory·writer lock·filesystem identity 검사는 raw와 같은 코드를 사용한다. `AllowDirectory`는 로컬 디렉터리를 의도적으로 사용할 때만 선택한다. 누락된 mount를 내부 경로로 대신 만들지 않는다.

`CreateObjects`는 명시적 신규 초기화이고 `OpenObjects`는 기존 저장소만 연다. 저장 형식 marker가 달라 raw 저장소로 잘못 열 수 없다. 공유 volume의 기존 예약이나 marker를 삭제/재생성하지 않는다.

`Put(ctx, digest, size, reader)`는 32KiB 버퍼로 정확한 길이와 SHA-256을 검사한다. 같은 디렉터리의 임시 파일에 기록하고 file fsync → rename → directory fsync를 끝낸 뒤 참조를 반환한다. 기본 단일 객체 한도는1GiB, 명시적 최대16GiB이며 기본 파일 수 한도는100만 개다. 실제 수용량은 지정한 전체 예산/여유 공간에 제한된다. 사용자 reader가 `Read` 자체를 무한히 막으면 context만으로 이를 강제 중단할 수 없으므로 호출자가 cancellable reader를 제공해야 한다.

`ObjectRef`는 store ID·업로드별 난수 ID·digest·길이만 포함한다. 외부 URI/상대 경로 입력을 실행하지 않는다. 파일 헤더와 DB 참조를 대조하고 실제 바이트를 다시 해시한다. 대용량 객체는 RAM에 통째로 읽지 않는다. `Copy`의 소비자는 최종 검증 성공 전 파생 결과를 commit하지 않아야 한다. 오류 전 일부 바이트가 writer에 전달될 수 있다.

## DB와 파일 사이의 커밋

`capture.NewWithObjects(db, chain, fresh, objects)`는 열린 영구 저장소를 빌려 쓴다. `Input.Objects`에 dataset kind와 참조를 지정하며 동일 kind의 inline payload를 함께 넣지 않는다. 출처·검증·codec·digest·coverage는 기존 manifest에 보존한다.

1. 객체를 완전히 저장한다. 이 시점은 capture ACK가 아니다.
2. chain별 DB transaction에서 generation/fence/plan을 검사하고 실제 객체를 다시 검증한다.
3. 기존 shared RPC pool로 fresh canonical hash를 확인한다.
4. 객체 binding·inline dataset·정규화 행·manifest·canonical pointer·연속 cursor·`facts_ready`를 같은 durable transaction으로 확정한다.

파일과 DB가 하나의 분산 transaction이라고 가정하지 않는다. 객체 저장 뒤 DB 커밋이 실패하면 파일은 orphan으로 남을 수 있다. 커밋 결과를 잃은 경우 동일 입력 재시도로 이미 확정된 참조를 확인한다. `Read`는 큰 바이트 대신 검증한 객체 참조를 반환하며 `CopyPayload`가 스트리밍으로 읽는다. 객체 저장소를 연결하지 않은 handle은 외부 입력의 Read/GC를 실패시킨다.

`SessionOptions.Objects`와 `SessionConfig.Objects`로 런타임에 연결한다. 세션은 객체 경로 건강 상태도 주기적으로 검사하며, 작업을 모두 join할 때까지 객체 저장소를 닫지 않는다. 저장소의 소유자는 호출자이며 반환 후 Close한다. 기존 YAML의 proposed `feed_storage.objects_path`를 CLI가 지원한다고 해석하지 않는다.

## Orphan 정리와 복구

`CollectObjects(ctx, afterID, limit)`는 최대256개의 candidate를 검사한다. 연결된 객체는 generation reset 뒤의 이전 fork까지 계속 보존한다. TTL이나 raw 보존 기간으로 영구 객체를 지우지 않는다. orphan은 직렬 DB transaction에 **영구 retirement 표식**을 먼저 commit한다. 표식이 있는 ID를 참조하는 새 capture는 거부하므로 늦은/불확실한 커밋과 삭제가 교차하지 않는다. DB 커밋이 실패하거나 결과가 불확실하면 unlink를 호출하지 않는다.

retirement 후 unlink 전 종료됐다면 같은 표식으로 재개할 수 있다. 실제 unlink는 파일 descriptor와 참조를 대조하고 directory fsync를 완료한다. 실패한 업로드를 다시 사용해야 하면 같은 내용으로 새 난수 ID를 발급받아 업로드한다. 살아 있는 객체의 자동 만료·참조 수 차감·압축/이동은 구현하지 않았다. orphan 정리는 명시적 관리 API이며 런타임의 자동 주기 작업이 아니다. 현재 candidate 페이지는 전체 디렉터리를 작은 batch로 순회해 선택하므로 한 페이지당 O(파일 수) 시간이 든다. 큰 저장소의 정기 정리는 별도 색인 최적화가 필요하다.

재시작은 writer lock 아래에서 정확한 private temporary 파일만 제거하고 완료 파일의 헤더·길이·store identity를 검사한다. 완료 객체의 digest는 Commit/Read/Copy/원본 GC 때 다시 검사한다. DB retirement 표식은 늦은 참조를 막기 위해 보존한다. 삭제 도중 파일을 잃었거나 외장 저장소가 끊긴 상황에서 성공한 ACK를 만들어내지 않는다.

## 정규화 행과 조회

Arc 검증기가 모든 commitment/타입/서명/receipt 검사를 통과한 뒤 `Input.Rows`와 `RowManifest`를 만든다. 행은 `capture_records`의 독립 원천 namespace 아래에 저장된다. PostgreSQL 전용 관계형 테이블이나 기존 projection 테이블을 추가한 구현은 아니다. 값은 기존 SQL 파라미터 바인딩으로 저장되며, row kind는 코드가 정한 enum이다.

| kind | 조회 값의 codec | 내용 |
|---|---|---|
| header | `model.block.v1` | transaction/uncle/withdrawal 목록을 제외한 header 모델과 등록된 체인 확장 |
| body | `evm.body.json.v1` | `capture.BodyFacts`: transaction hash 순서, uncle hash, optional withdrawals |
| transaction | `model.transaction.v1` | 서명·sender·입력·raw encoding·체인 확장을 포함한 각 transaction |
| receipt | `model.receipt.v1` | logs를 분리한 각 receipt와 확장 |
| log | `evm.log.json.v1` | 블록/transaction 참조·주소·topics·data를 포함한 `model.Log` |

정확한 encoded header/body/receipt도 기존 dataset으로 보존한다. body의 uncle 전체 header 등은 그 encoded dataset에 남는다. 원래 commitment 재구성의 기준은 검증한 encoded dataset이다. 중복된 model header의 `Size`는 원래 블록 크기이며 목록을 뺀 행의 바이트 크기가 아니다.

행의 identity는 chain/genesis·block hash·plan digest·generation과 kind/ordinal이다. header/body는 각각1행, transaction/receipt 수는 같고 각 종류 최대32,768행이다. 한 행 최대8MiB, 블록의 정규화 값 합계64MiB이며 기존 전체 DB transaction 한도128MiB도 적용된다. 한도 초과는 전체 커밋을 거부하며 일부 행만 완료로 게시하지 않는다. 각 행 digest·identity owner와 정렬된 전체 row digest/count를 기록한다. 페이지 조회도 다른 chain/block/plan/generation의 행을 거부한다. duplicate commit과 원본 GC는 모든 정규화 행을 다시 검사한다.

`Rows(ctx, identity, kind, offset, limit)`는 최대256행/8MiB로 페이지를 읽는다. `Next`를 다음 offset으로 사용하고 `Done`으로 끝을 확인한다. `RowCodec(kind)`에 맞춰 `model.DecodeBlock`, `DecodeTransaction`, `DecodeReceipt` 또는 JSON decoder를 사용한다. 현재 canonical 입력을 고를 때는 별도 `CanonicalAt`으로 generation/높이 포인터를 확인한다. Rows는 불변 fork 조회이며 live canonical 검사를 대신하지 않는다.

이전 encoded-only capture는 정규화 조회에 `ErrCaptureIncomplete`를 반환한다. 로그가 없는 검증된 블록은 명시적 count0을 기록해 빈 성공을 구분한다. 기존 입력에 행을 소급 덧붙이거나 이미 삭제한 과거 자료가 복구됐다고 표시하지 않는다. 기존 저장소의 전체 정규화 coverage가 필요하면 별도 명시적 재구성 절차가 필요하다. 주소/topic/transaction hash 전역 검색·관계형 SQL schema·계정/계약/storage/trace/blob 조회는 후속 공급/API 단계다.

선택 `source.lookup.v1` worker는 source DB만 읽어 블록/거래 해시별 canonical prefix 색인을 만들고, `/indexing/source/*`에서 버전이 있는 header/body/transaction/receipt/log 행을 제공한다. 별도 cursor·immutable root·fresh/동일5 HTTP/s 예산과 reorg/page 고정은 [원천 검색](SOURCE_QUERIES.md)을 따른다. 주소/topic 전역 검색과 기존 projected API의 대체는 포함하지 않는다.

선택 `source.search.v1`은 주소/topic/높이의 ordered posting과 블록별 immutable root를 별도 cursor/event transaction에 기록한다. 기존 source.lookup.v1 형식을 바꾸지 않는다. DB-only 고정 범위·cursor 페이지와 예산/운영 한도는 [원천 검색](SOURCE_QUERIES.md)을 따른다.
