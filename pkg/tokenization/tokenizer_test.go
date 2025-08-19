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
package tokenization

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This should be skipped in fast unit tests.
const testModelName = "google-bert/bert-base-uncased"

func TestCachedHFTokenizer_Encode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping tokenizer integration test in short mode")
	}

	config := &HFTokenizerConfig{
		TokenizersCacheDir: t.TempDir(),
	}
	tokenizer, err := NewCachedHFTokenizer(config)
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	tests := []struct {
		name      string
		input     string
		modelName string
	}{
		{
			name:      "simple text",
			input:     "hello world",
			modelName: testModelName,
		},
		{
			name:      "empty string",
			input:     "",
			modelName: testModelName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenIds, offsets, err := tokenizer.Encode(tt.input, tt.modelName)

			assert.NoError(t, err)
			assert.GreaterOrEqual(t, len(tokenIds), 0)
			assert.Equal(t, len(tokenIds), len(offsets))
		})
	}
}

func TestCachedHFTokenizer_CacheTokenizer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping tokenizer integration test in short mode")
	}

	tokenizer, err := NewCachedHFTokenizer(&HFTokenizerConfig{
		TokenizersCacheDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	// Test that the same model is cached
	input := "test input"

	// First call - loads tokenizer
	tokenIds1, offsets1, err1 := tokenizer.Encode(input, testModelName)
	require.NoError(t, err1)

	// Second call - should use cached tokenizer
	tokenIds2, offsets2, err2 := tokenizer.Encode(input, testModelName)
	require.NoError(t, err2)

	// Results should be identical
	assert.Equal(t, tokenIds1, tokenIds2)
	assert.Equal(t, offsets1, offsets2)
}

func TestCachedHFTokenizer_InvalidModel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping tokenizer integration test in short mode")
	}

	tokenizer, err := NewCachedHFTokenizer(&HFTokenizerConfig{
		TokenizersCacheDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	// Test with non-existent model
	tokenIds, offsets, err := tokenizer.Encode("test", "non-existent/model")
	assert.Error(t, err)
	assert.Nil(t, tokenIds)
	assert.Nil(t, offsets)
}
