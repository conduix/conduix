package types

import (
	"github.com/conduix/conduix/pipeline-core/pkg/actor"
	"github.com/conduix/conduix/shared/types"
)

func init() {
	// 기본 Actor 타입 등록
	registerDefaultActors()
}

// registerDefaultActors 기본 Actor 타입들을 팩토리에 등록
func registerDefaultActors() {
	factory := actor.DefaultFactory

	// Source Actors
	factory.Register(types.ActorTypeSource, func(name string, config map[string]any) actor.Actor {
		if factory.UseBento() {
			return NewBentoSourceActor(name, config)
		}
		return NewSourceActor(name, config)
	})

	factory.Register(actor.ActorTypeBentoSource, func(name string, config map[string]any) actor.Actor {
		return NewBentoSourceActor(name, config)
	})

	// Transform Actors
	factory.Register(types.ActorTypeTransform, func(name string, config map[string]any) actor.Actor {
		if factory.UseBento() {
			return NewBentoTransformActor(name, config)
		}
		return NewTransformActor(name, config)
	})

	factory.Register(actor.ActorTypeBentoTransform, func(name string, config map[string]any) actor.Actor {
		return NewBentoTransformActor(name, config)
	})

	// Sink Actors
	factory.Register(types.ActorTypeSink, func(name string, config map[string]any) actor.Actor {
		if factory.UseBento() {
			return NewBentoSinkActor(name, config)
		}
		return NewSinkActor(name, config)
	})

	factory.Register(actor.ActorTypeBentoSink, func(name string, config map[string]any) actor.Actor {
		return NewBentoSinkActor(name, config)
	})

	// Router Actors
	factory.Register(types.ActorTypeRouter, func(name string, config map[string]any) actor.Actor {
		routerType := "condition"
		if rt, ok := config["router_type"].(string); ok {
			routerType = rt
		}

		// outputs 추출
		var outputs []string
		if o, ok := config["outputs"].([]string); ok {
			outputs = o
		} else if o, ok := config["outputs"].([]any); ok {
			for _, item := range o {
				if s, ok := item.(string); ok {
					outputs = append(outputs, s)
				}
			}
		}

		switch routerType {
		case "broadcast":
			return NewBroadcastRouter(name, outputs)
		case "round_robin":
			return NewRoundRobinRouter(name, outputs)
		default:
			return NewRouterActor(name, config)
		}
	})

	// Supervisor
	factory.Register(types.ActorTypeSupervisor, func(name string, config map[string]any) actor.Actor {
		return actor.NewBaseActor(name, config)
	})
}

// RegisterCustomActor 커스텀 Actor 타입 등록
func RegisterCustomActor(actorType types.ActorType, factory func(name string, config map[string]any) actor.Actor) {
	actor.DefaultFactory.Register(actorType, factory)
}

// SetUseBento Bento 기반 Actor 사용 여부 설정
func SetUseBento(use bool) {
	actor.DefaultFactory.SetUseBento(use)
}
