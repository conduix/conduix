# Web UI

파이프라인 관리 운영툴 프론트엔드

## 개요

`web-ui`는 Conduix 시스템의 웹 기반 관리 인터페이스입니다. React와 TypeScript로 구축되었으며, 파이프라인 설정, 모니터링, 스케줄 관리 등의 기능을 제공합니다.

## 기술 스택

| 분류 | 기술 |
|-----|------|
| 프레임워크 | React 18 |
| 언어 | TypeScript |
| 빌드 도구 | Vite |
| 상태 관리 | Zustand |
| UI 컴포넌트 | Ant Design |
| HTTP 클라이언트 | Axios |
| 라우팅 | React Router v6 |
| 스타일링 | CSS + Ant Design |

## 아키텍처

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Web UI 아키텍처                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                           Pages                                      │    │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐   │    │
│  │  │  Login  │  │Dashboard│  │Pipelines│  │ Agents  │  │Schedules│   │    │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘  └─────────┘   │    │
│  └──────────────────────────────┬──────────────────────────────────────┘    │
│                                 │                                            │
│  ┌──────────────────────────────┼──────────────────────────────────────┐    │
│  │                         Components                                   │    │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐        │    │
│  │  │MainLayout │  │PipelineCard│  │ YAMLEditor │  │ StatCard  │        │    │
│  │  └───────────┘  └───────────┘  └───────────┘  └───────────┘        │    │
│  └──────────────────────────────┬──────────────────────────────────────┘    │
│                                 │                                            │
│  ┌──────────────────────────────┼──────────────────────────────────────┐    │
│  │                          Stores                                      │    │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐                        │    │
│  │  │ AuthStore │  │PipelineStore│ │ AgentStore │                        │    │
│  │  └───────────┘  └───────────┘  └───────────┘                        │    │
│  └──────────────────────────────┬──────────────────────────────────────┘    │
│                                 │                                            │
│  ┌──────────────────────────────┴──────────────────────────────────────┐    │
│  │                         Services                                     │    │
│  │  ┌─────────────────────────────────────────────────────────────┐    │    │
│  │  │                    API Client (Axios)                        │    │    │
│  │  │             → Control Plane REST API                         │    │    │
│  │  └─────────────────────────────────────────────────────────────┘    │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 디렉토리 구조

```
web-ui/
├── public/                      # 정적 파일
├── src/
│   ├── components/              # 재사용 컴포넌트
│   │   └── Layout/
│   │       └── MainLayout.tsx   # 메인 레이아웃 (사이드바, 헤더)
│   ├── pages/                   # 페이지 컴포넌트
│   │   ├── Login.tsx            # 로그인 페이지
│   │   ├── Dashboard.tsx        # 대시보드
│   │   ├── Pipelines.tsx        # 파이프라인 목록
│   │   ├── PipelineDetail.tsx   # 파이프라인 상세
│   │   ├── Agents.tsx           # 에이전트 목록
│   │   ├── Schedules.tsx        # 스케줄 관리
│   │   └── History.tsx          # 실행 히스토리
│   ├── services/
│   │   └── api.ts               # API 클라이언트
│   ├── store/
│   │   └── auth.ts              # 인증 상태 관리 (Zustand)
│   ├── types/                   # TypeScript 타입 정의
│   ├── App.tsx                  # 앱 루트 컴포넌트
│   ├── main.tsx                 # 진입점
│   └── index.css                # 전역 스타일
├── index.html
├── vite.config.ts               # Vite 설정
├── tsconfig.json                # TypeScript 설정
└── package.json
```

## 주요 화면

### 1. 로그인

SSO 기반 로그인 화면입니다.

```
┌─────────────────────────────────────┐
│         Conduix             │
│                                     │
│  ┌─────────────────────────────┐   │
│  │     Sign in with Google     │   │
│  └─────────────────────────────┘   │
│                                     │
│  ┌─────────────────────────────┐   │
│  │    Sign in with Keycloak    │   │
│  └─────────────────────────────┘   │
│                                     │
└─────────────────────────────────────┘
```

