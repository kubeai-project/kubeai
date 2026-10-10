# Dynamic Resource Allocation (DRA) Support

## Problem

KubeAI allocates GPUs using the legacy device plugin model: `resources.limits["nvidia.com/gpu": "N"]`. This approach has hard limits that matter more as GPU clusters grow:

* **No MIG awareness**: NVIDIA MIG partitions (e.g., `1g.10gb`) cannot be expressed as simple integer limits. Users must pick the right node via `nodeSelector` manually.
* **No time-slicing control**: Kubernetes device plugins expose time-sliced GPUs as if they were real GPUs; there is no way to express priority or quota within a slice.
* **No cross-vendor composability**: A model that needs "1 GPU of at least 80GB VRAM" must be expressed as a vendor-specific resource name today. DRA enables abstract capacity requests.
* **No structured parameters**: Device plugins have no mechanism to pass per-allocation config (e.g., ECC mode, compute mode) to the driver. DRA `DeviceParameters` enables this.

Kubernetes 1.32 graduated DRA to stable for the `resource.k8s.io/v1beta1` API. Major GPU vendors (NVIDIA, AMD, Intel) are shipping DRA drivers alongside or replacing their device plugins.

## Solution

Extend `ResourceProfile` in the system config to support a `dra` mode. When a model's `resourceProfile` resolves to a DRA-backed profile, the controller patches the Pod spec to reference the appropriate `ResourceClaim`. Device plugin behavior remains the default when `dra` is absent.

Two paths are supported:

- **Template path (primary)**: The cluster admin pre-creates a `ResourceClaimTemplate`. The scheduler creates one `ResourceClaim` per Pod from the template automatically, giving each Pod exclusive GPU access. This is the standard pattern for workloads and what llm-d uses in production.
- **Shared claim path (secondary)**: The cluster admin pre-creates a single `ResourceClaim`. All Pods of a model share it. Used for MPS or time-slicing pools.

In both cases KubeAI does not create or delete `ResourceClaim` objects. The lifecycle is handled by the scheduler (template path) or is pre-managed by the admin (shared path). The controller only patches the Pod spec.

### System Config Change

```yaml
resourceProfiles:
  # Existing device-plugin profile (unchanged)
  nvidia-gpu-l4:
    imageName: "nvidia-gpu"
    requests:
      cpu: "6"
      memory: "24Gi"
    limits:
      nvidia.com/gpu: "1"
    tolerations:
      - key: nvidia.com/gpu
        value: present
        effect: NoSchedule
    nodeSelector:
      cloud.google.com/gke-accelerator: nvidia-l4

  # DRA profile exclusive per-Pod via ResourceClaimTemplate
  nvidia-gpu-h100-dra:
    imageName: "nvidia-gpu"
    requests:
      cpu: "12"
      memory: "80Gi"
    tolerations:
      - key: nvidia.com/gpu
        effect: NoSchedule
    dra:
      resourceClaimTemplateName: "nvidia-h100-exclusive"
      # claimName defaults to "gpu-claim"
      # claimRequest defaults to "gpu"

  # DRA profile shared claim for MPS / time-slicing
  nvidia-gpu-mps:
    imageName: "nvidia-gpu"
    requests:
      cpu: "4"
      memory: "20Gi"
    dra:
      resourceClaimName: "nvidia-mps-shared"
```

### Model CRD (No Change)

The `Model` CRD does not change. Users continue to specify:

```yaml
spec:
  resourceProfile: "nvidia-gpu-h100-dra:1"
```

### Pod Spec Produced

For a model using the template path, the controller patches each Pod as follows:

```yaml
spec:
  resourceClaims:
    - name: gpu-claim
      resourceClaimTemplateName: nvidia-h100-exclusive
  containers:
    - name: server
      resources:
        claims:
          - name: gpu-claim
            request: gpu
```

The scheduler creates one `ResourceClaim` per Pod from the template and deletes it when the Pod is deleted. KubeAI never touches `ResourceClaim` objects directly.

