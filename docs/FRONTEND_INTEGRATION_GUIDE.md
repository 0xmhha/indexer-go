# Frontend Integration Guide

이 문서는 백엔드 리팩토링에 따른 프론트엔드 통합 가이드입니다.

## 아키텍처 개요

```
┌─────────────────────────────────────────────────────────────────┐
│                         Frontend                                 │
├─────────────────────────────────────────────────────────────────┤
│  GraphQL Client (Apollo/urql)                                   │
│    ├── Queries: 데이터 조회                                      │
│    ├── Mutations: 컨트랙트 등록/해제                              │
│    └── Subscriptions: 실시간 이벤트 수신                          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    GraphQL API (WebSocket)                       │
├─────────────────────────────────────────────────────────────────┤
│  ws://localhost:8080/graphql                                    │
└─────────────────────────────────────────────────────────────────┘
```

## 1. 시스템 컨트랙트 이벤트 구독

### 1.1 기본 구독

```graphql
subscription SystemContractEvents($filter: SystemContractSubscriptionFilter) {
  systemContractEvents(filter: $filter) {
    contract
    eventName
    blockNumber
    transactionHash
    logIndex
    data
    timestamp
  }
}
```

### 1.2 필터 옵션

```typescript
interface SystemContractSubscriptionFilter {
  // 특정 컨트랙트 주소로 필터링 (선택사항)
  contract?: string;

  // 특정 이벤트 타입들로 필터링 (선택사항)
  eventTypes?: string[];
}
```

### 1.3 시스템 컨트랙트 주소

| Contract | Address | Description |
|----------|---------|-------------|
| NativeCoinAdapter | `0x1000` | 토큰 민팅/소각 |
| GovValidator | `0x1001` | 검증자 거버넌스 |
| GovMasterMinter | `0x1002` | 마스터 민터 거버넌스 |
| GovMinter | `0x1003` | 민터 거버넌스 |
| GovCouncil | `0x1004` | 카운실 거버넌스 |

### 1.4 이벤트 타입

```typescript
type SystemContractEventType =
  // NativeCoinAdapter Events
  | 'Mint'
  | 'Burn'
  | 'MinterConfigured'
  | 'MinterRemoved'
  | 'MasterMinterChanged'
  | 'Transfer'
  | 'Approval'

  // Governance Events (모든 Gov* 컨트랙트 공통)
  | 'ProposalCreated'
  | 'ProposalVoted'
  | 'ProposalApproved'
  | 'ProposalRejected'
  | 'ProposalExecuted'
  | 'ProposalFailed'
  | 'ProposalExpired'
  | 'ProposalCancelled'
  | 'MemberAdded'
  | 'MemberRemoved'
  | 'MemberChanged'
  | 'QuorumUpdated'
  | 'MaxProposalsPerMemberUpdated'

  // GovValidator Specific
  | 'GasTipUpdated'

  // GovMasterMinter Specific
  | 'MaxMinterAllowanceUpdated'
  | 'EmergencyPaused'
  | 'EmergencyUnpaused'

  // GovMinter Specific
  | 'DepositMintProposed'
  | 'BurnPrepaid'
  | 'BurnExecuted'

  // GovCouncil Specific
  | 'AddressBlacklisted'
  | 'AddressUnblacklisted'
  | 'AuthorizedAccountAdded'
  | 'AuthorizedAccountRemoved'
  | 'ProposalExecutionSkipped';
```

## 2. 동적 컨트랙트 등록 (NEW)

### 2.1 컨트랙트 등록

```graphql
mutation RegisterContract($input: RegisterContractInput!) {
  registerContract(input: $input) {
    address
    name
    abi
    registeredAt
    blockNumber
    isVerified
    events
  }
}
```

**Input:**
```typescript
interface RegisterContractInput {
  address: string;      // 컨트랙트 주소
  name: string;         // 컨트랙트 이름
  abi: string;          // ABI JSON 문자열
  blockNumber?: string; // 시작 블록 번호 (선택)
}
```

**Example:**
```typescript
const { data } = await client.mutate({
  mutation: REGISTER_CONTRACT,
  variables: {
    input: {
      address: '0x1234567890abcdef...',
      name: 'MyToken',
      abi: JSON.stringify([
        {
          type: 'event',
          name: 'Transfer',
          inputs: [
            { name: 'from', type: 'address', indexed: true },
            { name: 'to', type: 'address', indexed: true },
            { name: 'value', type: 'uint256', indexed: false }
          ]
        }
      ])
    }
  }
});
```

### 2.2 컨트랙트 해제

```graphql
mutation UnregisterContract($address: Address!) {
  unregisterContract(address: $address)
}
```

### 2.3 등록된 컨트랙트 조회

```graphql
# 전체 목록
query RegisteredContracts {
  registeredContracts {
    address
    name
    events
    isVerified
    registeredAt
  }
}

# 특정 컨트랙트
query RegisteredContract($address: Address!) {
  registeredContract(address: $address) {
    address
    name
    abi
    events
    isVerified
    registeredAt
  }
}
```

