# GraphQL Subscription Replay 기능 가이드

## 개요

GraphQL Subscription에 `replayLast` 파라미터가 추가되었습니다. 이 기능을 사용하면 구독 시작 시 최근 이벤트를 즉시 수신할 수 있습니다.

### 해결하는 문제

```
기존 문제:
1. 사용자가 페이지 접속
2. WebSocket 연결 및 구독 시작
3. 구독 이후 발생하는 이벤트만 수신
4. 페이지에 아무 데이터도 표시되지 않음 (다음 이벤트까지 대기)

Replay 사용 시:
1. 사용자가 페이지 접속
2. WebSocket 연결 및 구독 시작 (replayLast: 5)
3. 즉시 최근 5개 이벤트 수신
4. 이후 실시간 이벤트 계속 수신
5. 페이지에 즉시 데이터 표시!
```

---

## API 변경 사항

### 지원되는 Subscriptions

| Subscription | replayLast | 설명 |
|--------------|------------|------|
| `newBlock(replayLast: Int)` | ✅ | 새 블록 |
| `newTransaction(replayLast: Int)` | ✅ | 새 트랜잭션 |
| `logs(filter: LogFilter!, replayLast: Int)` | ✅ | 로그 이벤트 |
| `systemContractEvents(filter: ..., replayLast: Int)` | ✅ | 시스템 컨트랙트 이벤트 |
| `dynamicContractEvents(filter: ..., replayLast: Int)` | ✅ | 동적 컨트랙트 이벤트 |
| `consensusBlock(replayLast: Int)` | ✅ | 컨센서스 블록 |
| `consensusFork(replayLast: Int)` | ✅ | 포크 감지 |
| `consensusValidatorChange(replayLast: Int)` | ✅ | 검증자 변경 |
| `consensusError(replayLast: Int)` | ✅ | 컨센서스 에러 |
| `newPendingTransactions(limit: Int)` | ❌ | 해당 없음 |

### 파라미터 스펙

| 파라미터 | 타입 | 기본값 | 최대값 | 설명 |
|----------|------|--------|--------|------|
| `replayLast` | `Int` (optional) | `0` (없음) | `100` | 구독 시 즉시 수신할 최근 이벤트 수 |

---

## 사용 예시

### 1. 기본 사용 (기존 방식, 변경 없음)

```graphql
subscription {
  newBlock {
    number
    hash
    timestamp
    transactionCount
  }
}
```

### 2. Replay 사용 - 최근 5개 블록

```graphql
subscription {
  newBlock(replayLast: 5) {
    number
    hash
    timestamp
    transactionCount
  }
}
```

### 3. 트랜잭션 구독 + Replay

```graphql
subscription {
  newTransaction(replayLast: 10) {
    hash
    from
    to
    value
    blockNumber
  }
}
```

### 4. 필터와 Replay 조합

```graphql
subscription {
  logs(
    filter: {
      addresses: ["0x1000000000000000000000000000000000000001"]
    }
    replayLast: 20
  ) {
    address
    topics
    data
    blockNumber
    transactionHash
  }
}
```

### 5. System Contract Events + Replay

```graphql
subscription {
  systemContractEvents(
    filter: {
      contract: "0x1000000000000000000000000000000000000001"
      eventTypes: ["Transfer", "Approval"]
    }
    replayLast: 15
  ) {
    contract
    eventName
    blockNumber
    data
  }
}
```

### 6. Consensus Block + Replay

```graphql
subscription {
  consensusBlock(replayLast: 10) {
    blockNumber
    blockHash
    epoch
    round
    proposer
    participationRate
    validators {
      address
      participated
    }
  }
}
```

---

## TypeScript/React 통합 예시

### Apollo Client 설정

