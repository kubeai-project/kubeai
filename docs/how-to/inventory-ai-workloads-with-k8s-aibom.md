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

Each supported workload gets an `AIBOM` resource. Today that covers
Deployments, StatefulSets and the operator CRDs listed in the
[k8s-aibom README](https://github.com/GoogleCloudPlatform/k8s-aibom#what-it-observes).
KubeAI's own `Model` resources, whose Pods KubeAI creates directly, are
not yet covered: that is tracked in
[GoogleCloudPlatform/k8s-aibom#172](https://github.com/GoogleCloudPlatform/k8s-aibom/issues/172)
and arrives with k8s-aibom v1.7. Until then, enabling the subchart
inventories the other AI workloads in the opted-in namespace, not
KubeAI-managed models.

```bash
kubectl get aiboms -n <kubeai-namespace>
```

The `kubectl aibom` plugin (`kubectl krew install aibom`) summarizes them:

```bash
kubectl aibom summary -n <kubeai-namespace>
```

Once `Model` support lands, KubeAI's vLLM and Ollama model servers will be
reported with the runtime and model identity `declared` from the `Model`
spec and image digests from the running Pods. Documents can be shipped to external sinks (object
storage, webhook) for audit retention — see the
[k8s-aibom documentation](https://github.com/GoogleCloudPlatform/k8s-aibom#readme).
