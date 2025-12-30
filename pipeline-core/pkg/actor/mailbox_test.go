package actor

import (
	"sync"
	"testing"
	"time"

	"github.com/conduix/conduix/shared/types"
)

func TestNewMailbox(t *testing.T) {
	tests := []struct {
		name             string
		config           *types.MailboxConfig
		expectedCapacity int
		expectedStrategy types.OverflowStrategy
	}{
		{
			name:             "nil config",
			config:           nil,
			expectedCapacity: 10000,
			expectedStrategy: types.OverflowBackpressure,
		},
		{
			name: "custom capacity",
			config: &types.MailboxConfig{
				Capacity: 100,
			},
			expectedCapacity: 100,
			expectedStrategy: types.OverflowBackpressure,
		},
		{
			name: "custom strategy",
			config: &types.MailboxConfig{
				Capacity:         500,
				OverflowStrategy: types.OverflowDropNewest,
			},
			expectedCapacity: 500,
			expectedStrategy: types.OverflowDropNewest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb := NewMailbox(tt.config)
			if mb.capacity != tt.expectedCapacity {
				t.Errorf("capacity: expected %d, got %d", tt.expectedCapacity, mb.capacity)
			}
			if mb.overflowStrategy != tt.expectedStrategy {
				t.Errorf("strategy: expected %s, got %s", tt.expectedStrategy, mb.overflowStrategy)
			}
		})
	}
}

func TestMailboxPushPop(t *testing.T) {
	mb := NewMailbox(&types.MailboxConfig{Capacity: 10})

	msg := Message{
		ID:        "test-1",
		Type:      MessageTypeData,
		Payload:   "test payload",
		Timestamp: time.Now(),
	}

	// Push
	err := mb.Push(msg)
	if err != nil {
		t.Fatalf("push error: %v", err)
	}

	// Check length
	if mb.Len() != 1 {
		t.Errorf("expected length 1, got %d", mb.Len())
	}

	// Pop
	received, ok := mb.TryPop()
	if !ok {
		t.Fatal("expected message, got none")
	}
	if received.ID != msg.ID {
		t.Errorf("message ID mismatch: expected %s, got %s", msg.ID, received.ID)
	}
}

func TestMailboxTryPopEmpty(t *testing.T) {
	mb := NewMailbox(&types.MailboxConfig{Capacity: 10})

	msg, ok := mb.TryPop()
	if ok {
		t.Error("expected no message from empty mailbox")
	}
	if msg.ID != "" {
		t.Error("expected empty message")
	}
}

func TestMailboxClose(t *testing.T) {
	mb := NewMailbox(&types.MailboxConfig{Capacity: 10})

	if mb.IsClosed() {
		t.Error("mailbox should not be closed initially")
	}

	mb.Close()

	if !mb.IsClosed() {
		t.Error("mailbox should be closed")
	}

	// Push to closed mailbox should error
	err := mb.Push(Message{ID: "test"})
	if err != ErrMailboxClosed {
		t.Errorf("expected ErrMailboxClosed, got %v", err)
	}
}

func TestMailboxDoubleClose(t *testing.T) {
	mb := NewMailbox(&types.MailboxConfig{Capacity: 10})

	// Should not panic on double close
	mb.Close()
	mb.Close()
}

func TestMailboxDrain(t *testing.T) {
	mb := NewMailbox(&types.MailboxConfig{Capacity: 10})

	// Push multiple messages
	for i := 0; i < 5; i++ {
		_ = mb.Push(Message{ID: string(rune('a' + i))})
	}

	if mb.Len() != 5 {
		t.Errorf("expected 5 messages, got %d", mb.Len())
	}

	// Drain
	messages := mb.Drain()
	if len(messages) != 5 {
		t.Errorf("expected 5 drained messages, got %d", len(messages))
	}

	// Mailbox should be empty
	if mb.Len() != 0 {
		t.Errorf("expected 0 messages after drain, got %d", mb.Len())
	}
}

func TestMailboxDropNewestStrategy(t *testing.T) {
	mb := NewMailbox(&types.MailboxConfig{
		Capacity:         3,
		OverflowStrategy: types.OverflowDropNewest,
	})

	// Fill mailbox
	for i := 0; i < 3; i++ {
		err := mb.Push(Message{ID: string(rune('a' + i))})
		if err != nil {
			t.Fatalf("push error: %v", err)
		}
	}

	// Push one more - should return error
	err := mb.Push(Message{ID: "d"})
	if err != ErrMailboxFull {
		t.Errorf("expected ErrMailboxFull, got %v", err)
	}

	// Mailbox should still have original 3
	if mb.Len() != 3 {
		t.Errorf("expected 3 messages, got %d", mb.Len())
	}
}

func TestMailboxDropOldestStrategy(t *testing.T) {
	mb := NewMailbox(&types.MailboxConfig{
		Capacity:         3,
		OverflowStrategy: types.OverflowDropOldest,
	})

	// Fill mailbox
	for i := 0; i < 3; i++ {
		_ = mb.Push(Message{ID: string(rune('a' + i))})
	}

	// Push one more - should drop oldest
	_ = mb.Push(Message{ID: "d"})

	// First message should be 'b' (oldest 'a' was dropped)
	msg, ok := mb.TryPop()
	if !ok {
		t.Fatal("expected message")
	}
	if msg.ID != "b" {
		t.Errorf("expected 'b', got '%s'", msg.ID)
	}
}

func TestMailboxConcurrency(t *testing.T) {
	mb := NewMailbox(&types.MailboxConfig{
		Capacity:         1000,
		OverflowStrategy: types.OverflowDropNewest,
	})

	var wg sync.WaitGroup
	numWriters := 10
	numMessages := 100

	// Writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < numMessages; j++ {
				_ = mb.Push(Message{ID: string(rune(writerID*100 + j))})
			}
		}(i)
	}

	// Reader
	received := 0
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				if _, ok := mb.TryPop(); ok {
					received++
				}
			}
		}
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	close(done)

	// Drain remaining
	remaining := mb.Drain()
	total := received + len(remaining)

	// Should have received all or some were dropped
	if total > numWriters*numMessages {
		t.Errorf("received more than sent: %d > %d", total, numWriters*numMessages)
	}
}

func BenchmarkMailboxPush(b *testing.B) {
	mb := NewMailbox(&types.MailboxConfig{
		Capacity:         100000,
		OverflowStrategy: types.OverflowDropNewest,
	})

	msg := Message{
		ID:        "test",
		Type:      MessageTypeData,
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mb.Push(msg)
	}
}

func BenchmarkMailboxPushPop(b *testing.B) {
	mb := NewMailbox(&types.MailboxConfig{
		Capacity:         100000,
		OverflowStrategy: types.OverflowDropNewest,
	})

	msg := Message{
		ID:        "test",
		Type:      MessageTypeData,
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mb.Push(msg)
		mb.TryPop()
	}
}
