package types

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/actor"
	"github.com/conduix/conduix/pipeline-core/pkg/adapter/bento"
)

// BentoTransformActor Bento 기반 변환 Actor
type BentoTransformActor struct {
	*actor.BaseActor
	transformType  string
	config         map[string]any
	outputs        []string
	adapter        *bento.ProcessorAdapter
	configBuilder  *bento.ConfigBuilder
	processedCount int64
	errorCount     int64
}

// NewBentoTransformActor 새 BentoTransformActor 생성
func NewBentoTransformActor(name string, config map[string]any) *BentoTransformActor {
	transformType := "passthrough"
	if tt, ok := config["transform_type"].(string); ok {
		transformType = tt
	}
	// type 필드도 지원
	if tt, ok := config["type"].(string); ok {
		transformType = tt
	}

	outputs := make([]string, 0)
	if outs, ok := config["outputs"].([]string); ok {
		outputs = outs
	}

	return &BentoTransformActor{
		BaseActor:     actor.NewBaseActor(name, config),
		transformType: transformType,
		config:        config,
		outputs:       outputs,
		configBuilder: bento.NewConfigBuilder(),
	}
}

// PreStart 시작 전 초기화
func (t *BentoTransformActor) PreStart(ctx actor.ActorContext) error {
	if err := t.BaseActor.PreStart(ctx); err != nil {
		return err
	}

	// 변환 타입에 따라 Bento processor 또는 내장 처리 선택
	bentoType, bentoConfig := t.mapToBentoProcessor()
	if bentoType != "" {
		processor, err := t.configBuilder.BuildProcessor(bentoType, bentoConfig)
		if err != nil {
			ctx.Logger().Warn("Failed to create Bento processor, using fallback",
				"type", t.transformType, "error", err)
		} else {
			t.adapter = bento.NewProcessorAdapter(processor)
		}
	}

	ctx.Logger().Info("Bento transform actor started", "type", t.transformType)
	return nil
}

// mapToBentoProcessor 우리 설정을 Bento processor로 매핑
func (t *BentoTransformActor) mapToBentoProcessor() (string, map[string]any) {
	switch t.transformType {
	case "remap":
		// VRL-like source를 Bloblang으로 변환
		source := ""
		if s, ok := t.config["source"].(string); ok {
			source = t.convertVRLToBloblang(s)
		}
		return "bloblang", map[string]any{"mapping": source}

	case "json_parse":
		return "json_parse", nil

	case "compress":
		algorithm := "gzip"
		if a, ok := t.config["algorithm"].(string); ok {
			algorithm = a
		}
		return "compress", map[string]any{"algorithm": algorithm}

	case "decompress":
		algorithm := "gzip"
		if a, ok := t.config["algorithm"].(string); ok {
			algorithm = a
		}
		return "decompress", map[string]any{"algorithm": algorithm}

	default:
		// passthrough, filter, sample 등은 내장 처리
		return "", nil
	}
}

// convertVRLToBloblang VRL 스타일을 Bloblang으로 변환
func (t *BentoTransformActor) convertVRLToBloblang(vrl string) string {
	// 기본 변환 규칙
	result := vrl

	// . = parse_json!(.message) → root = this.message.parse_json()
	result = strings.ReplaceAll(result, ". = parse_json!(.message)", "root = this.message.parse_json()")
	result = strings.ReplaceAll(result, "parse_json!", "parse_json")

	// . = 를 root = 로 변환
	result = strings.ReplaceAll(result, ". = ", "root = ")

	// .field 를 this.field 로 변환 (단순 필드 접근)
	// 주의: 복잡한 표현식은 수동 변환 필요

	// .processed_at = now() → root.processed_at = now()
	result = strings.ReplaceAll(result, ".processed_at = now()", "root.processed_at = now()")

	return result
}

// PostStop 종료 후 정리
func (t *BentoTransformActor) PostStop(ctx actor.ActorContext) error {
	if t.adapter != nil {
		if err := t.adapter.Stop(); err != nil {
			ctx.Logger().Error("Failed to stop adapter", "error", err)
		}
	}

	ctx.Logger().Info("Bento transform actor stopped",
		"type", t.transformType,
		"processed", t.processedCount,
		"errors", t.errorCount)

	return t.BaseActor.PostStop(ctx)
}

// Receive 메시지 처리
func (t *BentoTransformActor) Receive(ctx actor.ActorContext, msg actor.Message) error {
	switch msg.Type {
	case actor.MessageTypeData:
		return t.handleData(ctx, msg)
	case actor.MessageTypeCommand:
		return t.handleCommand(ctx, msg)
	default:
		return nil
	}
}

