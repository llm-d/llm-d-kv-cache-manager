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
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/daulet/tokenizers"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
)

const (
	// defaultBlockSize defines how many tokens each block contains in the prefix cache.
	defaultBlockSize = 256
	// defaultMaxCacheSize sets the maximum number of blocks the LRU cache can store.
	defaultMaxCacheSize = 500000
	// defaultPodMappingTTL is the default TTL for cached pod mappings.
	defaultPodMappingTTL = 30 * time.Second
	// defaultMaxPodSetsPerBlock is the default maximum number of pod sets cached per block.
	defaultMaxPodSetsPerBlock = 5
	// defaultCleanupInterval is the default interval for cleaning up expired cache entries.
	defaultCleanupInterval = 60 * time.Second
)

// LRUStoreConfig contains initialization settings for LRUTokenStore (block size and cache size).
type LRUStoreConfig struct {
	CacheSize             int           `json:"cacheSize"`
	BlockSize             int           `json:"blockSize"` // number of tokens per block
	EnablePodMappingCache bool          `json:"enablePodMappingCache"`
	PodMappingTTL         time.Duration `json:"podMappingTTL"`
	MaxPodSetsPerBlock    int           `json:"maxPodSetsPerBlock"`
	CleanupInterval       time.Duration `json:"cleanupInterval"`
}

// defaultLRUStoreConfig returns an LRUStoreConfig instance with default configuration.
func defaultLRUStoreConfig() *LRUStoreConfig {
	return &LRUStoreConfig{
		CacheSize:             defaultMaxCacheSize,
		BlockSize:             defaultBlockSize,
		EnablePodMappingCache: true,
		PodMappingTTL:         defaultPodMappingTTL,
		MaxPodSetsPerBlock:    defaultMaxPodSetsPerBlock,
		CleanupInterval:       defaultCleanupInterval,
	}
}

// Block holds the tokens contained in the block and cached pod mappings.
// A token is contained iff its [_, high] offset is associated with a substring
// of the chunk that was used to generate the hash (key) of the block.
type Block struct {
	Tokens      []uint32
	PodMappings map[string]*CachedPodMapping // podSetHash -> cached pod mappings
}

// LRUTokenStore is an in-memory prefix-to-block cache with xxhash keys and LRU
// eviction.
// TODO: optimize implementation and check chunk-tokenization vs tokenization-chunking.
type LRUTokenStore struct {
	mu sync.RWMutex

	cacheSize             int
	blockSize             int
	enablePodMappingCache bool
	podMappingTTL         time.Duration
	maxPodSetsPerBlock    int

	store map[string]*lru.Cache[uint64, Block]
}

var _ Indexer = &LRUTokenStore{}

// hashPodSet creates a deterministic hash for a set of pod identifiers.
func hashPodSet(pods []string) string {
	if len(pods) == 0 {
		return "all-pods" // Special case for empty pod filter
	}

	// Sort pods to ensure deterministic hashing regardless of input order
	sortedPods := make([]string, len(pods))
	copy(sortedPods, pods)
	sort.Strings(sortedPods)

	hash := sha256.Sum256([]byte(strings.Join(sortedPods, "|")))
	return fmt.Sprintf("%x", hash)
}

