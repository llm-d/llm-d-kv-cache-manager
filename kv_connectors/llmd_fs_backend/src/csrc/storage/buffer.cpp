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

// storage_buffer.cpp
#include <torch/extension.h>
#include <cuda.h>
#include <cuda_runtime.h>
#include <numa.h>
#include <iostream>
#include <fstream>
#include <filesystem>
#include <thread>
#include <vector>
#include <algorithm>
#include <sys/sysinfo.h>

#include "buffer.hpp"
#include "debug_utils.hpp"

// Thread-local pinned buffer used by each IO thread
thread_local PinnedBufferInfo t_pinned_buffer{};

// Global pinned buffers used by IO threads
std::vector<PinnedBufferInfo> g_pinned_buffers;

// Return thread-local pinned buffer, allocating or reallocating if needed
std::pair<void*, size_t> get_thread_local_pinned(size_t required_bytes, int numa_node) {
    if (!t_pinned_buffer.ptr || t_pinned_buffer.size < required_bytes) {
        if (t_pinned_buffer.ptr) {
            cudaFreeHost(t_pinned_buffer.ptr);
            t_pinned_buffer.ptr = nullptr;
            t_pinned_buffer.size = 0;
            std::cerr << "[WARN] Thread " << std::this_thread::get_id() << " existing pinned buffer too small (" << t_pinned_buffer.size
                      << " bytes), reallocating " << required_bytes << " bytes\n";
        }

        size_t alloc_size = std::max(required_bytes, (size_t)16 * 1024 * 1024);
        cudaError_t err = cudaHostAlloc(&t_pinned_buffer.ptr, alloc_size, cudaHostAllocMapped | cudaHostAllocPortable);

        if (err != cudaSuccess) {
            std::cerr << "[ERROR] cudaHostAlloc failed: " << cudaGetErrorString(err) << "\n";
            t_pinned_buffer.ptr = nullptr;
            t_pinned_buffer.size = 0;
        } else {
            t_pinned_buffer.size = alloc_size;
            DEBUG_PRINT("[INFO] Thread " << std::this_thread::get_id() << " allocated pinned buffer " << (alloc_size / (1024 * 1024))
                                         << " MB");
        }
    }
    return {t_pinned_buffer.ptr, t_pinned_buffer.size};
}

// Preallocate thread-local pinned buffers for all IO threads
void preallocate_pinned_buffers(size_t io_threads, size_t pinned_buffer_size_mb) {
    g_pinned_buffers.resize(io_threads);
    size_t alloc_bytes = pinned_buffer_size_mb * 1024 * 1024;

    std::vector<std::thread> workers;
    workers.reserve(io_threads);

    for (size_t i = 0; i < io_threads; ++i) {
        workers.emplace_back([i, alloc_bytes]() {
            auto [ptr, size] = get_thread_local_pinned(alloc_bytes);
            if (!ptr) {
                std::cerr << "[ERROR] Failed to preallocate pinned buffer for thread " << i << std::endl;
                g_pinned_buffers[i] = {nullptr, 0};
            } else {
                g_pinned_buffers[i] = {ptr, size};
            }
        });
    }

    // Wait for all threads to complete initialization
    for (auto& t : workers) t.join();

    std::cout << "[INFO] Pre-allocated pinned buffer " << (alloc_bytes / (1024 * 1024)) << " MB for " << io_threads << " threads"
              << std::endl;
}

// Return NUMA node associated with a given GPU
int get_gpu_numa_node(int device_id) {
    CUdevice device;
    int numa_node = -1;

    cuInit(0);
    cuDeviceGet(&device, device_id);
    cuDeviceGetAttribute(&numa_node, CU_DEVICE_ATTRIBUTE_HOST_NUMA_ID, device);
    return numa_node;
}

// Return list of CPU cores that belong to a NUMA node
std::vector<int> get_cpus_in_numa_node(int node) {
    // Read the cpulist file for this NUMA node (e.g. "0-13,84-97")
    std::vector<int> cpus;
    if (node < 0) return cpus;
    std::string path = "/sys/devices/system/node/node" + std::to_string(node) + "/cpulist";
    std::ifstream f(path);
    if (!f.is_open()) return cpus;
    std::string list;
    f >> list;
    f.close();

    // Parse ranges like "0-13,84-97"
    size_t start = 0;
    while (start < list.size()) {
        size_t comma = list.find(',', start);
        std::string token = list.substr(start, comma - start);
        size_t dash = token.find('-');
        if (dash != std::string::npos) {
            int a = std::stoi(token.substr(0, dash));
            int b = std::stoi(token.substr(dash + 1));
            for (int c = a; c <= b; ++c) cpus.push_back(c);
        } else {
            cpus.push_back(std::stoi(token));
        }
        if (comma == std::string::npos) break;
        start = comma + 1;
    }
    return cpus;
}
