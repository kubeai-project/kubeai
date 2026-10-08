# Route System One requests through KubeAI

KubeAI exposes `POST /v1/systemone` and forwards it to
`POST /v1/systemone` on a selected model server. This endpoint is an inference
extension alongside chat, embeddings and reranking.

Supported engine values are `OLlama`, `VLLM`, `LlamaCpp` and `SGLang`. This
integration assumes the selected server image already implements
`/v1/systemone`. KubeAI adds routing and access to its existing inference
lifecycle; it does not install an endpoint into the engine, add a wrapper,
change the model server launcher or translate between engine payload formats.

`POST /openai/v1/systemone` remains available during the migration and serves
the same backend endpoint. It is deprecated and planned for removal in the
release following the one that introduces `/v1/systemone`. See the
[API route migration](../reference/api-route-migration.md) before upgrading clients.

## Enable the feature on a Model

Install a KubeAI version and Model CRD containing the `SystemOne` feature.
Upgrade the controller and CRD together using your normal installation method;
an older CRD rejects `SystemOne` in `spec.features`.

For an existing Model, add `SystemOne` to its feature list, preserving the
features it already serves:

```yaml
spec:
  features: [TextGeneration, SystemOne]
```

Keep its existing `engine`, `url`, resource profile, image, arguments and
credentials. The image must serve the endpoint at the model Pod's configured
inference port. Declaring the feature enables KubeAI routing; it does not check
backend endpoint availability or change startup/readiness probes.

The following example uses vLLM and a small Hugging Face model. Choose a model,
image and resource profile suitable for your backend's System One implementation:

```yaml
apiVersion: kubeai.org/v1
kind: Model
metadata:
  name: ticket-decisions
  labels:
    team: support
spec:
  engine: VLLM
  features: [TextGeneration, SystemOne]
  url: hf://Qwen/Qwen2.5-0.5B-Instruct
  resourceProfile: nvidia-gpu-l4:1
  minReplicas: 1
  loadBalancing:
    strategy: LeastLoad
```

The engine and URL combinations below illustrate how to use the other existing
launchers. They are configuration examples, not a guarantee that the chart's
default images or the example models support a particular decision schema.

| Runtime | `spec.engine` | Example `spec.url` |
| --- | --- | --- |
| Ollama | `OLlama` | `ollama://qwen2.5:0.5b` |
| vLLM | `VLLM` | `hf://Qwen/Qwen2.5-0.5B-Instruct` |
| llama.cpp | `LlamaCpp` | `hf://Qwen/Qwen2.5-0.5B-Instruct-GGUF:Q4_K_M` |
| SGLang | `SGLang` | `hf://Qwen/Qwen2.5-0.5B-Instruct` |

Use the existing engine configuration for model formats, sources, hardware and
credentials. Set `spec.image` when you need an explicitly tested server image;
use a digest for reproducible deployments. Infinity and FasterWhisper are not
accepted by this endpoint.

## Discover and select models

List Models that advertise the feature:

```bash
curl --fail-with-body \
  'http://localhost:8000/v1/models?feature=SystemOne'
```

Without a feature query, both `/v1/models` and `/openai/v1/models` list all
installed Models and their adapters. Use `?feature=TextGeneration` for chat-only
discovery. Discovery includes Models scaled to zero; it does not probe backend
endpoint availability.
In the request, `model` must be the Model's Kubernetes `metadata.name`, such as
`ticket-decisions`. It is not a Hugging Face repository, an Ollama model tag or
an SDK's default model name. KubeAI forwards this value unchanged; the server's
existing model alias configuration must accept it.

The standard `X-Label-Selector` header restricts eligible Models. For example,
`X-Label-Selector: team=support` selects the Model above. A Model not found under
the supplied selectors returns 404.

Adapter selection through `model_adapter` or `model:adapter` is rejected.
System One forwarding preserves the original envelope and does not perform the
model-name rewriting used by other endpoints for adapters.

## Send a JSON request

KubeAI requires a JSON object with a non-empty string `model`. All other fields
are backend-defined. The example below illustrates a decision payload; confirm
its field names and question types against your selected engine implementation.

