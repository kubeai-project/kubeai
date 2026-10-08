# API routes and migration

KubeAI exposes OpenAI-compatible endpoints under `/openai/v1`, and independent
inference extensions under `/v1`. All paths below are relative to the KubeAI
inference service, normally port 8000. The table shows public client URLs;
inference engines receive the corresponding `/v1/...` path.

| Operation | Public route | Compatibility route | Migration status |
| --- | --- | --- | --- |
| Chat completions | `POST /openai/v1/chat/completions` | — | Unchanged |
| Completions | `POST /openai/v1/completions` | — | Unchanged |
| Embeddings | `POST /openai/v1/embeddings` | — | Unchanged |
| Reranking | `POST /v1/rerank` | `POST /openai/v1/rerank` | Compatibility route deprecated |
| System One | `POST /v1/systemone` | `POST /openai/v1/systemone` | Compatibility route deprecated |
| Model discovery | `GET /v1/models` | `GET /openai/v1/models` | Both supported |
| Transcriptions | `POST /openai/v1/audio/transcriptions` | — | Unchanged |
| Responses | `POST /openai/v1/responses` | — | Unchanged |
| Anthropic Messages | `POST /v1/messages` | — | New |

Only these routes are exposed. There is no general forwarding of `/v1/*` or
`/openai/v1/*` to engines. Backend administration, Messages token counting and
message batch endpoints are not exposed by this change.

## Two-phase compatibility policy

The complete list of deprecated routes is:

- `POST /openai/v1/rerank`, replaced by `POST /v1/rerank`.
- `POST /openai/v1/systemone`, replaced by `POST /v1/systemone`.

No other route in the table is deprecated by this migration. In particular,
both `/openai/v1/models` and `/v1/models` remain supported.

Reranking and System One are inference extensions outside the standard OpenAI
API contract. Independent `/v1` routes make that distinction explicit and let
Anthropic clients use their native Messages path. OpenAI SDK routes retain their
existing prefix so their configured base URLs keep working.

In the release that introduces the new routes, both old and new rerank and
System One URLs work. The old routes invoke the same parsing, model lookup,
scaling, load balancing and proxy code as their replacements. Requests are
forwarded directly, without a redirect or a change to the response format.

The compatibility routes add these response headers, including on errors:

```http
Deprecation: @1791417600
Link: </v1/rerank>; rel="successor-version"
```

For System One the link points to `/v1/systemone`. The `Deprecation` value is
the structured date for 2026-10-08T00:00:00Z, following
[RFC 9745](https://www.rfc-editor.org/rfc/rfc9745.html). No calendar removal
date is announced with a `Sunset` header.

Removal of `/openai/v1/rerank` and `/openai/v1/systemone` is planned for the
release following the one that introduces their replacements. This change
implements the compatibility phase; it does not remove the old routes.
`/openai/v1/models` is not deprecated.

## Upgrade clients and ingress

1. Deploy the release supporting both route forms before changing clients.
2. If an ingress or API gateway currently allows only `/openai/`, also allow
   the four public `/v1/` routes in the table. Preserve the full path. Apply the
   same authentication, TLS, timeout and request-size policy to the new paths.
3. Change rerank clients from `/openai/v1/rerank` to `/v1/rerank`, and System One
   clients from `/openai/v1/systemone` to `/v1/systemone`. Keep their request
   payloads and model configuration unchanged.
4. Verify normal responses, backend validation errors, selectors and cold
   starts. Monitor traffic to the old paths before upgrading to their removal
   release. Keep clients on the old URLs if rolling back to an older KubeAI.

Keep OpenAI SDK `base_url="http://kubeai/openai/v1"` for chat, completions,
embeddings, transcriptions and Responses. Changing that base URL globally to
`http://kubeai/v1` would break those unchanged endpoints. Configure rerank and
System One URLs separately. Anthropic SDKs use the KubeAI service root as their
base URL; see [Anthropic Messages](../how-to/use-anthropic-messages.md).

## Model discovery now includes every feature

Both model-list URLs return all installed Models visible to the KubeAI client,
plus their configured adapters, using the existing OpenAI-style
`{"object":"list","data":[...]}` response. Each entry includes its `features`.
The default no longer restricts discovery to text generation. Models scaled to
zero are included; a listing is not a readiness check or a probe of which
backend routes are implemented.

```bash
# All model types
curl --fail-with-body http://localhost:8000/v1/models

# Chat-only clients should request the feature explicitly, on either URL
curl --fail-with-body \
  'http://localhost:8000/openai/v1/models?feature=TextGeneration'

# Multiple features form a union; entries are not duplicated
curl --fail-with-body \
  'http://localhost:8000/v1/models?feature=Reranking&feature=SystemOne'

# Restrict discovery to Models carrying a label
curl --fail-with-body http://localhost:8000/v1/models \
  -H 'X-Label-Selector: team=support'
```

Repeated feature parameters use OR semantics. Label selectors still restrict
the results, including when no feature parameter is present. An invalid label
selector returns 400; a successful empty listing returns `data: []`.
