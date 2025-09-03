# Configuration

This document describes all configuration options available in the llm-d KV Cache Manager. 
All configurations are JSON-serializable.

## Main Configuration

This package consists of two components:
1. **KV Cache Indexer**: Manages the KV cache index, allowing efficient retrieval of cached blocks.
2. **KV Event Processing**: Handles events from vLLM to update the cache index.

See the [Architecture Overview](architecture.md) for a high-level view of how these components work and interact.

The two components are configured separately, but share the index backend for storing KV block localities.
The latter is configured via the `kvBlockIndexConfig` field in the KV Cache Indexer configuration.

### Indexer Configuration (`Config`)

The main configuration structure for the KV Cache Indexer module.

```json
{
  "prefixStoreConfig": { ... },
  "tokenProcessorConfig": { ... },
  "kvBlockIndexConfig": { ... },
  "tokenizersPoolConfig": { ... }
}
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `prefixStoreConfig` | [LRUStoreConfig](#lru-store-configuration-lrustoreconfig) | Configuration for the prefix store | See defaults |
| `tokenProcessorConfig` | [TokenProcessorConfig](#token-processor-configuration-tokenprocessorconfig) | Configuration for token processing | See defaults |
| `kvBlockIndexConfig` | [IndexConfig](#index-configuration-indexconfig) | Configuration for KV block indexing | See defaults |
| `tokenizersPoolConfig` | [Config](#tokenization-pool-configuration-config) | Configuration for tokenization pool | See defaults |


## Complete Example Configuration

Here's a complete configuration example with all options:

```json
{
  "prefixStoreConfig": {
    "cacheSize": 500000,
    "blockSize": 256
  },
  "tokenProcessorConfig": {
    "blockSize": 16,
    "hashSeed": "12345"
  },
  "kvBlockIndexConfig": {
    "inMemoryConfig": {
      "size": 100000000,
      "podCacheSize": 10
    },
    "enableMetrics": true,
    "metricsLoggingInterval": "1m0s"
  },
  "tokenizersPoolConfig": {
    "workersCount": 8,
    "minPrefixOverlapRatio": 0.85,
    "huggingFaceToken": "your_hf_token_here",
    "tokenizersCacheDir": "/tmp/tokenizers"
  }
}
```

## KV-Block Index Configuration

### Index Configuration (`IndexConfig`)

Configures the KV-block index backend. Multiple backends can be configured, but only the first available one will be used.

```json
{
  "inMemoryConfig": { ... },
  "costAwareMemoryConfig": { ... },
  "redisConfig": { ... },
  "enableMetrics": false
}
```

| Field | Type                                                  | Description | Default |
|-------|-------------------------------------------------------|-------------|---------|
| `inMemoryConfig` | [InMemoryIndexConfig](#in-memory-index-configuration) | In-memory index configuration | See defaults |
| `costAwareMemoryConfig` | [CostAwareMemoryIndexConfig](#cost-aware-memory-index-configuration) | Cost-aware memory index configuration | `null` |
| `redisConfig` | [RedisIndexConfig](#redis-index-configuration)        | Redis index configuration | `null` |
| `enableMetrics` | `boolean`                                             | Enable admissions/evictions/hits/misses recording | `false` |
| `metricsLoggingInterval` | `string` (duration) | Interval at which metrics are logged (e.g., `"1m0s"`). If zero or omitted, metrics logging is disabled. Requires `enableMetrics` to be `true`. | `"0s"` |

### In-Memory Index Configuration (`InMemoryIndexConfig`)

Configures the in-memory KV block index implementation.

```json
{
  "size": 100000000,
  "podCacheSize": 10
}
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `size` | `integer` | Maximum number of keys that can be stored | `100000000` |
| `podCacheSize` | `integer` | Maximum number of pod entries per key | `10` |

### Cost-Aware Memory Index Configuration (`CostAwareMemoryIndexConfig`)

Configures the cost-aware memory-based KV block index implementation using Ristretto cache.

```json
{
  "size": "2GiB"
}
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `size` | `string` | Maximum memory size for the cache. Supports human-readable formats like "2GiB", "500MiB", "1GB", etc. | `"2GiB"` |

### Redis Index Configuration (`RedisIndexConfig`)

Configures the Redis-backed KV block index implementation.

```json
{
  "address": "redis://127.0.0.1:6379"
}
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `address` | `string` | Redis server address (can include auth: `redis://user:pass@host:port/db`) | `"redis://127.0.0.1:6379"` |

