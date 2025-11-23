# Copyright 2025 The llm-d Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import math
import os
import torch
from pathlib import Path
from typing import Optional, Dict
from vllm.attention.backends.abstract import AttentionBackend
from vllm.logger import init_logger
from vllm.v1.kv_offload.worker.worker import OffloadingHandler, TransferSpec, TransferResult

import storage_offload

logger = init_logger(__name__)

# ----------------------------------------------------------------------
# Base Storage Offloading Handler
# ----------------------------------------------------------------------
DEFAULT_MAX_STAGING_MEMORY_GB = 150
DEFAULT_MAX_THREADS_PER_GPU = 64

class StorageOffloadingHandler(OffloadingHandler):
    """Base handler with common helpers for Storage offloading."""

    def __init__(
        self,
        model_name: str,
        tp_size: int,
        tp_rank: int,
        dtype: torch.dtype,
        gpu_blocks_per_file: int,
        threads_per_gpu: int ,
        attn_backends: dict[str, type[AttentionBackend]] ,
        max_staging_memory_gb: int = DEFAULT_MAX_STAGING_MEMORY_GB,  # in GB
        root_dir: str = "/tmp/shared-kv"
    ):

        self.model_name = model_name
        self.tp_size = tp_size
        self.tp_rank = tp_rank
        self.dtype = dtype
        self.gpu_blocks_per_file = gpu_blocks_per_file
        self.base_path = self.get_kv_cache_base_path(model_name, tp_size, tp_rank, dtype, root_dir)
        self.threads_per_gpu = min(threads_per_gpu , int(os.cpu_count()), DEFAULT_MAX_THREADS_PER_GPU)
        self.max_staging_memory_gb = max_staging_memory_gb
        self.h2d_stream = torch.cuda.Stream()
        self.d2h_stream = torch.cuda.Stream()
        self.attn_backends = attn_backends

    # ----------------------------
    # Shared path helpers
    # ----------------------------
    @staticmethod
    def get_kv_cache_base_path(model_name, tp_size, tp_rank, dtype, root_dir: str) -> Path:
        """Build base path for KV cache storage."""
        dtype_str = str(dtype).replace("torch.", "")
        base_path = Path(f"{root_dir}/{model_name}/tp_{tp_size}/rank_{tp_rank}/{dtype_str}")
        base_path.mkdir(parents=True, exist_ok=True)
        return base_path

    @staticmethod
    def get_file_name(base_path: Path, block_hash: int) -> Path:
        """Return file path for a given block hash."""
        if isinstance(block_hash, bytes): # convert bytes to int
            block_hash = int.from_bytes(block_hash, "little")
        block_hash_hex = f"{block_hash & ((1 << 64) - 1):016x}"
        subfolder1, subfolder2 = block_hash_hex[:3], block_hash_hex[3:5]
        full_path = base_path / subfolder1 / subfolder2 / f"{block_hash_hex}.bin"
        os.makedirs(full_path.parent, exist_ok=True)
        return full_path

    def compute_buffer_size_mb(self,tensors, gpu_blocks_per_file, layers_before_num_blocks, num_blocks_idx, safety=1.0, min_mb=32, max_mb=None):
        """Estimate staging memory size in MB, applying safety and min/max limits."""
        ref = tensors[0]
        # extract exactly one KV block (block 0) using index_select and then remove(squeeze) the block dimension
        per_block = ref.index_select(num_blocks_idx, torch.tensor([0], device=ref.device)).squeeze(num_blocks_idx)

        per_block_elems = per_block.numel()
        block_elems = per_block_elems * gpu_blocks_per_file # multiply by blocks per file
        total_elems = block_elems * len(tensors) if layers_before_num_blocks else block_elems
        total_bytes = total_elems * ref.element_size()
        mb = math.ceil(total_bytes / (1024 * 1024) * safety)
        if min_mb: mb = max(mb, min_mb)
        if max_mb: mb = min(mb, max_mb)
        return mb

    def get_finished(self) -> list[TransferResult]:
        """Poll finished async transfers."""
        return storage_offload.get_finished()

    def __del__(self):
        """Clean up performance resources on destruction."""
        storage_offload.cleanup_resources()

    def get_kv_cache_parmeters(self, gpu_caches: dict[str, torch.Tensor]):
        """Determine KV cache layout parameters according to the attention backend."""
        # These lists collect per-layer metadata about the KV-cache layout.
        list_num_blocks_idx = []
        list_kv_before_num_blocks = []
        list_layers_before_num_blocks = []

        for layer_name, gpu_tensor in gpu_caches.items():
            gpu_shape = gpu_tensor.shape
            attn_backend = self.attn_backends[layer_name]

            # Generate a reference KV-cache shape using known parameters.
            # We compare gpu_shape with this synthetic shape to infer the layout.
            test_shape = attn_backend.get_kv_cache_shape(
                num_blocks=1234, block_size=16, num_kv_heads=8, head_size=256
            )

            # Case 1: Cross-layer tensor - an extra layer dimension exists on each tensor.
            # In this case, num_blocks is the leading dimension.
            if len(gpu_shape) != len(test_shape):
                assert len(gpu_shape) == len(test_shape) + 1
                num_blocks_idx = 0
                kv_before_num_blocks = False
                layers_before_num_blocks = False

            # Case 2: Standard layout - each element represents a single layer with
            # tensor shaped as (num_blocks, ...). The first dimension matches num_blocks.
            elif test_shape[0] == 1234:
                num_blocks_idx = 0
                kv_before_num_blocks = False
                layers_before_num_blocks = True

            # Case 3: (2, num_blocks, ...) - standard layout but with KV first:
            # (2, num_blocks, heads, block_size, head_size).
            else:
                assert test_shape[0] == 2
                assert test_shape[1] == 1234
                assert gpu_shape[0] == 2

                num_blocks_idx = 1
                kv_before_num_blocks = True
                layers_before_num_blocks = True

            # Store layout metadata for this layer.
            list_num_blocks_idx.append(num_blocks_idx)
            list_kv_before_num_blocks.append(kv_before_num_blocks)
            list_layers_before_num_blocks.append(layers_before_num_blocks)

        return (
            list_num_blocks_idx,
            list_kv_before_num_blocks,
            list_layers_before_num_blocks,
        )

