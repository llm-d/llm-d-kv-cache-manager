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
	blockKeys := int64KeysToKVBlockKeys([]uint64{1001, 1002, 1003, 1004, 1005, 1006})

	hitmap := map[kvblock.Key][]string{
		{ModelName: testModelName, ChunkHash: 1001}: {podA},
		{ModelName: testModelName, ChunkHash: 1002}: {podA},
		{ModelName: testModelName, ChunkHash: 1003}: {podA},
		{ModelName: testModelName, ChunkHash: 1004}: {podB},
		{ModelName: testModelName, ChunkHash: 1005}: {podB},
		{ModelName: testModelName, ChunkHash: 1006}: {podA},
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

func int64KeysToKVBlockKeys(keys []uint64) []kvblock.Key {
	kvKeys := make([]kvblock.Key, len(keys))
	for i, key := range keys {
		kvKeys[i] = kvblock.Key{
			ModelName: testModelName,
			ChunkHash: key,
		}
	}
	return kvKeys
}
