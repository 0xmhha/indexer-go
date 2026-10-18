# 영구 원천 데이터 검색

`feed.projections: [source.lookup.v1]`를 선택하면 별도 순차 worker가 영구 원천 DB에서 블록 해시·트랜잭션 해시 색인을 만든다. 기본 비활성이다. 기존 `execution.summary.v1`과 함께 선택할 수 있다. 운영 `config.yaml`은 변경하지 않았다. 새 worker는 수신/capture와 독립된 완료 cursor를 가지며 이미 저장한 source prefix를 durable queue로 사용한다. 한 번에 한 블록을 처리하며 없는 데이터를 건너뛰거나 latest로 대체하지 않는다.

## 저장·복구 계약

worker는 지정한 chain profile로 encoded header/body/receipt의 해시·서명·transaction/receipt root·gas/bloom을 다시 검증한 뒤 순서가 있는 거래 해시를 색인한다. chain/genesis별 capture namespace의 immutable content-addressed hash trie를 사용한다. 블록/거래 해시는 서로 다른 key domain이고, 값은 높이·블록 해시·거래 ordinal이다. canonical prefix의 중복 거래 해시는 덮어쓰지 않고 실패한다. 이 첫 형식은 EVM의 canonical 거래 해시 유일성을 전제로 한다.

새 trie node·블록별 root/입력 digest·worker cursor·자체 output event는 기존 source transaction에서 함께 저장한다. commit 및 불확실한 commit 재시도는 uncached fresh canonical 검사를 수행한다. `lookupVerify`는 영구 source 읽기/commitment 재검증, `lookupCommit`은 trie 구성·DB transaction·fresh 대기/commit을 측정한다. 런타임 표본은 durable 완료 cursor와 구분한다. 추가 검사는 기존 체인별 **5 HTTP/s** pool과 burst1을 공유한다. 색인은 RPC 데이터 획득 요청을 추가하지 않지만 fresh 검사 비용은 추가하므로 운영 처리량을 높인다고 보장하지 않는다. optional worker도 실패하면 기존 app task 집합이 취소/join한다.

reorg에서는 기존 worker reset 계약의 최대128높이 retained-prefix 탐색으로 현재 canonical root를 선택한다. 찾지 못하면 명시적 anchor부터 영구 source DB를 재생한다. 이전 fork node/output은 역사 자료로 남고 현재 검색 root에는 포함되지 않는다. 기존 feature의 전역 undo/outbox·base plan/Through·raw ACK·24시간 GC 조건을 변경하지 않는다. 색인용 노드도 영구 DB 용량을 사용하며 이 버전은 과거 root의 별도 GC를 구현하지 않는다.

DB에서 읽은 trie node는 요청한 Keccak hash와 대조한다. 알려진 node가 없거나 손상됐으면 검색 실패이며 `found:false`로 바꾸지 않는다. 결과가 있는 경우에는 원천 encoded 데이터를 다시 검증하고 실제 블록/거래 해시와 ordinal을 대조한다. root는 로컬 파생 색인의 commit 증거다. 모든 DB metadata를 일관되게 다시 쓸 수 있는 공격자에 대한 외부 consensus 증명이나 전 역사 부재 증명은 아니다.

새 SQL/table/migration은 없다. 기존 `CaptureUpdate`/`CaptureView`를 사용하므로 PostgreSQL의 schema 검증·pgx identifier 처리와 값 파라미터 바인딩이 그대로 적용된다. profile·해시·ordinal·페이지 입력을 SQL 문장으로 조립하지 않는다.

## HTTP API

아래 GET 경로는 single-chain 기준이며 multi-chain에서는 `/chains/{id}`를 앞에 붙인다. 기존 인증/속도 제한 middleware를 따른다.

| 경로 | 자료 |
|---|---|
| `/indexing/source/blocks/{height}/{kind}` | 색인 완료 구간의 정확한 높이 |
| `/indexing/source/blocks/by-hash/{hash}/{kind}` | 현재 canonical 색인의 블록 해시 |
| `/indexing/source/transactions/{hash}/{kind}` | 현재 canonical 색인의 거래·receipt·해당 거래 log |