### 2. 대시보드

시스템 전체 현황을 한눈에 파악합니다.

```
┌─────────────────────────────────────────────────────────────────┐
│  Dashboard                                                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐            │
│  │Pipelines│  │ Agents  │  │ Running │  │ Errors  │            │
│  │   12    │  │    5    │  │    8    │  │    2    │            │
│  └─────────┘  └─────────┘  └─────────┘  └─────────┘            │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Recent Pipelines                                        │   │
│  │  • log-processor      Running   ↑ 1.2M events/min       │   │
│  │  • metric-collector   Running   ↑ 500K events/min       │   │
│  │  • backup-job         Stopped   Last run: 2h ago        │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 3. 파이프라인 목록

모든 파이프라인을 관리합니다.

```
┌─────────────────────────────────────────────────────────────────┐
│  Pipelines                              [+ Create Pipeline]     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ Name             Status    Events/min   Agent    Actions │   │
│  ├─────────────────────────────────────────────────────────┤   │
│  │ log-processor    Running   1.2M         agent-1   ▶ ⏸ ⏹  │   │
│  │ metric-collector Running   500K         agent-2   ▶ ⏸ ⏹  │   │
│  │ backup-job       Stopped   -            -         ▶ ⏸ ⏹  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 4. 파이프라인 상세

파이프라인 설정 및 모니터링 화면입니다.

```
┌─────────────────────────────────────────────────────────────────┐
│  log-processor                          [Edit] [Start] [Delete] │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  [Overview] [Configuration] [Metrics] [History]                 │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Configuration (YAML)                                    │   │
│  │  ┌─────────────────────────────────────────────────┐    │   │
│  │  │ version: "1.0"                                   │    │   │
│  │  │ name: "log-processor"                            │    │   │
│  │  │                                                  │    │   │
│  │  │ sources:                                         │    │   │
│  │  │   kafka:                                         │    │   │
│  │  │     type: kafka                                  │    │   │
│  │  │     brokers: ["kafka:9092"]                      │    │   │
│  │  │     ...                                          │    │   │
│  │  └─────────────────────────────────────────────────┘    │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 5. 에이전트 관리

분산 에이전트 상태를 모니터링합니다.

```
┌─────────────────────────────────────────────────────────────────┐
│  Agents                                                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ Hostname      Status   Pipelines  CPU   Memory  Last HB │   │
│  ├─────────────────────────────────────────────────────────┤   │
│  │ agent-1       Online   3          45%   2.1GB   10s ago │   │
│  │ agent-2       Online   2          30%   1.8GB   8s ago  │   │
│  │ agent-3       Offline  0          -     -       5m ago  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## 상태 관리 (Zustand)

### Auth Store

```typescript
// src/store/auth.ts
interface AuthState {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  login: (provider: string) => void;
  logout: () => void;
  setUser: (user: User) => void;
  setToken: (token: string) => void;
}

const useAuthStore = create<AuthState>((set) => ({
  user: null,
  token: localStorage.getItem('token'),
  isAuthenticated: !!localStorage.getItem('token'),

  login: (provider) => {
    window.location.href = `/api/v1/auth/login/${provider}`;
  },

  logout: () => {
    localStorage.removeItem('token');
    set({ user: null, token: null, isAuthenticated: false });
  },

  setUser: (user) => set({ user }),
  setToken: (token) => {
    localStorage.setItem('token', token);
    set({ token, isAuthenticated: true });
  },
}));
```

## API 클라이언트

### 설정

```typescript
// src/services/api.ts
import axios from 'axios';

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
});

// 인증 토큰 자동 추가
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// 에러 처리
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('token');
      window.location.href = '/login';
    }
    return Promise.reject(error);
  }
);
```

### API 함수