# ----------------------------------------------------------------------
# GPU → Storage (PUT)
# ----------------------------------------------------------------------
class GPUStorageOffloadingHandler(StorageOffloadingHandler):
    """Handler for writing KV blocks from GPU tensors into shared storage."""
    def __init__(
        self,
        model_name: str,
        tp_size: int,
        tp_rank: int,
        kv_caches: Dict[str, torch.Tensor],
        gpu_blocks_per_file: int,
        attn_backends: Dict[str, type[AttentionBackend]],
        dtype: torch.dtype,
        threads_per_gpu: Optional[int] = None,
        max_staging_memory_gb: float = DEFAULT_MAX_STAGING_MEMORY_GB,
        root_dir: str = "/tmp/shared-kv"
    ):

        super().__init__(model_name, tp_size, tp_rank, dtype,
                         gpu_blocks_per_file, threads_per_gpu, attn_backends, max_staging_memory_gb, root_dir)

        self.src_tensors = list(kv_caches.values())
        # Determine KV cache layout parameters
        num_blocks_idx, kv_before_num_blocks, layers_before_num_blocks = self.get_kv_cache_parmeters(kv_caches)
        # Compute staging memory buffer size
        self.buffer_size_mb = self.compute_buffer_size_mb(self.src_tensors, gpu_blocks_per_file, layers_before_num_blocks[0], num_blocks_idx=num_blocks_idx[0])
        if self.buffer_size_mb * self.threads_per_gpu > self.max_staging_memory_gb * 1024:
            self.threads_per_gpu = min(self.threads_per_gpu, int(self.max_staging_memory_gb * 1024 / self.buffer_size_mb))
            print(f"[WARN] Adjusted threads_per_gpu to {self.threads_per_gpu} due to max_staging_memory_gb {self.max_staging_memory_gb} limit "+
                  f" (buffer_size_mb={self.buffer_size_mb}).")


        # Initialize storage offload resources
        storage_offload.init_resources(
            io_threads            = self.threads_per_gpu,
            staging_buffer_size_mb = self.buffer_size_mb,
            max_staging_memory_gb  = self.max_staging_memory_gb,
            tp_rank               = self.tp_rank,
            kv_before_blocks      = kv_before_num_blocks[0],     # assuming all layers have the same layout
            layers_before_blocks  = layers_before_num_blocks[0], # assuming all layers have the same layout
            block_axis            = num_blocks_idx[0],           # assuming all layers have the same layout
        )

        logger.info(
            f"GPUStorageOffloadingHandler: "
            f"number_of_gpu={self.tp_size},"
            f"tp_rank={self.tp_rank},"
            f"threads_per_gpu={self.threads_per_gpu},"
            f"staging_buffer_size_mb={self.buffer_size_mb}, "
            f"max_staging_memory_gb={self.max_staging_memory_gb}, "
            f"root_dir={self.base_path},"
            f"kv_before_num_blocks={kv_before_num_blocks[0]}, "
            f"layers_before_num_blocks={layers_before_num_blocks[0]}, "
            f"block_axis={num_blocks_idx[0]}"
        )

    # GPU → Storage async transfer
    def transfer_async(self, job_id: int, spec: TransferSpec) -> bool:
        """Launch async PUT transfers from GPU tensors to files.
        Prepare arrays containing file paths, GPU block IDs to copy, and the list of GPU tensors."""
        src_spec, dst_spec = spec
        if dst_spec is None or len(dst_spec.block_hashes) == 0:
            return True

        target_files    = []
        all_block_ids   = []
        for i, block_hash in enumerate(dst_spec.block_hashes):
            start = i * self.gpu_blocks_per_file
            end = min((i + 1) * self.gpu_blocks_per_file, len(src_spec.block_ids))
            if start >= len(src_spec.block_ids):
                break
            block_ids = src_spec.block_ids[start:end]
            target_file = str(self.get_file_name(self.base_path, block_hash))
            target_files.append(target_file)
            all_block_ids.append(block_ids)
            logger.debug(f"[DEBUG PUT] dst_spec {i}: len block_ids={len(block_ids)} block_ids={block_ids}")

        # Launch async transfer
        storage_offload.transfer_async_put(job_id, target_files, self.src_tensors, all_block_ids)

        return True