블록 `kind`는 `header`, `body`, `transaction`, `receipt`, `log`다. 거래 경로는 `transaction`, `receipt`, `log`를 지원한다. 해시는 `0x`와64자리 hex다. `limit`은1..256, 기본50이고 `after`는 선택 결과 목록의0-based offset이다. 페이지는 원래 행 값 기준8MiB 이하이며 JSON의 base64 오버헤드는 별도다. 한 handler가 모든 source 경로를 합쳐 최대2개 요청을 처리하고 초과 요청은429로 거부한다.

응답은 `epoch`, `anchor`, `indexedThrough`, `capturedThrough`, 색인 `root`, `found`, exact `identity`, `scope`, `kind`, `codec`, `rows`, `next`, `done`을 포함한다. `found:false`는 **anchor 다음부터 indexedThrough까지의 구간**에서만 찾지 못했다는 뜻이다. bootstrap 자체는 색인하지 않는다. hash 검색의 부재는200이며, 높이 미완료/미시작은404다. `/indexing/status`의 `source.sourceLookup`은 profile/버전을, `workers`의 `source.lookup.v1`은 별도 완료 높이·epoch·sequence를 제공한다.

다음 페이지는 `after=<next>&epoch=<scope.epoch>&hash=<scope.hash>&definition=<scope.definition>`을 모두 보낸다. 일부만 보내면400, epoch/블록 해시/정의가 바뀌면409다. 같은 epoch에서 더 높은 블록의 색인 root가 추가되는 것은 기존 블록 페이지를 무효화하지 않는다. log 행의 `index`는 블록 전체 ordinal이며 거래 log 조회의 `after`/`next`는 그 거래의 선택 목록에 대한 offset이다.

행 `value`는 JSON에서 base64인 byte 배열이다. `model.block.v1`, `model.transaction.v1`, `model.receipt.v1`은 버전이 있는 모델 RLP이므로 `model.DecodeBlock/DecodeTransaction/DecodeReceipt`로 읽는다. `evm.body.json.v1`과 `evm.log.json.v1`은 UTF-8 JSON이다. chain extension을 임의의 JSON 변환으로 버리지 않는다. receipt 행의 log 목록은 별도 log 행에 있다.

조회는 DB와 명시적으로 제공된 영구 객체 읽기 경로만 사용한다. HTTP app 경로는 현재 inline 원천 저장을 사용하며 외부 객체를 쓰는 library 조립자는 `capture.NewReader`에 해당 영구 객체 영역을 주어야 한다. 설정 없는 객체 영역을 raw window/RPC로 대신하지 않는다. 읽기 전용 PostgreSQL handle/pool1을 지원하며 페이지 읽기는 쓰기/ACK/GC를 수행하지 않는다. 응답은 `Cache-Control: no-store`다. 기능 미설정503, 잘못된 요청400, 재동기화409, 완료 자료의 누락/손상500으로 구분한다.

## 해시 조회 범위

첫 검색은 높이·블록/거래 해시의 exact lookup과 블록/거래 단위 페이지다. 주소·topic·블록 범위 검색은 아래 별도 선택 색인으로 제공한다. 시간 범위·주소/topic OR 조건은 아래 필터 API에 포함한다. 전용 관계형 source 테이블은 비교 후 별도 저장 계약을 결정한다. 기존 GraphQL/JSON-RPC/REST의 legacy projected table 조회를 자동 대체하지 않는다. 읽을 때 source commitment를 다시 계산하므로 큰 블록의 CPU/메모리 비용은 남는다. admission과 입력/행 한도로 제한했으며 별도 계측·동일 입력 비교로 최적화해야 한다. 로컬 fixture·Docker 검증은 실제 Arc 처리량/450ms·24시간 운영 근거가 아니다.

## 주소·topic·블록 범위 검색

`feed.projections: [source.search.v1]`를 선택하면 별도 ordered worker가 영구 source DB의 블록을 profile/commitment로 검증하고 주소·topic의 ordered posting 색인을 만든다. 해시 조회용 `source.lookup.v1`과 독립적이며 함께 선택할 수 있다. 기본 비활성이고 운영 `config.yaml`은 변경하지 않았다. 각 worker의 commit/retry fresh 검사는 기존 체인별5 HTTP/s/burst1 예산을 공유한다. 여러 worker를 켜면 추가 fresh 요청과 DB 동기화 비용이 생기므로 실시간 처리량 향상을 보장하지 않는다.

