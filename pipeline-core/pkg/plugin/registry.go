package plugin

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Registry 빌트인 + 플러그인 Stage 통합 레지스트리
type Registry struct {
	factories map[string]StageFactory
	metadata  map[string]*StageMetadata
	mu        sync.RWMutex
}

// NewRegistry 빈 레지스트리 생성
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]StageFactory),
		metadata:  make(map[string]*StageMetadata),
	}
}

// Register Stage 등록
func (r *Registry) Register(meta *StageMetadata, factory StageFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[meta.Type]; exists {
		return fmt.Errorf("stage type %q already registered", meta.Type)
	}

	r.factories[meta.Type] = factory
	r.metadata[meta.Type] = meta
	return nil
}

// Create Stage 인스턴스 생성 및 초기화
func (r *Registry) Create(stageType string, config json.RawMessage) (Stage, error) {
	r.mu.RLock()
	factory, exists := r.factories[stageType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown stage type: %s", stageType)
	}

	stage := factory()
	if err := stage.Init(config); err != nil {
		return nil, fmt.Errorf("failed to init stage %s: %w", stageType, err)
	}

	return stage, nil
}

// Get Stage 팩토리 조회
func (r *Registry) Get(stageType string) (StageFactory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[stageType]
	return factory, ok
}

// Has Stage 존재 여부 확인
func (r *Registry) Has(stageType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[stageType]
	return ok
}

// List 등록된 Stage 메타데이터 목록 반환
func (r *Registry) List() []*StageMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*StageMetadata, 0, len(r.metadata))
	for _, m := range r.metadata {
		result = append(result, m)
	}
	return result
}

// ListByCategory 카테고리별 Stage 목록
func (r *Registry) ListByCategory(category string) []*StageMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*StageMetadata
	for _, m := range r.metadata {
		if m.Category == category {
			result = append(result, m)
		}
	}
	return result
}

// Types 등록된 모든 Stage 타입명 목록
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}

// GetMetadata Stage 메타데이터 조회
func (r *Registry) GetMetadata(stageType string) (*StageMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.metadata[stageType]
	return m, ok
}
