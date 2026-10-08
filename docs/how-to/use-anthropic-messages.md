# Use the Anthropic Messages API

KubeAI accepts `POST /v1/messages` and forwards it to the same path on the
selected model server. This lets clients using the Anthropic Messages wire
format reach locally hosted inference engines through KubeAI's model lookup,
scale-from-zero, load balancing and request accounting.

KubeAI does not translate Messages into OpenAI chat completions and does not
contact Anthropic's hosted service. Deploy an engine image that implements
`/v1/messages`, and test the capabilities your application requires. For
example, [vLLM documents its Anthropic-compatible API](https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html).
An OpenAI-compatible image alone does not establish Messages compatibility.

## Configure and discover a Model

Use an existing Model with `TextGeneration` in `spec.features`. Keep the engine,
image, model source and server arguments appropriate for that engine's native
Messages implementation. There is no new CRD feature or engine launcher for
this endpoint. Declaring `TextGeneration` does not enable an engine route.

```yaml
apiVersion: kubeai.org/v1
kind: Model
metadata:
  name: assistant
  labels:
    team: support
spec:
  engine: VLLM
  features: [TextGeneration]
  url: hf://Qwen/Qwen2.5-0.5B-Instruct
  resourceProfile: nvidia-gpu-l4:1
  minReplicas: 1
```

This is a configuration example; select and pin a server image that supports
Messages and your model's chat template, tools and multimodal inputs. KubeAI
does not maintain a separate engine allowlist for Messages. The backend decides
which Messages capabilities it supports and validates its full request schema.

```bash
curl --fail-with-body \
  'http://localhost:8000/v1/models?feature=TextGeneration' \
  -H 'X-Label-Selector: team=support'
```

Use the returned KubeAI Model ID in the request's `model` field. Discovery uses
the existing OpenAI-style list format, not Anthropic's model-list format.
Without `feature`, both model-list URLs return all feature types. Availability
in discovery does not prove that an image supports `/v1/messages`.

## Send a request

```bash
curl --fail-with-body http://localhost:8000/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'X-Label-Selector: team=support' \
  --data-binary '{
    "model": "assistant",
    "max_tokens": 128,
    "system": "You are a helpful assistant.",
    "messages": [{"role": "user", "content": "Explain Kubernetes in one sentence."}]
  }'
```

KubeAI requires a non-empty string `model` and a valid JSON object. It checks
that the selected Model declares `TextGeneration`. The engine validates
`messages`, `max_tokens`, content blocks and all other parameters. KubeAI
preserves extension fields, system prompts, tool schemas, image blocks, thinking
options and cache-control fields. Sending a field through does not guarantee
that the selected backend supports it.

For a base Model the complete JSON body is forwarded byte-for-byte, retaining
schema key order for prompt caching. Adapter IDs use the existing
`model_adapter` convention: KubeAI selects the base Model and adapter and
rewrites only the routing `model` value to the adapter name expected by the
backend. Other fields survive serialization, but byte order may change on
this adapter path. The engine must support adapters through Messages.

Headers such as `anthropic-version`, `anthropic-beta`, `x-api-key` and
`Authorization` are passed through subject to normal HTTP proxy handling.
KubeAI does not validate Anthropic API versions or use these headers to add
authentication. Supply credentials required by your ingress and backend;
deploy the new route behind the same authentication policy as existing routes.

## Use an Anthropic SDK

Anthropic SDKs append `/v1/messages` to the base URL. Set the base URL to the
KubeAI service root, without `/openai` or `/v1`:

```python
import anthropic

client = anthropic.Anthropic(
    base_url="http://localhost:8000",
    api_key="replace-with-your-gateway-or-backend-key",
)
message = client.messages.create(
    model="assistant",
    max_tokens=128,
    messages=[{"role": "user", "content": "Explain Kubernetes briefly."}],
    extra_headers={"X-Label-Selector": "team=support"},
)
print(message.content)
```

The SDK's token-counting, batch and model-list methods are outside the scope of
this route. `/v1/messages/count_tokens`, `/v1/messages/batches` and
`/openai/v1/messages` are not exposed. The
[official Messages reference](https://platform.claude.com/docs/en/api/messages/create)
describes the wire format; actual feature support depends on your local engine.

## Streaming and routing

Set `"stream": true` to request backend streaming. KubeAI forwards the backend's
`text/event-stream` response, including named events and their JSON payloads,
without translating them into OpenAI chunks. Events are flushed as they arrive.

```bash
curl --no-buffer --fail-with-body http://localhost:8000/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  --data-binary '{"model":"assistant","max_tokens":128,"stream":true,
    "messages":[{"role":"user","content":"Tell me a short story."}]}'
```

Disable response buffering in an ingress when necessary and set timeouts for
cold starts and generation. The
[Anthropic streaming documentation](https://platform.claude.com/docs/en/build-with-claude/streaming)
explains the event format. KubeAI does not resume interrupted streams.

`LeastLoad` uses normal in-flight request accounting. `PrefixHash` uses text
from the first user message, including text content blocks; it ignores image
and tool-result blocks. Inspection never modifies the forwarded body. Prompts
without user text have an empty routing prefix.

## Limits, retries and errors

The complete Messages body is limited to **32 MiB (33,554,432 bytes)**. KubeAI
buffers it to inspect the routing envelope and replay requests during retries.
JSON is the only accepted media type; an omitted `Content-Type` is treated as
JSON. Chunked input is forwarded with its buffered `Content-Length`.

The existing proxy retries configured retry statuses and transport failures
before response delivery. Default retry statuses are 500, 502, 503 and 504.
Retries can repeat inference and combine with SDK retries; configure client
deadlines and retry policies accordingly. An error occurring after streamed
bytes have been delivered cannot be retried as a new response to the client.

| Condition | Status | Result |
| --- | --- | --- |
| Method other than POST | 405 | `Allow: POST`; no model scaling |
| Invalid JSON, missing/blank/non-string model or unsupported media type | 400 | No model scaling |
| Model lacks `TextGeneration` | 400 | No model scaling |
| Model or adapter not found, including selector mismatch | 404 | No model scaling |
| Body exceeds 32 MiB | 413 | No model lookup or scaling |
| Model scaling fails | 500 | Internal details are logged |
| Deadline expires while waiting for an address | 504 | Request accounting is released |
| Backend returns a response | Backend status | Body and headers pass through, subject to retries |
| Backend has no Messages route | Backend status, commonly 404 | No conversion or fallback to chat |

KubeAI-generated Messages errors use the Anthropic envelope:

```json
{"type":"error","error":{"type":"invalid_request_error","message":"..."}}
```

Generated 404 errors use `not_found_error`, 413 uses `request_too_large`, and
5xx uses `api_error`, with internal details hidden. Backend responses are not
reformatted. Client cancellation propagates to the backend and releases active
request accounting. See the
[Anthropic error format](https://platform.claude.com/docs/en/api/errors).

## Verify your deployment

Check a normal request, tools and images if used, and a streamed response with
the exact engine image you deploy. Verify model selectors, cold starts,
cancellation and unsupported parameters. KubeAI's automated tests use HTTP
mock backends to validate forwarding, adapter rewriting, errors, retries and
incremental streaming; they do not validate real model generation or GPU images.