```typescript
// 파이프라인 API
export const pipelineApi = {
  list: () => api.get('/pipelines'),
  get: (id: string) => api.get(`/pipelines/${id}`),
  create: (data: CreatePipelineRequest) => api.post('/pipelines', data),
  update: (id: string, data: UpdatePipelineRequest) => api.put(`/pipelines/${id}`, data),
  delete: (id: string) => api.delete(`/pipelines/${id}`),
  start: (id: string, agentId?: string) => api.post(`/pipelines/${id}/start`, { agent_id: agentId }),
  stop: (id: string) => api.post(`/pipelines/${id}/stop`),
  pause: (id: string) => api.post(`/pipelines/${id}/pause`),
  resume: (id: string) => api.post(`/pipelines/${id}/resume`),
  getStatus: (id: string) => api.get(`/pipelines/${id}/status`),
  getHistory: (id: string) => api.get(`/pipelines/${id}/history`),
  getMetrics: (id: string) => api.get(`/pipelines/${id}/metrics`),
};

// 에이전트 API
export const agentApi = {
  list: () => api.get('/agents'),
  get: (id: string) => api.get(`/agents/${id}`),
  getStatus: (id: string) => api.get(`/agents/${id}/status`),
};

// 스케줄 API
export const scheduleApi = {
  list: () => api.get('/schedules'),
  create: (data: CreateScheduleRequest) => api.post('/schedules', data),
  update: (id: string, data: UpdateScheduleRequest) => api.put(`/schedules/${id}`, data),
  delete: (id: string) => api.delete(`/schedules/${id}`),
  enable: (id: string) => api.post(`/schedules/${id}/enable`),
  disable: (id: string) => api.post(`/schedules/${id}/disable`),
};
```

## 개발 환경 설정

### 설치

```bash
cd web-ui
npm install
```

### 개발 서버 실행

```bash
npm run dev

# 기본: http://localhost:5173
```

### 환경 변수

```bash
# .env.development
VITE_API_URL=http://localhost:8080
```

### Vite 설정

```typescript
// vite.config.ts
export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/auth': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});
```

## 빌드

### 프로덕션 빌드

```bash
npm run build

# 출력: dist/
```

### 빌드 미리보기

```bash
npm run preview
```

### Docker 빌드

```bash
docker build -t conduix/web-ui:latest -f ../deploy/docker/Dockerfile.web-ui .
```

## 테스트

```bash
# 단위 테스트
npm run test

# E2E 테스트
npm run test:e2e

# 린트
npm run lint

# 타입 체크
npm run type-check
```

## 의존성

```json
{
  "dependencies": {
    "react": "^18.2.0",
    "react-dom": "^18.2.0",
    "react-router-dom": "^6.21.0",
    "antd": "^5.12.0",
    "axios": "^1.6.0",
    "zustand": "^4.4.0",
    "@ant-design/icons": "^5.2.0"
  },
  "devDependencies": {
    "@types/react": "^18.2.0",
    "@vitejs/plugin-react": "^4.2.0",
    "typescript": "^5.3.0",
    "vite": "^5.0.0"
  }
}
```

## 커스터마이징

### 테마 설정

```typescript
// src/App.tsx
import { ConfigProvider } from 'antd';

<ConfigProvider
  theme={{
    token: {
      colorPrimary: '#1890ff',
      borderRadius: 6,
    },
  }}
>
  <App />
</ConfigProvider>
```

### 새 페이지 추가

```typescript
// 1. 페이지 컴포넌트 생성
// src/pages/NewPage.tsx
export default function NewPage() {
  return <div>New Page</div>;
}

// 2. 라우트 추가
// src/App.tsx
<Route path="/new-page" element={<NewPage />} />

// 3. 사이드바에 메뉴 추가
// src/components/Layout/MainLayout.tsx
const menuItems = [
  // ...
  { key: '/new-page', icon: <Icon />, label: 'New Page' },
];
```

## 관련 문서

- [Control Plane API](../control-plane/README.md)
- [배포 가이드](../deploy/README.md)
