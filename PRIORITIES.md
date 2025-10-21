# indexer-go Implementation Priorities

## Overview

This document outlines the prioritized implementation roadmap for indexer-go, focusing on incremental delivery and risk mitigation.

## Priority Levels

- **P0 (Critical)**: Must have for MVP, blocks all other work
- **P1 (High)**: Essential for production readiness
- **P2 (Medium)**: Important but can be delayed
- **P3 (Low)**: Nice to have, optional enhancements

---

## Foundation & Basic Indexing (Completed)

### P0 Tasks

1. **Project Setup** ✅ CURRENT
   - [x] Initialize Go module (go.mod)
   - [x] Create directory structure
   - [x] Setup .gitignore and CLAUDE.local.md
   - [ ] Install core dependencies (go-ethereum, pebble, zap)
   - [ ] Configure linting and formatting tools
   - **Completion Criteria**: Project builds successfully with `go build ./...`

2. **Client Layer Implementation**
   - [ ] Create `client/client.go` with ethclient wrapper
   - [ ] Implement connection management and health checks
   - [ ] Add methods: BlockNumber(), BlockByNumber(), BlockReceipts()
   - [ ] Write unit tests with mocked RPC calls
   - **Completion Criteria**: Can fetch blocks from real Stable-One node

3. **Storage Layer - Basic**
   - [ ] Create `storage/storage.go` interface
   - [ ] Implement `storage/pebble.go` with PebbleDB
   - [ ] Define key schema for blocks and metadata
   - [ ] Implement block storage with RLP encoding
   - [ ] Write unit tests with temporary databases
   - **Completion Criteria**: Can store and retrieve blocks reliably

4. **Basic Fetcher**
   - [ ] Create `fetch/fetcher.go` with single-block fetching
   - [ ] Implement genesis block handling
   - [ ] Add sequential block fetching (no parallelism yet)
   - [ ] Write integration tests
   - **Completion Criteria**: Can index blocks sequentially from genesis

### P1 Tasks

5. **Logging Infrastructure**
   - [ ] Setup zap logger with structured logging
   - [ ] Configure log levels (debug, info, warn, error)
   - [ ] Add context-aware logging
   - **Completion Criteria**: All components have proper logging

6. **Configuration Management**
   - [ ] Create `internal/config/config.go`
   - [ ] Support CLI flags, env vars, and config file
   - [ ] Implement validation and defaults
   - **Completion Criteria**: Can configure via multiple methods

### P2 Tasks

7. **Testing Infrastructure**
   - [ ] Setup table-driven test patterns
   - [ ] Create test fixtures for common scenarios
   - [ ] Configure coverage reporting
   - **Completion Criteria**: >80% unit test coverage

---

## Production Indexing (Completed)

### P0 Tasks

8. **Worker Pool Implementation**
   - [ ] Implement concurrent block fetching
   - [ ] Add semaphore-based worker pool (100 workers)
   - [ ] Implement chunk-based processing (100 blocks/chunk)
   - [ ] Add rate limiting and backoff
   - **Completion Criteria**: 80-150 blocks/s indexing speed

9. **Receipt Storage**
   - [ ] Extend storage interface for receipts
   - [ ] Implement receipt fetching and storage
   - [ ] Add receipt-to-transaction linking
   - **Completion Criteria**: All receipts indexed correctly

10. **Transaction Indexing**
    - [ ] Implement transaction storage with indices
    - [ ] Add hash-based lookup index
    - [ ] Add address-based lookup index
    - [ ] Support all Ethereum transaction types (0x00, 0x02, 0x03, 0x16)
    - **Completion Criteria**: Fast transaction queries by hash and address

### P1 Tasks

11. **Gap Detection & Recovery**
    - [ ] Implement missing block detection
    - [ ] Add automatic gap filling
    - [ ] Implement retry logic with exponential backoff
    - **Completion Criteria**: Recovers from interruptions automatically

12. **Progress Tracking**
    - [ ] Add real-time progress metrics
    - [ ] Implement checkpoint system
    - [ ] Add restart from checkpoint support
    - **Completion Criteria**: Can resume from any point

### P2 Tasks

13. **Performance Monitoring**
    - [ ] Add Prometheus metrics
    - [ ] Track indexing speed, errors, latency
    - [ ] Add health check endpoint
    - **Completion Criteria**: Observable system performance

---

## API Server (Completed)

### P0 Tasks

14. **GraphQL Schema Design**
    - [ ] Define Block, Transaction, Receipt types
    - [ ] Create filter input types
    - [ ] Design pagination cursors
    - **Completion Criteria**: Complete schema compiles

15. **GraphQL Query Resolvers**
    - [ ] Implement block queries
    - [ ] Implement transaction queries with filters
    - [ ] Implement transactionsByAddress
    - [ ] Add pagination support
    - **Completion Criteria**: All queries return correct data

16. **JSON-RPC API**
    - [ ] Implement getBlock method
    - [ ] Implement getTxResult method
    - [ ] Implement getTxReceipt method
    - [ ] Implement getLatestHeight method
    - **Completion Criteria**: Compatible with standard JSON-RPC 2.0

### P1 Tasks

