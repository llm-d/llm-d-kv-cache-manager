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
	"strings"
	"time"
)

// TestBasicE2E verifies that the indexer initially returns no scores for the first prompt and
// correct scores for the second request.
func (s *KVCacheSuite) TestBasicE2E() {
	prompt := "What is the capital of France?"
	blockKeys := s.promptToRedisKeys(prompt, defaultModelName)

	fakePodList := []string{s.Pod1IP}

	s.setRedisMockEntries(blockKeys, fakePodList)
	pods, err := s.indexer.GetPodScores(s.ctx, prompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Received pod scores: %+v", pods)
	s.Empty(pods, "expected no pod scores")

	time.Sleep(5 * time.Second)

	pods, err = s.indexer.GetPodScores(s.ctx, prompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.Len(pods, 1, "expected one pod score")
	s.T().Logf("Received pod scores: %+v", pods)

	s.Equal(1, pods[s.Pod1IP], "expected pod score to equal 1")
}

// TestPrefixReduction tests scoring behavior when querying progressively shorter prefixes of a fully cached prompt.
func (s *KVCacheSuite) TestPrefixReduction() {
	//nolint:lll // long prompt
	fullPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat."
	//nolint:lll // long prompt
	midPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."
	shortPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit."

	blockKeys := s.promptToRedisKeys(fullPrompt, defaultModelName)
	fakePodList := []string{s.Pod1IP}

	s.setRedisMockEntries(blockKeys, fakePodList)

	// Test 1: Full prompt (no match expected)
	pods, err := s.indexer.GetPodScores(s.ctx, fullPrompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Received pod scores: %+v", pods)
	s.Empty(pods, "expected no pod scores")

	time.Sleep(5 * time.Second)

	// Test 2: mid-length prompt(should return a match)
	pods, err = s.indexer.GetPodScores(s.ctx, midPrompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)

	s.T().Logf("Received pod scores: %+v", pods)
	s.Equal(pods[s.Pod1IP], 10, "expected pod score to equal 10")

	// Test 3: short prompt(should return a match)
	pods, err = s.indexer.GetPodScores(s.ctx, shortPrompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.Len(pods, 1, "expected one pod score")
	s.T().Logf("Received pod scores: %+v", pods)
	s.Equal(pods[s.Pod1IP], 5, "expected pod score to equal 5")
}

// TestPrefixExpansion tests that prompts longer than the cached prefix still return partial match scores.
func (s *KVCacheSuite) TestPrefixExpansion() {
	//nolint:lll // long prompt
	fullPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat."
	//nolint:lll // long prompt
	midPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."
	shortPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit."
	modelName := defaultModelName
	// Insert only short prompt
	blockKeys := s.promptToRedisKeys(shortPrompt, modelName)
	fakePodList := []string{s.Pod1IP}

	s.setRedisMockEntries(blockKeys, fakePodList)

	// Test 1: short prompt
	pods, err := s.indexer.GetPodScores(s.ctx, shortPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Received pod scores: %+v", pods)
	s.Empty(pods, "expected no pod scores")

	time.Sleep(5 * time.Second)

	// Test 2: mid prompt
	pods, err = s.indexer.GetPodScores(s.ctx, midPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)

	s.T().Logf("Received pod scores: %+v", pods)
	s.Equal(5, pods[s.Pod1IP], "expected pod score to equal 5")

	blockKeys = s.promptToRedisKeys(midPrompt, modelName)
	s.setRedisMockEntries(blockKeys, fakePodList) // update redis
	time.Sleep(5 * time.Second)

	// Test 3: full prompt
	pods, err = s.indexer.GetPodScores(s.ctx, fullPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)

	s.T().Logf("Received pod scores: %+v", pods)
	s.Equal(10, pods[s.Pod1IP], "expected pod score to equal 10")
}

func (s *KVCacheSuite) TestLongPrefixExpansion() {
	base := "The quick brown fox jumps over the lazy dog"
	modelName := defaultModelName
	s.T().Logf("s.config.PrefixStoreConfig: %+v", s.config.PrefixStoreConfig.LRUStoreConfig)
	s.T().Logf("s.config.PrefixStoreConfig: %+v", s.config.TokenProcessorConfig)
	// Generate long prompts
	shortPrompt := strings.Repeat(base, 2)
	midPrompt := strings.Repeat(base, 100)  // ~900 tokens
	longPrompt := strings.Repeat(base, 500) // ~4500 tokens

	// Insert only short prompt into Redis
	blockKeys := s.promptToRedisKeys(shortPrompt, modelName)
	fakePodList := []string{s.Pod1IP}
	s.setRedisMockEntries(blockKeys, fakePodList)

	// Test 1: short prompt (should return no pod scores yet)
	pods, err := s.indexer.GetPodScores(s.ctx, shortPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Short prompt scores: %+v", pods)
	s.Empty(pods, "expected no pod scores")
	time.Sleep(5 * time.Second)

	// Test 2: mid prompt (should return partial match if indexer picks it up)
	pods, err = s.indexer.GetPodScores(s.ctx, midPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Mid prompt scores: %+v", pods)
	s.True(len(pods) > 0, "expected at least one pod score for mid prompt")
	blockKeys = s.promptToRedisKeys(midPrompt, modelName)
	s.setRedisMockEntries(blockKeys, fakePodList)
	time.Sleep(5 * time.Second)

	// Test 3: long prompt (should return higher score)
	pods, err = s.indexer.GetPodScores(s.ctx, longPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Long prompt scores: %+v", pods)
	s.True(len(pods) > 0, "expected at least one pod score for long prompt")
}
