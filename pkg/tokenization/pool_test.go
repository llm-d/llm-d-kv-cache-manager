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
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/daulet/tokenizers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization/prefixstore"
)

const (
	benchmarkMaxWords    = 1_000
	benchmarkWordLength  = 2
	benchmarkSeed        = 42
	benchmarkWorkerCount = 5
)

var benchmarkModels = []string{
	"google-bert/bert-base-uncased",
	"openai-community/gpt2",
}

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

//nolint:gocritic // unnamedResult: tokens and overlapRatio are self-explanatory from context
func (m *MockIndexer) FindLongestContainedTokens(prompt, modelName string) ([]uint32, float64) {
	args := m.Called(prompt, modelName)
	tokens := args.Get(0).([]uint32) //nolint:errcheck // unused mock
	return tokens, 0.0
}

func TestPool_ProcessTask(t *testing.T) {
	mockIndexer := &MockIndexer{}
	mockTokenizer := &MockTokenizer{}

	pool := &Pool{
		workers:               1,
		indexer:               mockIndexer,
		tokenizer:             mockTokenizer,
		minPrefixOverlapRatio: defaultMinPrefixOverlapRatio,
	}

	task := Task{
		Prompt:    "hello world",
		ModelName: testModelName,
	}

	// Setup specific mock return values
	expectedTokens := []uint32{12345, 67890, 11111}
	expectedOffsets := []tokenizers.Offset{{0, 5}, {6, 11}}

	// Mock FindLongestContainedTokens to return low overlap ratio
	mockIndexer.On("FindLongestContainedTokens", task.Prompt, task.ModelName).Return([]uint32{}, 0.0)

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
		mockIndexer.On("FindLongestContainedTokens", prompt, testModelName).Return([]uint32{}, 0.0)
		mockIndexer.On("AddTokenization", testModelName, prompt,
			mock.Anything, mock.Anything).Return(nil).Once()
	}

	config := &Config{
		WorkersCount: 5,
		HFTokenizerConfig: &HFTokenizerConfig{
			TokenizersCacheDir: t.TempDir(),
		},
		MinPrefixOverlapRatio: defaultMinPrefixOverlapRatio,
	}

	pool, err := NewTokenizationPool(config, mockIndexer)
	require.NoError(t, err)

	// Create context for the pool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, prompt := range prompts {
		pool.EnqueueTokenization(prompt, testModelName)
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

func generateRandomSentence(wordLength, maxWords int, rng *rand.Rand) string {
	numWords := rng.Intn(maxWords) + 1
	words := make([]string, numWords)

	for i := range numWords {
		word := make([]byte, wordLength)
		for j := range wordLength {
			word[j] = byte('a' + rng.Intn(26))
		}
		words[i] = string(word)
	}

	return strings.Join(words, " ")
}

func setupStressTest(b *testing.B) *Pool {
	b.Helper()

	config := &Config{
		WorkersCount: benchmarkWorkerCount,
		HFTokenizerConfig: &HFTokenizerConfig{
			TokenizersCacheDir: b.TempDir(),
		},
		MinPrefixOverlapRatio: defaultMinPrefixOverlapRatio,
	}

	inMemoryIndexer, err := prefixstore.NewLRUTokenStore(nil)
	require.NoError(b, err)

	pool, err := NewTokenizationPool(config, inMemoryIndexer)
	require.NoError(b, err)

	for _, modelName := range benchmarkModels {
		_, _, err := pool.tokenizer.Encode("", modelName)
		require.NoError(b, err)
	}
	return pool
}

func BenchmarkAsyncTokenizationStress(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping tokenizer integration test in short mode")
	}

	pool := setupStressTest(b)

	// Return RNG for on-demand prompt generation
	rng := rand.New(rand.NewSource(benchmarkSeed)) //nolint:gosec // Test code - weak random is acceptable

	// Generate and enqueue prompts on-the-fly to avoid memory bloat
	for i := range b.N {
		prompt := generateRandomSentence(benchmarkWordLength, benchmarkMaxWords, rng)
		modelName := benchmarkModels[i%len(benchmarkModels)]
		pool.EnqueueTokenization(prompt, modelName)
	}

	// Create context for the pool
	ctx, cancel := context.WithCancel(context.Background())

	// Run pool
	go pool.Run(ctx)

	b.ResetTimer()

	// when poo gets empty pool.queue.Len() == 0 call cancel to the context:
	for pool.queue.Len() > 0 {
		time.Sleep(100 * time.Millisecond)
	}

	b.StopTimer()
	cancel()

	frequency := float64(b.N) / b.Elapsed().Seconds()
	b.Logf("Processed %d tasks in %v (%.2f tasks/sec)",
		b.N, b.Elapsed(), frequency)
}

func BenchmarkSyncTokenizationStress(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping tokenizer integration test in short mode")
	}

	pool := setupStressTest(b)

	// Return RNG for on-demand prompt generation
	rng := rand.New(rand.NewSource(benchmarkSeed)) //nolint:gosec // Test code - weak random is acceptable

	// Create context for the pool
	ctx, cancel := context.WithCancel(context.Background())

	// Run pool
	go pool.Run(ctx)

	// Now that workers are running, reset benchmark timer
	b.ResetTimer()

	// Submit tokenization requests in a loop until limit
	for i := 0; b.Loop(); i++ {
		prompt := generateRandomSentence(benchmarkWordLength, benchmarkMaxWords, rng)
		model := benchmarkModels[i%len(benchmarkModels)]
		pool.Tokenize(prompt, model)
	}

	b.StopTimer()
	cancel()

	frequency := float64(b.N) / b.Elapsed().Seconds()
	b.Logf("Processed %d tasks in %v (%.2f tasks/sec)",
		b.N, b.Elapsed(), frequency)
}