## Token Processing Configuration

### Token Processor Configuration (`TokenProcessorConfig`)

Configures how tokens are converted to KV-block keys.

```json
{
  "blockSize": 16,
  "hashSeed": ""
}
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `blockSize` | `integer` | Number of tokens per block | `16` |
| `hashSeed` | `string` | Seed for hash generation (should align with vLLM's PYTHONHASHSEED) | `""` |

## Prefix Store Configuration

### LRU Store Configuration (`LRUStoreConfig`)

Configures the LRU-based prefix token store.

```json
{
  "cacheSize": 500000,
  "blockSize": 256
}
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `cacheSize` | `integer` | Maximum number of blocks the LRU cache can store | `500000` |
| `blockSize` | `integer` | Number of **characters** per block in the tokenization prefix-cache | `256` |

## Tokenization Configuration

### Tokenization Pool Configuration (`Config`)

Configures the tokenization worker pool and cache utilization strategy.

```json
{
  "workersCount": 5,
  "minPrefixOverlapRatio": 0.8,
  "huggingFaceToken": "",
  "tokenizersCacheDir": ""
}
```

| Field | Type | Description | Default |
|-------|------|-------------|--------|
| `workersCount` | `integer` | Number of tokenization worker goroutines | `5` |
| `minPrefixOverlapRatio` | `float64` | Minimum overlap ratio to use cached prefix tokens (0.0-1.0) | `0.8` |
| `huggingFaceToken` | `string` | HuggingFace authentication token | `""` |
| `tokenizersCacheDir` | `string` | Directory for caching tokenizers | `""` |


### HuggingFace Tokenizer Configuration (`HFTokenizerConfig`)

Configures the HuggingFace tokenizer backend.

```json
{
  "huggingFaceToken": "",
  "tokenizersCacheDir": ""
}
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `huggingFaceToken` | `string` | HuggingFace API token for accessing models | `""` |
| `tokenizersCacheDir` | `string` | Local directory for caching downloaded tokenizers | `"./bin"` |

## KV-Event Processing Configuration

### KV-Event Pool Configuration (`Config`)

Configures the ZMQ event processing pool for handling KV cache events.

```json
{
  "zmqEndpoint": "tcp://*:5557",
  "topicFilter": "kv@",
  "concurrency": 4
}
```

## Event Processing Configuration Example

For the ZMQ event processing pool:

```json
{
  "zmqEndpoint": "tcp://indexer:5557",
  "topicFilter": "kv@",
  "concurrency": 8
}
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `zmqEndpoint` | `string` | ZMQ address to connect to | `"tcp://*:5557"` |
| `topicFilter` | `string` | ZMQ subscription filter | `"kv@"` |
| `concurrency` | `integer` | Number of parallel workers | `4` |

---
## Notes

1. **Hash Seed Alignment**: The `hashSeed` in `TokenProcessorConfig` should be aligned with vLLM's `PYTHONHASHSEED` environment variable to ensure consistent hashing across the system.

2. **Memory Considerations**: 
   - The `size` parameter in `InMemoryIndexConfig` directly affects memory usage. Each key-value pair consumes memory proportional to the number of associated pods.
   - The `size` parameter in `CostAwareMemoryIndexConfig` controls the maximum memory footprint and supports human-readable formats (e.g., "2GiB", "500MiB", "1GB").

3. **Performance Tuning**: 
   - Increase `workersCount` in tokenization config for higher tokenization throughput
   - Adjust `minPrefixOverlapRatio`: lower values accept shorter cached prefixes, reducing full tokenization overhead
   - Adjust `concurrency` in event processing for better event handling performance
   - Tune cache sizes based on available memory and expected workload

4. **Cache Directories**: If used, ensure the `tokenizersCacheDir` has sufficient disk space and appropriate permissions for the application to read/write tokenizer files.

5. **Redis Configuration**: When using Redis backend, ensure Redis server is accessible and has sufficient memory. The `address` field supports full Redis URLs including authentication: `redis://user:pass@host:port/db`.
