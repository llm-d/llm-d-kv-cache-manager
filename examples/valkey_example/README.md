# Valkey Example for KV-Cache Manager

This example demonstrates how to use Valkey as the backend for the KV-Cache Manager's KV-block indexing system.

## What is Valkey?

Valkey is a community-forked version of Redis that remains under the original BSD license. It's fully API-compatible with Redis and offers additional features like RDMA support for improved latency in high-performance scenarios.

## Benefits of Using Valkey

- **Open Source**: Remains under the BSD license
- **Redis Compatibility**: Drop-in replacement for Redis
- **RDMA Support**: Lower latency networking for high-performance workloads  
- **Community Backed**: Supported by major cloud vendors and Linux Foundation
- **Performance**: Optimizations for modern hardware

## Prerequisites

1. **Valkey Server**: Install and run a Valkey server
   ```bash
   # Using Docker
   docker run -d -p 6379:6379 valkey/valkey:latest
   
   # Or install from source/package manager
   ```

2. **Go Environment**: Go 1.24.1 or later

3. **Optional**: Hugging Face token for tokenizer access
   ```bash
   export HF_TOKEN="your-huggingface-token"
   ```

## Running the Example

### Basic Usage

```bash
# Run with default Valkey configuration
go run main.go

# Run with custom Valkey address
VALKEY_ADDR="valkey://your-valkey-server:6379" go run main.go
```

### With RDMA Support

If your Valkey server supports RDMA:

```bash
VALKEY_ADDR="valkey://rdma-valkey-server:6379" \
VALKEY_ENABLE_RDMA="true" \
go run main.go
```

### Environment Variables

- `VALKEY_ADDR`: Valkey server address (default: `valkey://127.0.0.1:6379`)
- `VALKEY_ENABLE_RDMA`: Enable RDMA transport (default: `false`)
- `HF_TOKEN`: Hugging Face token for tokenizer access (optional)

## What the Example Does

1. **Configuration**: Sets up a KV-Cache Manager with Valkey backend
2. **Cache Operations**: Demonstrates adding prompts to the cache
3. **Cache Hits**: Shows how repeated prompts result in cache hits
4. **Multi-Pod Lookup**: Demonstrates cache sharing across multiple pods
5. **Metrics**: Enables metrics collection for monitoring cache performance

## Expected Output

```
I0104 10:30:00.123456       1 main.go:45] Initializing KV-Cache Manager with Valkey backend valkeyAddr="valkey://127.0.0.1:6379" rdmaEnabled=false
I0104 10:30:00.234567       1 main.go:109] Processing prompt iteration=1 prompt="Hello, how are you today?"
I0104 10:30:00.345678       1 main.go:122] Cache score prompt="Hello, how are you today?" score=1.0 podID="demo-pod-1"
I0104 10:30:00.456789       1 main.go:109] Processing prompt iteration=3 prompt="Hello, how are you today?"
I0104 10:30:00.567890       1 main.go:122] Cache score prompt="Hello, how are you today?" score=1.0 podID="demo-pod-1"
...
I0104 10:30:02.123456       1 main.go:65] Valkey example completed successfully
```

## Comparison with Redis

The Valkey backend is API-compatible with Redis, so you can easily switch between them:

### Redis Configuration
```json
{
  "kvBlockIndexConfig": {
    "redisConfig": {
      "address": "redis://127.0.0.1:6379"
    }
  }
}
```

### Valkey Configuration  
```json
{
  "kvBlockIndexConfig": {
    "valkeyConfig": {
      "address": "valkey://127.0.0.1:6379",
      "backendType": "valkey",
      "enableRDMA": false
    }
  }
}
```

## Performance Considerations

- **RDMA**: Enable RDMA for ultra-low latency if your infrastructure supports it
- **Connection Pooling**: The underlying Redis client handles connection pooling
- **Persistence**: Valkey data persists across restarts (unlike in-memory backends)
- **Scalability**: Suitable for distributed deployments with multiple indexer replicas

## Troubleshooting

### Connection Issues
- Ensure Valkey server is running and accessible
- Check network connectivity and firewall rules
- Verify the address format (supports `valkey://`, `redis://`, or plain addresses)

### RDMA Issues
- Confirm Valkey server is compiled with RDMA support
- Verify RDMA hardware and drivers are properly configured
- Check that both client and server are on RDMA-enabled networks

### Performance Issues
- Monitor cache hit rates using the built-in metrics
- Adjust block size in TokenProcessorConfig for your use case
- Consider using multiple Valkey instances for horizontal scaling

## See Also

- [Valkey Configuration Guide](../valkey_configuration.md)
- [KV-Cache Manager Architecture](../../docs/architecture.md)
- [Configuration Reference](../../docs/configuration.md)