# External Load Balancer

## Problem

The built-in Go proxy and load balancer are deeply coupled to the controller-runtime manager. Users who want to use Envoy, Istio, or the Kubernetes Gateway API for traffic routing cannot opt out of the built-in proxy without disabling KubeAI's core functionality.

As described in [Issue #430](https://github.com/kubeai-project/kubeai/issues/430), KubeAI already has two logically independent components:

1. **A model proxy**: OpenAI-compatible API with prefix-aware load balancing (CHWBL), request queueing (scale-from-zero), and retries.
2. **A model operator**: manages backend Pods, downloads models, mounts volumes, loads LoRA adapters.

Both are colocated in the same deployment for simplicity. They integrate via the Kubernetes API, and it would already be possible to deploy one without the other. This proposal formalizes that separation.

While working within the following constraints:

* KubeAI should remain installable with a single command when using the built-in proxy
* KubeAI should not require external dependencies for basic functionality
* Users with existing infrastructure (Envoy, Gateway API) should be able to leverage it

## Solution

Add a `proxy.mode` setting to KubeAI's system config. The default (`internal`) preserves current behavior. A new `external` mode disables the built-in proxy, and the operator continues running independently, creating Kubernetes-native resources (headless Services) that any external load balancer can discover.

### KubeAI Config

```yaml
proxy:
  # "internal" (default): current built-in Go proxy with CHWBL.
  # "external": disable built-in proxy, create headless Services per Model.
  mode: internal
```

### Example: Internal Mode (Default, No Change)

```
Client -> KubeAI Proxy (:8000) -> vLLM Pods
```

### Example: External Mode

```
Client -> Envoy / Gateway API -> headless Service -> vLLM Pods
                                       |
                               KubeAI reconciles
```

When `mode=external`:

* The `modelproxy.Handler` HTTP server on `:8000` is **not started**.
* The `LoadBalancer` controller is **not registered** with the manager.
* The `ModelReconciler` creates a **headless `Service`** per Model, pointing to ready Pods.
* Autoscaling is **disabled** (see [Autoscaler in External Mode](#3-autoscaler-in-external-mode) for rationale).

---

## Design Decisions

### 1. Headless Service Ownership & Lifecycle

The `Model` CR owns the headless Service. Since each Model has its own headless Service, this is a natural 1:1 ownership relationship via standard Kubernetes `ownerReferences`.

The `Model` CRD is namespace-scoped, so model names are unique within a namespace. This guarantees no Service name collisions. To add an extra layer of safety for cross-namespace scenarios, the Service name includes a suffix derived from the Model's UID.

* **Creation**: When `mode=external`, `ModelReconciler.Reconcile()` ensures a headless Service exists for each Model, in the same namespace. The Service uses `clusterIP: None` and a selector matching both `kubeai.org/model: <model-name>` and `kubeai.org/model-uid: <model-uid>` to guarantee uniqueness.
* **Update**: On each reconciliation, the Service selector and ports are reconciled to match the current Model spec.
* **Deletion**: Kubernetes garbage collection handles cleanup when the Model CR is deleted, since the Service has an `ownerReference` pointing to the Model.
* **Name convention**: `svc/<model-name>-<uid-prefix>`, e.g., `llama-3-1-8b-instruct-a1b2c3`.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: llama-3-1-8b-instruct-a1b2c3
  ownerReferences:
    - apiVersion: kubeai.io/v1
      kind: Model
      name: llama-3-1-8b-instruct
      uid: a1b2c3d4-e5f6-...
spec:
  clusterIP: None
  selector:
    kubeai.org/model: llama-3-1-8b-instruct
    kubeai.org/model-uid: a1b2c3d4-e5f6-...
  ports:
    - port: 8000
      targetPort: 8000
      protocol: TCP
      name: inference
```

### 2. Scale-From-Zero Without the Proxy

In `external` mode, scale-from-zero is **delegated to the external infrastructure**. Supported patterns:

1. **Gateway API Inference Extension** (recommended): The `InferencePool` spec supports `targetMinReplicas: 1` or the EPP can send a signal. KubeAI's bridge controller (from the [Gateway API Inference Extension proposal](./gateway-api-inference-extension.md)) sets the `InferenceModel.spec.minReplicas` to match the KubeAI Model's `minReplicas`.

2. **Prometheus-based autoscaler** (KEDA/HPA): Users configure KEDA or HPA with a query like `sum(vllm:num_requests_running{model="<name>"}) > 0` to scale from zero.

Since autoscaling is disabled in `external` mode and fully delegated to external components, KubeAI does not enforce `minReplicas` constraints. If a Model has `minReplicas: 0`, KubeAI logs a warning indicating that scale-from-zero will only work if the external infrastructure handles it, but it does **not** reject the Model. This avoids imposing Kubernetes anti-patterns (rejecting valid CRDs based on a system-level config flag) and keeps the operator permissive.

> [!NOTE]
> When `proxy.mode=external`, KubeAI logs a warning for Models with `minReplicas: 0` but does not reject them. The responsibility for scale-from-zero is entirely on the external infrastructure.

### 3. Autoscaler in External Mode

**Current flow**: The proxy handler (`handler.go`) increments/decrements the `kubeai.inference.requests.active` OTel counter per request. This is exposed as a Prometheus gauge on KubeAI's `:8080/metrics` endpoint. The autoscaler (`metrics.go`) discovers all KubeAI Pod IPs via the `LoadBalancer` controller's `GetSelfIPs()`, scrapes each Pod's `/metrics`, and aggregates `active_requests_by_model` to make scaling decisions.

> [!NOTE]
> The `LoadBalancer` controller itself does **not** expose metrics. It only maintains in-flight request counts internally for the CHWBL algorithm. The autoscaler's metrics come entirely from the proxy's OTel counters.

**Decision**: In `external` mode, since the proxy is not running, there are no `kubeai.inference.requests.active` metrics to scrape. Rather than introducing a large `MetricsSource` interface refactoring, we take the simpler approach:

* **Autoscaling is disabled** when `mode=external`. The autoscaler goroutine is not started in `run.go`.
* Users are expected to use external autoscaling solutions (KEDA, HPA, or the Gateway API EPP's built-in scaling) that can scrape metrics directly from the vLLM Pods or the external load balancer.
* A future enhancement (Phase 2+) could add a lightweight vLLM-metric scraper, but this would be a separate proposal.

This keeps the change minimal and avoids touching the autoscaler internals.

### 4. Adapter Routing in External Mode

In `external` mode, adapter routing becomes the responsibility of the external load balancer:

* KubeAI continues to manage adapter loading on Pods (via `vllmclient`) and labels Pods with loaded adapters (`kubeai.org/adapter-<name>: "true"`).
* The headless Service per Model includes **all ready Pods** for that Model.
* How traffic is distributed across those Pods depends entirely on how the external LB is configured (round-robin, header-based routing, weighted subsets, etc.).
* External load balancers that support header-based routing (e.g., Envoy with header match to subset) can use the Pod labels to route to replicas with a specific adapter loaded.

> [!NOTE]
> Adapter-aware routing in `external` mode is a **Phase 2** feature. In Phase 1, all Pods for a Model are in the headless Service. This is acceptable because vLLM can serve any loaded adapter regardless of which Pod receives the request.

### 5. Configuration Validation

* **Switching modes requires a restart** of the KubeAI controller. This is consistent with all other `system.go` config changes.
* **Validation** in `System.DefaultAndValidate()`:
  * `proxy.mode` must be either `"internal"` or `"external"` (defaults to `"internal"` if empty).
  * When `mode=external`, `fixedSelfMetricAddrs` is ignored (unused).
  * When `mode=external`, Models with `minReplicas: 0` trigger a warning log (not rejection).

### 6. Interaction with Other Proposals

| Proposal | Interaction |
|---|---|
| [Engine Interface Refactor](./engine-interface-refactor.md) | No direct dependency. The `Engine` interface is orthogonal to proxy mode. The `DefaultPort()` method will be used by the headless Service reconciler. |
| [Gateway API Inference Extension](./gateway-api-inference-extension.md) | **Strong coupling**. `proxy.mode=external` + `gatewayAPI.enabled=true` is the "golden path" for external mode. The bridge controller replaces the headless Service with `InferenceModel`/`InferencePool` resources. |
| [Prefill/Decode Disaggregation](./prefill-decode-disaggregation.md) | In `external` mode with disaggregation, two headless Services per Model (`<model>-prefill`, `<model>-decode`). The external LB must be configured for two-phase routing. **Disaggregation in external mode is Phase 3**. |

---

## Implementation Phases

### Phase 1 (this proposal): Core External Mode
* Add `Proxy` config struct to `system.go`
* Conditionalize proxy/LB/autoscaler startup in `run.go`
* Add headless Service reconciliation in `ModelReconciler`
* Config validation and `minReplicas: 0` warning
* Add `SetupWithManager` ownership of `corev1.Service`

### Phase 2: Gateway API Bridge Integration
* Wire `proxy.mode=external` + `gatewayAPI.enabled=true`
* Replace headless Service with `InferenceModel`/`InferencePool` when Gateway API is enabled
* Adapter-aware routing via Gateway API
* Optional: lightweight vLLM-metric scraper for autoscaling in external mode

### Phase 3: Advanced External Features
* Disaggregated serving in external mode (dual headless Services)
* KEDA/HPA scale-from-zero integration guide


---

## References

* [KubeAI Issue #430: Independent Proxy Deployment](https://github.com/kubeai-project/kubeai/issues/430)
* [Kubernetes Gateway API Inference Extension](https://gateway-api-inference-extension.sigs.k8s.io/)
* [vLLM Metrics](https://docs.vllm.ai/en/latest/serving/metrics.html)
