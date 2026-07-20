# Serve Multi-Node Models

This guide covers how to serve large models across multiple nodes using KubeAI's multi-node serving feature powered by [LeaderWorkerSet](https://github.com/kubernetes-sigs/lws) (LWS).

## Overview

Some models are too large to fit on a single GPU node. KubeAI supports distributing model inference across multiple nodes using vLLM's tensor-parallel and pipeline-parallel capabilities via [Ray](https://ray.io/).

When you specify a multi-node [resource profile](../concepts/resource-profiles.md), KubeAI creates a LeaderWorkerSet instead of bare Pods:

- **Head pod** — runs vLLM with Ray head, serves the OpenAI-compatible API
- **Worker pods** — join the Ray cluster and contribute GPU resources

Only head pods receive inference traffic; worker pods are excluded from load balancing.

## Prerequisites

- KubeAI installed (see [installation guides](../installation/any.md))
- [LeaderWorkerSet (LWS)](https://github.com/kubernetes-sigs/lws) controller installed in your cluster
- Multiple GPU nodes available
- `distributedInference` enabled in KubeAI system configuration (Helm value: `distributedInference: true`)

> [!WARNING]
> The prebuilt `vllm/vllm-openai` image does not include Ray integration required for multi-node serving.
> Build and use your own image with Ray installed.
>
> Example Dockerfile:
>
> ```dockerfile
> FROM vllm/vllm-openai
> RUN pip install "ray[cgraph]"
> ```

Install the LWS controller:
```bash
kubectl apply --server-side -f https://github.com/kubernetes-sigs/lws/releases/latest/download/manifests.yaml
```

## Resource Profile Format

Multi-node models use a 3-part resource profile format:

```
<profile-name>:<tensor-parallel>:<pipeline-parallel>
```

| Segment | Meaning |
|---------|---------|
| `profile-name` | The resource profile name (e.g., `nvidia-gpu-a100`) |
| `tensor-parallel` | Number of GPUs per node for tensor parallelism |
| `pipeline-parallel` | Number of nodes (group size) for pipeline parallelism |

For comparison, single-node models use the 2-part format `<name>:<count>`.

## Example: Serving a 70B Model Across 2 Nodes

Create a Model resource with a 3-part resource profile:

```yaml
apiVersion: kubeai.org/v1
kind: Model
metadata:
  name: llama-3.1-70b-instruct
spec:
  features: [TextGeneration]
  url: hf://meta-llama/Llama-3.1-70B-Instruct
  engine: VLLM
  args:
    - --max-model-len=4096
    - --gpu-memory-utilization=0.9
    - --disable-log-requests
    - --trust-remote-code
  # 4 GPUs per node (tensor-parallel), 2 nodes (pipeline-parallel)
  resourceProfile: nvidia-gpu-a100:4:2
  minReplicas: 1
```

This creates a LeaderWorkerSet with:
- **1 replica** (each replica is a group of 2 nodes)
- **Group size 2** (1 head + 1 worker)
- **4 GPUs per pod** (tensor-parallel-size=4)
- **2-way pipeline parallelism** across nodes

## How It Works

1. KubeAI parses the 3-part resource profile and creates a `LeaderWorkerSet` resource
2. The LWS controller creates pod groups, each with a head and `pipeline-parallel - 1` workers
3. The head pod starts vLLM with `--tensor-parallel-size` and `--pipeline-parallel-size` args
4. Worker pods start as Ray workers and connect to the head pod's Ray cluster
5. KubeAI's autoscaler manages replicas by scaling the LWS (each replica = one full multi-node group)

## Scaling

Autoscaling works the same as single-node models. When KubeAI scales the model:
- **Scale up** — the LWS controller creates additional pod groups (head + workers)
- **Scale down** — the LWS controller removes pod groups
- **Scale to zero** — the entire LWS is scaled to 0 replicas

## Limitations

- **Engine support** — Multi-node serving is currently only supported with the **VLLM** engine
- **Adapters** — LoRA adapters are not supported with multi-node models
- **Cache profiles** — Model cache profiles work with multi-node models, but each pod in the group needs access to the cached model

## Next

- Learn about [resource profiles](../concepts/resource-profiles.md)
- See [how to configure resource profiles](./configure-resource-profiles.md)
- Read about [autoscaling](../concepts/autoscaling.md)
