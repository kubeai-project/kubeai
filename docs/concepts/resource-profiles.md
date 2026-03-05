# Resource Profiles

A resource profile maps a type of compute resource (i.e. NVIDIA L4 GPU) to a collection of Kubernetes settings that are configured on inference server Pods. These profiles are defined in the KubeAI `config.yaml` file (via a ConfigMap). Each model specifies the resource profile that it requires.

## Single-Node Format

Kubernetes Model resources specify a resource profile and the count of that resource that they require (for example `resourceProfile: nvidia-gpu-l4:2` - 2x L4 GPUs).

## Multi-Node Format

For models that are too large to fit on a single node, KubeAI supports a 3-part resource profile format that enables multi-node serving via [LeaderWorkerSet](https://github.com/kubernetes-sigs/lws):

```
<profile-name>:<tensor-parallel-size>:<pipeline-parallel-size>
```

For example, `resourceProfile: nvidia-gpu-a100:4:2` means 4 GPUs per node with 2-way pipeline parallelism across nodes. Multi-node resource profiles are only supported with the **VLLM** engine.

See [how to serve multi-node models](../how-to/serve-multi-node-models.md) for a full guide.

## Profile Settings

A given profile might need to contain slightly different settings based on the cluster/cloud that KubeAI is deployed in.

Example: A resource profile named `nvidia-gpu-l4` might contain the following node selectors when installing KubeAI on a GKE Kubernetes cluster:

```yaml
cloud.google.com/gke-accelerator: "nvidia-l4"
cloud.google.com/gke-spot: "true"
```

and add the following resource requests to the model server Pods:

```yaml
nvidia.com/gpu: "1"
```

In addition to node selectors and resource requirements, a resource profile may optionally specify an image name. This name maps to the container image that will be selected when serving a model on that resource.

## Next

Read about [how to configure resource profiles](../how-to/configure-resource-profiles.md).