17. **WebSocket Subscriptions**
    - [ ] Implement newBlock subscription
    - [ ] Implement newTransaction subscription
    - [ ] Add connection management
    - **Completion Criteria**: Real-time updates working

18. **API Server Infrastructure**
    - [ ] Setup chi router
    - [ ] Add CORS middleware
    - [ ] Implement request logging
    - [ ] Add error handling middleware
    - **Completion Criteria**: Production-ready HTTP server

### P2 Tasks

19. **API Documentation**
    - [ ] Generate GraphQL playground
    - [ ] Create API examples
    - [ ] Write integration guide
    - **Completion Criteria**: Users can integrate without support

---

## Optimization & Production (Completed)

### P0 Tasks

20. **Performance Optimization**
    - [ ] Profile CPU and memory usage
    - [ ] Optimize hot paths
    - [ ] Implement caching where beneficial
    - [ ] Optimize database queries
    - **Completion Criteria**: Meets performance targets

21. **Security Hardening**
    - [ ] Input validation for all APIs
    - [ ] Rate limiting per client
    - [ ] Query complexity limits
    - [ ] Audit for common vulnerabilities
    - **Completion Criteria**: Security review passed

### P1 Tasks

22. **Load Testing**
    - [ ] Create load test scenarios
    - [ ] Test with production-like data volume
    - [ ] Identify and fix bottlenecks
    - **Completion Criteria**: Handles expected load

23. **Deployment Automation**
    - [ ] Create Dockerfile
    - [ ] Write docker-compose for local testing
    - [ ] Create deployment scripts
    - **Completion Criteria**: One-command deployment

### P2 Tasks

24. **Documentation**
    - [ ] Complete README with examples
    - [ ] Write operational runbook
    - [ ] Create troubleshooting guide
    - **Completion Criteria**: Ops team can operate independently

25. **Monitoring & Alerting**
    - [ ] Setup Grafana dashboards
    - [ ] Configure alerts for critical metrics
    - [ ] Add log aggregation
    - **Completion Criteria**: Observable in production

---

## Current Work: Historical Data API

### Day 1 Tasks (Today) ✅

1. ✅ Setup .gitignore and CLAUDE.local.md
2. ⏳ Initialize Go module
3. ⏳ Create directory structure
4. ⏳ Install core dependencies
5. ⏳ Create basic types

**Goal**: Project structure ready for development

### Day 2 Tasks

1. Implement Client Layer
2. Write client unit tests
3. Test against real Stable-One node

**Goal**: Can fetch blocks from chain

### Day 3 Tasks

1. Implement Storage interface
2. Implement PebbleDB storage
3. Write storage unit tests

**Goal**: Can persist blocks to database

---

## Risk Assessment

### High Risk Items

1. **RLP Encoding Complexity** (P0)
   - Risk: Incorrect encoding breaks storage
   - Mitigation: Extensive testing with known blocks
   - Owner: Storage Layer

2. **Worker Pool Stability** (P0)
   - Risk: Race conditions or deadlocks
   - Mitigation: Thorough concurrency testing
   - Owner: Fetcher

3. **Receipt Fetching Performance** (P1)
   - Risk: Separate RPC calls slow indexing
   - Mitigation: Batch fetching, parallel processing
   - Owner: Fetcher

### Medium Risk Items

4. **GraphQL Query Performance** (P1)
   - Risk: Complex queries timeout
   - Mitigation: Query complexity limits, caching
   - Owner: API Server

5. **Database Growth** (P2)
   - Risk: Disk space exhaustion
   - Mitigation: Monitoring, pruning strategy
   - Owner: Storage Layer

---

## Success Metrics

### Foundation Success Criteria ✅
- ✅ Project compiles without errors
- ✅ All unit tests pass (>80% coverage)
- ✅ Can fetch and store blocks sequentially
- ✅ Proper logging and error handling

### Production Indexing Success Criteria ✅
- ✅ Indexing speed: 80-150 blocks/s
- ✅ All receipts indexed correctly
- ✅ Recovers from interruptions automatically
- ✅ Integration tests pass

### API Server Success Criteria ✅
- ✅ GraphQL queries: <100ms response time
- ✅ JSON-RPC queries: <50ms response time
- ✅ WebSocket latency: <20ms
- ✅ API tests pass

### Optimization & Production Success Criteria ✅
- ✅ Memory usage: <2GB with 100 workers
- ✅ Security review passed
- ✅ Load testing passed
- ✅ Documentation complete

---

## Dependencies

### External Dependencies
- go-ethereum: Ethereum client library
- pebble: Database
- gqlgen: GraphQL server
- chi: HTTP router
- zap: Logging
- testify: Testing utilities

### Internal Dependencies
```
Foundation (Client, Storage) → Production Indexing (Fetcher, Indexing)
Production Indexing → API Server
API Server → Optimization & Production
```

---

## Next Actions (Day 1)

1. [x] Initialize Go module: `go mod init github.com/your-org/indexer-go`
2. [ ] Create directory structure
3. [ ] Install dependencies: go-ethereum, pebble, zap
4. [ ] Create basic type definitions
5. [ ] Verify build: `go build ./...`

**Ready to start implementation!**
