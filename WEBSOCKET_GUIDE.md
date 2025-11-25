# WebSocket Subscription Guide

이 문서는 프론트엔드에서 indexer-go의 GraphQL WebSocket 구독을 사용하는 방법을 설명합니다.

## 엔드포인트

```
ws://localhost:8545/graphql/ws
```

또는 HTTPS 환경:
```
wss://your-domain.com/graphql/ws
```

---

## 프로토콜

**graphql-transport-ws** (권장) 또는 **graphql-ws** 프로토콜 사용

---

## 1. JavaScript/TypeScript 연결 예시

### 기본 WebSocket 사용

```javascript
const ws = new WebSocket('ws://localhost:8545/graphql/ws', 'graphql-transport-ws');

ws.onopen = () => {
  console.log('WebSocket connected');

  // 1. Connection 초기화
  ws.send(JSON.stringify({
    type: 'connection_init'
  }));
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);
  console.log('Received:', message);

  switch (message.type) {
    case 'connection_ack':
      console.log('Connection acknowledged');
      // 이제 구독 시작
      subscribeToBlocks(ws);
      break;

    case 'next':
      // 실제 데이터 수신
      console.log('Data:', message.payload.data);
      // 예: message.payload.data.newBlock, message.payload.data.newTransaction 등
      break;

    case 'error':
      console.error('Error:', message.payload);
      break;
  }
};

ws.onerror = (error) => {
  console.error('WebSocket error:', error);
};

ws.onclose = () => {
  console.log('WebSocket disconnected');
};
```

---

## 2. 구독 타입별 사용법

### 2-1. 새 블록 구독 (newBlock)

```javascript
function subscribeToBlocks(ws) {
  const subscriptionId = 'block-sub-1';

  ws.send(JSON.stringify({
    id: subscriptionId,
    type: 'subscribe',
    payload: {
      query: `
        subscription {
          newBlock {
            number
            hash
            timestamp
            txCount
            parentHash
            miner
          }
        }
      `
    }
  }));
}
```

**수신 데이터 형식**:
```json
{
  "id": "block-sub-1",
  "type": "next",
  "payload": {
    "data": {
      "newBlock": {
        "number": 12345,
        "hash": "0xabc...",
        "timestamp": 1234567890,
        "txCount": 150,
        "parentHash": "0xdef...",
        "miner": "0x123..."
      }
    }
  }
}
```

---

### 2-2. 새 트랜잭션 구독 (newTransaction)

```javascript
function subscribeToTransactions(ws) {
  const subscriptionId = 'tx-sub-1';

  ws.send(JSON.stringify({
    id: subscriptionId,
    type: 'subscribe',
    payload: {
      query: `
        subscription {
          newTransaction {
            hash
            from
            to
            value
            blockNumber
          }
        }
      `
    }
  }));
}
```

**수신 데이터 형식**:
```json
{
  "id": "tx-sub-1",
  "type": "next",
  "payload": {
    "data": {
      "newTransaction": {
        "hash": "0x123...",
        "from": "0xabc...",
        "to": "0xdef...",
        "value": "1000000000000000000",
        "blockNumber": 12345
      }
    }
  }
}
```

---

### 2-3. 특정 계정 트랜잭션 구독 (필터 사용) ✅

**지원됨** - from/to 주소 기반 트랜잭션 필터링을 지원합니다.

```javascript
function subscribeToAccountTransactions(ws, accountAddress) {
  ws.send(JSON.stringify({
    id: 'account-tx-sub',
    type: 'subscribe',
    payload: {
      query: `
        subscription {
          newTransaction {
            hash
            from
            to
            value
            blockNumber
          }
        }
      `,
      variables: {
        filter: {
          from: accountAddress  // from 주소가 accountAddress인 트랜잭션만
          // 또는 to: accountAddress  // to 주소가 accountAddress인 트랜잭션만
          // 또는 둘 다 지정 가능
        }
      }
    }
  }));
}
```

---

### 2-4. 컨트랙트 이벤트 로그 구독 (logs)

```javascript
function subscribeToContractLogs(ws, contractAddress, eventSignature) {
  const subscriptionId = 'log-sub-1';

  ws.send(JSON.stringify({
    id: subscriptionId,
    type: 'subscribe',
    payload: {
      query: `
        subscription($filter: LogFilterInput) {
          logs(filter: $filter) {
            address
            topics
            data
            blockNumber
            transactionHash
            transactionIndex
            logIndex
            removed
          }
        }
      `,
      variables: {
        filter: {
          address: contractAddress,
          topics: [eventSignature]  // 예: Transfer 이벤트
        }
      }
    }
  }));
}
```

