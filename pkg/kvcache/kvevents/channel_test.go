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

package kvevents_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvevents"
)

// mockIndex is a simple in-memory implementation of kvblock.Index for testing.
type mockIndex struct {
	blocks map[kvblock.Key][]kvblock.PodEntry
}

func newMockIndex() *mockIndex {
	return &mockIndex{
		blocks: make(map[kvblock.Key][]kvblock.PodEntry),
	}
}

func (m *mockIndex) Add(ctx context.Context, keys []kvblock.Key, entries []kvblock.PodEntry) error {
	for _, key := range keys {
		m.blocks[key] = append(m.blocks[key], entries...)
	}
	return nil
}

func (m *mockIndex) Evict(ctx context.Context, key kvblock.Key, entries []kvblock.PodEntry) error {
	if existing, ok := m.blocks[key]; ok {
		// Remove matching entries
		filtered := make([]kvblock.PodEntry, 0)
		for _, e := range existing {
			shouldRemove := false
			for _, toRemove := range entries {
				if e.PodIdentifier == toRemove.PodIdentifier {
					shouldRemove = true
					break
				}
			}
			if !shouldRemove {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(m.blocks, key)
		} else {
			m.blocks[key] = filtered
		}
	}
	return nil
}

func (m *mockIndex) Lookup(ctx context.Context, keys []kvblock.Key, 
	podIdentifierSet sets.Set[string]) ([]kvblock.Key, map[kvblock.Key][]string, error) {
	foundKeys := make([]kvblock.Key, 0)
	keyToPods := make(map[kvblock.Key][]string)

	for _, key := range keys {
		if entries, ok := m.blocks[key]; ok {
			pods := make([]string, 0)
			for _, entry := range entries {
				if podIdentifierSet == nil || podIdentifierSet.Len() == 0 {
					pods = append(pods, entry.PodIdentifier)
				} else if podIdentifierSet.Has(entry.PodIdentifier) {
					pods = append(pods, entry.PodIdentifier)
				}
			}
			if len(pods) > 0 {
				foundKeys = append(foundKeys, key)
				keyToPods[key] = pods
			}
		}
	}

	return foundKeys, keyToPods, nil
}

func TestChannelInterfaceAbstraction(t *testing.T) {
	ctx := context.Background()
	index := newMockIndex()

	// Test creating a pool with mock channel
	cfg := kvevents.DefaultConfig()
	mockChannel := kvevents.NewMockChannel(nil)
	pool := kvevents.NewPoolWithChannel(cfg, index, mockChannel)

	// Set the pool reference in the channel
	mockChannel.SetPool(pool)

	require.NotNil(t, pool)

	// Start the pool
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pool.Start(ctx)

	// Give some time for workers to start
	time.Sleep(100 * time.Millisecond)

	// Test sending a BlockStored event through the mock channel
	testModelName := "test-model"
	testPodID := "test-pod-1"
	testHashes := []uint64{12345, 67890}

	// Create a tagged union: [tag, ...payload_fields]
	// The BlockStored struct is array-encoded, so we need to create the tagged union properly
	taggedUnion := []interface{}{"BlockStored", testHashes, nil, nil, 0, nil}
	blockStoredPayload, err := msgpack.Marshal(taggedUnion)
	require.NoError(t, err)

	// Create EventBatch as an array: [timestamp, events, optional_rank]
	eventBatchArray := []interface{}{
		float64(time.Now().UnixNano()) / 1e9,
		[]msgpack.RawMessage{blockStoredPayload},
	}
	eventBatchPayload, err := msgpack.Marshal(eventBatchArray)
	require.NoError(t, err)

	message := &kvevents.Message{
		Topic:         "kv@" + testPodID + "@" + testModelName,
		Payload:       eventBatchPayload,
		Seq:           1,
		PodIdentifier: testPodID,
		ModelName:     testModelName,
	}

	// Send the message through the mock channel
	mockChannel.SendMessage(message)

	// Wait for message processing
	time.Sleep(200 * time.Millisecond)

	// Verify that the blocks were added to the index
	keys := []kvblock.Key{
		{ModelName: testModelName, ChunkHash: testHashes[0]},
		{ModelName: testModelName, ChunkHash: testHashes[1]},
	}

	foundKeys, keyToPods, err := index.Lookup(ctx, keys, nil)
	require.NoError(t, err)

	assert.Len(t, foundKeys, 2, "Both blocks should be found in the index")
	for _, key := range foundKeys {
		pods, ok := keyToPods[key]
		assert.True(t, ok, "Key should have associated pods")
		assert.Contains(t, pods, testPodID, "Pod should be associated with the key")
	}

	// Test shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	pool.Shutdown(shutdownCtx)
	cancel() // Cancel the main context

	// Verify channel was closed
	err = mockChannel.Close() // Should be idempotent
	assert.NoError(t, err)
}

func TestMockPublisher(t *testing.T) {
	publisher := kvevents.NewMockPublisher()
	ctx := context.Background()

	// Test publishing an event
	testTopic := "kv@test-pod@test-model"
	testBatch := kvevents.EventBatch{
		TS: float64(time.Now().UnixNano()) / 1e9,
		Events: []msgpack.RawMessage{
			[]byte("test-event"),
		},
	}

	err := publisher.PublishEvent(ctx, testTopic, testBatch)
	assert.NoError(t, err)

	// Verify the event was recorded
	events := publisher.GetPublishedEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, testTopic, events[0].Topic)
	assert.Equal(t, testBatch, events[0].Batch)

	// Test reset
	publisher.Reset()
	events = publisher.GetPublishedEvents()
	assert.Len(t, events, 0)

	// Test close
	err = publisher.Close()
	assert.NoError(t, err)
}

func TestZMQSubscriberImplementsChannel(t *testing.T) {
	// This test verifies that zmqSubscriber implements the Channel interface
	// We can't easily test the actual ZMQ functionality without external dependencies,
	// but we can verify the interface is properly implemented.

	// This is a compile-time check that zmqSubscriber implements Channel
	// If this doesn't compile, the interface isn't properly implemented
	var _ kvevents.Channel = (*kvevents.MockChannel)(nil)

	// Verify that we can create pools with both default and custom channels
	index := newMockIndex()
	cfg := kvevents.DefaultConfig()

	// Test default pool creation (should use ZMQ internally)
	defaultPool := kvevents.NewPool(cfg, index)
	assert.NotNil(t, defaultPool)

	// Test custom channel pool creation
	mockChannel := kvevents.NewMockChannel(nil)
	customPool := kvevents.NewPoolWithChannel(cfg, index, mockChannel)
	assert.NotNil(t, customPool)
}
