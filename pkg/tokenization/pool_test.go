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
	"context"
	"testing"
	"time"

	"github.com/daulet/tokenizers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockTokenizer implements the Tokenizer interface for testing.
type MockTokenizer struct {
	mock.Mock
}

func (m *MockTokenizer) Encode(input, modelName string) ([]uint32, []tokenizers.Offset, error) {
	args := m.Called(input, modelName)
	return args.Get(0).([]uint32), args.Get(1).([]tokenizers.Offset), args.Error(2) //nolint:errcheck // return mocked values
}

// MockIndexer implements the prefixstore.Indexer interface for testing.
type MockIndexer struct {
	mock.Mock
}

func (m *MockIndexer) AddTokenization(modelName, prompt string, tokens []uint32, offsets []tokenizers.Offset) error {
	args := m.Called(modelName, prompt, tokens, offsets)
	return args.Error(0)
}

func (m *MockIndexer) FindLongestContainedTokens(prompt, modelName string) []uint32 {
	args := m.Called(prompt, modelName)
	return args.Get(0).([]uint32) //nolint:errcheck // unused mock
}

func TestPool_ProcessTask(t *testing.T) {
	mockIndexer := &MockIndexer{}
	mockTokenizer := &MockTokenizer{}

	pool := &Pool{
		workers:   1,
		indexer:   mockIndexer,
		tokenizer: mockTokenizer,
	}

	task := Task{
		Prompt:    "hello world",
		ModelName: testModelName,
	}

	// Setup specific mock return values
	expectedTokens := []uint32{12345, 67890, 11111}
	expectedOffsets := []tokenizers.Offset{{0, 5}, {6, 11}}

	mockTokenizer.On("Encode", task.Prompt, task.ModelName).Return(expectedTokens, expectedOffsets, nil)

	// Verify that indexer receives exactly the same tokens and offsets that tokenizer returned
	mockIndexer.On("AddTokenization", task.ModelName, task.Prompt, expectedTokens, expectedOffsets).Return(nil)

	// Execute
	err := pool.processTask(task)

	// Assert
	assert.NoError(t, err)
	mockTokenizer.AssertExpectations(t)
	mockIndexer.AssertExpectations(t)
}

func TestPool_RunIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping tokenizer integration test in short mode")
	}

	mockIndexer := &MockIndexer{}

	prompts := []string{"hello world", "this is a test", "unicode test: 世界"}

	// Setup mock expectations for each prompt
	for _, prompt := range prompts {
		mockIndexer.On("AddTokenization", testModelName, prompt,
			mock.Anything, mock.Anything).Return(nil).Once()
	}

	config := &Config{
		WorkersCount: 2,
		HFTokenizerConfig: &HFTokenizerConfig{
			TokenizersCacheDir: t.TempDir(),
		},
	}

	pool, err := NewTokenizationPool(config, mockIndexer)
	require.NoError(t, err)

	// Create context for the pool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, prompt := range prompts {
		pool.AddTask(prompt, testModelName)
	}

	// Run pool
	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.Run(ctx)
	}()

	time.Sleep(2 * time.Second)
	cancel()
	<-done

	mockIndexer.AssertExpectations(t)
}
