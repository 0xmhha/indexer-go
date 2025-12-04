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

---

## 10. Transaction Receipt API

Transaction Receipt는 트랜잭션 실행 결과를 나타내며, 가스 사용량, 상태, 이벤트 로그 등의 정보를 포함합니다.

### 10.1 GraphQL Queries

#### 단일 Receipt 조회

```graphql
query GetReceipt($txHash: Hash!) {
  receipt(transactionHash: $txHash) {
    transactionHash
    blockNumber
    blockHash
    transactionIndex
    status           # 0: 실패, 1: 성공
    gasUsed
    cumulativeGasUsed
    effectiveGasPrice
    contractAddress  # 컨트랙트 생성 시에만
    logs {
      address
      topics
      data
      logIndex
      blockNumber
    }
    logsBloom
  }
}
```

#### 블록별 Receipt 조회

```graphql
query GetReceiptsByBlock($blockNumber: BigInt!) {
  receiptsByBlock(blockNumber: $blockNumber) {
    transactionHash
    status
    gasUsed
    logs {
      address
      topics
      data
    }
  }
}
```

### 10.2 JSON-RPC Methods

#### getTxReceipt

트랜잭션 해시로 Receipt 조회:

```typescript
// Request
{
  "method": "getTxReceipt",
  "params": ["0x1234..."]
}

// Response
{
  "transactionHash": "0x1234...",
  "blockNumber": "0x1a4",
  "blockHash": "0xabcd...",
  "transactionIndex": "0x0",
  "status": "0x1",
  "gasUsed": "0x5208",
  "cumulativeGasUsed": "0x5208",
  "contractAddress": null,
  "logs": [...]
}
```

### 10.3 Receipt 데이터 구조

```typescript
interface TransactionReceipt {
  transactionHash: string;      // 트랜잭션 해시
  blockNumber: string;          // 블록 번호
  blockHash: string;            // 블록 해시
  transactionIndex: number;     // 블록 내 트랜잭션 인덱스
  status: number;               // 0: 실패, 1: 성공
  gasUsed: string;              // 실제 사용된 가스
  cumulativeGasUsed: string;    // 블록 내 누적 가스
  effectiveGasPrice: string;    // 실효 가스 가격
  contractAddress?: string;     // 생성된 컨트랙트 주소 (있을 경우)
  logs: Log[];                  // 이벤트 로그
  logsBloom: string;            // 로그 블룸 필터
}

interface Log {
  address: string;              // 이벤트 발생 컨트랙트
  topics: string[];             // 인덱싱된 파라미터
  data: string;                 // 비인덱싱 파라미터
  logIndex: number;             // 로그 인덱스
  blockNumber: string;          // 블록 번호
  transactionHash: string;      // 트랜잭션 해시
}
```

### 10.4 Frontend 활용 예시

#### React Hook for Receipt

```typescript
import { useQuery, gql } from '@apollo/client';

const GET_RECEIPT = gql`
  query GetReceipt($txHash: Hash!) {
    receipt(transactionHash: $txHash) {
      transactionHash
      status
      gasUsed
      effectiveGasPrice
      logs {
        address
        topics
        data
      }
    }
  }
`;

interface UseReceiptResult {
  receipt: TransactionReceipt | null;
  loading: boolean;
  error: Error | null;
  isSuccess: boolean;
  isFailed: boolean;
}

export function useReceipt(txHash: string): UseReceiptResult {
  const { data, loading, error } = useQuery(GET_RECEIPT, {
    variables: { txHash },
    skip: !txHash,
  });

  const receipt = data?.receipt || null;

  return {
    receipt,
    loading,
    error: error || null,
    isSuccess: receipt?.status === 1,
    isFailed: receipt?.status === 0,
  };
}
```

#### 트랜잭션 상태 표시 컴포넌트

```typescript
function TransactionStatus({ txHash }: { txHash: string }) {
  const { receipt, loading, isSuccess, isFailed } = useReceipt(txHash);

  if (loading) {
    return <Spinner />;
  }

  if (!receipt) {
    return <Badge color="gray">Pending</Badge>;
  }

  if (isSuccess) {
    return (
      <div>
        <Badge color="green">Success</Badge>
        <span>Gas Used: {formatGas(receipt.gasUsed)}</span>
      </div>
    );
  }

  if (isFailed) {
    return <Badge color="red">Failed</Badge>;
  }

  return null;
}
```

#### 가스 비용 계산

```typescript
function calculateTxCost(receipt: TransactionReceipt): string {
  const gasUsed = BigInt(receipt.gasUsed);
  const gasPrice = BigInt(receipt.effectiveGasPrice);
  const costWei = gasUsed * gasPrice;

  // Wei to Ether 변환
  return formatEther(costWei);
}

function formatGas(gas: string): string {
  return parseInt(gas).toLocaleString();
}
```

#### 로그 파싱 예시

```typescript
import { ethers } from 'ethers';

// ERC20 Transfer 이벤트 파싱
const TRANSFER_TOPIC = ethers.id('Transfer(address,address,uint256)');

interface TransferEvent {
  from: string;
  to: string;
  value: bigint;
}

function parseTransferLogs(logs: Log[]): TransferEvent[] {
  return logs
    .filter(log => log.topics[0] === TRANSFER_TOPIC)
    .map(log => ({
      from: ethers.getAddress('0x' + log.topics[1].slice(26)),
      to: ethers.getAddress('0x' + log.topics[2].slice(26)),
      value: BigInt(log.data),
    }));
}
```

