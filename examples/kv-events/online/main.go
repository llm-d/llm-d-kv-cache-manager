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

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvevents"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
)

//nolint:lll // need prompt as-is, chunking to string concatenation is too much of a hassle
const (
	envHFToken     = "HF_TOKEN"
	envModelName   = "MODEL_NAME"
	envZMQEndpoint = "ZMQ_ENDPOINT"
	envZMQTopic    = "ZMQ_TOPIC"

	envPoolConcurrency = "POOL_CONCURRENCY"
	defaultZMQEndpoint = "tcp://localhost:5557"
	defaultZMQTopic    = "kv@"
	defaultConcurrency = 4
)

func getKVCacheIndexerConfig() (*kvcache.Config, error) {
	config := kvcache.NewDefaultConfig()

	huggingFaceToken := os.Getenv(envHFToken)
	if huggingFaceToken != "" {
		config.TokenizersPoolConfig.HuggingFaceToken = huggingFaceToken
	}

	return config, nil
}

func getEventsPoolConfig() (int, string, string) {
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

	return concurrency, zmqEndpoint, zmqTopic
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := klog.FromContext(ctx)
	logger.Info("Starting KV Events Pool Example")

	kvCacheIndexer, err := setupKVCacheIndexer(ctx)
	if err != nil {
		logger.Error(err, "failed to setup KVCacheIndexer")
		os.Exit(1)
	}

	// Setup events pool with ZMQ subscriber
	eventsPool, err := setupEventsPool(ctx, kvCacheIndexer.KVBlockIndex())
	if err != nil {
		logger.Error(err, "failed to setup events pool")
		os.Exit(1)
	}

	// Start events pool
	eventsPool.Start(ctx)
	logger.Info("Events pool started and listening for ZMQ messages")

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Run the demonstration
	if err := runEventsDemo(ctx, kvCacheIndexer); err != nil {
		logger.Error(err, "failed to run events demo")
		os.Exit(1)
	}
}

func setupKVCacheIndexer(ctx context.Context) (*kvcache.Indexer, error) {
	logger := klog.FromContext(ctx)

	config, err := getKVCacheIndexerConfig()
	if err != nil {
		return nil, err
	}

	//nolint:contextcheck // NewKVCacheIndexer does not accept context parameter
	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(config)
	if err != nil {
		return nil, err
	}

	logger.Info("Created Indexer")

	go kvCacheIndexer.Run(ctx)
	logger.Info("Started Indexer")

	return kvCacheIndexer, nil
}

func setupEventsPool(ctx context.Context, kvBlockIndex kvblock.Index) (*kvevents.Pool, error) {
	logger := klog.FromContext(ctx)

	concurrency, zmqEndpoint, zmqTopic := getEventsPoolConfig()

	logger.Info("Creating events pool",
		"concurrency", concurrency,
		"zmqEndpoint", zmqEndpoint,
		"zmqTopic", zmqTopic)

	pool := kvevents.NewPool(concurrency, zmqEndpoint, zmqTopic, kvBlockIndex)
	return pool, nil
}

func runEventsDemo(ctx context.Context, kvCacheIndexer *kvcache.Indexer) error {
	logger := klog.FromContext(ctx)

	modelName := os.Getenv(envModelName)
	logger.Info("Starting KV Events Demo", "model", modelName)

	for {
		// Read a prompt from stdin
		var prompt string
		fmt.Print("Enter prompt: ")
		if _, err := fmt.Scanln(&prompt); err != nil {
			if err.Error() == "unexpected newline" {
				// EOF or empty input, exit the loop
				logger.Info("No prompt entered, exiting")
				break
			}
			return fmt.Errorf("failed to read prompt: %w", err)
		}

		if prompt == "" {
			logger.Info("Empty prompt, exiting")
			break
		}

		pods, err := kvCacheIndexer.GetPodScores(ctx, prompt, modelName, nil)
		if err != nil {
			return err
		}

		logger.Info("Retrieved pods for prompt", "prompt", prompt, "pods", pods)
	}

	return nil
}
