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

//nolint:testpackage // allow tests to run in the same package
package e2e

import (
	"strings"
	"time"
)

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatTemplateRequest represents the request to render a chat template.
type ChatTemplateRequest struct {
	Conversations [][]ChatMessage        `json:"conversations"`
	ChatTemplate  string                 `json:"chatTemplate"`
	TemplateVars  map[string]interface{} `json:"templateVars,omitempty"`
}

// ChatTemplateResponse represents the response from the Python function.
type ChatTemplateResponse struct {
	RenderedChats     []string  `json:"renderedChats"`
	GenerationIndices [][][]int `json:"generationIndices"`
}

// GetChatTemplateRequest represents the request to get a model's chat template.
type GetChatTemplateRequest struct {
	ModelName string `json:"modelName"`
	Revision  string `json:"revision,omitempty"`
	Token     string `json:"token,omitempty"`
}

// MockChatTemplateWrapper provides a mock implementation for testing.
type MockChatTemplateWrapper struct{}

func NewMockChatTemplateWrapper() *MockChatTemplateWrapper {
	return &MockChatTemplateWrapper{}
}

//nolint:nonamedreturns // Mock implementation uses named returns for clarity and consistency with interface.
func (w *MockChatTemplateWrapper) GetModelChatTemplate(
	req GetChatTemplateRequest,
) (template string, templateVars map[string]interface{}, err error) {
	// Mock implementation that returns a simple template.
	template = `{% for message in messages %}{{ message.role }}: {{ message.content }}
{% endfor %}`
	templateVars = map[string]interface{}{
		"bos_token": "<s>",
		"eos_token": "</s>",
	}
	return template, templateVars, nil
}

func (w *MockChatTemplateWrapper) RenderChatTemplate(req ChatTemplateRequest) (*ChatTemplateResponse, error) {
	// Mock implementation that renders the template.
	renderedChats := make([]string, 0, len(req.Conversations))
	for _, conversation := range req.Conversations {
		rendered := ""
		for _, message := range conversation {
			rendered += message.Role + ": " + message.Content + "\n"
		}
		renderedChats = append(renderedChats, rendered)
	}

	return &ChatTemplateResponse{
		RenderedChats:     renderedChats,
		GenerationIndices: [][][]int{},
	}, nil
}