### 10.5 Receipt 조회 시 주의사항

1. **Pending 트랜잭션**: Receipt가 없으면 트랜잭션이 아직 처리 중
2. **실패한 트랜잭션**: `status === 0`이어도 가스는 소비됨
3. **로그 필터링**: 필요한 이벤트만 필터링하여 처리 효율 향상

```typescript
// Receipt 폴링 예시
async function waitForReceipt(
  txHash: string,
  maxAttempts = 30,
  intervalMs = 2000
): Promise<TransactionReceipt> {
  for (let i = 0; i < maxAttempts; i++) {
    const receipt = await fetchReceipt(txHash);
    if (receipt) {
      return receipt;
    }
    await sleep(intervalMs);
  }
  throw new Error('Transaction receipt not found after timeout');
}
```

---

## 11. Backend Receipt 시스템 개선사항 (v2.0+)

최신 버전에서 Receipt 처리 관련 다음 기능이 추가되었습니다:

### 11.1 Receipt Gap Detection

블록과 Receipt 간 데이터 불일치를 감지하고 복구합니다.

**동작 방식:**
1. 블록의 트랜잭션 목록과 저장된 Receipt 비교
2. 누락된 Receipt 감지
3. RPC 노드에서 자동 재수집

**Frontend 영향:**
- Receipt 조회 시 데이터 정합성 보장
- 이전 버전에서 누락된 데이터 자동 복구

### 11.2 Receipt Validation

저장 시점에 Receipt 유효성을 검증합니다.

**검증 항목:**
- `TxHash`: 유효한 트랜잭션 해시 (zero hash 불가)
- `Status`: 0 또는 1만 허용
- `CumulativeGasUsed`: GasUsed보다 크거나 같아야 함

**에러 응답:**
```json
{
  "error": {
    "code": -32000,
    "message": "invalid receipt: transaction hash is not set"
  }
}
```

### 11.3 Batch Receipt 조회 개선

대량 Receipt 조회 시 부분 실패를 지원합니다.

**특징:**
- 일부 Receipt 조회 실패 시에도 성공한 결과 반환
- 실패 원인 추적 가능
- 성공/실패 카운트 제공

**Frontend 처리 권장:**
```typescript
interface BatchReceiptResult {
  receipts: (TransactionReceipt | null)[];
  successCount: number;
  failureCount: number;
  errors: { txHash: string; error: string }[];
}

function handleBatchResult(result: BatchReceiptResult) {
  if (result.failureCount > 0) {
    console.warn(`${result.failureCount} receipts failed to fetch`);
    result.errors.forEach(err => {
      console.error(`Receipt ${err.txHash}: ${err.error}`);
    });
  }

  // 성공한 Receipt만 처리
  const validReceipts = result.receipts.filter(r => r !== null);
  return validReceipts;
}
```

---

## 12. API 버전 호환성

| 기능 | v1.x | v2.0+ |
|------|------|-------|
| 단일 Receipt 조회 | ✅ | ✅ |
| 블록별 Receipt 조회 | ✅ | ✅ |
| Receipt Gap 자동 복구 | ❌ | ✅ |
| Receipt 유효성 검증 | ❌ | ✅ |
| Batch 부분 실패 지원 | ❌ | ✅ |

### 마이그레이션 가이드

v1.x에서 v2.0+로 업그레이드 시:

1. **에러 처리 강화**: 새로운 validation 에러 타입 처리
2. **Batch 결과 처리**: 부분 성공 시나리오 대응
3. **데이터 정합성**: Gap detection으로 과거 누락 데이터 자동 복구

---

## 13. RPC Proxy API (NEW)

RPC Proxy는 프론트엔드가 직접 RPC 노드에 연결하지 않고, 백엔드를 통해 블록체인과 상호작용할 수 있게 해주는 기능입니다.

### 13.1 아키텍처

```
┌─────────────────────────────────────────────────────────────────┐
│                         Frontend                                 │
│  (GraphQL Query로 Contract Call, Tx Status 조회)                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    RPC Proxy Service                             │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │ Rate Limiter │  │ Priority     │  │ Response     │          │
│  │              │  │ Queue        │  │ Cache        │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│                              │                                   │
│                    ┌─────────▼─────────┐                        │
│                    │   Worker Pool     │                        │
│                    │   (Goroutines)    │                        │
│                    └─────────┬─────────┘                        │
│                              │                                   │
│                    ┌─────────▼─────────┐                        │
│                    │ Circuit Breaker   │                        │
│                    └─────────┬─────────┘                        │
└──────────────────────────────┼──────────────────────────────────┘
                               │
                               ▼
                    ┌──────────────────┐
                    │   RPC Node       │
                    │   (eth_call,     │
                    │    debug_trace)  │
                    └──────────────────┘
```

### 13.2 GraphQL Queries

#### Contract Call (읽기 전용 컨트랙트 호출)

