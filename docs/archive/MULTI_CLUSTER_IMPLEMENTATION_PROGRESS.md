# 멀티 Kubernetes 클러스터 지원 구현 진행 상황

## 작업 일시: 2026-02-23

## 개요
운영툴(Control Plane)에서 여러 Kubernetes 클러스터를 관리하고, Workflow별로 실행할 클러스터를 지정할 수 있는 기능 구현

## 구현 단계 및 진행 상황

### Phase 1: 데이터 모델 확장 - [x] 완료
- [x] Cluster 모델 추가 (`control-plane/pkg/models/models.go`)
- [x] Agent 모델에 ClusterID 필드 추가
- [x] Workflow 모델에 ClusterID 필드 추가
- [x] WorkflowExecution 모델에 ClusterID, AgentID 필드 추가

### Phase 2: Shared Types 수정 - [x] 완료
- [x] `AgentHeartbeat`에 `ClusterID` 필드 추가
- [x] `WorkflowExecutionCommand`에 `TargetClusterID` 필드 추가
- [x] `shared/types/api.go`에 `ErrCodeDuplicateResource`, `ErrCodeResourceInUse` 추가

### Phase 3: Control Plane API - [x] 완료
- [x] Cluster Handler 생성 (`control-plane/internal/api/handlers/cluster.go`)
- [x] Agent Handler 수정 (ClusterID 필터/등록/ClusterName 조회 추가)
- [x] Workflow Handler 수정 (ClusterID 지원)
- [x] Redis Service 수정 (클러스터별 Publish)
- [x] Routes 수정 (Cluster 라우트 추가)

### Phase 4: Pipeline Agent 수정 - [x] 완료
- [x] Config에 CLUSTER_ID 환경변수 추가
- [x] 등록 로직 수정 (ClusterID 전송)
- [x] 채널 구독 수정 (클러스터별 채널)
- [x] 하트비트 수정 (ClusterID 포함)

### Phase 5: Web UI 구현 - [x] 완료
- [x] API Service 확장 (`web-ui/src/services/api.ts`)
- [x] Clusters 페이지 생성 (`web-ui/src/pages/Clusters.tsx`)
- [x] Agents 페이지 수정 (클러스터 필터)
- [x] App.tsx 라우팅 추가
- [x] MainLayout 네비게이션 추가
- [x] i18n 번역 추가

### Phase 6: 테스트 및 검증 - [x] 완료
- [x] Go 빌드 테스트 (make build-go) - 성공
- [x] Web UI 빌드 테스트 (make build-web) - 성공
- [ ] Cluster CRUD 테스트 (런타임 테스트 필요)
- [ ] Agent 등록 테스트 (런타임 테스트 필요)
- [ ] Workflow 실행 테스트 (런타임 테스트 필요)
- [ ] 하위호환성 테스트 (런타임 테스트 필요)

## 수정된 파일 목록

### Control Plane (Go)
| 파일 | 상태 | 작업 내용 |
|------|------|---------|
| `control-plane/pkg/models/models.go` | [x] | Cluster 모델 추가, Agent/Workflow 수정 |
| `control-plane/internal/api/handlers/cluster.go` | [x] | 신규 생성 |
| `control-plane/internal/api/handlers/agent.go` | [x] | ClusterID 필터/등록 추가 |
| `control-plane/internal/api/handlers/workflow.go` | [x] | ClusterID 지원 추가 |
| `control-plane/internal/services/redis_service.go` | [x] | 클러스터별 Publish 추가 |
| `control-plane/internal/api/routes.go` | [x] | Cluster 라우트 추가 |

### Shared (Go)
| 파일 | 상태 | 작업 내용 |
|------|------|---------|
| `shared/types/agent.go` | [x] | ClusterID 필드 추가 |
| `shared/types/api.go` | [x] | 에러 코드 추가 |

### Pipeline Agent (Go)
| 파일 | 상태 | 작업 내용 |
|------|------|---------|
| `pipeline-agent/internal/agent/agent.go` | [x] | ClusterID 구독/등록 |
| `pipeline-agent/cmd/agent/main.go` | [x] | CLUSTER_ID 환경변수 |

### Web UI (TypeScript/React)
| 파일 | 상태 | 작업 내용 |
|------|------|---------|
| `web-ui/src/services/api.ts` | [x] | Cluster API 추가 |
| `web-ui/src/pages/Clusters.tsx` | [x] | 신규 생성 |
| `web-ui/src/pages/Agents.tsx` | [x] | 클러스터 필터 추가 |
| `web-ui/src/App.tsx` | [x] | 라우팅 추가 |
| `web-ui/src/components/Layout/MainLayout.tsx` | [x] | 네비게이션 추가 |
| `web-ui/src/i18n/locales/ko.json` | [x] | 번역 추가 |
| `web-ui/src/i18n/locales/en.json` | [x] | 번역 추가 |

## 주요 결정 사항
- 하위 호환성: 기존 Agent/Workflow는 ClusterID 없이 동작 가능
- 채널 구조: `cluster:{id}:execute` (클러스터별), `group:execute:broadcast` (브로드캐스트)
- Agent는 시작 시 CLUSTER_ID 환경변수로 클러스터 소속 결정

## API 엔드포인트

### Cluster API
| Method | Endpoint | 설명 | 권한 |
|--------|----------|------|------|
| GET | `/api/v1/clusters` | 클러스터 목록 조회 | 모든 사용자 |
| POST | `/api/v1/clusters` | 클러스터 생성 | Admin |
| GET | `/api/v1/clusters/:id` | 클러스터 상세 조회 | 모든 사용자 |
| PUT | `/api/v1/clusters/:id` | 클러스터 수정 | Admin |
| DELETE | `/api/v1/clusters/:id` | 클러스터 삭제 | Admin |
| GET | `/api/v1/clusters/:id/agents` | 클러스터별 에이전트 조회 | 모든 사용자 |

## 런타임 테스트 체크리스트

### Cluster CRUD 테스트
1. GUI에서 클러스터 생성
2. 클러스터 수정
3. 클러스터 삭제 (에이전트가 없는 경우만 가능)

### Agent 등록 테스트
1. `CLUSTER_ID` 환경변수 설정 후 Agent 시작
2. Control Plane에서 Agent의 ClusterID 확인
3. Agents 페이지에서 클러스터 필터 동작 확인

### Workflow 실행 테스트
1. ClusterID 지정된 Workflow 생성
2. 해당 클러스터의 Agent에서만 실행되는지 확인
3. 다른 클러스터 Agent는 실행하지 않는지 확인

### 하위호환성 테스트
1. ClusterID 없는 기존 Agent가 정상 동작하는지 확인
2. ClusterID 없는 기존 Workflow가 정상 동작하는지 확인
