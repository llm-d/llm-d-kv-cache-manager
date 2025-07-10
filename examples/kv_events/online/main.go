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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvevents"
)

const (
	envHFToken     = "HF_TOKEN"
	envModelName   = "MODEL_NAME"
	envZMQEndpoint = "ZMQ_ENDPOINT"
	envZMQTopic    = "ZMQ_TOPIC"

	envPoolConcurrency = "POOL_CONCURRENCY"
	defaultZMQEndpoint = "tcp://localhost:5557"
	defaultZMQTopic    = "kv@"
	defaultConcurrency = 4

	pythonHashSeed  = "PYTHONHASHSEED"
	blockSizeEnvVar = "BLOCK_SIZE"

	envHTTPPort     = "HTTP_PORT"
	defaultHTTPPort = "8080"
)

func getKVCacheIndexerConfig() *kvcache.Config {
	config := kvcache.NewDefaultConfig()

	huggingFaceToken := os.Getenv(envHFToken)
	if huggingFaceToken != "" {
		config.TokenizersPoolConfig.HuggingFaceToken = huggingFaceToken
	}

	hashSeed := os.Getenv(pythonHashSeed)
	if hashSeed != "" {
		config.TokenProcessorConfig.HashSeed = hashSeed
	}

	blockSize, err := strconv.Atoi(os.Getenv(blockSizeEnvVar))
	if err == nil || blockSize >= 0 {
		config.TokenProcessorConfig.BlockSize = blockSize
	}

	config.KVBlockIndexConfig.EnableMetrics = true
	config.KVBlockIndexConfig.MetricsLoggingInterval = 15 * time.Second

	return config
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

	eventsPool := setupEventsPool(ctx, kvCacheIndexer.KVBlockIndex())

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

	// Expose HTTP endpoint for prompts
	modelName := os.Getenv(envModelName)
	httpPort := os.Getenv(envHTTPPort)
	if httpPort == "" {
		httpPort = defaultHTTPPort
	}
	http.HandleFunc("/score", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if req.Prompt == "" {
			http.Error(w, "field 'prompt' required", http.StatusBadRequest)
			return
		}

		pods, err := kvCacheIndexer.GetPodScores(ctx, req.Prompt, modelName, nil, false)
		if err != nil {
			http.Error(w, fmt.Sprintf("error: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(pods); err != nil {
			logger.Error(err, "failed to encode response")
		}
	})
	logger.Info("HTTP endpoint /score exposed", "port", httpPort)
	//nolint:gosec // no timeout
	if err := http.ListenAndServe(":"+httpPort, nil); err != nil {
		logger.Error(err, "HTTP server error")
		return
	}
}

func setupKVCacheIndexer(ctx context.Context) (*kvcache.Indexer, error) {
	logger := klog.FromContext(ctx)

	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(ctx, getKVCacheIndexerConfig())
	if err != nil {
		return nil, err
	}

	logger.Info("Created Indexer")

	go kvCacheIndexer.Run(ctx)
	logger.Info("Started Indexer")

	return kvCacheIndexer, nil
}

func setupEventsPool(ctx context.Context, kvBlockIndex kvblock.Index) *kvevents.Pool {
	logger := klog.FromContext(ctx)

	cfg := getEventsPoolConfig()

	logger.Info("Creating events pool", "config", cfg)
	pool := kvevents.NewPool(cfg, kvBlockIndex)

	return pool
}
