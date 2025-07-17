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

package kvblock

import (
	"context"
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils/logging"
)

const (
	defaultInMemoryIndexSize = 1e8 // TODO: change to memory-size based configuration
	defaultPodsPerKey        = 10  // number of pods per key
)

// InMemoryIndexConfig holds the configuration for the InMemoryIndex.
type InMemoryIndexConfig struct {
	// Size is the maximum number of keys that can be stored in the index.
	Size int `json:"size"`
	// PodCacheSize is the maximum number of pod entries per key.
	PodCacheSize int `json:"podCacheSize"`
}

// DefaultInMemoryIndexConfig returns a default configuration for the InMemoryIndex.
func DefaultInMemoryIndexConfig() *InMemoryIndexConfig {
	return &InMemoryIndexConfig{
		Size:         defaultInMemoryIndexSize,
		PodCacheSize: defaultPodsPerKey,
	}
}

// NewInMemoryIndex creates a new InMemoryIndex instance.
func NewInMemoryIndex(cfg *InMemoryIndexConfig) (*InMemoryIndex, error) {
	if cfg == nil {
		cfg = DefaultInMemoryIndexConfig()
	}

	cache, err := lru.New[Key, *PodCache](cfg.Size)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize in-memory index: %w", err)
	}

	return &InMemoryIndex{
		data:         cache,
		podCacheSize: cfg.PodCacheSize,
	}, nil
}

// InMemoryIndex is an in-memory implementation of the Index interface.
type InMemoryIndex struct {
	// data holds the mapping of keys to sets of pod identifiers.
	data *lru.Cache[Key, *PodCache]
	// podCacheSize is the maximum number of pod entries per key.
	podCacheSize int
}

var _ Index = &InMemoryIndex{}

// PodCache represents a cache for pod entries.
type PodCache struct {
	// cache is an LRU cache that maps PodEntry to their last access time.
	// thread-safe.
	cache *lru.Cache[PodEntry, struct{}]
}

// Lookup receives a list of keys and a set of pod identifiers,
// and retrieves the filtered pods associated with those keys.
// The filtering is done based on the pod identifiers provided.
// If the podIdentifierSet is empty, all pods are returned.
//
// It returns:
// 1. A slice of the hit keys.
// 2. A map where the keys are those in (1) and the values are pod-identifiers.
// 3. An error if any occurred during the operation.
func (m *InMemoryIndex) Lookup(ctx context.Context, keys []Key,
	podIdentifierSet sets.Set[string],
) ([]Key, map[Key][]string, error) {
	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("no keys provided for lookup")
	}

	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvblock.InMemoryIndex.Lookup")

	podsPerKey := make(map[Key][]string)
	highestHitIdx := 0

	for idx, key := range keys {
		if pods, found := m.data.Get(key); found { //nolint:nestif // TODO: can this be optimized?
			if pods == nil || pods.cache.Len() == 0 {
				traceLogger.Info("no pods found for key, cutting search", "key", key)
				return keys[:idx], podsPerKey, nil // early stop since prefix-chain breaks here
			}

			highestHitIdx = idx

			if podIdentifierSet.Len() == 0 {
				// If no pod identifiers are provided, return all pods
				podsPerKey[key] = append(podsPerKey[key],
					utils.SliceMap(pods.cache.Keys(), func(pod PodEntry) string {
						return pod.PodIdentifier
					})...)
			} else {
				// Filter pods based on the provided pod identifiers
				for _, pod := range pods.cache.Keys() {
					if podIdentifierSet.Has(pod.PodIdentifier) {
						podsPerKey[key] = append(podsPerKey[key], pod.PodIdentifier)
					}
				}
			}
		} else {
			traceLogger.Info("key not found in index", "key", key)
		}
	}

	traceLogger.Info("lookup completed", "highest-hit-index", highestHitIdx,
		"pods-per-key", podsPerKeyPrintHelper(podsPerKey))

	return keys[:highestHitIdx+1], podsPerKey, nil
}

// Add adds a set of keys and their associated pod entries to the index backend.
func (m *InMemoryIndex) Add(ctx context.Context, keys []Key, entries []PodEntry) error {
	if len(keys) == 0 || len(entries) == 0 {
		return fmt.Errorf("no keys or entries provided for adding to index")
	}

	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvblock.InMemoryIndex.Add")

	for _, key := range keys {
		podCache, found := m.data.Get(key) // bumps LRU timestamp if found
		if !found {
			cache, err := lru.New[PodEntry, struct{}](m.podCacheSize)
			if err != nil {
				return fmt.Errorf("failed to create pod cache for key %s: %w", key.String(), err)
			}

			podCache = &PodCache{
				cache: cache,
			}

			m.data.ContainsOrAdd(key, podCache)
		}

		for _, entry := range entries {
			podCache.cache.Add(entry, struct{}{}) // TODO: can this be batched to avoid multiple locks?
		}

		traceLogger.Info("added pods to key", "key", key, "pods", entries)
	}

	return nil
}

// Evict removes a key and its associated pod entries from the index backend.
func (m *InMemoryIndex) Evict(ctx context.Context, key Key, entries []PodEntry) error {
	if len(entries) == 0 {
		return fmt.Errorf("no entries provided for eviction from index")
	}

	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvblock.InMemoryIndex.Evict")

	podCache, found := m.data.Get(key)
	if !found || podCache == nil {
		traceLogger.Info("key not found in index, nothing to evict", "key", key)
		return nil
	}

	for _, entry := range entries {
		podCache.cache.Remove(entry) // TODO: can this be batched to avoid multiple locks?
	}

	traceLogger.Info("evicted pods from key", "key", key, "pods", entries)

	if podCache.cache.Len() == 0 {
		m.data.Remove(key)
		traceLogger.Info("evicted key from index as no pods remain", "key", key)
	}

	return nil
}

// podsPerKeyPrintHelper formats a map of keys to pod names for printing.
func podsPerKeyPrintHelper(ks map[Key][]string) string {
	flattened := ""
	for k, v := range ks {
		flattened += fmt.Sprintf("%s: %v\n", k.String(), v)
	}

	return flattened
}
