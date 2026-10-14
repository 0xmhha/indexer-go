# 빠른 시작

현재 범위는 EVM 체인이다. 설정 상세는 [CONFIG](CONFIG.md), 구조는 [ARCHITECTURE](ARCHITECTURE.md), 구현을 붙이는 방법은 [체인 확장 계약](CHAIN_CUSTOMIZATION.md)과 [SDK](SDK.md)에 있다.

## 1. 빌드

Go 모듈은 `go 1.24.0`, toolchain `go1.24.9`를 사용한다. Makefile이 실제 `pkg/app` 버전 필드에 빌드 정보를 넣는다.

```bash
go mod download
make build
./build/indexer-go --version
```

## 2. Full node 시작 조건

운영에 archive 노드는 없다. 기본 활성 기능 `balance.native`·`token.metadata`가 필요한 과거 상태를 노드가 보관해야 시작할 수 있다. 새 DB는 시작 블록 직전 상태, 재시작은 다음 블록 직전 상태, 새 기능은 backfill 시작 직전 상태를 확인한다.

1. RPC와 chain id를 확인한다.
2. 실제 RPC가 상태를 보관하는 최근 높이를 정한다. 보존 기간을 고정된 블록 수로 가정하지 않는다.
3. 그 상태 다음 블록을 시작 높이로 정하고 활성 기능을 확인한다.
4. 기존 DB를 재개할 때는 DB의 다음 블록과 기능 backfill 범위까지 확인한다.

최근 시작하면 이전 블록의 색인 결과는 없다. 처리 중에도 상태가 지워질 수 있다. `allow_pruned_state: false`(기본값)면 필요한 상태 조회 실패 시 블록 전체를 취소하고 같은 높이를 재시도한다. 사용자는 이 방침과 네 테스트넷 동시 실행을 선택했다. `true`를 명시하면 근사 처리를 허용하지만 과거 상태를 복원하지 않으며 잔액 또는 메타데이터 정확성을 떨어뜨릴 수 있다. 실제 보존 범위·허용 중단 시간·처리 성능은 운영 환경에서 확인해야 한다.

## 3. 설정과 실행

예를 들어 사용자 제공 Sepolia RPC로 새 DB를 만들 때 `config.yaml`을 다음과 같이 둔다. 기존 DB나 운영 구성을 바꾸는 예제가 아니다.

```yaml
rpc:
  endpoint: https://ethereum-sepolia-rpc.publicnode.com
  timeout: 15s
database:
  driver: pebble
  path: ./data-sepolia
indexer:
  workers: 1
  poll_interval: 50ms
api:
  enabled: true
  host: 127.0.0.1
  port: 8080
log:
  level: info
```

선택한 최근 시작 높이를 `INDEXER_START_HEIGHT`에 넣고 실행한다.

```bash
./build/indexer-go --config config.yaml \
  --start-height "${INDEXER_START_HEIGHT:?set a retained recent start height}"
```

Profile은 자동 감지된다. 등록된 이름이나 alias만 명시적으로 `--adapter`로 지정한다. generic `evm`, StableNet의 `stablenet`/`stableone`, Arc의 `arc`가 있다. Anvil은 generic EVM을 사용한다. 등록되지 않은 adapter 이름도 현재 자동 감지로 넘어가므로 오타와 실제 선택 로그를 확인한다.

Arc 예제는 [별도 profile 모듈](../examples/chainprofile/README.md)을 따른다. Arc profile은 tag `v0.2.15` 이후 main에 추가됐다. generic EVM 회계로 만든 기존 Arc DB는 새 profile로 재색인해야 하며 자동 보정되지 않는다.

## 4. 동작 확인

```bash
curl http://127.0.0.1:8080/health
curl -s http://127.0.0.1:8080/graphql \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ latestHeight }"}'
curl -s http://127.0.0.1:8080/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"getLatestHeight","params":{},"id":1}'
```

GraphQL Playground는 `/playground`다. 최근 시작 높이 이전의 블록을 조회할 수 있다고 가정하지 않는다. API 필드와 REST·구독 경로는 [API 안내](API.md)에 있다.

## 5. 운영 역할·멀티체인·데이터 관리

- API를 분리하려면 PostgreSQL과 `node.role: ingest`/`api` 구성을 사용한다. Pebble은 `all`/`ingest`만 지원한다.
- 멀티체인은 현재 `all` 역할만 지원하며 API가 `/chains/{id}/` 아래에 열린다. 실제 동시 운영 검증은 남은 작업이다.
- `--reindex`와 `--clear-data`는 데이터를 삭제하는 동작이다. 적용 범위·보존 데이터는 [CONFIG의 데이터 관리](CONFIG.md#data-management)를 확인하고 대상 DB와 재수집 가능한 범위를 먼저 정한다.
- 외부 배포·새 태그·원격 branch 삭제는 이번 문서 정리의 실행 범위가 아니다. 사용자 승인이 필요하다.

Docker·systemd 스크립트는 저장소에 있지만, 이 문서 정리에서 실제 배포를 검증하거나 실행하지 않았다. 운영 구성은 [남은 작업](analysis/open-items.md)의 R03/R04에서 확정한다. 사용자의 기존 노드·컨테이너를 이 안내의 실행 전제로 재설정하지 않는다.
