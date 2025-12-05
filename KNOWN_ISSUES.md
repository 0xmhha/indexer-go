# Known Issues

This document tracks known issues in the indexer-go codebase.

---

## Flaky Tests

### TestEventBusStats
- **File**: `api/graphql/subscription_integration_test.go:737`
- **Symptom**: Test expects 5 events but sometimes receives 4
- **Frequency**: Intermittent (passes most of the time)
- **Root Cause**: Race condition in async event publishing timing
- **Impact**: CI may occasionally fail; does not affect production code
- **Discovered**: 2024-12-05 during Phase 1 refactoring review
- **Status**: OPEN

```
--- FAIL: TestEventBusStats (0.20s)
    subscription_integration_test.go:737: Subscriber stats: received=4, dropped=0
    subscription_integration_test.go:740: Expected 5 events received, got 4
```

**Workaround**: Re-run failed CI job; test usually passes on retry.

**Potential Fix**: Add synchronization or increase timeout in test to ensure all events are processed before assertion.

---

## Deprecated Features

(None currently)

---

## Performance Issues

(None currently)

---

## Change Log

| Date | Issue | Action |
|------|-------|--------|
| 2024-12-05 | TestEventBusStats flaky | Documented |