```graphql
query ContractCall(
  $address: Address!
  $method: String!
  $params: String
  $abi: String
) {
  contractCall(
    address: $address
    method: $method
    params: $params
    abi: $abi
  ) {
    result      # 디코딩된 결과 (JSON string)
    rawResult   # Raw hex 결과
    decoded     # 디코딩 성공 여부
  }
}
```

**Parameters:**
- `address` (필수): 컨트랙트 주소
- `method` (필수): 호출할 메서드 이름 (예: "balanceOf", "name", "symbol")
- `params` (선택): 메서드 파라미터 JSON 배열 (예: `'["0x1234..."]'`)
- `abi` (선택): 컨트랙트 ABI JSON 문자열 (제공하지 않으면 등록된 ABI 사용)

**Example:**
```typescript
// 토큰 잔액 조회
const { data } = await client.query({
  query: CONTRACT_CALL,
  variables: {
    address: '0x1000', // NativeCoinAdapter
    method: 'balanceOf',
    params: JSON.stringify(['0xYourAddress...']),
  }
});

console.log(data.contractCall.result); // "1000000000000000000"
```

#### Transaction Status (실시간 트랜잭션 상태)

```graphql
query TransactionStatus($txHash: Hash!) {
  transactionStatus(txHash: $txHash) {
    txHash
    status         # pending, success, failed, not_found, confirmed
    blockNumber    # 확인된 경우
    blockHash      # 확인된 경우
    confirmations  # 확인 수
    gasUsed        # 사용된 가스
  }
}
```

**Status Values:**
| Status | Description |
|--------|-------------|
| `pending` | 트랜잭션이 멤풀에 있음 |
| `success` | 트랜잭션 성공 (status=1) |
| `failed` | 트랜잭션 실패 (status=0) |
| `confirmed` | 충분한 확인 수 도달 |
| `not_found` | 트랜잭션을 찾을 수 없음 |

**Example:**
```typescript
const { data } = await client.query({
  query: TX_STATUS,
  variables: { txHash: '0xabc123...' }
});

if (data.transactionStatus.status === 'confirmed') {
  console.log('Transaction confirmed!');
  console.log(`Gas used: ${data.transactionStatus.gasUsed}`);
}
```

#### Internal Transactions (debug_traceTransaction)

```graphql
query InternalTransactionsRPC($txHash: Hash!) {
  internalTransactionsRPC(txHash: $txHash) {
    txHash
    totalCount
    internalTransactions {
      type          # CALL, CREATE, DELEGATECALL, STATICCALL
      from
      to
      value
      gas
      gasUsed
      input
      output
      error
      depth
      traceAddress
    }
  }
}
```

**Example:**
```typescript
const { data } = await client.query({
  query: INTERNAL_TXS_RPC,
  variables: { txHash: '0xabc123...' }
});

data.internalTransactionsRPC.internalTransactions.forEach(tx => {
  console.log(`${tx.type}: ${tx.from} → ${tx.to}, value: ${tx.value}`);
});
```

#### RPC Proxy Metrics

```graphql
query RPCProxyMetrics {
  rpcProxyMetrics {
    totalRequests
    successfulRequests
    failedRequests
    cacheHits
    cacheMisses
    averageLatencyMs
    queueDepth
    activeWorkers
    circuitState    # closed, open, half-open
  }
}
```

### 13.3 React Hooks

#### useContractCall Hook

```typescript
import { useQuery, useLazyQuery, gql } from '@apollo/client';

const CONTRACT_CALL = gql`
  query ContractCall(
    $address: Address!
    $method: String!
    $params: String
    $abi: String
  ) {
    contractCall(
      address: $address
      method: $method
      params: $params
      abi: $abi
    ) {
      result
      rawResult
      decoded
    }
  }
`;

interface ContractCallResult {
  result: string | null;
  rawResult: string;
  decoded: boolean;
}

export function useContractCall(
  address: string,
  method: string,
  params?: any[],
  abi?: string
) {
  const { data, loading, error, refetch } = useQuery<{
    contractCall: ContractCallResult;
  }>(CONTRACT_CALL, {
    variables: {
      address,
      method,
      params: params ? JSON.stringify(params) : undefined,
      abi,
    },
    skip: !address || !method,
  });

  return {
    result: data?.contractCall,
    loading,
    error,
    refetch,
  };
}

// Lazy version for on-demand calls
export function useContractCallLazy() {
  const [execute, { data, loading, error }] = useLazyQuery<{
    contractCall: ContractCallResult;
  }>(CONTRACT_CALL);

  const call = async (
    address: string,
    method: string,
    params?: any[],
    abi?: string
  ) => {
    const result = await execute({
      variables: {
        address,
        method,
        params: params ? JSON.stringify(params) : undefined,
        abi,
      },
    });
    return result.data?.contractCall;
  };

  return { call, data: data?.contractCall, loading, error };
}
```

#### useTransactionStatus Hook

```typescript
const TX_STATUS = gql`
  query TransactionStatus($txHash: Hash!) {
    transactionStatus(txHash: $txHash) {
      txHash
      status
      blockNumber
      blockHash
      confirmations
      gasUsed
    }
  }
