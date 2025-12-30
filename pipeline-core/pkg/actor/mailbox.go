package actor

import (
	"errors"
	"sync"

	"github.com/conduix/conduix/shared/types"
)

var (
	ErrMailboxFull   = errors.New("mailbox is full")
	ErrMailboxClosed = errors.New("mailbox is closed")
)

// Mailbox Actor의 메시지 큐
type Mailbox struct {
	capacity         int
	overflowStrategy types.OverflowStrategy
	messages         chan Message
	closed           bool
	mu               sync.RWMutex
}

// NewMailbox 새 Mailbox 생성
func NewMailbox(config *types.MailboxConfig) *Mailbox {
	capacity := 10000
	strategy := types.OverflowBackpressure

	if config != nil {
		if config.Capacity > 0 {
			capacity = config.Capacity
		}
		if config.OverflowStrategy != "" {
			strategy = config.OverflowStrategy
		}
	}

	return &Mailbox{
		capacity:         capacity,
		overflowStrategy: strategy,
		messages:         make(chan Message, capacity),
		closed:           false,
	}
}

// Push 메시지 추가
func (m *Mailbox) Push(msg Message) error {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return ErrMailboxClosed
	}
	m.mu.RUnlock()

	switch m.overflowStrategy {
	case types.OverflowBackpressure:
		// 블로킹 방식으로 메시지 추가
		select {
		case m.messages <- msg:
			return nil
		default:
			// 채널이 가득 찼을 때 블로킹
			m.messages <- msg
			return nil
		}

	case types.OverflowDropOldest:
		// 가장 오래된 메시지 삭제 후 추가
		select {
		case m.messages <- msg:
			return nil
		default:
			// 오래된 메시지 하나 버리고 새 메시지 추가
			select {
			case <-m.messages:
			default:
			}
			m.messages <- msg
			return nil
		}

	case types.OverflowDropNewest:
		// 새 메시지 드롭
		select {
		case m.messages <- msg:
			return nil
		default:
			return ErrMailboxFull
		}

	default:
		select {
		case m.messages <- msg:
			return nil
		default:
			return ErrMailboxFull
		}
	}
}

// Pop 메시지 꺼내기 (블로킹)
func (m *Mailbox) Pop() (Message, bool) {
	msg, ok := <-m.messages
	return msg, ok
}

// TryPop 메시지 꺼내기 (논블로킹)
func (m *Mailbox) TryPop() (Message, bool) {
	select {
	case msg := <-m.messages:
		return msg, true
	default:
		return Message{}, false
	}
}

// Len 현재 메시지 수
func (m *Mailbox) Len() int {
	return len(m.messages)
}

// Close Mailbox 닫기
func (m *Mailbox) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		m.closed = true
		close(m.messages)
	}
}

// IsClosed 닫힘 여부
func (m *Mailbox) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}

// Drain 모든 메시지 꺼내기
func (m *Mailbox) Drain() []Message {
	messages := make([]Message, 0)
	for {
		select {
		case msg, ok := <-m.messages:
			if !ok {
				return messages
			}
			messages = append(messages, msg)
		default:
			return messages
		}
	}
}
