# Helm Chart

This helm chart was generated by Gemini 2.5 Pro, given a set of concrete CRs.

## Running

### Prerequisites
Environment variables:
```
export HF_TOKEN=...
export NAMESPACE=...
```

### Deploying
Deploying (repo root as working directory):
```
helm upgrade --install vllm-p2p ./vllm-setup-helm \
  --namespace $NAMESPACE \
  --create-namespace \
  --set secret.create=true \
  --set secret.hfTokenValue=$HF_TOKEN \
  --set vllm.poolLabelValue="vllm-llama3-8b-instruct" 
```

See [values](./values.yaml) for all configurable parameters.

### LMCache Configuration

- **Disabling LMCache**
To disable LMCache completely, remove `--kv-transfer-config '{"kv_connector":"LMCacheConnectorV1","kv_role":"kv_both"}'` from the vLLM deployment’s spec.containers[0].args

- **Disabling Indexing (keeps only offloading enabled)**
Remove the `LMCACHE_ENABLE_P2P` and the `LMCACHE_LOOKUP_URL` env vars

TODO: make configurable

### Cleanup
Uninstalling:
```
helm uninstall vllm-p2p \
  --namespace $NAMESPACE
```

