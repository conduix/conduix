package actor

import (
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

func TestMessageTypes(t *testing.T) {
	tests := []struct {
		msgType  MessageType
		expected string
	}{
		{MessageTypeData, "data"},
		{MessageTypeCommand, "command"},
		{MessageTypeError, "error"},
		{MessageTypeLifecycle, "lifecycle"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.msgType) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.msgType)
			}
		})
	}
}

func TestMessage(t *testing.T) {
	now := time.Now()
	msg := Message{
		ID:        "msg-1",
		Type:      MessageTypeData,
		Payload:   map[string]any{"key": "value"},
		Timestamp: now,
	}

	if msg.ID != "msg-1" {
		t.Errorf("ID mismatch")
	}
	if msg.Type != MessageTypeData {
		t.Errorf("Type mismatch")
	}
	if msg.Payload == nil {
		t.Errorf("Payload should not be nil")
	}
}

func TestNewBaseActor(t *testing.T) {
	config := map[string]any{
		"key1": "value1",
		"key2": 42,
	}

	actor := NewBaseActor("test-actor", config)

	if actor.name != "test-actor" {
		t.Errorf("name mismatch: expected 'test-actor', got '%s'", actor.name)
	}
	if actor.state != types.ActorStateCreated {
		t.Errorf("state should be Created")
	}
	if actor.config["key1"] != "value1" {
		t.Errorf("config key1 mismatch")
	}
}

func TestBaseActorLifecycle(t *testing.T) {
	actor := NewBaseActor("test", nil)

	// Initial state
	if actor.state != types.ActorStateCreated {
		t.Errorf("expected Created state")
	}

	// PreStart
	if err := actor.PreStart(nil); err != nil {
		t.Errorf("PreStart error: %v", err)
	}
	if actor.state != types.ActorStateRunning {
		t.Errorf("expected Running state after PreStart")
	}

	// PreRestart
	if err := actor.PreRestart(nil, nil); err != nil {
		t.Errorf("PreRestart error: %v", err)
	}
	if actor.state != types.ActorStateRestarting {
		t.Errorf("expected Restarting state")
	}

	// PostRestart
	if err := actor.PostRestart(nil, nil); err != nil {
		t.Errorf("PostRestart error: %v", err)
	}
	if actor.state != types.ActorStateRunning {
		t.Errorf("expected Running state after PostRestart")
	}

	// PostStop
	if err := actor.PostStop(nil); err != nil {
		t.Errorf("PostStop error: %v", err)
	}
	if actor.state != types.ActorStateStopped {
		t.Errorf("expected Stopped state")
	}
}

func TestBaseActorReceive(t *testing.T) {
	actor := NewBaseActor("test", nil)

	msg := Message{
		ID:      "msg-1",
		Type:    MessageTypeData,
		Payload: "test",
	}

	// Default receive does nothing
	err := actor.Receive(nil, msg)
	if err != nil {
		t.Errorf("Receive should not error: %v", err)
	}
}

func TestProps(t *testing.T) {
	props := Props{
		Name: "test-actor",
		Factory: func() Actor {
			return NewBaseActor("test", nil)
		},
		Parallelism: 4,
		Supervision: &types.SupervisionConfig{
			Strategy:    types.OneForOne,
			MaxRestarts: 3,
		},
		Mailbox: &types.MailboxConfig{
			Capacity: 1000,
		},
		Outputs: []string{"output1", "output2"},
	}

	if props.Name != "test-actor" {
		t.Errorf("Name mismatch")
	}
	if props.Parallelism != 4 {
		t.Errorf("Parallelism mismatch")
	}
	if props.Factory == nil {
		t.Errorf("Factory should not be nil")
	}

	actor := props.Factory()
	if actor == nil {
		t.Errorf("Factory should create actor")
	}
}

func TestActorRef(t *testing.T) {
	mb := NewMailbox(&types.MailboxConfig{Capacity: 10})
	actor := NewBaseActor("test", nil)

	ref := &ActorRef{
		Path:    "/user/test",
		Name:    "test",
		mailbox: mb,
		actor:   actor,
	}

	if ref.Path != "/user/test" {
		t.Errorf("Path mismatch")
	}
	if ref.Name != "test" {
		t.Errorf("Name mismatch")
	}

	// Tell
	msg := Message{ID: "msg-1", Type: MessageTypeData}
	err := ref.Tell(msg)
	if err != nil {
		t.Errorf("Tell error: %v", err)
	}

	// Check message was received
	received, ok := mb.TryPop()
	if !ok {
		t.Error("expected message in mailbox")
	}
	if received.ID != msg.ID {
		t.Errorf("message ID mismatch")
	}
}

// TestActor custom test actor implementation
type TestActor struct {
	*BaseActor
	receivedMessages []Message
}

func NewTestActor(name string) *TestActor {
	return &TestActor{
		BaseActor:        NewBaseActor(name, nil),
		receivedMessages: make([]Message, 0),
	}
}

func (a *TestActor) Receive(ctx ActorContext, msg Message) error {
	a.receivedMessages = append(a.receivedMessages, msg)
	return nil
}

func TestCustomActor(t *testing.T) {
	actor := NewTestActor("custom")

	msg1 := Message{ID: "1", Type: MessageTypeData}
	msg2 := Message{ID: "2", Type: MessageTypeData}

	_ = actor.Receive(nil, msg1)
	_ = actor.Receive(nil, msg2)

	if len(actor.receivedMessages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(actor.receivedMessages))
	}
	if actor.receivedMessages[0].ID != "1" {
		t.Errorf("first message ID mismatch")
	}
	if actor.receivedMessages[1].ID != "2" {
		t.Errorf("second message ID mismatch")
	}
}
