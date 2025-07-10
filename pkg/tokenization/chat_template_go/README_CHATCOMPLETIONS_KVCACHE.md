# HOWTO: Using `GetCompletionsPodScores` for OpenAI-API ChatCompletions Requests with kv-cache-manager

## Overview

`GetCompletionsPodScores` in `indexer.go` enables the kv-cache-manager to support OpenAI-compatible ChatCompletions requests by rendering the full message structure (including tools and documents) into a prompt using a Python Jinja2 template, before tokenization and KV block key calculation.

---

## What struct do I need to receive from the router?

You must provide a `chattemplatego.ChatTemplateRequest` with the following fields:

```go
// ChatTemplateRequest represents the request to render a chat template
 type ChatTemplateRequest struct {
     Conversations             [][]ChatMessage        `json:"conversations"`
     Tools                     []interface{}          `json:"tools,omitempty"`
     Documents                 []interface{}          `json:"documents,omitempty"`
     ChatTemplate              string                 `json:"chat_template,omitempty"`
     ReturnAssistantTokensMask bool                   `json:"return_assistant_tokens_mask,omitempty"`
     ContinueFinalMessage      bool                   `json:"continue_final_message,omitempty"`
     AddGenerationPrompt       bool                   `json:"add_generation_prompt,omitempty"`
     TemplateVars              map[string]interface{} `json:"template_vars,omitempty"`
 }
```

- **Conversations**: List of message lists (role/content pairs)
- **Tools**: (Optional) List of tool schemas
- **Documents**: (Optional) List of document dicts
- **ChatTemplate**: (Optional) Override for the chat template
- **ReturnAssistantTokensMask**: (Optional) Whether to return assistant token indices
- **ContinueFinalMessage**: (Optional) Whether to continue from the final message
- **AddGenerationPrompt**: (Optional) Whether to add a generation prompt
- **TemplateVars**: (Optional) Special tokens for template rendering

This struct mirrors the OpenAI ChatCompletions request, supporting messages, tools, documents, and advanced template options.

### ChatMessage Struct

The `ChatMessage` struct represents individual messages within conversations:

```go
// ChatMessage represents a single message in a conversation
type ChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}
```

- **Role**: The role of the message sender (e.g., "user", "assistant", "system")
- **Content**: The actual message content/text

**Example usage:**
```go
conversation := []chattemplatego.ChatMessage{
    {Role: "user", Content: "What is the weather in Paris?"},
    {Role: "assistant", Content: "Let me check that for you."},
    {Role: "user", Content: "Thank you!"},
}
```

This structure follows the OpenAI ChatCompletions API format, making it compatible with existing chat-based applications.

---

## How do the three scoring functions differ?

- **`GetPromptPodScores`**:  
  Accepts a simple prompt string, tokenizes it, and calculates KV block keys directly.

- **`GetCompletionsPodScores`**:  
  Accepts a full `ChatTemplateRequest` (with messages, tools, etc.), uses the Python Jinja2 template (via CGO) to flatten the structure into a prompt, then tokenizes and calculates KV block keys. This ensures the prompt matches what the model would actually see.

- **`GetPodScores`**:  
  A unified interface that automatically dispatches to either `GetPromptPodScores` or `GetCompletionsPodScores` based on the input type:
  - If input is a `string` → calls `GetPromptPodScores`
  - If input is a `ChatTemplateRequest` → calls `GetCompletionsPodScores`
  - This provides a single entry point for both simple prompts and complex chat completions.

---

## Detailed Flow: `GetCompletionsPodScores` Pipeline

When `indexer.go:GetCompletionsPodScores()` is called, here's the complete flow through files and functions:

