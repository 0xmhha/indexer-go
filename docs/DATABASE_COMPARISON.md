# Database Comparison for indexer-go

## PebbleDB - Our Choice

### What is PebbleDB?

**PebbleDB**는 CockroachDB에서 개발한 **Go 네이티브 key-value 스토어**입니다.
- LevelDB와 RocksDB에서 영감을 받아 Go로 완전히 재작성
- LSM-tree (Log-Structured Merge-tree) 기반 아키텍처
- 2020년부터 CockroachDB의 기본 스토리지 엔진으로 프로덕션 사용 중

### License: BSD-3-Clause (무료 오픈소스)

✅ **완전히 무료이며 상업적 사용 가능**
- BSD-3-Clause 라이센스
- 저작권 표시만 필요
- 유료 라이센스 없음
- GitHub: https://github.com/cockroachdb/pebble

### Why PebbleDB?

#### 1. tx-indexer와 동일한 선택
```go
// tx-indexer/go.mod
github.com/cockroachdb/pebble v1.1.5
```
- tx-indexer (Gno chain)가 이미 검증하고 사용 중
- 유사한 아키텍처이므로 동일한 선택이 합리적

#### 2. Go 네이티브
- Go로 작성되어 CGO 불필요
- 크로스 컴파일 용이
- Go 생태계와 완벽한 통합
- 디버깅과 프로파일링 쉬움

#### 3. 높은 성능
- RocksDB 대비 개선된 역방향 반복 (reverse iteration)
- 더 나은 동시성 처리
- L0 sublevels로 읽기 증폭 감소
- 쓰기 부하가 높을 때 더 좋은 성능

#### 4. 프로덕션 검증
- CockroachDB에서 2020년부터 대규모 프로덕션 사용
- 안정성 검증 완료
- 활발한 유지보수

#### 5. 간결한 API
- RocksDB보다 단순한 API
- Column families, transactions 등 불필요한 기능 제거
- 학습 곡선 낮음

## 대안 비교

### 1. LevelDB

**장점:**
- 가장 오래되고 안정적
- 많은 프로젝트에서 사용

**단점:**
- ❌ C++로 작성되어 CGO 필요
- ❌ 개발이 거의 중단됨
- ❌ 성능이 PebbleDB보다 낮음
- ❌ Go 1.20+ 빌드 이슈

**결론:** 레거시 선택, 새 프로젝트에 부적합

---

### 2. RocksDB

**장점:**
- 매우 높은 성능
- 풍부한 기능 (column families, transactions 등)
- Facebook에서 개발 및 유지보수

**단점:**
- ❌ C++로 작성되어 CGO 필요
- ❌ 크로스 컴파일 복잡
- ❌ 복잡한 API와 설정
- ❌ Go 바인딩 불안정
- ❌ 바이너리 크기 증가

**결론:** 고급 기능이 필요한 경우에만 고려

---

### 3. BadgerDB

**장점:**
- ✅ Go 네이티브
- ✅ 순수 Go, CGO 불필요
- DGRAPH에서 개발

**단점:**
- ❌ 메모리 사용량이 높음
- ❌ PebbleDB보다 느린 쓰기 성능
- ❌ 커뮤니티가 작음
- ❌ LSM-tree가 아닌 다른 구조

**결론:** 메모리가 충분하고 단순한 사용 사례에 적합

---

### 4. BoltDB / bbolt

**장점:**
- ✅ Go 네이티브
- ✅ 단순한 API
- ✅ ACID 트랜잭션

**단점:**
- ❌ B+tree 기반 (LSM-tree보다 쓰기 느림)
- ❌ 단일 쓰기 스레드 (동시성 제한)
- ❌ 대용량 데이터에 부적합
- ❌ 개발 중단 (etcd에서 fork한 bbolt는 유지 중)

**결론:** 소규모 임베디드 DB용, 블록체인 인덱서에 부적합

---

### 5. PostgreSQL / MySQL

**장점:**
- ✅ 매우 성숙한 생태계
- ✅ 풍부한 쿼리 기능
- ✅ 운영 도구 많음

**단점:**
- ❌ 별도 서버 필요 (임베디드 불가)
- ❌ 설정 및 운영 복잡도 증가
- ❌ 오버헤드 높음
- ❌ Key-value 워크로드에 최적화 안 됨

**결론:** GraphQL 레이어가 이미 있으므로 불필요한 복잡도

---

## 선택 기준

### 우리의 요구사항

1. **임베디드 데이터베이스**
   - 별도 서버 불필요
   - 단일 바이너리 배포

2. **높은 쓰기 성능**
   - 블록 인덱싱: 80-150 blocks/s
   - LSM-tree 구조 필수

