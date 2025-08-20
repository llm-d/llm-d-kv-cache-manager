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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvevents"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/preprocessing/chat_completions_template"
)

const (
	envHFToken     = "HF_TOKEN"
	envModelName   = "MODEL_NAME"
	envZMQEndpoint = "ZMQ_ENDPOINT"
	envZMQTopic    = "ZMQ_TOPIC"

	envPoolConcurrency = "POOL_CONCURRENCY"
	defaultZMQEndpoint = "tcp://localhost:5557"
	defaultZMQTopic    = "kv@"
	defaultConcurrency = 4

	pythonHashSeed  = "PYTHONHASHSEED"
	blockSizeEnvVar = "BLOCK_SIZE"

	envHTTPPort     = "HTTP_PORT"
	defaultHTTPPort = "8080"
)

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

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := klog.FromContext(ctx)
	logger.Info("Starting Unified Online KV Events Example (Regular + Chat Completions)")

	// Check if running inside container
	isContainer := os.Getenv("CONTAINER_ENV") == "true"

	if !isContainer {
		// Running on host - orchestrate Docker container
		logger.Info("Running on host - orchestrating Docker container")
		if err := runDockerOrchestration(ctx); err != nil {
			logger.Error(err, "Failed to orchestrate Docker container")
			return
		}
		return
	}

	// Running inside container - start the actual KV-cache service
	logger.Info("Running inside container - starting unified KV-cache service")
	if err := runUnifiedKVCacheService(ctx); err != nil {
		logger.Error(err, "Failed to run unified KV-cache service")
		return
	}
}

func runDockerOrchestration(ctx context.Context) error {
	logger := klog.FromContext(ctx)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	containerName := "kv-cache-unified-online"
	imageName := "kv-cache-unified-online"

	// Clean up any existing container
	logger.Info("Cleaning up any existing container...")
	if err := removeContainer(ctx, containerName); err != nil {
		logger.Info("No existing container to remove (this is normal)")
	}

	// Build Docker image
	logger.Info("Building Docker image...")
	if err := buildDockerImage(ctx, imageName); err != nil {
		return fmt.Errorf("failed to build Docker image: %w", err)
	}

	// Run container
	logger.Info("Starting Docker container...")
	if err := runContainer(ctx, containerName, imageName); err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	// Wait for container to be ready
	logger.Info("Waiting for container to be ready...")
	if err := waitForContainer(ctx, 30*time.Second); err != nil {
		logger.Error(err, "Container failed to become ready")
		removeContainer(ctx, containerName)
		return err
	}

	logger.Info("🚀 Unified Online KV Events Example is ready!")
	logger.Info("Available endpoints:")
	logger.Info("  - POST http://localhost:8080/score - Score prompts using KV-cache (original)")
	logger.Info("  - POST http://localhost:8080/score_completions - Score chat completions with template rendering")
	logger.Info("  - POST http://localhost:8080/v1/chat/completions - Full chat completions with template processing")
	logger.Info("Press Ctrl+C to stop")

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutting down...")

	// Clean up container
	if err := removeContainer(ctx, containerName); err != nil {
		logger.Error(err, "Failed to remove container")
	}

	return nil
}

func buildDockerImage(ctx context.Context, imageName string) error {
	logger := klog.FromContext(ctx)

	cmd := exec.CommandContext(ctx, "podman", "build",
		"--platform", "linux/amd64",
		"--build-arg", "TARGETOS=linux",
		"--build-arg", "TARGETARCH=amd64",
		"-f", "examples/kv_events/unified_online/Dockerfile",
		"-t", imageName,
		".")

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Info("Building Docker image", "command", cmd.String())
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	return nil
}