블록당 immutable trie root와 block marker·입력 digest·연속 cursor·자체 event가 같은 source transaction에 기록된다. empty block도 marker/root를 가지며 빈 색인 결과와 미처리를 구분한다. transaction height 목록과 sender/recipient의 합집합, log height 목록과 emitting address·topic 위치0..3별 목록을 저장한다. self transfer는 같은 주소에 중복 posting을 만들지 않는다. sender가 검증되지 않은 opaque 거래는 거부한다. 주소는 거래의 from/to 또는 log 발행 주소이며 내부 호출·storage·잔액·계약 생성 주소나 모든 관련 주소를 추론하지 않는다. raw ACK/GC·기존 undo/outbox·source plan/Through를 변경하지 않는다.

새 SQL/table/migration은 없다. 기존 capture KV의 값 바인딩을 사용하고, key domain·정렬·namespace는 코드가 소유한다. 입력은 transactions/logs 각각32,768개·topic4개로 제한하고 기존128MiB source transaction 한도를 적용한다. immutable node 보존에 따른 디스크 증가는 추가된다. 실제 대용량 블록/장기 DB 증가의 검증은 남아 있다.

| GET 경로 | 필터 |
|---|---|
| `/indexing/source/logs` | `from`, `to`, 선택 `address`, `topic0`..`topic3` |
| `/indexing/source/transactions` | `from`, `to`, 선택 `address`, `role=from` 또는 `role=to` |

multi-chain에서는 `/chains/{id}`를 앞에 붙인다. `from`/`to`는 양 끝을 포함한 필수 uint64 블록 높이이며 anchor보다 커야 한다. `to`가 색인 완료점보다 높으면404이고 범위를 조용히 줄이지 않는다. `address`는 정확한20바이트 hex이고 쉼표로 구분한 주소 목록은 OR다. log의 `topic0`..`topic3`도 각 위치의 정확한32바이트 hex 목록을 받아 위치 내부는 OR, 위치 사이는 AND로 판정한다. 생략한 위치는 wildcard이고 지정한 위치는 존재해야 한다. 주소·topic·시간 차원 사이도 AND다. zero address/topic도 실제 값으로 검색한다. 거래 `role`을 생략하면 from/to 중 하나가 주소와 일치하는 거래를 반환한다. role은 address가 있을 때만 허용한다. 거래 검색의 topic, log 검색의 role, 반복/알 수 없는 query parameter와 잘못된 hex는400이다. 빈 목록 항목은 허용하지 않는다. 주소/각 topic 위치마다 최대16개, 모든 차원 합계64개 대안을 중복 제거 전 입력 기준으로 제한한다. URL query는8KiB 이하이며 같은 parameter를 반복하지 않고 하나의 쉼표 목록으로 보낸다.

선택 `from_time`/`to_time`은 양 끝을 포함하는 Unix 초(uint64)다. 하나만 보낼 수도 있고0도 실제 값이다. `from_time > to_time`과 overflow는400이다. 필수 블록 높이 `from`/`to`를 계속 지정한다. 시간은 profile/commitment로 다시 검증한 원천 header의 `Time`으로 판정하며 별도 RPC 호출·timestamp 색인·기존 worker 재구축을 추가하지 않는다. 시간 순으로 높이를 추측하거나 중간에서 조용히 범위를 자르지 않는다. 시간만 선택적이면 그 높이 범위의 후보를 페이지별로 검사하므로 성능은 선택한 주소/topic 색인에 달려 있다.

첫 페이지는 `limit=1..256`(기본50)을 받는다. 응답의 `rows`는 각각 exact source `identity`와 버전 codec의 `row`를 포함한다. log는 UTF-8 JSON, transaction은 `model.transaction.v1`의 RLP/base64다. 검색 결과에서 row ordinal은 블록 전체의 transaction/log index다. header/body/receipt 조회는 앞 절의 해시/블록별 API를 사용한다.