### 2.4 동적 컨트랙트 이벤트 구독

```graphql
subscription DynamicContractEvents($filter: DynamicContractSubscriptionFilter) {
  dynamicContractEvents(filter: $filter) {
    contract
    contractName
    eventName
    blockNumber
    txHash
    logIndex
    data
    timestamp
  }
}
```

**Filter:**
```typescript
interface DynamicContractSubscriptionFilter {
  contract?: string;      // 특정 컨트랙트 주소
  eventNames?: string[];  // 특정 이벤트 이름들
}
```

## 3. React/TypeScript 통합 예제

### 3.1 Apollo Client 설정

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

const httpLink = new HttpLink({
  uri: 'http://localhost:8080/graphql',
});

const wsLink = new GraphQLWsLink(createClient({
  url: 'ws://localhost:8080/graphql',
}));

const splitLink = split(
  ({ query }) => {
    const definition = getMainDefinition(query);
    return (
      definition.kind === 'OperationDefinition' &&
      definition.operation === 'subscription'
    );
  },
  wsLink,
  httpLink,
);

export const client = new ApolloClient({
  link: splitLink,
  cache: new InMemoryCache(),
});
```

### 3.2 시스템 컨트랙트 이벤트 Hook

```typescript
import { useSubscription, gql } from '@apollo/client';

const SYSTEM_CONTRACT_EVENTS = gql`
  subscription SystemContractEvents($filter: SystemContractSubscriptionFilter) {
    systemContractEvents(filter: $filter) {
      contract
      eventName
      blockNumber
      transactionHash
      logIndex
      data
      timestamp
    }
  }
`;

interface SystemContractEvent {
  contract: string;
  eventName: string;
  blockNumber: string;
  transactionHash: string;
  logIndex: number;
  data: string;
  timestamp: string;
}

export function useSystemContractEvents(filter?: {
  contract?: string;
  eventTypes?: string[];
}) {
  const { data, loading, error } = useSubscription<{
    systemContractEvents: SystemContractEvent;
  }>(SYSTEM_CONTRACT_EVENTS, {
    variables: { filter },
  });

  return {
    event: data?.systemContractEvents,
    loading,
    error,
  };
}
```

### 3.3 이벤트 데이터 파싱

```typescript
interface MintEventData {
  minter: string;
  to: string;
  amount: string;
}

interface ProposalCreatedData {
  proposalId: string;
  proposer: string;
  actionType: string;
  memberVersion: string;
  requiredApprovals: string;
  callData: string;
}

interface DepositMintProposedData {
  proposalId: string;
  depositId: string;
  requester: string;
  beneficiary: string;
  amount: string;
  bankReference: string;
}

function parseEventData<T>(jsonString: string): T {
  return JSON.parse(jsonString) as T;
}

// Usage
function EventHandler({ event }: { event: SystemContractEvent }) {
  switch (event.eventName) {
    case 'Mint': {
      const data = parseEventData<MintEventData>(event.data);
      return <MintEventCard minter={data.minter} to={data.to} amount={data.amount} />;
    }
    case 'ProposalCreated': {
      const data = parseEventData<ProposalCreatedData>(event.data);
      return <ProposalCard proposalId={data.proposalId} proposer={data.proposer} />;
    }
    case 'DepositMintProposed': {
      const data = parseEventData<DepositMintProposedData>(event.data);
      return (
        <DepositMintCard
          proposalId={data.proposalId}
          requester={data.requester}
          beneficiary={data.beneficiary}
          amount={data.amount}
          bankReference={data.bankReference}
        />
      );
    }
    default:
      return <GenericEventCard event={event} />;
  }
}
```

### 3.4 동적 컨트랙트 관리 Hook

```typescript
import { useMutation, useQuery, gql } from '@apollo/client';

const REGISTER_CONTRACT = gql`
  mutation RegisterContract($input: RegisterContractInput!) {
    registerContract(input: $input) {
      address
      name
      events
      isVerified
    }
  }
`;

const UNREGISTER_CONTRACT = gql`
  mutation UnregisterContract($address: Address!) {
    unregisterContract(address: $address)
  }
`;

const REGISTERED_CONTRACTS = gql`
  query RegisteredContracts {
    registeredContracts {
      address
      name
      events
      isVerified
      registeredAt
    }
  }
`;

