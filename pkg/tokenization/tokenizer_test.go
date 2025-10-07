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
	"os"
	"path/filepath"
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

func TestCachedLocalTokenizer_Encode(t *testing.T) {
	config := LocalTokenizerConfig{
		Mapping: map[string]string{
			"test-model": "testdata/test-model/tokenizer.json",
		},
	}
	tokenizer, err := NewCachedLocalTokenizer(config)
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
			modelName: "test-model",
		},
		{
			name:      "empty string",
			input:     "",
			modelName: "test-model",
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

func TestCachedLocalTokenizer_CacheTokenizer(t *testing.T) {
	config := LocalTokenizerConfig{
		Mapping: map[string]string{
			"test-model": "testdata/test-model/tokenizer.json",
		},
	}
	tokenizer, err := NewCachedLocalTokenizer(config)
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	// Test that the same model is cached
	input := "test input"

	// First call - loads tokenizer
	tokenIds1, offsets1, err1 := tokenizer.Encode(input, "test-model")
	require.NoError(t, err1)

	// Second call - should use cached tokenizer
	tokenIds2, offsets2, err2 := tokenizer.Encode(input, "test-model")
	require.NoError(t, err2)

	// Results should be identical
	assert.Equal(t, tokenIds1, tokenIds2)
	assert.Equal(t, offsets1, offsets2)
}

func TestCachedLocalTokenizer_InvalidModel(t *testing.T) {
	config := LocalTokenizerConfig{
		Mapping: map[string]string{
			"test-model": "testdata/test-model/tokenizer.json",
		},
	}
	tokenizer, err := NewCachedLocalTokenizer(config)
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	// Test with non-existent model
	tokenIds, offsets, err := tokenizer.Encode("test", "non-existent-model")
	assert.Error(t, err)
	assert.Nil(t, tokenIds)
	assert.Nil(t, offsets)
}

func TestCachedLocalTokenizer_InvalidPath(t *testing.T) {
	config := LocalTokenizerConfig{
		Mapping: map[string]string{
			"invalid-model": "testdata/non-existent/tokenizer.json",
		},
	}
	tokenizer, err := NewCachedLocalTokenizer(config)
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	// Test with model that points to non-existent file
	tokenIds, offsets, err := tokenizer.Encode("test", "invalid-model")
	assert.Error(t, err)
	assert.Nil(t, tokenIds)
	assert.Nil(t, offsets)
}

func TestCompositeTokenizer_FallbackBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping tokenizer integration test in short mode")
	}

	localTokenizer, err := NewCachedLocalTokenizer(LocalTokenizerConfig{
		Mapping: map[string]string{
			"test-model": "testdata/test-model/tokenizer.json",
		},
	})
	require.NoError(t, err)

	hfTokenizer, err := NewCachedHFTokenizer(&HFTokenizerConfig{
		TokenizersCacheDir: t.TempDir(),
	})
	require.NoError(t, err)

	composite := &CompositeTokenizer{
		Tokenizers: []Tokenizer{localTokenizer, hfTokenizer},
	}

	tests := []struct {
		name      string
		input     string
		modelName string
		wantErr   bool
	}{
		{
			name:      "local tokenizer succeeds",
			input:     "hello world",
			modelName: "test-model",
			wantErr:   false,
		},
		{
			name:      "fallback to HF tokenizer",
			input:     "hello world",
			modelName: testModelName,
			wantErr:   false,
		},
		{
			name:      "both tokenizers fail",
			input:     "test",
			modelName: "non-existent-model",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenIds, offsets, err := composite.Encode(tt.input, tt.modelName)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, tokenIds)
				assert.Nil(t, offsets)
			} else {
				assert.NoError(t, err)
				assert.GreaterOrEqual(t, len(tokenIds), 0)
				assert.Equal(t, len(tokenIds), len(offsets))
			}
		})
	}
}

func TestDefaultLocalTokenizerConfig(t *testing.T) {
	// Save original env var
	originalDir := localTokenizerDir
	t.Cleanup(func() {
		localTokenizerDir = originalDir
	})

	tests := []struct {
		name           string
		envValue       string
		setupFunc      func(t *testing.T) string
		wantEnabled    bool
		wantMappingLen int
		wantModels     []string
		wantErr        bool
	}{
		{
			name:     "with testdata directory",
			envValue: "testdata",
			setupFunc: func(t *testing.T) string {
				t.Helper()
				return "testdata"
			},
			wantEnabled:    true,
			wantMappingLen: 1, // Should find testdata/test-model/tokenizer.json
			wantModels:     []string{"test-model"},
			wantErr:        false,
		},
		{
			name:     "with non-existent directory",
			envValue: "non-existent-dir",
			setupFunc: func(t *testing.T) string {
				t.Helper()
				return "non-existent-dir"
			},
			wantEnabled:    false,
			wantMappingLen: 0,
			wantModels:     []string{},
			wantErr:        false,
		},
		{
			name:     "with empty directory",
			envValue: "",
			setupFunc: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				return dir
			},
			wantEnabled:    false,
			wantMappingLen: 0,
			wantModels:     []string{},
			wantErr:        false,
		},
		{
			name:     "with nested directories (org/model structure)",
			envValue: "",
			setupFunc: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				// Create nested structure: org1/model1, org1/model2, org2/model3
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "org1", "model1"), 0o755))
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "org1", "model2"), 0o755))
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "org2", "model3"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "org1", "model1", "tokenizer.json"), []byte("{}"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "org1", "model2", "tokenizer.json"), []byte("{}"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "org2", "model3", "tokenizer.json"), []byte("{}"), 0o600))
				return dir
			},
			wantEnabled:    true,
			wantMappingLen: 3,
			wantModels:     []string{"org1/model1", "org1/model2", "org2/model3"},
			wantErr:        false,
		},
		{
			name:     "with deeply nested directories",
			envValue: "",
			setupFunc: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				// Create deeply nested: a/b/c/model
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "b", "c", "model"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "c", "model", "tokenizer.json"), []byte("{}"), 0o600))
				return dir
			},
			wantEnabled:    true,
			wantMappingLen: 1,
			wantModels:     []string{"a/b/c/model"},
			wantErr:        false,
		},
		{
			name:     "with custom tokenizer filename",
			envValue: "",
			setupFunc: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				// Create models with custom filename
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "model1"), 0o755))
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "model2"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "model1", "custom.json"), []byte("{}"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "model2", "custom.json"), []byte("{}"), 0o600))
				// Also create a tokenizer.json that should be ignored
				require.NoError(t, os.WriteFile(filepath.Join(dir, "model1", "tokenizer.json"), []byte("{}"), 0o600))

				// Save original and set custom filename
				originalFilename := localTokenizerFileName
				localTokenizerFileName = "custom.json"
				t.Cleanup(func() {
					localTokenizerFileName = originalFilename
				})
				return dir
			},
			wantEnabled:    true,
			wantMappingLen: 2,
			wantModels:     []string{"model1", "model2"},
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setupFunc(t)
			localTokenizerDir = dir

			config, err := DefaultLocalTokenizerConfig()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, config)
				assert.Equal(t, tt.wantEnabled, config.IsEnabled())
				assert.Equal(t, tt.wantMappingLen, len(config.Mapping))

				// Verify expected models are in the mapping
				for _, modelName := range tt.wantModels {
					_, ok := config.Mapping[modelName]
					assert.True(t, ok, "expected model %q to be in mapping", modelName)
				}
			}
		})
	}
}
