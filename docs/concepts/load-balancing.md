# Load Balancing

To optimize inference performance and resource utilization, KubeAI supports load balancing strategies specifically tailored for model inference servers such as vLLM. This document explains two primary load balancing strategies available in KubeAI: Least Load and Prefix Hash.

## Least Load

The Least Load strategy distributes inference requests to the model replica that has the least number of in-flight requests. This strategy aims to balance the inference workload evenly across available replicas, reducing the risk of overloading any single server.

## Prefix Hash

The Prefix Hash strategy leverages the <a target="_blank" href="https://research.google/blog/consistent-hashing-with-bounded-loads/">Consistent Hashing with With Bounded Loads</a> (CHWBL) algorithm to optimize the performance of engines such as vLLM that support prefix caching. This strategy increases the likelihood of KV cache hits for common prefixes. See <a target="_blank" href="https://docs.vllm.ai/en/latest/design/prefix_caching/#automatic-prefix-caching">vLLM prefix hashing docs</a> for more info.

With this strategy, KubeAI hashes incoming requests based on their prefixes (in addition to a requested LoRA adapter name - if present). Requests with the same hash value are routed to the same replica, except when that replica's in-flight requests exceed the overall average by a configurable percentage.

This strategy has the most benefit for use cases such as chat completion. This is because the entire chat thread is sent in each successive chat requests.

KubeAI supports this strategy for the following endpoints:

```
/openai/v1/completions
/openai/v1/chat/completions
```

## Next

See the [Kubernetes API docs](../reference/kubernetes-api.md) to view how to configure Model load balancing.