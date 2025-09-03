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
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/dustin/go-humanize"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils/logging"
)

const (
	defaultNumCounters = 1e8                    // 100M keys
	defaultIndexSize   = 2 * 1024 * 1024 * 1024 // 2 GiB in bytes
	defaultBufferItems = 64                     // default buffer size for ristretto
)

// CostAwareMemoryIndexConfig holds the configuration for the CostAwareMemoryIndex.
type CostAwareMemoryIndexConfig struct {
	// Size is the maximum memory size that can be used by the index.
	// Supports human-readable formats like "2GiB", "500MiB", "1GB", etc.
	Size string `json:"size,omitempty"`
}

func DefaultCostAwareMemoryIndexConfig() *CostAwareMemoryIndexConfig {
	return &CostAwareMemoryIndexConfig{
		Size: "2GiB", // 2GiB default size
	}
}

// NewCostAwareMemoryIndex creates a new CostAwareMemoryIndex instance.
func NewCostAwareMemoryIndex(cfg *CostAwareMemoryIndexConfig) (*CostAwareMemoryIndex, error) {
	if cfg == nil {
		cfg = DefaultCostAwareMemoryIndexConfig()
	}

	// Parse the size string to get byte value using go-humanize

	sizeBytes, err := humanize.ParseBytes(cfg.Size)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cost aware index: %w", err)
	}
	cache, err := ristretto.NewCache(&ristretto.Config[string, *CostPodCache]{
		NumCounters: defaultNumCounters, // number of keys to track.
		MaxCost:     int64(sizeBytes),   // #nosec G115 , maximum cost of cache
		BufferItems: defaultBufferItems, // number of keys per Get buffer.
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cost aware index: %w", err)
	}

	return &CostAwareMemoryIndex{
		data: cache,
	}, nil
}

// CostAwareMemoryIndex implements the Index interface using Ristretto cache for cost-aware memory management.
type CostAwareMemoryIndex struct {
	// data holds the mapping of keys to sets of pod identifiers.
	data *ristretto.Cache[string, *CostPodCache]
	// mu protects concurrent access to the index operations
	mu sync.RWMutex
}

func (m *CostAwareMemoryIndex) MaxCost() int64 {
	return m.data.MaxCost()
}

// CostPodCache wraps a sync.Map of PodEntry and provides cost calculation for memory usage estimation.
type CostPodCache struct {
	cache sync.Map // map[PodEntry]struct{}
}

// Add adds a PodEntry to the cache.
func (c *CostPodCache) Add(entry PodEntry) {
	c.cache.Store(entry, struct{}{})
}

// Len returns the number of entries in the cache.
func (c *CostPodCache) Len() int {
	count := 0
	c.cache.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// CalculateByteSize estimates memory usage for ristretto cost calculation.
// This is an approximation used for cache eviction decisions.
func (c *CostPodCache) CalculateByteSize(keyStr string) int64 {
	var totalBytes int64
	var entryCount int64

	// Key string memory usage
	totalBytes += int64(len(keyStr))

	// CostPodCache struct overhead (sync.Map overhead)
	totalBytes += 64 // approximate sync.Map overhead

	// Count entries and calculate their size
	c.cache.Range(func(key, value interface{}) bool {
		entry, ok := key.(PodEntry)
		if !ok {
			return true
		}

		entryCount++
		totalBytes += int64(len(entry.PodIdentifier)) // PodIdentifier string content
		totalBytes += int64(len(entry.DeviceTier))    // DeviceTier string content
		totalBytes += 32                              // string headers (16 bytes each for 2 strings)
		totalBytes += 8                               // struct padding/alignment
		return true
	})

	// sync.Map overhead estimation
	if entryCount > 0 {
		// Map overhead: assuming 24 bytes per entry (key+value+metadata in sync.Map)
		totalBytes += entryCount * 24
	}

	return totalBytes
}

var _ Index = &CostAwareMemoryIndex{}

// Add adds a set of keys and their associated pod entries to the index backend.
func (m *CostAwareMemoryIndex) Add(ctx context.Context, keys []Key, entries []PodEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(keys) == 0 || len(entries) == 0 {
		return fmt.Errorf("no keys or entries provided for adding to index")
	}

	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvblock.CostAwareMemoryIndex.Add")

	for _, key := range keys {
		keyStr := key.String()
		podCache, found := m.data.Get(keyStr)
		if !found {
			podCache = &CostPodCache{}
		}

		for _, entry := range entries {
			podCache.cache.Store(entry, struct{}{})
		}

		// Calculate the actual cost for this cache entry
		cost := podCache.CalculateByteSize(keyStr)
		m.data.Set(keyStr, podCache, cost)
		traceLogger.Info("added pods to key", "key", key, "pods", entries, "cost-bytes", cost)
	}
	m.data.Wait()
	return nil
}

func (m *CostAwareMemoryIndex) Lookup(ctx context.Context, keys []Key,
	podIdentifierSet sets.Set[string],
) (map[Key][]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(keys) == 0 {
		return nil, fmt.Errorf("no keys provided for lookup")
	}

	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvblock.CostAwareMemoryIndex.Lookup")

	podsPerKey := make(map[Key][]string)
	highestHitIdx := 0

	for idx, key := range keys {
		keyStr := key.String()
		if pods, found := m.data.Get(keyStr); found { //nolint:nestif // TODO: can this be optimized?
			if pods == nil || pods.Len() == 0 {
				traceLogger.Info("no pods found for key, cutting search", "key", key)
				return podsPerKey, nil // early stop since prefix-chain breaks here
			}

			highestHitIdx = idx

			if podIdentifierSet.Len() == 0 {
				// If no pod identifiers are provided, return all pods
				pods.cache.Range(func(k, value interface{}) bool {
					if pod, ok := k.(PodEntry); ok {
						podsPerKey[key] = append(podsPerKey[key], pod.PodIdentifier)
					}
					return true
				})
			} else {
				// Filter pods based on the provided pod identifiers
				pods.cache.Range(func(k, value interface{}) bool {
					if pod, ok := k.(PodEntry); ok {
						if podIdentifierSet.Has(pod.PodIdentifier) {
							podsPerKey[key] = append(podsPerKey[key], pod.PodIdentifier)
						}
					}
					return true
				})
			}
		} else {
			traceLogger.Info("key not found in index", "key", key)
		}
	}

	traceLogger.Info("lookup completed", "highest-hit-index", highestHitIdx,
		"pods-per-key", podsPerKeyPrintHelper(podsPerKey))

	return podsPerKey, nil
}

// Evict removes a key and its associated pod entries from the index backend.
func (m *CostAwareMemoryIndex) Evict(ctx context.Context, key Key, entries []PodEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(entries) == 0 {
		return fmt.Errorf("no entries provided for eviction from index")
	}

	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvblock.CostAwareMemoryIndex.Evict")
	keyStr := key.String()
	podCache, found := m.data.Get(keyStr)
	if !found || podCache == nil {
		traceLogger.Info("key not found in index, nothing to evict", "key", key)
		return nil
	}

	podCacheLenBefore := podCache.Len()

	for _, entry := range entries {
		podCache.cache.Delete(entry)
	}

	if podCache.Len() == 0 {
		m.data.Del(keyStr)
		traceLogger.Info("evicted key from index as no pods remain", "key", key)
	} else if podCacheLenBefore != podCache.Len() {
		m.data.Set(keyStr, podCache, podCache.CalculateByteSize(keyStr))
		traceLogger.Info("evicted pods from key", "key", key, "pods", entries)
	}
	m.data.Wait()
	return nil
}
