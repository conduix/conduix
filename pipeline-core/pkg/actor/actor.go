package actor

import (
	"context"
	"sync"
	"time"

	"github.com/conduix/conduix/shared/types"
)

// Message Actor 간 전달되는 메시지
type Message struct {
	ID        string
	Type      MessageType
	Payload   any
	Sender    *ActorRef
	Timestamp time.Time
	ReplyTo   chan Message
}

// MessageType 메시지 타입
type MessageType string

const (
	MessageTypeData      MessageType = "data"
	MessageTypeCommand   MessageType = "command"
	MessageTypeError     MessageType = "error"
	MessageTypeLifecycle MessageType = "lifecycle"
)

// ActorRef Actor 참조
type ActorRef struct {
	Path    string
	Name    string
	mailbox *Mailbox
	actor   Actor
	system  *System
}

// Tell 메시지 전송 (비동기)
func (ref *ActorRef) Tell(msg Message) error {
	return ref.mailbox.Push(msg)
}

// Ask 메시지 전송 후 응답 대기
func (ref *ActorRef) Ask(ctx context.Context, msg Message) (Message, error) {
	replyChan := make(chan Message, 1)
	msg.ReplyTo = replyChan

	if err := ref.mailbox.Push(msg); err != nil {
		return Message{}, err
	}

	select {
	case reply := <-replyChan:
		return reply, nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}

// Actor 인터페이스
type Actor interface {
	// Receive 메시지 수신 처리
	Receive(ctx ActorContext, msg Message) error

	// PreStart Actor 시작 전 호출
	PreStart(ctx ActorContext) error

	// PostStop Actor 종료 후 호출
	PostStop(ctx ActorContext) error

	// PreRestart 재시작 전 호출
	PreRestart(ctx ActorContext, reason error) error

	// PostRestart 재시작 후 호출
	PostRestart(ctx ActorContext, reason error) error
}

// BaseActor 기본 Actor 구현
type BaseActor struct {
	name   string
	state  types.ActorState
	config map[string]any
}

// NewBaseActor 새 BaseActor 생성
func NewBaseActor(name string, config map[string]any) *BaseActor {
	return &BaseActor{
		name:   name,
		state:  types.ActorStateCreated,
		config: config,
	}
}

func (a *BaseActor) PreStart(ctx ActorContext) error {
	a.state = types.ActorStateRunning
	return nil
}

func (a *BaseActor) PostStop(ctx ActorContext) error {
	a.state = types.ActorStateStopped
	return nil
}

func (a *BaseActor) PreRestart(ctx ActorContext, reason error) error {
	a.state = types.ActorStateRestarting
	return nil
}

func (a *BaseActor) PostRestart(ctx ActorContext, reason error) error {
	a.state = types.ActorStateRunning
	return nil
}

func (a *BaseActor) Receive(ctx ActorContext, msg Message) error {
	// 기본 구현: 아무것도 하지 않음
	return nil
}

// ActorContext Actor 컨텍스트
type ActorContext interface {
	// Self 자신의 ActorRef 반환
	Self() *ActorRef

	// Parent 부모 ActorRef 반환
	Parent() *ActorRef

	// Children 자식 ActorRef 목록 반환
	Children() []*ActorRef

	// Spawn 자식 Actor 생성
	Spawn(props Props) (*ActorRef, error)

	// Stop Actor 중지
	Stop(ref *ActorRef) error

	// Watch 다른 Actor 감시
	Watch(ref *ActorRef)

	// Unwatch 감시 해제
	Unwatch(ref *ActorRef)

	// System Actor 시스템 반환
	System() *System

	// Logger 로거 반환
	Logger() Logger

	// Checkpoint 체크포인트 저장
	Checkpoint(data map[string]any) error

	// GetCheckpoint 체크포인트 조회
	GetCheckpoint() (map[string]any, error)
}

// Props Actor 생성 속성
type Props struct {
	Name        string
	Factory     func() Actor
	Parallelism int
	Supervision *types.SupervisionConfig
	Mailbox     *types.MailboxConfig
	Outputs     []string
}

// actorContext ActorContext 구현
type actorContext struct {
	self     *ActorRef
	parent   *ActorRef
	children map[string]*ActorRef
	system   *System
	logger   Logger
	mu       sync.RWMutex
	watchers map[string]*ActorRef
	watching map[string]*ActorRef
}

func newActorContext(self, parent *ActorRef, system *System) *actorContext {
	return &actorContext{
		self:     self,
		parent:   parent,
		system:   system,
		children: make(map[string]*ActorRef),
		watchers: make(map[string]*ActorRef),
		watching: make(map[string]*ActorRef),
		logger:   system.logger,
	}
}

func (c *actorContext) Self() *ActorRef {
	return c.self
}

func (c *actorContext) Parent() *ActorRef {
	return c.parent
}

func (c *actorContext) Children() []*ActorRef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	children := make([]*ActorRef, 0, len(c.children))
	for _, child := range c.children {
		children = append(children, child)
	}
	return children
}

func (c *actorContext) Spawn(props Props) (*ActorRef, error) {
	ref, err := c.system.spawn(props, c.self)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.children[props.Name] = ref
	c.mu.Unlock()

	return ref, nil
}

func (c *actorContext) Stop(ref *ActorRef) error {
	return c.system.stop(ref)
}

func (c *actorContext) Watch(ref *ActorRef) {
	c.mu.Lock()
	c.watching[ref.Path] = ref
	c.mu.Unlock()
}

func (c *actorContext) Unwatch(ref *ActorRef) {
	c.mu.Lock()
	delete(c.watching, ref.Path)
	c.mu.Unlock()
}

func (c *actorContext) System() *System {
	return c.system
}

func (c *actorContext) Logger() Logger {
	return c.logger
}

func (c *actorContext) Checkpoint(data map[string]any) error {
	return c.system.saveCheckpoint(c.self.Path, data)
}

func (c *actorContext) GetCheckpoint() (map[string]any, error) {
	return c.system.getCheckpoint(c.self.Path)
}

// Logger 로거 인터페이스
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}
