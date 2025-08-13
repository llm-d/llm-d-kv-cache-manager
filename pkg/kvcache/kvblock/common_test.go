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

package kvblock_test

import (
	"testing"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
)

// testAddBasic is a common test helper function for testing basic Add and Lookup functionality.
func testAddBasic(t *testing.T, index kvblock.Index) {
	t.Helper()

	key := kvblock.Key{ModelName: "12345", ChunkHash: 12345}
	entries := []kvblock.PodEntry{
		{PodIdentifier: "10.0.0.1", DeviceTier: "gpu"},
		{PodIdentifier: "10.0.0.2", DeviceTier: "gpu"},
	}

	// Add entries
	err := index.Add(t.Context(), []kvblock.Key{key}, entries)
	assert.NoError(t, err)

	// Lookup after add
	podsPerKey, err := index.Lookup(t.Context(), []kvblock.Key{key}, sets.Set[string]{})
	assert.NoError(t, err)
	assert.Len(t, podsPerKey, 1)
	assert.Contains(t, podsPerKey, key)
	assert.Equal(t, podsPerKey[key], []string{"10.0.0.1", "10.0.0.2"})
}
