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

	"k8s.io/apimachinery/pkg/util/sets"
)

// Key struct represents a unique identifier for a KV-cache block.
type Key struct {
	ModelName string // TODO: eject after aligning LMCache
	ChunkHash string
}

// String returns a string representation of the Key.
func (c *Key) String() string {
	return fmt.Sprintf("%s@%s", c.ModelName, c.ChunkHash)
}

// PodEntry struct represents a pod entry in the KV-block index.
type PodEntry struct {
	// PodIdentifier is the unique identifier for the pod.
	PodIdentifier string
	// DeviceTier is the tier of the device where the KV-block is stored.
	DeviceTier string
}

// String returns a string representation of the PodEntry.
func (e *PodEntry) String() string {
	return fmt.Sprintf("%s@%s", e.PodIdentifier, e.DeviceTier)
}

// IndexBackend defines the interface for a backend that manages KV-block
// indexing.
//
// An index backend is a data store that will aggregate possibly the entire
// global KV cache block index, and will be used to retrieve pod-localities
// for a given set of consecutive keys that constitute a prefix-cache hit.
// The hit may not necessarily be on all keys, but of the longest prefix match.
//
// The index backend allows efficient tracking of which vLLM engines hold which
// KV-blocks, on what device tier, and when they were last updated.
type IndexBackend interface {
	// Lookup receives a list of keys and a set of pod identifiers,
	// and retrieves the filtered pods associated with those keys.
	// The filtering is done based on the pod identifiers provided.
	// If the podIdentifierSet is empty, all pods are returned.
	//
	// It returns:
	// 1. A slice of strings representing the keys.
	// 2. A map where the keys are those in (1) and the values are pod names.
	// 3. An error if any occurred during the operation.
	Lookup(ctx context.Context, keys []Key, podIdentifierSet sets.Set[string]) ([]string, map[string][]string, error)
	// Add adds a set of keys and their associated pod entries to the index backend.
	Add(ctx context.Context, keys []Key, entries []PodEntry) error
	// Evict removes a key and its associated pod entries from the index backend.
	Evict(ctx context.Context, key Key, entries []PodEntry) error
}
