# KVCache Manager Setup Guide

This guide provides instructions for deploying the llm-d-kv-cache-manager with vLLM, LMCache, and Redis using Helm.

## Deployment with Helm

The llm-d-kv-cache-manager repository includes a Helm chart for deploying vLLM with LMCache and Redis. This section describes how to use this Helm chart for a complete deployment.

### Prerequisites

- Kubernetes cluster with GPU support
- Helm 3.x
- HuggingFace token for accessing models
- kubectl configured to access your cluster

### Installation

1. Set environment variables:

```bash
export HF_TOKEN=<your-huggingface-token>
export NAMESPACE=<your-namespace>
```

2. Deploy using Helm:

```bash
helm upgrade --install vllm-p2p ./vllm-setup-helm \
  --namespace $NAMESPACE \
  --create-namespace \
  --set secret.create=true \
  --set secret.hfTokenValue=$HF_TOKEN \
  --set vllm.poolLabelValue="vllm-model-pool" \
  --set vllm.model.name="meta-llama/Llama-3.1-8B-Instruct" \
  -f ./vllm-setup-helm/values.yaml
```

**Note:**

- Adjust the resource and limit allocations for vLLM and Redis in `values.yaml` to match your cluster's capacity.
- By default, the chart uses a `PersistentVolume` to cache the model. To disable this, set `.persistence.enabled` to `false`.

3. Verify the deployment:

```bash
kubectl get deployments -n $NAMESPACE
```

You should see:

- vLLM pods (default: 4 replicas)
- Redis lookup server pod

### Configuration Options

The Helm chart supports various configuration options. See [values.yaml](../../vllm-setup-helm/values.yaml) for all available options.

Key configuration parameters:

- `vllm.model.name`: The HuggingFace model to use (default: `meta-llama/Llama-3.1-8B-Instruct`)
- `vllm.replicaCount`: Number of vLLM replicas (default: 4)
- `vllm.poolLabelValue`: Label value for the inference pool (used by scheduler)
- `redis.enabled`: Whether to deploy Redis for KV cache indexing (default: true)
- `persistence.enabled`: Enable persistent storage for model cache (default: true)
- `secret.create`: Create HuggingFace token secret (default: true)

## Deploying the Inference Scheduler

The llm-d-inference-scheduler can be deployed alongside the vLLM and KVCache Manager.

### Prerequisites

- vLLM with LMCache and Redis deployed using the Helm chart above
- llm-d-inference-scheduler repository

<!-- TODO -->
4. Configure the Gateway API resources to use the endpoint-picker. Refer to the [llm-d-inference-scheduler documentation](https://github.com/llm-d/llm-d-inference-scheduler) for detailed instructions.

## Manual Configuration (Alternative to Helm)

If you prefer manual configuration or need to understand the underlying components, here's the configuration tested against `lmcache/vllm-openai:2025-03-10` image.

### vLLM Engine Configuration

vLLM engine args:

```yaml
- vllm
- serve
- meta-llama/Llama-3.1-8B-Instruct
- "--host"
- 0.0.0.0
- "--port"
- "8000"
- "--enable-chunked-prefill"
- "false"
- "--max-model-len"
- "16384"
- "--kv-transfer-config"
- '{"kv_connector":"LMCacheConnector","kv_role":"kv_both"}'
```

### LMCache Configuration

LMCache environment variables:

```yaml
- name: LMCACHE_USE_EXPERIMENTAL
  value: "True"
- name: VLLM_RPC_TIMEOUT
  value: "1000000"
- name: LMCACHE_LOCAL_CPU
  value: "True"
- name: LMCACHE_ENABLE_DEBUG
  value: "True"
- name: LMCACHE_MAX_LOCAL_CPU_SIZE
  value: "20"
- name: LMCACHE_ENABLE_P2P
  value: "True"
- name: LMCACHE_LOOKUP_URL
  value: "vllm-p2p-lookup-server-service.routing-workstream.svc.cluster.local:8100"
- name: LMCACHE_DISTRIBUTED_URL
  value: "vllm-p2p-engine-service:8200"
```

### Pod Command Override

For baseline deployment with Production-Stack P2P, update deployments with:

```yaml
command:
  - /bin/sh
  - -c
args:
  - |
    export LMCACHE_DISTRIBUTED_URL=${POD_IP}:80 && \
    vllm serve meta-llama/Llama-3.1-8B-Instruct \
      --host 0.0.0.0 \
      --port 8000 \
      --enable-chunked-prefill false \
      --max-model-len 16384 \
      --kv-transfer-config '{"kv_connector":"LMCacheConnector","kv_role":"kv_both"}'
```

## Using the KV Cache Indexer Example

### Prerequisites

Ensure you have a running deployment with vLLM and Redis as described above.

### Running the Example

The vLLM node can be tested with the prompt found in `examples/kv-cache-index/main.go`.

```shell
export HF_TOKEN=<token>
export REDIS_HOST=<redis-host>  # optional, defaults to localhost:6379
export REDIS_PASSWORD=<redis-password>  # optional
export MODEL_NAME=<model_name_used_in_vllm_deployment> # optional, defaults to meta-llama/Llama-3.1-8B-Instruct

go run -ldflags="-extldflags '-L$(pwd)/lib'" examples/kv-cache-index/main.go
```

Environment variables:

- `HF_TOKEN` (required): HuggingFace access token
- `REDIS_HOST` (optional): Redis address; defaults to localhost:6379
- `REDIS_PASSWORD` (optional): Redis password
- `MODEL_NAME` (optional): The model name used in vLLM deployment; defaults to meta-llama/Llama-3.1-8B-Instruct

### Model Name Alignment

> **Important**: Ensure that the model name used in your application (e.g., in the kv-cache-index example) matches the model name configured in the Helm chart (`vllm.model.name`). This is crucial for the KVCache indexer to correctly identify and score pods based on KV cache locality.

The current example uses `meta-llama/Llama-3.1-8B-Instruct` which matches the default in the Helm chart. 
If you change the model in the Helm chart, configure MODEL_NAME environment variable accordingly to instruct the example code to use the correct model.