# ----------------------------------------------------------------------
# Storage → GPU (GET)
# ----------------------------------------------------------------------
class StorageGPUOffloadingHandler(StorageOffloadingHandler):
    """Handler for reading KV blocks from shared storage back into GPU."""

    def __init__(
        self,
        model_name: str,
        tp_size: int,
        tp_rank: int,
        dtype: torch.dtype,
        gpu_blocks_per_file: int,
        kv_caches: Dict[str, torch.Tensor],
        attn_backends: Dict[str, type[AttentionBackend]],
        threads_per_gpu: Optional[int] = None,
        max_staging_memory_gb: float = DEFAULT_MAX_STAGING_MEMORY_GB,
        root_dir: str = "/tmp/shared-kv"
    ):

        super().__init__(model_name, tp_size, tp_rank, dtype,
                         gpu_blocks_per_file, threads_per_gpu, attn_backends, max_staging_memory_gb, root_dir)
        self.dst_tensors = list(kv_caches.values())

    # Storage → GPU async transfer
    def transfer_async(self, job_id: int, spec: TransferSpec) -> bool:
        """Launch async GET transfers from files to GPU tensors,
        preparing arrays of file paths, block IDs, and tensors."""
        src_spec, dst_spec = spec
        if src_spec is None or len(src_spec.block_hashes) == 0:
            return True

        source_files = []
        all_block_ids = []
        first_len = len(dst_spec.block_ids) % self.gpu_blocks_per_file or self.gpu_blocks_per_file
        start = 0
        for i, block_hash in enumerate(src_spec.block_hashes):
            if i == 0:
                size = first_len
            else:
                size = self.gpu_blocks_per_file

            end = min(start + size, len(dst_spec.block_ids))
            block_ids = dst_spec.block_ids[start:end]
            logger.debug(f"[DEBUG GET] src_spec {i}: len block_ids={len(block_ids)} block_ids={block_ids}")
            source_files.append(str(self.get_file_name(self.base_path, block_hash)))
            all_block_ids.append(block_ids)
            start += size

        # Launch async transfer
        storage_offload.transfer_async_get(job_id, source_files, all_block_ids, self.dst_tensors, self.gpu_blocks_per_file)

        return True
