//go:build !exclude && chat_completions

//nolint // CGo compilation is excluded from linting.

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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/preprocessing/chat_completions_template"
)

const (
	envHFToken  = "HF_TOKEN"
	scorePort   = ":8080"
	zmqEndpoint = "tcp://localhost:5557"
)

// setupPythonPath sets up the Python path environment for CGo compilation.
func setupPythonPath(ctx context.Context) error {
	logger := klog.FromContext(ctx)

	// Check if PYTHONPATH is already set
	pythonPath := os.Getenv("PYTHONPATH")
	if pythonPath == "" {
		err := fmt.Errorf("PYTHONPATH environment variable must be set to run this example")
		logger.Error(err, "PYTHONPATH not set")
		return err
	}

	logger.Info("PYTHONPATH is set", "path", pythonPath)
	return nil
}

// ChatCompletionsRequest represents a chat completions request.
type ChatCompletionsRequest struct {
	Model        string                 `json:"model"`
	Messages     []ChatMessage          `json:"messages"`
	MaxTokens    int                    `json:"maxTokens,omitempty"`
	Temperature  float64                `json:"temperature,omitempty"`
	Stream       bool                   `json:"stream,omitempty"`
	TemplateVars map[string]interface{} `json:"templateVars,omitempty"`
}

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionsResponse represents the response from chat completions.
type ChatCompletionsResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finishReason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"promptTokens"`
		CompletionTokens int `json:"completionTokens"`
		TotalTokens      int `json:"totalTokens"`
	} `json:"usage"`
}

// ScoreRequest represents a request to score a prompt.
type ScoreRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"`
}

// ScoreResponse represents the response from scoring.
type ScoreResponse struct {
	PodScores map[string]int `json:"podScores"`
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt"`
}

func getKVCacheIndexerConfig() *kvcache.Config {
	config := kvcache.NewDefaultConfig()

	huggingFaceToken := os.Getenv(envHFToken)
	if huggingFaceToken != "" {
		config.TokenizersPoolConfig.HuggingFaceToken = huggingFaceToken
	}

	config.TokenProcessorConfig.BlockSize = 256
	config.KVBlockIndexConfig.EnableMetrics = true
	config.KVBlockIndexConfig.MetricsLoggingInterval = 15 * time.Second

	return config
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := klog.FromContext(ctx)
	logger.Info("Starting Unified Offline KV Events Example (Regular + Chat Completions)")

	// Setup Python path environment
	logger.Info("Setting up Python path environment...")
	if err := setupPythonPath(ctx); err != nil {
		logger.Error(err, "Failed to setup Python path")
		return
	}
	logger.Info("Python environment ready")

	// Setup ZMQ publisher
	logger.Info("Initializing ZMQ publisher...")
	publisher, err := NewPublisher(zmqEndpoint)
	if err != nil {
		logger.Error(err, "Failed to create ZMQ publisher")
		return
	}
	defer publisher.Close()
	logger.Info("ZMQ publisher initialized", "endpoint", zmqEndpoint)

	// Setup chat template wrapper
	logger.Info("Initializing chat template wrapper...")
	chatTemplateWrapper, err := setupChatTemplateWrapper()
	if err != nil {
		logger.Error(err, "Failed to setup chat template wrapper")
		return
	}
	defer chatTemplateWrapper.Finalize()
	logger.Info("Chat template wrapper initialized successfully")

	// Setup KV Cache Indexer
	logger.Info("Initializing KV cache indexer...")
	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(ctx, getKVCacheIndexerConfig())
	if err != nil {
		logger.Error(err, "Failed to create KV cache indexer")
		return
	}
	logger.Info("KV cache indexer created successfully")

	go kvCacheIndexer.Run(ctx)
	logger.Info("KV cache indexer started")

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Setup HTTP endpoints for scoring and chat completions
	setupHTTPEndpoints(ctx, kvCacheIndexer, chatTemplateWrapper)

	// Run offline demonstrations
	logger.Info("=== Running Offline Demonstrations ===")

	// Demo 1: Regular KV-cache scoring (from original offline example)
	logger.Info("Demo 1: Regular KV-cache scoring with test prompts")
	runRegularScoringDemo(ctx, kvCacheIndexer, publisher)

	// Demo 2: Chat completions processing (main feature)
	logger.Info("Demo 2: Chat completions processing with template rendering")
	runChatCompletionsDemo(ctx, kvCacheIndexer, chatTemplateWrapper, publisher)

	// Demo 3: Combined scoring - show how chat completions can be scored
	logger.Info("Demo 3: Combined demo - scoring rendered chat templates")
	if err := runCombinedDemo(ctx, kvCacheIndexer, chatTemplateWrapper, publisher); err != nil {
		logger.Error(err, "Failed to run combined demo")
	}

	logger.Info("=== All demonstrations completed ===")
	logger.Info("HTTP server running on http://localhost:8080")
	logger.Info("Available endpoints:")
	logger.Info("  - POST /score_completions - Score prompts using KV-cache")
	logger.Info("  - POST /v1/chat/completions - Chat completions with template processing")
	logger.Info("Press Ctrl+C to exit")

	// Wait for shutdown
	<-ctx.Done()
	logger.Info("Shutting down...")
}

