# llmd-fs-backend README

## Overview
The llmd-fs-backend extends the native [vLLM Offloading Connector](#offloading-connector-docs) to support a file system backend.
This backend provides a shared-storage offloading layer for vLLM. It moves KV-cache blocks between GPU and shared storage efficiently using:

- Async CUDA copies or GPU kernels
- Pinned memory pools
- Multi-threaded I/O workers
- NUMA-aware CPU affinity
- Atomic file writes and zero-copy reads

The fs connector (llmd_fs_backend) is used for shared storage but it can also work with local disk.

For architectural clarity, the fs connector is not responsible for cleanup. Storage systems should manage this.
For simple setups, see the **Storage Cleanup** section.

<img src="./docs/images/fs_connector.png" width="400" />

## System Requirements
- vLLM version 0.11.0 or above, which includes the Offloading Connector

## Installation

```bash
apt-get update && apt-get install -y libnuma-dev
pip install git+https://github.com/llm-d-kv-cache-manager.git#subdirectory=kv_connectors/llmd_fs_backend
```

This installs:
- Python module `llmd_fs_backend`
- CUDA extension `storage_offload.so`

## Configuration Flags

### Connector parameters

- `shared_storage_path`: filesystem path for store and load the KV files.
- `block_size`: number of GPU blocks grouped into each file (must be in granulaity of GPU block size that)
- `threads_per_gpu`: number of I/O threads per GPU
- `max_pinned_memory_gb`: total pinned memory limit

### Environment variables
- `STORAGE_CONNECTOR_DEBUG`: enable debug logs
- `USE_KERNEL_COPY_WRITE`: enable GPU-kernel writes (default 0)
- `USE_KERNEL_COPY_READ`: enable GPU-kernel reads (default 1)

## Example vLLM YAML

To load the fs connector:

```yaml
--kv-transfer-config '{
  "kv_connector": "OffloadingConnector",
  "kv_role": "kv_both",
  "kv_connector_extra_config": {
    "spec_name": "SharedStorageOffloadingSpec",
    "spec_module_path": "llmd_fs_backend.spec",
    "shared_storage_path": "/mnt/files-storage/kv-cache/",
    "block_size": 256,
    "threads_per_gpu": "64"
  }
}'
--distributed_executor_backend "mp"
```

A full deployment example can be found in the [`docs`](./docs/deployment) folder.

It is recommended to use multiprocess mode by setting:
`--distributed_executor_backend "mp"`

To configure environment variables:

```yaml
env:
- name: STORAGE_CONNECTOR_DEBUG
  value: 1
```

## Storage Cleanup
TBD

## Troubleshooting

### Missing `numa.h`
Install the required package:

```bash
apt-get install -y libnuma-dev
```

---

## Link Aliases

- **Offloading Connector Docs**
  <a name="offloading-connector-docs"></a>
  https://docs.vllm.ai/en/stable/features/disagg_prefill/#usage-example:~:text=backends%22%3A%5B%22UCX%22%2C%20%22GDS%22%5D%7D%7D%27-,OffloadingConnector,-%3A%20enable%20offloading%20of