// NewLRUTokenStore initializes the LRUTokenStore with LRU cache.
func NewLRUTokenStore(config *Config) (Indexer, error) {
	if config == nil {
		config = DefaultConfig()
	} // TODO: add validation

	return &LRUTokenStore{
		cacheSize:             config.CacheSize,
		blockSize:             config.BlockSize,
		enablePodMappingCache: config.EnablePodMappingCache,
		podMappingTTL:         config.PodMappingTTL,
		maxPodSetsPerBlock:    config.MaxPodSetsPerBlock,
		store:                 make(map[string]*lru.Cache[uint64, Block]),
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

		block := Block{
			Tokens:      []uint32{},
			PodMappings: make(map[string]*CachedPodMapping),
		}
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

// FindLongestContainedTokensWithPodMappings performs cache-aware lookup that
// returns both tokens and potentially cached pod mappings for the given pod set.
func (c *LRUTokenStore) FindLongestContainedTokensWithPodMappings(prompt, modelName string, podIdentifiers []string) (*CacheResult, error) {
	// 1. Get tokens using existing logic
	tokens := c.FindLongestContainedTokens(prompt, modelName)
	if len(tokens) == 0 {
		fmt.Printf("FastPath: FindLongestContainedTokensWithPodMappings() Found no tokens\n")
		return &CacheResult{CacheHit: false, Tokens: tokens}, nil
	}

	// 2. Check if pod mapping cache is enabled
	if !c.enablePodMappingCache {
		fmt.Printf("FastPath: FindLongestContainedTokensWithPodMappings() caching is disabled !\n")
		return &CacheResult{CacheHit: false, Tokens: tokens}, nil
	}

	// 3. Try to find cached pod mappings
	podSetHash := hashPodSet(podIdentifiers)
	cachedMapping := c.findCachedPodMapping(prompt, modelName, podSetHash)

	if cachedMapping != nil && !c.isExpired(cachedMapping) {
		fmt.Printf("FastPath: FindLongestContainedTokensWithPodMappings() cache hit && not expired mapping %+v!\n", cachedMapping)
		// Cache hit - return cached pod mappings
		return &CacheResult{
			CacheHit: true,
			Tokens:   tokens,
			Mapping:  cachedMapping,
		}, nil
	}

	// Cache miss - return tokens only
	fmt.Printf("FastPath: FindLongestContainedTokensWithPodMappings() cache miss, returning %d tokens only!\n", len(tokens))
	return &CacheResult{CacheHit: false, Tokens: tokens}, nil
}

// findCachedPodMapping searches for cached pod mappings in the blocks corresponding to the prompt.
func (c *LRUTokenStore) findCachedPodMapping(prompt, modelName, podSetHash string) *CachedPodMapping {
	c.mu.RLock()
	cache, ok := c.store[modelName]
	c.mu.RUnlock()

	if !ok {
		fmt.Printf("FastPath: findCachedPodMapping - no cache for model %s\n", modelName)
		return nil
	}
	fmt.Printf("FastPath: findCachedPodMapping() called with podSetHash %+v \n", podSetHash)

	promptBytes := []byte(prompt)
	previousHash := uint64(0)
	digest := xxhash.New()

	// Check blocks in reverse order to find the most recent/complete mapping
	blockHashes := []uint64{}
	for i := 0; i < len(promptBytes); i += c.blockSize {
		end := i + c.blockSize
		if end > len(promptBytes) {
			break // no partial blocks
		}

		digest.Reset()
		if err := binary.Write(digest, binary.LittleEndian, previousHash); err != nil {
			break
		}
		if _, err := digest.Write(promptBytes[i:end]); err != nil {
			break
		}

		blockHash := digest.Sum64()
		previousHash = blockHash
		blockHashes = append(blockHashes, blockHash)
	}

	fmt.Printf("FastPath: findCachedPodMapping computed %d block hashes for prompt\n", len(blockHashes))

	// Search blocks in reverse order (latest first)
	for i := len(blockHashes) - 1; i >= 0; i-- {
		blockHash := blockHashes[i]
		fmt.Printf("FastPath: checking block hash %d (index %d)\n", blockHash, i)

		if block, ok := cache.Get(blockHash); ok {
			fmt.Printf("FastPath: found block %d, checking for pod mappings (count: %d)\n", blockHash, len(block.PodMappings))

			if block.PodMappings == nil {
				fmt.Printf("FastPath: block %d has nil PodMappings\n", blockHash)
				continue
			}

			for existingHash := range block.PodMappings {
				fmt.Printf("FastPath: block %d has pod mapping with hash: %s\n", blockHash, existingHash)
			}

			if mapping, exists := block.PodMappings[podSetHash]; exists {
				fmt.Printf("FastPath: FOUND cached mapping in block %d for podSetHash %s\n", blockHash, podSetHash)
				return mapping
			} else {
				fmt.Printf("FastPath: block %d does not contain podSetHash %s\n", blockHash, podSetHash)
			}
		} else {
			fmt.Printf("FastPath: block hash %d not found in cache\n", blockHash)
		}
	}

	fmt.Printf("FastPath: findCachedPodMapping - no cached mapping found for podSetHash %s\n", podSetHash)
	return nil
}

// isExpired checks if a cached mapping has exceeded its TTL.
func (c *LRUTokenStore) isExpired(mapping *CachedPodMapping) bool {
	return time.Since(mapping.CachedAt) > c.podMappingTTL
}

// CachePodMappings stores pod mapping results for future cache hits.
func (c *LRUTokenStore) CachePodMappings(prompt, modelName string, mapping *CachedPodMapping) error {
	if !c.enablePodMappingCache || mapping == nil {
		fmt.Printf("SlowPath: CachePodMappings skipped - enableCache=%v, mapping=%v\n", c.enablePodMappingCache, mapping != nil)
		return nil // Feature disabled or invalid mapping
	}

	fmt.Printf("SlowPath: CachePodMappings called - enableCache=%v, model=%s\n", c.enablePodMappingCache, modelName)

	c.mu.Lock()
	defer c.mu.Unlock()

	cache, ok := c.store[modelName]
	if !ok {
		fmt.Printf("SlowPath: no cache found for model %s \n", modelName)
		return fmt.Errorf("no cache found for model %s", modelName)
	}

	// Find the target block based on prompt (use the last block of the sequence)
	targetBlockHash := c.findTargetBlockHash(prompt)
	if targetBlockHash == 0 {
		fmt.Printf("SlowPath: could not find target block for prompt (hash=0)\n")
		return fmt.Errorf("could not find target block for prompt")
	}

	fmt.Printf("SlowPath: targetBlockHash=%d\n", targetBlockHash)

	block, ok := cache.Get(targetBlockHash)
	if !ok {
		fmt.Printf("SlowPath: target block %d not found in cache\n", targetBlockHash)
		return fmt.Errorf("target block not found in cache")
	}

	// Initialize PodMappings if nil
	if block.PodMappings == nil {
		block.PodMappings = make(map[string]*CachedPodMapping)
		fmt.Printf("SlowPath: initialized PodMappings map for block %d\n", targetBlockHash)
	}

	// Enforce cache size limits per block
	if len(block.PodMappings) >= c.maxPodSetsPerBlock {
		fmt.Printf("SlowPath: evicting oldest mapping (current count: %d, max: %d)\n", len(block.PodMappings), c.maxPodSetsPerBlock)
		c.evictOldestMapping(block.PodMappings)
	}

	// Store the new mapping
	block.PodMappings[mapping.PodSetHash] = mapping
	fmt.Printf("SlowPath: successfully cached pod mapping with hash=%s, hitKeys=%d\n", mapping.PodSetHash, len(mapping.HitKeys))

	return nil
}

// findTargetBlockHash finds the hash of the last block for a given prompt.
func (c *LRUTokenStore) findTargetBlockHash(prompt string) uint64 {
	promptBytes := []byte(prompt)
	previousHash := uint64(0)
	digest := xxhash.New()

	var lastHash uint64
	for i := 0; i < len(promptBytes); i += c.blockSize {
		end := i + c.blockSize
		if end > len(promptBytes) {
			break
		}

		digest.Reset()
		if err := binary.Write(digest, binary.LittleEndian, previousHash); err != nil {
			break
		}
		if _, err := digest.Write(promptBytes[i:end]); err != nil {
			break
		}

		lastHash = digest.Sum64()
		previousHash = lastHash
	}

	return lastHash
}

// evictOldestMapping removes the oldest cached mapping from a block.
func (c *LRUTokenStore) evictOldestMapping(mappings map[string]*CachedPodMapping) {
	var oldestHash string
	var oldestTime time.Time

	for hash, mapping := range mappings {
		if oldestTime.IsZero() || mapping.CachedAt.Before(oldestTime) {
			oldestTime = mapping.CachedAt
			oldestHash = hash
		}
	}

	if oldestHash != "" {
		delete(mappings, oldestHash)
	}
}

// InvalidatePodMappingsForKeys removes cached pod mappings that depend on the given KV-block keys.
func (c *LRUTokenStore) InvalidatePodMappingsForKeys(keys []kvblock.Key) error {
	if !c.enablePodMappingCache || len(keys) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	invalidatedCount := 0
	for _, modelCache := range c.store {
		for _, blockHash := range modelCache.Keys() {
			if block, ok := modelCache.Get(blockHash); ok {
				for podSetHash, mapping := range block.PodMappings {
					// Check if any of the mapping's keys intersect with the invalidation keys
					if c.mappingContainsKeys(mapping, keys) {
						delete(block.PodMappings, podSetHash)
						invalidatedCount++
					}
				}
			}
		}
	}

	return nil
}

// mappingContainsKeys checks if a cached mapping contains any of the specified keys.
func (c *LRUTokenStore) mappingContainsKeys(mapping *CachedPodMapping, keys []kvblock.Key) bool {
	keySet := make(map[kvblock.Key]bool)
	for _, key := range keys {
		keySet[key] = true
	}

	for _, mappingKey := range mapping.KVBlockKeys {
		if keySet[mappingKey] {
			return true
		}
	}
	for _, hitKey := range mapping.HitKeys {
		if keySet[hitKey] {
			return true
		}
	}

	return false
}

// CleanupExpiredMappings removes expired cache entries based on TTL.
func (c *LRUTokenStore) CleanupExpiredMappings() int {
	if !c.enablePodMappingCache {
		return 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cleanedCount := 0
	for _, modelCache := range c.store {
		for _, blockHash := range modelCache.Keys() {
			if block, ok := modelCache.Get(blockHash); ok {
				for podSetHash, mapping := range block.PodMappings {
					if c.isExpired(mapping) {
						delete(block.PodMappings, podSetHash)
						cleanedCount++
					}
				}
			}
		}
	}

	return cleanedCount
}