**사용 예시**:
```javascript
// ERC20 Transfer 이벤트 구독
const transferSignature = '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef';
subscribeToContractLogs(ws, '0x...token-address...', transferSignature);
```

**수신 데이터 형식**:
```json
{
  "id": "log-sub-1",
  "type": "next",
  "payload": {
    "data": {
      "logs": {
        "address": "0x123...",
        "topics": [
          "0xddf252ad...",
          "0x000...from",
          "0x000...to"
        ],
        "data": "0x...",
        "blockNumber": 12345,
        "transactionHash": "0xabc...",
        "transactionIndex": 5,
        "logIndex": 2,
        "removed": false
      }
    }
  }
}
```

---

### 2-5. 체인 설정 변경 구독 (chainConfig)

```javascript
function subscribeToChainConfig(ws) {
  const subscriptionId = 'chainconfig-sub-1';

  ws.send(JSON.stringify({
    id: subscriptionId,
    type: 'subscribe',
    payload: {
      query: `
        subscription {
          chainConfig {
            blockNumber
            blockHash
            parameter
            oldValue
            newValue
          }
        }
      `
    }
  }));
}
```

**수신 데이터 형식**:
```json
{
  "id": "chainconfig-sub-1",
  "type": "next",
  "payload": {
    "data": {
      "chainConfig": {
        "blockNumber": 12345,
        "blockHash": "0xabc...",
        "parameter": "gasLimit",
        "oldValue": "8000000",
        "newValue": "10000000"
      }
    }
  }
}
```

---

### 2-6. Validator 변경 구독 (validatorSet)

```javascript
function subscribeToValidatorSet(ws) {
  const subscriptionId = 'validator-sub-1';

  ws.send(JSON.stringify({
    id: subscriptionId,
    type: 'subscribe',
    payload: {
      query: `
        subscription {
          validatorSet {
            blockNumber
            blockHash
            changeType
            validator
            validatorSetSize
            validatorInfo
          }
        }
      `
    }
  }));
}
```

**수신 데이터 형식**:
```json
{
  "id": "validator-sub-1",
  "type": "next",
  "payload": {
    "data": {
      "validatorSet": {
        "blockNumber": 12345,
        "blockHash": "0xabc...",
        "changeType": "added",
        "validator": "0x123...",
        "validatorSetSize": 5,
        "validatorInfo": ""
      }
    }
  }
}
```

**changeType 값**:
- `"added"`: Validator가 추가됨
- `"removed"`: Validator가 제거됨
- `"updated"`: Validator 정보가 업데이트됨

---

### 2-7. Pending 트랜잭션 구독 (newPendingTransactions)

```javascript
function subscribeToPendingTransactions(ws) {
  const subscriptionId = 'pending-tx-sub-1';

  ws.send(JSON.stringify({
    id: subscriptionId,
    type: 'subscribe',
    payload: {
      query: `
        subscription {
          newPendingTransactions {
            hash
            from
            to
            value
            nonce
            gas
            gasPrice
            maxFeePerGas
            maxPriorityFeePerGas
            type
          }
        }
      `
    }
  }));
}
```

**수신 데이터 형식**:
```json
{
  "id": "pending-tx-sub-1",
  "type": "next",
  "payload": {
    "data": {
      "newPendingTransactions": {
        "hash": "0x123...",
        "from": "0xabc...",
        "to": "0xdef...",
        "value": "1000000000000000000",
        "nonce": 42,
        "gas": 21000,
        "gasPrice": "20000000000",
        "maxFeePerGas": "30000000000",
        "maxPriorityFeePerGas": "2000000000",
        "type": "0x2"
      }
    }
  }
}
```

**주의사항**:
- RPC 서버가 `newPendingTransactions` subscription을 지원해야 합니다
- Pending 트랜잭션은 블록에 포함되기 전 상태이므로 blockNumber, blockHash가 없습니다
- Stable-One은 블록 생성이 빠르므로 (1-2초), pending 상태가 매우 짧을 수 있습니다

---

## 3. 구독 중지

```javascript
function unsubscribe(ws, subscriptionId) {
  ws.send(JSON.stringify({
    id: subscriptionId,
    type: 'complete'
  }));
}
```

---

## 4. Apollo Client 사용 예시

