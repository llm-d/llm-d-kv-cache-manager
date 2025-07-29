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

package kvcache

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization/prefixstore"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils/logging"
)

// Config holds the configuration for the Indexer module.
// The configuration cover the different components found in the Indexer
// module.
type Config struct {
	PrefixStoreConfig    *prefixstore.Config           `json:"prefixStoreConfig"`
	TokenProcessorConfig *kvblock.TokenProcessorConfig `json:"tokenProcessorConfig"`
	KVBlockIndexConfig   *kvblock.IndexConfig          `json:"kvBlockIndexConfig"`
	KVBLockScorerConfig  *KVBlockScorerConfig          // not exported
	TokenizersPoolConfig *tokenization.Config          `json:"tokenizersPoolConfig"`

	// Cache configuration
	EnablePodMappingCache bool          `json:"enablePodMappingCache"` // Feature flag
	CacheCleanupInterval  time.Duration `json:"cacheCleanupInterval"`  // Cleanup interval
}

// NewDefaultConfig returns a default configuration for the Indexer module.
func NewDefaultConfig() *Config {
	return &Config{
		PrefixStoreConfig:     prefixstore.DefaultConfig(),
		TokenProcessorConfig:  kvblock.DefaultTokenProcessorConfig(),
		KVBlockIndexConfig:    kvblock.DefaultIndexConfig(),
		KVBLockScorerConfig:   DefaultKVBlockScorerConfig(),
		TokenizersPoolConfig:  tokenization.DefaultConfig(),
		EnablePodMappingCache: true,             // Enable cache by default
		CacheCleanupInterval:  60 * time.Second, // Cleanup every minute
	}
}

// Indexer is a concrete implementation of the KVCacheIndex interface.
type Indexer struct {
	config *Config

	tokensIndexer   prefixstore.Indexer    // gets tokens for a prompt
	tokensProcessor kvblock.TokenProcessor // turns tokens to kv block keys
	kvBlockIndex    kvblock.Index          // looks up pods for block keys
	kvBlockScorer   KVBlockScorer          // scores pods based on block hits

	tokenizersPool *tokenization.Pool
}

// NewKVCacheIndexer creates a KVCacheIndex given a Config.
func NewKVCacheIndexer(ctx context.Context, config *Config) (*Indexer, error) {
	tokensIndexer, err := prefixstore.NewLRUTokenStore(config.PrefixStoreConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create prefixstore.Indexer: %w", err)
	}

	tokensProcessor := kvblock.NewChunkedTokenDatabase(config.TokenProcessorConfig)

	kvBlockIndex, err := kvblock.NewIndex(ctx, config.KVBlockIndexConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create RedisKVBlockIndexer: %w", err)
	}

	scorer, err := NewKVBlockScorer(config.KVBLockScorerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create KVBlockScorer: %w", err)
	}

	tokenizersPool, err := tokenization.NewTokenizationPool(config.TokenizersPoolConfig, tokensIndexer)
	if err != nil {
		return nil, fmt.Errorf("failed to create tokenizers pool: %w", err)
	}

	return &Indexer{
		config:          config,
		tokensIndexer:   tokensIndexer,
		tokensProcessor: tokensProcessor,
		kvBlockIndex:    kvBlockIndex,
		kvBlockScorer:   scorer,
		tokenizersPool:  tokenizersPool,
	}, nil
}

// Run starts the indexer.
func (k *Indexer) Run(ctx context.Context) {
	k.tokenizersPool.Run(ctx)

	// Start cache cleanup if pod mapping cache is enabled
	if k.config.EnablePodMappingCache && k.config.CacheCleanupInterval > 0 {
		klog.Info("starting cache cleanup", "interval", k.config.CacheCleanupInterval)
		k.StartCacheCleanup(ctx, k.config.CacheCleanupInterval)
	}
}

// KVBlockIndex returns the kvblock.Index used by the Indexer.
func (k *Indexer) KVBlockIndex() kvblock.Index {
	return k.kvBlockIndex
}