```
1. indexer.go:GetCompletionsPodScores(ctx, req, modelName, podIdentifiers)
   │
   ├── 1.1. **CGO Binding**: chattemplatego.NewChatTemplateCGoWrapper()
   │   └── cgo_functions.go:NewChatTemplateCGoWrapper()
   │       └── Creates ChatTemplateCGoWrapper struct with initialized=false
   │
   ├── 1.2. **CGO Binding**: wrapper.GetModelChatTemplate(getReq)
   │   ├── cgo_functions.go:GetModelChatTemplate(req)
   │   │   ├── Initialize() Python interpreter via CGO
   │   │   ├── executePythonCode() - **CGO Binding** to Python
   │   │   └── **Python Wrapper**: chat_template_wrapper.py:get_model_chat_template()
   │   │       └── Uses Hugging Face AutoTokenizer to fetch model template
   │   └── Returns: (template, template_vars)
   │
   ├── 1.3. **CGO Binding**: wrapper.RenderChatTemplate(req)
   │   ├── cgo_functions.go:RenderChatTemplate(req)
   │   │   ├── Initialize() Python interpreter via CGO (if not already done)
   │   │   ├── executePythonCode() - **CGO Binding** to Python
   │   │   └── **Python Wrapper**: chat_template_wrapper.py:render_jinja_template()
   │   │       ├── _compile_jinja_template() - Compiles Jinja2 template
   │   │       ├── AssistantTracker class - Tracks assistant token indices
   │   │       └── Returns: (rendered_chats, generation_indices)
   │   └── Returns: ChatTemplateResponse
   │
   ├── 1.4. Extract prompt from response
   │   └── prompt := resp.RenderedChats[0]
   │
   ├── 1.5. **Tokenization**: k.tokenizersPool.AddTask(prompt, modelName)
   │   └── tokenization/pool.go:AddTask() - Queues tokenization task
   │
   ├── 1.6. **Prefix Store**: k.tokensIndexer.FindLongestContainedTokens(prompt, modelName)
   │   └── prefixstore/lru-store.go:FindLongestContainedTokens() - Finds cached tokens
   │
   ├── 1.7. **Token Processing**: k.tokensProcessor.TokensToKVBlockKeys(tokens, modelName)
   │   └── kv-cache/token-processor.go:TokensToKVBlockKeys() - Converts tokens to block keys
   │
   ├── 1.8. **KV Block Indexing**: k.kvBlockIndexer.GetPodsForKeys(ctx, blockKeys, podSet)
   │   └── kv-cache/kvblock-indexer.go:GetPodsForKeys() - Queries Redis for pod mappings
   │
   └── 1.9. **Scoring**: k.kvBlockScorer.Score(strBlockKeys, keyToPods)
       └── kv-cache/kvblock-scorer.go:Score() - Calculates pod scores
```

### Key Components in the Pipeline:

**🔗 CGO Bindings** (Go → Python):
- `cgo_functions.go` - Provides the bridge between Go and Python
- Uses Python's C API via CGO to call Python functions directly
- Manages Python interpreter lifecycle (Initialize/Finalize)

**📦 Python Wrapper** (Python → Hugging Face):
- `chat_template_wrapper.py` - Wraps Hugging Face's complex template system
- Provides clean API for template rendering and model template fetching
- Handles Jinja2 compilation, assistant tracking, and error handling

**🔄 Data Flow**:
1. **Input**: `ChatTemplateRequest` (messages, tools, documents)
2. **Template Fetching**: Model-specific chat template from Hugging Face
3. **Template Rendering**: Jinja2 template processing with tools/documents
4. **Tokenization**: Convert rendered prompt to tokens
5. **KV Cache Lookup**: Find cached token blocks and associated pods
6. **Scoring**: Calculate pod scores based on cache hits

This pipeline ensures that chat completion requests are properly templated, tokenized, and scored against the KV cache, providing accurate pod recommendations for efficient request routing.

---

## Summary

- The router should send a `ChatTemplateRequest` (not just a prompt string) to the indexer.
- `GetCompletionsPodScores` will handle template rendering and tokenization internally, ensuring correct KV block key calculation for all supported models.
- The integration uses a CGO bridge (`cgo_functions.go`) to call Python (`chat_template_wrapper.py`) for template rendering, matching vLLM and OpenAI API behavior.