검색은 주소가 있으면 해당 주소 posting들의 합집합, 없으면 첫 지정 topic 위치의 posting 합집합, 아무 조건도 없으면 해당 종류의 height posting을 seek한다. 최대16개 iterator의 현재 후보만 보유하고 높이/ordinal 순서로 병합한다. 동일 거래의 from/to가 서로 다른 OR 주소에 일치해도 한 번만 반환한다. 전체 이력을 정렬하거나 가능한 조건의 Cartesian product를 만들지 않는다. 각 요청은 최대256개 후보와16개 source 블록만 검사한다. 한 번에 한 블록만 재검증/보유하며 secondary filter를 원천 bytes와 대조한 결과만 반환한다. 원래 행 값8MiB 한도와 기존 모든 source route 합산 동시2개 admission을 공유한다. `scannedCandidates`와 `decodedBlocks`로 실제 검사량을 표시한다. 일치 행이0개여도 후보 한도에 닿으면 `done:false`/`next`를 반환한다. `done`이 true가 될 때까지 같은 필터의 다음 페이지를 읽어야 한다.

다음 페이지는 응답 `next`를 변경하지 않고 `after`에 보낸다. opaque base64url cursor는 version·epoch·범위 끝 블록 hash·그 높이까지의 immutable root·filter digest·worker definition·마지막 검사 위치를 고정한다. 페이지 크기는 바꿀 수 있다. 대안의 순서·중복·hex 대소문자는 정규화해 같은 필터 digest를 만들며 한 개 대안의 digest는 기존 exact 필터와 같다. 기존 단일값 cursor는 계속 유효하다. 시간 조건을 포함한 실질 필터 변경은409다. 새로운 높은 블록이 색인돼도 고정한 `to`의 root는 바뀌지 않는다. reorg가 epoch를 바꾸거나 필터/정의가 달라지면409다. cursor는 서명된 인증 token이 아니며 일반 keyset 위치와 같고, canonical encoding/길이/범위를 검증한다. `scope.root`가 검색 범위 끝 root이고 `snapshot.root`는 응답 당시 worker 완료 prefix root여서 서로 다를 수 있다.

`capture.Reader.ScanSearch`의 `SearchCandidatePage`는 **primary posting의 후보**다. 직접 library를 쓰는 호출자는 전체 필터가 적용된 결과로 취급하면 안 된다. `indexingstatus.ReadSearch`는 source profile/commitment와 정확한 hash/ordinal·primary/secondary predicate를 재검증한다. primary key가 원천 자료와 다르면 손상500이고, 정상 후보의 secondary 불일치는 건너뛰면서 cursor를 전진한다. 범위 검색의 root도 로컬 파생 commit 근거이며 전체 DB를 일관되게 다시 쓰는 공격자에 대한 외부 부재 증명은 아니다.

조회는 RPC·원본 윈도우·legacy projected table로 fallback하거나 DB에 쓰지 않는다. HTTP 경로는 inline 영구 자료를 사용하며 없는 외부 객체 경로를 임의로 만들지 않는다. PostgreSQL read-only/pool1을 지원한다. `Cache-Control: no-store`, 미설정503, 잘못된 요청400, 미시작/미완료404, epoch/filter 재동기화409, 초과 동시성429, 필요한 node/자료의 누락·손상500이다. source 상태의 `sourceSearch` descriptor와 `workers`에서 name이 `source.search.v1`인 항목의 완료점을 확인한다. 선택 계측의 `searchVerify`는 영구 입력 읽기/commitment 재검증, `searchCommit`은 trie 구성·source transaction·fresh 대기/commit을 포함한다.

timestamp/OR·다중 주소 조건을 구현했고 아래 고정 입력으로 조회와 색인 용량을 비교한다. 전용 관계형 source 테이블의 운영 적용은 별도 원자성·epoch/복구 계약 검증 후 결정한다. 실제 Arc peer/보존·450ms/24시간 운영 측정과 추가 stateful worker의 소유권은 별도 잔여 항목이다.


## 고정 입력 비교 (2026-10-16)

`TestMeasureSourceSearch`는 같은32블록·64거래·512로그를 실제 profile/영구 DB/선택 worker에 넣는다. 두 주소 OR + topic1의 두 값 OR + 시간 구간을 결합한 결과16행이 전체 검증 스캔과 byte 단위로 일치하는지 확인한다. 전체 스캔도 동일한 source commitment 검증과 페이지/시간 조건을 수행하고, 주소/topic 조건만 읽은 행에 적용한다. 색인 검색은64후보/4블록, 전체 스캔은512후보/32블록을 검사했다. 두 경로의 측정 중 RPC 호출은0회다. 수집/commit은 기존5 HTTP/s 설정을 사용했다.

