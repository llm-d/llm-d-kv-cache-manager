# KV-Events Examples

## Offline

The offline example demonstrates how the KV-Cache Manager handles KV-Events through a dummy ZMQ publisher.

### Prerequisites

Set the following environment variables:
```
export HF_TOKEN="your-huggingface-token"
```

Download tokenizer bindings:
```bash
make download-tokenizer
```

### Running the Example
```
go run -ldflags="-extldflags '-L$(pwd)/lib'" examples/kv_events/offline/main.go
```

The example will start the KV-Cache Manager (indexer) and a dummy publisher that simulates KV-Events. 
The demo will progress through:
1. Initializing the KV-Cache Manager and the dummy publisher
2. Querying the KV-Cache Manager for Pod scores (initially empty)
3. Simulating KV-Events by the publisher for a dummy prompt
4. Querying the KV-Cache Manager for pod scores again (expecting updated scores)

## Online

The online example demonstrates how to deploy the KV-Cache Manager with real-time KV-Events processing and HTTP endpoints for scoring prompts and chat completions.

### Prerequisites

Set Environment Variables:
```bash
export HF_TOKEN=<token>
export NAMESPACE=<namespace>
export MODEL="Qwen/Qwen3-8B"
```

Deploy the helm chart which includes all the necessary components by default:
```bash
helm upgrade --install demo ./vllm-setup-helm \
  --namespace $NAMESPACE \
  --set secret.hfTokenValue=$HF_TOKEN \
  --set kvCacheManager.enabled=true \
  --set vllm.model.name="$MODEL" \
  --set vllm.model.label="qwen3-8b" \
  --set vllm.replicaCount=1
```

Refer to the [vLLM Helm Chart README](../vllm-setup-helm/README.md) for more details on the chart and its parameters.

### Running the Example
Assuming the helm chart is deployed, the resulting `demo-kv-cache-manager` service is port-forwarded to `localhost:8080`, 
and the vLLM service is port-forwarded to `localhost:8000`, e.g.,:

```bash
kubectl port-forward svc/demo-kv-cache-manager 8080:8080 -n $NAMESPACE
kubectl port-forward svc/demo-vllm-qwen3-8b 8000:8000 -n $NAMESPACE
```

Then, set the long prompt text:

```bash
export TEXT="lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Curabitur pretium tincidunt lacus. Nulla gravida orci a odio. Nullam varius, turpis et commodo pharetra, est eros bibendum elit, nec luctus magna felis sollicitudin mauris. Integer in mauris eu nibh euismod gravida. Duis ac tellus et risus vulputate vehicula. Donec lobortis risus a elit. Etiam tempor. Ut ullamcorper, ligula eu tempor congue, eros est euismod turpis, id tincidunt sapien risus a quam. Maecenas fermentum consequat mi. Donec fermentum. Pellentesque malesuada nulla a mi. Duis sapien sem, aliquet nec, commodo eget, consequat quis, neque. Aliquam faucibus, elit ut dictum aliquet, felis nisl adipiscing sapien, sed malesuada diam lacus eget erat. Cras mollis scelerisque nunc. Nullam arcu. Aliquam consequat. Curabitur augue lorem, dapibus quis, laoreet et, pretium ac, nisi. Aenean magna nisl, mollis quis, molestie eu, feugiat in, orci. In hac habitasse platea dictumst. sunt in culpa qui officia deserunt mollit anim id est laborum. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Curabitur pretium tincidunt lacus. Nulla gravida orci a odio. Nullam varius, turpis et commodo pharetra, est eros bibendum elit, nec luctus magna felis sollicitudin mauris. Integer in mauris eu nibh euismod gravida. Duis ac tellus et risus vulputate vehicula. Donec lobortis risus a elit. Etiam tempor. Ut ullamcorper, ligula eu tempor congue, eros est euismod turpis, id tincidunt sapien risus a quam. Maecenas fermentum consequat mi. Donec fermentum. Pellentesque malesuada nulla a mi. Duis sapien sem, aliquet nec, commodo eget, consequat quis, neque. Aliquam faucibus, elit ut dictum aliquet, felis nisl adipiscing sapien, sed malesuada diam lacus eget erat. Cras mollis scelerisque nunc. Nullam arcu. Aliquam consequat. Curabitur augue lorem, dapibus quis, laoreet et, pretium ac, nisi. Aenean magna nisl, mollis quis, molestie eu, feugiat in, orci. In hac habitasse platea dictumst."
```

#### /v1/completions

1. Send a long prompt to the manager (expect no pod scores):
```bash
curl -X POST "http://localhost:8080/score_completions" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"'"${TEXT}"'", "model":"'"${MODEL}"'"}' | jq
```

1. Send an inference request to the vLLM endpoint (`v1/completions`):
```bash
curl -X POST "http://localhost:8000/v1/completions" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"'"${TEXT}"'","max_tokens":50,"temperature":0.7}' | jq
```

1. Query the manager again with the same prompt:
```bash
curl -X POST "http://localhost:8080/score_completions" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"'"${TEXT}"'", "model":"'"${MODEL}"'"}' | jq
```

#### /v1/chat_completions

1. Send a long prompt to the manager (expect no pod scores):
```bash
curl -X POST "http://localhost:8080/score_chat_completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"'"${MODEL}"'",
    "messages": [
      {"role": "user", "content": "'"${TEXT}"'"}
    ]
  }' | jq
```

1. Send an inference request to the vLLM endpoint (`v1/chat_completions`):
```bash
curl -X POST "http://localhost:8000/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"'"${MODEL}"'",
    "messages": [
      {"role": "user", "content": "'"${TEXT}"'"}
    ]
  }' | jq
```

1. Query the manager again with the same prompt:
```bash
curl -X POST "http://localhost:8080/score_chat_completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"'"${MODEL}"'",
    "messages": [
      {"role": "user", "content": "'"${TEXT}"'"}
    ]
  }' | jq
```

These endpoints allow for scoring rendered chat templates and generating full chat completions using the KV-Cache Manager.
