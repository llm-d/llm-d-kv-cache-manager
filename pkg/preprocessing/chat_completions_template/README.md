# Chat Template Integration for OpenAI-API v1/chat_completions Compatibility

## Why Templating is Needed

When processing OpenAI ChatCompletions requests, vLLM templates the input before tokenization. 
For KV-cache lookups to work correctly, we must replicate this templating process in our indexer.

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

**Without templating**, we'd not be able to recreate the same tokens vLLM will produce, leading to incorrect KV-cache lookups.

## Integration with Existing Pipeline

This package provides a library to be used for templating before using the `kvcache.Indexer` entry point.

### Requirements

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

### Template Processing Flow

The templating process (steps 1.1-1.4) handles the conversion from structured request to flattened prompt:

```
1.1. **CGO Binding**: chattemplatego.NewChatTemplateCGoWrapper()
    └── cgo_functions.go:NewChatTemplateCGoWrapper()
        └── Creates ChatTemplateCGoWrapper struct with initialized=false

1.2. **Template Fetching**: wrapper.GetModelChatTemplate(ctx, getReq)
    ├── cgo_functions.go:GetModelChatTemplate(ctx, req)
    │   ├── Initialize() Python interpreter via CGO
    │   ├── executePythonCode() - **CGO Binding** to Python
    │   └── **Python Wrapper**: chat_template_wrapper.py:get_model_chat_template()
    │       └── Uses Hugging Face AutoTokenizer to fetch model template
    └── Returns: (template, template_vars)

1.3. **Template Rendering**: wrapper.RenderChatTemplate(ctx, req)
    ├── cgo_functions.go:RenderChatTemplate(ctx, req)
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