한 번 예열한 뒤20회 반복한 전체 조회 완료 지연(ms)이다. 측정은 race instrumentation 없이 macOS native Pebble과 로컬 Docker PostgreSQL16(fsync on)에서 순차 실행했다. 테스트 도우미의 결과 일치 검사 비용도 포함한다.20개 표본의 p99는 최대 표본이며 운영 SLO 추정치가 아니다.

| DB / 조회 경로 | p50 | p95 | p99 |
|---|---:|---:|---:|
| Pebble / 전체 검증 스캔 | 10.904 | 11.577 | 11.639 |
| Pebble / 색인 + 원천 재검증 | 1.447 | 1.553 | 1.855 |
| PostgreSQL / 전체 검증 스캔 | 516.917 | 580.427 | 593.003 |
| PostgreSQL / 색인 + 원천 재검증 | 80.448 | 93.261 | 94.682 |

후보 발견 단계만 별도로 비교했다. SQL은 같은512로그의 높이/ordinal/hash/주소/topic/시각을 파라미터 바인딩으로 넣은 세션 전용 임시 테이블에 `(height,ordinal)` PK와 `(address,height,ordinal)` B-tree를 둔다. trie와 SQL 모두 주소 OR의64개 후보를 높이/ordinal/hash까지 대조한다. 양쪽 모두 이 단계에서는 시간/topic secondary 조건을 적용하지 않는다.

| 후보 발견 경로 | p50 ms | p95 ms | p99 ms |
|---|---:|---:|---:|
| Pebble trie | 0.309 | 0.439 | 0.935 |
| PostgreSQL capture KV trie | 37.634 | 49.457 | 55.199 |
| PostgreSQL 임시 관계형 후보 | 0.618 | 0.763 | 0.778 |

이 입력의 보존된 과거 root 포함 trie node는 두 DB에서 모두1,974개, namespace를 제외한 node key+value 합계357,648bytes(약349.3KiB)다. SQL 임시 테이블+PK/주소 색인의 실제 relation 크기는204,800bytes(200KiB)다. **동일 보장/기능의 디스크 비교는 아니다.** trie 수치에는 거래/from-to/topic·높이 색인과 모든 과거 root의 node가 들어가지만 물리 DB page/WAL/namespace overhead는 제외했다. SQL 프로토타입은 로그 후보만 다루며 과거 root·epoch·fresh/원자적 cursor/event·reorg 복구·원천 재검증·영구 commit을 구현하지 않았다. 이 작은 fixture를 하루 DB 용량이나 장기 GC 추정으로 외삽하지 않는다.

첫 비교는 PostgreSQL에서 trie node별 `CaptureReader.Get`가 SQL 왕복을 만드는 비용을 보여 주었다. 아래 스냅샷 내부 캐시/일괄 읽기를 반영하고 같은 입력을 다시 측정했다. 별도 관계형 source 색인을 도입한다면 현재 epoch/고정 범위 cursor·fresh·원자성·복구를 먼저 갖춰야 한다. 시간 색인의 추가 가치는 별도 분포로 판단한다. 실제 Arc ingest/450ms/24시간 검증은 이 조회 측정에 포함되지 않는다.

재현은 `INDEXER_MEASURE_SOURCE_SEARCH=1 go test -count=1 -v ./pkg/app -run '^TestMeasureSourceSearch$'`로 한다. 실제 SQL 비교에는 테스트 전용 DB의 `INDEXER_TEST_POSTGRES`와 `INDEXER_TEST_DRIVER=postgres`를 함께 설정한다. 앱 fixture는 pool1을 사용한다. SQL 프로토타입 테이블은 연결 종료 때 사라진다. 이번 컨테이너/볼륨과 Linux 바이너리는 테스트 소유 자원만 정리했다. 원본 측정·검증 로그는 `/private/tmp/indexer-source-search-or-20261016/`에 있다.


## PostgreSQL trie 읽기 최적화

