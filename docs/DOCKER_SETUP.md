# Docker Compose 설정 가이드

> Stable-One 노드와 Indexer를 Docker Compose로 실행하는 완전한 가이드

**Last Updated**: 2025-10-21

---

## 📋 목차

1. [개요](#개요)
2. [시스템 요구사항](#시스템-요구사항)
3. [Quick Start](#quick-start)
4. [서비스 구성](#서비스-구성)
5. [환경 변수](#환경-변수)
6. [네트워크 구성](#네트워크-구성)
7. [볼륨 관리](#볼륨-관리)
8. [사용 가이드](#사용-가이드)
9. [트러블슈팅](#트러블슈팅)
10. [고급 설정](#고급-설정)

---

## 개요

이 Docker Compose 설정은 다음을 제공합니다:

- **Stable-One Node**: Ethereum 호환 블록체인 노드 (Geth 기반)
- **Indexer**: 블록체인 데이터 인덱싱 및 API 서버
- **통합 환경**: 서비스 간 자동 네트워킹 및 의존성 관리
- **원클릭 실행**: 단일 명령어로 전체 스택 실행

### 아키텍처

```
┌─────────────────────────────────────────────────────────┐
│                    Docker Network                        │
│                  (172.25.0.0/16)                        │
│                                                          │
│  ┌──────────────────┐         ┌──────────────────┐     │
│  │  Stable-One Node │         │     Indexer      │     │
│  │   (Geth)         │◄────────│    (indexer-go)  │     │
│  │                  │  RPC    │                  │     │
│  │  - HTTP: 8545    │         │  - GraphQL: 8080 │     │
│  │  - WS: 8546      │         │  - JSON-RPC      │     │
│  │  - P2P: 30303    │         │  - WebSocket     │     │
│  └──────────────────┘         └──────────────────┘     │
│         ▲                              ▲                │
│         │                              │                │
│  ┌──────┴───────┐             ┌───────┴────────┐       │
│  │ blockchain-  │             │ indexer-data   │       │
│  │ data volume  │             │ volume         │       │
│  └──────────────┘             └────────────────┘       │
└─────────────────────────────────────────────────────────┘
```

---

## 시스템 요구사항

### 최소 요구사항

```yaml
CPU: 4 cores
RAM: 8 GB
Disk: 500 GB SSD (블록체인 데이터 증가에 따라 확장)
OS: Linux, macOS, Windows (WSL2)
```

### 권장 사양

```yaml
CPU: 8+ cores
RAM: 16+ GB
Disk: 1+ TB NVMe SSD
Network: 10+ Mbps (업로드/다운로드)
```

### 소프트웨어 요구사항

- Docker Engine 20.10+
- Docker Compose 2.0+
- 최소 500 GB 여유 디스크 공간

---

## Quick Start

### 1. 저장소 클론

```bash
git clone https://github.com/0xmhha/indexer-go.git
cd indexer-go
```

### 2. 환경 설정 파일 생성

```bash
cp .env.example .env
```

### 3. 서비스 시작

```bash
docker-compose up -d
```

### 4. 로그 확인

```bash
# 모든 서비스 로그
docker-compose logs -f

# Stable-One 노드 로그
docker-compose logs -f stable-one

# Indexer 로그
docker-compose logs -f indexer
```

### 5. 서비스 상태 확인

```bash
# Stable-One RPC 확인
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'

# Indexer Health Check
curl http://localhost:8080/health

# GraphQL Playground
open http://localhost:8080/graphql
```

---

## 서비스 구성

### Stable-One Node

```yaml
Service Name: stable-one
Image: ethereum/client-go:stable
Container Name: stable-one-node
```

#### 포트

| 포트 | 프로토콜 | 용도 |
|------|---------|------|
| 8545 | HTTP | JSON-RPC API |
| 8546 | WebSocket | WebSocket API |
| 30303 | TCP/UDP | P2P 네트워킹 |

#### 주요 설정

```yaml
Sync Mode: snap (빠른 동기화)
Cache: 2048 MB
Max Peers: 50
Network: mainnet (기본값)
```

#### 헬스 체크

```bash
Test: geth attach --exec "eth.blockNumber"
Interval: 30초
Timeout: 10초
Retries: 5
Start Period: 5분
```

### Indexer Service

```yaml
Service Name: indexer
Build: Dockerfile
Container Name: indexer-go
```

#### 포트

| 포트 | 프로토콜 | 용도 |
|------|---------|------|
| 8080 | HTTP | GraphQL + JSON-RPC + WebSocket |

#### 주요 설정

```yaml
RPC Endpoint: http://stable-one:8545
Workers: 100
Chunk Size: 100
Start Height: 0 (제네시스부터)
```

#### 헬스 체크

```bash
Test: wget --spider http://localhost:8080/health
Interval: 30초
Timeout: 10초
Retries: 3
Start Period: 40초
```

---

## 환경 변수

### Stable-One 노드

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `GETH_NETWORK` | mainnet | 네트워크 선택 (mainnet/testnet/devnet) |

### Indexer

#### RPC 설정

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_RPC_ENDPOINT` | http://stable-one:8545 | Ethereum RPC 엔드포인트 |
| `INDEXER_RPC_TIMEOUT` | 30s | RPC 요청 타임아웃 |

#### 데이터베이스 설정

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_DB_PATH` | /data | 데이터베이스 경로 |
| `INDEXER_DB_READONLY` | false | 읽기 전용 모드 |

#### 인덱서 설정

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_WORKERS` | 100 | 병렬 워커 수 |
| `INDEXER_CHUNK_SIZE` | 100 | 블록 청크 크기 |
| `INDEXER_START_HEIGHT` | 0 | 시작 블록 높이 |

#### API 서버 설정

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_API_ENABLED` | true | API 서버 활성화 |
| `INDEXER_API_HOST` | 0.0.0.0 | API 서버 호스트 |
| `INDEXER_API_PORT` | 8080 | API 서버 포트 |
| `INDEXER_API_GRAPHQL` | true | GraphQL API 활성화 |
| `INDEXER_API_JSONRPC` | true | JSON-RPC API 활성화 |
| `INDEXER_API_WEBSOCKET` | true | WebSocket API 활성화 |

#### 로깅 설정

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `INDEXER_LOG_LEVEL` | info | 로그 레벨 (debug/info/warn/error) |
| `INDEXER_LOG_FORMAT` | json | 로그 포맷 (json/text) |

### .env 파일 예제

```bash
# Stable-One 노드 설정
GETH_NETWORK=mainnet

# Indexer 설정
INDEXER_RPC_ENDPOINT=http://stable-one:8545
INDEXER_RPC_TIMEOUT=30s
INDEXER_DB_PATH=/data
INDEXER_WORKERS=100
INDEXER_CHUNK_SIZE=100
INDEXER_START_HEIGHT=0
INDEXER_LOG_LEVEL=info
INDEXER_LOG_FORMAT=json

# API 서버 설정
INDEXER_API_ENABLED=true
INDEXER_API_HOST=0.0.0.0
INDEXER_API_PORT=8080
INDEXER_API_GRAPHQL=true
INDEXER_API_JSONRPC=true
INDEXER_API_WEBSOCKET=true
```

---

## 네트워크 구성

### 네트워크 설정

```yaml
Network Name: indexer-network
Driver: bridge
Subnet: 172.25.0.0/16
```

### 서비스 간 통신

- **Indexer → Stable-One**: `http://stable-one:8545` (HTTP RPC)
- **Indexer → Stable-One**: `ws://stable-one:8546` (WebSocket)
- **외부 → Stable-One**: `http://localhost:8545`
- **외부 → Indexer**: `http://localhost:8080`

### 포트 포워딩

| 서비스 | 컨테이너 포트 | 호스트 포트 | 프로토콜 |
|--------|--------------|------------|----------|
| stable-one | 8545 | 8545 | HTTP |
| stable-one | 8546 | 8546 | WebSocket |
| stable-one | 30303 | 30303 | TCP/UDP |
| indexer | 8080 | 8080 | HTTP |

---

## 볼륨 관리

### 볼륨 구성

```yaml
blockchain-data:
  Type: named volume
  Mount: /root/.ethereum (Stable-One 컨테이너)
  Purpose: 블록체인 데이터 저장

data:
  Type: named volume
  Mount: /data (Indexer 컨테이너)
  Purpose: 인덱싱된 데이터 저장
```

### 볼륨 명령어

#### 볼륨 조회

```bash
docker volume ls
```

#### 볼륨 상세 정보

```bash
docker volume inspect indexer-go_blockchain-data
docker volume inspect indexer-go_data
```

#### 볼륨 사용량 확인

```bash
docker system df -v
```

#### 볼륨 백업

```bash
# Blockchain 데이터 백업
docker run --rm \
  -v indexer-go_blockchain-data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/blockchain-backup.tar.gz /data

# Indexer 데이터 백업
docker run --rm \
  -v indexer-go_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/indexer-backup.tar.gz /data
```

#### 볼륨 복원

```bash
# Blockchain 데이터 복원
docker run --rm \
  -v indexer-go_blockchain-data:/data \
  -v $(pwd):/backup \
  alpine sh -c "cd / && tar xzf /backup/blockchain-backup.tar.gz"

# Indexer 데이터 복원
docker run --rm \
  -v indexer-go_data:/data \
  -v $(pwd):/backup \
  alpine sh -c "cd / && tar xzf /backup/indexer-backup.tar.gz"
```

#### 볼륨 정리 (⚠️ 주의: 데이터 삭제)

```bash
# 서비스 중지 및 볼륨 삭제
docker-compose down -v

# 특정 볼륨만 삭제
docker volume rm indexer-go_blockchain-data
docker volume rm indexer-go_data
```

---

## 사용 가이드

### 서비스 시작

```bash
# 백그라운드로 시작
docker-compose up -d

# 포그라운드로 시작 (로그 실시간 확인)
docker-compose up

# 특정 서비스만 시작
docker-compose up -d stable-one
docker-compose up -d indexer
```

### 서비스 중지

```bash
# 모든 서비스 중지 (볼륨 유지)
docker-compose down

# 모든 서비스 중지 및 볼륨 삭제
docker-compose down -v

# 특정 서비스만 중지
docker-compose stop stable-one
docker-compose stop indexer
```

### 서비스 재시작

```bash
# 모든 서비스 재시작
docker-compose restart

# 특정 서비스만 재시작
docker-compose restart stable-one
docker-compose restart indexer
```

### 로그 확인

```bash
# 모든 서비스 로그 (실시간)
docker-compose logs -f

# 특정 서비스 로그
docker-compose logs -f stable-one
docker-compose logs -f indexer

# 최근 100줄 로그
docker-compose logs --tail=100

# 타임스탬프 포함
docker-compose logs -t
```

### 서비스 상태 확인

```bash
# 서비스 상태
docker-compose ps

# 상세 상태 정보
docker-compose ps -a

# 리소스 사용량
docker stats
```

### 컨테이너 접속

```bash
# Stable-One 컨테이너 접속
docker-compose exec stable-one sh

# Indexer 컨테이너 접속
docker-compose exec indexer sh

# Geth 콘솔 접속
docker-compose exec stable-one geth attach http://localhost:8545
```

### 빌드 및 재배포

```bash
# 이미지 재빌드
docker-compose build

# 재빌드 후 시작
docker-compose up -d --build

# 특정 서비스만 재빌드
docker-compose build indexer
docker-compose up -d indexer
```

---

## 트러블슈팅

### 일반적인 문제

#### 1. Stable-One 노드가 시작되지 않음

**증상**:
```
stable-one-node exited with code 1
```

**원인**:
- 포트 충돌 (8545, 8546, 30303)
- 디스크 공간 부족
- 권한 문제

**해결책**:
```bash
# 포트 사용 확인
lsof -i :8545
lsof -i :8546
lsof -i :30303

# 디스크 공간 확인
df -h

# 볼륨 권한 확인
docker volume inspect indexer-go_blockchain-data

# 로그 확인
docker-compose logs stable-one
```

#### 2. Indexer가 Stable-One에 연결되지 않음

**증상**:
```
Failed to connect to RPC endpoint
```

**원인**:
- Stable-One 노드가 아직 준비되지 않음
- 네트워크 설정 문제
- RPC 엔드포인트 설정 오류

**해결책**:
```bash
# Stable-One 상태 확인
docker-compose ps stable-one

# RPC 연결 테스트
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'

# 네트워크 확인
docker network inspect indexer-go_indexer-network

# Indexer 재시작
docker-compose restart indexer
```

#### 3. 동기화가 너무 느림

**증상**:
- 블록 동기화 속도가 매우 느림

**원인**:
- Snap 동기화 초기 단계
- 네트워크 속도 제한
- 디스크 I/O 병목

**해결책**:
```bash
# Geth 동기화 상태 확인
docker-compose exec stable-one geth attach http://localhost:8545 \
  --exec "eth.syncing"

# 피어 수 확인
docker-compose exec stable-one geth attach http://localhost:8545 \
  --exec "net.peerCount"

# 캐시 크기 증가 (docker-compose.yml 수정)
# --cache=4096

# 피어 수 증가 (docker-compose.yml 수정)
# --maxpeers=100
```

#### 4. 메모리 부족

**증상**:
```
OOMKilled
```

**원인**:
- 할당된 메모리 부족
- 캐시 크기가 너무 큼

**해결책**:
```bash
# 메모리 사용량 확인
docker stats

# 캐시 크기 감소 (docker-compose.yml)
# --cache=1024

# Docker 메모리 제한 설정
# deploy:
#   resources:
#     limits:
#       memory: 8G
```

#### 5. 디스크 공간 부족

**증상**:
```
no space left on device
```

**해결책**:
```bash
# 디스크 사용량 확인
df -h
docker system df

# 로그 정리
docker-compose logs --tail=0 stable-one > /dev/null
docker-compose logs --tail=0 indexer > /dev/null

# 사용하지 않는 이미지 정리
docker image prune -a

# 사용하지 않는 볼륨 정리
docker volume prune
```

### 헬스 체크 실패

```bash
# Stable-One 헬스 체크
docker-compose exec stable-one geth attach http://localhost:8545 \
  --exec "eth.blockNumber"

# Indexer 헬스 체크
curl http://localhost:8080/health

# 헬스 체크 로그 확인
docker inspect stable-one-node | jq '.[0].State.Health'
docker inspect indexer-go | jq '.[0].State.Health'
```

### 네트워크 문제 디버깅

```bash
# 네트워크 상세 정보
docker network inspect indexer-go_indexer-network

# 컨테이너 간 연결 테스트
docker-compose exec indexer ping stable-one

# DNS 확인
docker-compose exec indexer nslookup stable-one
```

---

## 고급 설정

### 프로덕션 환경 최적화

#### 리소스 제한 설정

```yaml
services:
  stable-one:
    deploy:
      resources:
        limits:
          cpus: '4'
          memory: 8G
        reservations:
          cpus: '2'
          memory: 4G

  indexer:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 4G
        reservations:
          cpus: '1'
          memory: 2G
```

#### 로깅 최적화

```yaml
logging:
  driver: "json-file"
  options:
    max-size: "50m"
    max-file: "5"
    compress: "true"
```

### 네트워크 변경

#### Testnet 사용

```yaml
# docker-compose.yml
services:
  stable-one:
    command:
      - --goerli  # 또는 --sepolia
      # ... 기타 옵션
    environment:
      - GETH_NETWORK=testnet
```

#### Private Network 사용

```yaml
services:
  stable-one:
    command:
      - --networkid=12345
      - --datadir=/root/.ethereum/private
      # ... 기타 옵션
    volumes:
      - ./genesis.json:/genesis.json
      - blockchain-data:/root/.ethereum/private
```

### 모니터링 통합

#### Prometheus + Grafana 추가

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

### 보안 강화

#### 외부 RPC 접근 제한

```yaml
services:
  stable-one:
    ports:
      # 외부 노출 제거, 내부 네트워크만 사용
      # - "8545:8545"
      # - "8546:8546"
      - "30303:30303"  # P2P는 유지
```

#### TLS/SSL 설정

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

## 다음 단계

1. ✅ **기본 설정 완료**: Docker Compose로 서비스 시작
2. ⏳ **동기화 대기**: Stable-One 노드가 블록 동기화 (수 시간 소요)
3. ⏳ **인덱싱 시작**: Indexer가 블록 데이터 수집 시작
4. ⏳ **API 테스트**: GraphQL/JSON-RPC API 테스트
5. ⏳ **모니터링 설정**: Prometheus + Grafana 대시보드 구성
6. ⏳ **프로덕션 배포**: 보안 및 최적화 적용

---

## 참고 자료

- [Docker Compose 공식 문서](https://docs.docker.com/compose/)
- [Geth 공식 문서](https://geth.ethereum.org/docs)
- [Indexer-Go README](../README.md)
- [Operations Guide](./OPERATIONS_GUIDE.md)
- [Metrics Monitoring Guide](./METRICS_MONITORING.md)

---

**마지막 업데이트**: 2025-10-21
**버전**: 1.0.0
**작성자**: Indexer-Go Team
