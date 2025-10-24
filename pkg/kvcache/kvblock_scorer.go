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
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils/backend"
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
	BackendConfigs  map[string]*backend.BackendConfig `json:"backendConfigs"`
}

// DefaultKVBlockScorerConfig returns the default configuration for the KVBlockScorer.
func DefaultKVBlockScorerConfig() *KVBlockScorerConfig {
	return &KVBlockScorerConfig{
		ScoringStrategy: LongestPrefixMatch,
		BackendConfigs:  backend.DefaultBackendConfig(),
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
		return &LongestPrefixScorer{
			BackendConfigs: config.BackendConfigs,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported scoring strategy: %s", config.ScoringStrategy)
	}
}

// LongestPrefixScorer scores based on longest consecutive block matches count
// starting from block 0.
type LongestPrefixScorer struct {
	BackendConfigs map[string]*backend.BackendConfig
}

// Strategy returns the strategy type: LongestPrefixMatch.
func (s *LongestPrefixScorer) Strategy() KVScoringStrategy {
	return LongestPrefixMatch
}

// Score implements the longest prefix scoring logic with weighted sum based on BackendConfig.
func (s *LongestPrefixScorer) Score(keys []kvblock.Key, keyToPods map[kvblock.Key][]string) (map[string]int, error) {
	podScores := make(map[string]float64)

	if len(keys) == 0 {
		return make(map[string]int), nil
	}

	activePods := sets.NewString()

	for i := 0; i < len(keys); i++ {
		podsForKey := keyToPods[keys[i]]
		currentPodsSet := sets.NewString(podsForKey...)

		if i == 0 {
			activePods = currentPodsSet
		} else {
			// update active pods to the intersection
			activePods = activePods.Intersection(currentPodsSet)
		}

		if activePods.Len() == 0 {
			break
		}

		for podString := range activePods {
			podIdentifier, deviceTier := extractPodIdentifierAndTier(podString)

			// Get weight for this device tier from BackendConfigs
			weight := 1.0 // default weight
			if s.BackendConfigs != nil {
				if backendConfig, exists := s.BackendConfigs[deviceTier]; exists {
					weight = backendConfig.Weight
				}
			}

			// Add weight for each match using PodIdentifier as key
			podScores[podIdentifier] += weight
		}
	}

	// Convert float64 scores to int
	finalScores := make(map[string]int)
	for podIdentifier, score := range podScores {
		finalScores[podIdentifier] = int(score)
	}

	// Return the map containing the final score for each pod encountered.
	return finalScores, nil
}

// extractPodIdentifierAndTier extracts the PodIdentifier and DeviceTier from a string in the format "PodIdentifier@DeviceTier"
func extractPodIdentifierAndTier(podString string) (string, string) {
	if idx := strings.Index(podString, "@"); idx != -1 {
		return podString[:idx], podString[idx+1:]
	}
	// If no @ found, return the original string as PodIdentifier and empty DeviceTier
	return podString, ""
}