// GetPodScores retrieves the pod scores for a given prompt and model name.
// This optimized version first attempts a cache-aware lookup that can skip
// token-to-key conversion and index lookup for cache hits.
//
// The function receives the mentioned information and a list of relevant pod
// identifiers. A Pod identifier should be its address.
// If the set of pod identifiers is empty, the function assumes all pods are
// relevant.
//
// The function returns a map of pod identifiers to scores.
func (k *Indexer) GetPodScores(ctx context.Context, prompt, modelName string,
	podIdentifiers []string,
) (map[string]int, error) {
	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvcache.GetPodScores")
	fmt.Printf("FastPath: GetPodScores() called with podIdentifiers %+v\n modelName %s\n", podIdentifiers, modelName)

	// 0. add to tokenizers pool (unchanged)
	k.tokenizersPool.AddTask(prompt, modelName)

	// 1. FAST PATH: Try cache-aware lookup
	result, err := k.tokensIndexer.FindLongestContainedTokensWithPodMappings(prompt, modelName, podIdentifiers)
	if err != nil {
		return nil, fmt.Errorf("cache lookup failed: %w", err)
		fmt.Printf("FastPath: lookup failed err: %+v\n", err)
	}

	if result.CacheHit && result.Mapping != nil {
		traceLogger.Info("pod mapping cache hit", "tokens", len(result.Tokens), "keys", len(result.Mapping.HitKeys))
		fmt.Printf("FastPath: CACHE HIT: Got cached pod mappings for %d tokens, %d hit keys\n",
			len(result.Tokens), len(result.Mapping.HitKeys))

		// Skip steps 2 & 3 - go directly to scoring
		podScores, err := k.kvBlockScorer.Score(result.Mapping.HitKeys, result.Mapping.KeyToPods)
		if err != nil {
			return nil, fmt.Errorf("failed to score cached pod mappings: %w", err)
		}
		traceLogger.Info("found pod scores from cache", "pod-scores", podScores)
		fmt.Printf("FastPath: found pod scores from cache %+v\n", podScores)

		return podScores, nil
	}

	// 2. SLOW PATH: Cache miss - execute original flow + populate cache
	traceLogger.Info("pod mapping cache miss", "tokens", len(result.Tokens))
	fmt.Printf("FastPath: CACHE MISS: Executing full pipeline for %d tokens\n", len(result.Tokens))

	return k.getPodScoresWithCaching(ctx, prompt, modelName, podIdentifiers, result.Tokens)
}

