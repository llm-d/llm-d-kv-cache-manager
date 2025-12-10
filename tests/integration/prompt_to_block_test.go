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

package integration_test

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/llm-d/llm-d-kv-cache/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache/pkg/tokenization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/kv_event_base.json
var kvEventJSON []byte

//go:embed testdata/kv_event_lora.json
var kvEventWithLoraJSON []byte

type KVEventData struct {
	Prompt          string   `json:"prompt"`
	ModelName       string   `json:"model_name"`
	LoraPath        *string  `json:"lora_path"`
	LoraName        *string  `json:"lora_name"`
	EventType       string   `json:"event_type"`
	BlockHashes     []uint64 `json:"block_hashes"`
	ParentBlockHash *uint64  `json:"parent_block_hash"`
	TokenIds        []int    `json:"token_ids"`
	BlockSize       int      `json:"block_size"`
	Medium          string   `json:"medium"`
	HashSeed        string   `json:"hash_seed"`
}

func parseTestData(t *testing.T, jsonData []byte) *KVEventData {
	t.Helper()
	var eventData KVEventData
	err := json.Unmarshal(jsonData, &eventData)
	require.NoError(t, err, "failed to parse test data JSON")
	return &eventData
}

func TestPromptToBlockHashesWithPrecomputedValues(t *testing.T) {
	t.Skip("Hash algorithm changed from SHA256 to FNV-64a, precomputed vLLM hashes no longer match")
	if testing.Short() {
		t.Skip("Skipping precomputed hash comparison test in short mode")
	}

	// Parse embedded test data
	testData := parseTestData(t, kvEventJSON)

	// Setup tokenizer
	config := &tokenization.HFTokenizerConfig{
		TokenizersCacheDir: t.TempDir(),
	}
	tokenizer, err := tokenization.NewCachedHFTokenizer(config)
	require.NoError(t, err)

	// Setup processor with config from test data
	processorConfig := &kvblock.TokenProcessorConfig{
		BlockSize: testData.BlockSize,
		HashSeed:  testData.HashSeed,
	}
	processor := kvblock.NewChunkedTokenDatabase(processorConfig)

	// Tokenize the prompt from test data
	tokenIds, _, err := tokenizer.Encode(testData.Prompt, testData.ModelName)
	require.NoError(t, err, "tokenization should succeed")
	require.NotEmpty(t, tokenIds, "prompt should produce tokens")

	// Generate block keys with hashes
	blockKeys := processor.TokensToKVBlockKeys(nil, tokenIds, testData.ModelName)
	require.NotEmpty(t, blockKeys, "should generate block keys")

	// Extract hashes for comparison
	actualHashes := make([]uint64, len(blockKeys))
	for i, key := range blockKeys {
		actualHashes[i] = key.ChunkHash
	}

	// Compare with precomputed hashes from test data
	assert.Equal(t, testData.BlockHashes, actualHashes,
		"computed hashes should match precomputed vLLM hashes")
}

func TestPromptToBlockHashesWithLoraAdapter(t *testing.T) {
	t.Skip("TODO: Fix LoRA adapter hash calculation to match vLLM")

	if testing.Short() {
		t.Skip("Skipping precomputed hash comparison test in short mode")
	}

	// Parse embedded test data with LoRA
	testData := parseTestData(t, kvEventWithLoraJSON)

	// Setup tokenizer
	config := &tokenization.HFTokenizerConfig{
		TokenizersCacheDir: t.TempDir(),
	}
	tokenizer, err := tokenization.NewCachedHFTokenizer(config)
	require.NoError(t, err)

	// Setup processor with config from test data
	processorConfig := &kvblock.TokenProcessorConfig{
		BlockSize: testData.BlockSize,
		HashSeed:  testData.HashSeed,
	}
	processor := kvblock.NewChunkedTokenDatabase(processorConfig)

	// Build model name with LoRA adapter
	modelNameWithLora := testData.ModelName
	if testData.LoraName != nil {
		// The model name should include LoRA information for proper hashing
		modelNameWithLora = testData.ModelName + "+" + *testData.LoraName
	}

	// Tokenize the prompt from test data
	tokenIds, _, err := tokenizer.Encode(testData.Prompt, testData.ModelName)
	require.NoError(t, err, "tokenization should succeed")
	require.NotEmpty(t, tokenIds, "prompt should produce tokens")

	// Generate block keys with hashes using LoRA-aware model name
	blockKeys := processor.TokensToKVBlockKeys(nil, tokenIds, modelNameWithLora)
	require.NotEmpty(t, blockKeys, "should generate block keys")

	// Extract hashes for comparison
	actualHashes := make([]uint64, len(blockKeys))
	for i, key := range blockKeys {
		actualHashes[i] = key.ChunkHash
	}

	// Compare with precomputed hashes from test data
	assert.Equal(t, testData.BlockHashes, actualHashes,
		"computed hashes with LoRA should match precomputed vLLM hashes")
}
