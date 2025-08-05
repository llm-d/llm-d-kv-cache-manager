# InferencePerf Benchmark Report

## Configuration

### Workload profile

```yaml
load:
  type: constant
  stages:
  - rate: 2
    duration: 50
  - rate: 5
    duration: 50
  - rate: 8
    duration: 50
  - rate: 10
    duration: 50
  - rate: 12
    duration: 50
  - rate: 15
    duration: 50
  - rate: 20
    duration: 50
api:
  type: completion
  streaming: true
server:
  type: vllm
  model_name: meta-llama/Llama-3.1-70B-Instruct
  base_url: <endpoint>
  ignore_eos: true
tokenizer:
  pretrained_model_name_or_path: meta-llama/Llama-3.1-70B-Instruct
data:
  type: shared_prefix
  shared_prefix:
    num_groups: 60                
    num_prompts_per_group: 1     
    system_prompt_len: 3500       
    question_len: 256             
    output_len: 20               
report:
  request_lifecycle:
    summary: true
    per_stage: true
    per_request: true
storage:
  local_storage:
    path: /workspace
```

### Scheduler Configurations

**baseline**

```yaml
apiVersion: inference.networking.x-k8s.io/v1alpha1
kind: EndpointPickerConfig
plugins:
- type: single-profile-handler
- type: random-picker
schedulingProfiles:
- name: default
  plugins:
    - pluginRef: random-picker
```

**load**

```yaml
apiVersion: inference.networking.x-k8s.io/v1alpha1
kind: EndpointPickerConfig
plugins:
- type: queue-scorer
- type: kv-cache-scorer
- type: max-score-picker
  parameters:
    maxNumOfEndpoints: 1
- type: single-profile-handler
schedulingProfiles:
- name: default
  plugins:
  - pluginRef: queue-scorer
    weight: 1
  - pluginRef: kv-cache-scorer
    weight: 1
  - pluginRef: max-score-picker
```

**estimate-prefix-cache**

```yaml
apiVersion: inference.networking.x-k8s.io/v1alpha1
kind: EndpointPickerConfig
plugins:
- type: queue-scorer
- type: kv-cache-scorer
- type: prefix-cache-scorer
  parameters:
    hashBlockSize: 64
    maxPrefixBlocksToMatch: 256
    lruCapacityPerServer: 31250
- type: max-score-picker
  parameters:
    maxNumOfEndpoints: 1
- type: single-profile-handler
schedulingProfiles:
- name: default
  plugins:
  - pluginRef: queue-scorer
    weight: 1
  - pluginRef: kv-cache-scorer
    weight: 1
  - pluginRef: prefix-cache-scorer
    weight: 1
  - pluginRef: max-score-picker
```

**precise-prefix-cache**

```yaml
apiVersion: inference.networking.x-k8s.io/v1alpha1
kind: EndpointPickerConfig
plugins:
- type: single-profile-handler
- type: prefix-cache-scorer
  parameters:
    mode: cache_tracking
    indexerConfig:
      tokenProcessorConfig:
        blockSize: 64   
        hashSeed: "0"   
      kvBlockIndexConfig:
        enableMetrics: true    
- type: kv-cache-scorer
- type: queue-scorer
- type: max-score-picker
schedulingProfiles:
- name: default
  plugins:
    - pluginRef: prefix-cache-scorer
      weight: 1.0
    - pluginRef: kv-cache-scorer
      weight: 1.0
    - pluginRef: queue-scorer
      weight: 1.0
    - pluginRef: max-score-picker
```

## Charts

### Latency

<img src="medium-scale-frequent-evictions/latency_vs_qps.png" alt="Latency vs QPS" width="950"/>

### Throughput

<img src="medium-scale-frequent-evictions/throughput_vs_qps.png" alt="Throughput vs QPS" width="950"/>

### How to read this report (quick)

- **TTFT** is time to first token; **ITL** is the gap between tokens (both lower is better).
- **TTFT p50/p90** shows median/90th percentile latency for the first token.
- **Output tokens/sec** is the primary throughput metric (higher is better).

- **Requests/sec** shows the rate of completed requests.

- **Success Rate** reflects outcome quality, not volume.

### Summary across QPS


| Experiment | Output toks/s | Requests/s | Success Rate | TTFT mean (s) | TTFT p50/ p90 (s) | ITL mean (s) | ITL p50/ p90 (s) |
|---|---:|---:|---:|---:|---:|---:|---:|
| precise-prefix-cache | 56.9 | 4.135 | 99.92% | 31.932 | 35.078/45.305 | 0.156 | 0.0001/0.378 |
| estimate-prefix-cache | 52.1 | 3.828 | 99.61% | 36.451 | 40.225/47.518 | 0.164 | 0.0001/0.377 |
| load | 52.1 | 3.721 | 100.00% | 37.991 | 43.249/48.244 | 0.167 | 0.0001/0.369 |
| random-routing | 49.8 | 3.796 | 96.33% | 36.009 | 37.268/54.466 | 0.167 | 0.0001/0.377 |

## Per-QPS Results


### QPS = 2.0


| Experiment | Output toks/s | Requests/s | Success Rate | TTFT mean (s) | TTFT p50/ p90 (s) | ITL mean (s) | ITL p50/ p90 (s) |
|---|---:|---:|---:|---:|---:|---:|---:|
| estimate-prefix-cache | 34.8 | 2.033 | 100.00% | 4.491 | 2.707/11.865 | 0.113 | 0.0001/0.353 |
| random-routing | 31.4 | 1.971 | 100.00% | 4.969 | 2.722/11.175 | 0.112 | 0.0001/0.364 |
| precise-prefix-cache | 31.2 | 1.949 | 100.00% | 4.988 | 3.230/8.902 | 0.128 | 0.0001/0.367 |
| load | 32.4 | 1.997 | 100.00% | 6.527 | 2.896/21.968 | 0.102 | 0.0001/0.061 |

