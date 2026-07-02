// Package sdk provides NativeStage interface for in-process plugin execution.
// Plugin authors implement NativeStage instead of the gRPC-based Stage interface
// when their stage will be compiled directly into the pipeline-batch-job binary.
package sdk

// NativeStage 인프로세스 실행 Stage 인터페이스
// Process 또는 ProcessBatch 중 하나만 구현해도 됨.
// ProcessBatch 미구현 시 Process를 반복 호출하여 처리.
type NativeStage interface {
	// Init 초기화 (config 전달, 외부 연결 설정 등)
	Init(config map[string]any) error

	// Process 단일 레코드 처리
	// nil 반환 → 레코드 드롭
	Process(record map[string]any) (map[string]any, error)

	// ProcessBatch 배치 처리 (선택적 구현)
	// 미구현 시 Process를 반복 호출
	ProcessBatch(records []map[string]any) ([]map[string]any, error)

	// Close 리소스 정리 (DB 연결, HTTP 클라이언트 등)
	Close() error
}

// BaseNativeStage 기본 구현체 — ProcessBatch의 기본 동작 제공
// Plugin 작성자는 이 struct를 embed하고 Process만 구현하면 됨
type BaseNativeStage struct{}

// ProcessBatch 기본 구현: Process를 반복 호출
func (b *BaseNativeStage) ProcessBatch(records []map[string]any) ([]map[string]any, error) {
	return nil, nil // sentinel: adapter에서 Process 반복 호출로 대체
}

// Close 기본 구현: no-op
func (b *BaseNativeStage) Close() error {
	return nil
}