export function useContractRegistration() {
  const [registerMutation] = useMutation(REGISTER_CONTRACT);
  const [unregisterMutation] = useMutation(UNREGISTER_CONTRACT);
  const { data, refetch } = useQuery(REGISTERED_CONTRACTS);

  const registerContract = async (input: {
    address: string;
    name: string;
    abi: string;
    blockNumber?: string;
  }) => {
    const result = await registerMutation({ variables: { input } });
    await refetch();
    return result.data.registerContract;
  };

  const unregisterContract = async (address: string) => {
    await unregisterMutation({ variables: { address } });
    await refetch();
  };

  return {
    contracts: data?.registeredContracts || [],
    registerContract,
    unregisterContract,
  };
}
```

## 4. 이벤트 데이터 구조

### 4.1 Mint Event

```typescript
interface MintEventData {
  minter: string;    // 민팅을 수행한 주소
  to: string;        // 토큰을 받는 주소
  amount: string;    // 민팅된 양 (wei)
}
```

### 4.2 Burn Event

```typescript
interface BurnEventData {
  burner: string;       // 소각을 수행한 주소
  amount: string;       // 소각된 양 (wei)
  withdrawalId?: string; // 출금 ID (선택)
}
```

### 4.3 ProposalCreated Event

```typescript
interface ProposalCreatedData {
  proposalId: string;       // 제안 ID
  proposer: string;         // 제안자 주소
  actionType: string;       // 액션 타입 (bytes32 hex)
  memberVersion: string;    // 멤버 버전
  requiredApprovals: string;// 필요한 승인 수
  callData: string;         // 호출 데이터 (bytes hex)
}
```

### 4.4 DepositMintProposed Event

```typescript
interface DepositMintProposedData {
  proposalId: string;    // 제안 ID
  depositId: string;     // 입금 ID
  requester: string;     // 요청자 주소
  beneficiary: string;   // 수혜자 주소
  amount: string;        // 금액 (wei)
  bankReference: string; // 은행 참조 번호
}
```

### 4.5 ProposalVoted Event

```typescript
interface ProposalVotedData {
  proposalId: string;  // 제안 ID
  voter: string;       // 투표자 주소
  approval: boolean;   // 찬성 여부
  approved: string;    // 현재 찬성 수
  rejected: string;    // 현재 반대 수
}
```

## 5. 에러 처리

### 5.1 WebSocket 연결 에러

```typescript
const wsLink = new GraphQLWsLink(createClient({
  url: 'ws://localhost:8080/graphql',
  connectionParams: {
    // 인증 토큰 등
  },
  on: {
    connected: () => console.log('WebSocket connected'),
    closed: () => console.log('WebSocket closed'),
    error: (error) => console.error('WebSocket error:', error),
  },
  retryAttempts: 5,
  shouldRetry: () => true,
}));
```

### 5.2 구독 에러 처리

```typescript
const { data, loading, error } = useSubscription(SYSTEM_CONTRACT_EVENTS, {
  variables: { filter },
  onError: (error) => {
    console.error('Subscription error:', error);
    // 재연결 로직 또는 에러 UI 표시
  },
  onData: ({ data }) => {
    // 새 이벤트 처리
    console.log('New event:', data?.systemContractEvents);
  },
});
```

## 6. 성능 최적화

### 6.1 이벤트 버퍼링

```typescript
import { useRef, useEffect, useState } from 'react';

function useBufferedEvents(bufferSize = 100) {
  const [events, setEvents] = useState<SystemContractEvent[]>([]);
  const bufferRef = useRef<SystemContractEvent[]>([]);

  const { event } = useSystemContractEvents();

  useEffect(() => {
    if (event) {
      bufferRef.current = [event, ...bufferRef.current].slice(0, bufferSize);
      setEvents([...bufferRef.current]);
    }
  }, [event, bufferSize]);

  return events;
}
```

### 6.2 필터링으로 트래픽 감소

```typescript
// 특정 컨트랙트만 구독
useSystemContractEvents({
  contract: '0x1000', // NativeCoinAdapter만
});

// 특정 이벤트 타입만 구독
useSystemContractEvents({
  eventTypes: ['Mint', 'Burn'], // Mint, Burn 이벤트만
});

// 조합
useSystemContractEvents({
  contract: '0x1004',
  eventTypes: ['ProposalCreated', 'ProposalExecuted'],
});
```

## 7. 마이그레이션 체크리스트

프론트엔드 리팩토링 시 확인 사항:

- [ ] Apollo Client WebSocket 링크 설정
- [ ] 시스템 컨트랙트 이벤트 구독 구현
- [ ] 동적 컨트랙트 등록 UI 구현 (필요시)
- [ ] 이벤트 데이터 파싱 로직 구현
- [ ] 에러 처리 및 재연결 로직 구현
- [ ] 이벤트 버퍼링/페이지네이션 구현
- [ ] TypeScript 타입 정의 업데이트

## 8. API 엔드포인트

| Type | Endpoint | Description |
|------|----------|-------------|
| HTTP | `http://localhost:8080/graphql` | Query, Mutation |
| WebSocket | `ws://localhost:8080/graphql` | Subscription |

## 9. 변경 사항 요약

### 새로 추가된 기능

1. **동적 컨트랙트 등록 API**
   - `registerContract` mutation
   - `unregisterContract` mutation
   - `registeredContracts` query
   - `registeredContract` query

2. **동적 컨트랙트 이벤트 구독**
   - `dynamicContractEvents` subscription

3. **확장 가능한 ABI 기반 파싱**
   - ABI JSON으로 새 컨트랙트 등록
   - 자동 이벤트 파싱

### 기존 API 유지

- `systemContractEvents` subscription (변경 없음)
- 모든 기존 Query/Mutation (변경 없음)