```javascript
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { createClient } from 'graphql-ws';
import { ApolloClient, InMemoryCache } from '@apollo/client';

// WebSocket 클라이언트 생성
const wsClient = createClient({
  url: 'ws://localhost:8545/graphql/ws',
  connectionParams: {
    // 인증 토큰 등 추가 파라미터
  },
});

// GraphQL WS Link 생성
const wsLink = new GraphQLWsLink(wsClient);

// Apollo Client 생성
const client = new ApolloClient({
  link: wsLink,
  cache: new InMemoryCache(),
});

// 구독 사용
import { useSubscription, gql } from '@apollo/client';

const NEW_BLOCK_SUBSCRIPTION = gql`
  subscription OnNewBlock {
    newBlock {
      number
      hash
      timestamp
      txCount
    }
  }
`;

function BlockMonitor() {
  const { data, loading, error } = useSubscription(NEW_BLOCK_SUBSCRIPTION);

  if (loading) return <div>Connecting...</div>;
  if (error) return <div>Error: {error.message}</div>;

  return (
    <div>
      <h2>Latest Block: #{data.newBlock.number}</h2>
      <p>Hash: {data.newBlock.hash}</p>
      <p>Transactions: {data.newBlock.txCount}</p>
    </div>
  );
}
```

---

## 5. React Hooks 예시

```javascript
import { useState, useEffect } from 'react';

function useBlockSubscription() {
  const [block, setBlock] = useState(null);
  const [error, setError] = useState(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const ws = new WebSocket('ws://localhost:8545/graphql/ws', 'graphql-transport-ws');

    ws.onopen = () => {
      console.log('Connected');
      setConnected(true);

      // Connection init
      ws.send(JSON.stringify({ type: 'connection_init' }));
    };

    ws.onmessage = (event) => {
      const message = JSON.parse(event.data);

      if (message.type === 'connection_ack') {
        // 구독 시작
        ws.send(JSON.stringify({
          id: 'blocks',
          type: 'subscribe',
          payload: {
            query: `
              subscription {
                newBlock {
                  number
                  hash
                  timestamp
                  txCount
                }
              }
            `
          }
        }));
      } else if (message.type === 'next') {
        // 데이터 수신
        setBlock(message.payload.data.newBlock);
      } else if (message.type === 'error') {
        setError(message.payload);
      }
    };

    ws.onerror = (err) => {
      setError(err);
      setConnected(false);
    };

    ws.onclose = () => {
      setConnected(false);
    };

    return () => {
      ws.close();
    };
  }, []);

  return { block, error, connected };
}

// 컴포넌트에서 사용
function App() {
  const { block, error, connected } = useBlockSubscription();

  if (error) return <div>Error: {error}</div>;
  if (!connected) return <div>Connecting...</div>;
  if (!block) return <div>Waiting for blocks...</div>;

  return (
    <div>
      <h1>Block #{block.number}</h1>
      <p>Hash: {block.hash}</p>
      <p>Transactions: {block.txCount}</p>
    </div>
  );
}
```

---

## 6. 메시지 프로토콜 (graphql-transport-ws)

### 클라이언트 → 서버

| 메시지 타입 | 설명 | 페이로드 |
|------------|------|---------|
| `connection_init` | 연결 초기화 | 선택적 auth params |
| `subscribe` | 구독 시작 | `{ query, variables, operationName }` |
| `complete` | 구독 종료 | 없음 |
| `ping` | Keep-alive | 없음 |

### 서버 → 클라이언트

| 메시지 타입 | 설명 | 페이로드 |
|------------|------|---------|
| `connection_ack` | 연결 승인 | 없음 |
| `next` | 구독 데이터 | GraphQL 결과 데이터 |
| `error` | 에러 발생 | `[{ message: "..." }]` |
| `complete` | 구독 완료 | 없음 |
| `pong` | Ping 응답 | 없음 |

---

## 7. 디버깅 체크리스트

### 프론트엔드 체크

- [ ] WebSocket URL이 올바른가? (`ws://localhost:8545/graphql/ws`)
- [ ] Subprotocol을 지정했는가? (`graphql-transport-ws` 또는 `graphql-ws`)
- [ ] `connection_init` 메시지를 보냈는가?
- [ ] `connection_ack`를 받은 후 `subscribe`를 보냈는가?
- [ ] 구독 쿼리 문법이 올바른가?

### 백엔드 체크

- [ ] API 서버가 GraphQL과 WebSocket을 활성화했는가?
- [ ] EventBus가 실행 중인가?
- [ ] Fetcher가 블록을 인덱싱하고 있는가?
- [ ] 로그에 "GraphQL subscriptions enabled" 메시지가 있는가?