func runContainer(ctx context.Context, containerName, imageName string) error {
	logger := klog.FromContext(ctx)

	cmd := exec.CommandContext(ctx, "podman", "run",
		"-d",
		"--name", containerName,
		"-p", "8080:8080",
		"-e", "CONTAINER_ENV=true",
		"-e", "MODEL_NAME=ibm-granite/granite-3.3-2b-instruct",
		"-e", "ZMQ_ENDPOINT=tcp://localhost:5557",
		"-e", "ZMQ_TOPIC=kv@",
		"-e", "HTTP_PORT=8080",
		imageName)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Info("Starting container", "command", cmd.String())
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

func removeContainer(ctx context.Context, containerName string) error {
	// Stop container
	stopCmd := exec.CommandContext(ctx, "podman", "stop", containerName)
	if err := stopCmd.Run(); err != nil {
		// Log but don't fail - container might not be running
		logger := klog.FromContext(ctx)
		logger.V(1).Info("Container stop failed (might not be running)", "error", err)
	}

	// Remove container
	rmCmd := exec.CommandContext(ctx, "podman", "rm", "-f", containerName)
	return rmCmd.Run()
}

func waitForContainer(ctx context.Context, timeout time.Duration) error {
	logger := klog.FromContext(ctx)

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for container to be ready")
		case <-ticker.C:
			// Check if container is responding
			req, err := http.NewRequestWithContext(timeoutCtx, "GET", "http://localhost:8080/health", nil)
			if err != nil {
				logger.Error(err, "Failed to create health check request")
				continue
			}
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				logger.Info("Container is ready!")
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
			logger.Info("Waiting for container to be ready...")
		}
	}
}

func runUnifiedKVCacheService(ctx context.Context) error {
	logger := klog.FromContext(ctx)

	// Create a cancellable context for the service
	serviceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Setup Python path environment for chat completions
	logger.Info("Setting up Python path environment...")
	if err := setupPythonPath(ctx); err != nil {
		logger.Error(err, "Failed to setup Python path")
		return err
	}

	// Setup chat template wrapper for chat completions
	logger.Info("Initializing chat template wrapper...")
	chatTemplateWrapper, err := setupChatTemplateWrapper()
	if err != nil {
		logger.Error(err, "Failed to setup chat template wrapper")
		return err
	}
	defer chatTemplateWrapper.Finalize()
	logger.Info("Chat template wrapper initialized successfully")

	// Setup KV Cache Indexer (from original online example)
	kvCacheIndexer, err := setupKVCacheIndexer(serviceCtx)
	if err != nil {
		logger.Error(err, "failed to setup KVCacheIndexer")
		return err
	}

	// Setup events pool (from original online example)
	eventsPool := setupEventsPool(serviceCtx, kvCacheIndexer.KVBlockIndex())
	eventsPool.Start(serviceCtx)
	logger.Info("Events pool started and listening for ZMQ messages")

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Setup unified HTTP endpoints (original + chat completions)
	setupUnifiedHTTPEndpoints(serviceCtx, kvCacheIndexer, chatTemplateWrapper)

	logger.Info("=== Unified Online KV Events Example Started ===")
	logger.Info("HTTP server running on http://localhost:8080")
	logger.Info("Available endpoints:")
	logger.Info("  - GET /health - Health check")
	logger.Info("  - POST /score - Score prompts using KV-cache (original)")
	logger.Info("  - POST /score_completions - Score chat completions with template rendering")
	logger.Info("  - POST /v1/chat/completions - Full chat completions with template processing")

	// Wait for shutdown
	<-serviceCtx.Done()
	logger.Info("Shutting down unified KV-cache service...")
	return nil
}

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

func setupChatTemplateWrapper() (*chat_completions_template.ChatTemplateCGoWrapper, error) {
	wrapper := chat_completions_template.NewChatTemplateCGoWrapper()
	if err := wrapper.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize chat template wrapper: %w", err)
	}
	return wrapper, nil
}

// Original online example functions.
func getKVCacheIndexerConfig() *kvcache.Config {
	config := kvcache.NewDefaultConfig()

	huggingFaceToken := os.Getenv(envHFToken)
	if huggingFaceToken != "" {
		config.TokenizersPoolConfig.HuggingFaceToken = huggingFaceToken
	}

	hashSeed := os.Getenv(pythonHashSeed)
	if hashSeed != "" {
		config.TokenProcessorConfig.HashSeed = hashSeed
	}

	blockSize, err := strconv.Atoi(os.Getenv(blockSizeEnvVar))
	if err == nil && blockSize >= 0 {
		config.TokenProcessorConfig.BlockSize = blockSize
	}

	config.KVBlockIndexConfig.EnableMetrics = true
	config.KVBlockIndexConfig.MetricsLoggingInterval = 15 * time.Second

	return config
}

