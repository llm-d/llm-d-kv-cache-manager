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

//nolint:testpackage // allow tests to run in the same package
package e2e

import (
	"context"
	"testing"

	kvblock "github.com/llm-d/llm-d-kv-cache-manager/pkg/kv-cache/kv-block"

	kvcache "github.com/llm-d/llm-d-kv-cache-manager/pkg/kv-cache"

	"github.com/alicebob/miniredis/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"
)

const (
	defaultModelName = "google-bert/bert-base-uncased"
)

// KVCacheSuite defines a testify test suite for end-to-end testing of the KVCache indexer.
// It uses a mock Redis server (miniredis) and a tokenizer to simulate and verify cache behavior.
type KVCacheSuite struct {
	suite.Suite

	ctx             context.Context
	cancel          context.CancelFunc
	server          *miniredis.Miniredis
	rdb             *redis.Client
	tokenizer       tokenization.Tokenizer
	tokensProcessor kvcache.TokenProcessor
	config          *kvcache.Config
	indexer         *kvcache.Indexer
	Pod1IP          string
}

// SetupTest initializes the mock Redis, tokenizer, config, and starts the indexer before each test.
func (s *KVCacheSuite) SetupTest() {
	s.ctx, s.cancel = context.WithCancel(context.Background())

	var err error
	s.server, err = miniredis.Run()
	s.Require().NoError(err)

	s.config = kvcache.NewDefaultConfig()
	s.config.KVBlockIndexerConfig.RedisOpt = &redis.Options{
		Addr: s.server.Addr(),
	}
	s.config.PrefixStoreConfig.BlockSize = 4
	s.config.TokenProcessorConfig.ChunkSize = 4

	s.rdb = redis.NewClient(&redis.Options{Addr: s.server.Addr()})
	s.tokenizer, err = tokenization.NewCachedHFTokenizer(s.config.TokenizersPoolConfig.HFTokenizerConfig)
	s.Require().NoError(err)

	s.tokensProcessor = kvcache.NewChunkedTokenDatabase(s.config.TokenProcessorConfig)

	s.Pod1IP = "10.0.0.1"

	s.indexer, err = kvcache.NewKVCacheIndexer(s.config)
	s.Require().NoError(err)

	go s.indexer.Run(s.ctx)
}

// TearDownTest cleans up resources and stops the mock Redis after each test.
func (s *KVCacheSuite) TearDownTest() {
	s.cancel()
	if s.server != nil {
		s.server.Close()
	}
}

// promptToRedisKeys tokenizes a prompt and returns its corresponding KV block keys.
//
//nolint:unparam // allow future support for multiple models
func (s *KVCacheSuite) promptToRedisKeys(prompt, model string) []kvblock.Key {
	tokens, _, err := s.tokenizer.Encode(prompt, model)
	s.Require().NoError(err)

	blockKeys := s.tokensProcessor.TokensToKVBlockKeys(tokens, model)
	s.Require().NotEmpty(blockKeys)

	return blockKeys
}

// setRedisMockEntries inserts KV block keys into mock Redis mapped to pod IPs.
func (s *KVCacheSuite) setRedisMockEntries(blockKeys []kvblock.Key, podList []string) {
	s.Require().NotEmpty(blockKeys)

	redisKeys := make([]string, len(blockKeys))
	for i, blockKey := range blockKeys {
		redisKey := blockKey.String()
		redisKeys[i] = redisKey
		s.server.HSet(redisKey, podList[0]+":80", "doesn't matter")
	}
}

// TestKVCacheSuite runs the KVCacheSuite using testify's suite runner.
func TestKVCacheSuite(t *testing.T) {
	suite.Run(t, new(KVCacheSuite))
}
