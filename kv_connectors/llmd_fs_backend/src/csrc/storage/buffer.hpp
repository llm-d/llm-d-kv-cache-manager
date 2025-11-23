/*
 * Copyright 2025 The llm-d Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#pragma once

#include <cstddef>
#include <vector>

struct PinnedBufferInfo {
    void* ptr = nullptr;
    size_t size = 0;
};

// Thread-local pinned pointers defined in buffer.cpp
extern thread_local PinnedBufferInfo t_pinned_buffer;

// Global preallocated pool per IO thread
extern std::vector<PinnedBufferInfo> g_pinned_buffers;

// Preallocate pinned buffers for IO threads
void preallocate_pinned_buffers(size_t io_threads, size_t pinned_buffer_size_mb);

// Return thread-local pinned buffer, allocating or reallocating if needed
std::pair<void*, size_t> get_thread_local_pinned(size_t required_bytes, int numa_node = -1);

// Return NUMA node that a GPU belongs to
int get_gpu_numa_node(int device_id);

// Return list of CPU cores in the given NUMA node
std::vector<int> get_cpus_in_numa_node(int node);
