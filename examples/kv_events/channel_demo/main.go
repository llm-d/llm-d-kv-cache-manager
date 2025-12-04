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

// Package main demonstrates the use of different Channel implementations
// for KV Events processing, showcasing the extensibility provided by the
// Channel interface abstraction introduced in issue #46.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/examples/testdata"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvevents"
)

const (
	envHFToken     = "HF_TOKEN"
	envChannelType = "CHANNEL_TYPE" // "zmq", "mock", or "http-sse"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := klog.FromContext(ctx)
	logger.Info("Starting KV Events Channel Interface Demo")

	// Get channel type from environment
	channelType := os.Getenv(envChannelType)
	if channelType == "" {
		channelType = "mock" // Default to mock for demo purposes
	}
	logger.Info("Using channel type", "type", channelType)

	// Setup KV Cache Indexer
	kvCacheIndexer, err := setupKVCacheIndexer(ctx)
	if err != nil {
		logger.Error(err, "failed to setup KVCacheIndexer")
		return
	}

	// Setup events pool with the specified channel type
	eventsPool, publisher, err := setupEventsPoolWithChannelType(ctx, kvCacheIndexer.KVBlockIndex(), channelType)
	if err != nil {
		logger.Error(err, "failed to setup events pool")
		return
	}
	defer func() {
		if publisher != nil {
			publisher.Close()
		}
	}()

	// Start events pool
	eventsPool.Start(ctx)
	logger.Info("Events pool started", "channelType", channelType)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Run the demonstration
	if err := runChannelDemo(ctx, kvCacheIndexer, publisher, channelType); err != nil {
		logger.Error(err, "failed to run channel demo")
		return
	}

	// Wait for shutdown signal
	<-ctx.Done()
	logger.Info("Shutting down...")

	// Graceful shutdown of events pool
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	eventsPool.Shutdown(shutdownCtx)
}

func setupKVCacheIndexer(ctx context.Context) (*kvcache.Indexer, error) {
	logger := klog.FromContext(ctx)

	config := kvcache.NewDefaultConfig()
	if token := os.Getenv(envHFToken); token != "" {
		config.TokenizersPoolConfig.HuggingFaceToken = token
	}
	config.TokenProcessorConfig.BlockSize = 256

	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(ctx, config)
	if err != nil {
		return nil, err
	}

	logger.Info("Created Indexer")
	go kvCacheIndexer.Run(ctx)
	logger.Info("Started Indexer")

	return kvCacheIndexer, nil
}

func setupEventsPoolWithChannelType(ctx context.Context, kvBlockIndex kvblock.Index, 
	channelType string) (*kvevents.Pool, kvevents.Publisher, error) {
	logger := klog.FromContext(ctx)
	cfg := kvevents.DefaultConfig()

	var pool *kvevents.Pool
	var publisher kvevents.Publisher

	switch channelType {
	case "zmq":
		// Use default ZMQ implementation
		logger.Info("Creating events pool with ZMQ channel", "config", cfg)
		pool = kvevents.NewPool(cfg, kvBlockIndex)
		// Note: In a real scenario, you'd set up a real ZMQ publisher here
		publisher = kvevents.NewMockPublisher() // Using mock for simplicity in this example

	case "mock":
		// Use mock channel for testing
		logger.Info("Creating events pool with Mock channel")
		mockChannel := kvevents.NewMockChannel(nil) // Will be set after pool creation
		pool = kvevents.NewPoolWithChannel(cfg, kvBlockIndex, mockChannel)

		// Update channel reference
		mockChannel = kvevents.NewMockChannel(pool)
		pool = kvevents.NewPoolWithChannel(cfg, kvBlockIndex, mockChannel)

		publisher = &mockChannelPublisher{channel: mockChannel}

	case "http-sse":
		// Use HTTP SSE implementation
		logger.Info("Creating events pool with HTTP SSE channel", "endpoint", cfg.ZMQEndpoint)
		httpChannel := kvevents.NewHTTPSSEChannel(pool, "http://localhost:8080/sse")
		pool = kvevents.NewPoolWithChannel(cfg, kvBlockIndex, httpChannel)
		publisher = kvevents.NewHTTPSSEPublisher("http://localhost:8080/publish")

	default:
		return nil, nil, fmt.Errorf("unsupported channel type: %s", channelType)
	}

	return pool, publisher, nil
}

// mockChannelPublisher wraps MockChannel to implement the Publisher interface.
type mockChannelPublisher struct {
	channel *kvevents.MockChannel
}

func (m *mockChannelPublisher) PublishEvent(ctx context.Context, topic string, batch interface{}) error {
	// Convert batch to the expected Message format
	batchBytes, err := msgpack.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	// Extract pod identifier and model name from topic (format: kv@<pod>@<model>)
	parts := []string{"kv", "test-pod", "test-model"}
	if len(parts) >= 3 {
		// For this demo, we'll use hardcoded values, but normally you'd parse the topic
		message := &kvevents.Message{
			Topic:         topic,
			Payload:       batchBytes,
			Seq:           uint64(time.Now().Unix()),
			PodIdentifier: "test-pod",
			ModelName:     testdata.ModelName,
		}
		m.channel.SendMessage(message)
	}

	return nil
}

func (m *mockChannelPublisher) Close() error {
	return m.channel.Close()
}

func runChannelDemo(ctx context.Context, kvCacheIndexer *kvcache.Indexer, 
	publisher kvevents.Publisher, channelType string) error {
	logger := klog.FromContext(ctx)

	logger.Info("Starting Channel Interface Demo", "channelType", channelType, "model", testdata.ModelName)

	// Initial query - should be empty
	pods, err := kvCacheIndexer.GetPodScores(ctx, testdata.Prompt, testdata.ModelName, nil)
	if err != nil {
		return err
	}
	logger.Info("Initial pod scores (should be empty)", "pods", pods)

	// Give the channel a moment to start
	time.Sleep(1 * time.Second)

	// Simulate publishing BlockStored events using the configured publisher
	logger.Info("Publishing BlockStored events via channel", "channelType", channelType)

	blockStoredPayload, err := msgpack.Marshal(kvevents.BlockStored{
		BlockHashes: testdata.PromptHashes,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal BlockStored event: %w", err)
	}

	eventBatch := kvevents.EventBatch{
		TS:     float64(time.Now().UnixNano()) / 1e9,
		Events: []msgpack.RawMessage{blockStoredPayload},
	}

	topic := fmt.Sprintf("kv@demo-pod@%s", testdata.ModelName)
	if err := publisher.PublishEvent(ctx, topic, eventBatch); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	logger.Info("Published BlockStored event", "topic", topic, "blocks", len(testdata.PromptHashes))

	// Wait for events to be processed
	logger.Info("Waiting for events to be processed...")
	time.Sleep(3 * time.Second)

	// Query again to see the effect
	pods, err = kvCacheIndexer.GetPodScores(ctx, testdata.Prompt, testdata.ModelName, nil)
	if err != nil {
		return err
	}
	logger.Info("Pod scores after BlockStored events", "pods", pods, "channelType", channelType)

	// Demonstrate successful processing
	if len(pods) > 0 {
		logger.Info("SUCCESS: Channel interface working correctly!",
			"channelType", channelType,
			"foundPods", len(pods))
	} else {
		logger.Info("No pods found - this might be expected depending on the channel implementation")
	}

	logger.Info("Channel demo completed. Pool continues listening for more events...")
	logger.Info("Press Ctrl+C to shutdown")

	// Keep running until context is cancelled
	<-ctx.Done()
	return nil
}