`;

interface TransactionStatusResult {
  txHash: string;
  status: 'pending' | 'success' | 'failed' | 'confirmed' | 'not_found';
  blockNumber?: string;
  blockHash?: string;
  confirmations: string;
  gasUsed?: string;
}

export function useTransactionStatus(
  txHash: string,
  pollInterval?: number
) {
  const { data, loading, error, startPolling, stopPolling } = useQuery<{
    transactionStatus: TransactionStatusResult;
  }>(TX_STATUS, {
    variables: { txHash },
    skip: !txHash,
    pollInterval,
  });

  const status = data?.transactionStatus;

  return {
    status,
    loading,
    error,
    isPending: status?.status === 'pending',
    isSuccess: status?.status === 'success',
    isFailed: status?.status === 'failed',
    isConfirmed: status?.status === 'confirmed',
    startPolling,
    stopPolling,
  };
}
```

#### useTokenInfo Hook

```typescript
// 토큰 정보 조회 (name, symbol, decimals, totalSupply)
export function useTokenInfo(tokenAddress: string) {
  const { result: nameResult } = useContractCall(tokenAddress, 'name');
  const { result: symbolResult } = useContractCall(tokenAddress, 'symbol');
  const { result: decimalsResult } = useContractCall(tokenAddress, 'decimals');
  const { result: totalSupplyResult } = useContractCall(tokenAddress, 'totalSupply');

  const parseResult = (result: ContractCallResult | undefined) => {
    if (!result?.decoded || !result.result) return null;
    try {
      return JSON.parse(result.result);
    } catch {
      return result.result;
    }
  };

  return {
    name: parseResult(nameResult),
    symbol: parseResult(symbolResult),
    decimals: parseResult(decimalsResult),
    totalSupply: parseResult(totalSupplyResult),
    loading: !nameResult || !symbolResult || !decimalsResult || !totalSupplyResult,
  };
}
```

### 13.4 사용 예시

#### 토큰 잔액 표시 컴포넌트

```typescript
function TokenBalance({
  tokenAddress,
  walletAddress
}: {
  tokenAddress: string;
  walletAddress: string;
}) {
  const { result, loading, error } = useContractCall(
    tokenAddress,
    'balanceOf',
    [walletAddress]
  );

  if (loading) return <Spinner />;
  if (error) return <ErrorMessage error={error} />;

  const balance = result?.decoded
    ? formatUnits(JSON.parse(result.result), 18)
    : '0';

  return (
    <div>
      <span>Balance: {balance}</span>
    </div>
  );
}
```

#### 트랜잭션 추적 컴포넌트

```typescript
function TransactionTracker({ txHash }: { txHash: string }) {
  const {
    status,
    isPending,
    isSuccess,
    isConfirmed,
    isFailed
  } = useTransactionStatus(txHash, isPending ? 2000 : 0);

  useEffect(() => {
    if (isConfirmed) {
      // 확인 완료 시 폴링 중지
      stopPolling();
    }
  }, [isConfirmed]);

  return (
    <div>
      <h3>Transaction: {shortenHash(txHash)}</h3>

      {isPending && (
        <Badge color="yellow">
          <Spinner size="sm" /> Pending...
        </Badge>
      )}

      {isSuccess && !isConfirmed && (
        <Badge color="blue">
          Success ({status?.confirmations} confirmations)
        </Badge>
      )}

      {isConfirmed && (
        <Badge color="green">
          ✓ Confirmed ({status?.confirmations} confirmations)
        </Badge>
      )}

      {isFailed && (
        <Badge color="red">✗ Failed</Badge>
      )}

      {status?.gasUsed && (
        <div>Gas Used: {formatNumber(status.gasUsed)}</div>
      )}
    </div>
  );
}
```

#### Internal Transactions 뷰어

```typescript
const INTERNAL_TXS_RPC = gql`
  query InternalTransactionsRPC($txHash: Hash!) {
    internalTransactionsRPC(txHash: $txHash) {
      txHash
      totalCount
      internalTransactions {
        type
        from
        to
        value
        gasUsed
        error
        depth
      }
    }
  }
`;

function InternalTransactionsView({ txHash }: { txHash: string }) {
  const { data, loading, error } = useQuery(INTERNAL_TXS_RPC, {
    variables: { txHash },
  });

  if (loading) return <Spinner />;
  if (error) return <ErrorMessage error={error} />;

  const { internalTransactions, totalCount } = data.internalTransactionsRPC;

  return (
    <div>
      <h4>Internal Transactions ({totalCount})</h4>
      <table>
        <thead>
          <tr>
            <th>Type</th>
            <th>From</th>
            <th>To</th>
            <th>Value</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {internalTransactions.map((tx, idx) => (
            <tr key={idx} style={{ paddingLeft: tx.depth * 20 }}>
              <td>{tx.type}</td>
              <td>{shortenAddress(tx.from)}</td>
              <td>{shortenAddress(tx.to)}</td>
              <td>{formatEther(tx.value)} ETH</td>
              <td>{tx.error ? '❌' : '✅'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
```

### 13.5 캐싱 동작

RPC Proxy는 응답을 자동으로 캐싱하여 성능을 최적화합니다:

| 데이터 유형 | TTL | 설명 |
|------------|-----|------|
| Token Metadata (name, symbol) | 24시간 | 불변 데이터 |
| Balance Data | 15초 | 자주 변경됨 |
| Confirmed Tx Traces | 24시간 | 확인된 트랜잭션은 불변 |
| Transaction Status | 5초 | 상태 변경 가능 |
| Contract Call (기본) | 30초 | 블록마다 변경 가능 |