`ScanSearch`는 한 `CaptureView` 안에서만 쓸 수 있는 immutable node 캐시를 공유한다. 같은 root를 반복 열거나 여러 OR iterator가 경로를 공유해도 node를 다시 DB에서 읽지 않는다. cache key는 code-owned node prefix와 hash를 포함하며 chain/genesis는 해당 DB view에 고정된다. metadata/canonical cursor/epoch/worker definition은 캐시하지 않는다. node는 Keccak hash·1MiB 크기 검사를 통과한 뒤 보유하고 호출자에게 복사한 bytes를 돌려준다. cache의 계산된 key/value/entry 비용은4MiB, 항목 수는4,096개 이하다. 한도에 닿으면 검증된 단건 읽기를 계속하고 cache를 다음 요청이나 새 snapshot으로 넘기지 않는다. DB buffer/반환 복사/Go heap 전체가4MiB라는 뜻은 아니다.

범위 검색은 검증된 branch/extension node의 직접 child hash만 optional `port.CaptureBatchReader.GetMany`로 함께 읽는다. branch terminal value와 leaf value는32바이트여도 자식으로 취급하지 않는다. embedded node도 hashed child가 없어 별도 조회하지 않는다. 해시 exact lookup과 snapshot point check는 캐시만 사용해 불필요한 형제 경로 prefetch를 피한다. Pebble 및 Get-only 구현은 같은 hash 검증/캐시 경로로 동작하며 새 batch 포트 구현은 선택이다.

PostgreSQL `GetMany`는 기존 capture reader와 같은 chain·transaction·snapshot을 사용한다. unbound 조회는 RepeatableRead이며 bound 일반 full view는 FOR UPDATE, 공개 prefix 검증은 선택 KEY SHARE로 세대를 보호한다. [잠금 범위 계약](CAPTURE_STORAGE.md)을 따른다. pool1과 읽기 전용 handle을 지원한다. `chain=$1 AND key=ANY($2::bytea[])`와 값 크기 `$3`를 바인딩한다. 서버의 CASE가1MiB를 넘는 value 전송을 막는다. 한 번에 최대32개 입력 key, 각 value1MiB, 반환 key+value 합계4MiB를 적용하며 key 중복 제거 전 입력 수를 제한한다. 결과는 중복 없는 unordered map이고, 없는 key는 생략하며 존재하는 빈 byte 값은 유지한다. 잘못된 입력·과대 값/결과·I/O 오류는 partial success 없이 callback 전체를 실패시키고 이후 접근도 실패한다. query가 없거나 batch가 실패했다고 단건/RPC fallback으로 감추지 않는다.

prefetch한 형제 node가 누락/손상됐어도 그 query가 사용하지 않는 형제면 결과를 바꾸지 않는다. 필요한 경로에서 해당 node를 만나면 conflict로 실패하며 `not found` 검색 결과로 바꾸지 않는다. 새 view는 수리된 DB를 다시 읽고 이전 view의 negative entry를 물려받지 않는다. batch I/O/프로토콜 실패는 즉시 전달한다. 취소된 context는 cache hit에서도 거부한다.

쓰기·worker root/digest/cursor/event·fresh·source plan/Through·undo/outbox·raw ACK/GC 계약은 동일하다. 새 SQL table/migration·설정·공유 전역 cache는 추가하지 않았다. source.search/source.lookup worker의 optional 활성화와5 HTTP/s 비용도 기존과 같다.


2026-10-17에 변경 전 코드를 바이너리로 보관한 뒤 최종 바이너리와 같은32블록/64거래/512로그 fixture를 순차 실행했다. PostgreSQL은 같은 임시 fsync-on 서버에 별도 테스트 schema를 사용했다. 다른 검증 실행은 끝낸 상태였으며 기존 상시 호스트 작업은 유지했다. 수집 설정은5 HTTP/s이고 조회 중 RPC는0회였다. 두 버전 모두 검증 스캔과 같은16행을 반환했고, 후보64개/검증4블록·과거 trie1,974node/357,648bytes도 같았다. 각 경로20 warm 표본의 ms다.

