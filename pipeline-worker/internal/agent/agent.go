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
	"strconv"
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
	GroupExecutor *executor.GroupExecutor // in-process 실행의 모니터링·제어용 참조. streaming 위임 실행이면 nil.

	// streaming 위임(K8s Deployment)일 때만 설정. GroupExecutor 는 pod 안에서 돌므로
	// 제어(stop/pause/resume)는 pod REST 로, 정리는 Deployment 삭제로 한다.
	StreamingDeployment string // Deployment 이름(비면 in-process 실행)
	StreamingNamespace  string
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
	ReconcileInterval   time.Duration `json:"reconcile_interval"`    // reconcile 백스톱 주기 (기본 60초, pub/sub 유실 명령 복구)

	// batch 위임 시 생성할 K8s Job 설정
	Namespace   string `json:"namespace"`    // Job 생성 네임스페이스 (비면 in-cluster 기본)
	RunnerImage string `json:"runner_image"` // 배치 실행용 pipeline-batch-job 이미지
	// 실행 파드(batch Job/streaming pod)에 envFrom 으로 붙일 Secret/ConfigMap 이름들.
	// 파이프라인 config 의 ${VAR} 해소용 값을 실행 파드에 공급한다(비면 평문 config 만 동작).
	RunnerEnvFromSecrets    []string `json:"runner_env_from_secrets"`
	RunnerEnvFromConfigMaps []string `json:"runner_env_from_configmaps"`
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

	// reconcile 백스톱 시작: pub/sub 실행 명령은 at-most-once 라 모든 agent 가 죽어있거나 구독이
	// 끊긴 창에 발행되면 유실된다. DB 의 running execution 을 주기적으로 조회해 안 도는 것을 재발견·복구한다.
	go a.reconcileLoop()

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

// matchingExecutions 는 제어 명령 대상 실행 목록을 반환한다.
// executionID 가 있고 이 agent 가 그 실행을 가지면 그것 하나. 없으면(파티션 분산의 부모 execID 등)
// workflowID 로 매칭되는 모든 로컬 실행 — 파티션 sub-execution 이 이 agent 에 여러 개일 수 있으므로
// 전부 제어해야 한다(하나만 멈추면 나머지 pod 가 계속 돈다).
func (a *Agent) matchingExecutions(executionID, workflowID string) []*RunningExecution {
	a.execMu.RLock()
	defer a.execMu.RUnlock()

	if executionID != "" {
		if exec, ok := a.runningExecs[executionID]; ok {
			return []*RunningExecution{exec}
		}
	}
	var matches []*RunningExecution
	for _, exec := range a.runningExecs {
		if exec.WorkflowID == workflowID {
			matches = append(matches, exec)
		}
	}
	return matches
}

// controlExecution 은 단일 실행에 제어 명령을 적용한다(streaming 은 pod REST/삭제, in-process 는 executor).
func (a *Agent) controlExecution(exec *RunningExecution, command string) error {
	if exec.StreamingDeployment != "" {
		if command == "stop" {
			return a.stopStreamingExecution(exec)
		}
		return a.sendStreamingCommand(exec, command)
	}
	if exec.GroupExecutor == nil {
		return fmt.Errorf("execution %s has no controllable executor", exec.ExecutionID)
	}
	switch command {
	case "stop":
		return exec.GroupExecutor.Stop()
	case "pause":
		return exec.GroupExecutor.Pause()
	case "resume":
		return exec.GroupExecutor.Resume()
	default:
		return fmt.Errorf("unknown control command: %s", command)
	}
}