### QPS = 5.0


| Experiment | Output toks/s | Requests/s | Success Rate | TTFT mean (s) | TTFT p50/ p90 (s) | ITL mean (s) | ITL p50/ p90 (s) |
|---|---:|---:|---:|---:|---:|---:|---:|
| precise-prefix-cache | 47.0 | 4.168 | 100.00% | 8.417 | 4.391/18.770 | 0.111 | 0.0001/0.371 |
| estimate-prefix-cache | 38.3 | 5.037 | 94.40% | 14.197 | 9.817/32.146 | 0.128 | 0.0001/0.373 |
| random-routing | 50.9 | 4.536 | 100.00% | 14.940 | 14.372/33.748 | 0.161 | 0.0001/0.377 |
| load | 48.8 | 5.057 | 100.00% | 18.657 | 18.382/32.973 | 0.158 | 0.0001/0.376 |

### QPS = 8.0


| Experiment | Output toks/s | Requests/s | Success Rate | TTFT mean (s) | TTFT p50/ p90 (s) | ITL mean (s) | ITL p50/ p90 (s) |
|---|---:|---:|---:|---:|---:|---:|---:|
| precise-prefix-cache | 66.8 | 6.176 | 100.00% | 22.308 | 21.156/42.723 | 0.142 | 0.0001/0.379 |
| estimate-prefix-cache | 59.3 | 4.814 | 100.00% | 31.786 | 34.422/49.165 | 0.161 | 0.0001/0.378 |
| load | 58.5 | 4.695 | 100.00% | 32.227 | 38.884/47.590 | 0.166 | 0.0001/0.378 |
| random-routing | 48.3 | 4.509 | 94.50% | 34.728 | 34.975/60.651 | 0.166 | 0.0001/0.377 |

### QPS = 10.0


| Experiment | Output toks/s | Requests/s | Success Rate | TTFT mean (s) | TTFT p50/ p90 (s) | ITL mean (s) | ITL p50/ p90 (s) |
|---|---:|---:|---:|---:|---:|---:|---:|
| precise-prefix-cache | 65.4 | 4.837 | 100.00% | 30.473 | 35.537/46.260 | 0.154 | 0.0001/0.379 |
| random-routing | 56.5 | 4.294 | 97.20% | 35.171 | 38.494/54.614 | 0.168 | 0.0001/0.378 |
| estimate-prefix-cache | 58.1 | 4.263 | 100.00% | 36.131 | 41.961/49.614 | 0.167 | 0.0001/0.378 |
| load | 54.9 | 3.952 | 100.00% | 38.835 | 47.402/51.098 | 0.169 | 0.0001/0.378 |

### QPS = 12.0


| Experiment | Output toks/s | Requests/s | Success Rate | TTFT mean (s) | TTFT p50/ p90 (s) | ITL mean (s) | ITL p50/ p90 (s) |
|---|---:|---:|---:|---:|---:|---:|---:|
| precise-prefix-cache | 63.9 | 4.340 | 100.00% | 35.035 | 41.907/49.855 | 0.160 | 0.0001/0.379 |
| estimate-prefix-cache | 58.6 | 3.960 | 100.00% | 37.995 | 43.164/49.018 | 0.168 | 0.0001/0.378 |
| random-routing | 54.2 | 3.965 | 97.17% | 38.846 | 38.289/59.042 | 0.169 | 0.0001/0.378 |
| load | 55.1 | 3.723 | 100.00% | 41.399 | 48.498/50.922 | 0.169 | 0.0001/0.377 |

### QPS = 15.0


| Experiment | Output toks/s | Requests/s | Success Rate | TTFT mean (s) | TTFT p50/ p90 (s) | ITL mean (s) | ITL p50/ p90 (s) |
|---|---:|---:|---:|---:|---:|---:|---:|
| precise-prefix-cache | 60.4 | 3.866 | 100.00% | 38.792 | 41.938/49.783 | 0.169 | 0.0001/0.379 |
| random-routing | 51.7 | 3.741 | 93.33% | 38.873 | 40.217/56.338 | 0.170 | 0.0001/0.378 |
| load | 57.3 | 3.655 | 100.00% | 41.565 | 46.168/50.071 | 0.170 | 0.0001/0.378 |
| estimate-prefix-cache | 57.0 | 3.601 | 100.00% | 42.164 | 47.032/50.074 | 0.170 | 0.0001/0.378 |

### QPS = 20.0


| Experiment | Output toks/s | Requests/s | Success Rate | TTFT mean (s) | TTFT p50/ p90 (s) | ITL mean (s) | ITL p50/ p90 (s) |
|---|---:|---:|---:|---:|---:|---:|---:|
| precise-prefix-cache | 63.5 | 3.863 | 99.70% | 38.095 | 42.052/50.059 | 0.163 | 0.0001/0.379 |
| estimate-prefix-cache | 58.9 | 3.553 | 100.00% | 41.715 | 45.739/50.186 | 0.170 | 0.0001/0.378 |
| random-routing | 55.4 | 3.502 | 97.10% | 41.779 | 44.261/57.679 | 0.170 | 0.0001/0.378 |
| load | 57.6 | 3.449 | 100.00% | 43.130 | 47.832/50.548 | 0.171 | 0.0001/0.378 |

