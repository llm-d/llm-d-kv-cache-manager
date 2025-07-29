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
	"time"

	"github.com/daulet/tokenizers"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
)

// Config holds the configuration for the Indexer module.
type Config struct {
	*LRUStoreConfig
}

// DefaultConfig returns the default configuration for the Indexer module.
func DefaultConfig() *Config {
	return &Config{
		LRUStoreConfig: defaultLRUStoreConfig(),
	}
}

// CachedPodMapping represents cached pod mapping results for a specific pod set.
type CachedPodMapping struct {
	KVBlockKeys []kvblock.Key            // Pre-computed from tokens
	HitKeys     []kvblock.Key            // Keys that had cache hits in index
	KeyToPods   map[kvblock.Key][]string // Pod mappings per key
	CachedAt    time.Time                // Timestamp for TTL validation
	PodSetHash  string                   // Hash of pod identifiers for verification
}

// CacheResult represents the result of a cache-aware token lookup.
type CacheResult struct {
	CacheHit bool              // Whether we got a cache hit for pod mappings
	Tokens   []uint32          // Tokens from longest contained prefix
	Mapping  *CachedPodMapping // Pod mappings (only set if CacheHit = true)
}

// Indexer interface defines the methods for managing tokenization data.
// It allows looking up the longest tokenization prefix for a given
// model-name and prompt.
// TODO: generalize interface to a generic prefix-based store.
type Indexer interface {
	// AddTokenization adds the full tokenization of a string to the
	// indexer for a given model.
	// The function assumes tokens and offsets are of the same length.
	// The function assumes that tokens will not be mutated after the call.
	AddTokenization(modelName string, prompt string, tokens []uint32, offsets []tokenizers.Offset) error

	// FindLongestContainedTokens finds the sequence of contained tokens for
	// the longest matching prefix.
	FindLongestContainedTokens(prompt, modelName string) []uint32

	// FindLongestContainedTokensWithPodMappings performs cache-aware lookup that
	// returns both tokens and potentially cached pod mappings for the given pod set.
	// This is the optimized method that can skip token-to-key conversion and index lookup.
	FindLongestContainedTokensWithPodMappings(prompt, modelName string, podIdentifiers []string) (*CacheResult, error)

	// CachePodMappings stores pod mapping results for future cache hits.
	// The mapping will be associated with the prompt prefix that generated the tokens.
	CachePodMappings(prompt, modelName string, mapping *CachedPodMapping) error

	// InvalidatePodMappingsForKeys removes cached pod mappings that depend on the given KV-block keys.
	// This is called when KV-blocks are added/removed from the vLLM fleet.
	InvalidatePodMappingsForKeys(keys []kvblock.Key) error

	// CleanupExpiredMappings removes expired cache entries based on TTL.
	// Returns the number of entries that were cleaned up.
	CleanupExpiredMappings() int
}