```typescript
import {
  ApolloClient,
  InMemoryCache,
  split,
  HttpLink
} from '@apollo/client';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { createClient } from 'graphql-ws';
import { getMainDefinition } from '@apollo/client/utilities';

// WebSocket 링크 설정
const wsLink = new GraphQLWsLink(
  createClient({
    url: 'ws://localhost:8080/graphql',
  })
);

// HTTP 링크 설정
const httpLink = new HttpLink({
  uri: 'http://localhost:8080/graphql',
});

// 쿼리/뮤테이션은 HTTP, 구독은 WebSocket
const splitLink = split(
  ({ query }) => {
    const definition = getMainDefinition(query);
    return (
      definition.kind === 'OperationDefinition' &&
      definition.operation === 'subscription'
    );
  },
  wsLink,
  httpLink
);

const client = new ApolloClient({
  link: splitLink,
  cache: new InMemoryCache(),
});
```

### 기본 구독 Hook

```typescript
import { gql, useSubscription } from '@apollo/client';

// GraphQL Subscription 정의
const NEW_BLOCK_SUBSCRIPTION = gql`
  subscription NewBlock($replayLast: Int) {
    newBlock(replayLast: $replayLast) {
      number
      hash
      timestamp
      transactionCount
    }
  }
`;

// React Hook 사용
function useNewBlocks(replayLast: number = 0) {
  const { data, loading, error } = useSubscription(NEW_BLOCK_SUBSCRIPTION, {
    variables: { replayLast },
  });

  return {
    block: data?.newBlock,
    loading,
    error,
  };
}
```

### 블록 목록 컴포넌트 예시

```typescript
import React, { useState, useEffect } from 'react';
import { gql, useSubscription } from '@apollo/client';

const NEW_BLOCK_SUBSCRIPTION = gql`
  subscription NewBlock($replayLast: Int) {
    newBlock(replayLast: $replayLast) {
      number
      hash
      timestamp
      transactionCount
    }
  }
`;

interface Block {
  number: string;
  hash: string;
  timestamp: string;
  transactionCount: number;
}

function BlockList() {
  const [blocks, setBlocks] = useState<Block[]>([]);

  // replayLast: 10으로 최근 10개 블록 즉시 수신
  const { data, error } = useSubscription(NEW_BLOCK_SUBSCRIPTION, {
    variables: { replayLast: 10 },
  });

  useEffect(() => {
    if (data?.newBlock) {
      setBlocks(prev => {
        // 중복 방지
        const exists = prev.some(b => b.hash === data.newBlock.hash);
        if (exists) return prev;

        // 최대 50개 유지
        const updated = [data.newBlock, ...prev];
        return updated.slice(0, 50);
      });
    }
  }, [data]);

  if (error) {
    return <div>Error: {error.message}</div>;
  }

  return (
    <div>
      <h2>Recent Blocks</h2>
      <ul>
        {blocks.map(block => (
          <li key={block.hash}>
            Block #{block.number} - {block.transactionCount} txs
          </li>
        ))}
      </ul>
    </div>
  );
}
```

### 재연결 시 Replay 활용

```typescript
import { createClient } from 'graphql-ws';

const wsClient = createClient({
  url: 'ws://localhost:8080/graphql',

  // 재연결 설정
  retryAttempts: Infinity,
  shouldRetry: () => true,

  // 연결 이벤트 핸들링
  on: {
    connected: () => {
      console.log('WebSocket connected');
      // 재연결 시 replayLast로 놓친 이벤트 복구 가능
    },
    closed: () => {
      console.log('WebSocket closed');
    },
  },
});
```

### Custom Hook - 자동 재연결 및 Replay

