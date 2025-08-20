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

# Single stage: Build and run everything in one container with Python
FROM quay.io/projectquay/golang:1.24
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

USER root
# Install EPEL repository and all necessary dependencies including Python
RUN dnf install -y 'https://dl.fedoraproject.org/pub/epel/epel-release-latest-8.noarch.rpm' && \
    dnf install -y gcc-c++ libstdc++ libstdc++-devel clang zeromq-devel pkgconfig python39-devel python39-pip && \
    dnf clean all

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy examples first (for better caching)
COPY examples/kv_events examples/kv_events

# Copy all source code
COPY . .

# HuggingFace tokenizer bindings
RUN mkdir -p lib
ARG RELEASE_VERSION=v1.22.1
RUN curl -L https://github.com/daulet/tokenizers/releases/download/${RELEASE_VERSION}/libtokenizers.${TARGETOS:-linux}-${TARGETARCH:-amd64}.tar.gz | tar -xz -C lib
RUN ranlib lib/*.a

# Fix CGo paths for container Python installation
RUN sed -i 's|#cgo CFLAGS: -I.*|#cgo CFLAGS: -I/usr/include/python3.9|g' pkg/preprocessing/chat_completions_template/cgo_functions.go && \
    sed -i 's|#cgo LDFLAGS: -L.* -lpython.*|#cgo LDFLAGS: -L/usr/lib64 -lpython3.9|g' pkg/preprocessing/chat_completions_template/cgo_functions.go

# Build the UNIFIED online example (not the original online_docker_chat_completions)
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -tags=chat_completions -ldflags="-extldflags '-L$(pwd)/lib'" -a -o bin/kv-cache-manager examples/kv_events/unified_online/main.go

# Install Python dependencies for chat completions
COPY pkg/preprocessing/chat_completions_template/requirements-docker.txt /workspace/requirements.txt
RUN python3.9 -m pip install -r /workspace/requirements.txt

# Set up Python environment variables
ENV PYTHONPATH=/workspace/pkg/preprocessing/chat_completions_template:/usr/local/lib64/python3.9/site-packages:/usr/local/lib/python3.9/site-packages:/usr/lib64/python3.9/site-packages:/usr/lib/python3.9/site-packages:$PYTHONPATH
ENV HF_HOME=/tmp/hf_cache

# Create HF cache directory
RUN mkdir -p /tmp/hf_cache

# Set working directory
WORKDIR /workspace

# Run the unified online service
ENTRYPOINT ["/workspace/bin/kv-cache-manager"] 