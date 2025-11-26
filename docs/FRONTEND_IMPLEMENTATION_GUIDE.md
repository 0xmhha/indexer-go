# Frontend Implementation Guide - Consensus Event System

**Version**: 1.0
**Last Updated**: 2025-01-26
**Target Audience**: Frontend Developers
**Tech Stack**: React + TypeScript + Apollo Client

---

## 📑 목차

1. [프로젝트 설정](#1-프로젝트-설정)
2. [WebSocket 연결 구성](#2-websocket-연결-구성)
3. [구현 가능한 기능들](#3-구현-가능한-기능들)
4. [상태 관리 전략](#4-상태-관리-전략)
5. [컴포넌트 구현 가이드](#5-컴포넌트-구현-가이드)
6. [에러 처리 및 복구](#6-에러-처리-및-복구)
7. [성능 최적화](#7-성능-최적화)
8. [테스트 전략](#8-테스트-전략)

---

## 1. 프로젝트 설정

### 1.1 필수 Dependencies 설치

```bash
# Core dependencies
npm install @apollo/client graphql graphql-ws

# TypeScript types
npm install -D @types/node

# UI libraries (선택사항)
npm install react-toastify chart.js react-chartjs-2
npm install @headlessui/react @heroicons/react

# State management (선택사항)
npm install zustand  # 또는 redux, recoil
```

### 1.2 프로젝트 구조

```
src/
├── config/
│   └── apollo.config.ts          # Apollo Client 설정
├── types/
│   └── consensus.types.ts        # TypeScript 타입 정의
├── hooks/
│   ├── useConsensusBlock.ts      # 블록 구독 훅
│   ├── useConsensusError.ts      # 에러 구독 훅
│   ├── useConsensusFork.ts       # 포크 구독 훅
│   └── useValidatorChange.ts     # Validator 변경 구독 훅
├── stores/
│   └── consensusStore.ts         # 전역 상태 관리
├── components/
│   ├── ConsensusBlock/
│   │   ├── BlockCard.tsx
│   │   └── BlockList.tsx
│   ├── ConsensusError/
│   │   ├── ErrorAlert.tsx
│   │   └── ErrorHistory.tsx
│   ├── ConsensusFork/
│   │   └── ForkDetector.tsx
│   ├── ValidatorChange/
│   │   └── ValidatorChangeCard.tsx
│   └── Dashboard/
│       ├── ConsensusDashboard.tsx
│       ├── NetworkHealth.tsx
│       └── ValidatorMetrics.tsx
└── utils/
    ├── formatters.ts             # 데이터 포매팅 유틸
    └── notifications.ts          # 알림 시스템
```

---

## 2. WebSocket 연결 구성

### 2.1 Apollo Client 설정 (`config/apollo.config.ts`)

```typescript
import { ApolloClient, InMemoryCache, split, HttpLink, ApolloLink } from '@apollo/client';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { getMainDefinition } from '@apollo/client/utilities';
import { createClient } from 'graphql-ws';
import { onError } from '@apollo/client/link/error';

// 환경변수에서 URL 가져오기
const HTTP_URL = process.env.REACT_APP_GRAPHQL_HTTP || 'http://localhost:8080/graphql';
const WS_URL = process.env.REACT_APP_GRAPHQL_WS || 'ws://localhost:8080/subscriptions';

// HTTP link for queries and mutations
const httpLink = new HttpLink({
  uri: HTTP_URL,
  credentials: 'same-origin',
});

// WebSocket link for subscriptions
const wsLink = new GraphQLWsLink(
  createClient({
    url: WS_URL,
    connectionParams: () => {
      // 인증 토큰이 필요한 경우
      const token = localStorage.getItem('auth_token');
      return {
        authorization: token ? `Bearer ${token}` : '',
      };
    },
    // 재연결 설정
    retryAttempts: 5,
    retryWait: (retries) => Math.min(1000 * Math.pow(2, retries), 30000),
    shouldRetry: () => true,
    // 연결 상태 로깅
    on: {
      connected: () => console.log('✅ WebSocket connected'),
      closed: () => console.log('❌ WebSocket closed'),
      error: (error) => console.error('WebSocket error:', error),
    },
  })
);

// Error handling
const errorLink = onError(({ graphQLErrors, networkError, operation }) => {
  if (graphQLErrors) {
    graphQLErrors.forEach(({ message, locations, path }) => {
      console.error(
        `[GraphQL error]: Message: ${message}, Location: ${locations}, Path: ${path}`
      );
    });
  }

  if (networkError) {
    console.error(`[Network error]: ${networkError}`);
    // 네트워크 에러 시 사용자에게 알림
    // toast.error('네트워크 연결에 문제가 있습니다.');
  }
});

// Split traffic: subscriptions -> WebSocket, queries/mutations -> HTTP
const splitLink = split(
  ({ query }) => {
    const definition = getMainDefinition(query);
    return (
      definition.kind === 'OperationDefinition' &&
      definition.operation === 'subscription'
    );
  },
  wsLink,
  ApolloLink.from([errorLink, httpLink])
);

// Apollo Client instance
export const apolloClient = new ApolloClient({
  link: splitLink,
  cache: new InMemoryCache({
    typePolicies: {
      Query: {
        fields: {
          // 필요한 경우 캐시 정책 설정
        },
      },
    },
  }),
  defaultOptions: {
    watchQuery: {
      fetchPolicy: 'cache-and-network',
      errorPolicy: 'all',
    },
    query: {
      fetchPolicy: 'network-only',
      errorPolicy: 'all',
    },
    mutate: {
      errorPolicy: 'all',
    },
  },
});

// WebSocket 연결 상태 체크 함수
export const checkWebSocketConnection = (): boolean => {
  // @ts-ignore - wsLink의 내부 상태 체크
  return wsLink?.client?.getState?.() === 'connected';
};
```

### 2.2 App.tsx에 Apollo Provider 추가

```typescript
// src/App.tsx
import { ApolloProvider } from '@apollo/client';
import { apolloClient } from './config/apollo.config';
import ConsensusDashboard from './components/Dashboard/ConsensusDashboard';

function App() {
  return (
    <ApolloProvider client={apolloClient}>
      <div className="App">
        <ConsensusDashboard />
      </div>
    </ApolloProvider>
  );
}

export default App;
```

---

## 3. 구현 가능한 기능들

### 3.1 기능 목록 및 우선순위

| 기능 | 우선순위 | 난이도 | 예상 시간 | 설명 |
|-----|---------|-------|----------|------|
| **실시간 블록 모니터** | 🔴 High | ⭐⭐ | 1일 | 최신 블록 정보 실시간 표시 |
| **네트워크 헬스 대시보드** | 🔴 High | ⭐⭐⭐ | 2일 | 참여율, Round 변경 등 헬스 지표 |
| **Validator 참여율 차트** | 🟡 Medium | ⭐⭐⭐ | 1-2일 | 시간별 validator 참여율 그래프 |
| **합의 에러 알림** | 🔴 High | ⭐⭐ | 1일 | Critical/High 에러 실시간 알림 |
| **Epoch 경계 알림** | 🟡 Medium | ⭐ | 0.5일 | Epoch 변경 시 알림 및 정보 표시 |
| **포크 감지 및 알림** | 🟢 Low | ⭐⭐ | 1일 | 체인 포크 감지 시 경고 |
| **Validator 변경 히스토리** | 🟡 Medium | ⭐⭐ | 1일 | Validator 추가/제거 이력 |
| **Round 변경 통계** | 🟢 Low | ⭐⭐⭐ | 1-2일 | Round 변경 빈도 및 패턴 분석 |
| **Proposer 활동 추적** | 🟢 Low | ⭐⭐ | 1일 | 각 validator의 proposer 활동 |
| **실시간 블록 피드** | 🟡 Medium | ⭐⭐ | 1일 | 트위터 피드 스타일 블록 목록 |

---

## 4. 상태 관리 전략

### 4.1 Zustand Store 설정 (`stores/consensusStore.ts`)

```typescript
import { create } from 'zustand';
import { devtools, persist } from 'zustand/middleware';

export interface ConsensusBlock {
  blockNumber: number;
  blockHash: string;
  timestamp: number;
  round: number;
  roundChanged: boolean;
  proposer: string;
  validatorCount: number;
  commitCount: number;
  participationRate: number;
  missedValidatorRate: number;
  isEpochBoundary: boolean;
  epochNumber?: number;
  receivedAt: Date;
}

export interface ConsensusError {
  blockNumber: number;
  errorType: string;
  severity: 'critical' | 'high' | 'medium' | 'low';
  errorMessage: string;
  participationRate: number;
  consensusImpacted: boolean;
  missedValidators?: string[];
  receivedAt: Date;
}

interface ConsensusState {
  // 현재 상태
  latestBlock: ConsensusBlock | null;
  recentBlocks: ConsensusBlock[];
  recentErrors: ConsensusError[];
  isConnected: boolean;

  // 통계
  stats: {
    totalBlocks: number;
    roundChanges: number;
    averageParticipation: number;
    errorCount: number;
  };

  // Actions
  setLatestBlock: (block: ConsensusBlock) => void;
  addError: (error: ConsensusError) => void;
  setConnectionStatus: (status: boolean) => void;
  clearHistory: () => void;
  updateStats: () => void;
}

export const useConsensusStore = create<ConsensusState>()(
  devtools(
    persist(
      (set, get) => ({
        // Initial state
        latestBlock: null,
        recentBlocks: [],
        recentErrors: [],
        isConnected: false,
        stats: {
          totalBlocks: 0,
          roundChanges: 0,
          averageParticipation: 0,
          errorCount: 0,
        },

        // Actions
        setLatestBlock: (block) => {
          set((state) => {
            const newBlocks = [block, ...state.recentBlocks].slice(0, 50); // 최근 50개만 유지
            return {
              latestBlock: block,
              recentBlocks: newBlocks,
            };
          });
          get().updateStats();
        },

        addError: (error) => {
          set((state) => ({
            recentErrors: [error, ...state.recentErrors].slice(0, 100),
          }));
          get().updateStats();
        },

        setConnectionStatus: (status) => {
          set({ isConnected: status });
        },

        clearHistory: () => {
          set({
            recentBlocks: [],
            recentErrors: [],
            stats: {
              totalBlocks: 0,
              roundChanges: 0,
              averageParticipation: 0,
              errorCount: 0,
            },
          });
        },

        updateStats: () => {
          const { recentBlocks, recentErrors } = get();

          const totalBlocks = recentBlocks.length;
          const roundChanges = recentBlocks.filter(b => b.roundChanged).length;
          const averageParticipation = totalBlocks > 0
            ? recentBlocks.reduce((sum, b) => sum + b.participationRate, 0) / totalBlocks
            : 0;
          const errorCount = recentErrors.length;

          set({
            stats: {
              totalBlocks,
              roundChanges,
              averageParticipation,
              errorCount,
            },
          });
        },
      }),
      {
        name: 'consensus-store',
        partialize: (state) => ({
          // 연결 상태는 persist하지 않음
          recentBlocks: state.recentBlocks.slice(0, 10), // 최근 10개만 저장
          stats: state.stats,
        }),
      }
    )
  )
);
```

---

## 5. 컴포넌트 구현 가이드

### 5.1 기능 #1: 실시간 블록 모니터

**파일**: `components/ConsensusBlock/BlockCard.tsx`

```typescript
import React from 'react';
import { gql, useSubscription } from '@apollo/client';
import { useConsensusStore } from '../../stores/consensusStore';
import { formatDistance } from 'date-fns';
import { ko } from 'date-fns/locale';

const CONSENSUS_BLOCK_SUBSCRIPTION = gql`
  subscription OnConsensusBlock {
    consensusBlock {
      blockNumber
      blockHash
      timestamp
      round
      prevRound
      roundChanged
      proposer
      validatorCount
      prepareCount
      commitCount
      participationRate
      missedValidatorRate
      isEpochBoundary
      epochNumber
      epochValidators
    }
  }
`;

export const BlockCard: React.FC = () => {
  const { setLatestBlock, latestBlock } = useConsensusStore();

  const { data, loading, error } = useSubscription(CONSENSUS_BLOCK_SUBSCRIPTION, {
    onData: ({ data }) => {
      if (data.data?.consensusBlock) {
        setLatestBlock({
          ...data.data.consensusBlock,
          receivedAt: new Date(),
        });
      }
    },
  });

  if (loading && !latestBlock) {
    return (
      <div className="animate-pulse bg-gray-800 rounded-lg p-6">
        <div className="h-4 bg-gray-700 rounded w-1/4 mb-4"></div>
        <div className="h-8 bg-gray-700 rounded w-1/2 mb-2"></div>
        <div className="h-4 bg-gray-700 rounded w-3/4"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="bg-red-900/20 border border-red-500 rounded-lg p-6">
        <p className="text-red-400">연결 오류: {error.message}</p>
      </div>
    );
  }

  if (!latestBlock) return null;

  const participationColor =
    latestBlock.participationRate >= 90 ? 'text-green-400' :
    latestBlock.participationRate >= 75 ? 'text-yellow-400' :
    latestBlock.participationRate >= 66.7 ? 'text-orange-400' :
    'text-red-400';

  return (
    <div className="bg-gray-800 rounded-lg p-6 shadow-xl border-2 border-gray-700">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-2xl font-bold text-white">
          Block #{latestBlock.blockNumber.toLocaleString()}
        </h3>
        {latestBlock.isEpochBoundary && (
          <span className="bg-purple-600 text-white px-3 py-1 rounded-full text-sm font-semibold">
            🎯 Epoch #{latestBlock.epochNumber}
          </span>
        )}
      </div>

      {/* Round info */}
      {latestBlock.roundChanged && (
        <div className="bg-yellow-900/30 border border-yellow-600 rounded-lg p-3 mb-4">
          <span className="text-yellow-400 font-semibold">
            ⚠️ Round Changed: {latestBlock.prevRound} → {latestBlock.round}
          </span>
        </div>
      )}

      {/* Stats Grid */}
      <div className="grid grid-cols-2 gap-4 mb-4">
        <div>
          <p className="text-gray-400 text-sm">Proposer</p>
          <p className="text-white font-mono text-sm truncate">
            {latestBlock.proposer.slice(0, 10)}...{latestBlock.proposer.slice(-8)}
          </p>
        </div>
        <div>
          <p className="text-gray-400 text-sm">Participation</p>
          <p className={`font-bold text-xl ${participationColor}`}>
            {latestBlock.participationRate.toFixed(1)}%
          </p>
        </div>
        <div>
          <p className="text-gray-400 text-sm">Validators</p>
          <p className="text-white font-semibold">
            {latestBlock.commitCount} / {latestBlock.validatorCount}
          </p>
        </div>
        <div>
          <p className="text-gray-400 text-sm">Round</p>
          <p className="text-white font-semibold">
            {latestBlock.round === 0 ? '✅ First Try' : `🔄 Round ${latestBlock.round}`}
          </p>
        </div>
      </div>

      {/* Progress Bar */}
      <div className="mb-4">
        <div className="flex justify-between text-xs text-gray-400 mb-1">
          <span>Validator Participation</span>
          <span>{latestBlock.commitCount}/{latestBlock.validatorCount}</span>
        </div>
        <div className="w-full bg-gray-700 rounded-full h-2">
          <div
            className={`h-2 rounded-full transition-all duration-300 ${
              latestBlock.participationRate >= 90 ? 'bg-green-500' :
              latestBlock.participationRate >= 75 ? 'bg-yellow-500' :
              latestBlock.participationRate >= 66.7 ? 'bg-orange-500' :
              'bg-red-500'
            }`}
            style={{ width: `${latestBlock.participationRate}%` }}
          />
        </div>
      </div>

      {/* Footer */}
      <div className="text-xs text-gray-400 pt-4 border-t border-gray-700">
        Received {formatDistance(latestBlock.receivedAt, new Date(), {
          addSuffix: true,
          locale: ko
        })}
      </div>
    </div>
  );
};
```

### 5.2 기능 #2: 네트워크 헬스 대시보드

**파일**: `components/Dashboard/NetworkHealth.tsx`

```typescript
import React, { useState, useEffect } from 'react';
import { useConsensusStore } from '../../stores/consensusStore';
import { Line } from 'react-chartjs-2';
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
} from 'chart.js';

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
);

export const NetworkHealth: React.FC = () => {
  const { recentBlocks, stats } = useConsensusStore();
  const [participationHistory, setParticipationHistory] = useState<number[]>([]);
  const [blockNumbers, setBlockNumbers] = useState<string[]>([]);

  useEffect(() => {
    // 최근 20개 블록의 참여율 이력
    const history = recentBlocks
      .slice(0, 20)
      .reverse()
      .map(b => b.participationRate);

    const numbers = recentBlocks
      .slice(0, 20)
      .reverse()
      .map(b => `#${b.blockNumber}`);

    setParticipationHistory(history);
    setBlockNumbers(numbers);
  }, [recentBlocks]);

  const chartData = {
    labels: blockNumbers,
    datasets: [
      {
        label: 'Participation Rate (%)',
        data: participationHistory,
        borderColor: 'rgb(34, 197, 94)',
        backgroundColor: 'rgba(34, 197, 94, 0.1)',
        fill: true,
        tension: 0.4,
      },
    ],
  };

  const chartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: {
        display: true,
        position: 'top' as const,
        labels: {
          color: 'rgb(209, 213, 219)',
        },
      },
      tooltip: {
        backgroundColor: 'rgba(31, 41, 55, 0.9)',
        titleColor: 'rgb(209, 213, 219)',
        bodyColor: 'rgb(209, 213, 219)',
      },
    },
    scales: {
      y: {
        min: 0,
        max: 100,
        ticks: {
          color: 'rgb(156, 163, 175)',
          callback: (value: any) => `${value}%`,
        },
        grid: {
          color: 'rgba(75, 85, 99, 0.3)',
        },
      },
      x: {
        ticks: {
          color: 'rgb(156, 163, 175)',
          maxRotation: 45,
          minRotation: 45,
        },
        grid: {
          color: 'rgba(75, 85, 99, 0.3)',
        },
      },
    },
  };

  const healthScore = Math.round(
    (stats.averageParticipation * 0.7) +
    ((1 - (stats.roundChanges / stats.totalBlocks)) * 30)
  );

  const healthColor =
    healthScore >= 90 ? 'text-green-400' :
    healthScore >= 75 ? 'text-yellow-400' :
    healthScore >= 60 ? 'text-orange-400' :
    'text-red-400';

  const healthStatus =
    healthScore >= 90 ? '🟢 Excellent' :
    healthScore >= 75 ? '🟡 Good' :
    healthScore >= 60 ? '🟠 Fair' :
    '🔴 Poor';

  return (
    <div className="bg-gray-800 rounded-lg p-6 shadow-xl">
      <h2 className="text-2xl font-bold text-white mb-6">Network Health</h2>

      {/* Health Score */}
      <div className="bg-gray-900 rounded-lg p-6 mb-6">
        <div className="flex items-center justify-between mb-2">
          <span className="text-gray-400">Overall Health Score</span>
          <span className="text-sm text-gray-500">{healthStatus}</span>
        </div>
        <div className="flex items-baseline">
          <span className={`text-5xl font-bold ${healthColor}`}>{healthScore}</span>
          <span className="text-gray-400 ml-2">/100</span>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        <div className="bg-gray-900 rounded-lg p-4">
          <p className="text-gray-400 text-sm mb-1">Total Blocks</p>
          <p className="text-white text-2xl font-bold">
            {stats.totalBlocks.toLocaleString()}
          </p>
        </div>
        <div className="bg-gray-900 rounded-lg p-4">
          <p className="text-gray-400 text-sm mb-1">Avg Participation</p>
          <p className="text-green-400 text-2xl font-bold">
            {stats.averageParticipation.toFixed(1)}%
          </p>
        </div>
        <div className="bg-gray-900 rounded-lg p-4">
          <p className="text-gray-400 text-sm mb-1">Round Changes</p>
          <p className="text-yellow-400 text-2xl font-bold">
            {stats.roundChanges}
          </p>
        </div>
        <div className="bg-gray-900 rounded-lg p-4">
          <p className="text-gray-400 text-sm mb-1">Errors</p>
          <p className="text-red-400 text-2xl font-bold">
            {stats.errorCount}
          </p>
        </div>
      </div>

      {/* Participation Chart */}
      <div className="bg-gray-900 rounded-lg p-4">
        <h3 className="text-white font-semibold mb-4">Participation Rate History</h3>
        <div className="h-64">
          <Line data={chartData} options={chartOptions} />
        </div>
      </div>
    </div>
  );
};
```

### 5.3 기능 #3: 합의 에러 알림 시스템

**파일**: `components/ConsensusError/ErrorAlert.tsx`

```typescript
import React, { useEffect } from 'react';
import { gql, useSubscription } from '@apollo/client';
import { toast, ToastContainer } from 'react-toastify';
import 'react-toastify/dist/ReactToastify.css';
import { useConsensusStore } from '../../stores/consensusStore';

const CONSENSUS_ERROR_SUBSCRIPTION = gql`
  subscription OnConsensusError {
    consensusError {
      blockNumber
      errorType
      severity
      errorMessage
      round
      participationRate
      consensusImpacted
      missedValidators
      errorDetails
    }
  }
`;

export const ErrorAlert: React.FC = () => {
  const { addError } = useConsensusStore();

  const { data } = useSubscription(CONSENSUS_ERROR_SUBSCRIPTION, {
    onData: ({ data }) => {
      if (data.data?.consensusError) {
        const error = data.data.consensusError;

        // Store에 저장
        addError({
          ...error,
          receivedAt: new Date(),
        });

        // 심각도에 따라 다른 알림
        const message = `Block #${error.blockNumber}: ${error.errorMessage}`;

        switch (error.severity) {
          case 'critical':
            toast.error(message, {
              autoClose: false, // 자동으로 닫히지 않음
              icon: '🚨',
            });
            // 브라우저 알림도 표시
            if ('Notification' in window && Notification.permission === 'granted') {
              new Notification('Critical Consensus Error', {
                body: message,
                icon: '/logo.png',
                tag: `consensus-error-${error.blockNumber}`,
              });
            }
            break;

          case 'high':
            toast.warning(message, {
              autoClose: 10000,
              icon: '⚠️',
            });
            break;

          case 'medium':
            toast.info(message, {
              autoClose: 5000,
              icon: 'ℹ️',
            });
            break;

          case 'low':
            // Low severity는 UI에만 표시하고 toast는 생략
            console.log('Low severity error:', error);
            break;
        }
      }
    },
  });

  // 브라우저 알림 권한 요청
  useEffect(() => {
    if ('Notification' in window && Notification.permission === 'default') {
      Notification.requestPermission();
    }
  }, []);

  return (
    <ToastContainer
      position="top-right"
      theme="dark"
      closeOnClick
      pauseOnHover
      draggable
    />
  );
};
```

**파일**: `components/ConsensusError/ErrorHistory.tsx`

```typescript
import React from 'react';
import { useConsensusStore } from '../../stores/consensusStore';
import { formatDistanceToNow } from 'date-fns';
import { ko } from 'date-fns/locale';

export const ErrorHistory: React.FC = () => {
  const { recentErrors } = useConsensusStore();

  const getSeverityStyles = (severity: string) => {
    switch (severity) {
      case 'critical':
        return 'bg-red-900/30 border-red-500 text-red-400';
      case 'high':
        return 'bg-orange-900/30 border-orange-500 text-orange-400';
      case 'medium':
        return 'bg-yellow-900/30 border-yellow-500 text-yellow-400';
      case 'low':
        return 'bg-blue-900/30 border-blue-500 text-blue-400';
      default:
        return 'bg-gray-900/30 border-gray-500 text-gray-400';
    }
  };

  const getSeverityIcon = (severity: string) => {
    switch (severity) {
      case 'critical': return '🚨';
      case 'high': return '⚠️';
      case 'medium': return '⚡';
      case 'low': return 'ℹ️';
      default: return '•';
    }
  };

  if (recentErrors.length === 0) {
    return (
      <div className="bg-gray-800 rounded-lg p-6">
        <h3 className="text-white font-semibold mb-4">Recent Errors</h3>
        <div className="text-center py-8">
          <p className="text-green-400 text-lg">✅ No errors detected</p>
          <p className="text-gray-400 text-sm mt-2">Network is running smoothly</p>
        </div>
      </div>
    );
  }

  return (
    <div className="bg-gray-800 rounded-lg p-6">
      <h3 className="text-white font-semibold mb-4">
        Recent Errors ({recentErrors.length})
      </h3>

      <div className="space-y-3 max-h-96 overflow-y-auto">
        {recentErrors.map((error, index) => (
          <div
            key={`${error.blockNumber}-${index}`}
            className={`border rounded-lg p-4 ${getSeverityStyles(error.severity)}`}
          >
            <div className="flex items-start justify-between mb-2">
              <div className="flex items-center gap-2">
                <span className="text-xl">{getSeverityIcon(error.severity)}</span>
                <div>
                  <p className="font-semibold">
                    Block #{error.blockNumber.toLocaleString()}
                  </p>
                  <p className="text-xs opacity-75">
                    {formatDistanceToNow(error.receivedAt, {
                      addSuffix: true,
                      locale: ko
                    })}
                  </p>
                </div>
              </div>
              <span className={`text-xs font-semibold uppercase px-2 py-1 rounded ${getSeverityStyles(error.severity)}`}>
                {error.severity}
              </span>
            </div>

            <p className="text-sm mb-2">{error.errorMessage}</p>

            <div className="flex gap-4 text-xs">
              <span>Type: {error.errorType.replace(/_/g, ' ')}</span>
              <span>Participation: {error.participationRate.toFixed(1)}%</span>
              {error.consensusImpacted && (
                <span className="font-bold">⚠️ Consensus Impacted</span>
              )}
            </div>

            {error.missedValidators && error.missedValidators.length > 0 && (
              <div className="mt-2 pt-2 border-t border-current border-opacity-20">
                <p className="text-xs">
                  Missed Validators: {error.missedValidators.length}
                </p>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
};
```

### 5.4 Custom Hooks

**파일**: `hooks/useConsensusBlock.ts`

```typescript
import { gql, useSubscription } from '@apollo/client';
import { useConsensusStore } from '../stores/consensusStore';

const CONSENSUS_BLOCK_SUBSCRIPTION = gql`
  subscription OnConsensusBlock {
    consensusBlock {
      blockNumber
      blockHash
      timestamp
      round
      prevRound
      roundChanged
      proposer
      validatorCount
      prepareCount
      commitCount
      participationRate
      missedValidatorRate
      isEpochBoundary
      epochNumber
      epochValidators
    }
  }
`;

export const useConsensusBlock = () => {
  const { setLatestBlock, latestBlock } = useConsensusStore();

  const subscription = useSubscription(CONSENSUS_BLOCK_SUBSCRIPTION, {
    onData: ({ data }) => {
      if (data.data?.consensusBlock) {
        setLatestBlock({
          ...data.data.consensusBlock,
          receivedAt: new Date(),
        });
      }
    },
    onError: (error) => {
      console.error('Consensus block subscription error:', error);
    },
  });

  return {
    latestBlock,
    loading: subscription.loading,
    error: subscription.error,
  };
};
```

---

## 6. 에러 처리 및 복구

### 6.1 재연결 로직

```typescript
// utils/reconnection.ts
export class ReconnectionManager {
  private retryCount = 0;
  private maxRetries = 5;
  private baseDelay = 1000;

  async attemptReconnection(
    reconnectFn: () => Promise<void>
  ): Promise<boolean> {
    while (this.retryCount < this.maxRetries) {
      try {
        await reconnectFn();
        this.retryCount = 0; // 성공 시 리셋
        return true;
      } catch (error) {
        this.retryCount++;
        const delay = Math.min(
          this.baseDelay * Math.pow(2, this.retryCount),
          30000
        );

        console.log(
          `Reconnection attempt ${this.retryCount}/${this.maxRetries} failed. ` +
          `Retrying in ${delay}ms...`
        );

        await new Promise(resolve => setTimeout(resolve, delay));
      }
    }

    return false;
  }

  reset() {
    this.retryCount = 0;
  }
}
```

### 6.2 연결 상태 모니터링

```typescript
// components/ConnectionStatus.tsx
import React, { useEffect, useState } from 'react';
import { useConsensusStore } from '../stores/consensusStore';

export const ConnectionStatus: React.FC = () => {
  const { isConnected, setConnectionStatus } = useConsensusStore();
  const [lastPing, setLastPing] = useState<Date | null>(null);

  useEffect(() => {
    // 5초마다 연결 상태 체크
    const interval = setInterval(() => {
      // WebSocket 연결 상태 체크 로직
      const connected = checkWebSocketConnection();
      setConnectionStatus(connected);

      if (connected) {
        setLastPing(new Date());
      }
    }, 5000);

    return () => clearInterval(interval);
  }, [setConnectionStatus]);

  return (
    <div className={`fixed top-4 right-4 px-4 py-2 rounded-lg shadow-lg ${
      isConnected ? 'bg-green-600' : 'bg-red-600'
    }`}>
      <div className="flex items-center gap-2 text-white text-sm">
        <div className={`w-2 h-2 rounded-full ${
          isConnected ? 'bg-white animate-pulse' : 'bg-gray-300'
        }`} />
        <span>
          {isConnected ? '연결됨' : '연결 끊김'}
        </span>
        {lastPing && (
          <span className="text-xs opacity-75">
            (마지막: {lastPing.toLocaleTimeString()})
          </span>
        )}
      </div>
    </div>
  );
};
```

---

## 7. 성능 최적화

### 7.1 메모이제이션

```typescript
import React, { useMemo } from 'react';
import { useConsensusStore } from '../stores/consensusStore';

export const OptimizedComponent: React.FC = () => {
  const { recentBlocks } = useConsensusStore();

  // 비용이 큰 계산은 memoize
  const statistics = useMemo(() => {
    return {
      avgParticipation: recentBlocks.reduce((sum, b) =>
        sum + b.participationRate, 0) / recentBlocks.length,
      roundChangeRate: recentBlocks.filter(b =>
        b.roundChanged).length / recentBlocks.length,
      // ... other calculations
    };
  }, [recentBlocks]);

  return <div>{/* Use statistics */}</div>;
};
```

### 7.2 가상 스크롤링 (긴 리스트용)

```typescript
// npm install react-window
import { FixedSizeList } from 'react-window';

export const VirtualizedBlockList: React.FC = () => {
  const { recentBlocks } = useConsensusStore();

  const Row = ({ index, style }: any) => (
    <div style={style}>
      <BlockListItem block={recentBlocks[index]} />
    </div>
  );

  return (
    <FixedSizeList
      height={600}
      itemCount={recentBlocks.length}
      itemSize={80}
      width="100%"
    >
      {Row}
    </FixedSizeList>
  );
};
```

---

## 8. 테스트 전략

### 8.1 Mock Provider 설정

```typescript
// test-utils/MockApolloProvider.tsx
import { MockedProvider, MockedResponse } from '@apollo/client/testing';
import { ReactNode } from 'react';

export const MockApolloProvider = ({
  children,
  mocks
}: {
  children: ReactNode;
  mocks?: MockedResponse[];
}) => {
  return (
    <MockedProvider mocks={mocks} addTypename={false}>
      {children}
    </MockedProvider>
  );
};
```

### 8.2 컴포넌트 테스트

```typescript
// components/ConsensusBlock/BlockCard.test.tsx
import { render, screen, waitFor } from '@testing-library/react';
import { MockApolloProvider } from '../../test-utils/MockApolloProvider';
import { BlockCard } from './BlockCard';
import { CONSENSUS_BLOCK_SUBSCRIPTION } from './BlockCard';

const mockBlock = {
  blockNumber: 12345,
  blockHash: '0x1234...',
  timestamp: 1640000000,
  round: 0,
  prevRound: 0,
  roundChanged: false,
  proposer: '0xabc...',
  validatorCount: 21,
  commitCount: 21,
  participationRate: 100,
  missedValidatorRate: 0,
  isEpochBoundary: false,
};

const mocks = [
  {
    request: {
      query: CONSENSUS_BLOCK_SUBSCRIPTION,
    },
    result: {
      data: {
        consensusBlock: mockBlock,
      },
    },
  },
];

describe('BlockCard', () => {
  it('renders block information correctly', async () => {
    render(
      <MockApolloProvider mocks={mocks}>
        <BlockCard />
      </MockApolloProvider>
    );

    await waitFor(() => {
      expect(screen.getByText(/Block #12,345/)).toBeInTheDocument();
      expect(screen.getByText(/100.0%/)).toBeInTheDocument();
    });
  });

  it('shows round change alert when round > 0', async () => {
    const roundChangedMock = {
      ...mocks[0],
      result: {
        data: {
          consensusBlock: {
            ...mockBlock,
            round: 1,
            roundChanged: true,
          },
        },
      },
    };

    render(
      <MockApolloProvider mocks={[roundChangedMock]}>
        <BlockCard />
      </MockApolloProvider>
    );

    await waitFor(() => {
      expect(screen.getByText(/Round Changed/)).toBeInTheDocument();
    });
  });
});
```

---

## 9. 배포 체크리스트

### 9.1 환경 변수 설정

```bash
# .env.development
REACT_APP_GRAPHQL_HTTP=http://localhost:8080/graphql
REACT_APP_GRAPHQL_WS=ws://localhost:8080/subscriptions

# .env.production
REACT_APP_GRAPHQL_HTTP=https://api.stable-one.io/graphql
REACT_APP_GRAPHQL_WS=wss://api.stable-one.io/subscriptions
```

### 9.2 Pre-launch 체크리스트

- [ ] WebSocket 연결 성공 확인
- [ ] 4가지 이벤트 타입 모두 수신 확인
- [ ] 에러 알림 시스템 작동 확인
- [ ] 브라우저 알림 권한 요청 작동
- [ ] 재연결 로직 테스트
- [ ] 성능 프로파일링 (React DevTools)
- [ ] 메모리 누수 체크
- [ ] 다양한 브라우저에서 테스트
- [ ] 모바일 반응형 확인
- [ ] Lighthouse 점수 확인

---

## 10. 문제 해결 가이드

### 10.1 WebSocket 연결 실패

```typescript
// 연결 실패 시 디버깅
if (!isConnected) {
  console.log('WebSocket connection failed. Checking:');
  console.log('1. Backend server running?');
  console.log('2. CORS configured correctly?');
  console.log('3. Network firewall blocking WebSocket?');
  console.log('4. Check browser console for errors');

  // Backend health check
  fetch(HTTP_URL.replace('/graphql', '/health'))
    .then(r => r.json())
    .then(data => console.log('Backend health:', data))
    .catch(e => console.error('Backend unreachable:', e));
}
```

### 10.2 이벤트가 수신되지 않음

1. **EventBus 활성화 확인**: Backend에서 EventBus가 실행 중인지 확인
2. **ConsensusFetcher 설정**: EventBus가 ConsensusFetcher에 연결되었는지 확인
3. **GraphQL 쿼리 확인**: 구독 쿼리 문법 검증
4. **Browser DevTools**: Network 탭에서 WebSocket 프레임 확인

---

## 📚 추가 리소스

- **Backend API 문서**: `docs/ToFrontend-New.md`
- **Phase B 완료 보고서**: `docs/PHASE_B_CONSENSUS_EVENTS_COMPLETE.md`
- **Apollo Client 공식 문서**: https://www.apollographql.com/docs/react/
- **React Query (대안)**: https://tanstack.com/query/latest

---

**작성 완료!** 🎉

이 가이드를 따라 구현하면 완전한 consensus 모니터링 시스템을 구축할 수 있습니다.
