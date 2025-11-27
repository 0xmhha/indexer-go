# GraphQL Best Practices

## Query Optimization

### 1. Request Only Needed Fields

❌ **Bad**: Requesting all fields
```graphql
query GetBlock($number: BigInt!) {
  block(number: $number) {
    number
    hash
    parentHash
    nonce
    sha3Uncles
    logsBloom
    transactionsRoot
    stateRoot
    receiptsRoot
    miner
    difficulty
    totalDifficulty
    extraData
    size
    gasLimit
    gasUsed
    timestamp
    transactions {
      # All transaction fields...
    }
    uncles
  }
}
```

✅ **Good**: Request only what you need
```graphql
query GetBlock($number: BigInt!) {
  block(number: $number) {
    number
    hash
    timestamp
    transactionCount
  }
}
```

**Impact**:
- Reduces response size by ~80%
- Faster response time
- Lower bandwidth usage

### 2. Use Aliases for Multiple Queries

❌ **Bad**: Multiple round trips
```typescript
// Three separate requests
const block1 = await client.query({ query: GET_BLOCK, variables: { number: "1000" } });
const block2 = await client.query({ query: GET_BLOCK, variables: { number: "2000" } });
const block3 = await client.query({ query: GET_BLOCK, variables: { number: "3000" } });
```

✅ **Good**: Single batched query
```graphql
query GetMultipleBlocks {
  block1000: block(number: "1000") {
    number
    hash
    timestamp
  }
  block2000: block(number: "2000") {
    number
    hash
    timestamp
  }
  block3000: block(number: "3000") {
    number
    hash
    timestamp
  }
}
```

**Impact**:
- Reduces from 3 requests to 1
- Lower latency
- Better server resource utilization

### 3. Implement Proper Pagination

❌ **Bad**: Fetching all data
```graphql
query GetAllProposals {
  proposals(
    filter: {
      fromBlock: "0"
      toBlock: "999999999"
    }
  ) {
    nodes {
      proposalId
      status
    }
  }
}
```

✅ **Good**: Paginated approach
```graphql
query GetProposalsPage {
  proposals(
    filter: {
      fromBlock: "0"
      toBlock: "999999999"
    }
    pagination: {
      limit: 50
      offset: 0
    }
  ) {
    nodes {
      proposalId
      status
    }
    pageInfo {
      hasNextPage
      totalCount
    }
  }
}
```

**Impact**:
- Faster initial load
- Better user experience
- Prevents timeout errors

## Filter Optimization

### 1. Filter at Source

❌ **Bad**: Client-side filtering
```typescript
// Fetch all, filter client-side
const allEvents = await client.query({
  query: GET_ALL_MINT_EVENTS,
  variables: {
    fromBlock: "0",
    toBlock: "999999999"
  }
});

const filtered = allEvents.data.mintEvents.nodes.filter(
  e => e.minter === targetMinter
);
```

✅ **Good**: Server-side filtering
```graphql
query GetMinterEvents($minter: Address!) {
  mintEvents(
    filter: {
      fromBlock: "0"
      toBlock: "999999999"
      minter: $minter
    }
    pagination: {
      limit: 100
    }
  ) {
    nodes {
      blockNumber
      amount
    }
  }
}
```

**Impact**:
- Transfers less data
- Faster query execution
- Lower memory usage

### 2. Use Appropriate Block Ranges

❌ **Bad**: Querying entire blockchain
```graphql
query GetRecentEvents {
  mintEvents(
    filter: {
      fromBlock: "0"
      toBlock: "999999999"
    }
  ) {
    nodes { ... }
  }
}
```

✅ **Good**: Specific time range
```typescript
const latestBlock = await getLatestBlockNumber();
const fromBlock = latestBlock - 1000; // Last ~1000 blocks

const events = await client.query({
  query: GET_MINT_EVENTS,
  variables: {
    fromBlock: fromBlock.toString(),
    toBlock: latestBlock.toString()
  }
});
```

**Impact**:
- Much faster queries
- Reduced server load
- More relevant results

## Caching Strategies

### 1. Cache Immutable Data

Block data is immutable once confirmed:

