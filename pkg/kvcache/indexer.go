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
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization/prefixstore"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils/logging"
)

// Config holds the configuration for the Indexer module.
// The configuration cover the different components found in the Indexer
// module.
type Config struct {
	PrefixStoreConfig    *prefixstore.Config           `json:"prefixStoreConfig"`
	TokenProcessorConfig *kvblock.TokenProcessorConfig `json:"tokenProcessorConfig"`
	KVBlockIndexConfig   *kvblock.IndexConfig          `json:"kvBlockIndexConfig"`
	KVBLockScorerConfig  *KVBlockScorerConfig          // not exported
	TokenizersPoolConfig *tokenization.Config          `json:"tokenizersPoolConfig"`
}

// NewDefaultConfig returns a default configuration for the Indexer module.
func NewDefaultConfig() *Config {
	return &Config{
		PrefixStoreConfig:    prefixstore.DefaultConfig(),
		TokenProcessorConfig: kvblock.DefaultTokenProcessorConfig(),
		KVBlockIndexConfig:   kvblock.DefaultIndexConfig(),
		KVBLockScorerConfig:  DefaultKVBlockScorerConfig(),
		TokenizersPoolConfig: tokenization.DefaultConfig(),
	}
}

// Indexer is a concrete implementation of the KVCacheIndex interface.
type Indexer struct {
	config *Config

	tokensIndexer   prefixstore.Indexer    // gets tokens for a prompt
	tokensProcessor kvblock.TokenProcessor // turns tokens to kv block keys
	kvBlockIndex    kvblock.Index          // looks up pods for block keys
	kvBlockScorer   KVBlockScorer          // scores pods based on block hits

	tokenizersPool *tokenization.Pool
}

// NewKVCacheIndexer creates a KVCacheIndex given a Config.
func NewKVCacheIndexer(ctx context.Context, config *Config) (*Indexer, error) {
	tokensIndexer, err := prefixstore.NewLRUTokenStore(config.PrefixStoreConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create prefixstore.Indexer: %w", err)
	}

	tokensProcessor := kvblock.NewChunkedTokenDatabase(config.TokenProcessorConfig)

	kvBlockIndex, err := kvblock.NewIndex(ctx, config.KVBlockIndexConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create RedisKVBlockIndexer: %w", err)
	}

	scorer, err := NewKVBlockScorer(config.KVBLockScorerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create KVBlockScorer: %w", err)
	}

	tokenizersPool, err := tokenization.NewTokenizationPool(config.TokenizersPoolConfig, tokensIndexer)
	if err != nil {
		return nil, fmt.Errorf("failed to create tokenizers pool: %w", err)
	}

	return &Indexer{
		config:          config,
		tokensIndexer:   tokensIndexer,
		tokensProcessor: tokensProcessor,
		kvBlockIndex:    kvBlockIndex,
		kvBlockScorer:   scorer,
		tokenizersPool:  tokenizersPool,
	}, nil
}

// Run starts the indexer.
func (k *Indexer) Run(ctx context.Context) {
	// Create a span for indexer startup - this will always be generated
	tracer := otel.GetTracerProvider().Tracer("llm-d-epp")
	ctx, span := tracer.Start(ctx, "kv_cache_manager.indexer_startup")
	defer span.End()
	
	span.SetAttributes(
		attribute.String("component", "llm-d-kv-cache-manager"),
		attribute.String("operation", "indexer_run"),
		attribute.String("service.name", "llm-d-kv-cache-manager"),
	)
	
	k.tokenizersPool.Run(ctx)
	span.SetAttributes(attribute.String("operation", "tokenizers_pool_started"))
}

// KVBlockIndex returns the kvblock.Index used by the Indexer.
func (k *Indexer) KVBlockIndex() kvblock.Index {
	return k.kvBlockIndex
}