func getEventsPoolConfig() *kvevents.Config {
	concurrency := defaultConcurrency
	if envConcurrency := os.Getenv(envPoolConcurrency); envConcurrency != "" {
		if c, err := strconv.Atoi(envConcurrency); err == nil && c > 0 {
			concurrency = c
		}
	}

	zmqEndpoint := os.Getenv(envZMQEndpoint)
	if zmqEndpoint == "" {
		zmqEndpoint = defaultZMQEndpoint
	}

	zmqTopic := os.Getenv(envZMQTopic)
	if zmqTopic == "" {
		zmqTopic = defaultZMQTopic
	}

	return &kvevents.Config{
		Concurrency: concurrency,
		ZMQEndpoint: zmqEndpoint,
		TopicFilter: zmqTopic,
	}
}

func setupKVCacheIndexer(ctx context.Context) (*kvcache.Indexer, error) {
	logger := klog.FromContext(ctx)

	kvCacheIndexer, err := kvcache.NewKVCacheIndexer(ctx, getKVCacheIndexerConfig())
	if err != nil {
		return nil, err
	}

	logger.Info("Created Indexer")

	go kvCacheIndexer.Run(ctx)
	logger.Info("Started Indexer")

	return kvCacheIndexer, nil
}

func setupEventsPool(ctx context.Context, kvBlockIndex kvblock.Index) *kvevents.Pool {
	logger := klog.FromContext(ctx)

	cfg := getEventsPoolConfig()

	logger.Info("Creating events pool", "config", cfg)
	pool := kvevents.NewPool(cfg, kvBlockIndex)

	return pool
}

func setupUnifiedHTTPEndpoints(
	ctx context.Context,
	kvCacheIndexer *kvcache.Indexer,
	chatTemplateWrapper *chat_completions_template.ChatTemplateCGoWrapper,
) {
	logger := klog.FromContext(ctx)

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			logger.Error(err, "Failed to write health response")
		}
	})

	// Original scoring endpoint (from online example)
	modelName := os.Getenv(envModelName)
	http.HandleFunc("/score", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if req.Prompt == "" {
			http.Error(w, "field 'prompt' required", http.StatusBadRequest)
			return
		}

		pods, err := kvCacheIndexer.GetPodScores(ctx, req.Prompt, modelName, nil)
		if err != nil {
			http.Error(w, fmt.Sprintf("error: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(pods); err != nil {
			logger.Error(err, "failed to encode response")
		}
	})

	// Chat completions scoring endpoint (renders template then scores)
	http.HandleFunc("/score_completions", func(w http.ResponseWriter, r *http.Request) {
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
			http.Error(w, fmt.Sprintf("Failed to get KV-cache scores: %v", err), http.StatusInternalServerError)
			return
		}

		// Return scoring response (not full completion)
		scoreResponse := struct {
			PodScores        map[string]int `json:"podScores"`
			Model            string         `json:"model"`
			RenderedTemplate string         `json:"renderedTemplate"`
			OriginalMessages []ChatMessage  `json:"originalMessages"`
		}{
			PodScores:        pods,
			Model:            req.Model,
			RenderedTemplate: renderedPrompt,
			OriginalMessages: req.Messages,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(scoreResponse); err != nil {
			logger.Error(err, "Failed to encode score response")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	})

	// Chat completions endpoint with real template processing
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

	// Start HTTP server
	httpPort := os.Getenv(envHTTPPort)
	if httpPort == "" {
		httpPort = defaultHTTPPort
	}

	go func() {
		logger.Info("HTTP endpoint /score exposed", "port", httpPort)
		logger.Info("HTTP endpoint /score_completions exposed", "port", httpPort)
		logger.Info("HTTP endpoint /v1/chat/completions exposed", "port", httpPort)
		//nolint:gosec // no timeout
		if err := http.ListenAndServe(":"+httpPort, nil); err != nil {
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
