package stream

import (
	"context"
	"fmt"
	"sync"

	sdk "github.com/conduix/conduix/plugin-sdk"
)

// CustomStageFactory 커스텀 스테이지 생성 함수 타입
type CustomStageFactory func(name string, config map[string]any) (Stage, error)

// customStageRegistry 커스텀 스테이지 팩토리 레지스트리
var (
	customStageFactories = make(map[string]CustomStageFactory)
	customStageMu        sync.RWMutex
)

// RegisterCustomStage 커스텀 스테이지 팩토리를 등록
// 빌드 시 자동 생성되는 registry_custom.go의 init()에서 호출
func RegisterCustomStage(stageType string, factory CustomStageFactory) {
	customStageMu.Lock()
	defer customStageMu.Unlock()
	customStageFactories[stageType] = factory
}

// GetCustomStageFactory 등록된 커스텀 스테이지 팩토리를 반환
func GetCustomStageFactory(stageType string) (CustomStageFactory, bool) {
	customStageMu.RLock()
	defer customStageMu.RUnlock()
	f, ok := customStageFactories[stageType]
	return f, ok
}

// GetRegisteredCustomStages 등록된 모든 커스텀 스테이지 타입 목록을 반환
func GetRegisteredCustomStages() []string {
	customStageMu.RLock()
	defer customStageMu.RUnlock()
	types := make([]string, 0, len(customStageFactories))
	for t := range customStageFactories {
		types = append(types, t)
	}
	return types
}

// NativeStageAdapter sdk.NativeStage를 stream.Stage로 변환하는 어댑터
type NativeStageAdapter struct {
	BaseStage
	native sdk.NativeStage
}

// NewNativeStageAdapter NativeStage를 stream.Stage로 래핑
func NewNativeStageAdapter(name string, native sdk.NativeStage, config map[string]any) (Stage, error) {
	if err := native.Init(config); err != nil {
		return nil, fmt.Errorf("native stage init failed: %w", err)
	}

	return &NativeStageAdapter{
		BaseStage: BaseStage{name: name, typ: "native", config: config},
		native:    native,
	}, nil
}

// Process 단일 레코드 처리 (sdk.NativeStage.Process → stream.Stage.Process)
func (a *NativeStageAdapter) Process(ctx context.Context, record *Record) (*Record, error) {
	a.incrementInput()

	// context 취소 확인
	select {
	case <-ctx.Done():
		return record, ctx.Err()
	default:
	}

	result, err := a.native.Process(record.Data)
	if err != nil {
		a.incrementError()
		// 에러 시 원본 레코드 passthrough
		return record, nil
	}

	if result == nil {
		// nil → 레코드 드롭
		return nil, nil
	}

	a.incrementOutput()
	return &Record{Data: result, Metadata: record.Metadata}, nil
}

// Close 리소스 정리
func (a *NativeStageAdapter) Close() error {
	return a.native.Close()
}