func (t *BentoTransformActor) handleData(ctx actor.ActorContext, msg actor.Message) error {
	data, ok := msg.Payload.(map[string]any)
	if !ok {
		t.errorCount++
		return fmt.Errorf("invalid payload type: %T", msg.Payload)
	}

	// 변환 실행
	results, err := t.transform(ctx, data)
	if err != nil {
		t.errorCount++
		ctx.Logger().Error("Transform failed", "error", err)
		return err
	}

	// 결과 전송
	for _, result := range results {
		t.processedCount++
		t.emit(ctx, result, msg.Sender)
	}

	return nil
}

func (t *BentoTransformActor) handleCommand(ctx actor.ActorContext, msg actor.Message) error {
	return nil
}

// transform 데이터 변환
func (t *BentoTransformActor) transform(ctx actor.ActorContext, data map[string]any) ([]map[string]any, error) {
	// Bento adapter가 있으면 사용
	if t.adapter != nil {
		return t.adapter.Process(context.Background(), data)
	}

	// 내장 변환 로직 사용
	result, err := t.builtinTransform(ctx, data)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return []map[string]any{}, nil // 필터링됨
	}
	return []map[string]any{result}, nil
}

// builtinTransform 내장 변환 로직
func (t *BentoTransformActor) builtinTransform(ctx actor.ActorContext, data map[string]any) (map[string]any, error) {
	switch t.transformType {
	case "passthrough":
		return data, nil

	case "remap":
		return t.remap(data)

	case "filter":
		return t.filter(data)

	case "sample":
		return t.sample(data)

	case "enrich":
		return t.enrich(data)

	case "aggregate":
		return t.aggregate(ctx, data)

	default:
		return data, nil
	}
}

// remap 필드 변환 (Bento 실패 시 폴백)
func (t *BentoTransformActor) remap(data map[string]any) (map[string]any, error) {
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
func (t *BentoTransformActor) filter(data map[string]any) (map[string]any, error) {
	condition, ok := t.config["condition"].(string)
	if !ok {
		return data, nil
	}

	// 조건 평가
	if t.evaluateCondition(condition, data) {
		return data, nil
	}
	return nil, nil // 필터링됨
}

// evaluateCondition 조건 평가
func (t *BentoTransformActor) evaluateCondition(condition string, data map[string]any) bool {
	// 간단한 조건 파싱
	// 지원 형식: .field == "value", .field != "value", .field exists

	// .level == "error"
	if strings.Contains(condition, "==") {
		parts := strings.Split(condition, "==")
		if len(parts) == 2 {
			field := strings.TrimSpace(strings.TrimPrefix(parts[0], "."))
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, `"'`)

			if v, ok := data[field]; ok {
				return fmt.Sprintf("%v", v) == value
			}
		}
	}

	// .level != "error"
	if strings.Contains(condition, "!=") {
		parts := strings.Split(condition, "!=")
		if len(parts) == 2 {
			field := strings.TrimSpace(strings.TrimPrefix(parts[0], "."))
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, `"'`)

			if v, ok := data[field]; ok {
				return fmt.Sprintf("%v", v) != value
			}
		}
	}

	// .field exists
	if strings.Contains(condition, "exists") {
		parts := strings.Split(condition, "exists")
		if len(parts) >= 1 {
			field := strings.TrimSpace(strings.TrimPrefix(parts[0], "."))
			_, exists := data[field]
			return exists
		}
	}

	// 기본: 통과
	return true
}

// sample 샘플링
func (t *BentoTransformActor) sample(data map[string]any) (map[string]any, error) {
	rate := 1.0
	if r, ok := t.config["rate"].(float64); ok {
		rate = r
	}

	if float64(t.processedCount%100) < rate*100 {
		return data, nil
	}
	return nil, nil // 샘플링에서 제외
}

// enrich 데이터 보강
func (t *BentoTransformActor) enrich(data map[string]any) (map[string]any, error) {
	result := make(map[string]any)
	for k, v := range data {
		result[k] = v
	}

	if lookupTable, ok := t.config["lookup_table"].(string); ok {
		result["enriched_from"] = lookupTable
	}

	return result, nil
}

// aggregate 집계
func (t *BentoTransformActor) aggregate(ctx actor.ActorContext, data map[string]any) (map[string]any, error) {
	// TODO: 윈도우 기반 집계 구현
	return data, nil
}

// emit 결과 전송
func (t *BentoTransformActor) emit(ctx actor.ActorContext, data map[string]any, originalSender *actor.ActorRef) {
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

// SetOutputs 출력 대상 설정
func (t *BentoTransformActor) SetOutputs(outputs []string) {
	t.outputs = outputs
}

// GetStats 통계 조회
func (t *BentoTransformActor) GetStats() map[string]int64 {
	return map[string]int64{
		"processed": t.processedCount,
		"errors":    t.errorCount,
	}
}
