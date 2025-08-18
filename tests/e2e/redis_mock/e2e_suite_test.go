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

	"github.com/stretchr/testify/suite"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils"
)

const (
	defaultModelName = "google-bert/bert-base-uncased"
)

// KVCacheSuite defines a testify test suite for end-to-end testing of the KVCache indexer.
// It uses a mock Redis server (miniredis) and a tokenizer to simulate and verify cache behavior.
type KVCacheSuite struct {
	suite.Suite

	ctx    context.Context
	cancel context.CancelFunc

	tokenizer       tokenization.Tokenizer
	tokensProcessor kvblock.TokenProcessor
	config          *kvcache.Config

	kvBlockIndex kvblock.Index
	indexer      *kvcache.Indexer // TODO: test for all index backends

	Pod1IP string
}

// SetupTest initializes the mock Redis, tokenizer, config, and starts the indexer before each test.
func (s *KVCacheSuite) SetupTest() {
	s.ctx, s.cancel = context.WithCancel(context.Background())

	var err error
	s.Require().NoError(err)

	s.config = kvcache.NewDefaultConfig()
	s.config.PrefixStoreConfig.BlockSize = 4
	s.config.TokenProcessorConfig.BlockSize = 4

	s.tokenizer, err = tokenization.NewCachedHFTokenizer(s.config.TokenizersPoolConfig.HFTokenizerConfig)
	s.Require().NoError(err)

	s.tokensProcessor = kvblock.NewChunkedTokenDatabase(s.config.TokenProcessorConfig)

	s.Pod1IP = "10.0.0.1"

	s.indexer, err = kvcache.NewKVCacheIndexer(s.ctx, s.config)
	s.kvBlockIndex = s.indexer.KVBlockIndex()
	s.Require().NoError(err)

	go s.indexer.Run(s.ctx)
}

// promptToKeys tokenizes a prompt and returns its corresponding KV block keys.
func (s *KVCacheSuite) promptToKeys(prompt, model string) []kvblock.Key {
	tokens, _, err := s.tokenizer.Encode(prompt, model)
	s.Require().NoError(err)

	blockKeys := s.tokensProcessor.TokensToKVBlockKeys(tokens, model)
	s.Require().NotEmpty(blockKeys)

	return blockKeys
}

func (s *KVCacheSuite) addEntriesToIndex(blockKeys []kvblock.Key, podList []string) {
	s.Require().NotEmpty(blockKeys)

	// Add entries to the indexer
	err := s.kvBlockIndex.Add(s.ctx, blockKeys, utils.SliceMap(podList, func(pod string) kvblock.PodEntry {
		return kvblock.PodEntry{
			PodIdentifier: pod,
			DeviceTier:    "gpu",
		}
	}))
	s.Require().NoError(err)
}

// TestKVCacheSuite runs the KVCacheSuite using testify's suite runner.
func TestKVCacheSuite(t *testing.T) {
	suite.Run(t, new(KVCacheSuite))
}