---

## 8. 브라우저 개발자 도구에서 테스트

```javascript
// 개발자 도구 콘솔에서 실행
const ws = new WebSocket('ws://localhost:8545/graphql/ws', 'graphql-transport-ws');

ws.onopen = () => {
  console.log('✅ Connected');
  ws.send(JSON.stringify({ type: 'connection_init' }));
};

ws.onmessage = (e) => {
  const msg = JSON.parse(e.data);
  console.log('📨 Received:', msg);

  if (msg.type === 'connection_ack') {
    console.log('✅ Connection acknowledged, subscribing to blocks...');
    ws.send(JSON.stringify({
      id: 'test-1',
      type: 'subscribe',
      payload: {
        query: 'subscription { newBlock { number hash txCount } }'
      }
    }));
  }
};

ws.onerror = (e) => console.error('❌ Error:', e);
ws.onclose = () => console.log('🔌 Disconnected');
```

**예상 출력**:
```
✅ Connected
📨 Received: {type: "connection_ack"}
✅ Connection acknowledged, subscribing to blocks...
📨 Received: {id: "test-1", type: "next", payload: {newBlock: {number: 12345, hash: "0x...", txCount: 150}}}
📨 Received: {id: "test-1", type: "next", payload: {newBlock: {number: 12346, hash: "0x...", txCount: 98}}}
...
```

---

## 9. 현재 지원되는 구독 타입

| 구독 타입 | 상태 | 설명 |
|---------|------|------|
| `newBlock` | ✅ 지원 | 새로운 블록 생성 시 실시간 전송 (miner 필드 포함) |
| `newTransaction` | ✅ 지원 | 모든 트랜잭션 실시간 전송 (from/to 필터 지원) |
| `logs` | ✅ 지원 | 컨트랙트 이벤트 로그 (필터 지원) |
| `chainConfig` | ✅ 지원 | 체인 설정 변경 이벤트 (예: gasLimit, chainId 변경) |
| `validatorSet` | ✅ 지원 | Validator 추가/제거/변경 이벤트 |
| `newPendingTransactions` | ✅ 지원 | Mempool의 대기 중인 트랜잭션 실시간 전송 |

---

## 10. 필터 사용 (logs만 지원)

### 특정 컨트랙트의 모든 이벤트

```javascript
ws.send(JSON.stringify({
  id: 'contract-logs',
  type: 'subscribe',
  payload: {
    query: `
      subscription($filter: LogFilterInput) {
        logs(filter: $filter) {
          address
          topics
          data
          blockNumber
          transactionHash
        }
      }
    `,
    variables: {
      filter: {
        address: "0x1234567890abcdef1234567890abcdef12345678"
      }
    }
  }
}));
```

### 특정 이벤트 시그니처 필터링

```javascript
// ERC20 Transfer 이벤트만 구독
const TRANSFER_SIGNATURE = '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef';

ws.send(JSON.stringify({
  id: 'transfer-events',
  type: 'subscribe',
  payload: {
    query: `
      subscription($filter: LogFilterInput) {
        logs(filter: $filter) {
          address
          topics
          data
          transactionHash
        }
      }
    `,
    variables: {
      filter: {
        address: "0x...token-contract-address...",
        topics: [TRANSFER_SIGNATURE]
      }
    }
  }
}));
```

### 블록 범위 지정

```javascript
ws.send(JSON.stringify({
  id: 'ranged-logs',
  type: 'subscribe',
  payload: {
    query: `subscription($filter: LogFilterInput) { logs(filter: $filter) { ... } }`,
    variables: {
      filter: {
        address: "0x...",
        fromBlock: 10000,
        toBlock: 20000
      }
    }
  }
}));
```

---

## 11. 에러 처리

### 일반적인 에러

**503 Service Unavailable**:
```json
{
  "id": "sub-1",
  "type": "error",
  "payload": [{
    "message": "subscriptions not available"
  }]
}
```
→ EventBus가 설정되지 않음. 서버 로그 확인 필요.

**Invalid payload**:
```json
{
  "id": "sub-1",
  "type": "error",
  "payload": [{
    "message": "invalid payload"
  }]
}
```
→ 구독 쿼리 문법 오류. `payload.query` 확인.

**Unknown subscription type**:
```json
{
  "id": "sub-1",
  "type": "error",
  "payload": [{
    "message": "invalid subscription query"
  }]
}
```
→ 지원하지 않는 구독 타입. `newBlock`, `newTransaction`, `logs`만 지원.

