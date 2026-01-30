# WebSocket Resilience API Reference

WebSocket 연결 복구 및 이벤트 재생 시스템 가이드입니다.

**Last Updated**: 2026-01-31

---

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Session Management](#session-management)
- [Event Caching](#event-caching)
- [Recovery Protocol](#recovery-protocol)
- [Client Integration](#client-integration)
- [Best Practices](#best-practices)

---

## Overview

WebSocket Resilience 시스템은 네트워크 불안정 상황에서도 이벤트 손실 없이 안정적인 실시간 통신을 보장합니다:

- **세션 지속성** - 연결 끊김 후에도 세션 유지 (기본 24시간)
- **이벤트 캐싱** - 1시간 윈도우 내 이벤트 재생 지원
- **자동 복구** - 재연결 시 놓친 이벤트 자동 전달
- **상태 추적** - 세션별 구독 및 마지막 이벤트 추적

### Session State Flow

```
           connect
    ┌─────────────────┐
    │                 ▼
  ┌───────┐    ┌──────────┐    disconnect    ┌──────────────┐
  │ (new) │───▶│  ACTIVE  │───────────────▶│ DISCONNECTED │
  └───────┘    └──────────┘                 └──────────────┘
                    ▲                              │
                    │      reconnect (< TTL)      │
                    └──────────────────────────────┘
                                                   │
                                    TTL expired    ▼
                                              ┌─────────┐
                                              │ EXPIRED │
                                              └─────────┘
```

---

## Quick Start

### 1. Configuration 설정

```yaml
# config.yaml
resilience:
  enabled: true
  session:
    ttl: 24h              # 세션 유지 시간
    cleanup_period: 1h    # 만료 세션 정리 주기
  event_cache:
    window: 1h            # 이벤트 캐시 윈도우
    backend: "pebble"     # 또는 "redis"
    # redis:
    #   addr: "localhost:6379"
    #   password: ""
    #   db: 0
```

### 2. Client 연결 흐름

```javascript
// 1. 최초 연결
const ws = new WebSocket('ws://localhost:8080/graphql');

// 2. 세션 ID 저장
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'connection_ack') {
    localStorage.setItem('sessionId', msg.payload.sessionId);
  }
};

// 3. 재연결 시 세션 복구
const sessionId = localStorage.getItem('sessionId');
const lastEventId = localStorage.getItem('lastEventId');

ws.send(JSON.stringify({
  type: 'resume',
  payload: { sessionId, lastEventId }
}));
```

---

## Configuration

### ResilienceConfig

| 필드 | 타입 | 기본값 | 설명 |
|------|------|--------|------|
| `enabled` | bool | false | Resilience 기능 활성화 |
| `session.ttl` | duration | 24h | 세션 유지 시간 |
| `session.cleanup_period` | duration | 1h | 만료 세션 정리 주기 |
| `event_cache.window` | duration | 1h | 이벤트 캐시 윈도우 |
| `event_cache.backend` | string | "pebble" | 캐시 백엔드 (pebble/redis) |

### Backend Selection

| Backend | 장점 | 단점 | 사용 시나리오 |
|---------|------|------|--------------|
| `pebble` | 추가 인프라 불필요, 빠른 로컬 I/O | 단일 노드 제한 | 개발, 단일 서버 |
| `redis` | 다중 노드 공유, 고가용성 | 추가 인프라 필요 | 프로덕션, 클러스터 |

---

## Session Management

### Session Structure

```go
type Session struct {
    ID            string                   // 고유 세션 ID
    ClientID      string                   // 클라이언트 식별자
    State         SessionState             // active/disconnected/expired
    Subscriptions map[string]*SubState     // 구독 상태
    LastEventID   string                   // 마지막 수신 이벤트 ID
    LastSeen      time.Time                // 마지막 활동 시간
    CreatedAt     time.Time                // 생성 시간
    TTL           time.Duration            // 세션 만료 시간
}
```

### Session States

| 상태 | 설명 | 전환 조건 |
|------|------|----------|
| `ACTIVE` | 연결 활성화 상태 | 클라이언트 연결됨 |
| `DISCONNECTED` | 연결 끊김, 복구 대기 | 연결 끊김 감지 |
| `EXPIRED` | 세션 만료, 재사용 불가 | TTL 초과 |

### Subscription State

```go
type SubState struct {
    Topic       string    // 구독 토픽 (query hash)
    LastEventID string    // 해당 토픽의 마지막 이벤트
    CreatedAt   time.Time // 구독 시작 시간
    Active      bool      // 활성 상태
}
```

---

## Event Caching

### CachedEvent Structure

```go
type CachedEvent struct {
    ID        string      // 이벤트 ID (정렬 가능)
    SessionID string      // 대상 세션
    EventType string      // 이벤트 타입
    Payload   []byte      // 직렬화된 페이로드
    Timestamp time.Time   // 발생 시간
    Delivered bool        // 전달 완료 여부
    Topic     string      // 구독 토픽
}
```

### Event Cache Operations

| 연산 | 설명 |
|------|------|
| `Store` | 새 이벤트 캐시 저장 |
| `GetAfter` | 특정 이벤트 ID 이후 이벤트 조회 |
| `GetBySession` | 세션의 모든 캐시된 이벤트 조회 |
| `Cleanup` | 윈도우 초과 이벤트 정리 |

### Cache Retention

```
┌────────────────────────────────────────────────────────────┐
│                    Event Cache Window (1h)                  │
├────────────────────────────────────────────────────────────┤
│ E1 │ E2 │ E3 │ ... │ E100 │ E101 │ ... │ E200 │ ← 최신    │
│◄──────────── 재생 가능 ──────────────────────────────────►│
└────────────────────────────────────────────────────────────┘
     ▲
     │ window 초과 시 자동 삭제
```

---

## Recovery Protocol

### Message Types

| 타입 | 방향 | 설명 |
|------|------|------|
| `resume` | Client → Server | 세션 재연결 요청 |
| `resumed` | Server → Client | 재연결 성공 응답 |
| `replay_start` | Server → Client | 이벤트 재생 시작 |
| `event` | Server → Client | 이벤트 전달 (replay 포함) |
| `replay_end` | Server → Client | 이벤트 재생 완료 |
| `ack` | Client → Server | 이벤트 수신 확인 |

### Protocol Flow

```
┌──────────┐                                    ┌──────────┐
│  Client  │                                    │  Server  │
└────┬─────┘                                    └────┬─────┘
     │                                               │
     │  1. resume                                    │
     │  {sessionId: "sess_123",                      │
     │   lastEventId: "evt_456"}                     │
     │───────────────────────────────────────────────►
     │                                               │
     │  2. resumed                                   │
     │  {sessionId: "sess_123",                      │
     │   missedEvents: 5,                            │
     │   status: "ok"}                               │
     │◄───────────────────────────────────────────────
     │                                               │
     │  3. replay_start                              │
     │  {count: 5}                                   │
     │◄───────────────────────────────────────────────
     │                                               │
     │  4. event (replay)                            │
     │  {payload: {...},                             │
     │   meta: {eventId: "evt_457", replay: true}}   │
     │◄───────────────────────────────────────────────
     │                                               │
     │  ... (4 more events)                          │
     │                                               │
     │  5. replay_end                                │
     │◄───────────────────────────────────────────────
     │                                               │
     │  6. event (live)                              │
     │  {payload: {...},                             │
     │   meta: {eventId: "evt_462"}}                 │
     │◄───────────────────────────────────────────────
     │                                               │
```

### Request/Response Formats

#### Resume Request

```json
{
  "type": "resume",
  "payload": {
    "sessionId": "sess_abc123",
    "lastEventId": "evt_xyz789"
  }
}
```

#### Resumed Response

```json
{
  "type": "resumed",
  "payload": {
    "sessionId": "sess_abc123",
    "missedEvents": 5,
    "status": "ok"
  }
}
```

#### Event with Metadata

```json
{
  "type": "event",
  "payload": {
    "data": {
      "newBlock": {
        "number": "12345678",
        "hash": "0x..."
      }
    }
  },
  "meta": {
    "eventId": "evt_abc123",
    "replay": true
  }
}
```

---

## Client Integration

### JavaScript/TypeScript Client

```typescript
class ResilientWebSocket {
  private ws: WebSocket;
  private sessionId: string | null;
  private lastEventId: string | null;
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 10;

  constructor(private url: string) {
    this.sessionId = localStorage.getItem('ws_sessionId');
    this.lastEventId = localStorage.getItem('ws_lastEventId');
    this.connect();
  }

  private connect() {
    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      this.reconnectAttempts = 0;

      if (this.sessionId) {
        // 재연결: 세션 복구 요청
        this.ws.send(JSON.stringify({
          type: 'resume',
          payload: {
            sessionId: this.sessionId,
            lastEventId: this.lastEventId
          }
        }));
      }
    };

    this.ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      this.handleMessage(msg);
    };

    this.ws.onclose = () => {
      this.scheduleReconnect();
    };

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error);
    };
  }

  private handleMessage(msg: any) {
    switch (msg.type) {
      case 'connection_ack':
        // 새 세션 ID 저장
        this.sessionId = msg.payload.sessionId;
        localStorage.setItem('ws_sessionId', this.sessionId);
        break;

      case 'resumed':
        console.log(`Session resumed, ${msg.payload.missedEvents} events to replay`);
        break;

      case 'replay_start':
        console.log(`Starting replay of ${msg.payload.count} events`);
        break;

      case 'event':
        // 마지막 이벤트 ID 저장
        if (msg.meta?.eventId) {
          this.lastEventId = msg.meta.eventId;
          localStorage.setItem('ws_lastEventId', this.lastEventId);
        }

        // 이벤트 처리
        if (msg.meta?.replay) {
          this.handleReplayEvent(msg.payload);
        } else {
          this.handleLiveEvent(msg.payload);
        }
        break;

      case 'replay_end':
        console.log('Replay completed, now receiving live events');
        break;
    }
  }

  private handleReplayEvent(payload: any) {
    // 재생 이벤트 처리 (UI 업데이트 배치 가능)
    console.log('Replay event:', payload);
  }

  private handleLiveEvent(payload: any) {
    // 실시간 이벤트 처리
    console.log('Live event:', payload);
  }

  private scheduleReconnect() {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.error('Max reconnect attempts reached');
      return;
    }

    const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), 30000);
    this.reconnectAttempts++;

    console.log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);
    setTimeout(() => this.connect(), delay);
  }

  public send(data: any) {
    if (this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data));
    }
  }

  public close() {
    this.ws.close();
  }
}

// 사용
const ws = new ResilientWebSocket('ws://localhost:8080/graphql');
```

### React Hook

```typescript
import { useEffect, useRef, useState, useCallback } from 'react';

interface UseResilientWSOptions {
  url: string;
  onEvent?: (event: any) => void;
  onReplayStart?: (count: number) => void;
  onReplayEnd?: () => void;
}

export function useResilientWebSocket(options: UseResilientWSOptions) {
  const { url, onEvent, onReplayStart, onReplayEnd } = options;
  const wsRef = useRef<WebSocket | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [isReplaying, setIsReplaying] = useState(false);

  const connect = useCallback(() => {
    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => {
      setIsConnected(true);

      const sessionId = localStorage.getItem('ws_sessionId');
      const lastEventId = localStorage.getItem('ws_lastEventId');

      if (sessionId) {
        ws.send(JSON.stringify({
          type: 'resume',
          payload: { sessionId, lastEventId }
        }));
      }
    };

    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);

      switch (msg.type) {
        case 'connection_ack':
          localStorage.setItem('ws_sessionId', msg.payload.sessionId);
          break;
        case 'replay_start':
          setIsReplaying(true);
          onReplayStart?.(msg.payload.count);
          break;
        case 'replay_end':
          setIsReplaying(false);
          onReplayEnd?.();
          break;
        case 'event':
          if (msg.meta?.eventId) {
            localStorage.setItem('ws_lastEventId', msg.meta.eventId);
          }
          onEvent?.(msg.payload);
          break;
      }
    };

    ws.onclose = () => {
      setIsConnected(false);
      setTimeout(connect, 3000);
    };
  }, [url, onEvent, onReplayStart, onReplayEnd]);

  useEffect(() => {
    connect();
    return () => wsRef.current?.close();
  }, [connect]);

  return { isConnected, isReplaying };
}

// 사용
function BlockSubscription() {
  const [blocks, setBlocks] = useState<any[]>([]);

  const { isConnected, isReplaying } = useResilientWebSocket({
    url: 'ws://localhost:8080/graphql',
    onEvent: (event) => {
      setBlocks((prev) => [event.data.newBlock, ...prev].slice(0, 100));
    },
    onReplayStart: (count) => {
      console.log(`Replaying ${count} missed blocks`);
    },
    onReplayEnd: () => {
      console.log('Caught up with live blocks');
    },
  });

  return (
    <div>
      <div>Status: {isConnected ? (isReplaying ? 'Replaying' : 'Live') : 'Disconnected'}</div>
      {blocks.map((block) => (
        <div key={block.hash}>{block.number}</div>
      ))}
    </div>
  );
}
```

---

## Best Practices

### 1. 세션 ID 영구 저장

```typescript
// localStorage 사용 (브라우저 새로고침 후에도 유지)
localStorage.setItem('ws_sessionId', sessionId);

// IndexedDB 사용 (대용량 데이터)
const db = await openDB('websocket', 1);
await db.put('session', { id: sessionId, lastEventId });
```

### 2. 이벤트 ID 동기화

```typescript
// 모든 이벤트 수신 시 마지막 ID 저장
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.meta?.eventId) {
    localStorage.setItem('ws_lastEventId', msg.meta.eventId);
  }
};
```

### 3. 재연결 전략

```typescript
// Exponential backoff with jitter
function getReconnectDelay(attempt: number): number {
  const base = 1000;
  const max = 30000;
  const delay = Math.min(base * Math.pow(2, attempt), max);
  const jitter = delay * 0.1 * Math.random();
  return delay + jitter;
}
```

### 4. Replay 이벤트 처리

```typescript
// Replay 이벤트는 배치로 처리하여 UI 성능 최적화
let replayBuffer: any[] = [];
let flushTimeout: number;

function handleEvent(event: any, isReplay: boolean) {
  if (isReplay) {
    replayBuffer.push(event);
    clearTimeout(flushTimeout);
    flushTimeout = setTimeout(() => {
      updateUI(replayBuffer);
      replayBuffer = [];
    }, 100);
  } else {
    updateUI([event]);
  }
}
```

### 5. 오류 복구

```typescript
// 세션 만료 시 새 세션 시작
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'error' && msg.payload.code === 'SESSION_EXPIRED') {
    localStorage.removeItem('ws_sessionId');
    localStorage.removeItem('ws_lastEventId');
    ws.close();
    // 새 연결 시작
    connect();
  }
};
```

### 6. 서버 설정 최적화

```yaml
resilience:
  session:
    ttl: 24h              # 긴 세션 TTL로 재연결 기회 확보
    cleanup_period: 1h
  event_cache:
    window: 1h            # 일반적인 장애 복구에 충분
    backend: "redis"      # 프로덕션 환경
    redis:
      addr: "redis-cluster:6379"
```

---

## 참고 문서

- [EVENT_SUBSCRIPTION_API.md](./EVENT_SUBSCRIPTION_API.md) - 이벤트 구독 시스템
- [MULTICHAIN.md](./MULTICHAIN.md) - Multi-Chain 관리
- [WATCHLIST_API.md](./WATCHLIST_API.md) - 주소 모니터링
- [FRONTEND_SUBSCRIPTION_GUIDE.md](./FRONTEND_SUBSCRIPTION_GUIDE.md) - 프론트엔드 통합
