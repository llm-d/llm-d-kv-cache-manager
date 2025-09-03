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

package prefixstore //nolint:testpackage // convenience

import (
	"testing"

	"github.com/daulet/tokenizers"
	"github.com/stretchr/testify/assert"
)

func TestLRUTokenStore_AddAndRetrieve(t *testing.T) {
	store, err := NewLRUTokenStore(&Config{
		&LRUStoreConfig{CacheSize: defaultMaxCacheSize, BlockSize: 4},
	})
	assert.NoError(t, err)

	modelName := "test-model"
	text := "The capital of France is Paris"
	tokens := []uint32{1, 2, 3, 4, 5, 6}
	offsets := []tokenizers.Offset{
		{0, 3}, {4, 11}, {12, 14}, {15, 21}, {22, 24}, {25, 30},
	}

	// Add tokenization to the store
	err = store.AddTokenization(modelName, text, tokens, offsets)
	assert.NoError(t, err)

	// Retrieve tokens for a matching prefix
	prompt := "The capital of F"
	result, overlapPercent := store.FindLongestContainedTokens(prompt, modelName)
	assert.Equal(t, []uint32{1, 2, 3}, result)
	assert.Equal(t, overlapPercent, 1.0)
}

func TestLRUTokenStore_LRUEviction(t *testing.T) {
	cfg := &Config{
		&LRUStoreConfig{CacheSize: 2, BlockSize: 18}, // Small cache size for testing eviction
	}
	store, err := NewLRUTokenStore(cfg)
	assert.NoError(t, err)

	modelName := "test-model"
	texts := []string{
		"abcdefghjiklmno",
		"123456789011121314",
		"pqrstuvwxyz,./';lp",
	}
	tokens := [][]uint32{
		{1, 2, 3},
		{4, 5, 6},
		{7, 8, 9},
	}
	offsets := [][]tokenizers.Offset{
		{{0, 5}, {6, 10}, {11, 15}},
		{{0, 6}, {7, 12}, {13, 18}},
		{{0, 6}, {7, 12}, {13, 18}},
	}

	// Add tokenizations to the store
	for i, text := range texts {
		err = store.AddTokenization(modelName, text, tokens[i], offsets[i])
		assert.NoError(t, err)
	}

	// First text block should be evicted
	prompt := "abcdefghjiklmno"
	result, _ := store.FindLongestContainedTokens(prompt, modelName)
	assert.Empty(t, result, "First text block should be evicted")

	// Third text block should still be in cache
	prompt = "pqrstuvwxyz,./';lp"
	result, _= store.FindLongestContainedTokens(prompt, modelName)
	assert.Equal(t, []uint32{7, 8, 9}, result)
}
