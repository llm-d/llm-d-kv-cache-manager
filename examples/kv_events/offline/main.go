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
	_ "embed"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/llm-d/llm-d-kv-cache-manager/examples/helper"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/examples/testdata"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
)

const (
	envHFToken = "HF_TOKEN"
)

func getKVCacheIndexerConfig() *kvcache.Config {
	config := kvcache.NewDefaultConfig()

	huggingFaceToken := os.Getenv(envHFToken)
	if huggingFaceToken != "" {
		config.TokenizersPoolConfig.HuggingFaceToken = huggingFaceToken
	}

	config.TokenProcessorConfig.BlockSize = 256

	return config
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
	eventsPool := helper.SetupEventsPool(ctx, kvCacheIndexer.KVBlockIndex())

	// Start events pool
	eventsPool.Start(ctx)
	logger.Info("Events pool started and listening for ZMQ messages")

	// Setup ZMQ publisher to simulate vLLM engines
	publisher, err := helper.SetupPublisher(ctx)
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
	if err := RunEventsDemo(ctx, kvCacheIndexer, publisher); err != nil {
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

	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(ctx, getKVCacheIndexerConfig())
	if err != nil {
		return nil, err
	}

	logger.Info("Created Indexer")

	go kvCacheIndexer.Run(ctx)
	logger.Info("Started Indexer", "model", testdata.ModelName)

	return kvCacheIndexer, nil
}

func RunEventsDemo(ctx context.Context, kvCacheIndexer *kvcache.Indexer, publisher *helper.Publisher) error {
	logger := klog.FromContext(ctx)

	logger.Info("@@@ Starting KV Events Demo", "model", testdata.ModelName)

	// Initial query - should be empty since no events have been published
	pods, err := kvCacheIndexer.GetPodScores(ctx, testdata.Prompt, testdata.ModelName, nil)
	if err != nil {
		return err
	}
	logger.Info("@@@ Initial pod scores (should be empty)", "pods", pods)

	// Simulate vLLM engine publishing BlockStored events
	err = helper.SimulateProduceEvent(ctx, publisher)
	if err != nil {
		return err
	}

	// Query again to see the effect of the events
	pods, err = kvCacheIndexer.GetPodScores(ctx, testdata.Prompt, testdata.ModelName, nil)
	if err != nil {
		return err
	}
	logger.Info("@@@ Pod scores after BlockStored events", "pods", pods)

	// Simulate removing some blocks
	err = helper.SimulateRemoveEvent(ctx, publisher)
	if err != nil {
		return err
	}

	// Final query
	pods, err = kvCacheIndexer.GetPodScores(ctx, testdata.Prompt, testdata.ModelName, nil)
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