```typescript
import { ApolloClient, InMemoryCache } from '@apollo/client';

const client = new ApolloClient({
  cache: new InMemoryCache({
    typePolicies: {
      Block: {
        keyFields: ['number'],
        fields: {
          // Cache block data forever (it's immutable)
          hash: {
            read(cached) {
              return cached;
            }
          }
        }
      },
      Query: {
        fields: {
          block: {
            read(existing, { args, toReference }) {
              return existing || toReference({
                __typename: 'Block',
                number: args.number
              });
            }
          }
        }
      }
    }
  })
});
```

### 2. Different TTL for Different Data

```typescript
const GET_LATEST_BLOCK = gql`
  query GetLatestBlock {
    latestBlock {
      number
      timestamp
    }
  }
`;

// Short cache for latest block (5 seconds)
const { data } = useQuery(GET_LATEST_BLOCK, {
  pollInterval: 5000,
});

// Long cache for historical block (cache forever)
const { data: historicalData } = useQuery(GET_BLOCK, {
  variables: { number: "1000" },
  // No polling - block 1000 never changes
});
```

### 3. Use DataLoader Pattern

Batch and cache database lookups:

```typescript
import DataLoader from 'dataloader';

const blockLoader = new DataLoader(async (numbers) => {
  const blocks = await batchFetchBlocks(numbers);
  return numbers.map(num => blocks.find(b => b.number === num));
});

// Multiple requests batched into one
const [block1, block2, block3] = await Promise.all([
  blockLoader.load('1000'),
  blockLoader.load('2000'),
  blockLoader.load('3000')
]);
```

## Error Handling

### 1. Handle Partial Errors

```typescript
const { data, error } = useQuery(COMPLEX_QUERY);

if (error) {
  // Check if we have partial data
  if (data) {
    console.warn('Partial data received:', error);
    // Show partial results with warning
  } else {
    // Complete failure
    console.error('Query failed:', error);
    // Show error state
  }
}
```

### 2. Implement Retry Logic

```typescript
import { ApolloClient, ApolloLink, HttpLink } from '@apollo/client';
import { onError } from '@apollo/client/link/error';
import { RetryLink } from '@apollo/client/link/retry';

const retryLink = new RetryLink({
  delay: {
    initial: 300,
    max: 5000,
    jitter: true
  },
  attempts: {
    max: 3,
    retryIf: (error) => {
      // Retry on network errors, not on GraphQL errors
      return !!error && !error.result;
    }
  }
});

const errorLink = onError(({ graphQLErrors, networkError }) => {
  if (graphQLErrors) {
    graphQLErrors.forEach(({ message, locations, path }) =>
      console.error(
        `[GraphQL error]: Message: ${message}, Path: ${path}`
      )
    );
  }
  if (networkError) {
    console.error(`[Network error]: ${networkError}`);
  }
});

const client = new ApolloClient({
  link: ApolloLink.from([errorLink, retryLink, httpLink]),
  cache: new InMemoryCache(),
});
```

## Performance Monitoring

### 1. Track Query Performance

```typescript
import { ApolloLink } from '@apollo/client';

const perfLink = new ApolloLink((operation, forward) => {
  const startTime = Date.now();

  return forward(operation).map(response => {
    const duration = Date.now() - startTime;
    console.log(`Query ${operation.operationName} took ${duration}ms`);

    // Send to analytics
    if (duration > 1000) {
      analytics.track('slow_query', {
        operation: operation.operationName,
        duration,
        variables: operation.variables
      });
    }

    return response;
  });
});
```

### 2. Monitor Cache Hit Rate

```typescript
const cache = new InMemoryCache();

let cacheHits = 0;
let cacheMisses = 0;

cache.transformDocument = (document) => {
  // Track cache performance
  return document;
};

// Periodically log cache stats
setInterval(() => {
  const hitRate = cacheHits / (cacheHits + cacheMisses);
  console.log(`Cache hit rate: ${(hitRate * 100).toFixed(2)}%`);
}, 60000);
```

## Security Best Practices

### 1. Validate Input

```typescript
function isValidAddress(address: string): boolean {
  return /^0x[a-fA-F0-9]{40}$/.test(address);
}

function isValidBlockNumber(num: string): boolean {
  const n = parseInt(num, 10);
  return !isNaN(n) && n >= 0 && n < Number.MAX_SAFE_INTEGER;
}

// Before querying
if (!isValidAddress(userInput)) {
  throw new Error('Invalid address format');
}
```

### 2. Rate Limiting

