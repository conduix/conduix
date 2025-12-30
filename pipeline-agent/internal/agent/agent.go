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

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/pipeline"
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

// Agent 파이프라인 에이전트
type Agent struct {
	ID              string
	Hostname        string
	Status          types.AgentStatus
	config          *Config
	pipelines       map[string]*PipelineInstance
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	redisClient     *redisclient.ResilientClient
	httpClient      *http.Client
	controlPlaneURL string
	commMode        CommunicationMode
	redisHealthy    bool
	healthMu        sync.RWMutex
}

// Config 에이전트 설정
type Config struct {
	ID                string        `json:"id"`
	ControlPlaneURL   string        `json:"control_plane_url"`
	RedisHost         string        `json:"redis_host"`
	RedisPort         int           `json:"redis_port"`
	RedisPassword     string        `json:"redis_password"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	Labels            []string      `json:"labels"`
	// 새로운 설정
	CommandPollInterval time.Duration `json:"command_poll_interval"` // REST 폴링 간격
	EnableRESTFallback  bool          `json:"enable_rest_fallback"`  // REST 폴백 활성화
}

// PipelineInstance 파이프라인 인스턴스
type PipelineInstance struct {
	ID        string
	Config    *config.PipelineConfig
	Runner    *pipeline.Runner
	Status    types.PipelineStatus
	StartTime time.Time
	StopTime  time.Time
}

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
		pipelines:       make(map[string]*PipelineInstance),
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

	// 하트비트 시작
	go a.heartbeatLoop()

	// 명령 수신 시작
	go a.commandLoop()

	fmt.Printf("Agent started: %s (%s)\n", a.ID, a.Hostname)
	return nil
}

// Stop 에이전트 중지
func (a *Agent) Stop() error {
	a.cancel()

	// 모든 파이프라인 중지
	a.mu.Lock()
	for id, instance := range a.pipelines {
		if instance.Runner != nil {
			_ = instance.Runner.Stop()
		}
		delete(a.pipelines, id)
	}
	a.Status = types.AgentStatusOffline
	a.mu.Unlock()

	fmt.Printf("Agent stopped: %s\n", a.ID)
	return nil
}

// StartPipeline 파이프라인 시작
func (a *Agent) StartPipeline(pipelineID string, cfg *config.PipelineConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.pipelines[pipelineID]; exists {
		return fmt.Errorf("pipeline %s is already running", pipelineID)
	}

	runner, err := pipeline.NewRunner(cfg)
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	if err := runner.Start(); err != nil {
		return fmt.Errorf("failed to start pipeline: %w", err)
	}

	a.pipelines[pipelineID] = &PipelineInstance{
		ID:        pipelineID,
		Config:    cfg,
		Runner:    runner,
		Status:    types.PipelineStatusRunning,
		StartTime: time.Now(),
	}

	fmt.Printf("Pipeline started: %s\n", pipelineID)
	return nil
}

// StopPipeline 파이프라인 중지
func (a *Agent) StopPipeline(pipelineID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	instance, exists := a.pipelines[pipelineID]
	if !exists {
		return fmt.Errorf("pipeline %s not found", pipelineID)
	}

	if instance.Runner != nil {
		if err := instance.Runner.Stop(); err != nil {
			return fmt.Errorf("failed to stop pipeline: %w", err)
		}
	}

	instance.Status = types.PipelineStatusStopped
	instance.StopTime = time.Now()
	delete(a.pipelines, pipelineID)

	fmt.Printf("Pipeline stopped: %s\n", pipelineID)
	return nil
}

// PausePipeline 파이프라인 일시중지
func (a *Agent) PausePipeline(pipelineID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	instance, exists := a.pipelines[pipelineID]
	if !exists {
		return fmt.Errorf("pipeline %s not found", pipelineID)
	}

	if instance.Runner != nil {
		if err := instance.Runner.Pause(); err != nil {
			return fmt.Errorf("failed to pause pipeline: %w", err)
		}
	}

	instance.Status = types.PipelineStatusPaused
	return nil
}

// ResumePipeline 파이프라인 재개
func (a *Agent) ResumePipeline(pipelineID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	instance, exists := a.pipelines[pipelineID]
	if !exists {
		return fmt.Errorf("pipeline %s not found", pipelineID)
	}

	if instance.Runner != nil {
		if err := instance.Runner.Resume(); err != nil {
			return fmt.Errorf("failed to resume pipeline: %w", err)
		}
	}

	instance.Status = types.PipelineStatusRunning
	return nil
}

// GetPipelineStatus 파이프라인 상태 조회
func (a *Agent) GetPipelineStatus(pipelineID string) (*PipelineInstance, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	instance, exists := a.pipelines[pipelineID]
	if !exists {
		return nil, fmt.Errorf("pipeline %s not found", pipelineID)
	}

	return instance, nil
}

// ListPipelines 파이프라인 목록 조회
func (a *Agent) ListPipelines() []*PipelineInstance {
	a.mu.RLock()
	defer a.mu.RUnlock()

	pipelines := make([]*PipelineInstance, 0, len(a.pipelines))
	for _, instance := range a.pipelines {
		pipelines = append(pipelines, instance)
	}

	return pipelines
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
	a.mu.RLock()
	pipelineIDs := make([]string, 0, len(a.pipelines))
	pipelineStats := make([]types.PipelineStatShort, 0, len(a.pipelines))
	for id, instance := range a.pipelines {
		pipelineIDs = append(pipelineIDs, id)
		pipelineStats = append(pipelineStats, types.PipelineStatShort{
			PipelineID: id,
			Status:     instance.Status,
		})
	}
	a.mu.RUnlock()

	heartbeat := types.AgentHeartbeat{
		AgentID:       a.ID,
		Timestamp:     time.Now(),
		Pipelines:     pipelineIDs,
		PipelineStats: pipelineStats,
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

		// 그룹 실행 브로드캐스트 채널
		groupChannel := "group:execute:broadcast"
		err = a.redisClient.Subscribe(a.ctx, groupChannel, a.handleGroupExecution)
		if err != nil {
			fmt.Printf("Failed to subscribe to group execution channel: %v\n", err)
		} else {
			fmt.Printf("Subscribed to Redis channel: %s\n", groupChannel)
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
	case types.CommandStartPipeline:
		if cfg, ok := cmd.Payload.(*config.PipelineConfig); ok {
			if err := a.StartPipeline(cmd.PipelineID, cfg); err != nil {
				fmt.Printf("Failed to start pipeline: %v\n", err)
			}
		}
	case types.CommandStopPipeline:
		if err := a.StopPipeline(cmd.PipelineID); err != nil {
			fmt.Printf("Failed to stop pipeline: %v\n", err)
		}
	case types.CommandPausePipeline:
		if err := a.PausePipeline(cmd.PipelineID); err != nil {
			fmt.Printf("Failed to pause pipeline: %v\n", err)
		}
	case types.CommandResumePipeline:
		if err := a.ResumePipeline(cmd.PipelineID); err != nil {
			fmt.Printf("Failed to resume pipeline: %v\n", err)
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

	// 그룹 실행 시작 (비동기)
	go a.executeGroup(&cmd)
}

// executeGroup 파이프라인 그룹 실행
func (a *Agent) executeGroup(cmd *types.GroupExecutionCommand) {
	startTime := time.Now()
	workflow := cmd.WorkflowConfig

	fmt.Printf("Starting group execution: %s (%s)\n", workflow.Name, workflow.ID)

	// 결과 수집
	var results []types.PipelineExecutionResult
	var totalRecords, failedRecords int64
	var hasError bool
	var errorMessage string

	// 실행 모드에 따라 파이프라인 실행
	// 현재는 순차 실행만 지원 (TODO: parallel, dag 모드 지원)
	for _, pipeline := range workflow.Pipelines {
		pipelineStart := time.Now()
		fmt.Printf("  Executing pipeline: %s (%s)\n", pipeline.Name, pipeline.ID)

		// TODO: 실제 파이프라인 실행 로직 구현
		// 현재는 placeholder로 성공 처리
		result := types.PipelineExecutionResult{
			PipelineID:     pipeline.ID,
			PipelineName:   pipeline.Name,
			Status:         "completed",
			StartedAt:      pipelineStart,
			RecordsRead:    0,
			RecordsWritten: 0,
			ErrorCount:     0,
		}
		completedAt := time.Now()
		result.CompletedAt = completedAt

		results = append(results, result)
		totalRecords += result.RecordsWritten
	}

	// 완료 시간
	completedAt := time.Now()
	duration := completedAt.Sub(startTime)

	// 결과 상태 결정
	status := types.PipelineGroupStatusCompleted
	if hasError {
		status = types.PipelineGroupStatusError
	}

	// 결과 보고
	executionResult := &types.GroupExecutionResult{
		ExecutionID:     cmd.ExecutionID,
		WorkflowID:      cmd.WorkflowID,
		Status:          status,
		PipelineResults: results,
		TotalRecords:    totalRecords,
		FailedRecords:   failedRecords,
		StartedAt:       startTime,
		CompletedAt:     &completedAt,
		ErrorMessage:    errorMessage,
	}

	// Control Plane에 결과 보고
	if err := a.reportGroupExecutionResult(executionResult); err != nil {
		fmt.Printf("Failed to report group execution result: %v\n", err)
	}

	fmt.Printf("Group execution completed: %s (duration: %v)\n", workflow.Name, duration)
}

// reportGroupExecutionResult 그룹 실행 결과 보고
func (a *Agent) reportGroupExecutionResult(result *types.GroupExecutionResult) error {
	// Redis로 결과 발행
	if a.redisClient != nil && a.redisHealthy {
		channel := fmt.Sprintf("workflow:result:%s", result.WorkflowID)
		if err := a.redisClient.Publish(a.ctx, channel, result); err != nil {
			fmt.Printf("Failed to publish result via Redis: %v\n", err)
		} else {
			return nil
		}
	}

	// REST API로 결과 보고 (폴백)
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
			return fmt.Errorf("failed to send result: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("server returned error: %s", string(body))
		}
	}

	return nil
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