**캐시 우회:**
ABI를 명시적으로 제공하면 캐시된 ABI 대신 제공된 ABI를 사용합니다.

### 13.6 에러 처리

```typescript
// RPC Proxy 에러 타입
interface RPCProxyError {
  code: string;
  message: string;
}

const ERROR_CODES = {
  QUEUE_FULL: 'Request queue is full',
  CIRCUIT_OPEN: 'Circuit breaker is open',
  RATE_LIMITED: 'Request rate limited',
  TIMEOUT: 'Request timeout',
  CONTRACT_NOT_VERIFIED: 'Contract is not verified',
  ABI_NOT_FOUND: 'Contract ABI not found',
  METHOD_NOT_FOUND: 'Method not found in ABI',
};

// 에러 처리 예시
function handleRPCProxyError(error: Error) {
  const message = error.message;

  if (message.includes('CIRCUIT_OPEN')) {
    // RPC 노드 연결 문제 - 잠시 후 재시도
    return {
      retry: true,
      delay: 30000,
      userMessage: 'Network temporarily unavailable'
    };
  }

  if (message.includes('RATE_LIMITED')) {
    // 요청 제한 - 지수 백오프
    return {
      retry: true,
      delay: 5000,
      userMessage: 'Too many requests, please wait'
    };
  }

  if (message.includes('ABI_NOT_FOUND')) {
    // ABI 필요
    return {
      retry: false,
      userMessage: 'Please provide contract ABI'
    };
  }

  return {
    retry: false,
    userMessage: 'Unknown error occurred'
  };
}
```

### 13.7 TypeScript 타입 정의

```typescript
// GraphQL Types
export interface ContractCallInput {
  address: string;
  method: string;
  params?: string;
  abi?: string;
}

export interface ContractCallResult {
  result: string | null;
  rawResult: string;
  decoded: boolean;
}

export interface TransactionStatusResult {
  txHash: string;
  status: 'pending' | 'success' | 'failed' | 'confirmed' | 'not_found';
  blockNumber?: string;
  blockHash?: string;
  confirmations: string;
  gasUsed?: string;
}

export interface InternalTransactionRPC {
  type: 'CALL' | 'CREATE' | 'DELEGATECALL' | 'STATICCALL' | 'CALLCODE';
  from: string;
  to: string;
  value: string;
  gas: string;
  gasUsed: string;
  input?: string;
  output?: string;
  error?: string;
  depth: number;
  traceAddress: number[];
}

export interface InternalTransactionsRPCResult {
  txHash: string;
  internalTransactions: InternalTransactionRPC[];
  totalCount: number;
}

export interface RPCProxyMetrics {
  totalRequests: string;
  successfulRequests: string;
  failedRequests: string;
  cacheHits: string;
  cacheMisses: string;
  averageLatencyMs: string;
  queueDepth: number;
  activeWorkers: number;
  circuitState: 'closed' | 'open' | 'half-open';
}
```

### 13.8 기존 직접 RPC 호출에서 마이그레이션

#### Before (직접 RPC 호출)
```typescript
import { ethers } from 'ethers';

const provider = new ethers.JsonRpcProvider('https://rpc.example.com');
const contract = new ethers.Contract(address, abi, provider);

// 직접 호출
const balance = await contract.balanceOf(walletAddress);
```

#### After (RPC Proxy 사용)
```typescript
// GraphQL을 통한 호출
const { result } = useContractCall(address, 'balanceOf', [walletAddress]);
const balance = result?.decoded ? JSON.parse(result.result) : '0';
```

**마이그레이션 장점:**
1. ✅ RPC 엔드포인트 관리 불필요
2. ✅ 자동 캐싱으로 성능 향상
3. ✅ Rate limiting으로 안정성 보장
4. ✅ Circuit breaker로 장애 대응
5. ✅ 통합된 에러 처리
6. ✅ 모니터링 메트릭 제공

### 13.9 성능 최적화 팁

1. **배치 호출**: 여러 컨트랙트 호출이 필요한 경우, 개별 쿼리보다 한 번에 요청
```typescript
// 여러 토큰 정보를 한 번에 조회
const TOKEN_INFO = gql`
  query TokenInfo($addresses: [Address!]!) {
    tokens: contractCalls(addresses: $addresses, method: "symbol") {
      address
      result
    }
  }
`;
```

2. **폴링 최적화**: 필요한 경우에만 폴링 활성화
```typescript
const { status, startPolling, stopPolling } = useTransactionStatus(txHash);

// 트랜잭션 제출 직후에만 폴링
useEffect(() => {
  if (isPending) {
    startPolling(2000); // 2초 간격
  } else {
    stopPolling();
  }
}, [isPending]);
```

3. **캐시 활용**: 불변 데이터(name, symbol)는 캐시된 결과 재사용
```typescript
// Apollo Client 캐시 정책
const cache = new InMemoryCache({
  typePolicies: {
    Query: {
      fields: {
        contractCall: {
          keyArgs: ['address', 'method', 'params'],
          merge: true,
        },
      },
    },
  },
});
```

---

## 14. Fee Delegation API (NEW)