func setupChatTemplateWrapper() (*chat_completions_template.ChatTemplateCGoWrapper, error) {
	wrapper := chat_completions_template.NewChatTemplateCGoWrapper()
	if err := wrapper.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize chat template wrapper: %w", err)
	}
	return wrapper, nil
}

func setupHTTPEndpoints(
	ctx context.Context,
	kvCacheIndexer *kvcache.Indexer,
	chatTemplateWrapper *chat_completions_template.ChatTemplateCGoWrapper,
) {
	logger := klog.FromContext(ctx)

	// Score endpoint for prompts (from offline_chat_completions)
	http.HandleFunc("/score_completions", func(w http.ResponseWriter, r *http.Request) {
		var req ScoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}
		if req.Prompt == "" {
			http.Error(w, "Field 'prompt' required", http.StatusBadRequest)
			return
		}

		// Use KV-cache to score the prompt
		pods, err := kvCacheIndexer.GetPodScores(ctx, req.Prompt, req.Model, nil)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error: %v", err), http.StatusInternalServerError)
			return
		}

		response := ScoreResponse{
			PodScores: pods,
			Model:     req.Model,
			Prompt:    req.Prompt,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			logger.Error(err, "Failed to encode response")
		}
	})

	// Chat completions endpoint with actual KV-cache integration
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Get chat template for the model
		templateReq := chat_completions_template.GetChatTemplateRequest{
			ModelName: req.Model,
		}
		template, templateVars, err := chatTemplateWrapper.GetModelChatTemplate(ctx, templateReq)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get chat template: %v", err), http.StatusInternalServerError)
			return
		}

		// Render the chat template
		renderReq := chat_completions_template.ChatTemplateRequest{
			Conversations: [][]chat_completions_template.ChatMessage{
				func() []chat_completions_template.ChatMessage {
					messages := make([]chat_completions_template.ChatMessage, len(req.Messages))
					for i, msg := range req.Messages {
						messages[i] = chat_completions_template.ChatMessage{
							Role:    msg.Role,
							Content: msg.Content,
						}
					}
					return messages
				}(),
			},
			ChatTemplate: template,
			TemplateVars: func() map[string]interface{} {
				if req.TemplateVars != nil {
					return req.TemplateVars
				}
				return templateVars
			}(),
		}

		response, err := chatTemplateWrapper.RenderChatTemplate(ctx, renderReq)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to render chat template: %v", err), http.StatusInternalServerError)
			return
		}

		// Use KV-cache to score the rendered template
		var renderedPrompt string
		if len(response.RenderedChats) > 0 {
			renderedPrompt = response.RenderedChats[0]
		}

		// Get actual KV-cache scores for the rendered prompt
		pods, err := kvCacheIndexer.GetPodScores(ctx, renderedPrompt, req.Model, nil)
		if err != nil {
			logger.Error(err, "Failed to get KV-cache scores", "prompt", renderedPrompt)
		}

		// Create realistic completion based on template and scoring
		completion := generateCompletionFromTemplate(renderedPrompt, pods)

		// Create response with actual KV-cache integration
		chatResponse := ChatCompletionsResponse{
			ID:      "chatcmpl-" + fmt.Sprintf("%d", time.Now().Unix()),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []struct {
				Index   int `json:"index"`
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finishReason"`
			}{
				{
					Index: 0,
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{
						Role:    "assistant",
						Content: completion,
					},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int `json:"promptTokens"`
				CompletionTokens int `json:"completionTokens"`
				TotalTokens      int `json:"totalTokens"`
			}{
				PromptTokens:     len(renderedPrompt) / 4, // Rough estimate
				CompletionTokens: len(completion) / 4,     // Rough estimate
				TotalTokens:      (len(renderedPrompt) + len(completion)) / 4,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(chatResponse); err != nil {
			logger.Error(err, "Failed to encode chat response")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	})

	// Start HTTP server with proper timeout configuration
	go func() {
		logger.Info("Starting HTTP server", "port", scorePort)
		server := &http.Server{
			Addr:         scorePort,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(err, "HTTP server error")
		}
	}()
}

func generateCompletionFromTemplate(renderedPrompt string, pods map[string]int) string {
	// Generate a more realistic completion based on the rendered template and KV-cache scores
	if len(pods) > 0 {
		totalScore := 0
		for _, score := range pods {
			totalScore += score
		}
		return fmt.Sprintf(
			"Based on the rendered template and KV-cache analysis (total score: %d), "+
				"here's a response that considers the cached context: %s...",
			totalScore, renderedPrompt[:min(100, len(renderedPrompt))])
	}

	return fmt.Sprintf("Response based on template: %s...", renderedPrompt[:min(50, len(renderedPrompt))])
}

func runRegularScoringDemo(ctx context.Context, kvCacheIndexer *kvcache.Indexer, publisher *Publisher) {
	logger := klog.FromContext(ctx)
	logger.Info("Running regular KV-cache scoring demonstration...")

	// Test prompts from the original offline example
	testPrompts := []string{
		"Hello, how are you today?",
		"What is the weather like?",
		"Can you help me with a programming question?",
		"Tell me about machine learning",
	}

	for i, prompt := range testPrompts {
		logger.Info("Processing test prompt", "index", i+1, "prompt", prompt)

		// Score the prompt using KV-cache
		scores, err := kvCacheIndexer.GetPodScores(ctx, prompt, "test-model", nil)
		if err != nil {
			logger.Error(err, "Failed to get scores", "prompt", prompt)
			continue
		}

		logger.Info("Scoring results", "prompt", prompt, "scores", scores)

		// Simulate processing the prompt through the cache system
		if err := simulateKVCacheProcessing(ctx, prompt, "test-model", publisher); err != nil {
			logger.Error(err, "Failed to process through KV-cache", "prompt", prompt)
		}

		time.Sleep(500 * time.Millisecond) // Brief pause between demonstrations
	}

	logger.Info("Regular scoring demonstration completed")
}

func runChatCompletionsDemo(
	ctx context.Context,
	kvCacheIndexer *kvcache.Indexer,
	chatTemplateWrapper *chat_completions_template.ChatTemplateCGoWrapper,
	publisher *Publisher,
) {
	logger := klog.FromContext(ctx)
	logger.Info("Running chat completions demonstration...")

	// Test chat conversations
	testConversations := [][]ChatMessage{
		{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello! Can you help me understand how KV-cache works?"},
		},
		{
			{Role: "user", Content: "What are the benefits of using template rendering?"},
		},
		{
			{Role: "system", Content: "You are an expert in machine learning."},
			{Role: "user", Content: "Explain the concept of attention mechanisms."},
			{Role: "assistant", Content: "Attention mechanisms allow models to focus on relevant parts of the input."},
			{Role: "user", Content: "Can you give me a practical example?"},
		},
	}

	models := []string{
		"ibm-granite/granite-3.3-2b-instruct",
		"microsoft/DialoGPT-medium",
	}

	for modelIdx, model := range models {
		logger.Info("Testing model", "model", model)

		for convIdx, messages := range testConversations {
			logger.Info("Processing conversation", "model", model, "conversation", convIdx+1)

			// Get chat template for the model
			templateReq := chat_completions_template.GetChatTemplateRequest{
				ModelName: model,
			}

			start := time.Now()
			template, templateVars, err := chatTemplateWrapper.GetModelChatTemplate(ctx, templateReq)
			templateFetchTime := time.Since(start)

			if err != nil {
				logger.Error(err, "Failed to get chat template", "model", model)
				continue
			}

			logger.Info("Template fetched", "model", model, "time", templateFetchTime, "template_length", len(template))

			// Render the chat template
			renderReq := chat_completions_template.ChatTemplateRequest{
				Conversations: [][]chat_completions_template.ChatMessage{
					func() []chat_completions_template.ChatMessage {
						chatMessages := make([]chat_completions_template.ChatMessage, len(messages))
						for i, msg := range messages {
							chatMessages[i] = chat_completions_template.ChatMessage{
								Role:    msg.Role,
								Content: msg.Content,
							}
						}
						return chatMessages
					}(),
				},
				ChatTemplate: template,
				TemplateVars: templateVars,
			}

			start = time.Now()
			response, err := chatTemplateWrapper.RenderChatTemplate(ctx, renderReq)
			renderTime := time.Since(start)

			if err != nil {
				logger.Error(err, "Failed to render chat template", "model", model)
				continue
			}

			if len(response.RenderedChats) > 0 {
				renderedChat := response.RenderedChats[0]
				logger.Info("Template rendered", "model", model, "time", renderTime, "rendered_length", len(renderedChat))
				logger.Info("Rendered template preview", "preview", renderedChat[:min(200, len(renderedChat))])

				// Score the rendered template using KV-cache
				scores, err := kvCacheIndexer.GetPodScores(ctx, renderedChat, model, nil)
				if err != nil {
					logger.Error(err, "Failed to score rendered template")
				} else {
					logger.Info("KV-cache scores for rendered template", "scores", scores)
				}

				// Publish chat completion event to ZMQ
				chatEvent := map[string]interface{}{
					"id":                 fmt.Sprintf("chat-%d-%d", modelIdx, convIdx),
					"model":              model,
					"messages":           messages,
					"rendered_template":  renderedChat,
					"kv_scores":          scores,
					"template_vars":      templateVars,
					"timestamp":          time.Now().Unix(),
					"processing_time_ms": (templateFetchTime + renderTime).Milliseconds(),
				}

				topic := fmt.Sprintf("kv.chat.%s", strings.ReplaceAll(model, "/", "_"))
				if err := publisher.PublishEvent(ctx, topic, chatEvent); err != nil {
					logger.Error(err, "Failed to publish chat completion event", "topic", topic)
				}
			}

			time.Sleep(1 * time.Second) // Brief pause between conversations
		}
	}

	logger.Info("Chat completions demonstration completed")
}

func runCombinedDemo(
	ctx context.Context,
	kvCacheIndexer *kvcache.Indexer,
	chatTemplateWrapper *chat_completions_template.ChatTemplateCGoWrapper,
	publisher *Publisher,
) error {
	logger := klog.FromContext(ctx)
	logger.Info("Running combined demonstration (chat completions + KV-cache scoring)...")

	// Example of how chat completions and KV-cache work together
	conversation := []ChatMessage{
		{Role: "system", Content: "You are a helpful AI assistant specialized in explaining technical concepts."},
		{Role: "user", Content: "I'm working on a project that uses KV-cache. Can you explain how it improves performance?"},
	}

	model := "ibm-granite/granite-3.3-2b-instruct"

	// Step 1: Render the chat template
	templateReq := chat_completions_template.GetChatTemplateRequest{
		ModelName: model,
	}

	template, templateVars, err := chatTemplateWrapper.GetModelChatTemplate(ctx, templateReq)
	if err != nil {
		return fmt.Errorf("failed to get chat template: %w", err)
	}

	renderReq := chat_completions_template.ChatTemplateRequest{
		Conversations: [][]chat_completions_template.ChatMessage{
			func() []chat_completions_template.ChatMessage {
				chatMessages := make([]chat_completions_template.ChatMessage, len(conversation))
				for i, msg := range conversation {
					chatMessages[i] = chat_completions_template.ChatMessage{
						Role:    msg.Role,
						Content: msg.Content,
					}
				}
				return chatMessages
			}(),
		},
		ChatTemplate: template,
		TemplateVars: templateVars,
	}

	response, err := chatTemplateWrapper.RenderChatTemplate(ctx, renderReq)
	if err != nil {
		return fmt.Errorf("failed to render chat template: %w", err)
	}

	if len(response.RenderedChats) == 0 {
		return fmt.Errorf("no rendered chats available")
	}

	renderedChat := response.RenderedChats[0]
	logger.Info("Combined demo: Chat template rendered", "length", len(renderedChat))

	// Step 2: Score the rendered template
	scores, err := kvCacheIndexer.GetPodScores(ctx, renderedChat, model, nil)
	if err != nil {
		logger.Error(err, "Failed to get KV-cache scores")
	} else {
		logger.Info("Combined demo: KV-cache scoring results", "scores", scores)
	}

	// Step 3: Process through the cache system to generate events
	if err := simulateKVCacheProcessing(ctx, renderedChat, model, publisher); err != nil {
		logger.Error(err, "Failed to process through KV-cache system")
	}

	// Step 4: Show how this would be used in a real completion
	completion := generateCompletionFromTemplate(renderedChat, scores)
	logger.Info("Combined demo: Generated completion", "completion", completion)

	logger.Info("Combined demonstration completed successfully")
	return nil
}

func simulateKVCacheProcessing(
	ctx context.Context,
	prompt, model string,
	publisher *Publisher,
) error {
	logger := klog.FromContext(ctx)

	// This simulates the processing that would happen in the original offline example
	// Create a mock KV event for demonstration
	hash := fnv.New64a()
	hash.Write([]byte(prompt))
	promptHash := hash.Sum64()

	// Create sample KV data
	kvData := map[string]interface{}{
		"prompt":      prompt,
		"model":       model,
		"timestamp":   time.Now().Unix(),
		"prompt_hash": promptHash,
	}

	// Serialize as msgpack (like the original offline example)
	var buf bytes.Buffer
	encoder := msgpack.NewEncoder(&buf)
	if err := encoder.Encode(kvData); err != nil {
		return fmt.Errorf("failed to encode KV data: %w", err)
	}

	logger.Info("Simulated KV-cache processing", "prompt_hash", promptHash, "data_size", buf.Len())

	// Publish the KV data to ZMQ (real ZMQ publishing, not just simulation)
	topic := fmt.Sprintf("kv.%s", strings.ReplaceAll(model, "/", "_"))
	if err := publisher.PublishEvent(ctx, topic, kvData); err != nil {
		logger.Error(err, "Failed to publish KV event", "topic", topic)
		return err
	}

	logger.Info("Published KV event to ZMQ", "topic", topic, "prompt_hash", promptHash)
	return nil
}