```bash
curl --fail-with-body http://localhost:8000/v1/systemone \
  -H 'Content-Type: application/json' \
  -H 'X-Label-Selector: team=support' \
  --data-binary '{
    "model": "ticket-decisions",
    "state": {"ticket": "The checkout is unavailable."},
    "questions": {
      "urgent": {"type": "noul", "instructions": "Does this need immediate attention?"},
      "team": {
        "type": "choice",
        "instructions": "Which team should investigate?",
        "criteria": {"billing": "Payment issues", "engineering": "Software failures"}
      }
    }
  }'
```

KubeAI validates the routing envelope, then forwards the original body bytes.
It preserves JSON key order, unknown fields, numeric representations and
engine-specific options. It does not validate questions, calculate decisions,
normalize probabilities or convert schemas between engines. Missing
`Content-Type` defaults to JSON; `application/json; charset=utf-8` is accepted.

Backend status codes, response bodies and end-to-end response headers pass
through the shared proxy. Backend validation failures therefore retain their
own response format rather than becoming KubeAI routing errors.

## Send multipart input

Use `multipart/form-data` only when the selected backend implements that format.
KubeAI expects exactly one text form field named `request`, containing the JSON
routing envelope. Put `model` inside that JSON field. A separate `model` form
field does not select the destination.

```bash
curl --fail-with-body http://localhost:8000/v1/systemone \
  -F 'request={"model":"ticket-decisions","state":"Inspect this image","questions":{"damaged":{"type":"noul","instructions":"Is the item damaged?"}}}' \
  -F 'image=@photo.png;type=image/png'
```

The remaining field names, file types and payload schema belong to the backend.
KubeAI preserves the multipart boundary, headers, binary file contents and part
order. The `request` field must be a form value, not an uploaded JSON file;
missing or repeated `request` fields are rejected before contacting the engine.

## Routing, limits and retry behavior

Requests use the existing model lookup, load balancing, scale-from-zero,
active-request metrics and cancellation lifecycle. A valid request scales the
selected Model to at least one replica and waits for an available address.
Rejected requests do not trigger scaling. Models can retain `minReplicas: 0`
when cold-start latency is acceptable.

Use `loadBalancing.strategy: LeastLoad` for these workloads. KubeAI does not
extract a text prefix from System One payloads; `PrefixHash` therefore has no
payload-derived prefix to distribute them.

The complete body is limited to **32 MiB (33,554,432 bytes)**, including multipart
boundaries, form values and files. KubeAI buffers the body to inspect the model
and replay it during retries. A chunked incoming request is forwarded with the
buffered body's known `Content-Length`.

The shared proxy's retry settings apply, including its default retry status
codes 500, 502, 503 and 504. A retry can repeat inference after a backend has
started work. This endpoint provides no exactly-once or deduplication guarantee.
Set client deadlines to accommodate cold starts, and coordinate client retry
policies with the proxy's retry configuration.

| Condition | HTTP status | Behavior |
| --- | --- | --- |
| Method other than POST | 405 | `Allow: POST`; no backend request |
| Invalid JSON or multipart routing envelope | 400 | No model scaling |
| Missing, blank or non-string `model` | 400 | No model scaling |
| Unsupported content type, engine, adapter or missing feature | 400 | No model scaling |
| Model not found, including selector mismatch | 404 | No model scaling |
| Body exceeds 32 MiB | 413 | No model lookup or scaling |
| Backend returns an error | Backend status | Response passes through, subject to retry settings |
| Backend does not expose `/v1/systemone` | Backend status, commonly 404 | No fallback to another inference API |
| Address acquisition deadline expires | 504 | Shared proxy releases request accounting |

KubeAI-generated proxy errors use an `error` field; internal 5xx error details
are logged rather than returned to callers. Client cancellation uses the shared
request context and releases active-request and load-balancer accounting.

## Verify a deployment

Before serving production traffic, check model discovery, JSON forwarding and
one backend-defined decision request against each image you deploy. Check
multipart input if the backend supports it, validation failures, an unavailable
route, scale-from-zero and a client timeout. Readiness of the model server alone
does not establish System One endpoint compatibility.

Use the normal inference ingress controls for authentication, TLS, timeouts,
request size and concurrency. This route does not add a separate authentication
service. Budget memory for concurrent buffered uploads and configure the ingress
body limit consistently with the proxy limit.

Automated tests cover the four accepted engines using HTTP mock servers,
including JSON/multipart byte preservation, chunked requests, retries, backend
errors, feature gating and request accounting. CRD admission tests cover the
new feature on all four engine values. These checks validate KubeAI integration;
they do not establish compatibility with a particular real engine image or GPU.