### `ResourceProfile` Go Type Change

```go
// internal/config/system.go

type ResourceProfile struct {
    // Existing fields (unchanged)
    ImageName     string              `json:"imageName"`
    Requests      corev1.ResourceList `json:"requests,omitempty"`
    Limits        corev1.ResourceList `json:"limits,omitempty"`
    NodeSelector  map[string]string   `json:"nodeSelector,omitempty"`
    Affinity      *corev1.Affinity    `json:"affinity,omitempty"`
    Tolerations   []corev1.Toleration `json:"tolerations,omitempty"`
    SchedulerName string              `json:"schedulerName,omitempty"`
    RuntimeClass  *string             `json:"runtimeClassName,omitempty"`

    // DRA mode. Mutually exclusive with GPU entries in Limits.
    DRA *DRAConfig `json:"dra,omitempty"`
}

type DRAConfig struct {
    // ResourceClaimTemplateName names a pre-created ResourceClaimTemplate.
    // The scheduler creates one ResourceClaim per Pod (exclusive allocation).
    // Mutually exclusive with ResourceClaimName.
    ResourceClaimTemplateName string `json:"resourceClaimTemplateName,omitempty"`

    // ResourceClaimName names a pre-created shared ResourceClaim.
    // All Pods of a model share this single claim (MPS / time-slicing pools).
    // Mutually exclusive with ResourceClaimTemplateName.
    ResourceClaimName string `json:"resourceClaimName,omitempty"`

    // ClaimName is the local name for this claim inside the Pod spec.
    // Defaults to "gpu-claim".
    ClaimName string `json:"claimName,omitempty"`

    // ClaimRequest is the request name within the claim.
    // Defaults to "gpu".
    ClaimRequest string `json:"claimRequest,omitempty"`
}
```

`DefaultAndValidate()` enforces:
- `resourceClaimTemplateName` and `resourceClaimName` are mutually exclusive.
- At least one must be set when `dra` is present.
- `dra` is mutually exclusive with GPU device plugin limits (any limit key containing "gpu").
- `claimName` defaults to `"gpu-claim"`; `claimRequest` defaults to `"gpu"`.

### Controller Changes

In `pod_plan.go`, after building the Pod via the engine builder:

```go
applyResourceClaims(podForModel, modelConfig.ResourceProfile.DRA)
```

`applyResourceClaims` (in `resource_claims.go`) is a no-op when `dra` is nil, preserving existing device plugin behavior. When `dra` is set it upserts into `pod.Spec.ResourceClaims` and `container.Resources.Claims` on the server container.

No new RBAC is needed. KubeAI does not create or delete `ResourceClaim` objects.

## Design Decisions

### Why not create ResourceClaim objects in the controller?

The `ResourceClaimTemplate` path is the standard Kubernetes pattern for workloads: the admin creates one template, and the scheduler handles per-Pod claim creation and deletion automatically. llm-d uses this pattern in production. Having KubeAI generate claims itself would require new RBAC, a watch on `ResourceClaim`, and reconciliation logic for allocation failures - significant complexity for no additional capability at this stage.

### Why not expose CEL device selectors in DRAConfig?

DRA driver attribute schemas (e.g. `device.attributes['memory']`) are vendor-specific and still stabilizing across NVIDIA, AMD, and Intel drivers. Coupling KubeAI config to those schemas today would create fragile, driver-version-sensitive configuration. The `ResourceClaimTemplate` indirection lets the admin write CEL expressions against their specific driver outside KubeAI. This can be revisited once schemas stabilize.

### Why mutually exclusive with GPU device plugin limits?

A Pod cannot meaningfully use both a DRA claim and a device plugin GPU limit for the same GPU. Allowing both would silently produce a Pod that requests two different allocation mechanisms for the same resource, likely causing scheduling failures. Failing fast at config validation is safer.

