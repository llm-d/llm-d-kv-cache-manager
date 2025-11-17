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

//nolint:testpackage // need to test internal types
package integration_test

import (
	"testing"

	"github.com/llm-d/llm-d-kv-cache-manager/examples/testdata"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptToBlockHashesWithPrecomputedValues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping precomputed hash comparison test in short mode")
	}

	// Setup tokenizer
	config := &tokenization.HFTokenizerConfig{
		TokenizersCacheDir: t.TempDir(),
	}
	tokenizer, err := tokenization.NewCachedHFTokenizer(config)
	require.NoError(t, err)

	// Setup processor with config that should match vLLM settings
	processorConfig := &kvblock.TokenProcessorConfig{
		BlockSize: 16, // vLLM default
		HashSeed:  "", // Match vLLM's default
	}
	processor := kvblock.NewChunkedTokenDatabase(processorConfig)

	// Tokenize the testdata prompt
	tokenIds, _, err := tokenizer.Encode(testdata.Prompt, testdata.ModelName)
	require.NoError(t, err, "tokenization should succeed")
	require.NotEmpty(t, tokenIds, "prompt should produce tokens")

	// Generate block keys with hashes
	blockKeys := processor.TokensToKVBlockKeys(tokenIds, testdata.ModelName)
	require.NotEmpty(t, blockKeys, "should generate block keys")

	// Extract hashes for comparison
	actualHashes := make([]uint64, len(blockKeys))
	for i, key := range blockKeys {
		actualHashes[i] = key.ChunkHash
	}

	// Compare with precomputed hashes from vLLM
	assert.Equal(t, testdata.PromptHashes, actualHashes,
		"computed hashes should match precomputed vLLM hashes")

	// Additional validations
	assert.Equal(t, len(testdata.PromptHashes), len(actualHashes),
		"number of hash blocks should match expected count")

	for i, expectedHash := range testdata.PromptHashes {
		if i < len(actualHashes) {
			assert.Equal(t, expectedHash, actualHashes[i],
				"hash at position %d should match vLLM precomputed value", i)
		}
	}
}
