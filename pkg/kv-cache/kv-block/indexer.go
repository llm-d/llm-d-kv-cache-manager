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

	"k8s.io/apimachinery/pkg/util/sets"
)

// IndexerConfig holds the configuration for the Indexer.
type IndexerConfig struct {
	*RedisIndexConfig
}

// DefaultIndexerConfig returns the default configuration for the KVBlockIndexer.
func DefaultIndexerConfig() *IndexerConfig {
	return &IndexerConfig{
		RedisIndexConfig: defaultRedisIndexConfig(),
	}
}

// Indexer defines the interactions with the KVCache indexing backend.
type Indexer struct {
	index IndexBackend
}

// NewIndexer creates a new Indexer instance with the provided configuration.
func NewIndexer(config *IndexerConfig) (*Indexer, error) {
	index, err := NewRedisIndexBackend(config.RedisIndexConfig)
	if err != nil {
		return nil, err
	}

	return &Indexer{index: index}, nil
}

// GetPodsForKeys receives a list of keys and a set of pod identifiers,
// and retrieves the filtered pods associated with those keys.
// The filtering is done based on the pod identifiers provided.
// If the podIdentifierSet is empty, all pods are returned.
//
// It returns:
// 1. A slice of strings representing the keys.
// 2. A map where the keys are those in (1) and the values are pod names.
// 3. An error if any occurred during the operation.
//
//nolint:gocritic // no need named return values here
func (idxr *Indexer) GetPodsForKeys(ctx context.Context,
	keys []Key, podIdentifierSet sets.Set[string],
) ([]string, map[string][]string, error) {
	return idxr.index.Lookup(ctx, keys, podIdentifierSet)
}