// TestBasicE2E verifies that the indexer initially returns no scores for the first prompt and
// correct scores for the second request.
func (s *KVCacheSuite) TestBasicE2E() {
	prompt := "What is the capital of France?"
	blockKeys := s.promptToKeys(prompt, defaultModelName)

	fakePodList := []string{s.Pod1IP}

	s.addEntriesToIndex(blockKeys, fakePodList)
	pods, err := s.indexer.GetPodScores(s.ctx, prompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Received pod scores: %+v", pods)
	s.Empty(pods, "expected no pod scores")

	time.Sleep(5 * time.Second)

	pods, err = s.indexer.GetPodScores(s.ctx, prompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.Len(pods, 1, "expected one pod score")
	s.T().Logf("Received pod scores: %+v", pods)

	s.Equal(1, pods[s.Pod1IP], "expected pod score to equal 1")
}

// TestPrefixReduction tests scoring behavior when querying progressively shorter prefixes of a fully cached prompt.
func (s *KVCacheSuite) TestPrefixReduction() {
	//nolint:lll // long prompt
	fullPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat."
	//nolint:lll // long prompt
	midPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."
	shortPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit."

	blockKeys := s.promptToKeys(fullPrompt, defaultModelName)
	fakePodList := []string{s.Pod1IP}

	s.addEntriesToIndex(blockKeys, fakePodList)

	// Test 1: Full prompt (no match expected)
	pods, err := s.indexer.GetPodScores(s.ctx, fullPrompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Received pod scores: %+v", pods)
	s.Empty(pods, "expected no pod scores")

	time.Sleep(5 * time.Second)

	// Test 2: mid-length prompt(should return a match)
	pods, err = s.indexer.GetPodScores(s.ctx, midPrompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)

	s.T().Logf("Received pod scores: %+v", pods)
	s.Equal(pods[s.Pod1IP], 10, "expected pod score to equal 10")

	// Test 3: short prompt(should return a match)
	pods, err = s.indexer.GetPodScores(s.ctx, shortPrompt, defaultModelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.Len(pods, 1, "expected one pod score")
	s.T().Logf("Received pod scores: %+v", pods)
	s.Equal(pods[s.Pod1IP], 5, "expected pod score to equal 5")
}

// TestPrefixExpansion tests that prompts longer than the cached prefix still return partial match scores.
func (s *KVCacheSuite) TestPrefixExpansion() {
	//nolint:lll // long prompt
	fullPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat."
	//nolint:lll // long prompt
	midPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."
	shortPrompt := "lorem ipsum dolor sit amet, consectetur adipiscing elit."
	modelName := defaultModelName
	// Insert only short prompt
	blockKeys := s.promptToKeys(shortPrompt, modelName)
	fakePodList := []string{s.Pod1IP}
	s.addEntriesToIndex(blockKeys, fakePodList)

	// Test 1: short prompt
	pods, err := s.indexer.GetPodScores(s.ctx, shortPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Received pod scores: %+v", pods)
	s.Empty(pods, "expected no pod scores")

	time.Sleep(5 * time.Second)

	// Test 2: mid prompt
	pods, err = s.indexer.GetPodScores(s.ctx, midPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)

	s.T().Logf("Received pod scores: %+v", pods)
	s.Equal(5, pods[s.Pod1IP], "expected pod score to equal 5")

	blockKeys = s.promptToKeys(midPrompt, modelName)
	s.addEntriesToIndex(blockKeys, fakePodList) // update redis
	time.Sleep(5 * time.Second)

	// Test 3: full prompt
	pods, err = s.indexer.GetPodScores(s.ctx, fullPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)

	s.T().Logf("Received pod scores: %+v", pods)
	s.Equal(10, pods[s.Pod1IP], "expected pod score to equal 10")
}

func (s *KVCacheSuite) TestLongPrefixExpansion() {
	base := "The quick brown fox jumps over the lazy dog"
	modelName := defaultModelName
	s.T().Logf("s.config.PrefixStoreConfig: %+v, TokenProcessorConfig: %+v",
		s.config.PrefixStoreConfig.LRUStoreConfig, s.config.TokenProcessorConfig)
	// Generate long prompts
	shortPrompt := strings.Repeat(base, 2)
	midPrompt := strings.Repeat(base, 100)  // ~900 tokens
	longPrompt := strings.Repeat(base, 500) // ~4500 tokens

	// Insert only short prompt into Redis
	blockKeys := s.promptToKeys(shortPrompt, modelName)
	fakePodList := []string{s.Pod1IP}
	s.addEntriesToIndex(blockKeys, fakePodList)

	// Test 1: short prompt (should return no pod scores yet)
	pods, err := s.indexer.GetPodScores(s.ctx, shortPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Short prompt scores: %+v", pods)
	s.Empty(pods, "expected no pod scores")
	time.Sleep(5 * time.Second)

	// Test 2: mid prompt (should return partial match if indexer picks it up)
	pods, err = s.indexer.GetPodScores(s.ctx, midPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Mid prompt scores: %+v", pods)
	s.True(len(pods) > 0, "expected at least one pod score for mid prompt")
	blockKeys = s.promptToKeys(midPrompt, modelName)
	s.addEntriesToIndex(blockKeys, fakePodList)
	time.Sleep(5 * time.Second)

	// Test 3: long prompt (should return higher score)
	pods, err = s.indexer.GetPodScores(s.ctx, longPrompt, modelName, []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Long prompt scores: %+v", pods)
	s.True(len(pods) > 0, "expected at least one pod score for long prompt")
}

// TestChatCompletionsE2E tests the complete chat completions workflow with KV-cache integration.
func (s *KVCacheSuite) TestChatCompletionsE2E() {
	// Create a mock wrapper for testing.
	wrapper := NewMockChatTemplateWrapper()

	// Create a chat completion conversation.
	conversation := [][]ChatMessage{
		{
			{Role: "system", Content: "You are a helpful AI assistant."},
			{Role: "user", Content: "What is the capital of France?"},
			{Role: "assistant", Content: "The capital of France is Paris."},
		},
	}

	// Step 1: Get the model's chat template.
	templateRequest := GetChatTemplateRequest{
		ModelName: "ibm-granite/granite-3.3-8b-instruct",
	}
	template, templateVars, err := wrapper.GetModelChatTemplate(templateRequest)
	s.Require().NoError(err, "Failed to get model chat template")
	s.Require().NotEmpty(template, "ChatTemplate should not be empty")

	// Step 2: Render the conversation using the template.
	renderRequest := ChatTemplateRequest{
		Conversations: conversation,
		ChatTemplate:  template,
		TemplateVars:  templateVars,
	}
	response, err := wrapper.RenderChatTemplate(renderRequest)
	s.Require().NoError(err, "Failed to render chat template")
	s.Require().NotNil(response, "Response should not be nil")
	s.Require().NotEmpty(response.RenderedChats, "Rendered chats should not be empty")

	// Step 3: Extract the flattened prompt from the rendered template.
	flattenedPrompt := response.RenderedChats[0]
	s.Require().NotEmpty(flattenedPrompt, "Flattened prompt should not be empty")

	// Step 4: Use the flattened prompt for KV-cache lookup (similar to TestBasicE2E).
	blockKeys := s.promptToKeys(flattenedPrompt, "ibm-granite/granite-3.3-8b-instruct")
	fakePodList := []string{s.Pod1IP}

	// Add entries to the index.
	s.addEntriesToIndex(blockKeys, fakePodList)

	// First lookup - should return no scores initially.
	pods, err := s.indexer.GetPodScores(s.ctx, flattenedPrompt, "ibm-granite/granite-3.3-8b-instruct", []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("First lookup - Received pod scores: %+v", pods)
	s.Empty(pods, "expected no pod scores on first lookup")

	// Wait for the cache to be populated.
	time.Sleep(5 * time.Second)

	// Second lookup - should return scores.
	pods, err = s.indexer.GetPodScores(s.ctx, flattenedPrompt, "ibm-granite/granite-3.3-8b-instruct", []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Second lookup - Received pod scores: %+v", pods)
	s.Len(pods, 1, "expected one pod score")
	s.True(pods[s.Pod1IP] > 0, "expected positive pod score")

	s.T().Logf("Chat completions E2E test completed successfully")
}

// TestLongChatCompletionsE2E tests long chat completions with complex conversations.
func (s *KVCacheSuite) TestLongChatCompletionsE2E() {
	// Create a mock wrapper for testing.
	wrapper := NewMockChatTemplateWrapper()

	// Create a long, complex conversation.
	longConversation := [][]ChatMessage{
		{
			{Role: "system", Content: "You are an expert software engineer with deep knowledge of Go, Python, " +
				"and system design. Provide detailed, accurate responses."},
			{Role: "user", Content: "I'm building a high-performance caching system in Go. Can you help me " +
				"design the architecture?"},
			{Role: "assistant", Content: "Absolutely! For a high-performance caching system in Go, I'd recommend " +
				"starting with a layered architecture. Let's break this down into components."},
			{Role: "user", Content: "What about memory management and eviction policies?"},
			{Role: "assistant", Content: "Great question! Memory management is crucial. I'd suggest implementing " +
				"an LRU (Least Recently Used) eviction policy with configurable memory limits. " +
				"You can use a combination of a hash map for O(1) lookups and a doubly-linked list " +
				"for tracking access order."},
			{Role: "user", Content: "How should I handle concurrent access and thread safety?"},
			{Role: "assistant", Content: "For thread safety, you have several options. The most common approach is " +
				"to use sync.RWMutex for read-write locks, allowing multiple concurrent readers " +
				"but exclusive writers. Alternatively, you could use sync.Map for simpler cases " +
				"or implement a lock-free design with atomic operations for maximum performance."},
		},
	}

	// Step 1: Get the model's chat template.
	templateRequest := GetChatTemplateRequest{
		ModelName: "ibm-granite/granite-3.3-8b-instruct",
	}
	template, templateVars, err := wrapper.GetModelChatTemplate(templateRequest)
	s.Require().NoError(err, "Failed to get model chat template")
	s.Require().NotEmpty(template, "ChatTemplate should not be empty")

	// Step 2: Render the long conversation.
	renderRequest := ChatTemplateRequest{
		Conversations: longConversation,
		ChatTemplate:  template,
		TemplateVars:  templateVars,
	}
	response, err := wrapper.RenderChatTemplate(renderRequest)
	s.Require().NoError(err, "Failed to render long conversation")
	s.Require().NotNil(response, "Response should not be nil")
	s.Require().NotEmpty(response.RenderedChats, "Rendered chats should not be empty")

	// Step 3: Extract the flattened prompt.
	flattenedPrompt := response.RenderedChats[0]
	s.Require().NotEmpty(flattenedPrompt, "Flattened prompt should not be empty")
	s.Require().Greater(len(flattenedPrompt), 1000, "Long conversation should produce substantial output")

	// Step 4: Test KV-cache with the long flattened prompt.
	blockKeys := s.promptToKeys(flattenedPrompt, "ibm-granite/granite-3.3-8b-instruct")
	fakePodList := []string{s.Pod1IP}

	// Add entries to the index.
	s.addEntriesToIndex(blockKeys, fakePodList)

	// First lookup.
	pods, err := s.indexer.GetPodScores(s.ctx, flattenedPrompt, "ibm-granite/granite-3.3-8b-instruct", []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("First lookup - Received pod scores: %+v", pods)
	s.Empty(pods, "expected no pod scores on first lookup")

	// Wait for cache population.
	time.Sleep(5 * time.Second)

	// Second lookup.
	pods, err = s.indexer.GetPodScores(s.ctx, flattenedPrompt, "ibm-granite/granite-3.3-8b-instruct", []string{s.Pod1IP})
	s.Require().NoError(err)
	s.T().Logf("Second lookup - Received pod scores: %+v", pods)
	s.Len(pods, 1, "expected one pod score")
	s.True(pods[s.Pod1IP] > 0, "expected positive pod score")

	s.T().Logf("Long chat completions E2E test completed successfully")
}
