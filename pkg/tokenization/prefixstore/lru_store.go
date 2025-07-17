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

package prefixstore

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/daulet/tokenizers"
	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	// defaultBlockSize defines how many tokens each block contains in the prefix cache.
	defaultBlockSize = 256
	// defaultMaxCacheSize sets the maximum number of blocks the LRU cache can store.
	defaultMaxCacheSize = 500000
)

// LRUStoreConfig contains initialization settings for LRUTokenStore (block size and cache size).
type LRUStoreConfig struct {
	CacheSize int `json:"cacheSize"`
	BlockSize int `json:"blockSize"` // number of tokens per block
}

// defaultLRUStoreConfig returns an LRUStoreConfig instance with default configuration.
func defaultLRUStoreConfig() *LRUStoreConfig {
	return &LRUStoreConfig{
		CacheSize: defaultMaxCacheSize,
		BlockSize: defaultBlockSize,
	}
}

// Block holds the tokens contained in the block.
// A token is contained iff its [_, high] offset is associated with a substring
// of the chunk that was used to generate the hash (key) of the block.
type Block struct {
	Tokens []uint32
}

// LRUTokenStore is an in-memory prefix-to-block cache with xxhash keys and LRU
// eviction.
// TODO: optimize implementation and check chunk-tokenization vs tokenization-chunking.
type LRUTokenStore struct {
	mu sync.RWMutex

	cacheSize int
	blockSize int

	store map[string]*lru.Cache[uint64, Block]
}

var _ Indexer = &LRUTokenStore{}

// NewLRUTokenStore initializes the LRUTokenStore with LRU cache.
func NewLRUTokenStore(config *Config) (Indexer, error) {
	if config == nil {
		config = DefaultConfig()
	} // TODO: add validation

	return &LRUTokenStore{
		cacheSize: config.CacheSize,
		blockSize: config.BlockSize,
		store:     make(map[string]*lru.Cache[uint64, Block]),
	}, nil
}

// AddTokenization adds the full tokenization of a string to the
// indexer for a given model.
// The function assumes tokens and offsets are of the same length.
// The function assumes that tokens will not be mutated after the call.
func (c *LRUTokenStore) AddTokenization(modelName string, prompt string, tokens []uint32,
	offsets []tokenizers.Offset,
) error {
	if prompt == "" || len(tokens) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Get or create the LRU cache for the model
	cache, ok := c.store[modelName]
	if !ok {
		var err error
		cache, err = lru.New[uint64, Block](c.cacheSize)
		if err != nil {
			return fmt.Errorf("failed to create LRU cache for model %s: %w", modelName, err)
		}

		c.store[modelName] = cache
	}

	promptBytes := []byte(prompt)
	tokenIdxIterator := 0
	previousHash := uint64(0)
	digest := xxhash.New()

	// Chunk the text into blocks and populate the cache
	for start := 0; start < len(promptBytes); start += c.blockSize {
		end := start + c.blockSize
		if end > len(promptBytes) {
			break // no partial blocks
		}

		// Compute the hash for the current block
		digest.Reset()
		if err := binary.Write(digest, binary.LittleEndian, previousHash); err != nil {
			return fmt.Errorf("failed to add token: %w", err)
		}
		if _, err := digest.Write(promptBytes[start:end]); err != nil {
			return fmt.Errorf("failed to add token: %w", err)
		}

		blockHash := digest.Sum64()
		previousHash = blockHash

		// Only add tokens with [_, high] offset associated with the chunk range.
		// If a token's [low, _] index is less than the start, it is OK as long as
		// the above condition is satisfied.

		block := Block{Tokens: []uint32{}}
		for ; tokenIdxIterator < len(tokens); tokenIdxIterator++ {
			//nolint:gosec // Again end is tied to context-window size, safe to assume it won't reach max int32
			if offsets[tokenIdxIterator][1] <= uint(end) {
				block.Tokens = append(block.Tokens, tokens[tokenIdxIterator])
			} else {
				break
			}
		}

		cache.Add(blockHash, block)
	}

	return nil
}

// FindLongestContainedTokens finds the sequence of contained tokens for
// the longest matching prefix.
func (c *LRUTokenStore) FindLongestContainedTokens(prompt, modelName string) []uint32 {
	c.mu.RLock()
	cache, ok := c.store[modelName]
	c.mu.RUnlock()

	if !ok {
		return nil
	}

	containedTokens := []uint32{}

	promptBytes := []byte(prompt)
	previousHash := uint64(0)
	digest := xxhash.New()

	// Chunk the text into blocks and populate the cache
	for i := 0; i < len(promptBytes); i += c.blockSize {
		end := i + c.blockSize
		if end > len(promptBytes) {
			break // no partial blocks
		}

		// Compute the hash for the current block
		digest.Reset()
		if err := binary.Write(digest, binary.LittleEndian, previousHash); err != nil {
			break
		}
		if _, err := digest.Write(promptBytes[i:end]); err != nil {
			break
		}

		blockHash := digest.Sum64()
		previousHash = blockHash

		block, ok := cache.Get(blockHash)
		if !ok {
			break // early-stop
		}

		containedTokens = append(containedTokens, block.Tokens...)
	}

	return containedTokens
}
