# Chat Template Integration for OpenAI-API Compatibility

## Why Templating is Needed

When processing OpenAI ChatCompletions requests, the model doesn't see the raw message structure. Instead, it sees a **flattened prompt** created by applying a model-specific chat template (Jinja2 format) that converts messages, tools, and documents into the exact text the model will tokenize.

**Example:**
```json
// Input: ChatCompletions request
{
  "messages": [
    {"role": "user", "content": "What's 2+2?"},
    {"role": "assistant", "content": "Let me calculate that."},
    {"role": "user", "content": "Thanks!"}
  ]
}
```

```jinja2
<!-- Model template (e.g., Llama-2) -->
{% for message in messages %}
{% if message['role'] == 'user' %}
{{ '<s>[INST] ' + message['content'] + ' [/INST]' }}
{% elif message['role'] == 'assistant' %}
{{ message['content'] + '</s>' }}
{% endif %}
{% endfor %}
```

```text
<!-- Flattened prompt the model actually sees -->
<s>[INST] What's 2+2? [/INST]Let me calculate that.</s><s>[INST] Thanks! [/INST]
```

**Without templating**, we'd tokenize the raw JSON structure, producing completely different tokens than what the model will actually process, leading to incorrect KV cache lookups.

## Integration with Existing Pipeline

The chat template integration adds a **pre-processing step** to the existing KV cache pipeline:

1. **Template Fetching**: Get model-specific chat template from Hugging Face
2. **Template Rendering**: Apply Jinja2 template to flatten the request structure  
3. **Continue with existing pipeline**: Tokenize → KV Block Keys → Pod Scoring

See the main documentation for the complete pipeline details.

## Usage

### Unified API

The indexer provides a unified `GetPodScores()` function that handles both regular prompts and chat completion requests:

```go
// For regular prompts (default behavior)
scores, err := indexer.GetPodScores(ctx, prompt, modelName, podIdentifiers, false)
// or use the convenience function
scores, err := indexer.GetPodScoresDefault(ctx, prompt, modelName, podIdentifiers)

// For chat completion requests
scores, err := indexer.GetPodScores(ctx, prompt, modelName, podIdentifiers, true)
```

### For ChatCompletions Requests

The router can receive a standard OpenAI ChatCompletions request and convert it to a JSON string representing our `ChatTemplateRequest`:

**ChatTemplateRequest accepts these fields:**
- `Conversations` - List of message lists (role/content pairs)
- `Tools` - (Optional) List of tool schemas  
- `Documents` - (Optional) List of document dicts
- `ChatTemplate` - (Optional) Override for the chat template
- `ReturnAssistantTokensMask` - (Optional) Whether to return assistant token indices
- `ContinueFinalMessage` - (Optional) Whether to continue from the final message
- `AddGenerationPrompt` - (Optional) Whether to add a generation prompt
- `TemplateVars` - (Optional) Special tokens for template rendering

```json
// Input: OpenAI ChatCompletions request
{
  "model": "llama-2-7b-chat",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "What's the weather in Paris?"},
    {"role": "assistant", "content": "Let me check that for you."},
    {"role": "user", "content": "Thanks!"}
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get weather information",
        "parameters": {...}
      }
    }
  ],
  "documents": [
    {
      "type": "text",
      "content": "Paris weather data..."
    }
  ]
}
```

```go
// Converted to ChatTemplateRequest and then to JSON string
req := chattemplatego.ChatTemplateRequest{
    Conversations: [][]chattemplatego.ChatMessage{
        {
            {Role: "system", Content: "You are a helpful assistant."},
            {Role: "user", Content: "What's the weather in Paris?"},
            {Role: "assistant", Content: "Let me check that for you."},
            {Role: "user", Content: "Thanks!"},
        },
    },
    Tools: []interface{}{...}, // From OpenAI request
    Documents: []interface{}{...}, // From OpenAI request
    // Other fields are optional and can be set as needed
}

// Convert to JSON string for the unified API
reqJSON, err := json.Marshal(req)
if err != nil {
    return err
}

scores, err := indexer.GetPodScores(ctx, string(reqJSON), modelName, podIdentifiers, true)
```

### Template Processing Flow

The templating process (steps 1.1-1.4) handles the conversion from structured request to flattened prompt:

```
1.1. **CGO Binding**: chattemplatego.NewChatTemplateCGoWrapper()
    └── cgo_functions.go:NewChatTemplateCGoWrapper()
        └── Creates ChatTemplateCGoWrapper struct with initialized=false

1.2. **Template Fetching**: wrapper.GetModelChatTemplate(getReq)
    ├── cgo_functions.go:GetModelChatTemplate(req)
    │   ├── Initialize() Python interpreter via CGO
    │   ├── executePythonCode() - **CGO Binding** to Python
    │   └── **Python Wrapper**: chat_template_wrapper.py:get_model_chat_template()
    │       └── Uses Hugging Face AutoTokenizer to fetch model template
    └── Returns: (template, template_vars)

1.3. **Template Rendering**: wrapper.RenderChatTemplate(req)
    ├── cgo_functions.go:RenderChatTemplate(req)
    │   ├── Initialize() Python interpreter via CGO (if not already done)
    │   ├── executePythonCode() - **CGO Binding** to Python
    │   └── **Python Wrapper**: chat_template_wrapper.py:render_jinja_template()
    │       └── Imports render_jinja_template from transformers.utils.chat_template_utils
    │           └── Uses transformers library's core template rendering functionality
    └── Returns: ChatTemplateResponse

1.4. **Extract Flattened Prompt**
    └── prompt := resp.RenderedChats[0]
    └── Continue with existing pipeline: Tokenize → KV Block Keys → Pod Scoring
```

### API Functions

- **`GetPodScores(ctx, prompt, modelName, podIdentifiers, chatCompletion)`** - Unified function that handles both regular prompts and chat completions
- **`GetPodScoresDefault(ctx, prompt, modelName, podIdentifiers)`** - Convenience function for regular prompts (equivalent to `GetPodScores` with `chatCompletion=false`)

The integration ensures tokenization matches exactly what the model will process, enabling accurate KV cache lookups for chat completion requests.