---

## 12. 연결 상태 관리 (자동 재연결)

```javascript
class SubscriptionClient {
  constructor(url) {
    this.url = url;
    this.ws = null;
    this.reconnectDelay = 1000;
    this.maxReconnectDelay = 30000;
    this.reconnectAttempts = 0;
  }

  connect() {
    this.ws = new WebSocket(this.url, 'graphql-transport-ws');

    this.ws.onopen = () => {
      console.log('Connected');
      this.reconnectAttempts = 0;
      this.reconnectDelay = 1000;
      this.ws.send(JSON.stringify({ type: 'connection_init' }));
    };

    this.ws.onclose = () => {
      console.log('Disconnected, reconnecting...');
      this.reconnect();
    };

    this.ws.onerror = (error) => {
      console.error('Error:', error);
    };

    this.ws.onmessage = (event) => {
      const message = JSON.parse(event.data);
      this.handleMessage(message);
    };
  }

  reconnect() {
    this.reconnectAttempts++;
    const delay = Math.min(
      this.reconnectDelay * Math.pow(2, this.reconnectAttempts),
      this.maxReconnectDelay
    );

    console.log(`Reconnecting in ${delay}ms...`);
    setTimeout(() => this.connect(), delay);
  }

  handleMessage(message) {
    // 메시지 처리 로직
    console.log('Received:', message);
  }

  subscribe(id, query, variables) {
    this.ws.send(JSON.stringify({
      id,
      type: 'subscribe',
      payload: { query, variables }
    }));
  }

  unsubscribe(id) {
    this.ws.send(JSON.stringify({
      id,
      type: 'complete'
    }));
  }
}

// 사용
const client = new SubscriptionClient('ws://localhost:8545/graphql/ws');
client.connect();

// 연결 후 구독
setTimeout(() => {
  client.subscribe('blocks', 'subscription { newBlock { number hash txCount } }');
}, 1000);
```

---

## 13. 성능 고려사항

### 클라이언트 측

- **재연결 로직**: 지수 백오프 사용
- **버퍼링**: 수신 데이터를 적절히 버퍼링
- **메모리 관리**: 오래된 데이터 정리

### 서버 측 제한

- **Publish 버퍼**: 1000개 (가득 차면 이벤트 드롭)
- **Subscription 버퍼**: 100개 (각 구독자당)
- **최대 메시지 크기**: 4096 bytes
- **Ping 주기**: 54초
- **Pong 타임아웃**: 60초

---

## 14. 문제 해결

### 프론트엔드에서 데이터가 안 들어올 때

1. **WebSocket 연결 확인**:
   ```javascript
   ws.readyState === WebSocket.OPEN  // 1이어야 함
   ```

2. **네트워크 탭 확인** (브라우저 개발자 도구):
   - WS 탭에서 연결 상태 확인
   - 보낸 메시지와 받은 메시지 확인

3. **서버 로그 확인**:
   ```bash
   # 서버 시작 로그
   "GraphQL subscriptions endpoint registered" path=/graphql/ws
   "EventBus set for GraphQL subscriptions"

   # 연결 로그
   "WebSocket connection request received"
   "WebSocket connection established"
   "received connection_init, sending connection_ack"
   "received subscribe request"
   "subscription started"
   ```

4. **EventBus 상태 확인**:
   ```bash
   curl http://localhost:8545/subscribers
   ```

   응답 예시:
   ```json
   {
     "total_count": 1,
     "subscribers": [
       {
         "ID": "test-1",
         "EventTypes": ["block"],
         "HasFilter": false,
         "EventsReceived": 150,
         "EventsDropped": 0,
         "CreatedAt": "2025-11-25T...",
         "Uptime": "5m30s"
       }
     ]
   }
   ```

5. **Fetcher가 블록을 인덱싱하는지 확인**:
   - 서버 로그에서 "Successfully indexed block" 메시지 확인
   - 블록이 생성되지 않으면 이벤트도 발행되지 않음

---

## 15. 현재 알려진 제약사항

1. **RPC 서버 의존성**: `newPendingTransactions` 구독은 RPC 서버가 해당 subscription을 지원해야 합니다
2. **빠른 블록 생성**: Stable-One은 블록 생성이 매우 빠르므로 (1-2초) pending 트랜잭션 상태가 짧을 수 있습니다

---

이 가이드를 참고해서 프론트엔드를 구현하시고, 그래도 데이터가 안 들어오면 서버 로그를 공유해주세요!
