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
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization"
	chattemplatego "github.com/llm-d/llm-d-kv-cache-manager/pkg/tokenization/chat_template_go"
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
	k.tokenizersPool.Run(ctx)
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
	podIdentifiers []string, chatCompletion bool,
) (map[string]int, error) {
	traceLogger := klog.FromContext(ctx).V(logging.TRACE).WithName("kvcache.GetPodScores")

	// Handle chat completion requests
	if chatCompletion {
		// Parse the prompt as a ChatTemplateRequest JSON
		var req chattemplatego.ChatTemplateRequest
		if err := json.Unmarshal([]byte(prompt), &req); err != nil {
			return nil, fmt.Errorf("failed to parse chat template request: %w", err)
		}

		// Create or reuse the CGo wrapper (could be a singleton in production)
		// TODO: cache, instance management
		wrapper := chattemplatego.NewChatTemplateCGoWrapper()

		// Fetch the chat template for the model (if not already set)
		if req.ChatTemplate == "" {
			getReq := chattemplatego.GetChatTemplateRequest{ModelName: modelName}
			template, template_vars, err := wrapper.GetModelChatTemplate(getReq)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch chat template: %w", err)
			}
			req.ChatTemplate = template
			req.TemplateVars = template_vars
		}

		// Apply the template to the request
		resp, err := wrapper.RenderChatTemplate(req)
		if err != nil {
			return nil, fmt.Errorf("failed to render chat template: %w", err)
		}
		if len(resp.RenderedChats) == 0 {
			return nil, nil
		}
		prompt = resp.RenderedChats[0]
	}

	// 0. add to tokenizers pool
	k.tokenizersPool.AddTask(prompt, modelName)

	// 1. get available tokens of longest prefix
	tokens := k.tokensIndexer.FindLongestContainedTokens(prompt, modelName)

	if len(tokens) == 0 {
		//nolint:nilnil // no need to return an error
		return nil, nil
	}

	// 2. get block keys
	blockKeys := k.tokensProcessor.TokensToKVBlockKeys(tokens, modelName)
	traceLogger.Info("found tokens", "tokens", tokens, "block-keys", blockKeys)

	// 3. query kvblock indexer for pods
	strBlockKeys, keyToPods, err := k.kvBlockIndex.Lookup(ctx, blockKeys, sets.New(podIdentifiers...))
	if err != nil {
		return nil, fmt.Errorf("failed to query kvblock indexer: %w", err)
	}
	traceLogger.Info("found block keys", "block-keys", blockKeys,
		"pods", podsPerKeyPrintHelper(keyToPods))

	// 4. score pods
	podScores, err := k.kvBlockScorer.Score(strBlockKeys, keyToPods)
	if err != nil {
		return nil, fmt.Errorf("failed to query kvblock scorer: %w", err)
	}
	traceLogger.Info("found pod scores", "pod-scores", podScores)

	return podScores, nil
}

// GetPodScoresDefault is a convenience function for backward compatibility
// that calls GetPodScores with chatCompletion=false
func (k *Indexer) GetPodScoresDefault(ctx context.Context, prompt, modelName string,
	podIdentifiers []string,
) (map[string]int, error) {
	return k.GetPodScores(ctx, prompt, modelName, podIdentifiers, false)
}

// podsPerKeyPrintHelper formats a map of keys to pod names for printing.
func podsPerKeyPrintHelper(ks map[kvblock.Key][]string) string {
	flattened := ""
	for k, v := range ks {
		flattened += fmt.Sprintf("%s: %v\n", k.String(), v)
	}

	return flattened
}