Fee Delegation은 사용자 대신 제3자(Fee Payer)가 가스 비용을 지불하는 기능입니다. `/gas` 페이지의 Fee Delegation Dashboard에서 사용됩니다.

### 14.1 개요

Fee Delegation 트랜잭션은 type `0x16` (22)로 식별됩니다. 이 API는 Fee Delegation 사용 통계, 상위 Fee Payer, 개별 Fee Payer 통계를 제공합니다.

### 14.2 GraphQL Queries

#### feeDelegationStats - 전체 통계

```graphql
query FeeDelegationStats($fromBlock: BigInt, $toBlock: BigInt) {
  feeDelegationStats(fromBlock: $fromBlock, toBlock: $toBlock) {
    totalFeeDelegatedTxs   # 총 Fee Delegation 트랜잭션 수
    totalFeesSaved         # 사용자가 절약한 총 가스비 (wei)
    adoptionRate           # Fee Delegation 사용률 (%)
    avgFeeSaved            # 트랜잭션당 평균 절약 가스비 (wei)
  }
}
```

**Example:**
```typescript
const { data } = await client.query({
  query: FEE_DELEGATION_STATS,
  variables: {
    fromBlock: '1000000',  // optional
    toBlock: '2000000',    // optional
  }
});

console.log(data.feeDelegationStats);
// {
//   totalFeeDelegatedTxs: "15000",
//   totalFeesSaved: "1500000000000000000000",
//   adoptionRate: 12.5,
//   avgFeeSaved: "100000000000000000"
// }
```

#### topFeePayers - 상위 Fee Payer 목록

```graphql
query TopFeePayers($limit: Int, $fromBlock: BigInt, $toBlock: BigInt) {
  topFeePayers(limit: $limit, fromBlock: $fromBlock, toBlock: $toBlock) {
    nodes {
      address          # Fee Payer 주소
      txCount          # 스폰서한 트랜잭션 수
      totalFeesPaid    # 지불한 총 가스비 (wei)
      percentage       # 전체 Fee Delegation 중 비율 (%)
    }
    totalCount         # 고유 Fee Payer 총 수
  }
}
```

**Example:**
```typescript
const { data } = await client.query({
  query: TOP_FEE_PAYERS,
  variables: {
    limit: 10,
    fromBlock: '1000000',
    toBlock: '2000000',
  }
});

console.log(data.topFeePayers);
// {
//   nodes: [
//     {
//       address: "0x1234...",
//       txCount: "5000",
//       totalFeesPaid: "500000000000000000000",
//       percentage: 33.33
//     },
//     ...
//   ],
//   totalCount: "150"
// }
```

#### feePayerStats - 특정 Fee Payer 통계

```graphql
query FeePayerStats(
  $address: Address!
  $fromBlock: BigInt
  $toBlock: BigInt
) {
  feePayerStats(
    address: $address
    fromBlock: $fromBlock
    toBlock: $toBlock
  ) {
    address          # Fee Payer 주소
    txCount          # 스폰서한 트랜잭션 수
    totalFeesPaid    # 지불한 총 가스비 (wei)
    percentage       # 전체 Fee Delegation 중 비율 (%)
  }
}
```

**Example:**
```typescript
const { data } = await client.query({
  query: FEE_PAYER_STATS,
  variables: {
    address: '0x1234567890abcdef...',
  }
});

console.log(data.feePayerStats);
// {
//   address: "0x1234...",
//   txCount: "5000",
//   totalFeesPaid: "500000000000000000000",
//   percentage: 33.33
// }
```

### 14.3 React Hooks

#### useFeeDelegationStats Hook

```typescript
import { useQuery, gql } from '@apollo/client';

const FEE_DELEGATION_STATS = gql`
  query FeeDelegationStats($fromBlock: BigInt, $toBlock: BigInt) {
    feeDelegationStats(fromBlock: $fromBlock, toBlock: $toBlock) {
      totalFeeDelegatedTxs
      totalFeesSaved
      adoptionRate
      avgFeeSaved
    }
  }
`;

interface FeeDelegationStats {
  totalFeeDelegatedTxs: string;
  totalFeesSaved: string;
  adoptionRate: number;
  avgFeeSaved: string;
}

interface UseFeeDelegationStatsOptions {
  fromBlock?: string;
  toBlock?: string;
}

export function useFeeDelegationStats(options?: UseFeeDelegationStatsOptions) {
  const { data, loading, error, refetch } = useQuery<{
    feeDelegationStats: FeeDelegationStats;
  }>(FEE_DELEGATION_STATS, {
    variables: {
      fromBlock: options?.fromBlock,
      toBlock: options?.toBlock,
    },
  });

  return {
    stats: data?.feeDelegationStats,
    loading,
    error,
    refetch,
  };
}
```

#### useTopFeePayers Hook

```typescript
const TOP_FEE_PAYERS = gql`
  query TopFeePayers($limit: Int, $fromBlock: BigInt, $toBlock: BigInt) {
    topFeePayers(limit: $limit, fromBlock: $fromBlock, toBlock: $toBlock) {
      nodes {
        address
        txCount
        totalFeesPaid
        percentage
      }
      totalCount
    }
  }
