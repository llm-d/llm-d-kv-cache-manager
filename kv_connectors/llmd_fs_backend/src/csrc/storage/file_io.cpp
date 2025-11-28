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

#include <filesystem>
#include <fstream>
#include <vector>
#include <cstring>
#include <cerrno>
#include <fcntl.h>
#include <sys/stat.h>

#include "file_io.hpp"
#include "buffer.hpp"

namespace fs = std::filesystem;

// Thread-local index for CUDA streams
extern thread_local size_t thread_stream_idx;

// Write a tensor to disk using a temporary file and atomic rename
bool write_file_to_disk(const std::string& target_path, const torch::Tensor& host_buf) {
    // Pointer and size of data to write
    const void* data_ptr = host_buf.data_ptr();
    size_t nbytes = host_buf.nbytes();

    // Create parent directory if needed
    fs::path file_path(target_path);
    fs::path parent_dir = file_path.parent_path();
    try {
        fs::create_directories(parent_dir);
    } catch (const fs::filesystem_error& e) {
        std::cerr << "[ERROR] Failed to create directories: " << e.what() << "\n";
        return false;
    }

    // Write to a temporary file to ensure atomic replace on rename
    // Include thread_stream_idx so each thread uses a unique temporary file
    std::string tmp_path = target_path + std::to_string(thread_stream_idx) + ".tmp.";

    // Define a larger buffer (1MB) to reduce syscall overhead and speed up I/O
    const size_t WRITE_BUFFER_SIZE = 1 * 1024 * 1024;  // 1MB buffer

    std::ofstream ofs(tmp_path, std::ios::out | std::ios::binary);
    if (!ofs) {
        std::cerr << "[ERROR] Failed to open temporary file for writing: " << tmp_path << " - " << std::strerror(errno) << "\n";
        return false;
    }

    // Allocate custom I/O buffer for this stream (replaces small default buffer)
    std::vector<char> buffer(WRITE_BUFFER_SIZE);
    // Apply the custom buffer to the file stream
    ofs.rdbuf()->pubsetbuf(buffer.data(), WRITE_BUFFER_SIZE);

    // Write file contents
    ofs.write(reinterpret_cast<const char*>(data_ptr), nbytes);
    if (!ofs) {
        std::cerr << "[ERROR] Failed to write to temporary file: " << tmp_path << " - " << std::strerror(errno) << "\n";
        return false;
    }
    ofs.close();

    // Atomically rename temp file to final target name after successful write
    if (std::rename(tmp_path.c_str(), target_path.c_str()) != 0) {
        std::cerr << "[ERROR] "
                  << "Failed to rename " + tmp_path + " to " + target_path + " - " + std::strerror(errno) << "\n";
        return false;
    }

    return true;
}

// Read a file into a pinned CPU tensor using the thread-local pinned buffer
torch::Tensor read_file_from_disk(const std::string& path) {
    // Open file
    std::ifstream ifs(path, std::ios::in | std::ios::binary | std::ios::ate);
    if (!ifs) {
        std::cerr << "[ERROR] Failed to open file: " << path << "\n";
        throw std::runtime_error("Failed to open file: " + path);
    }

    // Determine file size
    size_t file_size = static_cast<size_t>(ifs.tellg());
    ifs.seekg(0, std::ios::beg);

    // Acquire pinned buffer of the required size
    auto [pinned_ptr, pinned_size] = get_thread_local_pinned(file_size);
    if (!pinned_ptr || pinned_size < file_size) {
        std::cerr << "[ERROR] Pinned buffer too small for file: " << path << "\n"
                  << "[INFO] Required size: " << file_size << " bytes, Available size: " << pinned_size << " bytes\n"
                  << "pinned_ptr: " << pinned_ptr << "\n";
        throw std::runtime_error("Pinned buffer too small for file: " + path);
    }

    // Read file into pinned memory
    ifs.read(reinterpret_cast<char*>(pinned_ptr), file_size);
    ifs.close();

    // Wrap pinned buffer into a Torch tensor (CPU, pinned)
    auto options = torch::TensorOptions()
                       .dtype(torch::kUInt8)  // raw bytes
                       .device(torch::kCPU)
                       .pinned_memory(true);

    // Wrap pinned memory into a tensor without copying the data
    auto tensor = torch::from_blob(
        pinned_ptr,
        {static_cast<long>(file_size)},
        [pinned_ptr](void* /*unused*/) {},
        options);
    return tensor;
}

// update_atime update only the atime of a file without changing mtime
void update_atime(const std::string& path) {
    struct timespec times[2];
    times[0].tv_nsec = UTIME_OMIT;  // keep mtime unchanged
    times[1].tv_nsec = UTIME_NOW;   // update atime to now

    utimensat(AT_FDCWD, path.c_str(), times, 0);
}