// GetPodScores retrieves the pod scores for a given prompt and model name.
// The function receives the mentioned information and a list of relevant pod
// identifiers. A Pod identifier should be its address.
// If the set of pod identifiers is empty, the function assumes all pods are
// relevant.
//
// The function returns a map of pod identifiers to scores.
func (k *Indexer) GetPodScores(ctx context.Context, prompt, modelName string,
	podIdentifiers []string,
) (map[string]int, error) {
	tracer := otel.GetTracerProvider().Tracer("llm-d-epp")
	ctx, span := tracer.Start(ctx, "kv-cache-manager.GetPodScores")
	defer span.End()

	span.SetAttributes(
		attribute.String("component", "llm-d-kv-cache-manager"),
		attribute.String("operation", "get_pod_scores"),
		attribute.String("gen_ai.request.model", modelName),
		attribute.Int("llm_d.kv_cache.pod_count", len(podIdentifiers)),
		attribute.Int("llm_d.kv_cache.prompt_length", len(prompt)),
	)

	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvcache.GetPodScores")
	// 0. add to tokenizers pool
	k.tokenizersPool.AddTask(prompt, modelName)

	// 1. get available tokens of longest prefix
	_, tokenSpan := tracer.Start(ctx, "kv_cache_manager.find_tokens")
	tokenSpan.SetAttributes(
		attribute.String("component", "llm-d-kv-cache-manager"),
		attribute.String("operation", "find_longest_contained_tokens"),
	)
	tokens := k.tokensIndexer.FindLongestContainedTokens(prompt, modelName)
	if len(tokens) == 0 {
		tokenSpan.SetAttributes(attribute.Int("tokens.found_count", 0))
		tokenSpan.End()
		//nolint:nilnil // no need to return an error
		return nil, nil
	}
	tokenSpan.SetAttributes(attribute.Int("tokens.found_count", len(tokens)))
	tokenSpan.End()

	// 2. get block keys
	_, blockSpan := tracer.Start(ctx, "kv_cache_manager.tokens_to_block_keys")
	blockSpan.SetAttributes(
		attribute.String("component", "llm-d-kv-cache-manager"),
		attribute.String("operation", "tokens_to_kv_block_keys"),
		attribute.Int("tokens.input_count", len(tokens)),
	)
	blockKeys := k.tokensProcessor.TokensToKVBlockKeys(tokens, modelName)
	blockSpan.SetAttributes(attribute.Int("block_keys.generated_count", len(blockKeys)))
	blockSpan.End()
	traceLogger.Info("found tokens", "tokens", tokens, "block-keys", blockKeys)

	// 3. query kvblock indexer for pods
	_, lookupSpan := tracer.Start(ctx, "kv_cache_manager.lookup_pods")
	lookupSpan.SetAttributes(
		attribute.String("component", "llm-d-kv-cache-manager"),
		attribute.String("operation", "kvblock_index_lookup"),
		attribute.Int("block_keys.count", len(blockKeys)),
	)
	keyToPods, err := k.kvBlockIndex.Lookup(ctx, blockKeys, sets.New(podIdentifiers...))
	if err != nil {
		lookupSpan.SetAttributes(attribute.String("error", "lookup_failed"))
		lookupSpan.End()
		span.RecordError(err)
		return nil, fmt.Errorf("failed to query kvblock indexer: %w", err)
	}
	lookupSpan.SetAttributes(attribute.Int("lookup.result_keys_count", len(keyToPods)))
	lookupSpan.End()
	traceLogger.Info("found block keys", "block-keys", blockKeys,
		"pods", podsPerKeyPrintHelper(keyToPods))

	// 4. score pods
	_, scoreSpan := tracer.Start(ctx, "kv_cache_manager.score_pods")
	scoreSpan.SetAttributes(
		attribute.String("component", "llm-d-kv-cache-manager"),
		attribute.String("operation", "kvblock_scorer_score"),
		attribute.Int("block_keys.count", len(blockKeys)),
	)
	podScores, err := k.kvBlockScorer.Score(blockKeys, keyToPods)
	if err != nil {
		scoreSpan.SetAttributes(attribute.String("error", "scoring_failed"))
		scoreSpan.End()
		span.RecordError(err)
		return nil, fmt.Errorf("failed to query kvblock scorer: %w", err)
	}
	scoreSpan.SetAttributes(attribute.Int("scoring.pod_scores_count", len(podScores)))
	scoreSpan.End()
	traceLogger.Info("found pod scores", "pod-scores", podScores)

	// Calculate hit ratio for observability
	totalPods := len(podIdentifiers)
	if totalPods == 0 {
		// If no specific pods requested, use all pods with scores
		totalPods = len(podScores)
	}

	var hitRatio float64
	if totalPods > 0 {
		hitRatio = float64(len(podScores)) / float64(totalPods)
	}

	span.SetAttributes(
		attribute.Float64("llm_d.kv_cache.hit_ratio", hitRatio),
		attribute.String("operation.outcome", "success"),
		attribute.Int("llm_d.kv_cache.scoring_results_count", len(podScores)),
	)

	return podScores, nil
}

// podsPerKeyPrintHelper formats a map of keys to pod names for printing.
func podsPerKeyPrintHelper(ks map[kvblock.Key][]string) string {
	flattened := ""
	for k, v := range ks {
		flattened += fmt.Sprintf("%s: %v\n", k.String(), v)
	}

	return flattened
}
