package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/pipeline-worker/internal/k8s"

	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/pipeline-core/pkg/link"
	redisclient "github.com/conduix/conduix/shared/redis"
	"github.com/conduix/conduix/shared/types"
)

// CommunicationMode 통신 모드
type CommunicationMode int

const (
	ModeRedis  CommunicationMode = iota // Redis Pub/Sub (기본)
	ModeREST                            // REST API 폴백
	ModeHybrid                          // 둘 다 사용
)

// RunningExecution 실행 중인 워크플로우 실행 정보
type RunningExecution struct {
	ExecutionID   string
	WorkflowID    string
	StartedAt     time.Time
	GroupExecutor *executor.GroupExecutor // 모니터링용 GroupExecutor 참조
}

// Agent 파이프라인 에이전트
type Agent struct {
	ID              string
	Hostname        string
	Status          types.AgentStatus
	config          *Config
	runningExecs    map[string]*RunningExecution // executionID -> RunningExecution
	mu              sync.RWMutex
	execMu          sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	redisClient     *redisclient.ResilientClient
	httpClient      *http.Client
	controlPlaneURL string
	commMode        CommunicationMode
	redisHealthy    bool
	healthMu        sync.RWMutex

	// batch 위임: control-plane이 이 cluster에 batch 워크플로우 실행을 위임하면
	// worker가 자기 cluster에 K8s Job을 생성한다. K8s 클라이언트가 없는 환경(로컬 등)에서는 nil.
	jobManager   *k8s.JobManager
	jobManagerMu sync.Mutex
}