| 경로 | 전 p50 | 후 p50 | 전 p95 | 후 p95 | 전 p99 | 후 p99 |
|---|---:|---:|---:|---:|---:|---:|
| Pebble 검증 검색 | 1.490 | 1.344 | 1.879 | 1.776 | 2.093 | 2.174 |
| PostgreSQL 검증 검색 | 88.159 | 58.996 | 226.101 | 70.853 | 527.756 | 95.621 |
| PostgreSQL 후보 발견 | 43.621 | 15.680 | 54.459 | 19.783 | 143.850 | 23.739 |
| PostgreSQL 검증 전체 스캔 | 576.358 | 372.753 | 625.310 | 442.234 | 710.227 | 448.186 |

별도의 진단 query는 latency 표본 밖에서 수행했다. 최종 코드에서 캐시는 유지하고 optional batch capability만 숨긴 Get-only control은 PostgreSQL node 단건88회였고, batch를 켠 경우 단건5회+배치19회=24회였다. 전체 capture 읽기는175단건→92단건+19배치=111회였다. 배치에서는102개 key·반환 key/value16,285bytes를 읽었다. 이 수치는 CaptureReader 호출만 세며 트랜잭션 begin/commit·ReadIndexingPage·모든 SQL 문장 수를 뜻하지 않는다. 시간 측정에 이 카운터 wrapper를 넣지 않았다.

SQL 임시 후보 테이블은 이번에도 후보 발견만 검증한 프로토타입이고 운영 source index로 바꾸지 않았다.20개 warm 표본의 p99는 최대값이므로 운영 tail/SLO 개선으로 해석하지 않는다. 실제 Arc의 수집/450ms/장기 보존 결과는 별도로 검증해야 한다. 임시 컨테이너·볼륨·변경 전후 및 Linux 바이너리는 소유 자원만 제거했다. 증거는 `/private/tmp/indexer-trie-batch-20261017/measurements.json`과 `validation.json`이다.

이후 metadata/영구 원천 입력 읽기를 아래 단일 스냅샷 경로로 합쳤다. 노드 조회에 맞춘1MiB GetMany 한도는 그대로 유지하고 큰 원천 payload에는 적용하지 않는다.


## Canonical 원천 입력의 단일 스냅샷 읽기

`capture.Store`/`Reader.ReadProjectionInput(ctx, epoch, identity)`는 현재 canonical 입력을 한 `CaptureView`에서 읽는다. 같은 snapshot의 state·height pointer·manifest를 확인한 뒤 inline dataset과 영구 객체 바인딩/bytes를 읽는다. 선택 GetMany가 있으면 작은 metadata3개만 바인딩된 한 SQL query로 받는다. Get-only 구현도 같은 metadata1MiB 한도를 적용하고 wrapped missing과 joined I/O 오류를 구분한다. metadata는 현재 snapshot 내부에서만 보유하고 다른 view/요청으로 넘기지 않는다. writer의 내부 checkProjection/fresh/commit 계약은 그대로다. public ValidateProjection도 같은 snapshot 내부 metadata 배치를 사용한다.

큰 body/receipt를1MiB node batch에 넣거나 그 한도를 늘리지 않는다. 각 materialized dataset8MiB·총64MiB·기존 최대11종의 한도를 적용하고 digest·identity·plan/coverage를 확인한다. 영구 객체는 지정 area/store ID와 DB binding·retired 여부를 확인하고 descriptor/length/digest·volume guard를 검사하는 Copy를 한 번 수행한다. 부분적으로 읽었어도 최종 오류면 빈 Input과 오류만 반환한다. bytes는 호출자 소유이고 결과에는 객체 ref 대신 materialized Payloads가 들어간다. 과거 fork의 ref/큰 객체 스트리밍이 필요한 호출자는 기존 Read/CopyPayload를 사용하며 그 계약은 변경하지 않았다.

`evmdata.ReadCaptured`는 optional ProjectionInputReader를 발견하면 이 경로를 사용한다. 기존 CapturedReader 필수 interface는 그대로이며 확장 없는 reader는 기존 검증/읽기 경로를 사용한다. 선택 경로가 실패하면 레거시/RPC/원본 window로 재시도하지 않는다. profile hash/sender·transaction/receipt root·gas/bloom·parent 검증을 그대로 수행한다. **디코딩 뒤 독립적인 최신 epoch/canonical ValidateProjection도 유지한다.** 읽기 snapshot이 일관됐다는 것만으로 디코딩 중 reset을 무시하거나 현재 canonical임을 보장하지 않는다. 현재 epoch2에서 retained canonical 입력의 원래 generation1을 읽는 것도 지원한다.

