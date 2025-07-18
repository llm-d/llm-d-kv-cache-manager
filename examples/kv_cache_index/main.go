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
	"fmt"
	"os"
	"time"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils"
	"github.com/redis/go-redis/v9"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/examples/testdata"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
)

const (
	defaultModelName = testdata.ModelName

	envRedisAddr = "REDIS_ADDR"
	envHFToken   = "HF_TOKEN"
	envModelName = "MODEL_NAME"
)

func getKVCacheIndexerConfig() (*kvcache.Config, error) {
	config := kvcache.NewDefaultConfig()

	huggingFaceToken := os.Getenv(envHFToken)
	if huggingFaceToken != "" {
		config.TokenizersPoolConfig.HuggingFaceToken = huggingFaceToken
	}

	redisAddr := os.Getenv(envRedisAddr)
	if redisAddr != "" {
		redisOpt, err := redis.ParseURL(redisAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse redis host: %w", err)
		}

		config.KVBlockIndexConfig.RedisConfig.Address = redisOpt.Addr
	} // Otherwise defaults to in-memory indexer

	return config, nil
}

func getModelName() string {
	modelName := os.Getenv(envModelName)
	if modelName != "" {
		return modelName
	}

	return defaultModelName
}

func main() {
	ctx := context.Background()
	logger := klog.FromContext(ctx)

	kvCacheIndexer, err := setupKVCacheIndexer(ctx)
	if err != nil {
		logger.Error(err, "failed to set up KVCacheIndexer")
		os.Exit(1)
	}

	if err := runPrompts(ctx, kvCacheIndexer); err != nil {
		logger.Error(err, "failed to run prompts")
		os.Exit(1)
	}
}

func setupKVCacheIndexer(ctx context.Context) (*kvcache.Indexer, error) {
	logger := klog.FromContext(ctx)

	config, err := getKVCacheIndexerConfig()
	if err != nil {
		return nil, err
	}

	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(ctx, config)
	if err != nil {
		return nil, err
	}

	logger.Info("Created Indexer")

	go kvCacheIndexer.Run(ctx)
	modelName := getModelName()
	logger.Info("Started Indexer", "model", modelName)

	return kvCacheIndexer, nil
}

func runPrompts(ctx context.Context, kvCacheIndexer *kvcache.Indexer) error {
	logger := klog.FromContext(ctx)

	modelName := getModelName()
	logger.Info("Started Indexer", "model", modelName)

	// Get pods for the prompt
	pods, err := kvCacheIndexer.GetPodScores(ctx, testdata.Prompt, modelName, nil)
	if err != nil {
		return err
	}

	// Print the pods - should be empty because no tokenization
	logger.Info("Got pods", "pods", pods)

	// Add entries in kvblock.Index manually
	//nolint // skip linting for this example
	_ = kvCacheIndexer.KVBlockIndex().Add(ctx, utils.SliceMap(testdata.PromptHashes, func(h uint64) kvblock.Key {
		return kvblock.Key{
			ModelName: modelName,
			ChunkHash: h,
		}
	}), []kvblock.PodEntry{{"pod1", "gpu"}})

	// Sleep 3 secs
	time.Sleep(3 * time.Second)

	// Get pods for the prompt
	pods, err = kvCacheIndexer.GetPodScores(ctx, testdata.Prompt, modelName, nil)
	if err != nil {
		return err
	}

	// Print the pods - should be empty because no tokenization
	logger.Info("Got pods", "pods", pods)
	return nil
}
