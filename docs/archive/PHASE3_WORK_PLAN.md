# Phase 3: Pipeline Runner 개발 작업 플랜

## 목표
Job/Deployment로 실행되는 독립 파이프라인 실행기 개발

## 작업 항목

| # | 작업 | 파일 | 상태 |
|---|------|------|------|
| 3.1 | Runner 진입점 | `pipeline-runner/cmd/runner/main.go` | ⬜ |
| 3.2 | Runner 코어 | `pipeline-runner/internal/runner/runner.go` | ⬜ |
| 3.3 | 설정 로더 | `pipeline-runner/internal/config/loader.go` | ⬜ |
| 3.4 | Checkpoint | `pipeline-runner/internal/checkpoint/manager.go` | ⬜ |
| 3.5 | Health Server | `pipeline-runner/internal/health/server.go` | ⬜ |
| 3.6 | go.mod | `pipeline-runner/go.mod` | ⬜ |
| 3.7 | Dockerfile | `pipeline-runner/Dockerfile` | ⬜ |
| 3.8 | Makefile | `pipeline-runner/Makefile` | ⬜ |
| 3.9 | 테스트 | `*_test.go` | ⬜ |

## 설계

### Pipeline Runner 실행 모드
- **batch**: 1회 실행 후 종료 (K8s Job)
- **streaming**: 지속 실행 (K8s Deployment)

### 환경변수 인터페이스
```
EXECUTION_MODE      # batch | streaming
WORKFLOW_ID         # 워크플로우 ID
EXECUTION_ID        # 실행 ID (batch only)
PIPELINES_CONFIG    # JSON - 파이프라인 설정
CONTROL_PLANE_URL   # Control Plane URL
CALLBACK_URL        # 결과 전송 URL (batch only)
TIMEOUT_SECONDS     # 타임아웃 (batch only, default: 3600)
CHECKPOINT_ENDPOINT # 체크포인트 저장 URL
HEALTH_PORT         # 헬스체크 포트 (default: 8082)
```

### 핵심 차이점 vs 기존 batch_runner.go
- 독립 Go 모듈 (pipeline-core 의존)
- batch + streaming 모드 통합
- 헬스체크 서버 (Deployment의 liveness/readiness)
- Checkpoint 관리자 (streaming 모드)
- Graceful shutdown
