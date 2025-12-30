package actor

import (
	"context"
	"sync"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// Supervisor Actor 감독자
type Supervisor struct {
	*BaseActor
	strategy     types.SupervisionStrategy
	maxRestarts  int
	withinWindow time.Duration
	children     map[string]*ChildInfo
	mu           sync.RWMutex
}

// ChildInfo 자식 Actor 정보
type ChildInfo struct {
	Ref          *ActorRef
	Props        Props
	RestartCount int
	LastRestart  time.Time
	RestartTimes []time.Time
}

// NewSupervisor 새 Supervisor 생성
func NewSupervisor(name string, config *types.SupervisionConfig) *Supervisor {
	strategy := types.OneForOne
	maxRestarts := 3
	withinWindow := 60 * time.Second

	if config != nil {
		if config.Strategy != "" {
			strategy = config.Strategy
		}
		if config.MaxRestarts > 0 {
			maxRestarts = config.MaxRestarts
		}
		if config.WithinSeconds > 0 {
			withinWindow = time.Duration(config.WithinSeconds) * time.Second
		}
	}

	return &Supervisor{
		BaseActor:    NewBaseActor(name, nil),
		strategy:     strategy,
		maxRestarts:  maxRestarts,
		withinWindow: withinWindow,
		children:     make(map[string]*ChildInfo),
	}
}

// AddChild 자식 추가
func (s *Supervisor) AddChild(ref *ActorRef, props Props) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.children[props.Name] = &ChildInfo{
		Ref:          ref,
		Props:        props,
		RestartCount: 0,
		RestartTimes: make([]time.Time, 0),
	}
}

// RemoveChild 자식 제거
func (s *Supervisor) RemoveChild(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.children, name)
}

// GetChild 자식 조회
func (s *Supervisor) GetChild(name string) (*ActorRef, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if info, ok := s.children[name]; ok {
		return info.Ref, true
	}
	return nil, false
}

// HandleFailure 자식 실패 처리
func (s *Supervisor) HandleFailure(ctx ActorContext, childName string, err error) SupervisionDecision {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.children[childName]
	if !ok {
		return DecisionStop
	}

	// 시간 윈도우 내 재시작 횟수 계산
	now := time.Now()
	windowStart := now.Add(-s.withinWindow)

	// 윈도우 밖의 재시작 기록 제거
	validRestarts := make([]time.Time, 0)
	for _, t := range info.RestartTimes {
		if t.After(windowStart) {
			validRestarts = append(validRestarts, t)
		}
	}
	info.RestartTimes = validRestarts

	// 최대 재시작 횟수 초과 확인
	if len(info.RestartTimes) >= s.maxRestarts {
		return DecisionStop
	}

	// 재시작 기록 추가
	info.RestartTimes = append(info.RestartTimes, now)
	info.RestartCount++
	info.LastRestart = now

	return DecisionRestart
}

// SupervisionDecision 감독 결정
type SupervisionDecision int

const (
	DecisionRestart  SupervisionDecision = iota // 재시작
	DecisionResume                              // 이어서 실행
	DecisionStop                                // 중지
	DecisionEscalate                            // 상위로 전파
)

// Receive 메시지 처리
func (s *Supervisor) Receive(ctx ActorContext, msg Message) error {
	switch msg.Type {
	case MessageTypeLifecycle:
		return s.handleLifecycle(ctx, msg)
	case MessageTypeError:
		return s.handleError(ctx, msg)
	default:
		// 자식에게 메시지 브로드캐스트 또는 라우팅
		return s.routeMessage(ctx, msg)
	}
}

func (s *Supervisor) handleLifecycle(ctx ActorContext, msg Message) error {
	switch payload := msg.Payload.(type) {
	case ChildTerminated:
		s.RemoveChild(payload.Name)
		ctx.Logger().Info("Child terminated", "name", payload.Name)
	case ChildFailed:
		decision := s.HandleFailure(ctx, payload.Name, payload.Error)
		return s.executeDecision(ctx, payload.Name, decision)
	}
	return nil
}

func (s *Supervisor) handleError(ctx ActorContext, msg Message) error {
	if payload, ok := msg.Payload.(ChildFailed); ok {
		decision := s.HandleFailure(ctx, payload.Name, payload.Error)
		return s.executeDecision(ctx, payload.Name, decision)
	}
	return nil
}