```typescript
import { useEffect, useRef, useState } from 'react';
import { useSubscription, gql } from '@apollo/client';

interface UseBlockSubscriptionOptions {
  replayLast?: number;
  maxBlocks?: number;
}

function useBlockSubscription(options: UseBlockSubscriptionOptions = {}) {
  const { replayLast = 5, maxBlocks = 100 } = options;
  const [blocks, setBlocks] = useState<Block[]>([]);
  const seenHashes = useRef(new Set<string>());

  const { data, error } = useSubscription(
    gql`
      subscription NewBlock($replayLast: Int) {
        newBlock(replayLast: $replayLast) {
          number
          hash
          timestamp
          transactionCount
        }
      }
    `,
    { variables: { replayLast } }
  );

  useEffect(() => {
    if (data?.newBlock) {
      const block = data.newBlock;

      // 중복 체크 (replay와 실시간 이벤트 중복 방지)
      if (seenHashes.current.has(block.hash)) {
        return;
      }
      seenHashes.current.add(block.hash);

      setBlocks(prev => {
        const updated = [block, ...prev];

        // 메모리 관리: 최대 개수 유지
        if (updated.length > maxBlocks) {
          const removed = updated.slice(maxBlocks);
          removed.forEach(b => seenHashes.current.delete(b.hash));
          return updated.slice(0, maxBlocks);
        }

        return updated;
      });
    }
  }, [data, maxBlocks]);

  return { blocks, error };
}
```

---

## 주의사항

### 1. 중복 이벤트 처리

Replay 이벤트와 실시간 이벤트가 겹칠 수 있습니다. 클라이언트에서 중복 처리가 필요합니다.

```typescript
// 권장: hash나 고유 ID로 중복 체크
const seenIds = new Set<string>();

function handleEvent(event: Event) {
  if (seenIds.has(event.id)) {
    return; // 중복 무시
  }
  seenIds.add(event.id);
  // 처리 로직
}
```

### 2. 이벤트 순서

- Replay 이벤트: 시간순 (오래된 것부터)
- 이후 실시간 이벤트 스트리밍

```
수신 순서: [Replay1] → [Replay2] → [Replay3] → [Live1] → [Live2] → ...
```

### 3. 최대값 제한

- `replayLast > 100` 지정 시 자동으로 100으로 제한됩니다
- 너무 큰 값은 초기 로딩 시간에 영향을 줄 수 있습니다

### 4. 필터 적용

- Replay 이벤트에도 filter가 동일하게 적용됩니다
- filter 조건에 맞는 최근 N개 이벤트만 replay됩니다

### 5. 메모리 관리

```typescript
// 클라이언트에서 이벤트 목록 크기 제한 권장
const MAX_EVENTS = 100;

setEvents(prev => {
  const updated = [newEvent, ...prev];
  return updated.slice(0, MAX_EVENTS);
});
```

---

## 권장 사용 패턴

| 시나리오 | replayLast 권장값 | 이유 |
|----------|-------------------|------|
| 대시보드 초기 로딩 | 10-20 | 즉시 데이터 표시 |
| 블록 탐색기 | 20-50 | 충분한 히스토리 |
| 실시간 모니터링 | 5-10 | 빠른 로딩 |
| 트랜잭션 추적 | 0 (없음) | 특정 이벤트만 관심 |
| 네트워크 재연결 | 10-30 | 놓친 이벤트 복구 |

---

## 마이그레이션

### 기존 코드 (변경 불필요)

기존 subscription 코드는 그대로 동작합니다:

```graphql
# 기존 코드 - 변경 없이 동작
subscription {
  newBlock {
    number
    hash
  }
}
```

### Replay 기능 추가 (선택적)

Replay가 필요한 경우에만 파라미터 추가:

```graphql
# replayLast 추가
subscription {
  newBlock(replayLast: 10) {
    number
    hash
  }
}
```

---

## FAQ

### Q: replayLast를 지정하지 않으면 어떻게 되나요?
A: 기존과 동일하게 동작합니다. 구독 시작 이후 발생하는 이벤트만 수신합니다.

### Q: replay 이벤트와 실시간 이벤트를 구분할 수 있나요?
A: 현재는 구분 필드가 없습니다. 필요하다면 timestamp로 추정하거나, 백엔드에 기능 추가를 요청해주세요.

### Q: 서버에 저장된 히스토리보다 큰 replayLast를 지정하면?
A: 저장된 만큼만 replay됩니다. 예: 히스토리 50개, replayLast: 100 → 50개만 수신

### Q: 여러 subscription에서 동시에 replay를 사용해도 되나요?
A: 네, 각 subscription은 독립적으로 동작합니다.

---

## 지원

문의사항이나 버그 리포트는 백엔드 팀에 연락해주세요.
