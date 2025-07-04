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

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvevents"
)

//nolint:lll // need prompt as-is, chunking to string concatenation is too much of a hassle
const (
	prompt           = `lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Curabitur pretium tincidunt lacus. Nulla gravida orci a odio. Nullam varius, turpis et commodo pharetra, est eros bibendum elit, nec luctus magna felis sollicitudin mauris. Integer in mauris eu nibh euismod gravida. Duis ac tellus et risus vulputate vehicula. Donec lobortis risus a elit. Etiam tempor. Ut ullamcorper, ligula eu tempor congue, eros est euismod turpis, id tincidunt sapien risus a quam. Maecenas fermentum consequat mi. Donec fermentum. Pellentesque malesuada nulla a mi. Duis sapien sem, aliquet nec, commodo eget, consequat quis, neque. Aliquam faucibus, elit ut dictum aliquet, felis nisl adipiscing sapien, sed malesuada diam lacus eget erat. Cras mollis scelerisque nunc. Nullam arcu. Aliquam consequat. Curabitur augue lorem, dapibus quis, laoreet et, pretium ac, nisi. Aenean magna nisl, mollis quis, molestie eu, feugiat in, orci. In hac habitasse platea dictumst. sunt in culpa qui officia deserunt mollit anim id est laborum. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Curabitur pretium tincidunt lacus. Nulla gravida orci a odio. Nullam varius, turpis et commodo pharetra, est eros bibendum elit, nec luctus magna felis sollicitudin mauris. Integer in mauris eu nibh euismod gravida. Duis ac tellus et risus vulputate vehicula. Donec lobortis risus a elit. Etiam tempor. Ut ullamcorper, ligula eu tempor congue, eros est euismod turpis, id tincidunt sapien risus a quam. Maecenas fermentum consequat mi. Donec fermentum. Pellentesque malesuada nulla a mi. Duis sapien sem, aliquet nec, commodo eget, consequat quis, neque. Aliquam faucibus, elit ut dictum aliquet, felis nisl adipiscing sapien, sed malesuada diam lacus eget erat. Cras mollis scelerisque nunc. Nullam arcu. Aliquam consequat. Curabitur augue lorem, dapibus quis, laoreet et, pretium ac, nisi. Aenean magna nisl, mollis quis, molestie eu, feugiat in, orci. In hac habitasse platea dictumst.`
	blockHash1       = 7116574867160041607
	blockHash2       = 9824230139929555794
	blockHash3       = 3874749374659795295
	defaultModelName = "bert-base-uncased"

	envHFToken         = "HF_TOKEN"
	envModelName       = "MODEL_NAME"
	envZMQEndpoint     = "ZMQ_ENDPOINT"
	envZMQTopic        = "ZMQ_TOPIC"
	envPoolConcurrency = "POOL_CONCURRENCY"

	defaultZMQEndpoint = "tcp://localhost:5557"
	defaultZMQTopic    = "kv@"
	defaultConcurrency = 4
)

func getKVCacheIndexerConfig() *kvcache.Config {
	config := kvcache.NewDefaultConfig()

	huggingFaceToken := os.Getenv(envHFToken)
	if huggingFaceToken != "" {
		config.TokenizersPoolConfig.HuggingFaceToken = huggingFaceToken
	}

	return config
}

func getModelName() string {
	modelName := os.Getenv(envModelName)
	if modelName != "" {
		return modelName
	}

	return defaultModelName
}