func (s *Supervisor) executeDecision(ctx ActorContext, childName string, decision SupervisionDecision) error {
	s.mu.RLock()
	info, ok := s.children[childName]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	switch decision {
	case DecisionRestart:
		ctx.Logger().Info("Restarting child", "name", childName)
		return s.restartChild(ctx, info)

	case DecisionStop:
		ctx.Logger().Info("Stopping child", "name", childName)
		return ctx.Stop(info.Ref)

	case DecisionEscalate:
		ctx.Logger().Info("Escalating failure", "name", childName)
		if parent := ctx.Parent(); parent != nil {
			return parent.Tell(Message{
				Type: MessageTypeError,
				Payload: ChildFailed{
					Name:  s.name,
					Error: nil,
				},
			})
		}
		return nil

	case DecisionResume:
		ctx.Logger().Info("Resuming child", "name", childName)
		return nil
	}

	return nil
}

func (s *Supervisor) restartChild(ctx ActorContext, info *ChildInfo) error {
	// 전략에 따른 처리
	switch s.strategy {
	case types.OneForOne:
		// 실패한 자식만 재시작
		return s.restartSingle(ctx, info)

	case types.OneForAll:
		// 모든 자식 재시작
		return s.restartAll(ctx)

	case types.RestForOne:
		// 실패한 자식과 그 이후 자식들 재시작
		return s.restartRest(ctx, info)
	}

	return nil
}

func (s *Supervisor) restartSingle(ctx ActorContext, info *ChildInfo) error {
	// 기존 Actor 중지
	if err := ctx.Stop(info.Ref); err != nil {
		ctx.Logger().Error("Failed to stop child", "name", info.Props.Name, "error", err)
	}

	// 새 Actor 생성
	newRef, err := ctx.Spawn(info.Props)
	if err != nil {
		return err
	}

	s.mu.Lock()
	info.Ref = newRef
	s.mu.Unlock()

	return nil
}

func (s *Supervisor) restartAll(ctx ActorContext) error {
	s.mu.RLock()
	children := make([]*ChildInfo, 0, len(s.children))
	for _, info := range s.children {
		children = append(children, info)
	}
	s.mu.RUnlock()

	for _, info := range children {
		if err := s.restartSingle(ctx, info); err != nil {
			ctx.Logger().Error("Failed to restart child", "name", info.Props.Name, "error", err)
		}
	}

	return nil
}

func (s *Supervisor) restartRest(ctx ActorContext, failedInfo *ChildInfo) error {
	// 간단한 구현: 실패한 것과 동일하게 처리
	return s.restartSingle(ctx, failedInfo)
}

func (s *Supervisor) routeMessage(ctx ActorContext, msg Message) error {
	// 모든 자식에게 브로드캐스트
	for _, child := range ctx.Children() {
		if err := child.Tell(msg); err != nil {
			ctx.Logger().Error("Failed to route message", "child", child.Name, "error", err)
		}
	}
	return nil
}

// ChildTerminated 자식 종료 이벤트
type ChildTerminated struct {
	Name string
}

// ChildFailed 자식 실패 이벤트
type ChildFailed struct {
	Name  string
	Error error
}

// SupervisorActor Supervisor를 Actor로 사용하기 위한 팩토리
func SupervisorActor(config *types.SupervisionConfig) func() Actor {
	return func() Actor {
		return NewSupervisor("supervisor", config)
	}
}

// StartChildren 모든 자식 시작
func (s *Supervisor) StartChildren(ctx context.Context, actorCtx ActorContext, definitions []types.ActorDefinition) error {
	for _, def := range definitions {
		props := Props{
			Name:        def.Name,
			Parallelism: def.Parallelism,
			Supervision: def.Supervision,
			Outputs:     def.Outputs,
		}

		// Actor 타입에 따른 팩토리 설정
		switch def.Type {
		case types.ActorTypeSupervisor:
			props.Factory = SupervisorActor(def.Supervision)
		case types.ActorTypeSource:
			props.Factory = func() Actor { return NewSourceActor(def.Name, def.Config) }
		case types.ActorTypeTransform:
			props.Factory = func() Actor { return NewTransformActor(def.Name, def.Config) }
		case types.ActorTypeSink:
			props.Factory = func() Actor { return NewSinkActor(def.Name, def.Config) }
		case types.ActorTypeRouter:
			props.Factory = func() Actor { return NewRouterActor(def.Name, def.Config) }
		}

		ref, err := actorCtx.Spawn(props)
		if err != nil {
			return err
		}

		s.AddChild(ref, props)

		// 자식의 자식들도 재귀적으로 시작
		if len(def.Children) > 0 {
			if supervisor, ok := ref.actor.(*Supervisor); ok {
				if err := supervisor.StartChildren(ctx, actorCtx, def.Children); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
