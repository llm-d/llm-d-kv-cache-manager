# KVCache Manager

The configuration tested against `lmcache/vllm-openai:2025-03-10` image, with vLLM/LMCache configured as follows:

vLLM engine args:

```yaml
  - vllm
  - serve
  - mistralai/Mistral-7B-Instruct-v0.2
  - '--host'
  - 0.0.0.0
  - '--port'
  - '8000'
  - '--enable-chunked-prefill'
  - 'false'
  - '--max-model-len'
  - '16384'
  - '--kv-transfer-config'
  - '{"kv_connector":"LMCacheConnector","kv_role":"kv_both"}'
```

LMCache config:

```yaml
 - name: LMCACHE_USE_EXPERIMENTAL
   value: 'True'
 - name: VLLM_RPC_TIMEOUT
   value: '1000000'
 - name: LMCACHE_LOCAL_CPU
   value: 'True'
 - name: LMCACHE_ENABLE_DEBUG
   value: 'True'
 - name: LMCACHE_MAX_LOCAL_CPU_SIZE
   value: '20'
 - name: LMCACHE_ENABLE_P2P
   value: 'True'
 - name: LMCACHE_LOOKUP_URL
   value: 'vllm-p2p-lookup-server-service.routing-workstream.svc.cluster.local:8100'
 - name: LMCACHE_DISTRIBUTED_URL
   value: 'vllm-p2p-engine-service:8200'
```

The vLLM node was fed the prompt found in `examples/kv-cache-index/main.go`.

## Usage

```shell
export HF_TOKEN=<token>
export REDIS_HOST=<redis-host>
export REDIS_PASSWORD=<redis-password>

go run -ldflags="-extldflags '-L$(pwd)/lib'" examples/kv-cache-index/main.go
```

- `HF_TOKEN` (required): HuggingFace access token
- `REDIS_HOST` (optional): Redis address; by default, it is set to localhost:6379
- `REDIS_PASSWORD` (optional): Redis password

## Note

Baseline deployment is Production-Stack P2P, then update deployments:

```yaml
command:
  - /bin/sh
  - -c
args:
  - |
    export LMCACHE_DISTRIBUTED_URL=${POD_IP}:80 && \
    vllm serve mistralai/Mistral-7B-Instruct-v0.2 \
      --host 0.0.0.0 \
      --port 8000 \
      --enable-chunked-prefill false \
      --max-model-len 16384 \
      --kv-transfer-config '{"kv_connector":"LMCacheConnector","kv_role":"kv_both"}'
```
