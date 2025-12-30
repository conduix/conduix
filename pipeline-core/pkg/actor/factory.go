package actor

import (
	"github.com/conduix/conduix/shared/types"
)

// ActorFactory Actor 팩토리
type ActorFactory struct {
	registry map[types.ActorType]func(name string, config map[string]any) Actor
	// useBento Bento 기반 Actor 사용 여부
	useBento bool
}

// NewActorFactory 새 팩토리 생성
func NewActorFactory() *ActorFactory {
	return &ActorFactory{
		registry: make(map[types.ActorType]func(name string, config map[string]any) Actor),
		useBento: true, // 기본값: Bento 사용
	}
}

// SetUseBento Bento 사용 여부 설정
func (f *ActorFactory) SetUseBento(use bool) {
	f.useBento = use
}

// UseBento Bento 사용 여부 반환
func (f *ActorFactory) UseBento() bool {
	return f.useBento
}

// Register Actor 타입 등록
func (f *ActorFactory) Register(actorType types.ActorType, factory func(name string, config map[string]any) Actor) {
	f.registry[actorType] = factory
}

// Create Actor 생성
func (f *ActorFactory) Create(actorType types.ActorType, name string, config map[string]any) Actor {
	if factory, ok := f.registry[actorType]; ok {
		return factory(name, config)
	}
	return nil
}

// DefaultFactory 기본 팩토리
var DefaultFactory = NewActorFactory()

// NewSourceActor 소스 Actor 생성 (외부에서 사용)
func NewSourceActor(name string, config map[string]any) Actor {
	return DefaultFactory.Create(types.ActorTypeSource, name, config)
}

// NewTransformActor 변환 Actor 생성 (외부에서 사용)
func NewTransformActor(name string, config map[string]any) Actor {
	return DefaultFactory.Create(types.ActorTypeTransform, name, config)
}

// NewSinkActor 싱크 Actor 생성 (외부에서 사용)
func NewSinkActor(name string, config map[string]any) Actor {
	return DefaultFactory.Create(types.ActorTypeSink, name, config)
}

// NewRouterActor 라우터 Actor 생성 (외부에서 사용)
func NewRouterActor(name string, config map[string]any) Actor {
	return DefaultFactory.Create(types.ActorTypeRouter, name, config)
}

// Bento 타입 상수
const (
	ActorTypeBentoSource    types.ActorType = "bento_source"
	ActorTypeBentoTransform types.ActorType = "bento_transform"
	ActorTypeBentoSink      types.ActorType = "bento_sink"
)

// InitDefaultFactory 기본 팩토리 초기화
// 이 함수는 types 패키지에서 호출됨
func InitDefaultFactory() {
	// 기본 Actor 타입들 등록은 types 패키지의 init()에서 수행
}
