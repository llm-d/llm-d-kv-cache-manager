# UDS Tokenizer Service

This service provides tokenization functionality via HTTP over Unix Domain Socket (UDS). It also exposes a separate HTTP endpoint for Kubernetes health checks.

## Features

- Apply chat templates to messages
- Tokenize text prompts
- Runtime configuration updates
- Health check endpoint for Kubernetes
- Support for multiple model formats (HuggingFace, ModelScope)
- Automatic model downloading and caching

## Services

The service exposes multiple endpoints:

1. `/chat-template` - Apply chat template to messages (UDS only)
2. `/tokenize` - Tokenize text (UDS only)
3. `/health` - Health check endpoint (TCP port, for Kubernetes probes)
4. `/config` - Get or update configuration (UDS only)

## Quick Start

Start the service:
```bash
python server.py
```

Or using Gunicorn for production:
```bash
./start_gunicorn.sh
```

The service will:
- Listen on `/tmp/tokenizer/tokenizer-uds.socket` for main functionality
- Listen on port 8080 (configurable via PROBE_PORT) for health checks

## Environment Variables

| Variable | Description | Default |
|---------|-------------|---------|
| `LOG_LEVEL` | Log level (DEBUG, INFO, WARNING, ERROR, CRITICAL) | INFO |
| `WORKERS` | Number of worker processes when using Gunicorn | CPU cores * 2 + 1 |
| `MODEL` | Path to the model directory | ./models/deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B |
| `ADD_SPECIAL_TOKENS` | Whether to add special tokens | true |
| `ENABLE_THINKING` | Whether to enable thinking mode | false |
| `ADD_GENERATION_PROMPT` | Whether to add generation prompt | true |
| `PROBE_PORT` | Port for health check endpoint | 8080 |
| `USE_MODELSCOPE` | Whether to download tokenizer files from ModelScope (true) or Hugging Face (false) | false |

## API Endpoints

### POST /chat-template
Apply chat template to a list of messages.

Request body:
```json
[
  {
    "role": "system",
    "content": "You are a helpful assistant."
  },
  {
    "role": "user",
    "content": "Hello!"
  }
]
```

Response:
Plain text with the formatted prompt.

### POST /tokenize
Tokenize a text prompt.

Request body:
```
Text to tokenize
```

Response:
JSON with tokenization results:
- `input_ids`: List of token IDs
- `attention_mask`: Attention mask for the tokens

### GET /health
Health check endpoint for Kubernetes probes.

Response:
```json
{
  "status": "healthy",
  "service": "tokenizer-service",
  "timestamp": 1234567890.123
}
```

### GET /config
Get current configuration.

Response:
```json
{
  "model": "./models/deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B",
  "add_special_tokens": true,
  "enable_thinking": false,
  "add_generation_prompt": true
}
```

### POST /config
Update configuration at runtime.

Request body:
```json
{
  "model": "./models/qwen/qwen3-0.6b",
  "add_special_tokens": false
}
```

Response:
```json
{
  "status": "success",
  "message": "Configuration updated successfully"
}
```

## Testing

### Unit Tests

Run unit tests with mocks (no service needed):
```bash
# Install test dependencies
pip install -r tests/requirements.txt

# Run unit tests
python -m pytest tests/test_tokenizer_unit.py -v
```

### Integration Tests

Run integration tests (requires service to be running):
```bash
# Start the service in the background
python server.py &

# Run integration tests with automatic waiting
python tests/run_integration_tests.py

# Stop the service
pkill -f "python server.py"
```

The integration test runner will automatically wait for the server to be ready before running tests.

## Kubernetes Deployment

The service is designed to run in Kubernetes with:
- A shared `emptyDir` volume for UDS communication between containers
- Health check endpoint for liveness and readiness probes
- Proper security context with non-root user

## Model Support

The service supports:
- HuggingFace models (local or remote)
- ModelScope models (automatically downloaded and cached)
- Custom models in standard format

Models are automatically downloaded and cached in the `models/` directory. 
The source for downloading can be controlled with the `USE_MODELSCOPE` environment variable:
- `false` (default): Download from Hugging Face
- `true`: Download from ModelScope

See [models/README.md](models/README.md) for detailed information about model caching, pre-populating the cache, and Kubernetes deployment strategies.

## Project Structure

```
├── server.py              # Main server entry point
├── tokenizer_service/     # Core tokenizer service implementation
│   ├── __init__.py
│   ├── tokenizer.py       # Tokenizer service implementation
│   └── exceptions.py      # Custom exceptions
├── utils/                 # Utility functions
│   ├── __init__.py
│   └── logger.py          # logger functionality
├── tests/                 # Test files
│   ├── __init__.py
│   ├── run_integration_tests.py  # Integration test runner
│   ├── test_tokenizer_unit.py    # Unit tests
│   └── test_tokenizer_service.py # Legacy integration test
├── models/                # Model files (downloaded automatically)
├── client/                # Client examples
├── requirements.txt       # Python dependencies
├── gunicorn.conf.py       # Gunicorn configuration
├── start_gunicorn.sh      # Gunicorn startup script
└── README.md              # This file
```