// applyControl 은 매칭되는 모든 실행에 명령을 적용하고 첫 에러를 반환한다(나머지는 계속 시도).
func (a *Agent) applyControl(executionID, workflowID, command string) error {
	execs := a.matchingExecutions(executionID, workflowID)
	if len(execs) == 0 {
		return fmt.Errorf("no running execution for workflow=%s execution=%s", workflowID, executionID)
	}
	var firstErr error
	for _, exec := range execs {
		if err := a.controlExecution(exec, command); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// StopGroupExecution 워크플로우(그룹) 실행을 중지한다.
// streaming 위임이면 pod 에 REST stop 을 보내 graceful 종료(checkpoint flush)시킨 뒤 Deployment 를 삭제한다.
func (a *Agent) StopGroupExecution(executionID, workflowID string) error {
	return a.applyControl(executionID, workflowID, "stop")
}

// PauseGroupExecution 워크플로우(그룹) 실행을 일시정지한다.
func (a *Agent) PauseGroupExecution(executionID, workflowID string) error {
	return a.applyControl(executionID, workflowID, "pause")
}

// ResumeGroupExecution 워크플로우(그룹) 실행을 재개한다.
func (a *Agent) ResumeGroupExecution(executionID, workflowID string) error {
	return a.applyControl(executionID, workflowID, "resume")
}

// RollGroupExecution 은 실행 중 realtime streaming Deployment 를 새 RunnerVersion 바이너리로 rolling 한다(S8).
// streaming 위임 실행에만 유효 — in-process 실행은 바이너리 주입이 없어 rolling 대상이 아니다(skip).
func (a *Agent) RollGroupExecution(executionID, workflowID, runnerVersionID string) error {
	if runnerVersionID == "" {
		return fmt.Errorf("roll requires runner_version_id")
	}
	execs := a.matchingExecutions(executionID, workflowID)
	if len(execs) == 0 {
		return fmt.Errorf("no running execution for workflow=%s execution=%s", workflowID, executionID)
	}

	jm := a.getJobManager()
	if jm == nil {
		return fmt.Errorf("no K8s client to roll streaming deployment")
	}

	var firstErr error
	rolled := 0
	for _, exec := range execs {
		if exec.StreamingDeployment == "" {
			continue // in-process 실행은 rolling 대상 아님
		}
		if err := jm.UpdateStreamingDeployment(a.ctx, exec.StreamingNamespace, exec.StreamingDeployment, runnerVersionID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		rolled++
		slog.Info("rolled streaming execution to new runner version",
			"execution_id", exec.ExecutionID, "deployment", exec.StreamingDeployment, "runner_version_id", runnerVersionID)
	}
	if rolled == 0 && firstErr == nil {
		return fmt.Errorf("no streaming execution to roll for workflow=%s (in-process executions are not rollable)", workflowID)
	}
	return firstErr
}

// stopStreamingExecution 은 streaming pod 를 graceful 종료 후 Deployment 를 삭제하고 추적을 정리한다.
// pod REST stop 을 먼저 보내 checkpoint flush 를 유도하되, pod 가 이미 사라졌으면(교체/크래시)
// 명령 실패를 무시하고 Deployment 삭제로 진행한다 — 최종 상태는 "삭제됨"으로 수렴해야 한다.
func (a *Agent) stopStreamingExecution(exec *RunningExecution) error {
	if err := a.sendStreamingCommand(exec, "stop"); err != nil {
		slog.Warn("streaming stop command failed, proceeding to delete deployment",
			"error", err, "execution_id", exec.ExecutionID, "deployment", exec.StreamingDeployment)
	}

	jm := a.getJobManager()
	if jm == nil {
		return fmt.Errorf("cannot delete streaming deployment %s: no K8s client", exec.StreamingDeployment)
	}
	if err := jm.DeleteStreamingDeployment(a.ctx, exec.StreamingNamespace, exec.StreamingDeployment); err != nil {
		return fmt.Errorf("failed to delete streaming deployment %s: %w", exec.StreamingDeployment, err)
	}

	a.execMu.Lock()
	delete(a.runningExecs, exec.ExecutionID)
	a.execMu.Unlock()
	a.releaseClaim(exec.ExecutionID)

	slog.Info("stopped streaming execution", "execution_id", exec.ExecutionID, "deployment", exec.StreamingDeployment)
	return nil
}

// sendStreamingCommand 는 execution 의 streaming pod 를 찾아 command REST(stop/pause/resume)를 POST 한다.
func (a *Agent) sendStreamingCommand(exec *RunningExecution, command string) error {
	jm := a.getJobManager()
	if jm == nil {
		return fmt.Errorf("no K8s client to reach streaming pod for execution=%s", exec.ExecutionID)
	}
	url, err := jm.StreamingCommandURL(a.ctx, exec.StreamingNamespace, exec.ExecutionID)
	if err != nil {
		return err
	}

	body, _ := json.Marshal(map[string]string{"command": command})
	reqCtx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build command request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send %s to streaming pod: %w", command, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("streaming pod returned %d for %s: %s", resp.StatusCode, command, string(b))
	}
	return nil
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

// reconcileDefaultInterval reconcile 백스톱 기본 주기. pub/sub 유실 명령의 최대 복구 지연이 된다.
// 너무 짧으면 CP·Redis 부하, 너무 길면 유실 실행이 오래 방치된다. 60초는 stale 감지(2분)보다 짧아
// 유실을 stale 오판 전에 복구할 여지를 준다.
const reconcileDefaultInterval = 60 * time.Second

// reconcileLoop 은 부팅 직후 1회 + 주기적으로 이 cluster 의 running execution 을 CP 에서 조회해,
// pub/sub 으로 유실됐거나 agent 재부팅으로 놓친 실행을 재발견·복구한다. 중복은 SETNX claim 과
// 로컬 runningExecs 로 막으므로 이미 도는 execution 은 건드리지 않는다(안전한 반복 실행).
func (a *Agent) reconcileLoop() {
	if a.controlPlaneURL == "" || a.config.ClusterID == "" {
		return // cluster 미지정(레거시 broadcast)에서는 reconcile 대상 cluster 를 특정할 수 없음
	}

	interval := a.config.ReconcileInterval
	if interval == 0 {
		interval = reconcileDefaultInterval
	}

	// 부팅 직후 구독·claim 이 안정화된 뒤 1회 실행(재부팅으로 놓친 명령 즉시 복구).
	select {
	case <-time.After(5 * time.Second):
	case <-a.ctx.Done():
		return
	}
	a.reconcileOnce()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.reconcileOnce()
		}
	}
}

// reconcileOnce 는 CP 에서 이 cluster 의 running execution 을 조회해, 로컬에서 안 도는 것을
// handleGroupExecution 경로로 재실행한다(claim 으로 중복 방지). 조회/파싱 실패는 로그만 남기고 넘어간다.
func (a *Agent) reconcileOnce() {
	cmds, err := a.fetchRunningExecutions()
	if err != nil {
		slog.Warn("reconcile fetch failed", "error", err, "agent_id", a.ID, "cluster_id", a.config.ClusterID)
		return
	}

	for _, raw := range cmds {
		var cmd types.GroupExecutionCommand
		if err := json.Unmarshal(raw, &cmd); err != nil {
			slog.Warn("reconcile: bad command payload, skipping", "error", err, "agent_id", a.ID)
			continue
		}

		a.execMu.RLock()
		local, tracked := a.runningExecs[cmd.ExecutionID]
		a.execMu.RUnlock()

		if tracked {
			// streaming 위임은 로컬 추적이 "의도"일 뿐 — Deployment 가 외부 삭제/유실됐어도 로컬엔 남는다.
			// 실제 K8s 상태를 확인해 Deployment 가 사라졌으면 로컬 추적을 버리고 복구를 진행한다.
			// in-process 실행은 로컬 goroutine 이 곧 실체이므로 로컬 추적을 그대로 신뢰(스킵).
			if local.StreamingDeployment == "" {
				continue // in-process — 로컬이 authoritative
			}
			if jm := a.getJobManager(); jm != nil {
				exists, err := jm.StreamingDeploymentExists(a.ctx, local.StreamingNamespace, cmd.WorkflowID, cmd.ExecutionID)
				if err != nil {
					slog.Warn("reconcile: deployment existence check failed, skipping this round",
						"error", err, "execution_id", cmd.ExecutionID)
					continue
				}
				if exists {
					continue // Deployment 살아있음 — 정상
				}
			}
			// Deployment 유실 확인 → 로컬 추적·claim 을 정리해 아래 재실행이 새로 claim·생성하게 한다.
			slog.Warn("reconcile: streaming deployment missing, will recover",
				"execution_id", cmd.ExecutionID, "workflow_id", cmd.WorkflowID)
			a.execMu.Lock()
			delete(a.runningExecs, cmd.ExecutionID)
			a.execMu.Unlock()
			a.releaseClaim(cmd.ExecutionID)
		}

		slog.Info("reconcile: recovering execution missed by pub/sub or lost",
			"execution_id", cmd.ExecutionID, "workflow_id", cmd.WorkflowID, "agent_id", a.ID)
		// 원본 JSON 그대로 handleGroupExecution 에 넘긴다(claim·타입분기·partition 배선 전부 재사용).
		a.handleGroupExecution(string(raw))
	}
}

// fetchRunningExecutions 는 CP reconcile API 에서 이 cluster 의 running execution 명령 목록을 받는다.
func (a *Agent) fetchRunningExecutions() ([]json.RawMessage, error) {
	url := fmt.Sprintf("%s/api/v1/clusters/%s/running-executions", a.controlPlaneURL, a.config.ClusterID)
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Success bool              `json:"success"`
		Data    []json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return response.Data, nil
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
	case types.CommandRollWorkflow:
		if err := a.RollGroupExecution(cmd.ExecutionID, cmd.WorkflowID, cmd.RunnerVersionID); err != nil {
			slog.Error("failed to roll workflow", "error", err, "workflow_id", cmd.WorkflowID, "execution_id", cmd.ExecutionID)
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

	// 부하 분산 배정: control-plane 이 이 sub 를 특정 agent 에 "선호" 지정했으면(PreferredAgentID),
	// 지정 agent 가 아닌 나는 claim 을 짧게 지연해 지정 agent 에게 우선권을 준다.
	// 지정 agent 가 죽었으면 이 지연 후 내가 claim 하게 되어 실행이 진행된다(SETNX 안전망 유지).
	// PreferredAgentID 가 비면(broadcast 기본) 지연 0 → 현행 즉시 경쟁과 동일.
	if cmd.PreferredAgentID != "" && cmd.PreferredAgentID != a.ID {
		select {
		case <-time.After(preferredClaimBackoff()):
		case <-a.ctx.Done():
			return
		}
	}

	// 실행 소유권 claim: 한 클러스터의 여러 에이전트가 같은 명령을 수신하므로,
	// Redis SETNX로 한 execution을 정확히 한 에이전트만 실행하도록 보장한다(중복 실행 방지).
	if !a.claimExecution(cmd.ExecutionID) {
		slog.Info("execution already claimed by another agent, skipping", "execution_id", cmd.ExecutionID, "workflow_id", cmd.WorkflowID, "agent_id", a.ID)
		return
	}

	// batch 워크플로우는 이 cluster에 K8s Job으로 위임 생성(일회성 격리 실행).
	if cmd.WorkflowConfig.Type == types.WorkflowTypeBatch {
		go a.delegateBatchJob(&cmd)
		return
	}

	// realtime + native stage(RunnerVersionID 有)는 최신 stage 바이너리를 주입한 streaming pod
	// (K8s Deployment, 상주 실행)로 위임한다. in-process 로는 이미 켜진 agent 에 컴파일된 stage 만
	// 쓸 수 있어 web-ui 로 새로 만든 native stage 를 반영하지 못하기 때문이다.
	// RunnerVersionID 가 비면(native 미사용) in-process 상주 실행을 유지한다(비-K8s standalone 포함).
	if cmd.RunnerVersionID != "" {
		go a.delegateStreamingDeployment(&cmd)
		return
	}

	// realtime(native 아님)은 worker 프로세스 내에서 직접 상주 실행.
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
		ExecutionID:        cmd.ExecutionID,
		WorkflowID:         cmd.WorkflowID,
		AgentID:            a.ID, // 위임 agent 기록 — batch-job 이 결과 콜백에 담아 분산 현황 노출
		PipelinesConfig:    string(pipelinesJSON),
		JobConfig:          jobConfig,
		AssignedPartitions: cmd.AssignedPartitions, // 파티션 분산: sub-execution 이면 배정 파티션만
		RunnerVersionID:    cmd.RunnerVersionID,    // native stage 면 CP 바이너리를 initContainer 로 주입
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

// delegateStreamingDeployment 는 native stage 를 쓰는 realtime 워크플로우를 이 cluster 의 K8s
// Deployment(streaming pod, 상주 실행)로 위임 생성한다. batch(delegateBatchJob)와 같은 위임 구조지만
// Job 이 아닌 Deployment(RestartPolicy=Always)로 무한 실행하며, 최신 stage 바이너리를 initContainer 로
// 주입해 web-ui 로 만든 native stage 를 반영한다. stop/pause/resume 은 pod REST 로 전달한다(W4).
func (a *Agent) delegateStreamingDeployment(cmd *types.GroupExecutionCommand) {
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
			ErrorMessage: "realtime native delegation requires a Kubernetes cluster, but no K8s client is available on this worker",
		})
		a.releaseClaim(cmd.ExecutionID)
		return
	}

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
		a.releaseClaim(cmd.ExecutionID)
		return
	}

	dep, err := jm.CreateStreamingDeployment(a.ctx, &k8s.StreamingSpec{
		ExecutionID:        cmd.ExecutionID,
		WorkflowID:         cmd.WorkflowID,
		AgentID:            a.ID,
		PipelinesConfig:    string(pipelinesJSON),
		JobConfig:          jobConfig,
		AssignedPartitions: cmd.AssignedPartitions,
		RunnerVersionID:    cmd.RunnerVersionID,
	})
	if err != nil {
		completedAt := time.Now()
		_ = a.reportGroupExecutionResult(&types.GroupExecutionResult{
			ExecutionID:  cmd.ExecutionID,
			WorkflowID:   cmd.WorkflowID,
			Status:       types.PipelineGroupStatusError,
			StartedAt:    startTime,
			CompletedAt:  &completedAt,
			ErrorMessage: fmt.Sprintf("failed to create streaming deployment: %v", err),
		})
		a.releaseClaim(cmd.ExecutionID)
		return
	}

	// 제어(stop/pause/resume)·정리(Deployment 삭제)를 위해 위임 실행을 추적한다.
	// GroupExecutor 는 pod 안에서 돌므로 nil — 제어는 StreamingDeployment 로 라우팅한다(W4).
	a.execMu.Lock()
	a.runningExecs[cmd.ExecutionID] = &RunningExecution{
		ExecutionID:         cmd.ExecutionID,
		WorkflowID:          cmd.WorkflowID,
		StartedAt:           startTime,
		StreamingDeployment: dep.Name,
		StreamingNamespace:  dep.Namespace,
	}
	a.execMu.Unlock()

	slog.Info("delegated streaming deployment", "deployment", dep.Name, "namespace", dep.Namespace, "execution_id", cmd.ExecutionID, "workflow_id", cmd.WorkflowID)
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
	a.jobManager = k8s.NewJobManager(client, a.controlPlaneURL, a.config.RunnerImage,
		a.config.RunnerEnvFromSecrets, a.config.RunnerEnvFromConfigMaps)
	return a.jobManager
}

