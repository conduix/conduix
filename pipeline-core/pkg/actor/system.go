package actor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/shared/types"
)

var (
	ErrActorNotFound = errors.New("actor not found")
	ErrSystemStopped = errors.New("actor system is stopped")
	ErrActorExists   = errors.New("actor already exists")
)

// System Actor 시스템
type System struct {
	name         string
	config       *types.ActorSystemConfig
	actors       map[string]*actorInfo
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	logger       Logger
	checkpointer Checkpointer
	dispatcher   *Dispatcher
	running      bool
}

// actorInfo Actor 내부 정보
type actorInfo struct {
	ref     *ActorRef
	ctx     *actorContext
	cancel  context.CancelFunc
	running bool
}

// Checkpointer 체크포인트 인터페이스
type Checkpointer interface {
	Save(path string, data map[string]any) error
	Load(path string) (map[string]any, error)
}

// NewSystem 새 Actor 시스템 생성
func NewSystem(name string, config *types.ActorSystemConfig, opts ...SystemOption) *System {
	ctx, cancel := context.WithCancel(context.Background())

	parallelism := 8
	if config != nil && config.Dispatcher.Parallelism > 0 {
		parallelism = config.Dispatcher.Parallelism
	}

	s := &System{
		name:       name,
		config:     config,
		actors:     make(map[string]*actorInfo),
		ctx:        ctx,
		cancel:     cancel,
		logger:     &defaultLogger{},
		dispatcher: NewDispatcher(parallelism),
		running:    false,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// SystemOption 시스템 옵션
type SystemOption func(*System)

// WithLogger 로거 설정
func WithLogger(logger Logger) SystemOption {
	return func(s *System) {
		s.logger = logger
	}
}

// WithCheckpointer 체크포인터 설정
func WithCheckpointer(cp Checkpointer) SystemOption {
	return func(s *System) {
		s.checkpointer = cp
	}
}

// Start 시스템 시작
func (s *System) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	s.running = true
	s.dispatcher.Start()
	s.logger.Info("Actor system started", "name", s.name)

	return nil
}

// Stop 시스템 중지
func (s *System) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	s.cancel()

	// 모든 Actor 중지
	for _, info := range s.actors {
		if info.cancel != nil {
			info.cancel()
		}
		if info.ref.mailbox != nil {
			info.ref.mailbox.Close()
		}
	}

	s.dispatcher.Stop()
	s.logger.Info("Actor system stopped", "name", s.name)

	return nil
}

// Spawn Actor 생성
func (s *System) Spawn(props Props) (*ActorRef, error) {
	return s.spawn(props, nil)
}

func (s *System) spawn(props Props, parent *ActorRef) (*ActorRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil, ErrSystemStopped
	}

	// 경로 생성
	var path string
	if parent != nil {
		path = fmt.Sprintf("%s/%s", parent.Path, props.Name)
	} else {
		path = fmt.Sprintf("/%s/%s", s.name, props.Name)
	}

	// 중복 확인
	if _, exists := s.actors[path]; exists {
		return nil, ErrActorExists
	}

	// Mailbox 설정
	mailboxConfig := props.Mailbox
	if mailboxConfig == nil && s.config != nil {
		mailboxConfig = &s.config.Mailbox
	}

	// Actor 생성
	actor := props.Factory()
	mailbox := NewMailbox(mailboxConfig)

	ref := &ActorRef{
		Path:    path,
		Name:    props.Name,
		mailbox: mailbox,
		actor:   actor,
		system:  s,
	}

	actorCtx, actorCancel := context.WithCancel(s.ctx)
	ctx := newActorContext(ref, parent, s)

	info := &actorInfo{
		ref:     ref,
		ctx:     ctx,
		cancel:  actorCancel,
		running: true,
	}

	s.actors[path] = info

	// Actor 시작
	go s.runActor(actorCtx, info, actor)

	s.logger.Info("Actor spawned", "path", path)

	return ref, nil
}

