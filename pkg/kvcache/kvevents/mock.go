/*
Copyright 2025 The llm-d Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kvevents

import (
	"context"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

// MockChannel is a test implementation of the Channel interface.
// It allows for controlled message injection for testing purposes.
type MockChannel struct {
	pool     *Pool
	messages chan *Message
	closed   bool
	mu       sync.RWMutex
}

// NewMockChannel creates a new mock channel for testing.
func NewMockChannel(pool *Pool) *MockChannel {
	return &MockChannel{
		pool:     pool,
		messages: make(chan *Message, 100), // Buffered channel for testing
		closed:   false,
	}
}

// SetPool sets the pool reference for the mock channel.
func (m *MockChannel) SetPool(pool *Pool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pool = pool
}

// Start begins listening for messages from the internal channel.
func (m *MockChannel) Start(ctx context.Context) {
	logger := klog.FromContext(ctx).WithName("mock-channel")
	logger.Info("Starting mock channel")

	for {
		select {
		case <-ctx.Done():
			logger.Info("Shutting down mock channel")
			return
		case msg, ok := <-m.messages:
			if !ok {
				logger.Info("Mock channel closed, shutting down")
				return
			}
			// Check if pool is set before adding task
			m.mu.RLock()
			pool := m.pool
			m.mu.RUnlock()

			if pool != nil {
				logger.V(5).Info("Adding message to pool", "topic", msg.Topic, "seq", msg.Seq)
				pool.AddTask(msg)
			} else {
				logger.Info("Pool is nil, dropping message", "topic", msg.Topic)
			}
		}
	}
}

// Close gracefully shuts down the mock channel.
func (m *MockChannel) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		close(m.messages)
		m.closed = true
	}
	return nil
}

// SendMessage sends a message through the mock channel for testing.
func (m *MockChannel) SendMessage(msg *Message) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.closed {
		select {
		case m.messages <- msg:
		case <-time.After(time.Second):
			// Timeout to prevent tests from hanging
		}
	}
}

// MockPublisher is a test implementation of the Publisher interface.
type MockPublisher struct {
	events []PublishedEvent
	mu     sync.RWMutex
}

// PublishedEvent represents an event that was published for testing verification.
type PublishedEvent struct {
	Topic string
	Batch interface{}
}

// NewMockPublisher creates a new mock publisher for testing.
func NewMockPublisher() *MockPublisher {
	return &MockPublisher{
		events: make([]PublishedEvent, 0),
	}
}

// PublishEvent records the event for testing verification.
func (m *MockPublisher) PublishEvent(_ context.Context, topic string, batch interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.events = append(m.events, PublishedEvent{
		Topic: topic,
		Batch: batch,
	})
	return nil
}

// Close closes the mock publisher.
func (m *MockPublisher) Close() error {
	return nil
}

// GetPublishedEvents returns all events that were published (for testing verification).
func (m *MockPublisher) GetPublishedEvents() []PublishedEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	events := make([]PublishedEvent, len(m.events))
	copy(events, m.events)
	return events
}

// Reset clears all recorded events.
func (m *MockPublisher) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.events = m.events[:0]
}