// getPodScoresWithCaching handles the cache miss case by executing the original flow
// and then caching the results for future requests.
func (k *Indexer) getPodScoresWithCaching(ctx context.Context, prompt, modelName string,
	podIdentifiers []string, tokens []uint32) (map[string]int, error) {

	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvcache.GetPodScoresWithCaching")

	if len(tokens) == 0 {
		//nolint:nilnil // no need to return an error
		return nil, nil
	}

	fmt.Printf("SlowPath: Got LongestContainedTokens: %d tokens\n", len(tokens))

	// Step 2: Convert tokens to KV block keys
	blockKeys := k.tokensProcessor.TokensToKVBlockKeys(tokens, modelName)
	traceLogger.Info("computed block keys", "tokens", len(tokens), "keys", len(blockKeys))

	fmt.Printf("SlowPath: Got blockKeys: %d keys\n", len(blockKeys))

	// Step 3: Query KV block index
	hitKeys, keyToPods, err := k.kvBlockIndex.Lookup(ctx, blockKeys, sets.New(podIdentifiers...))
	if err != nil {
		return nil, fmt.Errorf("kvblock index lookup failed: %w", err)
	}
	traceLogger.Info("index lookup completed", "hit-keys", len(hitKeys), "total-pods", len(keyToPods))
	fmt.Printf("SlowPath:  Index lookup: %d hit keys, pod mappings: %s\n", len(hitKeys), podsPerKeyPrintHelper(keyToPods))

	// Step 4: Score pods
	podScores, err := k.kvBlockScorer.Score(hitKeys, keyToPods)
	if err != nil {
		return nil, fmt.Errorf("pod scoring failed: %w", err)
	}
	traceLogger.Info("pod scoring completed", "pod-scores", podScores)
	fmt.Printf("SlowPath: pod scoring completed %+v\n", podScores)

	// Step 5: Cache the results for future requests
	if len(hitKeys) > 0 {
		podSetHash := k.hashPodSet(podIdentifiers)
	        fmt.Printf("SlowPath: podSetHash %+v\n", podSetHash)
		mapping := &prefixstore.CachedPodMapping{
			KVBlockKeys: blockKeys,
			HitKeys:     hitKeys,
			KeyToPods:   keyToPods,
			CachedAt:    time.Now(),
			PodSetHash:  podSetHash,
		}

	        fmt.Printf("SlowPath: Got mapping %+v\n", mapping)

		if cacheErr := k.tokensIndexer.CachePodMappings(prompt, modelName, mapping); cacheErr != nil {
			traceLogger.Info("failed to cache pod mappings", "error", cacheErr)
	                fmt.Printf("SlowPath: cache Err %+v\n", cacheErr)
			// Don't fail the request, just log the cache error
		} else {
			traceLogger.Info("successfully cached pod mappings", "keys", len(hitKeys), "pod-set-hash", podSetHash)
			fmt.Printf("SlowPath: CACHED: Stored pod mappings for future requests\n")
		}
	}

	fmt.Printf("SlowPath: returning podScores %+v \n", podScores)

	return podScores, nil
}

// hashPodSet creates a deterministic hash for a set of pod identifiers.
// This is used for cache key generation.
func (k *Indexer) hashPodSet(pods []string) string {
	// Use the same logic as in prefixstore
	if len(pods) == 0 {
		return "all-pods"
	}

	// For now, use a simple approach - we could import the prefixstore function
	// but keeping it simple to avoid circular dependencies
	hash := ""
	for _, pod := range pods {
		hash += pod + "|"
	}
	return hash
}

// podsPerKeyPrintHelper formats a map of keys to pod names for printing.
func podsPerKeyPrintHelper(ks map[kvblock.Key][]string) string {
	flattened := ""
	for k, v := range ks {
		flattened += fmt.Sprintf("%s: %v\n", k.String(), v)
	}

	return flattened
}

// InvalidateCacheForKVEvents invalidates cached pod mappings when KV events occur.
// This ensures cache consistency when vLLM pods add/remove KV blocks.
func (k *Indexer) InvalidateCacheForKVEvents(keys []kvblock.Key) error {
	if len(keys) == 0 {
		return nil
	}

	klog.V(logging.TRACE).Info("invalidating cache for KV events", "keys", len(keys))

	if err := k.tokensIndexer.InvalidatePodMappingsForKeys(keys); err != nil {
		return fmt.Errorf("failed to invalidate cache for KV events: %w", err)
	}

	klog.V(logging.TRACE).Info("successfully invalidated cache", "keys", len(keys))
	return nil
}

// StartCacheCleanup starts a background goroutine that periodically cleans up expired cache entries.
func (k *Indexer) StartCacheCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 60 * time.Second // Default cleanup interval
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				klog.Info("cache cleanup stopped")
				return
			case <-ticker.C:
				cleaned := k.tokensIndexer.CleanupExpiredMappings()
				if cleaned > 0 {
					klog.V(2).Info("cleaned expired cache entries", "count", cleaned)
				}
			}
		}
	}()
}

// GetCacheStats returns cache statistics for monitoring.
func (k *Indexer) GetCacheStats() map[string]interface{} {
	// This could be extended to provide detailed cache statistics
	// For now, return basic info
	return map[string]interface{}{
		"cache_cleanup_available": true,
		"invalidation_available":  true,
	}
}