`;

interface FeePayerStats {
  address: string;
  txCount: string;
  totalFeesPaid: string;
  percentage: number;
}

interface TopFeePayersResult {
  nodes: FeePayerStats[];
  totalCount: string;
}

interface UseTopFeePayersOptions {
  limit?: number;
  fromBlock?: string;
  toBlock?: string;
}

export function useTopFeePayers(options?: UseTopFeePayersOptions) {
  const { data, loading, error, refetch } = useQuery<{
    topFeePayers: TopFeePayersResult;
  }>(TOP_FEE_PAYERS, {
    variables: {
      limit: options?.limit ?? 10,
      fromBlock: options?.fromBlock,
      toBlock: options?.toBlock,
    },
  });

  return {
    feePayers: data?.topFeePayers.nodes ?? [],
    totalCount: data?.topFeePayers.totalCount ?? '0',
    loading,
    error,
    refetch,
  };
}
```

#### useFeePayerStats Hook

```typescript
const FEE_PAYER_STATS = gql`
  query FeePayerStats(
    $address: Address!
    $fromBlock: BigInt
    $toBlock: BigInt
  ) {
    feePayerStats(
      address: $address
      fromBlock: $fromBlock
      toBlock: $toBlock
    ) {
      address
      txCount
      totalFeesPaid
      percentage
    }
  }
`;

interface UseFeePayerStatsOptions {
  address: string;
  fromBlock?: string;
  toBlock?: string;
}

export function useFeePayerStats(options: UseFeePayerStatsOptions) {
  const { data, loading, error, refetch } = useQuery<{
    feePayerStats: FeePayerStats;
  }>(FEE_PAYER_STATS, {
    variables: {
      address: options.address,
      fromBlock: options.fromBlock,
      toBlock: options.toBlock,
    },
    skip: !options.address,
  });

  return {
    stats: data?.feePayerStats,
    loading,
    error,
    refetch,
  };
}
```

### 14.4 사용 예시

#### Fee Delegation Dashboard 컴포넌트

```typescript
function FeeDelegationDashboard() {
  const { stats, loading: statsLoading } = useFeeDelegationStats();
  const { feePayers, totalCount, loading: payersLoading } = useTopFeePayers({
    limit: 10,
  });

  if (statsLoading || payersLoading) return <Spinner />;

  return (
    <div className="fee-delegation-dashboard">
      {/* 전체 통계 카드 */}
      <div className="stats-grid">
        <StatCard
          title="Total Fee Delegated Txs"
          value={formatNumber(stats?.totalFeeDelegatedTxs)}
        />
        <StatCard
          title="Total Fees Saved"
          value={formatEther(stats?.totalFeesSaved)}
          suffix="ETH"
        />
        <StatCard
          title="Adoption Rate"
          value={stats?.adoptionRate.toFixed(2)}
          suffix="%"
        />
        <StatCard
          title="Avg Fee Saved"
          value={formatEther(stats?.avgFeeSaved)}
          suffix="ETH"
        />
      </div>

      {/* 상위 Fee Payer 테이블 */}
      <div className="top-fee-payers">
        <h3>Top Fee Payers ({totalCount} total)</h3>
        <table>
          <thead>
            <tr>
              <th>Rank</th>
              <th>Address</th>
              <th>Tx Count</th>
              <th>Total Fees Paid</th>
              <th>Share</th>
            </tr>
          </thead>
          <tbody>
            {feePayers.map((payer, index) => (
              <tr key={payer.address}>
                <td>{index + 1}</td>
                <td>
                  <AddressLink address={payer.address} />
                </td>
                <td>{formatNumber(payer.txCount)}</td>
                <td>{formatEther(payer.totalFeesPaid)} ETH</td>
                <td>
                  <ProgressBar value={payer.percentage} />
                  {payer.percentage.toFixed(2)}%
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
```

#### 블록 범위 필터가 있는 Dashboard

```typescript
function FeeDelegationDashboardWithFilter() {
  const [blockRange, setBlockRange] = useState<{
    from?: string;
    to?: string;
  }>({});

  const { stats, loading, refetch } = useFeeDelegationStats({
    fromBlock: blockRange.from,
    toBlock: blockRange.to,
  });

  const handleFilterChange = (from: string, to: string) => {
    setBlockRange({ from, to });
    refetch({ fromBlock: from, toBlock: to });
  };

  return (
    <div>
      <BlockRangeFilter
        onFilter={handleFilterChange}
        initialFrom={blockRange.from}
        initialTo={blockRange.to}
      />

      {loading ? (
        <Spinner />
      ) : (
        <StatsDisplay stats={stats} />
      )}
    </div>
  );
}
```

#### Fee Payer 상세 페이지

```typescript
function FeePayerDetailPage({ address }: { address: string }) {
  const { stats, loading, error } = useFeePayerStats({ address });

  if (loading) return <Spinner />;
  if (error) return <ErrorMessage error={error} />;
  if (!stats) return <NotFound message="Fee Payer not found" />;

  return (
    <div className="fee-payer-detail">
      <h2>Fee Payer: {shortenAddress(address)}</h2>

      <div className="stats-grid">
        <StatCard
          title="Sponsored Transactions"
          value={formatNumber(stats.txCount)}
        />
        <StatCard
          title="Total Fees Paid"
          value={formatEther(stats.totalFeesPaid)}
          suffix="ETH"
        />
        <StatCard
          title="Market Share"
          value={stats.percentage.toFixed(2)}
          suffix="%"
        />
      </div>

      {/* 이 Fee Payer가 스폰서한 트랜잭션 목록 */}
      <FeeDelegatedTransactionsList feePayer={address} />
    </div>
  );
}
```

