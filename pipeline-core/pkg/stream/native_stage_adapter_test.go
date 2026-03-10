package stream

import (
	"context"
	"fmt"
	"testing"
)

// mockNativeStage NativeStage 인터페이스를 구현하는 테스트용 mock
type mockNativeStage struct {
	initCalled  bool
	initErr     error
	closeCalled bool
	processFunc func(record map[string]any) (map[string]any, error)
}

func (m *mockNativeStage) Init(config map[string]any) error {
	m.initCalled = true
	return m.initErr
}

func (m *mockNativeStage) Process(record map[string]any) (map[string]any, error) {
	if m.processFunc != nil {
		return m.processFunc(record)
	}
	return record, nil
}

func (m *mockNativeStage) ProcessBatch(records []map[string]any) ([]map[string]any, error) {
	return nil, nil // sentinel: adapter에서 Process 반복 호출
}

func (m *mockNativeStage) Close() error {
	m.closeCalled = true
	return nil
}

func TestNativeStageAdapter_Process(t *testing.T) {
	mock := &mockNativeStage{
		processFunc: func(record map[string]any) (map[string]any, error) {
			record["processed"] = true
			return record, nil
		},
	}

	stage, err := NewNativeStageAdapter("test-native", mock, map[string]any{})
	if err != nil {
		t.Fatalf("NewNativeStageAdapter failed: %v", err)
	}

	if !mock.initCalled {
		t.Error("Init was not called")
	}

	record := &Record{Data: map[string]any{"key": "value"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected record, got nil")
	}
	if result.Data["processed"] != true {
		t.Errorf("expected processed=true, got %v", result.Data["processed"])
	}
}

func TestNativeStageAdapter_ProcessDrop(t *testing.T) {
	mock := &mockNativeStage{
		processFunc: func(record map[string]any) (map[string]any, error) {
			return nil, nil // 드롭
		},
	}

	stage, err := NewNativeStageAdapter("test-drop", mock, map[string]any{})
	if err != nil {
		t.Fatalf("NewNativeStageAdapter failed: %v", err)
	}

	record := &Record{Data: map[string]any{"key": "value"}}
	result, err := stage.Process(context.Background(), record)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if result != nil {
		t.Error("expected nil (drop), got record")
	}
}

func TestNativeStageAdapter_ProcessError(t *testing.T) {
	mock := &mockNativeStage{
		processFunc: func(record map[string]any) (map[string]any, error) {
			return nil, fmt.Errorf("processing error")
		},
	}

	stage, err := NewNativeStageAdapter("test-error", mock, map[string]any{})
	if err != nil {
		t.Fatalf("NewNativeStageAdapter failed: %v", err)
	}

	record := &Record{Data: map[string]any{"key": "value"}}
	result, err := stage.Process(context.Background(), record)
	// 에러 시 원본 레코드 passthrough
	if err != nil {
		t.Errorf("expected nil error (passthrough), got %v", err)
	}
	if result == nil {
		t.Error("expected original record, got nil")
	}
}

func TestNativeStageAdapter_InitError(t *testing.T) {
	mock := &mockNativeStage{
		initErr: fmt.Errorf("init failed"),
	}

	_, err := NewNativeStageAdapter("test-init-err", mock, map[string]any{})
	if err == nil {
		t.Error("expected init error, got nil")
	}
}

func TestNativeStageAdapter_Close(t *testing.T) {
	mock := &mockNativeStage{}
	stage, err := NewNativeStageAdapter("test-close", mock, map[string]any{})
	if err != nil {
		t.Fatalf("NewNativeStageAdapter failed: %v", err)
	}

	if err := stage.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if !mock.closeCalled {
		t.Error("Close was not called on native stage")
	}
}

func TestNativeStageAdapter_Stats(t *testing.T) {
	mock := &mockNativeStage{
		processFunc: func(record map[string]any) (map[string]any, error) {
			return record, nil
		},
	}

	stage, err := NewNativeStageAdapter("test-stats", mock, map[string]any{})
	if err != nil {
		t.Fatalf("NewNativeStageAdapter failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		record := &Record{Data: map[string]any{"i": i}}
		_, _ = stage.Process(context.Background(), record)
	}

	adapter := stage.(*NativeStageAdapter)
	input, output, errors := adapter.Stats()
	if input != 3 {
		t.Errorf("expected input=3, got %d", input)
	}
	if output != 3 {
		t.Errorf("expected output=3, got %d", output)
	}
	if errors != 0 {
		t.Errorf("expected errors=0, got %d", errors)
	}
}

func TestNativeStageAdapter_ContextCancelled(t *testing.T) {
	mock := &mockNativeStage{}
	stage, err := NewNativeStageAdapter("test-ctx", mock, map[string]any{})
	if err != nil {
		t.Fatalf("NewNativeStageAdapter failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 즉시 취소

	record := &Record{Data: map[string]any{"key": "value"}}
	result, err := stage.Process(ctx, record)
	if err == nil {
		t.Error("expected context cancellation error")
	}
	if result == nil {
		t.Error("expected original record on context cancel")
	}
}

func TestRegisterCustomStage(t *testing.T) {
	testType := "test-custom-stage-xyz"

	// 등록 전
	_, ok := GetCustomStageFactory(testType)
	if ok {
		t.Error("factory should not exist before registration")
	}

	// 등록
	RegisterCustomStage(testType, func(name string, config map[string]any) (Stage, error) {
		return NewPassthroughStage(name, config), nil
	})

	// 등록 후
	factory, ok := GetCustomStageFactory(testType)
	if !ok {
		t.Fatal("factory should exist after registration")
	}

	stage, err := factory("my-stage", map[string]any{})
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if stage.Name() != "my-stage" {
		t.Errorf("expected name=my-stage, got %s", stage.Name())
	}

	// 등록 목록에 포함
	types := GetRegisteredCustomStages()
	found := false
	for _, tp := range types {
		if tp == testType {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %s in registered types", testType)
	}
}