### Backwards compatibility

If `dra` is nil in the profile, behavior is identical to today. No existing models or configs break.

## Implementation

The following files were changed:

| File | Change |
|---|---|
| `internal/config/system.go` | Add `DRAConfig` struct to `ResourceProfile`; validation and defaulting in `DefaultAndValidate()` |
| `internal/config/system_test.go` | `TestDRAConfig` with 6 subtests covering both paths, defaults, and all error cases |
| `internal/modelcontroller/resource_claims.go` | `applyResourceClaims(pod, dra)` - patches Pod spec for both template and shared paths |
| `internal/modelcontroller/resource_claims_test.go` | Unit tests for nil no-op, template path, shared path, and idempotency |
| `internal/modelcontroller/pod_plan.go` | Call `applyResourceClaims` instead of per-model function |
| `api/k8s/v1/model_types.go` | Remove `ResourceClaimName` field (per-model approach, superseded) |
| `manifests/crds/kubeai.org_models.yaml` | Remove `resourceClaimName` from CRD |
| `charts/kubeai/templates/crds/kubeai.org_models.yaml` | Remove `resourceClaimName` from CRD |
| `charts/models/templates/models.yaml` | Remove `resourceClaimName` helm block |
| `charts/kubeai/values.yaml` | Add commented DRA profile examples |
| `docs/reference/kubernetes-api.md` | Remove `resourceClaimName` from ModelSpec table |

## Future Work

### Phase 2: Per-engine gating

Currently `applyResourceClaims` is called unconditionally for all engines. In practice, DRA profiles would only be configured for compatible engines (vLLM, Infinity), but there is no runtime enforcement. A clean solution requires adding a `SupportsDRA() bool` method to the `Engine` interface, which in turn depends on the [Engine Interface Refactor](./engine-interface-refactor.md). Without that refactor the alternative is an ad-hoc `switch` on `model.Spec.Engine` in `pod_plan.go`.

### Phase 3: Multi-device count

Respect the `:<N>` multiplier from `resourceProfile: "profile:N"` as a device count in the claim. This requires KubeAI to generate `ResourceClaim` objects directly per Pod (rather than just referencing a pre-created template), so it can set the `count` field. This also unlocks expressing partial GPU allocations (e.g. two MIG slices). Requires new RBAC (`resourceclaims` get/create/delete), a watch on `ResourceClaim` in the controller, and reconciliation for allocation failures.

### Phase 4: Structured device selectors

Expose `deviceSelectors` (CEL expressions) and `deviceConfig` in `DRAConfig` so the `ResourceClaim` KubeAI generates in Phase 3 can express precise constraints:

```yaml
dra:
  deviceClassName: "gpu.nvidia.com"
  selectors:
    - cel:
        expression: "device.attributes['memory'].isGreaterThan(quantity('79Gi'))"
  config:
    - opaque:
        driver: "gpu.nvidia.com"
        parameters:
          sharing:
            strategy: TimeSlicing
```

Deferred until DRA driver attribute schemas stabilize across NVIDIA, AMD, and Intel drivers. The `ResourceClaimTemplate` indirection in Phase 1 lets admins write these expressions outside KubeAI in the meantime.

## Relevant Reading

* [Kubernetes DRA Documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
* [NVIDIA DRA Driver](https://github.com/NVIDIA/k8s-dra-driver-gpu)
* [Intel Resource Drivers for Kubernetes](https://github.com/intel/intel-resource-drivers-for-kubernetes)
* [llm-d Gaudi DRA example](https://github.com/llm-d/llm-d/blob/main/guides/optimized-baseline/modelserver/hpu/vllm/resource-claim-template.yaml)
* [KubeAI Issue #639](https://github.com/substratusai/kubeai/issues/639)
* [KubeAI PR #642](https://github.com/substratusai/kubeai/pull/642)
* [resource.k8s.io/v1beta1 API Reference](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/resource-claim-v1beta1/)