### 14.5 TypeScript 타입 정의

```typescript
// Fee Delegation Stats Types
export interface FeeDelegationStats {
  totalFeeDelegatedTxs: string;  // BigInt as string
  totalFeesSaved: string;        // BigInt as string (wei)
  adoptionRate: number;          // Percentage (0-100)
  avgFeeSaved: string;           // BigInt as string (wei)
}

export interface FeePayerStats {
  address: string;               // Ethereum address
  txCount: string;               // BigInt as string
  totalFeesPaid: string;         // BigInt as string (wei)
  percentage: number;            // Percentage (0-100)
}

export interface TopFeePayersResult {
  nodes: FeePayerStats[];
  totalCount: string;            // BigInt as string
}

// Query Variables Types
export interface FeeDelegationStatsVariables {
  fromBlock?: string;
  toBlock?: string;
}

export interface TopFeePayersVariables {
  limit?: number;
  fromBlock?: string;
  toBlock?: string;
}

export interface FeePayerStatsVariables {
  address: string;
  fromBlock?: string;
  toBlock?: string;
}
```

### 14.6 유틸리티 함수

```typescript
import { formatUnits } from 'ethers';

// Wei to Ether 변환
export function formatEther(wei?: string): string {
  if (!wei) return '0';
  try {
    return formatUnits(wei, 18);
  } catch {
    return '0';
  }
}

// 숫자 포맷팅 (comma separator)
export function formatNumber(value?: string): string {
  if (!value) return '0';
  try {
    return BigInt(value).toLocaleString();
  } catch {
    return value;
  }
}

// 가스비 계산 (wei to gwei)
export function formatGwei(wei?: string): string {
  if (!wei) return '0';
  try {
    return formatUnits(wei, 9);
  } catch {
    return '0';
  }
}

// 퍼센트 포맷팅
export function formatPercentage(value?: number, decimals = 2): string {
  if (value === undefined || value === null) return '0%';
  return `${value.toFixed(decimals)}%`;
}
```

### 14.7 캐싱 권장사항

Fee Delegation 통계는 계산 비용이 높을 수 있으므로 적절한 캐싱을 권장합니다:

```typescript
// Apollo Client 캐시 정책
const cache = new InMemoryCache({
  typePolicies: {
    Query: {
      fields: {
        feeDelegationStats: {
          keyArgs: ['fromBlock', 'toBlock'],
          merge: true,
        },
        topFeePayers: {
          keyArgs: ['limit', 'fromBlock', 'toBlock'],
          merge: true,
        },
        feePayerStats: {
          keyArgs: ['address', 'fromBlock', 'toBlock'],
          merge: true,
        },
      },
    },
  },
});
```

**권장 캐시 TTL:**
| Query | TTL | 설명 |
|-------|-----|------|
| feeDelegationStats | 5분 | 전체 통계 (블록 범위 지정 시 더 길게) |
| topFeePayers | 5분 | 상위 Fee Payer 목록 |
| feePayerStats | 5분 | 개별 Fee Payer 통계 |

블록 범위를 고정한 쿼리는 결과가 변하지 않으므로 더 긴 캐시 TTL(24시간)을 적용할 수 있습니다.

### 14.8 에러 처리

```typescript
function FeeDelegationDashboardWithErrorHandling() {
  const { stats, loading, error } = useFeeDelegationStats();

  if (loading) {
    return <LoadingState message="Loading fee delegation stats..." />;
  }

  if (error) {
    // 에러 유형별 처리
    if (error.message.includes('storage does not implement')) {
      return (
        <ErrorState
          title="Feature Not Available"
          message="Fee delegation statistics are not available on this network."
        />
      );
    }

    return (
      <ErrorState
        title="Error Loading Data"
        message={error.message}
        onRetry={() => window.location.reload()}
      />
    );
  }

  if (!stats) {
    return (
      <EmptyState
        title="No Data"
        message="No fee delegation transactions found."
      />
    );
  }

  return <StatsDisplay stats={stats} />;
}
```

### 14.9 성능 최적화

1. **블록 범위 제한**: 전체 기간 조회 시 성능이 저하될 수 있으므로 적절한 블록 범위 사용
2. **페이지네이션**: `topFeePayers`의 `limit` 파라미터 활용
3. **캐싱 활용**: Apollo Client 캐시 정책 설정
4. **지연 로딩**: 탭이나 스크롤 시 데이터 로딩

```typescript
// 지연 로딩 예시
function LazyFeeDelegationStats() {
  const [shouldLoad, setShouldLoad] = useState(false);
  const { stats, loading } = useFeeDelegationStats(
    shouldLoad ? {} : undefined
  );

  // Intersection Observer로 뷰포트 진입 시 로딩
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setShouldLoad(true);
        }
      },
      { threshold: 0.1 }
    );

    if (ref.current) {
      observer.observe(ref.current);
    }

    return () => observer.disconnect();
  }, []);

  return (
    <div ref={ref}>
      {loading ? <Skeleton /> : <StatsDisplay stats={stats} />}
    </div>
  );
}
```
