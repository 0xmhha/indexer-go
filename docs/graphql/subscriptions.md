# GraphQL Subscription Examples

## Overview

Subscriptions provide real-time updates for blockchain events. They use WebSocket protocol to push data to clients as events occur.

## WebSocket Endpoint

```
ws://localhost:8080/graphql
```

## Available Subscriptions

### New Block Subscription

Get notified when a new block is mined.

```graphql
subscription OnNewBlock {
  newBlock {
    number
    hash
    timestamp
    transactionCount
    gasUsed
    miner
  }
}
```

**Response Format**:
```json
{
  "data": {
    "newBlock": {
      "number": "12345",
      "hash": "0xabcd...",
      "timestamp": "1234567890",
      "transactionCount": 25,
      "gasUsed": "8000000",
      "miner": "0x1234..."
    }
  }
}
```

**Note**: Field name is `transactionCount`, not `txCount` (changed in Phase 1).

### New Transaction Subscription

Get notified when a new transaction is added to a block.

```graphql
subscription OnNewTransaction {
  newTransaction {
    hash
    blockNumber
    from
    to
    value
    gasUsed
    status
  }
}
```

**Response Format**:
```json
{
  "data": {
    "newTransaction": {
      "hash": "0x5678...",
      "blockNumber": "12345",
      "from": "0xabcd...",
      "to": "0xef01...",
      "value": "1000000000000000000",
      "gasUsed": "21000",
      "status": "1"
    }
  }
}
```

## Usage Examples

### JavaScript/TypeScript (Apollo Client)

```typescript
import { ApolloClient, InMemoryCache, split, HttpLink } from '@apollo/client';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { createClient } from 'graphql-ws';
import { getMainDefinition } from '@apollo/client/utilities';

// HTTP link for queries and mutations
const httpLink = new HttpLink({
  uri: 'http://localhost:8080/graphql',
});

// WebSocket link for subscriptions
const wsLink = new GraphQLWsLink(
  createClient({
    url: 'ws://localhost:8080/graphql',
  })
);

// Split based on operation type
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

// Subscribe to new blocks
const subscription = client
  .subscribe({
    query: gql`
      subscription OnNewBlock {
        newBlock {
          number
          hash
          timestamp
          transactionCount
        }
      }
    `,
  })
  .subscribe({
    next: (result) => {
      console.log('New block:', result.data.newBlock);
    },
    error: (error) => {
      console.error('Subscription error:', error);
    },
  });

// Unsubscribe when done
subscription.unsubscribe();
```

### React Hook

```typescript
import { useSubscription, gql } from '@apollo/client';

const NEW_BLOCK_SUBSCRIPTION = gql`
  subscription OnNewBlock {
    newBlock {
      number
      hash
      timestamp
      transactionCount
    }
  }
`;

function BlockMonitor() {
  const { data, loading, error } = useSubscription(NEW_BLOCK_SUBSCRIPTION);

  if (loading) return <p>Connecting...</p>;
  if (error) return <p>Error: {error.message}</p>;

  return (
    <div>
      <h2>Latest Block</h2>
      <p>Number: {data?.newBlock?.number}</p>
      <p>Hash: {data?.newBlock?.hash}</p>
      <p>Transactions: {data?.newBlock?.transactionCount}</p>
    </div>
  );
}
```

### Go Client

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/hasura/go-graphql-client"
)

type NewBlock struct {
    Number           string `graphql:"number"`
    Hash             string `graphql:"hash"`
    Timestamp        string `graphql:"timestamp"`
    TransactionCount int    `graphql:"transactionCount"`
}

