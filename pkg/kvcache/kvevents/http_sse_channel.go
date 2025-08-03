/*
Copyright 2025 The llm-d Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this f		// Parse SSE format
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		case strings.HasPrefix(line, "id:"):
			idStr := strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
				seq = id
			}
		case line == "":
			// Empty line indicates end of event
			if eventType != "" && data != "" {
				h.processSSEEvent(ctx, eventType, data, seq)
				// Reset for next event
				eventType, data = "", ""
				seq = 0
			}
		}iance with the License.
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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils/logging"
)

// HTTPSSEChannel implements the Channel interface using HTTP Server-Sent Events.
// This is useful for scenarios where ZMQ is not available or when HTTP-based
// communication is preferred.
type HTTPSSEChannel struct {
	pool     *Pool
	endpoint string
	client   *http.Client
}

// NewHTTPSSEChannel creates a new HTTP SSE-based channel implementation.
func NewHTTPSSEChannel(pool *Pool, endpoint string) Channel {
	return &HTTPSSEChannel{
		pool:     pool,
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 0, // No timeout for SSE connections
		},
	}
}

// Start connects to an HTTP SSE endpoint and listens for events.
func (h *HTTPSSEChannel) Start(ctx context.Context) {
	logger := klog.FromContext(ctx).WithName("http-sse-channel")

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down http-sse-channel")
			return
		default:
			h.connectAndListen(ctx)
			// Wait before retrying connection
			select {
			case <-time.After(retryInterval):
				logger.Info("retrying http-sse-channel connection")
			case <-ctx.Done():
				logger.Info("shutting down http-sse-channel")
				return
			}
		}
	}
}

// Close gracefully shuts down the HTTP SSE channel.
func (h *HTTPSSEChannel) Close() error {
	// HTTP client connections are managed by the Go runtime
	return nil
}

// connectAndListen establishes the SSE connection and processes events.
func (h *HTTPSSEChannel) connectAndListen(ctx context.Context) {
	logger := klog.FromContext(ctx).WithName("http-sse-channel")
	debugLogger := logger.V(logging.DEBUG)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.endpoint, http.NoBody)
	if err != nil {
		logger.Error(err, "Failed to create HTTP request", "endpoint", h.endpoint)
		return
	}

	// Set headers for SSE
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := h.client.Do(req)
	if err != nil {
		logger.Error(err, "Failed to connect to SSE endpoint", "endpoint", h.endpoint)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error(nil, "SSE endpoint returned error status", "endpoint", h.endpoint, "status", resp.StatusCode)
		return
	}

	logger.Info("Connected to SSE endpoint", "endpoint", h.endpoint)

	scanner := bufio.NewScanner(resp.Body)
	var eventType, data string
	var seq uint64

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		// Parse SSE format
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		} else if strings.HasPrefix(line, "id:") {
			idStr := strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
				seq = id
			}
		} else if line == "" {
			// Empty line indicates end of event
			if eventType != "" && data != "" {
				h.processSSEEvent(ctx, eventType, data, seq)
				// Reset for next event
				eventType, data = "", ""
				seq++
			}
		}
	}

	if err := scanner.Err(); err != nil {
		debugLogger.Error(err, "Error reading from SSE stream", "endpoint", h.endpoint)
	}
}

// processSSEEvent processes a single SSE event and converts it to a Message.
func (h *HTTPSSEChannel) processSSEEvent(ctx context.Context, eventType, data string, seq uint64) {
	debugLogger := klog.FromContext(ctx).V(logging.DEBUG)

	// Parse the event data (assuming JSON format)
	var eventData struct {
		Topic         string `json:"topic"`
		PodIdentifier string `json:"podIdentifier"`
		ModelName     string `json:"modelName"`
		Payload       string `json:"payload"` // Base64 encoded payload
	}

	if err := json.Unmarshal([]byte(data), &eventData); err != nil {
		debugLogger.Error(err, "Failed to parse SSE event data", "eventType", eventType, "data", data)
		return
	}

	// Decode the payload (assuming base64)
	// For simplicity, we'll just use the data as-is for now
	payload := []byte(eventData.Payload)

	debugLogger.Info("Received SSE event",
		"eventType", eventType,
		"topic", eventData.Topic,
		"seq", seq,
		"podIdentifier", eventData.PodIdentifier,
		"modelName", eventData.ModelName,
		"payloadSize", len(payload))

	h.pool.AddTask(&Message{
		Topic:         eventData.Topic,
		Payload:       payload,
		Seq:           seq,
		PodIdentifier: eventData.PodIdentifier,
		ModelName:     eventData.ModelName,
	})
}

// HTTPSSEPublisher implements the Publisher interface using HTTP POST requests.
type HTTPSSEPublisher struct {
	endpoint string
	client   *http.Client
}

// NewHTTPSSEPublisher creates a new HTTP SSE publisher.
func NewHTTPSSEPublisher(endpoint string) Publisher {
	return &HTTPSSEPublisher{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// PublishEvent publishes an event via HTTP POST to the SSE server.
func (h *HTTPSSEPublisher) PublishEvent(ctx context.Context, topic string, batch interface{}) error {
	// Convert the event to JSON
	eventData := map[string]interface{}{
		"topic": topic,
		"batch": batch,
	}

	jsonData, err := json.Marshal(eventData)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
	}

	return nil
}

// Close closes the HTTP SSE publisher.
func (h *HTTPSSEPublisher) Close() error {
	return nil
}
