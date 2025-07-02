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

# Build Stage: using Go 1.24.1 image
FROM quay.io/projectquay/golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

USER root
RUN dnf install -y gcc-c++ libstdc++ libstdc++-devel clang && dnf clean all

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY examples/kv-cache-index/main.go cmd/cmd.go
COPY . .

# HuggingFace tokenizer bindings
RUN mkdir -p lib
RUN curl -L https://github.com/daulet/tokenizers/releases/download/v1.20.2/libtokenizers.${TARGETOS}-${TARGETARCH}.tar.gz | tar -xz -C lib
RUN ranlib lib/*.a

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make image-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.

RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -ldflags="-extldflags '-L$(pwd)/lib'" -a -o bin/kv-cache-manager examples/kv-events/online/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM registry.access.redhat.com/ubi9/ubi:latest
WORKDIR /
COPY --from=builder /workspace/bin/kv-cache-manager /app/kv-cache-manager
USER 65532:65532

# Set the entrypoint to the kv-cache-manager binary
ENTRYPOINT ["/app/kv-cache-manager"]


