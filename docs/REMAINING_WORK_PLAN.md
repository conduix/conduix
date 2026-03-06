# Phase 3~6 잔여 작업 플랜

## Phase 3: Pipeline Runner 개발
| # | 작업 | 파일 | 상태 |
|---|------|------|------|
| 3.1 | 설정 로더 | `pipeline-runner/internal/config/loader.go` | ⬜ |
| 3.2 | Health Server | `pipeline-runner/internal/health/server.go` | ⬜ |
| 3.3 | Checkpoint Manager | `pipeline-runner/internal/checkpoint/manager.go` | ⬜ |
| 3.4 | Runner 코어 | `pipeline-runner/internal/runner/runner.go` | ⬜ |
| 3.5 | Runner 진입점 | `pipeline-runner/cmd/runner/main.go` | ⬜ |
| 3.6 | Dockerfile | `pipeline-runner/Dockerfile` | ⬜ |
| 3.7 | Makefile | `pipeline-runner/Makefile` | ⬜ |
| 3.8 | 테스트 | `*_test.go` | ⬜ |
| 3.9 | go.mod tidy | `pipeline-runner/go.mod` | ⬜ |

## Phase 4: 플러그인 시스템 및 SDK
| # | 작업 | 파일 | 상태 |
|---|------|------|------|
| 4.1 | Stage 인터페이스 | `pipeline-core/pkg/plugin/stage_interface.go` | ⬜ |
| 4.2 | Stage 레지스트리 | `pipeline-core/pkg/plugin/registry.go` | ⬜ |
| 4.3 | Subprocess Runner | `pipeline-core/pkg/plugin/subprocess.go` | ⬜ |
| 4.4 | 테스트 | `pipeline-core/pkg/plugin/*_test.go` | ⬜ |

## Phase 5: Web UI
| # | 작업 | 파일 | 상태 |
|---|------|------|------|
| 5.1 | API 타입 | `web-ui/src/types/plugin.ts` | ⬜ |
| 5.2 | API 서비스 | `web-ui/src/services/pluginApi.ts` | ⬜ |
| 5.3 | 플러그인 목록 페이지 | `web-ui/src/pages/Plugins/PluginList.tsx` | ⬜ |
| 5.4 | 동적 Stage 폼 | `web-ui/src/components/DynamicStageForm.tsx` | ⬜ |
| 5.5 | 클러스터 대시보드 | `web-ui/src/pages/Clusters/ClusterDashboard.tsx` | ⬜ |
| 5.6 | 라우트 등록 | `web-ui/src/App.tsx` 수정 | ⬜ |

## Phase 6: 테스트 및 문서화
| # | 작업 | 상태 |
|---|------|------|
| 6.1 | E2E 테스트 시나리오 | ⬜ |
| 6.2 | 플러그인 개발 가이드 | ⬜ (사용자 요청 시) |
