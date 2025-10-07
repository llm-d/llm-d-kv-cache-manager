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

package main

import (
	"context"
	"fmt"

	indexerpb "github.com/llm-d/llm-d-kv-cache-manager/api"
	"github.com/llm-d/llm-d-kv-cache-manager/examples/testdata"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvevents"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils"
)

// IndexerService implements the IndexerServiceServer interface.
type IndexerService struct {
	indexerpb.UnimplementedIndexerServiceServer
	indexer      *kvcache.Indexer
	kvEventsPool *kvevents.Pool
}

// NewIndexerService creates a new IndexerService with the given indexer.
func NewIndexerService(pool *kvevents.Pool, indexer *kvcache.Indexer) *IndexerService {
	return &IndexerService{
		indexer:      indexer,
		kvEventsPool: pool,
	}
}

// AddSampleDataToIndexer adds some sample KV cache data for testing purposes.
func (s *IndexerService) AddSampleDataToIndexer(ctx context.Context, modelName string) error {
	// Use the pre-computed test data that matches the testdata.Prompt
	// This simulates what would happen when vLLM pods report KV cache events
	sampleKeys := utils.SliceMap(testdata.PromptHashes, func(h uint64) kvblock.Key {
		return kvblock.Key{
			ModelName: modelName,
			ChunkHash: h,
		}
	})

	// Sample pod entries simulating different pods with different device tiers
	podEntries := []kvblock.PodEntry{
		{PodIdentifier: "pod-1", DeviceTier: "gpu"},
		{PodIdentifier: "pod-2", DeviceTier: "gpu"},
		{PodIdentifier: "pod-3", DeviceTier: "cpu"},
	}

	// Add the sample data to the index
	return s.indexer.KVBlockIndex().Add(ctx, sampleKeys, podEntries)
}

// GetPodScores implements the GetPodScores RPC method.
func (s *IndexerService) GetPodScores(ctx context.Context,
	req *indexerpb.GetPodScoresRequest,
) (*indexerpb.GetPodScoresResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	// Call the underlying indexer
	podScores, err := s.indexer.GetPodScores(ctx, req.Prompt, req.ModelName,
		req.PodIdentifiers)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod scores: %w", err)
	}

	// Convert map[string]int to []*indexerpb.PodScore
	scores := make([]*indexerpb.PodScore, 0, len(podScores))
	for pod, score := range podScores {
		// Check for potential integer overflow
		if score > int(^uint32(0)>>1) || score < int(^uint32(0)>>1)*-1 {
			return nil, fmt.Errorf("score %d for pod %s exceeds int32 range", score, pod)
		}
		scores = append(scores, &indexerpb.PodScore{
			Pod:   pod,
			Score: int32(score),
		})
	}

	return &indexerpb.GetPodScoresResponse{
		Scores: scores,
	}, nil
}
