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
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
)

// KVScoringStrategy defines the strategy used to score pods for KV cache block reuse.
type KVScoringStrategy string

const (
	// LongestPrefixMatch Score by longest consecutive match from start.
	LongestPrefixMatch KVScoringStrategy = "LongestPrefix"
)

// KVBlockScorerConfig holds the configuration for the KVBlockScorer.
type KVBlockScorerConfig struct {
	ScoringStrategy KVScoringStrategy
}

// DefaultKVBlockScorerConfig returns the default configuration for the KVBlockScorer.
func DefaultKVBlockScorerConfig() *KVBlockScorerConfig {
	return &KVBlockScorerConfig{
		ScoringStrategy: LongestPrefixMatch,
	}
}

// KVBlockScorer defines the interface for implementing a KV block scoring
// strategy.
type KVBlockScorer interface {
	// Strategy returns the scoring strategy type.
	Strategy() KVScoringStrategy
	// Score scores the blocks based on the scoring strategy.
	// It returns a map of pod names to their scores.
	Score(keys []kvblock.Key, keyToPods map[kvblock.Key][]string) (map[string]int, error)
}

// NewKVBlockScorer creates a new KVBlockScorer based on the provided strategy.
func NewKVBlockScorer(config *KVBlockScorerConfig) (KVBlockScorer, error) {
	switch config.ScoringStrategy {
	case LongestPrefixMatch:
		return &LongestPrefixScorer{}, nil
	default:
		return nil, fmt.Errorf("unsupported scoring strategy: %s", config.ScoringStrategy)
	}
}

// LongestPrefixScorer scores based on longest consecutive block matches count
// starting from block 0.
type LongestPrefixScorer struct{}

// Strategy returns the strategy type: LongestPrefixMatch.
func (s *LongestPrefixScorer) Strategy() KVScoringStrategy {
	return LongestPrefixMatch
}

// Score implements the longest prefix scoring logic.
func (s *LongestPrefixScorer) Score(keys []kvblock.Key, keyToPods map[kvblock.Key][]string) (map[string]int, error) {
	podScores := make(map[string]int)

	if len(keys) == 0 {
		return podScores, nil
	}

	podsForFirstKey := keyToPods[keys[0]]
	activePods := sets.NewString(podsForFirstKey...)

	// set initial score of 1
	// pods not in the first key will retain the default score of 0.
	for _, pod := range podsForFirstKey {
		podScores[pod] = 1
	}

	for i := 1; i < len(keys); i++ {
		if activePods.Len() == 0 {
			break
		}

		podsForKey := keyToPods[keys[i]]
		currentPodsSet := sets.NewString(podsForKey...)

		// update scores and active pods to the intersection
		activePods = activePods.Intersection(currentPodsSet)
		for pod := range activePods {
			// increment score for each pod in the intersection
			podScores[pod]++
		}
	}

	// Return the map containing the final score for each pod encountered.
	return podScores, nil
}
