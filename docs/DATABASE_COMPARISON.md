# Database Comparison for indexer-go

이 문서는 indexer-go가 사용할 임베디드 스토리지 엔진을 비교 분석하기 위해 작성되었습니다. 분석 대상은 PebbleDB, LevelDB, RocksDB, BadgerDB, BoltDB/bbolt, PostgreSQL/MySQL이며, 기능 요구사항과 제약을 기준으로 평가했습니다.

## 평가 기준

1. **임베디드 배포 가능 여부**: 별도 서버 없이 단일 바이너리로 배포 가능한가?
2. **쓰기 지향 워크로드 적합성**: 블록 인덱싱(80~150 blocks/s) 같은 고 쓰기 부하를 처리가 가능한가?
3. **Go 친화성**: CGO 의존도 없이 빌드/배포가 쉬운가?
4. **운영 안정성**: 프로덕션에서 검증되었는가, 유지보수가 활발한가?
5. **호환성**: 기존 tx-indexer 아키텍처와의 정합성이 있는가?

## 후보별 핵심 요약

| 후보 | 장점 요약 | 제약 사항 | 결론 |
|------|-----------|-----------|-------|
| PebbleDB | Go 네이티브, 높은 성능, CockroachDB 프로덕션 사용 | 상대적으로 새로운 프로젝트 | 우선 선택 |
| LevelDB | 널리 사용, 구현 단순 | C++ 코드베이스, 유지보수 중단, Go 1.20+ 빌드 문제 | 신규 프로젝트에 부적합 |
| RocksDB | 매우 높은 성능, 풍부한 기능 | C++ 및 CGO 의존, 설정 복잡 | 고급 기능이 반드시 필요할 때만 고려 |
| BadgerDB | Go 네이티브, 단순 배포 | 메모리 사용량 큼, 쓰기 성능 열세 | 메모리 여유가 충분할 때만 부분적 대안 |
| BoltDB/bbolt | 단순 API, ACID 보장 | B+tree 기반으로 쓰기 병목, 단일 writer | 소규모 임베디드 용도에 한정 |
| PostgreSQL/MySQL | 성숙한 생태계, 쿼리 능력 | 서버 운영 필요, KV 워크로드 비효율 | indexer-go 요구와 불일치 |

## PebbleDB (선택)

### 개요
PebbleDB는 CockroachDB 팀이 Go로 재작성한 LSM-tree 기반 key-value 스토어입니다. tx-indexer도 동일한 버전을 사용하므로 운영 경험을 재활용할 수 있습니다.

### 라이선스 및 제공 형태
- BSD-3-Clause, 상업적 사용에 제한 없음
- GitHub: https://github.com/cockroachdb/pebble

### 선택 이유
- `github.com/cockroachdb/pebble v1.1.5`를 tx-indexer에서 이미 사용 중이라 호환성 확보
- 순수 Go 구현이라 CGO 없이 크로스 컴파일 가능
- RocksDB 대비 역방향 반복과 동시성 처리 성능 개선
- CockroachDB 프로덕션에서 2020년 이후 대규모로 검증
- Column family 같은 불필요한 기능이 없어 학습 곡선이 낮음

## 대안 분석

### LevelDB
- 장점: 역사가 길고 검증된 구현, 레거시 프로젝트에서 다수 사용
- 단점: C++ 구현으로 CGO 필요, 장기간 유지보수 중단, Go 1.20 이상에서 빌드 이슈, 성능이 PebbleDB보다 낮음
- 결론: 레거시 호환성 목적이 아니라면 신규 프로젝트에서 선택할 이유가 없음

### RocksDB
- 장점: 최고 수준의 성능, column family/transaction 등 고급 기능 제공, Facebook이 유지보수
- 단점: C++ 및 CGO 의존으로 배포 복잡, 크로스 컴파일 어려움, API/설정 난이도 높음, 바이너리 크기 증가, Go 바인딩 안정성 부족
- 결론: 고급 기능이 필수 요건일 때만 고려하되 indexer-go 범위에서는 과도한 도입비용

### BadgerDB
- 장점: Go 네이티브, CGO 불필요, 단순한 API, Dgraph에서 개발
- 단점: 메모리 사용량이 높고 장시간 런닝 시 GC 비용 상승, 쓰기 성능이 PebbleDB 대비 열세, 커뮤니티 규모가 작음
- 결론: 메모리가 충분한 단순 워크로드에는 적합하지만 indexer-go의 고 쓰기 부하 요구에는 여유 없음

### BoltDB / bbolt
- 장점: Go 네이티브, 단일 파일 저장, ACID 트랜잭션 지원
- 단점: B+tree 구조로 쓰기 성능이 LSM 대비 낮고 단일 writer 제약, 대용량 데이터 처리에 부적합, 원본 BoltDB는 유지보수 종료(bbolt fork만 유지)
- 결론: 소규모 임베디드 DB 시나리오에 한정되며 블록 인덱서에는 맞지 않음

### PostgreSQL / MySQL
- 장점: 성숙한 생태계와 풍부한 관리 도구, SQL 쿼리 기능
- 단점: 서버 프로세스 필요, 설정/운영 복잡도 증가, key-value 위주 워크로드에서는 오버헤드가 큼
- 결론: indexer-go는 이미 GraphQL 레이어를 제공하므로 RDBMS를 추가하면 관리 비용만 늘어남

## 결론

필수 요구사항(임베디드, 고성능 쓰기, Go 네이티브, 안정성, tx-indexer와의 호환성)을 모두 만족하는 후보는 PebbleDB뿐입니다. 나머지 후보는 특정 영역에서 장점이 있으나 프로젝트 목표와 맞지 않거나 운영 복잡도를 크게 높입니다. 따라서 indexer-go의 기본 스토리지 엔진은 PebbleDB로 유지하고, 향후 기능 요구가 변화할 경우에만 RocksDB와 같은 대안을 재검토합니다.
