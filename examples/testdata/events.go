// Copyright 2025 The llm-d Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testdata

import (
	"context"
	"fmt"
	"time"

	"github.com/llm-d/llm-d-kv-cache-manager/examples/unit"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvevents"
	"github.com/vmihailenco/msgpack/v5"
	"k8s.io/klog/v2"
)

func RunEventsDemo(ctx context.Context, kvCacheIndexer *kvcache.Indexer, publisher *unit.Publisher) error {
	logger := klog.FromContext(ctx)

	logger.Info("@@@ Starting KV Events Demo", "model", ModelName)

	// Initial query - should be empty since no events have been published
	pods, err := kvCacheIndexer.GetPodScores(ctx, Prompt, ModelName, nil)
	if err != nil {
		return err
	}
	logger.Info("@@@ Initial pod scores (should be empty)", "pods", pods)

	// Give the subscriber a moment to connect
	time.Sleep(1 * time.Second)

	// Simulate vLLM engine publishing BlockStored events
	logger.Info("@@@ Simulating vLLM engine publishing BlockStored events...")

	blockStoredEvent := kvevents.BlockStored{
		BlockHashes:     PromptHashes,
		ParentBlockHash: nil,
		TokenIds:        []uint32{1, 2, 3},
		BlockSize:       256,
		LoraID:          nil,
	}

	//nolint // won't fail
	blockStoredPayload, _ := msgpack.Marshal(blockStoredEvent.ToTaggedUnion())

	eventBatch := kvevents.EventBatch{
		TS:               float64(time.Now().UnixNano()) / 1e9,
		Events:           []msgpack.RawMessage{blockStoredPayload},
		DataParallelRank: nil,
	}

	topic := fmt.Sprintf("kv@vllm-pod1@%s", ModelName)
	if err := publisher.PublishEvent(ctx, topic, eventBatch); err != nil {
		return fmt.Errorf("failed to publish BlockStored event: %w", err)
	}
	logger.Info("@@@ Published BlockStored event", "topic", topic, "blocks", 3)

	// Wait for events to be processed by the pool
	logger.Info("@@@ Waiting for events to be processed...")
	time.Sleep(3 * time.Second)

	// Query again to see the effect of the events
	pods, err = kvCacheIndexer.GetPodScores(ctx, Prompt, ModelName, nil)
	if err != nil {
		return err
	}
	logger.Info("@@@ Pod scores after BlockStored events", "pods", pods)

	// Simulate removing some blocks
	logger.Info("@@@ Simulating vLLM engine removing some blocks...")

	blockRemovedEvent := kvevents.BlockRemoved{
		BlockHashes: PromptHashes[2:], // Remove last blocks
	}

	//nolint // won't fail
	blockRemovedPayload, _ := msgpack.Marshal(blockRemovedEvent.ToTaggedUnion())

	removeEventBatch := kvevents.EventBatch{
		TS:               float64(time.Now().UnixNano()) / 1e9,
		Events:           []msgpack.RawMessage{blockRemovedPayload},
		DataParallelRank: nil,
	}

	if err := publisher.PublishEvent(ctx, topic, removeEventBatch); err != nil {
		return fmt.Errorf("failed to publish BlockRemoved event: %w", err)
	}
	logger.Info("@@@ Published BlockRemoved event", "topic", topic, "blocks", 2)

	// Wait for removal events to be processed
	time.Sleep(3 * time.Second)

	// Final query
	pods, err = kvCacheIndexer.GetPodScores(ctx, Prompt, ModelName, nil)
	if err != nil {
		return err
	}
	logger.Info("@@@ Final pod scores after BlockRemoved events", "pods", pods)

	logger.Info("Events demo completed. Pool continues listening for more events...")
	logger.Info("Press Ctrl+C to shutdown")

	// Keep running until context is cancelled
	<-ctx.Done()
	return nil
}
