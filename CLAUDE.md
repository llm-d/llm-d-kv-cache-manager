# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the `llm-d-kv-cache-manager`, a high-performance Go service that provides KV-Cache aware routing for distributed LLM inference. The core component is the **KVCache Indexer** which maintains a global, near-real-time view of KV-Cache block locality across vLLM pods to enable intelligent request routing.

## Development Commands

### Building
- `make build` - Build the main binary (requires tokenizer download)
- `make download-tokenizer` - Download HuggingFace tokenizer bindings (required before building)
- `make image-build` - Build Docker image

### Testing
- `make test` - Run all tests (unit + e2e)  
- `make unit-test` - Run unit tests only
- `make e2e-test` - Run end-to-end tests only

### Code Quality
- `make precommit` - Run all pre-commit checks (tidy, lint, copyright fix)
- `make lint` - Run golangci-lint
- `make tidy-go` - Tidy go.mod and go.sum

### Development Setup
The project requires external tokenizer bindings. Always run `make download-tokenizer` before building or testing.

## Architecture

### Core Components
- **`kvcache.Indexer`** - Main orchestrator handling scoring requests
- **`kvevents.Pool`** - Ingests KV-cache events from vLLM pods via ZMQ
- **`kvblock.Index`** - Core data store mapping KV-block hashes to pod locations
- **`tokenization.PrefixStore`** - Caches tokenized prompt prefixes
- **`kvblock.TokenProcessor`** - Converts tokens to content-addressable block keys
- **`kvblock.Scorer`** - Scores pods based on cache hit sequences

### Key Directories
- `pkg/kvcache/` - Core indexer logic and KV-block management
- `pkg/tokenization/` - Tokenization subsystem with prefix caching  
- `pkg/kvcache/kvevents/` - Event ingestion from vLLM pods
- `examples/` - Reference implementations and usage examples
- `tests/e2e/` - End-to-end testing with Redis mocks

### Data Flows
1. **Read Path (Scoring)**: Router → Indexer → PrefixStore → TokenProcessor → Index → Scorer → Router
2. **Write Path (Events)**: vLLM Pod → ZMQ → Pool → Worker → Index

### Critical Implementation Details
- KV-block hashing must match vLLM's algorithm exactly (SHA-256, lower 64 bits)
- Hash chain uses configurable `HashSeed` that must align with vLLM's `PYTHONHASHSEED`
- Token chunking defaults to 256 tokens per block
- Events are sharded by pod ID (FNV-1a hash) to ensure ordering per pod
- Async tokenization prevents blocking on scoring requests

## Configuration Notes
- Index supports in-memory (default) and Redis backends
- PrefixStore has LRU (default) and Trie implementations  
- All major components are configurable via `Config` structs
- See `docs/configuration.md` for detailed configuration options