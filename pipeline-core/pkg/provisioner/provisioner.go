// Package provisioner 저장소 사전작업(Provisioning) 관리
package provisioner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/shared/types"
)

// Provisioner 저장소 사전작업 인터페이스
type Provisioner interface {
	// SinkType 지원하는 저장소 타입
	SinkType() types.SinkType

	// SupportedTypes 지원하는 사전작업 유형들
	SupportedTypes() []types.ProvisioningType

	// Provision 사전작업 수행
	Provision(ctx context.Context, req *types.ProvisioningRequest) (*types.ProvisioningResult, error)

	// Validate 설정 유효성 검증
	Validate(config map[string]any) error

	// RequiresExternalSetup 외부 설정이 필요한지 여부
	RequiresExternalSetup() bool

	// GetExternalSetupURL 외부 설정 페이지 URL 반환
	GetExternalSetupURL(req *types.ProvisioningRequest) string
}

// Registry Provisioner 레지스트리
type Registry struct {
	mu           sync.RWMutex
	provisioners map[types.SinkType]Provisioner
}

var (
	globalRegistry *Registry
	once           sync.Once
)

// Global 글로벌 레지스트리 반환
func Global() *Registry {
	once.Do(func() {
		globalRegistry = NewRegistry()
		// Noop provisioner들 (사전작업 불필요)
		globalRegistry.Register(&NoopProvisioner{sinkType: types.SinkTypeStub})
		globalRegistry.Register(&NoopProvisioner{sinkType: types.SinkTypeFile})
		globalRegistry.Register(&NoopProvisioner{sinkType: types.SinkTypeStdout})

		// 자동 생성 Provisioner들
		globalRegistry.Register(NewKafkaProvisioner())
		globalRegistry.Register(NewSQLProvisioner())
		globalRegistry.Register(NewMongoDBProvisioner())
		globalRegistry.Register(NewElasticsearchProvisioner())
		globalRegistry.Register(NewRestAPIProvisioner())

		// 외부 프로비저닝이 필요한 저장소들
		globalRegistry.Register(&ExternalProvisioner{sinkType: types.SinkTypeHBase})
		globalRegistry.Register(&ExternalProvisioner{sinkType: types.SinkTypeHDFS})
		globalRegistry.Register(&ExternalProvisioner{sinkType: types.SinkTypeS3})
	})
	return globalRegistry
}

// NewRegistry 새 레지스트리 생성
func NewRegistry() *Registry {
	return &Registry{
		provisioners: make(map[types.SinkType]Provisioner),
	}
}

// Register Provisioner 등록
func (r *Registry) Register(p Provisioner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provisioners[p.SinkType()] = p
}

// Get Provisioner 조회
func (r *Registry) Get(sinkType types.SinkType) (Provisioner, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.provisioners[sinkType]
	return p, ok
}

// List 등록된 모든 Provisioner 목록
func (r *Registry) List() []types.SinkType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]types.SinkType, 0, len(r.provisioners))
	for st := range r.provisioners {
		result = append(result, st)
	}
	return result
}

// NoopProvisioner 사전작업이 필요 없는 저장소용
type NoopProvisioner struct {
	sinkType types.SinkType
}

func (p *NoopProvisioner) SinkType() types.SinkType {
	return p.sinkType
}

func (p *NoopProvisioner) SupportedTypes() []types.ProvisioningType {
	return []types.ProvisioningType{types.ProvisioningTypeNone}
}

func (p *NoopProvisioner) Provision(ctx context.Context, req *types.ProvisioningRequest) (*types.ProvisioningResult, error) {
	now := time.Now()
	return &types.ProvisioningResult{
		ID:          uuid.New().String(),
		RequestID:   req.ID,
		PipelineID:  req.PipelineID,
		SinkType:    req.SinkType,
		Status:      types.ProvisioningStatusSkipped,
		Message:     "No provisioning required for this sink type",
		CompletedAt: &now,
	}, nil
}

func (p *NoopProvisioner) Validate(config map[string]any) error {
	return nil
}

func (p *NoopProvisioner) RequiresExternalSetup() bool {
	return false
}

func (p *NoopProvisioner) GetExternalSetupURL(req *types.ProvisioningRequest) string {
	return ""
}

// ExternalProvisioner 외부 서비스에서 사전작업을 수행하는 Provisioner
type ExternalProvisioner struct {
	sinkType        types.SinkType
	externalBaseURL string // 외부 설정 페이지 기본 URL
}

// NewExternalProvisioner 외부 Provisioner 생성
func NewExternalProvisioner(sinkType types.SinkType, baseURL string) *ExternalProvisioner {
	return &ExternalProvisioner{
		sinkType:        sinkType,
		externalBaseURL: baseURL,
	}
}