func main() {
    client := graphql.NewSubscriptionClient("ws://localhost:8080/graphql")
    defer client.Close()

    var subscription struct {
        NewBlock NewBlock `graphql:"newBlock"`
    }

    _, err := client.Subscribe(&subscription, nil, func(data []byte, err error) error {
        if err != nil {
            log.Printf("Subscription error: %v", err)
            return err
        }

        fmt.Printf("New block: %s (tx: %d)\n",
            subscription.NewBlock.Number,
            subscription.NewBlock.TransactionCount)
        return nil
    })

    if err != nil {
        log.Fatal(err)
    }

    // Run forever
    if err := client.Run(); err != nil {
        log.Fatal(err)
    }
}
```

## Connection Management

### Reconnection Strategy

Implement exponential backoff for reconnections:

```typescript
let reconnectAttempt = 0;
const maxReconnectAttempts = 5;

const wsLink = new GraphQLWsLink(
  createClient({
    url: 'ws://localhost:8080/graphql',
    retryAttempts: maxReconnectAttempts,
    retryWait: async () => {
      const delay = Math.min(1000 * 2 ** reconnectAttempt, 30000);
      reconnectAttempt++;
      await new Promise((resolve) => setTimeout(resolve, delay));
    },
    on: {
      connected: () => {
        reconnectAttempt = 0;
        console.log('WebSocket connected');
      },
      error: (error) => {
        console.error('WebSocket error:', error);
      },
    },
  })
);
```

### Heartbeat/Keepalive

Configure keepalive to prevent connection timeout:

```typescript
const wsLink = new GraphQLWsLink(
  createClient({
    url: 'ws://localhost:8080/graphql',
    keepAlive: 10000, // Send ping every 10 seconds
  })
);
```

## Performance Considerations

1. **Connection Limits**: Limit the number of concurrent WebSocket connections
2. **Buffering**: Buffer rapid updates on the client side to avoid UI thrashing
3. **Selective Updates**: Subscribe only to data you're actively displaying
4. **Cleanup**: Always unsubscribe when components unmount to prevent memory leaks

## Error Handling

### Common Errors

```json
{
  "errors": [
    {
      "message": "connection timeout",
      "extensions": {
        "code": "INTERNAL_SERVER_ERROR"
      }
    }
  ]
}
```

### Handling Errors in React

```typescript
function BlockMonitor() {
  const { data, loading, error } = useSubscription(NEW_BLOCK_SUBSCRIPTION, {
    onError: (error) => {
      console.error('Subscription failed:', error);
      // Implement retry logic or user notification
    },
    shouldResubscribe: true, // Auto-resubscribe on error
  });

  if (error) {
    return (
      <div>
        <p>Connection error. Retrying...</p>
        <button onClick={() => window.location.reload()}>
          Refresh
        </button>
      </div>
    );
  }

  // ... rest of component
}
```

## Use Cases

### Real-time Dashboard

Monitor blockchain activity in real-time:

```graphql
subscription DashboardUpdates {
  newBlock {
    number
    timestamp
    transactionCount
    gasUsed
  }
}
```

### Transaction Monitoring

Track transactions from specific addresses:

```typescript
// Note: This would require a filtered subscription
// Current implementation provides all transactions
subscription OnNewTransaction {
  newTransaction {
    hash
    from
    to
    value
  }
}

// Client-side filtering
.subscribe({
  next: (result) => {
    const tx = result.data.newTransaction;
    if (tx.from === myAddress || tx.to === myAddress) {
      console.log('My transaction:', tx);
    }
  }
})
```

### Block Explorer

Real-time block explorer updates:

```typescript
const [blocks, setBlocks] = useState([]);

useSubscription(NEW_BLOCK_SUBSCRIPTION, {
  onData: ({ data }) => {
    setBlocks(prev => [data.data.newBlock, ...prev].slice(0, 10));
  }
});
```

## Best Practices

1. **Unsubscribe on Unmount**: Always clean up subscriptions
2. **Throttle Updates**: Use debouncing for rapid updates
3. **Error Recovery**: Implement automatic reconnection
4. **State Management**: Consider using Redux or similar for subscription data
5. **Memory Management**: Limit stored subscription data to prevent memory leaks
