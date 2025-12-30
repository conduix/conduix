package types

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/actor"
)

// TransformActor 변환 Actor
type TransformActor struct {
	*actor.BaseActor
	transformType  string
	config         map[string]any
	outputs        []string
	processedCount int64
	errorCount     int64
}

// NewTransformActor 새 TransformActor 생성
func NewTransformActor(name string, config map[string]any) *TransformActor {
	transformType := "passthrough"
	if tt, ok := config["transform_type"].(string); ok {
		transformType = tt
	}

	outputs := make([]string, 0)
	if outs, ok := config["outputs"].([]string); ok {
		outputs = outs
	}

	return &TransformActor{
		BaseActor:     actor.NewBaseActor(name, config),
		transformType: transformType,
		config:        config,
		outputs:       outputs,
	}
}

// PreStart 시작 전 초기화
func (t *TransformActor) PreStart(ctx actor.ActorContext) error {
	if err := t.BaseActor.PreStart(ctx); err != nil {
		return err
	}

	ctx.Logger().Info("Transform actor started", "type", t.transformType)
	return nil
}

// PostStop 종료 후 정리
func (t *TransformActor) PostStop(ctx actor.ActorContext) error {
	ctx.Logger().Info("Transform actor stopped",
		"type", t.transformType,
		"processed", t.processedCount,
		"errors", t.errorCount)
	return t.BaseActor.PostStop(ctx)
}

// Receive 메시지 처리
func (t *TransformActor) Receive(ctx actor.ActorContext, msg actor.Message) error {
	switch msg.Type {
	case actor.MessageTypeData:
		return t.handleData(ctx, msg)
	case actor.MessageTypeCommand:
		return t.handleCommand(ctx, msg)
	default:
		return nil
	}
}

func (t *TransformActor) handleData(ctx actor.ActorContext, msg actor.Message) error {
	data, ok := msg.Payload.(map[string]any)
	if !ok {
		t.errorCount++
		return fmt.Errorf("invalid payload type: %T", msg.Payload)
	}

	// 변환 실행
	result, err := t.transform(ctx, data)
	if err != nil {
		t.errorCount++
		ctx.Logger().Error("Transform failed", "error", err)
		return err
	}

	if result == nil {
		// 필터링된 경우
		return nil
	}

	t.processedCount++

	// 결과 전송
	t.emit(ctx, result, msg.Sender)

	return nil
}

func (t *TransformActor) handleCommand(ctx actor.ActorContext, msg actor.Message) error {
	// 명령 처리
	return nil
}

// transform 데이터 변환
func (t *TransformActor) transform(ctx actor.ActorContext, data map[string]any) (map[string]any, error) {
	switch t.transformType {
	case "passthrough":
		return data, nil

	case "remap":
		return t.remap(data)

	case "filter":
		return t.filter(data)

	case "aggregate":
		return t.aggregate(ctx, data)

	case "sample":
		return t.sample(data)

	case "enrich":
		return t.enrich(data)

	default:
		return data, nil
	}
}

// remap 필드 변환
func (t *TransformActor) remap(data map[string]any) (map[string]any, error) {
	// TODO: VRL 같은 표현식 지원
	result := make(map[string]any)
	for k, v := range data {
		result[k] = v
	}

	// 타임스탬프 추가
	result["processed_at"] = time.Now().Format(time.RFC3339)

	// JSON 파싱 시도
	if msg, ok := data["message"].(string); ok {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(msg), &parsed); err == nil {
			for k, v := range parsed {
				result[k] = v
			}
		}
	}

	return result, nil
}

// filter 조건 필터링
func (t *TransformActor) filter(data map[string]any) (map[string]any, error) {
	condition, ok := t.config["condition"].(string)
	if !ok {
		return data, nil
	}

	// 간단한 조건 평가 (예: .level == "error")
	if condition == ".level == \"error\"" {
		if level, ok := data["level"].(string); ok && level == "error" {
			return data, nil
		}
		return nil, nil // 필터링됨
	}

	return data, nil
}

// aggregate 집계
func (t *TransformActor) aggregate(ctx actor.ActorContext, data map[string]any) (map[string]any, error) {
	// TODO: 윈도우 기반 집계 구현
	return data, nil
}

// sample 샘플링
func (t *TransformActor) sample(data map[string]any) (map[string]any, error) {
	rate, ok := t.config["rate"].(float64)
	if !ok {
		rate = 1.0
	}

	// 간단한 샘플링
	if float64(t.processedCount%100) < rate*100 {
		return data, nil
	}

	return nil, nil // 샘플링에서 제외
}

// enrich 데이터 보강
func (t *TransformActor) enrich(data map[string]any) (map[string]any, error) {
	result := make(map[string]any)
	for k, v := range data {
		result[k] = v
	}

	// 룩업 테이블에서 보강
	if lookupTable, ok := t.config["lookup_table"].(string); ok {
		result["enriched_from"] = lookupTable
	}

	return result, nil
}

// emit 결과 전송
func (t *TransformActor) emit(ctx actor.ActorContext, data map[string]any, originalSender *actor.ActorRef) {
	msg := actor.Message{
		ID:        actor.GenerateID(),
		Type:      actor.MessageTypeData,
		Payload:   data,
		Sender:    ctx.Self(),
		Timestamp: time.Now(),
	}

	// 지정된 출력으로 전송
	for _, output := range t.outputs {
		if ref, err := ctx.System().Get(output); err == nil {
			_ = ref.Tell(msg)
		}
	}

	// 출력이 없으면 부모에게 전송
	if len(t.outputs) == 0 {
		if parent := ctx.Parent(); parent != nil {
			_ = parent.Tell(msg)
		}
	}
}

// GetStats 통계 조회
func (t *TransformActor) GetStats() map[string]int64 {
	return map[string]int64{
		"processed": t.processedCount,
		"errors":    t.errorCount,
	}
}
