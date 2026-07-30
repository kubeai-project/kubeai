# Multi-Spec Models

## User stories

### 1. Scale & route across heterogenous hardware.

> As an admin, I would like to be resiliant to resource stockouts.

> As an admin I would like to use the best engine for the hardware (ollama on CPU, vLLM on GPUs, and maybe jetstream on TPUs).

### 2. Expose a simplified model ID (i.e. `llama3.2-8b`), abstract hardware from end client.

> As an end user I want to be able to send a request to `llama3.2-8b` instead of (`llama3.2-8b-tpu-v5e`) and it should be routed to the right hardware.

### 3. Support lower performance standby instances to mitigate cold start latencies.

> As an end user, I would like a fast response, even during off hours.

Example: Keep 1x replica of Ollama running on CPUs, when traffic comes, scale up vLLM on GPUs.

### 4. Support simultaneous scale-up.

> As a user I would prefer that successful scale-up happens faster at the expense of temporarily scaling up too many replicas. I might have a lot of different types of GPUs to choose from, each of which is relatively likely to be out-of-stock at any given moment.

Example: Attempt to scale up 3 different types of GPUs (A, B, C) when we only need N+1 more replicas. If GPUs B & C come online, then keep B and quickly downscale C.

## Considerations

1. The replicas of each spec will need to be tracked.

It likely makes sense to switch over to managing Pods via Deployments where 1 Deployment is created for every 1 items in the Model's `.specs[]`.

2. Scaleup failures will need to be tracked to avoid attempting to scale up the same spec over-and-over.

Option A: Always loop through all specs, moving on to the next spec when a failure to scale occurs on the current one. Only keep track of the index, and loop back around to `i=0` when the `i==len(specs)-1`.

Option B: Track failures-per-spec and configure a backoff before retrying that spec.

A good place for this would likely be in the `.spec.conditions[]` field in the spec's Deployment or on the Model. Some sort of backoff strategy will be needed. The backoff duration should likely be informed by `len(.specs)` to ensure that all specs can be tried before returning back to the initial.

3. Allow for scaling across regions/clusters. - Future

## Proposed API

```yaml
kind: Model
metadata:
  name: llama-3.1
spec:
  # NOTE: Anything field set in .spec will apply to all .specs[].
  # If a field is set in both .spec and .specs[], the field set in .specs[] will take precedence.
  env:
    FOO: bar

  # NOTE: Some fields from .spec are not allowed in .specs[]
  # For instance:
  # .features (should apply to all)
  # .loadBalancing (should apply to all)
  features: [TextGeneration]
  loadBalancing:
    strategy: PrefixHash
    prefixHash:
      meanLoadPercentage: 125
      replication: 200
      prefixChatLength: 100

# NOTE: Scale up would be attempted in the order specified below:
specs:
- name: gpu-l4-70b
  url: hf://neuralmagic/Meta-Llama-3.1-70B-Instruct-FP8
  engine: VLLM
  args:
    - --max-model-len=32768
    - --max-num-batched-token=32768
    - --max-num-seqs=512
    - --gpu-memory-utilization=0.9
    - --pipeline-parallel-size=4
    - --tensor-parallel-size=2
    - --enable-prefix-caching
    - --enable-chunked-prefill=false
    - --disable-log-requests
    - --kv-cache-dtype=fp8
    - --enforce-eager
  env:
    VLLM_ATTENTION_BACKEND: FLASHINFER
  # NOTE: Target requests will be used to weight backends when load balancing
  # in addition to how it is used today (it is only considered for autoscaling today).
  targetRequests: 500
  resourceProfile: nvidia-gpu-l4:8
- name: tpu-v5e-8b
  url: hf://meta-llama/Meta-Llama-3.1-8B-Instruct
  engine: VLLM
  args:
    - --disable-log-requests
    - --swap-space=8
    - --tensor-parallel-size=4
    - --num-scheduler-steps=4
    - --max-model-len=8192
    - --distributed-executor-backend=ray
  resourceProfile: google-tpu-v5e-2x2:4
  # NOTE: Simulataneous scale-up could be supported via the addition of another field that
  # indicates that this spec should be scaled with the previous one.
  # TBD: Not sure on the field name yet, this should likely be a followup-feature, not a part
  # of the initial implementation.
  simultaneousScaleUpGroup: foo
- name: cpu-3b
  engine: OLlama
  url: ollama://llama-3.1-3b
  args:
  - ...
  resourceProfile: cpu:2
  # AVOIDING COLD STARTS #
  minReplicas: 1
  # Setting targetRequests to 0 means that this spec should receive 0 load when there is at least
  # one other replica.
  # This also means it is OK to scale this replica down to 0 when
  # there are other replicas running.
  # (And scale back up to minReplicas before scaling all others to 0).
  targetRequests: 0
  ########################
```