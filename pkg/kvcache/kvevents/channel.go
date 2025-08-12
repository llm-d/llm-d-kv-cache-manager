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
)

// Channel represents an abstract message channel for KV events.
// This interface allows for different implementations (ZMQ, HTTP SSE, NATS, test mocks, etc.)
// providing extensibility and better testability as suggested in issue #46.
type Channel interface {
	// Start begins listening for messages and forwarding them to the pool.
	// It should run until the provided context is canceled.
	Start(ctx context.Context)

	// Close gracefully shuts down the channel and cleans up resources.
	Close() error
}

// Publisher represents an abstract publisher for KV events.
// This interface allows for different publishing implementations.
type Publisher interface {
	// PublishEvent publishes a KV cache event batch to the specified topic.
	PublishEvent(ctx context.Context, topic string, batch interface{}) error

	// Close closes the publisher and cleans up resources.
	Close() error
}
