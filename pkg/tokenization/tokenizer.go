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

package tokenization

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/daulet/tokenizers"
	"github.com/go-logr/logr"
	lru "github.com/hashicorp/golang-lru/v2"
	preprocessing "github.com/llm-d/llm-d-kv-cache-manager/pkg/preprocessing/chat_completions"
	"golang.org/x/sync/singleflight"
	ctrl "sigs.k8s.io/controller-runtime"
)

// tokenizersCacheSize is the size of the LRU cache for tokenizers.
// 1 tokenizer per base-model (NOT LoRAs).
const tokenizersCacheSize = 20

// Tokenizer interface defines the methods for tokenization.
type Tokenizer interface {
	// Encode tokenizes the input string and returns the token IDs and offsets.
	Encode(input, modelName string) ([]uint32, []tokenizers.Offset, error)
	RenderChatTemplate(model string, messages []byte) (string, error)
}

// HFTokenizerConfig holds the configuration for the HuggingFace tokenizer.
type HFTokenizerConfig struct {
	HuggingFaceToken   string `json:"huggingFaceToken"`
	TokenizersCacheDir string `json:"tokenizersCacheDir"` // Directory for caching tokenizers
}

// DefaultHFTokenizerConfig returns a default configuration for the HuggingFace
// tokenizer.
func DefaultHFTokenizerConfig() *HFTokenizerConfig {
	return &HFTokenizerConfig{
		HuggingFaceToken:   "",
		TokenizersCacheDir: getTokenizerCacheDir(),
	}
}

// CachedHFTokenizer is implements the Tokenizer interface using
// bindings to HuggingFace's rust tokenizer.
// The implementation wraps an LRU-cache for holding loaded per-model
// tokenizers.
type CachedHFTokenizer struct {
	cfg           tokenizers.TokenizerConfigOption
	cache         *lru.Cache[string, *tokenizers.Tokenizer]
	group         singleflight.Group
	chatTemplater preprocessing.ChatTemplatingProcessor
}

// NewCachedHFTokenizer creates a new instance of HFTokenizer with the provided configuration.
func NewCachedHFTokenizer(config *HFTokenizerConfig) (Tokenizer, error) {
	var cfg tokenizers.TokenizerConfigOption

	if config.TokenizersCacheDir != "" {
		cfg = tokenizers.WithCacheDir(config.TokenizersCacheDir)
	}
	if config.HuggingFaceToken != "" {
		cfg = tokenizers.WithAuthToken(config.HuggingFaceToken)
	}

	tokenizersCache, err := lru.New[string, *tokenizers.Tokenizer](tokenizersCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tokenizer cache: %w", err)
	}

	chatTemplater := preprocessing.NewChatTemplatingProcessor()
	err = chatTemplater.Initialize()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize chat templater: %w", err)
	}

	return &CachedHFTokenizer{
		cfg:           cfg,
		cache:         tokenizersCache,
		chatTemplater: *chatTemplater,
	}, nil
}

func (t *CachedHFTokenizer) getTokenizer(modelName string) (*tokenizers.Tokenizer, error) {
	tokenizer, ok := t.cache.Get(modelName)
	if !ok {
		result, err, shared := t.group.Do(modelName, func() (any, error) {
			return tokenizers.FromPretrained(modelName, t.cfg)
		})
		if err != nil {
			return nil, err
		}

		tokenizer, ok = result.(*tokenizers.Tokenizer)
		if !ok {
			return nil, fmt.Errorf("unexpected tokenizer type from singleflight result")
		}

		if !shared {
			// Only add to cache if this goroutine actually loaded the tokenizer
			t.cache.Add(modelName, tokenizer)
		}
	}
	return tokenizer, nil
}

// Encode converts a string into token IDs.
func (t *CachedHFTokenizer) Encode(input, modelName string) ([]uint32, []tokenizers.Offset, error) {
	tokenizer, err := t.getTokenizer(modelName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get tokenizer for model %q: %w", modelName, err)
	}

	encodeOptions := []tokenizers.EncodeOption{
		tokenizers.WithReturnTypeIDs(),
		tokenizers.WithReturnOffsets(),
	}

	resp := tokenizer.EncodeWithOptions(input, true, encodeOptions...)
	return resp.IDs, resp.Offsets, nil
}

// getTokenizerCacheDir returns the absolute path to the tokenizer cache directory relative to the project root.
func getTokenizerCacheDir() string {
	_, filename, _, _ := runtime.Caller(0) // this file
	base := filepath.Dir(filename)
	return filepath.Join(base, "..", "..", "bin")
}

func (t *CachedHFTokenizer) RenderChatTemplate(model string, messages []byte) (string, error) {
	conversations := []preprocessing.ChatMessage{}
	err := json.Unmarshal(messages, &conversations)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal chat messages: %w", err)
	}

	ctx := context.TODO()
	ctx = logr.NewContext(ctx, ctrl.Log)

	chatTemplate, chatTemplateKWArgs, err := t.chatTemplater.FetchChatTemplate(ctx, preprocessing.FetchChatTemplateRequest{
		Model: model,
	})
	if err != nil {
		return "", fmt.Errorf("failed to fetch chat template: %w", err)
	}

	res, err := t.chatTemplater.RenderChatTemplate(ctx, &preprocessing.RenderJinjaTemplateRequest{
		Conversations:      conversations,
		ChatTemplate:       chatTemplate,
		ChatTemplateKWArgs: chatTemplateKWArgs,
	})
	if err != nil {
		return "", fmt.Errorf("failed to render chat template: %w", err)
	}
	return res.RenderedChats[0], nil
}