3. **Go 네이티브**
   - CGO 없이 크로스 컴파일
   - 간편한 배포

4. **프로덕션 안정성**
   - 대규모 사용 사례 검증
   - 활발한 유지보수

5. **tx-indexer 호환성**
   - 유사한 아키텍처
   - 검증된 선택

### 요구사항별 점수

| Database   | 임베디드 | 쓰기 성능 | Go 네이티브 | 안정성 | tx-indexer | 총점 |
|------------|---------|----------|------------|--------|------------|------|
| **PebbleDB** | ✅ 5점 | ✅ 5점   | ✅ 5점     | ✅ 5점 | ✅ 5점     | **25점** |
| RocksDB    | ✅ 5점 | ✅ 5점   | ❌ 1점     | ✅ 5점 | ❌ 1점     | 17점 |
| BadgerDB   | ✅ 5점 | ⚠️ 3점   | ✅ 5점     | ⚠️ 3점 | ❌ 1점     | 17점 |
| LevelDB    | ✅ 5점 | ⚠️ 3점   | ❌ 1점     | ⚠️ 3점 | ❌ 1점     | 13점 |
| BoltDB     | ✅ 5점 | ❌ 2점   | ✅ 5점     | ⚠️ 3점 | ❌ 1점     | 16점 |
| PostgreSQL | ❌ 1점 | ⚠️ 3점   | ✅ 5점     | ✅ 5점 | ❌ 1점     | 15점 |

## PebbleDB 성능 특성

### LSM-tree 구조

```
Write Path:
User Write → MemTable → Immutable MemTable → L0 SSTable → Compaction → L1-L6

Read Path:
User Read → MemTable → L0 → L1 → ... → L6
```

### 성능 특성

**강점:**
- ✅ 순차 쓰기: 매우 빠름 (블록 인덱싱에 최적)
- ✅ 배치 쓰기: 높은 처리량
- ✅ 범위 스캔: 효율적
- ✅ 압축: Snappy 기본 지원

**약점:**
- ⚠️ 랜덤 읽기: B-tree보다 느림 (하지만 Bloom filter로 완화)
- ⚠️ 쓰기 증폭: Compaction 오버헤드
- ⚠️ 공간 증폭: 임시 공간 필요

**우리 워크로드에 적합한 이유:**
- 블록 인덱싱은 대부분 순차 쓰기
- 읽기는 주로 최근 블록 (MemTable/L0에서 처리)
- 범위 스캔 (address transactions) 빈번

## 실제 사용 사례

### CockroachDB
- 2020년부터 기본 스토리지 엔진
- 전세계 수천 개의 프로덕션 클러스터
- 페타바이트급 데이터 처리

### tx-indexer (Gno chain)
- Gno 블록체인 인덱서
- PebbleDB로 블록/트랜잭션 인덱싱
- 우리와 유사한 아키텍처

### go-ethereum (geth)
- go-ethereum도 PebbleDB를 선택적으로 지원
- LevelDB 대안으로 사용 가능

## 마이그레이션 경로

PebbleDB는 Storage 인터페이스 뒤에 숨겨져 있으므로, 필요시 다른 백엔드로 교체 가능:

```go
type Storage interface {
    Reader
    Writer
    Close() error
}

// 현재
storage := NewPebbleStorage(config)

// 미래에 필요하면
// storage := NewBadgerStorage(config)
// storage := NewPostgresStorage(config)
```

## 결론

**PebbleDB를 선택한 이유:**

1. ✅ **무료 오픈소스** (BSD-3-Clause)
2. ✅ **tx-indexer 검증** (동일한 선택)
3. ✅ **Go 네이티브** (CGO 불필요)
4. ✅ **높은 성능** (블록 인덱싱에 최적)
5. ✅ **프로덕션 검증** (CockroachDB)
6. ✅ **간결한 API** (학습 용이)
7. ✅ **활발한 개발** (지속적 개선)

**대안이 필요한 경우:**
- 메모리가 매우 제한적: BoltDB/bbolt 고려
- 복잡한 쿼리 필요: PostgreSQL 고려 (하지만 GraphQL이 이미 있음)
- 기존 RocksDB 인프라: RocksDB 유지

**현재로서는 PebbleDB가 최적의 선택입니다.**

## 참고 자료

- PebbleDB GitHub: https://github.com/cockroachdb/pebble
- CockroachDB 블로그: https://www.cockroachlabs.com/blog/pebble-rocksdb-kv-store/
- tx-indexer: https://github.com/gnolang/tx-indexer
- Go Database Comparison: https://github.com/dgraph-io/badger#benchmarks