이 경로는 source 해시/검색 API와 선택 lookup/search worker 및 facts Source의 실제 BlockWithReceipts 소비에 적용된다. facts writer도 같은 단일 snapshot materialization을 사용하고 기존 coverage 오류·부모 자료·profile 재검증·디코딩 뒤 epoch barrier·처리 ACK/fresh 계약을 유지한다. 실제 writer 비교는 [facts 읽기/쓰기](SOURCE_FACTS.md)의 측정 결과를 따른다. raw ACK/24시간 GC·undo/outbox·DB table/migration·운영 YAML은 변경하지 않았다.


2026-10-17에 변경 전후 app 바이너리를 같은32블록/64거래/512로그 입력으로 순차 비교했다. 비교 동안 다른 검증 명령은 실행하지 않았고 기존 상시 호스트 작업은 유지했다. SQL은 같은 PostgreSQL16 fsync-on 서버의 별도 테스트 schema에서 측정했다. 양쪽 모두 전체 검증 스캔과 같은16행·후보64개·검증4블록을 반환하고 조회 RPC0, 과거 trie1,974nodes/357,648bytes를 유지했다. 각20 warm 표본의 ms다.

| 경로 | 전 p50 | 후 p50 | 전 p95 | 후 p95 | 전 p99 | 후 p99 |
|---|---:|---:|---:|---:|---:|---:|
| PostgreSQL 원천 검증 포함 검색 | 74.502 | 46.108 | 96.927 | 85.307 | 152.403 | 94.592 |
| PostgreSQL 검증 전체 스캔 | 427.000 | 278.567 | 604.736 | 440.869 | 694.196 | 573.135 |
| Pebble 원천 검증 포함 검색 | 1.431 | 1.469 | 1.846 | 2.191 | 1.999 | 3.035 |

진단 counter는 시간 표본 밖에서만 켰다. 최적화 query의 PostgreSQL CaptureReader 호출은 기존92단건+19배치=111회에서57단건+28배치=85회로 줄었다. 새 배치28개는 node19개와 metadata9개다. node 조회 자체는 단건5회+node 배치19회=24회로 이전과 같고, 이번 감소는 원천/메타데이터 경로에서 발생했다. 전체 배치 입력129keys·반환 key/value44,906bytes였다. 최종 query의 CaptureView는16회다. 변경 전 바이너리는 Views counter가 없으므로 그 JSON에서 전후 view 수를 비교하지 않는다. 코드/테스트상 블록별 ReadCaptured의 입력 경로는 사전 검증/Read/사후 검증3개 view에서 통합 입력/사후 검증2개 view로 줄었다. 이 카운터는 모든 SQL 문장/transaction begin·commit·ReadIndexingPage를 세는 수치가 아니다.

같은 최종 코드에서 optional batch capability를 숨긴 Get-only control도 동일16행을 반환했다. SQL Get-only control의 단건175→167회는 input snapshot 통합의 효과이고, 실제 optimized path에는 작은 metadata 배치도 포함한다. 새로운 query p50은 이번 실행의 변경 전/후를 비교한 값이며 이전 날짜의 수치를 그대로 기준 삼지 않는다.20표본 p99는 최대 표본이고 tail/SLO 추정치가 아니다. 실제 Arc ingest/450ms/24시간 보존은 별도로 남는다.

후속 작업에서 facts Source.BlockWithReceipts도 새 경로에 연결하고 coverage 오류·큰 객체 스트리밍·부모 자료·era/replay·commit fresh·처리 ACK 경계를 확인했다. [실제 writer 전후 측정](SOURCE_FACTS.md#실제-facts-writer-전후-측정-2026-10-17)은 단독 읽기와 writer 내부 대기·최종 처리량을 구분한다. 이 절의 검색 비교 증거는 `/private/tmp/indexer-input-read-20261017/validation.json`·`measurements.json`과 SQL/Linux/동시성/비교 로그에 있다. UUID 컨테이너·명시적 볼륨·비교/Linux 바이너리는 소유 자원만 정리했다.
