# Inventory AI workloads with k8s-aibom

[k8s-aibom](https://github.com/GoogleCloudPlatform/k8s-aibom) is an
unprivileged Kubernetes controller that generates a
[CycloneDX 1.6 ML-BOM](https://cyclonedx.org/capabilities/mlbom/) for each
AI workload actually running in a cluster — the served model, the runtime,
and the container image digests, each attribute carrying an evidence locator
and a confidence tier. KubeAI can install it as an optional subchart.

It observes through the Kubernetes API only: no sidecars, no privileged
DaemonSet, no changes to the model server pods KubeAI creates.

## Enable the subchart

```bash
helm upgrade --install kubeai kubeai/kubeai \
  --set k8s-aibom.enabled=true \
  --reuse-values
```

## Opt in the namespace

Inventory is namespace-opt-in by design; nothing is recorded until a
namespace is labeled:

```bash
kubectl label namespace <kubeai-namespace> aibom.k8saibom.dev/enabled=true
```

## Read the inventory

Each model server Deployment gets an `AIBOM` resource:

```bash
kubectl get aiboms -n <kubeai-namespace>
```

The `kubectl aibom` plugin (`kubectl krew install aibom`) summarizes them:

```bash
kubectl aibom summary -n <kubeai-namespace>
```

KubeAI's vLLM and Ollama model servers are detected out of the box (runtime
confidence `inferred`, from the image; model identity `declared`, from the
server arguments). Documents can be shipped to external sinks (object
storage, webhook) for audit retention — see the
[k8s-aibom documentation](https://github.com/GoogleCloudPlatform/k8s-aibom#readme).
