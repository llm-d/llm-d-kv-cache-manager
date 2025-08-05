## Benchmarking Medium Scale Frequent Evictions

In this experiment, we evaluate the performance of the precise-prefix-cache-scorer (that is based on this project's `kvcache.Indexer`) in a
multi-user chat scenario, in which each user has a different chat history (prefix-hits are per user).

The total KVCache in the system holds about 160k tokens in the deployment of:
- 4 vLLM pods serving llama-3.1-70B-Instruct with TP=2
    - Each pod has a KVCache of 40k tokens

One cycle of users generates about 200k tokens in the system, which is about 1.25x the total cache size.