func (p *ExternalProvisioner) SinkType() types.SinkType {
	return p.sinkType
}

func (p *ExternalProvisioner) SupportedTypes() []types.ProvisioningType {
	return []types.ProvisioningType{types.ProvisioningTypeExternal}
}

func (p *ExternalProvisioner) Provision(ctx context.Context, req *types.ProvisioningRequest) (*types.ProvisioningResult, error) {
	// 외부 Provisioner는 직접 프로비저닝하지 않고,
	// 외부 페이지에서 작업 후 콜백으로 결과를 받음
	return &types.ProvisioningResult{
		ID:         uuid.New().String(),
		RequestID:  req.ID,
		PipelineID: req.PipelineID,
		SinkType:   req.SinkType,
		Status:     types.ProvisioningStatusPending,
		Message:    fmt.Sprintf("Waiting for external provisioning. Redirect to: %s", p.GetExternalSetupURL(req)),
		Metadata: map[string]any{
			"external_url": p.GetExternalSetupURL(req),
			"callback_url": req.CallbackURL,
		},
	}, nil
}

func (p *ExternalProvisioner) Validate(config map[string]any) error {
	// 외부에서 설정하므로 기본 검증만
	return nil
}

func (p *ExternalProvisioner) RequiresExternalSetup() bool {
	return true
}

func (p *ExternalProvisioner) GetExternalSetupURL(req *types.ProvisioningRequest) string {
	if req.ExternalURL != "" {
		return req.ExternalURL
	}
	if p.externalBaseURL != "" {
		return fmt.Sprintf("%s?pipeline_id=%s&sink=%s&callback=%s",
			p.externalBaseURL, req.PipelineID, req.SinkName, req.CallbackURL)
	}
	return ""
}

// Manager Provisioning 관리자
type Manager struct {
	registry       *Registry
	pendingResults map[string]*types.ProvisioningResult // RequestID -> Result
	mu             sync.RWMutex
}

// NewManager Provisioning Manager 생성
func NewManager() *Manager {
	return &Manager{
		registry:       Global(),
		pendingResults: make(map[string]*types.ProvisioningResult),
	}
}

// StartProvisioning 사전작업 시작
func (m *Manager) StartProvisioning(ctx context.Context, req *types.ProvisioningRequest) (*types.ProvisioningResult, error) {
	provisioner, ok := m.registry.Get(req.SinkType)
	if !ok {
		return nil, fmt.Errorf("no provisioner found for sink type: %s", req.SinkType)
	}

	// 설정 검증
	if err := provisioner.Validate(req.Config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 사전작업 수행
	result, err := provisioner.Provision(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provisioning failed: %w", err)
	}

	// Pending 상태인 경우 저장
	if result.Status == types.ProvisioningStatusPending {
		m.mu.Lock()
		m.pendingResults[req.ID] = result
		m.mu.Unlock()
	}

	return result, nil
}

// CompleteProvisioning 외부에서 사전작업 완료 시 호출
func (m *Manager) CompleteProvisioning(requestID string, result *types.ProvisioningResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pending, ok := m.pendingResults[requestID]
	if !ok {
		return fmt.Errorf("no pending provisioning found for request: %s", requestID)
	}

	// 결과 업데이트
	now := time.Now()
	pending.Status = result.Status
	pending.TableName = result.TableName
	pending.TopicName = result.TopicName
	pending.IndexName = result.IndexName
	pending.BucketName = result.BucketName
	pending.FilePath = result.FilePath
	pending.APIEndpoint = result.APIEndpoint
	pending.APIKey = result.APIKey
	pending.Metadata = result.Metadata
	pending.Message = result.Message
	pending.ErrorDetail = result.ErrorDetail
	pending.CompletedAt = &now
	pending.CompletedBy = result.CompletedBy

	// 완료된 경우 pending에서 제거
	if result.Status == types.ProvisioningStatusCompleted || result.Status == types.ProvisioningStatusFailed {
		delete(m.pendingResults, requestID)
	}

	return nil
}

// GetPendingResult Pending 결과 조회
func (m *Manager) GetPendingResult(requestID string) (*types.ProvisioningResult, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result, ok := m.pendingResults[requestID]
	return result, ok
}

// GetRequirements 저장소 요구사항 조회
func (m *Manager) GetRequirements(sinkType types.SinkType) (*types.SinkRequirement, bool) {
	requirements := types.GetSinkRequirements()
	for _, req := range requirements {
		if req.Type == sinkType {
			return &req, true
		}
	}
	return nil, false
}

// GetAllRequirements 모든 저장소 요구사항 조회
func (m *Manager) GetAllRequirements() []types.SinkRequirement {
	return types.GetSinkRequirements()
}
