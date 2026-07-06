package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

	// claimRenewInterval: claim 갱신 주기(0이면 기본 claimRenewInterval). 테스트에서 단축용.
	claimRenewEvery time.Duration

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
			slog.Warn("redis connection failed, using REST fallback", "error", err, "agent_id", agent.ID)
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

	slog.Info("redis connection state changed", "from", old, "to", new, "agent_id", a.ID)

	// 연결 복구 시 Redis 모드로 전환
	if new == redisclient.StateConnected {
		if a.commMode == ModeREST && a.config.EnableRESTFallback {
			slog.Info("redis reconnected, switching back to redis mode", "agent_id", a.ID)
			a.commMode = ModeHybrid // 안정화될 때까지 하이브리드 모드
		}
	} else if new == redisclient.StateDisconnected && a.config.EnableRESTFallback {
		slog.Warn("redis disconnected, switching to REST fallback mode", "agent_id", a.ID)
		a.commMode = ModeREST
	}
}

// onRedisError Redis 에러 콜백
func (a *Agent) onRedisError(err error) {
	slog.Error("redis error", "error", err, "agent_id", a.ID)
}

// Start 에이전트 시작
func (a *Agent) Start() error {
	a.mu.Lock()
	a.Status = types.AgentStatusOnline
	a.mu.Unlock()

	// Control Plane에 등록
	if err := a.registerToControlPlane(); err != nil {
		slog.Warn("failed to register to control plane", "error", err, "agent_id", a.ID)
	}

	// 하트비트 시작
	go a.heartbeatLoop()

	// 명령 수신 시작
	go a.commandLoop()

	slog.Info("agent started", "agent_id", a.ID, "hostname", a.Hostname)
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

	slog.Info("agent registered to control plane", "agent_id", a.ID)
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

	slog.Info("agent stopped", "agent_id", a.ID)
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
		slog.Warn("redis heartbeat failed", "error", redisErr, "agent_id", a.ID)
	}

	// REST 폴백
	if a.config.EnableRESTFallback && (a.commMode == ModeREST || a.commMode == ModeHybrid || redisErr != nil) {
		restErr = a.sendHeartbeatREST(heartbeat)
		if restErr != nil {
			slog.Warn("REST heartbeat failed", "error", restErr, "agent_id", a.ID)
		}
	}

	// 둘 다 실패한 경우 로깅
	if redisErr != nil && restErr != nil {
		slog.Error("all heartbeat methods failed", "redis_error", redisErr, "rest_error", restErr, "agent_id", a.ID)
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
			slog.Error("failed to subscribe to commands via redis", "error", err, "channel", channel, "agent_id", a.ID)
		} else {
			slog.Info("subscribed to redis channel", "channel", channel, "agent_id", a.ID)
		}

		// 클러스터별 실행 채널 (ClusterID가 있는 경우)
		if a.config.ClusterID != "" {
			clusterChannel := fmt.Sprintf("cluster:%s:execute", a.config.ClusterID)
			err = a.redisClient.Subscribe(a.ctx, clusterChannel, a.handleGroupExecution)
			if err != nil {
				slog.Error("failed to subscribe to cluster execution channel", "error", err, "channel", clusterChannel, "cluster_id", a.config.ClusterID, "agent_id", a.ID)
			} else {
				slog.Info("subscribed to redis channel", "channel", clusterChannel, "cluster_id", a.config.ClusterID, "agent_id", a.ID)
			}
		}

		// 그룹 실행 브로드캐스트 채널 (하위 호환성 - ClusterID 없는 경우에만)
		if a.config.ClusterID == "" {
			groupChannel := "group:execute:broadcast"
			err = a.redisClient.Subscribe(a.ctx, groupChannel, a.handleGroupExecution)
			if err != nil {
				slog.Error("failed to subscribe to group execution channel", "error", err, "channel", groupChannel, "agent_id", a.ID)
			} else {
				slog.Info("subscribed to redis channel", "channel", groupChannel, "agent_id", a.ID)
			}
		}

		// 워크플로우 제어 명령 채널 (stop/pause/resume). handleCommand로 라우팅한다.
		cmdChannel := "workflow:commands:broadcast"
		if a.config.ClusterID != "" {
			cmdChannel = fmt.Sprintf("cluster:%s:commands", a.config.ClusterID)
		}
		if err = a.redisClient.Subscribe(a.ctx, cmdChannel, a.handleCommand); err != nil {
			slog.Error("failed to subscribe to workflow command channel", "error", err, "channel", cmdChannel, "agent_id", a.ID)
		} else {
			slog.Info("subscribed to redis channel", "channel", cmdChannel, "agent_id", a.ID)
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
				slog.Error("failed to fetch commands via REST", "error", err, "agent_id", a.ID)
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
	slog.Info("received command", "message", message, "agent_id", a.ID)

	var cmd types.AgentCommand
	if err := json.Unmarshal([]byte(message), &cmd); err != nil {
		slog.Error("failed to parse command", "error", err, "agent_id", a.ID)
		return
	}

	switch cmd.Type {
	case types.CommandStopWorkflow:
		if err := a.StopGroupExecution(cmd.ExecutionID, cmd.WorkflowID); err != nil {
			slog.Error("failed to stop workflow", "error", err, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
		}
	case types.CommandPauseWorkflow:
		if err := a.PauseGroupExecution(cmd.ExecutionID, cmd.WorkflowID); err != nil {
			slog.Error("failed to pause workflow", "error", err, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
		}
	case types.CommandResumeWorkflow:
		if err := a.ResumeGroupExecution(cmd.ExecutionID, cmd.WorkflowID); err != nil {
			slog.Error("failed to resume workflow", "error", err, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
		}
	default:
		slog.Warn("unknown command type", "type", cmd.Type, "agent_id", a.ID)
	}
}

// handleGroupExecution 그룹 실행 명령 처리
func (a *Agent) handleGroupExecution(message string) {
	slog.Info("received group execution command", "message", message, "agent_id", a.ID)

	var cmd types.GroupExecutionCommand
	if err := json.Unmarshal([]byte(message), &cmd); err != nil {
		slog.Error("failed to parse group execution command", "error", err, "agent_id", a.ID)
		return
	}

	slog.Info("group execution", "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID, "triggered_by", cmd.TriggeredBy)

	// 워크플로우 설정이 없으면 처리 불가
	if cmd.WorkflowConfig == nil {
		slog.Error("group execution command missing group config", "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
		return
	}

	// 실행 소유권 claim: 한 클러스터의 여러 에이전트가 같은 명령을 수신하므로,
	// Redis SETNX로 한 execution을 정확히 한 에이전트만 실행하도록 보장한다(중복 실행 방지).
	if !a.claimExecution(cmd.ExecutionID) {
		slog.Info("execution already claimed by another agent, skipping", "execution_id", cmd.ExecutionID, "workflow_id", cmd.WorkflowID, "agent_id", a.ID)
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
			slog.Warn("invalid job_config, using defaults", "error", err, "execution_id", cmd.ExecutionID, "workflow_id", cmd.WorkflowID)
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
	slog.Info("delegated batch job", "job", job.Name, "execution_id", cmd.ExecutionID, "workflow_id", cmd.WorkflowID)
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
		slog.Warn("K8s client unavailable, batch delegation disabled", "error", err, "agent_id", a.ID)
		return nil
	}
	a.jobManager = k8s.NewJobManager(client, a.controlPlaneURL, a.config.RunnerImage)
	return a.jobManager
}

// claim TTL/갱신 주기. CDC 같은 장기 실행은 실행 시간이 TTL 을 넘으므로 반드시 주기 갱신이
// 필요하다(갱신 없이 만료되면 다른 에이전트가 같은 소스를 잡아 중복 실행 — binlog/slot 이중 소비).
// 갱신 주기는 TTL 의 1/3 로 두 번 놓쳐도 만료 전 복구 여지를 남긴다.
const (
	claimTTL           = 30 * time.Second
	claimRenewInterval = claimTTL / 3
)

func executionClaimKey(executionID string) string {
	return fmt.Sprintf("execution:claim:%s", executionID)
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

	// TTL 짧게(claimTTL) 두고 실행 중 renewClaim 으로 연장. 크래시 시 빠르게 만료되어 재배치.
	acquired, err := a.redisClient.SetNX(a.ctx, executionClaimKey(executionID), a.ID, claimTTL)
	if err != nil {
		slog.Error("failed to claim execution, skipping to avoid duplicate", "error", err, "execution_id", executionID, "agent_id", a.ID)
		return false
	}
	return acquired
}

// renewClaim은 이 에이전트가 여전히 소유한 claim의 TTL을 연장한다.
// 반환 false = 소유권 상실(다른 에이전트가 가져갔거나 만료 후 회수됨) → 호출자는 실행을 멈춰야 한다.
// standalone(Redis nil)/executionID 없음은 항상 true(소유 전제 유지).
func (a *Agent) renewClaim(executionID string) bool {
	if executionID == "" || a.redisClient == nil {
		return true
	}

	key := executionClaimKey(executionID)
	owner, err := a.redisClient.Get(a.ctx, key)
	if err == nil && owner == a.ID {
		// 여전히 우리 소유 → TTL 연장.
		if err := a.redisClient.Set(a.ctx, key, a.ID, claimTTL); err != nil {
			slog.Warn("failed to renew claim TTL, will retry next tick", "error", err, "execution_id", executionID, "agent_id", a.ID)
			return true // 일시적 오류는 즉시 상실로 보지 않음(다음 tick 재시도). 만료되면 소유자 부재로 회수.
		}
		return true
	}
	if err == nil && owner != "" && owner != a.ID {
		slog.Warn("claim taken over by another agent, stopping", "execution_id", executionID, "current_owner", owner, "agent_id", a.ID)
		return false
	}

	// owner 비어있음(키 만료/삭제): 우리가 재획득 시도. 성공하면 계속, 실패하면 남이 가져간 것.
	reacquired, sErr := a.redisClient.SetNX(a.ctx, key, a.ID, claimTTL)
	if sErr != nil {
		slog.Warn("failed to reacquire claim, will retry next tick", "error", sErr, "execution_id", executionID, "agent_id", a.ID)
		return true // Redis 순단은 즉시 중단시키지 않음(중단이 곧 중복은 아님; 다음 tick 판정).
	}
	if !reacquired {
		slog.Warn("claim lost and reacquire failed, stopping", "execution_id", executionID, "agent_id", a.ID)
		return false
	}
	return true
}

// releaseClaim은 실행 종료 시 claim을 해제해 TTL 대기 없이 즉시 재배치 가능하게 한다.
// 소유자 확인 없이 Del 하지 않도록, 우리 소유일 때만 해제한다(다른 에이전트 claim 삭제 방지).
func (a *Agent) releaseClaim(executionID string) {
	if executionID == "" || a.redisClient == nil {
		return
	}
	key := executionClaimKey(executionID)
	if owner, err := a.redisClient.Get(a.ctx, key); err == nil && owner == a.ID {
		if err := a.redisClient.Del(a.ctx, key); err != nil {
			slog.Warn("failed to release claim (will expire via TTL)", "error", err, "execution_id", executionID)
		}
	}
}

// claimRenewalLoop은 실행 ctx가 끝날 때까지 주기적으로 claim을 갱신한다.
// 소유권 상실 시 cancel()로 실행 ctx를 취소해 소스(CDC 포함)를 멈춘다.
// standalone/executionID 없음은 갱신이 항상 true라 취소되지 않는다(단일 인스턴스 전제).
func (a *Agent) claimRenewalLoop(ctx context.Context, cancel context.CancelFunc, executionID string) {
	if executionID == "" || a.redisClient == nil {
		return
	}
	interval := a.claimRenewEvery
	if interval <= 0 {
		interval = claimRenewInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !a.renewClaim(executionID) {
				slog.Error("execution claim lost, canceling to prevent duplicate execution",
					"execution_id", executionID, "agent_id", a.ID)
				cancel()
				return
			}
		}
	}
}

// executeGroup 파이프라인 그룹 실행
func (a *Agent) executeGroup(cmd *types.GroupExecutionCommand) {
	startTime := time.Now()
	workflow := cmd.WorkflowConfig

	// panic이 에이전트 전체를 죽이지 않도록 복구하고 실행을 실패로 보고한다.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in executeGroup", "panic", r, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
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

	slog.Info("starting group execution", "name", workflow.Name, "workflow_id", workflow.ID, "execution_id", cmd.ExecutionID)

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

	// 함수 종료 시 실행 추적 제거 + claim 해제(즉시 재배치 가능).
	defer func() {
		a.execMu.Lock()
		delete(a.runningExecs, cmd.ExecutionID)
		a.execMu.Unlock()
		a.releaseClaim(cmd.ExecutionID)
	}()

	// 실행 시작 (타임아웃은 설정값, 미설정 시 기본 10분)
	execTimeout := a.config.ExecutionTimeout
	if execTimeout <= 0 {
		execTimeout = defaultExecutionTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	// claim 갱신 루프: 소유권을 잃으면(다른 에이전트 인수/만료 후 회수 실패) ctx 를 취소해
	// 실행을 멈춘다. CDC 소스는 이 ctx 를 관측하므로 binlog/slot 이중 소비를 막는다.
	// 새 소유자는 checkpoint(committedPos/LSN)부터 재개하므로 유실 없이 이어받는다.
	go a.claimRenewalLoop(ctx, cancel, cmd.ExecutionID)

	_, err := groupExecutor.Start(ctx, cmd.TriggeredBy)
	if err != nil {
		slog.Error("failed to start group execution", "error", err, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)

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
			slog.Error("failed to report group execution result", "error", err, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
		}
		return
	}

	// 실행 완료 대기 (execTimeout까지)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 타임아웃(DeadlineExceeded)과 claim 상실(Canceled)을 구분해 보고한다.
			reason := "execution timed out"
			if ctx.Err() == context.Canceled {
				reason = "execution stopped: claim lost (another agent took over)"
			}
			slog.Error("group execution ended", "reason", reason, "name", workflow.Name, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
			_ = groupExecutor.Stop()

			completedAt := time.Now()
			executionResult := &types.GroupExecutionResult{
				ExecutionID:  cmd.ExecutionID,
				WorkflowID:   cmd.WorkflowID,
				Status:       types.PipelineGroupStatusError,
				StartedAt:    startTime,
				CompletedAt:  &completedAt,
				ErrorMessage: reason,
			}
			if err := a.reportGroupExecutionResult(executionResult); err != nil {
				slog.Error("failed to report group execution result", "error", err, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
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
					slog.Error("failed to report group execution result", "error", err, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
				}

				slog.Info("group execution completed",
					"name", workflow.Name, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID,
					"status", currentExec.Status, "duration", duration, "records", currentExec.TotalRecords)
				return
			}
		}
	}
}

// reportGroupExecutionResult 그룹 실행 결과 보고
func (a *Agent) reportGroupExecutionResult(result *types.GroupExecutionResult) error {
	slog.Info("reporting group execution result",
		"workflow_id", result.WorkflowID, "execution_id", result.ExecutionID, "status", result.Status)

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
			slog.Error("report result REST API failed", "error", err, "workflow_id", result.WorkflowID, "execution_id", result.ExecutionID)
		} else {
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				body, _ := io.ReadAll(resp.Body)
				slog.Error("report result REST API error", "status_code", resp.StatusCode, "body", string(body), "workflow_id", result.WorkflowID, "execution_id", result.ExecutionID)
			} else {
				slog.Info("result reported successfully via REST API", "workflow_id", result.WorkflowID, "execution_id", result.ExecutionID)
				return nil
			}
		}
	}

	// Redis로 결과 발행 (폴백)
	if a.redisClient != nil && a.redisHealthy {
		channel := fmt.Sprintf("workflow:result:%s", result.WorkflowID)
		if err := a.redisClient.Publish(a.ctx, channel, result); err != nil {
			slog.Error("failed to publish result via redis", "error", err, "workflow_id", result.WorkflowID, "execution_id", result.ExecutionID)
		} else {
			slog.Info("result published via redis", "workflow_id", result.WorkflowID, "execution_id", result.ExecutionID)
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
