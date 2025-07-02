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
	"time"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/redis/go-redis/v9"

	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
)

//nolint:lll // need prompt as-is, chunking to string concatenation is too much of a hassle
const (
	prompt           = `lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Curabitur pretium tincidunt lacus. Nulla gravida orci a odio. Nullam varius, turpis et commodo pharetra, est eros bibendum elit, nec luctus magna felis sollicitudin mauris. Integer in mauris eu nibh euismod gravida. Duis ac tellus et risus vulputate vehicula. Donec lobortis risus a elit. Etiam tempor. Ut ullamcorper, ligula eu tempor congue, eros est euismod turpis, id tincidunt sapien risus a quam. Maecenas fermentum consequat mi. Donec fermentum. Pellentesque malesuada nulla a mi. Duis sapien sem, aliquet nec, commodo eget, consequat quis, neque. Aliquam faucibus, elit ut dictum aliquet, felis nisl adipiscing sapien, sed malesuada diam lacus eget erat. Cras mollis scelerisque nunc. Nullam arcu. Aliquam consequat. Curabitur augue lorem, dapibus quis, laoreet et, pretium ac, nisi. Aenean magna nisl, mollis quis, molestie eu, feugiat in, orci. In hac habitasse platea dictumst. sunt in culpa qui officia deserunt mollit anim id est laborum. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Curabitur pretium tincidunt lacus. Nulla gravida orci a odio. Nullam varius, turpis et commodo pharetra, est eros bibendum elit, nec luctus magna felis sollicitudin mauris. Integer in mauris eu nibh euismod gravida. Duis ac tellus et risus vulputate vehicula. Donec lobortis risus a elit. Etiam tempor. Ut ullamcorper, ligula eu tempor congue, eros est euismod turpis, id tincidunt sapien risus a quam. Maecenas fermentum consequat mi. Donec fermentum. Pellentesque malesuada nulla a mi. Duis sapien sem, aliquet nec, commodo eget, consequat quis, neque. Aliquam faucibus, elit ut dictum aliquet, felis nisl adipiscing sapien, sed malesuada diam lacus eget erat. Cras mollis scelerisque nunc. Nullam arcu. Aliquam consequat. Curabitur augue lorem, dapibus quis, laoreet et, pretium ac, nisi. Aenean magna nisl, mollis quis, molestie eu, feugiat in, orci. In hac habitasse platea dictumst.`
	blockHash1       = -4485368336996688837
	blockHash2       = 9074243797801914421
	blockHash3       = -7335116218250569597
	defaultModelName = "bert-base-uncased"

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

		config.KVBlockIndexConfig.RedisConfig.RedisOpt = redisOpt
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
		logger.Error(err, "failed to setup KVCacheIndexer")
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

	//nolint:contextcheck // NewKVCacheIndexer does not accept context parameter
	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(config)
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
	pods, err := kvCacheIndexer.GetPodScores(ctx, prompt, modelName, nil)
	if err != nil {
		return err
	}

	// Print the pods - should be empty because no tokenization
	logger.Info("Got pods", "pods", pods)

	// Add entries in kvblock.Index manually
	//nolint // skip linting for this example
	_ = kvCacheIndexer.KVBlockIndex().Add(ctx, []kvblock.Key{
		{modelName, blockHash1},
		{modelName, blockHash2},
		{modelName, blockHash3},
	}, []kvblock.PodEntry{{"pod1", "gpu"}})

	// Sleep 3 secs
	time.Sleep(3 * time.Second)

	// Get pods for the prompt
	pods, err = kvCacheIndexer.GetPodScores(ctx, prompt, modelName, nil)
	if err != nil {
		return err
	}

	// Print the pods - should be empty because no tokenization
	logger.Info("Got pods", "pods", pods)
	return nil
}
