# Phase 2: Agent - 클러스터 매니저 역할 변경 작업 플랜

## 목표
Agent를 파이프라인 실행 엔진에서 클러스터 매니저로 변경
- K8s Job/CronJob/Deployment 생성/삭제
- 리소스 모니터링
- Leader Election
- Control Plane 통신 강화

## 작업 항목

| # | 작업 | 파일 | 상태 |
|---|------|------|------|
| 2.1 | K8s 클라이언트 래퍼 | `pipeline-agent/internal/k8s/client.go` | ✅ |
| 2.2 | Job/CronJob 관리자 | `pipeline-agent/internal/k8s/job_manager.go` | ✅ |
| 2.3 | Deployment 관리자 | `pipeline-agent/internal/k8s/deployment_manager.go` | ✅ |
| 2.4 | 리소스 모니터링 | `pipeline-agent/internal/monitor/resource_monitor.go` | ✅ |
| 2.5 | Leader Election | `pipeline-agent/internal/leader/election.go` | ✅ |
| 2.6 | Control Plane 통신 | `pipeline-agent/internal/controlplane/client.go` | ✅ |
| 2.7 | 테스트 | `*_test.go` | ✅ (커버리지 72.8%) |

## 세부 설계

### 2.1 K8s Client (`internal/k8s/client.go`)
- InCluster / OutOfCluster 자동 감지
- namespace 관리
- 인터페이스 기반 (테스트 용이)

### 2.2 Job Manager (`internal/k8s/job_manager.go`)
- CreateBatchJob: 배치 파이프라인용 K8s Job 생성
- CreateCronJob: 스케줄 파이프라인용 CronJob 생성
- DeleteJob/DeleteCronJob: 리소스 정리
- GetJobStatus: 실행 상태 조회
- ListJobs: 현재 Job 목록

### 2.3 Deployment Manager (`internal/k8s/deployment_manager.go`)
- CreateDeployment: 스트리밍 파이프라인용 Deployment 생성
- UpdateDeployment: replicas 변경 등
- DeleteDeployment: 리소스 정리
- GetDeploymentStatus: 상태 조회
- ScaleDeployment: 스케일 조정

### 2.4 Resource Monitor (`internal/monitor/resource_monitor.go`)
- 주기적 클러스터 리소스 수집 (CPU/Memory/Pod count)
- Job/Deployment 상태 감시
- 메트릭 Control Plane으로 보고

### 2.5 Leader Election (`internal/leader/election.go`)
- K8s Lease 기반 Leader Election
- 리더만 Job/Deployment 생성 권한
- 자동 Failover

### 2.6 Control Plane Client (`internal/controlplane/client.go`)
- 기존 agent.go 내 HTTP 통신 로직 분리
- 하트비트, 메트릭 보고, 명령 수신
- Pipeline Run 상태 업데이트

## 의존성
- `k8s.io/client-go` (이미 go.mod에 포함)
- `k8s.io/api`
- `k8s.io/apimachinery`