// Config 에이전트 설정
type Config struct {
	ID                string        `json:"id"`
	ClusterID         string        `json:"cluster_id"` // 에이전트가 속한 클러스터 ID
	ControlPlaneURL   string        `json:"control_plane_url"`
	RedisHost         string        `json:"redis_host"`
	RedisPort         int           `json:"redis_port"`
	RedisPassword     string        `json:"redis_password"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	Labels            []string      `json:"labels"`
	// 새로운 설정
	CommandPollInterval time.Duration `json:"command_poll_interval"` // REST 폴링 간격
	EnableRESTFallback  bool          `json:"enable_rest_fallback"`  // REST 폴백 활성화
	ExecutionTimeout    time.Duration `json:"execution_timeout"`     // 워크플로우 실행 최대 시간 (기본 10분)

	// batch 위임 시 생성할 K8s Job 설정
	Namespace   string `json:"namespace"`    // Job 생성 네임스페이스 (비면 in-cluster 기본)
	RunnerImage string `json:"runner_image"` // 배치 실행용 pipeline-batch-job 이미지
}

// defaultExecutionTimeout 워크플로우 실행 기본 타임아웃.
const defaultExecutionTimeout = 10 * time.Minute

// NewAgent 새 에이전트 생성
func NewAgent(cfg *Config) (*Agent, error) {
	hostname, _ := os.Hostname()

	id := cfg.ID
	if id == "" {
		id = uuid.New().String()
	}

	ctx, cancel := context.WithCancel(context.Background())

	agent := &Agent{
		ID:              id,
		Hostname:        hostname,
		Status:          types.AgentStatusOffline,
		config:          cfg,
		runningExecs:    make(map[string]*RunningExecution),
		ctx:             ctx,
		cancel:          cancel,
		controlPlaneURL: cfg.ControlPlaneURL,
		commMode:        ModeRedis,
		redisHealthy:    false,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	// Redis 클라이언트 초기화
	if cfg.RedisHost != "" {
		redisAddr := fmt.Sprintf("%s:%d", cfg.RedisHost, cfg.RedisPort)
		redisConfig := redisclient.DefaultConfig(redisAddr)
		redisConfig.Password = cfg.RedisPassword
		redisConfig.OnStateChange = agent.onRedisStateChange
		redisConfig.OnError = agent.onRedisError

		var err error
		agent.redisClient, err = redisclient.NewResilientClient(redisConfig)
		if err != nil {
			fmt.Printf("Warning: Redis connection failed, using REST fallback: %v\n", err)
			if cfg.EnableRESTFallback {
				agent.commMode = ModeREST
			}
		}
	} else if cfg.EnableRESTFallback {
		agent.commMode = ModeREST
	}

	return agent, nil
}

// onRedisStateChange Redis 연결 상태 변경 콜백
func (a *Agent) onRedisStateChange(old, new redisclient.ConnectionState) {
	a.healthMu.Lock()
	a.redisHealthy = (new == redisclient.StateConnected)
	a.healthMu.Unlock()

	fmt.Printf("Redis connection state changed: %s -> %s\n", old, new)

	// 연결 복구 시 Redis 모드로 전환
	if new == redisclient.StateConnected {
		if a.commMode == ModeREST && a.config.EnableRESTFallback {
			fmt.Println("Redis reconnected, switching back to Redis mode")
			a.commMode = ModeHybrid // 안정화될 때까지 하이브리드 모드
		}
	} else if new == redisclient.StateDisconnected && a.config.EnableRESTFallback {
		fmt.Println("Redis disconnected, switching to REST fallback mode")
		a.commMode = ModeREST
	}
}

// onRedisError Redis 에러 콜백
func (a *Agent) onRedisError(err error) {
	fmt.Printf("Redis error: %v\n", err)
}

// Start 에이전트 시작
func (a *Agent) Start() error {
	a.mu.Lock()
	a.Status = types.AgentStatusOnline
	a.mu.Unlock()

	// Control Plane에 등록
	if err := a.registerToControlPlane(); err != nil {
		fmt.Printf("Warning: Failed to register to control plane: %v\n", err)
	}

	// 하트비트 시작
	go a.heartbeatLoop()

	// 명령 수신 시작
	go a.commandLoop()

	fmt.Printf("Agent started: %s (%s)\n", a.ID, a.Hostname)
	return nil
}

// registerToControlPlane Control Plane에 에이전트 등록
func (a *Agent) registerToControlPlane() error {
	if a.controlPlaneURL == "" {
		return fmt.Errorf("control plane URL not configured")
	}

	url := fmt.Sprintf("%s/api/v1/agents/register", a.controlPlaneURL)

	reqBody := map[string]any{
		"id":         a.ID,
		"hostname":   a.Hostname,
		"labels":     a.config.Labels,
		"cluster_id": a.config.ClusterID,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("Agent registered to control plane: %s\n", a.ID)
	return nil
}

// Stop 에이전트 중지
func (a *Agent) Stop() error {
	a.cancel()

	// 실행 중인 워크플로우(그룹) 중지
	a.execMu.RLock()
	execs := make([]*RunningExecution, 0, len(a.runningExecs))
	for _, exec := range a.runningExecs {
		execs = append(execs, exec)
	}
	a.execMu.RUnlock()
	for _, exec := range execs {
		if exec.GroupExecutor != nil {
			_ = exec.GroupExecutor.Stop()
		}
	}

	a.mu.Lock()
	a.Status = types.AgentStatusOffline
	a.mu.Unlock()

	fmt.Printf("Agent stopped: %s\n", a.ID)
	return nil
}

// findGroupExecutor는 executionID 또는 workflowID로 실행 중인 GroupExecutor를 찾는다.
// executionID가 우선하며, 비어 있으면 workflowID로 매칭한다.
func (a *Agent) findGroupExecutor(executionID, workflowID string) *executor.GroupExecutor {
	a.execMu.RLock()
	defer a.execMu.RUnlock()

	if executionID != "" {
		if exec, ok := a.runningExecs[executionID]; ok {
			return exec.GroupExecutor
		}
		return nil
	}
	for _, exec := range a.runningExecs {
		if exec.WorkflowID == workflowID {
			return exec.GroupExecutor
		}
	}
	return nil
}

// StopGroupExecution 워크플로우(그룹) 실행을 중지한다.
func (a *Agent) StopGroupExecution(executionID, workflowID string) error {
	ge := a.findGroupExecutor(executionID, workflowID)
	if ge == nil {
		return fmt.Errorf("no running execution for workflow=%s execution=%s", workflowID, executionID)
	}
	return ge.Stop()
}

// PauseGroupExecution 워크플로우(그룹) 실행을 일시정지한다.
func (a *Agent) PauseGroupExecution(executionID, workflowID string) error {
	ge := a.findGroupExecutor(executionID, workflowID)
	if ge == nil {
		return fmt.Errorf("no running execution for workflow=%s execution=%s", workflowID, executionID)
	}
	return ge.Pause()
}

// ResumeGroupExecution 워크플로우(그룹) 실행을 재개한다.
func (a *Agent) ResumeGroupExecution(executionID, workflowID string) error {
	ge := a.findGroupExecutor(executionID, workflowID)
	if ge == nil {
		return fmt.Errorf("no running execution for workflow=%s execution=%s", workflowID, executionID)
	}
	return ge.Resume()
}

// GetStatus 에이전트 상태 조회
func (a *Agent) GetStatus() *types.Agent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	now := time.Now()
	return &types.Agent{
		ID:            a.ID,
		Hostname:      a.Hostname,
		Status:        a.Status,
		LastHeartbeat: &now,
		Labels:        a.config.Labels,
	}
}

// heartbeatLoop 하트비트 루프
func (a *Agent) heartbeatLoop() {
	interval := a.config.HeartbeatInterval
	if interval == 0 {
		interval = 10 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.sendHeartbeat()
		}
	}
}

// sendHeartbeat 하트비트 전송
func (a *Agent) sendHeartbeat() {
	// 실행 중인 워크플로우 정보 수집
	a.execMu.RLock()
	runningExecs := make([]types.RunningExecutionInfo, 0, len(a.runningExecs))
	for _, exec := range a.runningExecs {
		runningExecs = append(runningExecs, types.RunningExecutionInfo{
			ExecutionID: exec.ExecutionID,
			WorkflowID:  exec.WorkflowID,
			StartedAt:   exec.StartedAt,
		})
	}
	a.execMu.RUnlock()

	heartbeat := types.AgentHeartbeat{
		AgentID:      a.ID,
		ClusterID:    a.config.ClusterID,
		Hostname:     a.Hostname,
		Timestamp:    time.Now(),
		RunningExecs: runningExecs,
	}

	var redisErr, restErr error

	// Redis 하트비트 시도
	if a.redisClient != nil && (a.commMode == ModeRedis || a.commMode == ModeHybrid) {
		key := fmt.Sprintf("agent:%s:heartbeat", a.ID)
		redisErr = a.redisClient.Set(a.ctx, key, heartbeat, 30*time.Second)
		if redisErr == nil {
			return // Redis 성공
		}
		fmt.Printf("Redis heartbeat failed: %v\n", redisErr)
	}

	// REST 폴백
	if a.config.EnableRESTFallback && (a.commMode == ModeREST || a.commMode == ModeHybrid || redisErr != nil) {
		restErr = a.sendHeartbeatREST(heartbeat)
		if restErr != nil {
			fmt.Printf("REST heartbeat failed: %v\n", restErr)
		}
	}

	// 둘 다 실패한 경우 로깅
	if redisErr != nil && restErr != nil {
		fmt.Printf("All heartbeat methods failed - Redis: %v, REST: %v\n", redisErr, restErr)
	}

	// 모니터링 정보도 Redis에 저장 (하트비트와 별도로 업데이트)
	a.updateMonitoringInfo()
}

// updateMonitoringInfo 실행 중인 모든 워크플로우의 모니터링 정보를 Redis에 저장
func (a *Agent) updateMonitoringInfo() {
	if a.redisClient == nil || (a.commMode != ModeRedis && a.commMode != ModeHybrid) {
		return
	}

	a.execMu.RLock()
	execs := make([]*RunningExecution, 0, len(a.runningExecs))
	for _, exec := range a.runningExecs {
		execs = append(execs, exec)
	}
	a.execMu.RUnlock()

	// 각 실행의 모니터링 정보를 Redis에 저장
	for _, exec := range execs {
		if exec.GroupExecutor == nil {
			continue
		}

		monitoringInfo := exec.GroupExecutor.GetMonitoringInfo()
		if monitoringInfo == nil {
			continue
		}

		// 데이터 샘플 개수 제한 (성능 최적화)
		for i := range monitoringInfo.Pipelines {
			for j := range monitoringInfo.Pipelines[i].Stages {
				samples := monitoringInfo.Pipelines[i].Stages[j].Samples
				if len(samples) > 10 {
					// 최근 10개만 유지
					monitoringInfo.Pipelines[i].Stages[j].Samples = samples[len(samples)-10:]
				}
			}
		}

		key := fmt.Sprintf("agent:%s:monitoring:%s", a.ID, exec.ExecutionID)
		// 5초 TTL (하트비트보다 짧게, 빠른 갱신)
		err := a.redisClient.Set(a.ctx, key, monitoringInfo, 5*time.Second)
		if err != nil {
			// 조용히 실패 (하트비트는 성공했으므로)
			continue
		}
	}
}

// sendHeartbeatREST REST API를 통한 하트비트 전송
func (a *Agent) sendHeartbeatREST(heartbeat types.AgentHeartbeat) error {
	url := fmt.Sprintf("%s/api/v1/agents/%s/heartbeat", a.controlPlaneURL, a.ID)

	data, err := json.Marshal(heartbeat)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// commandLoop 명령 수신 루프
func (a *Agent) commandLoop() {
	// Redis Pub/Sub 구독
	if a.redisClient != nil && (a.commMode == ModeRedis || a.commMode == ModeHybrid) {
		// 에이전트별 명령 채널
		channel := fmt.Sprintf("agent:commands:%s", a.ID)
		err := a.redisClient.Subscribe(a.ctx, channel, a.handleCommand)
		if err != nil {
			fmt.Printf("Failed to subscribe to commands via Redis: %v\n", err)
		} else {
			fmt.Printf("Subscribed to Redis channel: %s\n", channel)
		}

		// 클러스터별 실행 채널 (ClusterID가 있는 경우)
		if a.config.ClusterID != "" {
			clusterChannel := fmt.Sprintf("cluster:%s:execute", a.config.ClusterID)
			err = a.redisClient.Subscribe(a.ctx, clusterChannel, a.handleGroupExecution)
			if err != nil {
				fmt.Printf("Failed to subscribe to cluster execution channel: %v\n", err)
			} else {
				fmt.Printf("Subscribed to Redis channel: %s\n", clusterChannel)
			}
		}

		// 그룹 실행 브로드캐스트 채널 (하위 호환성 - ClusterID 없는 경우에만)
		if a.config.ClusterID == "" {
			groupChannel := "group:execute:broadcast"
			err = a.redisClient.Subscribe(a.ctx, groupChannel, a.handleGroupExecution)
			if err != nil {
				fmt.Printf("Failed to subscribe to group execution channel: %v\n", err)
			} else {
				fmt.Printf("Subscribed to Redis channel: %s\n", groupChannel)
			}
		}

		// 워크플로우 제어 명령 채널 (stop/pause/resume). handleCommand로 라우팅한다.
		cmdChannel := "workflow:commands:broadcast"
		if a.config.ClusterID != "" {
			cmdChannel = fmt.Sprintf("cluster:%s:commands", a.config.ClusterID)
		}
		if err = a.redisClient.Subscribe(a.ctx, cmdChannel, a.handleCommand); err != nil {
			fmt.Printf("Failed to subscribe to workflow command channel: %v\n", err)
		} else {
			fmt.Printf("Subscribed to Redis channel: %s\n", cmdChannel)
		}
	}

	// REST 폴링 (폴백 또는 하이브리드 모드)
	if a.config.EnableRESTFallback {
		go a.commandPollLoop()
	}
}

// commandPollLoop REST API를 통한 명령 폴링 루프
func (a *Agent) commandPollLoop() {
	interval := a.config.CommandPollInterval
	if interval == 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			// Redis가 정상이고 하이브리드 모드가 아니면 폴링 스킵
			a.healthMu.RLock()
			healthy := a.redisHealthy
			a.healthMu.RUnlock()

			if healthy && a.commMode == ModeRedis {
				continue
			}

			// REST API로 명령 조회
			commands, err := a.fetchCommandsREST()
			if err != nil {
				fmt.Printf("Failed to fetch commands via REST: %v\n", err)
				continue
			}

			for _, cmd := range commands {
				a.handleCommand(cmd)
			}
		}
	}
}

// fetchCommandsREST REST API를 통한 명령 조회
func (a *Agent) fetchCommandsREST() ([]string, error) {
	url := fmt.Sprintf("%s/api/v1/agents/%s/commands", a.controlPlaneURL, a.ID)

	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil // 명령 없음
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	var commands []string
	if err := json.NewDecoder(resp.Body).Decode(&commands); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return commands, nil
}

// handleCommand 명령 처리
func (a *Agent) handleCommand(message string) {
	fmt.Printf("Received command: %s\n", message)

	var cmd types.AgentCommand
	if err := json.Unmarshal([]byte(message), &cmd); err != nil {
		fmt.Printf("Failed to parse command: %v\n", err)
		return
	}

	switch cmd.Type {
	case types.CommandStopWorkflow:
		if err := a.StopGroupExecution(cmd.ExecutionID, cmd.WorkflowID); err != nil {
			fmt.Printf("Failed to stop workflow: %v\n", err)
		}
	case types.CommandPauseWorkflow:
		if err := a.PauseGroupExecution(cmd.ExecutionID, cmd.WorkflowID); err != nil {
			fmt.Printf("Failed to pause workflow: %v\n", err)
		}
	case types.CommandResumeWorkflow:
		if err := a.ResumeGroupExecution(cmd.ExecutionID, cmd.WorkflowID); err != nil {
			fmt.Printf("Failed to resume workflow: %v\n", err)
		}
	default:
		fmt.Printf("Unknown command type: %s\n", cmd.Type)
	}
}

// handleGroupExecution 그룹 실행 명령 처리
func (a *Agent) handleGroupExecution(message string) {
	fmt.Printf("Received group execution command: %s\n", message)

	var cmd types.GroupExecutionCommand
	if err := json.Unmarshal([]byte(message), &cmd); err != nil {
		fmt.Printf("Failed to parse group execution command: %v\n", err)
		return
	}

	fmt.Printf("Group execution: workflow=%s, execution=%s, triggered_by=%s\n",
		cmd.WorkflowID, cmd.ExecutionID, cmd.TriggeredBy)

	// 워크플로우 설정이 없으면 처리 불가
	if cmd.WorkflowConfig == nil {
		fmt.Printf("Group execution command missing group config\n")
		return
	}

	// 실행 소유권 claim: 한 클러스터의 여러 에이전트가 같은 명령을 수신하므로,
	// Redis SETNX로 한 execution을 정확히 한 에이전트만 실행하도록 보장한다(중복 실행 방지).
	if !a.claimExecution(cmd.ExecutionID) {
		fmt.Printf("Execution %s already claimed by another agent, skipping\n", cmd.ExecutionID)
		return
	}

	// batch 워크플로우는 이 cluster에 K8s Job으로 위임 생성(일회성 격리 실행).
	// realtime은 worker 프로세스 내에서 직접 상주 실행.
	if cmd.WorkflowConfig.Type == types.WorkflowTypeBatch {
		go a.delegateBatchJob(&cmd)
		return
	}

	// 그룹 실행 시작 (비동기)
	go a.executeGroup(&cmd)
}

// delegateBatchJob은 batch 워크플로우를 이 worker가 속한 cluster의 K8s Job으로 생성한다.
// control-plane이 아니라 worker가 in-cluster 권한으로 Job을 만든다(위임 구조).
// Job Pod(pipeline-batch-job)가 실행 후 control-plane에 REST 콜백으로 결과를 보고한다.
func (a *Agent) delegateBatchJob(cmd *types.GroupExecutionCommand) {
	startTime := time.Now()
	workflow := cmd.WorkflowConfig

	jm := a.getJobManager()
	if jm == nil {
		completedAt := time.Now()
		_ = a.reportGroupExecutionResult(&types.GroupExecutionResult{
			ExecutionID:  cmd.ExecutionID,
			WorkflowID:   cmd.WorkflowID,
			Status:       types.PipelineGroupStatusError,
			StartedAt:    startTime,
			CompletedAt:  &completedAt,
			ErrorMessage: "batch delegation requires a Kubernetes cluster, but no K8s client is available on this worker",
		})
		return
	}

	// JobConfig(선택): 있으면 리소스 스펙으로 사용, 없으면 JobManager 기본값.
	var jobConfig types.JobConfig
	if cmd.JobConfig != "" {
		if err := json.Unmarshal([]byte(cmd.JobConfig), &jobConfig); err != nil {
			fmt.Printf("Invalid job_config for execution %s, using defaults: %v\n", cmd.ExecutionID, err)
		}
	}

	pipelinesJSON, err := json.Marshal(workflow.Pipelines)
	if err != nil {
		completedAt := time.Now()
		_ = a.reportGroupExecutionResult(&types.GroupExecutionResult{
			ExecutionID:  cmd.ExecutionID,
			WorkflowID:   cmd.WorkflowID,
			Status:       types.PipelineGroupStatusError,
			StartedAt:    startTime,
			CompletedAt:  &completedAt,
			ErrorMessage: fmt.Sprintf("failed to serialize pipelines: %v", err),
		})
		return
	}

	job, err := jm.CreateBatchJob(a.ctx, &k8s.JobSpec{
		ExecutionID:     cmd.ExecutionID,
		WorkflowID:      cmd.WorkflowID,
		PipelinesConfig: string(pipelinesJSON),
		JobConfig:       jobConfig,
	})
	if err != nil {
		completedAt := time.Now()
		_ = a.reportGroupExecutionResult(&types.GroupExecutionResult{
			ExecutionID:  cmd.ExecutionID,
			WorkflowID:   cmd.WorkflowID,
			Status:       types.PipelineGroupStatusError,
			StartedAt:    startTime,
			CompletedAt:  &completedAt,
			ErrorMessage: fmt.Sprintf("failed to create batch job: %v", err),
		})
		return
	}

	// Job 생성 성공. 이후 상태·결과는 Job Pod가 control-plane에 직접 콜백한다.
	fmt.Printf("Delegated batch job %s for execution %s (workflow=%s)\n", job.Name, cmd.ExecutionID, cmd.WorkflowID)
}

// getJobManager는 batch 위임용 JobManager를 지연 생성한다(in-cluster K8s 클라이언트).
// K8s 환경이 아니면 nil을 반환하며, 이 경우 batch 위임은 실패로 보고된다.
func (a *Agent) getJobManager() *k8s.JobManager {
	a.jobManagerMu.Lock()
	defer a.jobManagerMu.Unlock()
	if a.jobManager != nil {
		return a.jobManager
	}
	client, err := k8s.NewClient(a.config.Namespace)
	if err != nil {
		fmt.Printf("K8s client unavailable, batch delegation disabled: %v\n", err)
		return nil
	}
	a.jobManager = k8s.NewJobManager(client, a.controlPlaneURL, a.config.RunnerImage)
	return a.jobManager
}

// claimExecution은 Redis SETNX로 execution 소유권을 원자적으로 획득한다.
// true면 이 에이전트가 실행 담당이다. Redis 미가용 시 false(실행 안 함) — 중복보다 미실행이 안전.
// ExecutionID가 없으면(레거시/브로드캐스트) claim 없이 true (기존 동작 보존).
func (a *Agent) claimExecution(executionID string) bool {
	if executionID == "" {
		return true
	}
	if a.redisClient == nil {
		// Redis 없는 standalone 모드: 단일 에이전트 전제이므로 실행 허용
		return true
	}

	key := fmt.Sprintf("execution:claim:%s", executionID)
	// TTL은 실행 최대 시간(10분)보다 넉넉히. 담당 에이전트 크래시 시 만료되어 재배치 가능.
	acquired, err := a.redisClient.SetNX(a.ctx, key, a.ID, 15*time.Minute)
	if err != nil {
		fmt.Printf("Failed to claim execution %s: %v (skipping to avoid duplicate)\n", executionID, err)
		return false
	}
	return acquired
}

// executeGroup 파이프라인 그룹 실행
func (a *Agent) executeGroup(cmd *types.GroupExecutionCommand) {
	startTime := time.Now()
	workflow := cmd.WorkflowConfig

	// panic이 에이전트 전체를 죽이지 않도록 복구하고 실행을 실패로 보고한다.
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("PANIC in executeGroup (workflow=%s execution=%s): %v\n", cmd.WorkflowID, cmd.ExecutionID, r)
			completedAt := time.Now()
			_ = a.reportGroupExecutionResult(&types.GroupExecutionResult{
				ExecutionID:  cmd.ExecutionID,
				WorkflowID:   cmd.WorkflowID,
				Status:       types.PipelineGroupStatusError,
				StartedAt:    startTime,
				CompletedAt:  &completedAt,
				ErrorMessage: fmt.Sprintf("panic during execution: %v", r),
			})
		}
	}()

	fmt.Printf("Starting group execution: %s (%s)\n", workflow.Name, workflow.ID)

	// Link Client 생성
	linkClient := link.NewClient(a.controlPlaneURL)

	// GroupExecutor를 사용하여 파이프라인 그룹 실행
	groupExecutor := executor.NewGroupExecutor(workflow, executor.WithLinkClient(linkClient))

	// 실행 추적 시작 (GroupExecutor 포함)
	a.execMu.Lock()
	a.runningExecs[cmd.ExecutionID] = &RunningExecution{
		ExecutionID:   cmd.ExecutionID,
		WorkflowID:    cmd.WorkflowID,
		StartedAt:     startTime,
		GroupExecutor: groupExecutor,
	}
	a.execMu.Unlock()

	// 함수 종료 시 실행 추적 제거
	defer func() {
		a.execMu.Lock()
		delete(a.runningExecs, cmd.ExecutionID)
		a.execMu.Unlock()
	}()

	// 실행 시작 (타임아웃은 설정값, 미설정 시 기본 10분)
	execTimeout := a.config.ExecutionTimeout
	if execTimeout <= 0 {
		execTimeout = defaultExecutionTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	_, err := groupExecutor.Start(ctx, cmd.TriggeredBy)
	if err != nil {
		fmt.Printf("Failed to start group execution: %v\n", err)

		// 실패 결과 보고
		completedAt := time.Now()
		executionResult := &types.GroupExecutionResult{
			ExecutionID:  cmd.ExecutionID,
			WorkflowID:   cmd.WorkflowID,
			Status:       types.PipelineGroupStatusError,
			StartedAt:    startTime,
			CompletedAt:  &completedAt,
			ErrorMessage: err.Error(),
		}
		if err := a.reportGroupExecutionResult(executionResult); err != nil {
			fmt.Printf("Failed to report group execution result: %v\n", err)
		}
		return
	}

	// 실행 완료 대기 (execTimeout까지)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("Group execution timed out: %s\n", workflow.Name)
			_ = groupExecutor.Stop()

			completedAt := time.Now()
			executionResult := &types.GroupExecutionResult{
				ExecutionID:  cmd.ExecutionID,
				WorkflowID:   cmd.WorkflowID,
				Status:       types.PipelineGroupStatusError,
				StartedAt:    startTime,
				CompletedAt:  &completedAt,
				ErrorMessage: "execution timed out",
			}
			if err := a.reportGroupExecutionResult(executionResult); err != nil {
				fmt.Printf("Failed to report group execution result: %v\n", err)
			}
			return

		case <-ticker.C:
			// 실행 상태 확인
			currentExec := groupExecutor.Execution()
			if currentExec == nil {
				continue
			}

			// 완료 확인
			if currentExec.Status == types.PipelineGroupStatusCompleted ||
				currentExec.Status == types.PipelineGroupStatusError ||
				currentExec.Status == types.PipelineGroupStatusStopped {

				completedAt := time.Now()
				duration := completedAt.Sub(startTime)

				// 결과 보고
				executionResult := &types.GroupExecutionResult{
					ExecutionID:     cmd.ExecutionID,
					WorkflowID:      cmd.WorkflowID,
					Status:          currentExec.Status,
					PipelineResults: currentExec.PipelineResults,
					TotalRecords:    currentExec.TotalRecords,
					FailedRecords:   currentExec.FailedRecords,
					StartedAt:       startTime,
					CompletedAt:     &completedAt,
					ErrorMessage:    currentExec.ErrorMessage,
				}

				if err := a.reportGroupExecutionResult(executionResult); err != nil {
					fmt.Printf("Failed to report group execution result: %v\n", err)
				}

				fmt.Printf("Group execution completed: %s (status: %s, duration: %v, records: %d)\n",
					workflow.Name, currentExec.Status, duration, currentExec.TotalRecords)
				return
			}
		}
	}
}

// reportGroupExecutionResult 그룹 실행 결과 보고
func (a *Agent) reportGroupExecutionResult(result *types.GroupExecutionResult) error {
	fmt.Printf("[reportGroupExecutionResult] Reporting result for workflow=%s, execution=%s, status=%s\n",
		result.WorkflowID, result.ExecutionID, result.Status)

	// REST API로 결과 보고 (직접 DB 업데이트를 위해 우선 사용)
	if a.controlPlaneURL != "" {
		url := fmt.Sprintf("%s/api/v1/workflows/%s/executions/%s/result",
			a.controlPlaneURL, result.WorkflowID, result.ExecutionID)

		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("failed to marshal result: %w", err)
		}

		req, err := http.NewRequestWithContext(a.ctx, "POST", url, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			fmt.Printf("[reportGroupExecutionResult] REST API failed: %v\n", err)
		} else {
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				body, _ := io.ReadAll(resp.Body)
				fmt.Printf("[reportGroupExecutionResult] REST API error: %s\n", string(body))
			} else {
				fmt.Printf("[reportGroupExecutionResult] Result reported successfully via REST API\n")
				return nil
			}
		}
	}

	// Redis로 결과 발행 (폴백)
	if a.redisClient != nil && a.redisHealthy {
		channel := fmt.Sprintf("workflow:result:%s", result.WorkflowID)
		if err := a.redisClient.Publish(a.ctx, channel, result); err != nil {
			fmt.Printf("[reportGroupExecutionResult] Failed to publish result via Redis: %v\n", err)
		} else {
			fmt.Printf("[reportGroupExecutionResult] Result published via Redis\n")
			return nil
		}
	}

	return fmt.Errorf("failed to report result: no available method")
}

// GetCommunicationMode 현재 통신 모드 조회
func (a *Agent) GetCommunicationMode() CommunicationMode {
	return a.commMode
}

// IsRedisHealthy Redis 연결 상태 조회
func (a *Agent) IsRedisHealthy() bool {
	a.healthMu.RLock()
	defer a.healthMu.RUnlock()
	return a.redisHealthy
}

// GetRedisMetrics Redis 메트릭 조회
func (a *Agent) GetRedisMetrics() *redisclient.Metrics {
	if a.redisClient == nil {
		return nil
	}
	metrics := a.redisClient.GetMetrics()
	return &metrics
}

// GetExecutionMonitoring 특정 실행의 모니터링 정보 조회
func (a *Agent) GetExecutionMonitoring(executionID string) *types.ExecutionMonitoringInfo {
	a.execMu.RLock()
	exec, ok := a.runningExecs[executionID]
	a.execMu.RUnlock()

	if !ok || exec.GroupExecutor == nil {
		return nil
	}

	return exec.GroupExecutor.GetMonitoringInfo()
}

// GetAllExecutionMonitoring 모든 실행의 모니터링 정보 조회
func (a *Agent) GetAllExecutionMonitoring() []*types.ExecutionMonitoringInfo {
	a.execMu.RLock()
	defer a.execMu.RUnlock()

	result := make([]*types.ExecutionMonitoringInfo, 0, len(a.runningExecs))
	for _, exec := range a.runningExecs {
		if exec.GroupExecutor != nil {
			if info := exec.GroupExecutor.GetMonitoringInfo(); info != nil {
				result = append(result, info)
			}
		}
	}
	return result
}
