package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	redisclient "github.com/conduix/conduix/shared/redis"
	"github.com/conduix/conduix/shared/types"
)

// RedisService Redis 기반 서비스
type RedisService struct {
	client          *redisclient.ResilientClient
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex
	isHealthy       bool
	pendingCommands map[string]*PendingCommand // 전송 대기 명령 (Redis 장애 시)
	commandMu       sync.Mutex
}

// PendingCommand 전송 대기 중인 명령
type PendingCommand struct {
	AgentID   string
	Command   types.AgentCommand
	CreatedAt time.Time
	Retries   int
}

// RedisServiceConfig Redis 서비스 설정
type RedisServiceConfig struct {
	Addr             string
	Password         string
	DB               int
	OnStateChange    func(old, new redisclient.ConnectionState)
	EnableRetryQueue bool
}

// NewRedisService Redis 서비스 생성
func NewRedisService(cfg *RedisServiceConfig) (*RedisService, error) {
	ctx, cancel := context.WithCancel(context.Background())

	svc := &RedisService{
		ctx:             ctx,
		cancel:          cancel,
		isHealthy:       false,
		pendingCommands: make(map[string]*PendingCommand),
	}

	// Redis 클라이언트 설정
	redisConfig := redisclient.DefaultConfig(cfg.Addr)
	redisConfig.Password = cfg.Password
	redisConfig.DB = cfg.DB
	redisConfig.OnStateChange = func(old, new redisclient.ConnectionState) {
		svc.mu.Lock()
		svc.isHealthy = (new == redisclient.StateConnected)
		svc.mu.Unlock()

		fmt.Printf("[RedisService] Connection state: %s -> %s\n", old, new)

		// 연결 복구 시 대기 중인 명령 재전송
		if new == redisclient.StateConnected && cfg.EnableRetryQueue {
			go svc.retryPendingCommands()
		}

		if cfg.OnStateChange != nil {
			cfg.OnStateChange(old, new)
		}
	}
	redisConfig.OnError = func(err error) {
		fmt.Printf("[RedisService] Error: %v\n", err)
	}

	var err error
	svc.client, err = redisclient.NewResilientClient(redisConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create redis client: %w", err)
	}

	return svc, nil
}

// IsHealthy Redis 연결 상태 확인
func (s *RedisService) IsHealthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isHealthy
}

// GetClient Redis 클라이언트 반환
func (s *RedisService) GetClient() *redisclient.ResilientClient {
	return s.client
}

// SendCommandToAgent 에이전트에 명령 전송
func (s *RedisService) SendCommandToAgent(agentID string, cmdType types.CommandType, pipelineID string, payload any) error {
	cmd := types.AgentCommand{
		ID:         uuid.New().String(),
		Type:       cmdType,
		PipelineID: pipelineID,
		Payload:    payload,
		Timestamp:  time.Now(),
	}

	channel := fmt.Sprintf("agent:commands:%s", agentID)

	// Redis 전송 시도
	err := s.client.Publish(s.ctx, channel, cmd)
	if err != nil {
		fmt.Printf("[RedisService] Failed to publish command to %s: %v\n", agentID, err)

		// 대기 큐에 추가
		s.queuePendingCommand(agentID, cmd)
		return fmt.Errorf("command queued for retry: %w", err)
	}

	fmt.Printf("[RedisService] Command sent to agent %s: %s\n", agentID, cmdType)
	return nil
}

// queuePendingCommand 대기 큐에 명령 추가
func (s *RedisService) queuePendingCommand(agentID string, cmd types.AgentCommand) {
	s.commandMu.Lock()
	defer s.commandMu.Unlock()

	s.pendingCommands[cmd.ID] = &PendingCommand{
		AgentID:   agentID,
		Command:   cmd,
		CreatedAt: time.Now(),
		Retries:   0,
	}
}

// retryPendingCommands 대기 중인 명령 재전송
func (s *RedisService) retryPendingCommands() {
	s.commandMu.Lock()
	commands := make([]*PendingCommand, 0, len(s.pendingCommands))
	for _, cmd := range s.pendingCommands {
		commands = append(commands, cmd)
	}
	s.commandMu.Unlock()

	for _, pending := range commands {
		// 24시간 이상 지난 명령은 삭제
		if time.Since(pending.CreatedAt) > 24*time.Hour {
			s.commandMu.Lock()
			delete(s.pendingCommands, pending.Command.ID)
			s.commandMu.Unlock()
			continue
		}

		channel := fmt.Sprintf("agent:commands:%s", pending.AgentID)
		err := s.client.Publish(s.ctx, channel, pending.Command)
		if err != nil {
			pending.Retries++
			continue
		}

		// 전송 성공 - 큐에서 제거
		s.commandMu.Lock()
		delete(s.pendingCommands, pending.Command.ID)
		s.commandMu.Unlock()

		fmt.Printf("[RedisService] Retried command sent to agent %s: %s\n", pending.AgentID, pending.Command.Type)
	}
}