func getEventsPoolConfig() *kvevents.Config {
	concurrency := defaultConcurrency
	if envConcurrency := os.Getenv(envPoolConcurrency); envConcurrency != "" {
		if c, err := strconv.Atoi(envConcurrency); err == nil && c > 0 {
			concurrency = c
		}
	}

	zmqEndpoint := os.Getenv(envZMQEndpoint)
	if zmqEndpoint == "" {
		zmqEndpoint = defaultZMQEndpoint
	}

	zmqTopic := os.Getenv(envZMQTopic)
	if zmqTopic == "" {
		zmqTopic = defaultZMQTopic
	}

	return &kvevents.Config{
		Concurrency: concurrency,
		ZMQEndpoint: zmqEndpoint,
		TopicFilter: zmqTopic,
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := klog.FromContext(ctx)
	logger.Info("Starting KV Events Pool Example")

	kvCacheIndexer, err := setupKVCacheIndexer(ctx)
	if err != nil {
		logger.Error(err, "failed to setup KVCacheIndexer")
		return
	}

	// Setup events pool with ZMQ subscriber
	eventsPool := setupEventsPool(ctx, kvCacheIndexer.KVBlockIndex())

	// Start events pool
	eventsPool.Start(ctx)
	logger.Info("Events pool started and listening for ZMQ messages")

	// Setup ZMQ publisher to simulate vLLM engines
	publisher, err := setupPublisher(ctx)
	if err != nil {
		logger.Error(err, "failed to setup ZMQ publisher")
		return
	}
	defer publisher.Close()

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Run the demonstration
	if err := runEventsDemo(ctx, kvCacheIndexer, publisher); err != nil {
		logger.Error(err, "failed to run events demo")
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

	//nolint:contextcheck // NewKVCacheIndexer does not accept context parameter
	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(getKVCacheIndexerConfig())
	if err != nil {
		return nil, err
	}

	logger.Info("Created Indexer")

	go kvCacheIndexer.Run(ctx)
	modelName := getModelName()
	logger.Info("Started Indexer", "model", modelName)

	return kvCacheIndexer, nil
}

func setupEventsPool(ctx context.Context, kvBlockIndex kvblock.Index) *kvevents.Pool {
	logger := klog.FromContext(ctx)

	cfg := getEventsPoolConfig()

	logger.Info("Creating events pool", "config", cfg)
	pool := kvevents.NewPool(cfg, kvBlockIndex)

	return pool
}

func setupPublisher(ctx context.Context) (*Publisher, error) {
	logger := klog.FromContext(ctx)

	cfg := getEventsPoolConfig()

	logger.Info("Creating ZMQ publisher (simulating vLLM engines)", "endpoint", cfg.ZMQEndpoint)

	publisher, err := NewPublisher(cfg.ZMQEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create ZMQ publisher: %w", err)
	}

	logger.Info("ZMQ publisher created successfully")
	return publisher, nil
}

func runEventsDemo(ctx context.Context, kvCacheIndexer *kvcache.Indexer, publisher *Publisher) error {
	logger := klog.FromContext(ctx)

	modelName := getModelName()
	logger.Info("@@@ Starting KV Events Demo", "model", modelName)

	// Initial query - should be empty since no events have been published
	pods, err := kvCacheIndexer.GetPodScores(ctx, prompt, modelName, nil)
	if err != nil {
		return err
	}
	logger.Info("@@@ Initial pod scores (should be empty)", "pods", pods)

	// Give the subscriber a moment to connect
	time.Sleep(1 * time.Second)

	// Simulate vLLM engine publishing BlockStored events
	logger.Info("@@@ Simulating vLLM engine publishing BlockStored events...")

	//nolint // won't fail
	blockStoredPayloadBytes, _ := msgpack.Marshal(kvevents.BlockStored{
		BlockHashes: []uint64{blockHash1, blockHash2, blockHash3},
	})
	dpRank := 0

	eventBatch := kvevents.EventBatch{
		TS:               float64(time.Now().UnixNano()) / 1e9,
		Events:           []msgpack.RawMessage{blockStoredPayloadBytes},
		DataParallelRank: &dpRank,
	}

	topic := fmt.Sprintf("kv@vllm-pod1@%s", modelName)
	if err := publisher.PublishEvent(ctx, topic, eventBatch); err != nil {
		return fmt.Errorf("failed to publish BlockStored event: %w", err)
	}
	logger.Info("@@@ Published BlockStored event", "topic", topic, "blocks", 3)

	// Wait for events to be processed by the pool
	logger.Info("@@@ Waiting for events to be processed...")
	time.Sleep(3 * time.Second)

	// Query again to see the effect of the events
	pods, err = kvCacheIndexer.GetPodScores(ctx, prompt, modelName, nil)
	if err != nil {
		return err
	}
	logger.Info("@@@ Pod scores after BlockStored events", "pods", pods)

	// Simulate removing some blocks
	logger.Info("@@@ Simulating vLLM engine removing some blocks...")

	//nolint // won't fail
	blockRemovedPayloadBytes, _ := msgpack.Marshal(kvevents.BlockRemoved{
		BlockHashes: []uint64{blockHash1, blockHash2},
	})

	removeEventBatch := kvevents.EventBatch{
		TS:               float64(time.Now().UnixNano()) / 1e9,
		Events:           []msgpack.RawMessage{blockRemovedPayloadBytes},
		DataParallelRank: &dpRank,
	}

	if err := publisher.PublishEvent(ctx, topic, removeEventBatch); err != nil {
		return fmt.Errorf("failed to publish BlockRemoved event: %w", err)
	}
	logger.Info("@@@ Published BlockRemoved event", "topic", topic, "blocks", 2)

	// Wait for removal events to be processed
	time.Sleep(3 * time.Second)

	// Final query
	pods, err = kvCacheIndexer.GetPodScores(ctx, prompt, modelName, nil)
	if err != nil {
		return err
	}
	logger.Info("@@@ Final pod scores after BlockRemoved events", "pods", pods)

	logger.Info("Events demo completed. Pool continues listening for more events...")
	logger.Info("Press Ctrl+C to shutdown")

	// Keep running until context is cancelled
	<-ctx.Done()
	return nil
}
