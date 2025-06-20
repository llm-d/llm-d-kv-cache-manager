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

package kvcache_test

import (
	"testing"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"

	"github.com/stretchr/testify/assert"
)

const (
	testModelName = "test-model"
	podA          = "pod-a"
	podB          = "pod-b"
)

// TestLongestPrefixScorer verifies scoring based on consecutive block hits from the start.
func TestLongestPrefixScorer(t *testing.T) {
	scorer := &kvcache.LongestPrefixScorer{}
	blockKeys := stringKeysToKVBlockKeys([]string{"b1", "b2", "b3", "b4", "b5", "b6"})

	hitmap := map[kvblock.Key][]string{
		{ModelName: testModelName, ChunkHash: "b1"}: {podA},
		{ModelName: testModelName, ChunkHash: "b2"}: {podA},
		{ModelName: testModelName, ChunkHash: "b3"}: {podA},
		{ModelName: testModelName, ChunkHash: "b4"}: {podB},
		{ModelName: testModelName, ChunkHash: "b5"}: {podB},
		{ModelName: testModelName, ChunkHash: "b6"}: {podA},
	}

	expected := map[string]int{
		"pod-a": 3,
		"pod-b": 0,
	}

	scored, err := scorer.Score(blockKeys, hitmap)
	assert.NoError(t, err)
	for pod, score := range scored {
		assert.Equal(t, expected[pod], score)
	}
}

func stringKeysToKVBlockKeys(keys []string) []kvblock.Key {
	kvKeys := make([]kvblock.Key, len(keys))
	for i, key := range keys {
		kvKeys[i] = kvblock.Key{
			ModelName: testModelName,
			ChunkHash: key,
		}
	}
	return kvKeys
}
