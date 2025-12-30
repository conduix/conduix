package types

// ActorType Actor 타입
type ActorType string

const (
	ActorTypeSupervisor ActorType = "supervisor"
	ActorTypeSource     ActorType = "source"
	ActorTypeTransform  ActorType = "transform"
	ActorTypeSink       ActorType = "sink"
	ActorTypeRouter     ActorType = "router"
)

// SupervisionStrategy 감독 전략
type SupervisionStrategy string

const (
	// OneForOne 실패한 자식만 재시작
	OneForOne SupervisionStrategy = "one_for_one"
	// OneForAll 하나가 실패하면 모든 자식 재시작
	OneForAll SupervisionStrategy = "one_for_all"
	// RestForOne 실패한 자식과 그 이후에 시작된 자식들 재시작
	RestForOne SupervisionStrategy = "rest_for_one"
)

// OverflowStrategy Mailbox 오버플로우 전략
type OverflowStrategy string

const (
	OverflowBackpressure OverflowStrategy = "backpressure"
	OverflowDropOldest   OverflowStrategy = "drop_oldest"
	OverflowDropNewest   OverflowStrategy = "drop_newest"
)

// ActorState Actor 상태
type ActorState string

const (
	ActorStateCreated    ActorState = "created"
	ActorStateStarting   ActorState = "starting"
	ActorStateRunning    ActorState = "running"
	ActorStateStopping   ActorState = "stopping"
	ActorStateStopped    ActorState = "stopped"
	ActorStateRestarting ActorState = "restarting"
	ActorStateFailed     ActorState = "failed"
)

// SupervisionConfig 감독 설정
type SupervisionConfig struct {
	Strategy      SupervisionStrategy `json:"strategy" yaml:"strategy"`
	MaxRestarts   int                 `json:"max_restarts" yaml:"max_restarts"`
	WithinSeconds int                 `json:"within_seconds" yaml:"within_seconds"`
}

// MailboxConfig Mailbox 설정
type MailboxConfig struct {
	Capacity         int              `json:"capacity" yaml:"capacity"`
	OverflowStrategy OverflowStrategy `json:"overflow_strategy" yaml:"overflow_strategy"`
}

// DispatcherConfig Dispatcher 설정
type DispatcherConfig struct {
	Type        string `json:"type" yaml:"type"`
	Parallelism int    `json:"parallelism" yaml:"parallelism"`
}

// ActorSystemConfig Actor 시스템 설정
type ActorSystemConfig struct {
	Dispatcher DispatcherConfig `json:"dispatcher" yaml:"dispatcher"`
	Mailbox    MailboxConfig    `json:"mailbox" yaml:"mailbox"`
}

// ActorDefinition Actor 정의
type ActorDefinition struct {
	Name        string             `json:"name" yaml:"name"`
	Type        ActorType          `json:"type" yaml:"type"`
	Parallelism int                `json:"parallelism,omitempty" yaml:"parallelism,omitempty"`
	Supervision *SupervisionConfig `json:"supervision,omitempty" yaml:"supervision,omitempty"`
	Config      map[string]any     `json:"config,omitempty" yaml:"config,omitempty"`
	Outputs     []string           `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	Children    []ActorDefinition  `json:"children,omitempty" yaml:"children,omitempty"`
}

// ActorStatus Actor 상태 정보
type ActorStatus struct {
	Name           string     `json:"name"`
	Type           ActorType  `json:"type"`
	State          ActorState `json:"state"`
	ProcessedCount int64      `json:"processed_count"`
	ErrorCount     int64      `json:"error_count"`
	RestartCount   int        `json:"restart_count"`
	Children       []string   `json:"children,omitempty"`
}
