# vLLM Deployment Chart

This Helm chart assists with deploying vLLMs with optionally:
- KVEvents publishing and a demo KV Cache Manager indexing deployment
- LMCache for KV-cache offloading and deprecated Redis indexing

## Prerequisites

- A Kubernetes cluster with NVIDIA GPU resources.
- Helm 3 installed.
- `kubectl` configured to connect to your cluster.

## Installation

1.  **Set Environment Variables**

    If you are using a private model from Hugging Face, you will need an access token.

    ```bash
    export HF_TOKEN="your-huggingface-token"
    export NAMESPACE="default" # Or your desired namespace
    ```

2.  **Deploy the Chart**

    Install the chart with a release name (e.g., `my-vllm`):

    ```bash
    helm upgrade --install my-vllm ./vllm-setup-helm \
      --namespace $NAMESPACE \
      --set secret.hfTokenValue=$HF_TOKEN 
    ```

    You can customize the deployment by creating your own `values.yaml` file or by using `--set` flags for other parameters.

## Configuration

The most important configuration parameters are listed below. For a full list of options, see the [`values.yaml`](./values.yaml) file.

| Parameter | Description                                                          | Default                            |
| --- |----------------------------------------------------------------------|------------------------------------|
| `vllm.model.name` | The Hugging Face model to deploy.                                    | `meta-llama/Llama-3.1-8B-Instruct` |
| `vllm.replicaCount` | Number of vLLM replicas.                                             | `1`                                |
| `vllm.resources.limits` | GPU and other resource limits for the vLLM container.                | `nvidia.com/gpu: '1'`              |
| `lmcache.enabled` | Enable LMCache for KV-cache offloading.                              | `false`                            |
| `lmcache.redis.enabled` | Deploy a Redis instance for KV-cache indexing through LMCache.       | `false`                            |
| `kvCacheManager.enabled` | Deploy the KV Cache Manager for event indexing DEMO.                 | `true`                             |
| `persistence.enabled` | Enable persistent storage for model weights to speed up restarts.    | `true`                             |
| `secret.create` | Set to `true` to automatically create the secret for `hfTokenValue`. | `true`                             |
| `secret.hfTokenValue` | The Hugging Face token. Required if `secret.create` is `true`.       | `""`                               |

## Cleanup

To uninstall the chart and clean up all associated resources, run the following command, replacing `my-vllm` with your release name:

```bash
helm uninstall my-vllm --namespace $NAMESPACE
```
