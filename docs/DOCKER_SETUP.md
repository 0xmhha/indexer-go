# Docker Compose 설정 가이드

Stable-One 노드와 indexer-go를 동일한 호스트에서 실행하기 위한 단계별 안내입니다. 로컬 개발 환경뿐 아니라 운영 환경에서도 재현 가능한 구성을 목표로 합니다.

**Last Updated**: 2025-11-20

---

## 목차

1. [개요](#개요)
2. [시스템 요구사항](#시스템-요구사항)
3. [빠른 시작](#빠른-시작)
4. [서비스 구성](#서비스-구성)
5. [환경 변수와 설정](#환경-변수와-설정)
6. [네트워크 구성](#네트워크-구성)
7. [볼륨 및 데이터 관리](#볼륨-및-데이터-관리)
8. [운영 가이드](#운영-가이드)
9. [트러블슈팅](#트러블슈팅)
10. [고급 설정](#고급-설정)
11. [다음 단계와 참고 자료](#다음-단계와-참고-자료)

---

## 개요

이 Compose 스택은 두 개의 핵심 서비스를 포함합니다.

- Stable-One Node: Geth 기반 Ethereum 호환 노드. 블록 데이터를 인덱서가 가져갈 수 있도록 RPC/WS 엔드포인트를 노출합니다.
- Indexer: indexer-go 애플리케이션. Stable-One RPC를 사용해 블록을 수집하고 GraphQL/JSON-RPC/WebSocket API를 제공합니다.

### 아키텍처 개요

```
┌─────────────────────────────────────────────────────────┐
│                    Docker Network                       │
│                                                         │
│  ┌──────────────────┐         ┌──────────────────┐      │
│  │  Stable-One Node │◄───────►│      Indexer     │      │
│  │   (Geth)         │  RPC    │    (indexer-go)  │      │
│  └──────────────────┘         └──────────────────┘      │
│        ▲                              ▲                │
│        │                              │                │
│  ┌─────┴─────┐                ┌───────┴───────┐        │
│  │blockchain │                │ indexer-data  │        │
│  │  volume   │                │   volume      │        │
│  └───────────┘                └───────────────┘        │
└─────────────────────────────────────────────────────────┘
```

---

## 시스템 요구사항

### 최소 사양

- CPU 4 cores
- RAM 8 GB
- 디스크 500 GB SSD (블록체인 데이터 증가 대비 확장 필요)
- OS: Linux, macOS, Windows (WSL2)

### 권장 사양

- CPU 8 cores 이상
- RAM 16 GB 이상
- 디스크 1 TB 이상 NVMe SSD
- 네트워크 10 Mbps 이상 업/다운

### 필수 소프트웨어

- Docker Engine 20.10+
- Docker Compose 2.0+
- Git 2.30+

---

## 빠른 시작

1. **저장소 가져오기**
   ```bash
   git clone https://github.com/0xmhha/indexer-go.git
   cd indexer-go
   ```
2. **환경 변수 파일 복사**
   ```bash
   cp .env.example .env
   ```
3. **필요 시 .env 수정**
   - RPC 엔드포인트, 인덱서 옵션 등을 환경에 맞게 조정합니다.
4. **Compose 스택 실행**
   ```bash
   docker-compose up -d
   ```
5. **상태 확인**
   ```bash
   docker-compose ps
   curl http://localhost:8080/health
   ```

---

## 서비스 구성

### Stable-One Node

- 이미지: `ethereum/client-go:stable`
- 노출 포트: 8545 (HTTP), 8546 (WebSocket), 30303 (P2P)
- 주요 옵션: `--syncmode snap`, `--cache 2048`, `--maxpeers 50`
- 헬스 체크: `geth attach --exec "eth.blockNumber"`

### Indexer

- 빌드 대상: 리포지토리 루트의 `Dockerfile`
- 노출 포트: 8080 (GraphQL, JSON-RPC, WebSocket)
- 기본 설정: `workers=100`, `chunkSize=100`, `startHeight=0`
- 헬스 체크: `wget --spider http://localhost:8080/health`

---

## 환경 변수와 설정

### Stable-One 노드

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `GETH_NETWORK` | mainnet | 사용할 네트워크 (mainnet/testnet/devnet) |

### Indexer RPC

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_RPC_ENDPOINT` | http://stable-one:8545 | Stable-One HTTP RPC 주소 |
| `INDEXER_RPC_TIMEOUT` | 30s | RPC 요청 타임아웃 |

### 데이터베이스

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_DB_PATH` | /data | PebbleDB 데이터 경로 |
| `INDEXER_DB_READONLY` | false | 읽기 전용 모드 사용 여부 |

### 인덱서 동작

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_WORKERS` | 100 | 블록 처리 워커 수 |
| `INDEXER_CHUNK_SIZE` | 100 | 동시 요청 블록 개수 |
| `INDEXER_START_HEIGHT` | 0 | 시작 블록 높이 |

### API 서버

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_API_ENABLED` | true | API 서버 활성화 |
| `INDEXER_API_HOST` | 0.0.0.0 | 바인드 주소 |
| `INDEXER_API_PORT` | 8080 | 포트 번호 |
| `INDEXER_API_GRAPHQL` | true | GraphQL 노출 여부 |
| `INDEXER_API_JSONRPC` | true | JSON-RPC 노출 여부 |
| `INDEXER_API_WEBSOCKET` | true | WebSocket 노출 여부 |

### 로깅

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_LOG_LEVEL` | info | 로그 레벨 (debug/info/warn/error)
| `INDEXER_LOG_FORMAT` | json | 로그 포맷 (json/text)

### .env 예시

```bash
GETH_NETWORK=mainnet
INDEXER_RPC_ENDPOINT=http://stable-one:8545
INDEXER_RPC_TIMEOUT=30s
INDEXER_DB_PATH=/data
INDEXER_WORKERS=100
INDEXER_CHUNK_SIZE=100
INDEXER_START_HEIGHT=0
INDEXER_LOG_LEVEL=info
INDEXER_LOG_FORMAT=json
INDEXER_API_ENABLED=true
INDEXER_API_HOST=0.0.0.0
INDEXER_API_PORT=8080
INDEXER_API_GRAPHQL=true
INDEXER_API_JSONRPC=true
INDEXER_API_WEBSOCKET=true
```

---

## 네트워크 구성

- 네트워크 이름: `indexer-network`
- 드라이버: `bridge`
- 기본 서브넷: `172.25.0.0/16`

### 서비스 간 통신 경로

- Indexer → Stable-One HTTP RPC: `http://stable-one:8545`
- Indexer → Stable-One WS RPC: `ws://stable-one:8546`
- 외부 → Stable-One RPC: `http://localhost:8545`
- 외부 → Indexer API: `http://localhost:8080`

### 포트 매핑

| 서비스 | 컨테이너 포트 | 호스트 포트 | 용도 |
|--------|---------------|-------------|------|
| stable-one | 8545 | 8545 | HTTP RPC |
| stable-one | 8546 | 8546 | WebSocket RPC |
| stable-one | 30303 | 30303 | P2P |
| indexer | 8080 | 8080 | GraphQL/JSON-RPC/WebSocket |

---

## 볼륨 및 데이터 관리

### 볼륨 정의

| 볼륨 | 마운트 위치 | 용도 |
|------|-------------|------|
| `blockchain-data` | `/root/.ethereum` | 블록체인 동기화 데이터 |
| `data` | `/data` | 인덱싱된 데이터 |

### 주요 명령

```bash
# 볼륨 목록
docker volume ls

# 볼륨 정보
docker volume inspect indexer-go_blockchain-data

# 디스크 사용량
docker system df -v
```

### 백업과 복원

```bash
# blockchain-data 백업
docker run --rm \
  -v indexer-go_blockchain-data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/blockchain-backup.tar.gz /data

# indexer 데이터 백업
docker run --rm \
  -v indexer-go_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/indexer-backup.tar.gz /data

# 복원 예시
docker run --rm \
  -v indexer-go_data:/data \
  -v $(pwd):/backup \
  alpine sh -c "cd / && tar xzf /backup/indexer-backup.tar.gz"
```

### 볼륨 정리 (데이터 삭제)

```bash
docker-compose down -v
```

---

## 운영 가이드

### 기동과 종료

```bash
# 백그라운드 실행
docker-compose up -d

# 포그라운드 실행
docker-compose up

# 전체 중지
docker-compose down
```

### 특정 서비스 제어

```bash
# 특정 서비스 기동
docker-compose up -d stable-one
docker-compose up -d indexer

# 특정 서비스 중지
docker-compose stop stable-one
docker-compose stop indexer

# 특정 서비스 재시작
docker-compose restart indexer
```

### 로그와 상태 확인

```bash
# 전체 로그
docker-compose logs -f

# 특정 서비스 로그
docker-compose logs -f stable-one

# 최근 100줄
docker-compose logs --tail=100

# 컨테이너 상태
docker-compose ps

# 리소스 사용량
docker stats
```

### 컨테이너 접근

```bash
# Stable-One 쉘
docker-compose exec stable-one sh

# Indexer 쉘
docker-compose exec indexer sh

# Geth 콘솔
docker-compose exec stable-one geth attach http://localhost:8545
```

### 이미지 재빌드

```bash
# 전체 재빌드
docker-compose up -d --build

# Indexer만 재빌드
docker-compose build indexer
```

---

## 트러블슈팅

### Stable-One 노드가 종료되는 경우

1. 포트 충돌 확인: `lsof -i :8545` 등으로 사용 중인 프로세스를 종료합니다.
2. 디스크 공간 확인: `df -h` 명령으로 여유 공간 확보.
3. 권한 문제: 볼륨 권한을 `docker volume inspect`로 확인하고 필요 시 `chown` 실행.
4. 로그 확인: `docker-compose logs stable-one`.

### Indexer가 RPC에 연결하지 못하는 경우

- `.env`의 `INDEXER_RPC_ENDPOINT` 값 확인.
- Stable-One 컨테이너가 정상적으로 8545/8546을 노출하는지 `docker-compose ps`로 확인.
- 방화벽에서 로컬 포트를 차단하지 않는지 점검.

### 데이터가 누락되는 경우

- `INDEXER_START_HEIGHT` 설정이 의도한 범위인지 확인.
- `INDEXER_WORKERS` 값이 과도하면 RPC가 rate limit에 걸릴 수 있으므로 50 이하로 낮춰 테스트.
- `docker stats`로 메모리 사용량을 확인하고 부족하면 호스트 리소스를 증설.

### 볼륨 손상 대응

- 서비스 중지 후 백업 파일에서 복원합니다.
- 백업이 없다면 새 볼륨을 생성하고 Geth 동기화/인덱싱을 다시 수행해야 합니다.

---

## 고급 설정

### 리소스 제한

```yaml
services:
  stable-one:
    deploy:
      resources:
        limits:
          cpus: '4'
          memory: 8G
  indexer:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 4G
```

### 테스트넷 또는 프라이빗 네트워크

```yaml
services:
  stable-one:
    command:
      - --networkid=12345
    environment:
      - GETH_NETWORK=testnet
```

### 모니터링 스택 추가

```yaml
services:
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus
    networks:
      - indexer-network

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    volumes:
      - grafana-data:/var/lib/grafana
    networks:
      - indexer-network

volumes:
  prometheus-data:
  grafana-data:
```

### 보안 강화를 위한 프록시 예시

```yaml
services:
  nginx:
    image: nginx:alpine
    ports:
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf
      - ./certs:/etc/nginx/certs
    networks:
      - indexer-network
```

---

## 다음 단계와 참고 자료

### 다음 단계

1. Stable-One 노드 동기화가 완료될 때까지 모니터링합니다.
2. 인덱서 API (GraphQL/JSON-RPC/WebSocket)를 통해 데이터가 노출되는지 확인합니다.
3. Prometheus/Grafana 또는 선호하는 모니터링 스택을 통합합니다.
4. 운영 환경이라면 외부 RPC 접근 제어, TLS 프록시, 리소스 제한을 적용합니다.

### 참고 자료

- [Docker Compose 공식 문서](https://docs.docker.com/compose/)
- [Geth 문서](https://geth.ethereum.org/docs)
- [indexer-go README](../README.md)
- [Operations Guide](./OPERATIONS_GUIDE.md)
- [Metrics Monitoring Guide](./METRICS_MONITORING.md)