// claim TTL/갱신 주기. CDC 같은 장기 실행은 실행 시간이 TTL 을 넘으므로 반드시 주기 갱신이
// 필요하다(갱신 없이 만료되면 다른 에이전트가 같은 소스를 잡아 중복 실행 — binlog/slot 이중 소비).
// 갱신 주기는 TTL 의 1/3 로 두 번 놓쳐도 만료 전 복구 여지를 남긴다.
const (
	claimTTL           = 30 * time.Second
	claimRenewInterval = claimTTL / 3
)

// preferredClaimBackoffDefault 는 부하 분산 배정 시, 선호 agent 가 아닌 노드가 claim 을 미루는
// 기본 시간이다. 짧으면 선호 agent 의 지터/pub-sub 전달 지연으로 배정이 퇴화(broadcast 화)하고,
// 길면 선호 agent 사망 시 그만큼 실행이 지연된다. claim TTL(30s)과 무관 — 배정 편향과 사망 회복
// 지연의 하한. 환경별 pub/sub 전달 지연에 맞춰 AGENT_PREFERRED_BACKOFF_MS 로 조정한다.
const preferredClaimBackoffDefault = 300 * time.Millisecond

// preferredClaimBackoff 는 env(AGENT_PREFERRED_BACKOFF_MS)로 조정 가능한 백오프 값을 반환한다.
func preferredClaimBackoff() time.Duration {
	if v := os.Getenv("AGENT_PREFERRED_BACKOFF_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return preferredClaimBackoffDefault
}

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

	// GroupExecutor를 사용하여 파이프라인 그룹 실행.
	// 파티션 분산: cmd.AssignedPartitions 가 있으면 이 sub-execution 은 배정된 파티션만 실행한다
	// (없으면 전체 — 현행 단일 실행). control-plane 분할기가 sub 별로 배정한다.
	opts := []executor.GroupExecutorOption{executor.WithLinkClient(linkClient)}
	if len(cmd.AssignedPartitions) > 0 {
		opts = append(opts, executor.WithAssignedPartitions(cmd.AssignedPartitions))
	}
	groupExecutor := executor.NewGroupExecutor(workflow, opts...)

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
	// 실행한 노드 식별: 분산 현황(어느 agent 가 어떤 sub-execution 을 처리했는지) 모니터링용.
	// 결과 생성 지점(정상 완료/panic 복구)마다 채우지 않고 보고 진입부에서 일괄 설정한다.
	result.AgentID = a.ID

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