func (s *System) runActor(ctx context.Context, info *actorInfo, actor Actor) {
	// PreStart 호출
	if err := actor.PreStart(info.ctx); err != nil {
		s.logger.Error("Actor PreStart failed", "path", info.ref.Path, "error", err)
		s.notifyParentOfFailure(info)
		return
	}

	// 메시지 처리 루프
	for {
		select {
		case <-ctx.Done():
			// PostStop 호출
			if err := actor.PostStop(info.ctx); err != nil {
				s.logger.Error("Actor PostStop failed", "path", info.ref.Path, "error", err)
			}
			return

		default:
			msg, ok := info.ref.mailbox.TryPop()
			if !ok {
				// 메시지가 없으면 잠시 대기
				time.Sleep(1 * time.Millisecond)
				continue
			}

			// 메시지 처리
			s.dispatcher.Dispatch(func() {
				if err := actor.Receive(info.ctx, msg); err != nil {
					s.logger.Error("Actor message handling failed",
						"path", info.ref.Path,
						"msgType", msg.Type,
						"error", err)

					// 에러를 부모에게 전파
					s.notifyParentOfFailure(info)
				}

				// 응답이 필요한 경우
				if msg.ReplyTo != nil {
					// Actor가 응답하지 않으면 기본 응답
					select {
					case msg.ReplyTo <- Message{Type: MessageTypeData}:
					default:
					}
				}
			})
		}
	}
}

func (s *System) notifyParentOfFailure(info *actorInfo) {
	if parent := info.ctx.Parent(); parent != nil {
		_ = parent.Tell(Message{
			Type: MessageTypeLifecycle,
			Payload: ChildFailed{
				Name:  info.ref.Name,
				Error: errors.New("actor failed"),
			},
		})
	}
}

func (s *System) stop(ref *ActorRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.actors[ref.Path]
	if !ok {
		return ErrActorNotFound
	}

	info.running = false
	if info.cancel != nil {
		info.cancel()
	}
	if ref.mailbox != nil {
		ref.mailbox.Close()
	}

	delete(s.actors, ref.Path)
	s.logger.Info("Actor stopped", "path", ref.Path)

	return nil
}

// Get Actor 조회
func (s *System) Get(path string) (*ActorRef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, ok := s.actors[path]
	if !ok {
		return nil, ErrActorNotFound
	}

	return info.ref, nil
}

// saveCheckpoint 체크포인트 저장
func (s *System) saveCheckpoint(path string, data map[string]any) error {
	if s.checkpointer == nil {
		return nil
	}
	return s.checkpointer.Save(path, data)
}

// getCheckpoint 체크포인트 조회
func (s *System) getCheckpoint(path string) (map[string]any, error) {
	if s.checkpointer == nil {
		return nil, nil
	}
	return s.checkpointer.Load(path)
}

// defaultLogger 기본 로거
type defaultLogger struct{}

func (l *defaultLogger) Debug(msg string, args ...any) {
	fmt.Printf("[DEBUG] %s %v\n", msg, args)
}

func (l *defaultLogger) Info(msg string, args ...any) {
	fmt.Printf("[INFO] %s %v\n", msg, args)
}

func (l *defaultLogger) Warn(msg string, args ...any) {
	fmt.Printf("[WARN] %s %v\n", msg, args)
}

func (l *defaultLogger) Error(msg string, args ...any) {
	fmt.Printf("[ERROR] %s %v\n", msg, args)
}

// Dispatcher 메시지 디스패처
type Dispatcher struct {
	parallelism int
	workers     chan func()
	wg          sync.WaitGroup
	running     bool
	mu          sync.Mutex
}

// NewDispatcher 새 디스패처 생성
func NewDispatcher(parallelism int) *Dispatcher {
	return &Dispatcher{
		parallelism: parallelism,
		workers:     make(chan func(), parallelism*100),
	}
}

// Start 디스패처 시작
func (d *Dispatcher) Start() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return
	}

	d.running = true
	for i := 0; i < d.parallelism; i++ {
		d.wg.Add(1)
		go d.worker()
	}
}

// Stop 디스패처 중지
func (d *Dispatcher) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}

	d.running = false
	close(d.workers)
	d.wg.Wait()
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for task := range d.workers {
		task()
	}
}

// Dispatch 작업 디스패치
func (d *Dispatcher) Dispatch(task func()) {
	select {
	case d.workers <- task:
	default:
		// 워커가 모두 바쁘면 직접 실행
		go task()
	}
}

// GenerateID ID 생성
func GenerateID() string {
	return uuid.New().String()
}
