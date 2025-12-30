package types

import (
	"fmt"
	"strings"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/actor"
)

// RouterActor 라우터 Actor
type RouterActor struct {
	*actor.BaseActor
	routes        []Route
	defaultOutput string
}

// Route 라우팅 규칙
type Route struct {
	Condition string
	Output    string
}

// NewRouterActor 새 RouterActor 생성
func NewRouterActor(name string, config map[string]any) *RouterActor {
	routes := make([]Route, 0)

	if routing, ok := config["routing"].([]any); ok {
		for _, r := range routing {
			if rMap, ok := r.(map[string]any); ok {
				condition := ""
				output := ""
				if c, ok := rMap["condition"].(string); ok {
					condition = c
				}
				if o, ok := rMap["output"].(string); ok {
					output = o
				}
				routes = append(routes, Route{
					Condition: condition,
					Output:    output,
				})
			}
		}
	}

	defaultOutput := ""
	if d, ok := config["default"].(string); ok {
		defaultOutput = d
	}

	return &RouterActor{
		BaseActor:     actor.NewBaseActor(name, config),
		routes:        routes,
		defaultOutput: defaultOutput,
	}
}

// PreStart 시작 전 초기화
func (r *RouterActor) PreStart(ctx actor.ActorContext) error {
	if err := r.BaseActor.PreStart(ctx); err != nil {
		return err
	}

	ctx.Logger().Info("Router actor started", "routes", len(r.routes))
	return nil
}

// Receive 메시지 처리
func (r *RouterActor) Receive(ctx actor.ActorContext, msg actor.Message) error {
	switch msg.Type {
	case actor.MessageTypeData:
		return r.handleData(ctx, msg)
	default:
		return nil
	}
}

func (r *RouterActor) handleData(ctx actor.ActorContext, msg actor.Message) error {
	data, ok := msg.Payload.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid payload type: %T", msg.Payload)
	}

	// 라우팅 규칙 평가
	output := r.route(data)
	if output == "" {
		output = r.defaultOutput
	}

	if output == "" {
		ctx.Logger().Debug("No route matched, dropping message")
		return nil
	}

	// 출력으로 전송
	r.emit(ctx, data, output)

	return nil
}

// route 라우팅 결정
func (r *RouterActor) route(data map[string]any) string {
	for _, route := range r.routes {
		if r.evaluate(route.Condition, data) {
			return route.Output
		}
	}
	return ""
}

// evaluate 조건 평가
func (r *RouterActor) evaluate(condition string, data map[string]any) bool {
	// 간단한 조건 평가기
	// 지원 형식: .field == "value", .field != "value", true

	if condition == "true" {
		return true
	}

	// .field == "value" 형식 파싱
	if strings.Contains(condition, "==") {
		parts := strings.SplitN(condition, "==", 2)
		if len(parts) != 2 {
			return false
		}

		field := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// 필드명에서 . 제거
		field = strings.TrimPrefix(field, ".")

		// 값에서 따옴표 제거
		value = strings.Trim(value, "\"'")

		if v, ok := data[field].(string); ok {
			return v == value
		}
		if v, ok := data[field].(float64); ok {
			return fmt.Sprintf("%v", v) == value
		}
		if v, ok := data[field].(int); ok {
			return fmt.Sprintf("%v", v) == value
		}
	}

	// .field != "value" 형식 파싱
	if strings.Contains(condition, "!=") {
		parts := strings.SplitN(condition, "!=", 2)
		if len(parts) != 2 {
			return false
		}

		field := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		field = strings.TrimPrefix(field, ".")
		value = strings.Trim(value, "\"'")

		if v, ok := data[field].(string); ok {
			return v != value
		}
	}

	// .field exists 체크
	if strings.HasSuffix(condition, " exists") {
		field := strings.TrimSuffix(condition, " exists")
		field = strings.TrimSpace(field)
		field = strings.TrimPrefix(field, ".")
		_, exists := data[field]
		return exists
	}

	return false
}

// emit 데이터 전송
func (r *RouterActor) emit(ctx actor.ActorContext, data map[string]any, output string) {
	msg := actor.Message{
		ID:        actor.GenerateID(),
		Type:      actor.MessageTypeData,
		Payload:   data,
		Sender:    ctx.Self(),
		Timestamp: time.Now(),
	}

	// 지정된 출력으로 전송
	// 먼저 자식에서 찾기
	for _, child := range ctx.Children() {
		if child.Name == output {
			_ = child.Tell(msg)
			return
		}
	}

	// 시스템에서 전체 경로로 찾기
	if ref, err := ctx.System().Get(output); err == nil {
		_ = ref.Tell(msg)
		return
	}

	// 부모의 형제에서 찾기 (같은 레벨)
	if parent := ctx.Parent(); parent != nil {
		_ = parent.Tell(actor.Message{
			ID:   actor.GenerateID(),
			Type: actor.MessageTypeData,
			Payload: RouteRequest{
				Target:  output,
				Message: msg,
			},
			Sender:    ctx.Self(),
			Timestamp: time.Now(),
		})
	}
}

// RouteRequest 라우팅 요청
type RouteRequest struct {
	Target  string
	Message actor.Message
}

// BroadcastRouter 브로드캐스트 라우터
type BroadcastRouter struct {
	*actor.BaseActor
	outputs []string
}

// NewBroadcastRouter 새 브로드캐스트 라우터 생성
func NewBroadcastRouter(name string, outputs []string) *BroadcastRouter {
	return &BroadcastRouter{
		BaseActor: actor.NewBaseActor(name, nil),
		outputs:   outputs,
	}
}

func (b *BroadcastRouter) Receive(ctx actor.ActorContext, msg actor.Message) error {
	if msg.Type != actor.MessageTypeData {
		return nil
	}

	// 모든 출력에 브로드캐스트
	for _, output := range b.outputs {
		if ref, err := ctx.System().Get(output); err == nil {
			_ = ref.Tell(msg)
		}
	}

	return nil
}

// RoundRobinRouter 라운드로빈 라우터
type RoundRobinRouter struct {
	*actor.BaseActor
	outputs []string
	current int
}

// NewRoundRobinRouter 새 라운드로빈 라우터 생성
func NewRoundRobinRouter(name string, outputs []string) *RoundRobinRouter {
	return &RoundRobinRouter{
		BaseActor: actor.NewBaseActor(name, nil),
		outputs:   outputs,
		current:   0,
	}
}

func (rr *RoundRobinRouter) Receive(ctx actor.ActorContext, msg actor.Message) error {
	if msg.Type != actor.MessageTypeData {
		return nil
	}

	if len(rr.outputs) == 0 {
		return nil
	}

	output := rr.outputs[rr.current%len(rr.outputs)]
	rr.current++

	if ref, err := ctx.System().Get(output); err == nil {
		_ = ref.Tell(msg)
	}

	return nil
}