```typescript
import { RateLimiter } from 'limiter';

const limiter = new RateLimiter({
  tokensPerInterval: 100,
  interval: 'minute'
});

async function makeQuery(query, variables) {
  await limiter.removeTokens(1);
  return client.query({ query, variables });
}
```

### 3. Sanitize Inputs

```typescript
import DOMPurify from 'dompurify';

function sanitizeInput(input: string): string {
  return DOMPurify.sanitize(input, {
    ALLOWED_TAGS: [],
    ALLOWED_ATTR: []
  });
}

// For display only
const displayAddress = sanitizeInput(userInput);
```

## Common Patterns

### 1. Infinite Scroll

```typescript
function useInfiniteProposals() {
  const [proposals, setProposals] = useState([]);
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(true);

  const loadMore = async () => {
    const { data } = await client.query({
      query: GET_PROPOSALS,
      variables: {
        filter: { fromBlock: "0", toBlock: "999999999" },
        pagination: { limit: 20, offset }
      }
    });

    setProposals(prev => [...prev, ...data.proposals.nodes]);
    setOffset(prev => prev + 20);
    setHasMore(data.proposals.pageInfo.hasNextPage);
  };

  return { proposals, loadMore, hasMore };
}
```

### 2. Real-time + Historical Data

```typescript
function useBlockData() {
  // Historical data
  const { data: historical } = useQuery(GET_BLOCKS, {
    variables: { fromBlock: "1000", toBlock: "2000" }
  });

  // Real-time updates
  const { data: realtime } = useSubscription(NEW_BLOCK_SUBSCRIPTION);

  // Combine
  const blocks = useMemo(() => {
    const all = [...(historical?.blocks?.nodes || [])];
    if (realtime?.newBlock) {
      all.push(realtime.newBlock);
    }
    return all.sort((a, b) => b.number - a.number);
  }, [historical, realtime]);

  return blocks;
}
```

### 3. Optimistic Updates

```typescript
const [createProposal] = useMutation(CREATE_PROPOSAL, {
  optimisticResponse: {
    createProposal: {
      __typename: 'Proposal',
      proposalId: 'temp-id',
      status: 'pending',
      // ... other fields
    }
  },
  update: (cache, { data }) => {
    const existing = cache.readQuery({ query: GET_PROPOSALS });
    cache.writeQuery({
      query: GET_PROPOSALS,
      data: {
        proposals: {
          nodes: [data.createProposal, ...existing.proposals.nodes]
        }
      }
    });
  }
});
```

## Recommended Client Configuration

```typescript
import { ApolloClient, InMemoryCache, HttpLink, ApolloLink } from '@apollo/client';
import { onError } from '@apollo/client/link/error';
import { RetryLink } from '@apollo/client/link/retry';

// Error handling
const errorLink = onError(({ graphQLErrors, networkError }) => {
  if (graphQLErrors) {
    graphQLErrors.forEach(({ message }) =>
      console.error(`GraphQL Error: ${message}`)
    );
  }
  if (networkError) {
    console.error(`Network Error: ${networkError}`);
  }
});

// Retry logic
const retryLink = new RetryLink({
  delay: { initial: 300, max: 5000, jitter: true },
  attempts: { max: 3 }
});

// HTTP connection
const httpLink = new HttpLink({
  uri: 'http://localhost:8080/graphql',
  credentials: 'same-origin',
});

// Combine links
const link = ApolloLink.from([errorLink, retryLink, httpLink]);

// Cache configuration
const cache = new InMemoryCache({
  typePolicies: {
    Block: { keyFields: ['number'] },
    Transaction: { keyFields: ['hash'] },
    Proposal: { keyFields: ['contract', 'proposalId'] },
  }
});

// Create client
export const client = new ApolloClient({
  link,
  cache,
  defaultOptions: {
    watchQuery: {
      errorPolicy: 'all',
    },
    query: {
      errorPolicy: 'all',
    },
  },
});
```

## Summary Checklist

- [ ] Request only needed fields
- [ ] Use pagination for large datasets
- [ ] Filter data at the server (not client)
- [ ] Implement proper caching strategy
- [ ] Handle errors gracefully
- [ ] Monitor query performance
- [ ] Validate and sanitize inputs
- [ ] Use appropriate poll intervals
- [ ] Batch related queries
- [ ] Clean up subscriptions on unmount