// PublishWorkflowExecution 워크플로우 실행 명령 발행
func (s *RedisService) PublishWorkflowExecution(cmd *types.WorkflowExecutionCommand) error {
	channel := "workflow:execute:broadcast"

	err := s.client.Publish(s.ctx, channel, cmd)
	if err != nil {
		fmt.Printf("[RedisService] Failed to publish workflow execution command: %v\n", err)
		return fmt.Errorf("failed to publish workflow execution: %w", err)
	}

	fmt.Printf("[RedisService] Workflow execution command published for workflow %s (execution: %s)\n", cmd.WorkflowID, cmd.ExecutionID)
	return nil
}

// PublishGroupExecution is deprecated, use PublishWorkflowExecution instead
// Deprecated: Use PublishWorkflowExecution instead
func (s *RedisService) PublishGroupExecution(cmd *types.GroupExecutionCommand) error {
	return s.PublishWorkflowExecution(cmd)
}

// GetAgentHeartbeat 에이전트 하트비트 조회
func (s *RedisService) GetAgentHeartbeat(agentID string) (*types.AgentHeartbeat, error) {
	key := fmt.Sprintf("agent:%s:heartbeat", agentID)

	data, err := s.client.Get(s.ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get heartbeat: %w", err)
	}

	var heartbeat types.AgentHeartbeat
	if err := json.Unmarshal([]byte(data), &heartbeat); err != nil {
		return nil, fmt.Errorf("failed to unmarshal heartbeat: %w", err)
	}

	return &heartbeat, nil
}

// GetAllAgentHeartbeats 모든 에이전트 하트비트 조회
func (s *RedisService) GetAllAgentHeartbeats() (map[string]*types.AgentHeartbeat, error) {
	// 실제 구현에서는 Redis SCAN을 사용해야 함
	// 여기서는 간단한 구현
	return nil, fmt.Errorf("not implemented - use database query with last_heartbeat")
}

// SetPipelineCheckpoint 파이프라인 체크포인트 저장
func (s *RedisService) SetPipelineCheckpoint(pipelineID string, checkpoint *types.Checkpoint) error {
	key := fmt.Sprintf("pipeline:%s:checkpoint", pipelineID)
	return s.client.Set(s.ctx, key, checkpoint, 0) // TTL 없음
}

// GetPipelineCheckpoint 파이프라인 체크포인트 조회
func (s *RedisService) GetPipelineCheckpoint(pipelineID string) (*types.Checkpoint, error) {
	key := fmt.Sprintf("pipeline:%s:checkpoint", pipelineID)

	data, err := s.client.Get(s.ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint: %w", err)
	}

	var checkpoint types.Checkpoint
	if err := json.Unmarshal([]byte(data), &checkpoint); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}

	return &checkpoint, nil
}

// SetPipelineMetrics 파이프라인 메트릭 저장
func (s *RedisService) SetPipelineMetrics(pipelineID string, metrics *types.PipelineMetrics) error {
	key := fmt.Sprintf("pipeline:%s:metrics", pipelineID)
	return s.client.Set(s.ctx, key, metrics, 5*time.Minute) // 5분 TTL
}

// GetPipelineMetrics 파이프라인 메트릭 조회
func (s *RedisService) GetPipelineMetrics(pipelineID string) (*types.PipelineMetrics, error) {
	key := fmt.Sprintf("pipeline:%s:metrics", pipelineID)

	data, err := s.client.Get(s.ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}

	var metrics types.PipelineMetrics
	if err := json.Unmarshal([]byte(data), &metrics); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
	}

	return &metrics, nil
}

// GetMetrics Redis 서비스 메트릭 조회
func (s *RedisService) GetMetrics() redisclient.Metrics {
	return s.client.GetMetrics()
}

// GetPendingCommandCount 대기 중인 명령 수 조회
func (s *RedisService) GetPendingCommandCount() int {
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	return len(s.pendingCommands)
}

// Close 서비스 종료
func (s *RedisService) Close() error {
	s.cancel()
	return s.client.Close()
}

// Set 일반 키-값 저장
func (s *RedisService) Set(ctx context.Context, key, value string, expiration time.Duration) error {
	return s.client.Set(ctx, key, value, expiration)
}

// Get 일반 키-값 조회
func (s *RedisService) Get(ctx context.Context, key string) (string, error) {
	return s.client.Get(ctx, key)
}
