# OpenAI API Compatibility

KubeAI provides an OpenAI API compatibility layer under `/openai/v1`.
The paths below are public KubeAI URLs. See the
[API routes and migration](api-route-migration.md) for the complete route table,
the explicit list of deprecated URLs and the compatibility policy.

## General:

### Models

```
GET /openai/v1/models
GET /v1/models
```

* Both routes list all installed `kind: Model` objects and their adapters,
  including embedding, reranking, speech and System One models, and Models
  scaled to zero. `/openai/v1/models` is not deprecated.
* Use `?feature=TextGeneration` for chat-only discovery. Repeated `feature`
  parameters form a union; `X-Label-Selector` restricts results.


## Inference

### Text Generation

```
POST /openai/v1/chat/completions
POST /openai/v1/completions
```

* Supported for Models with `.spec.features: ["TextGeneration"]`.

### Embeddings

```
POST /openai/v1/embeddings
```

* Supported for  Models with `.spec.features: ["TextEmbedding"]`.

### Reranking

```
POST /v1/rerank
```

* Supported for  Models with `.spec.features: ["Reranking"]`.
* `/openai/v1/rerank` remains operational but is deprecated; migrate to
  `/v1/rerank` before the following release.

### Speech-to-Text

```
POST /openai/v1/audio/transcriptions
```

* Supported for Models with `.spec.features: ["SpeechToText"]`.

### System One decisions (extension)

```text
POST /v1/systemone
```

* `/openai/v1/systemone` remains operational but is deprecated; migrate to
  `/v1/systemone` before the following release.
* Requires `.spec.features: ["SystemOne"]` and engine `OLlama`, `VLLM`,
  `LlamaCpp` or `SGLang`.
* The engine image must already serve `/v1/systemone` at its inference port.
* Requires a non-empty `model` string naming a KubeAI Model. Adapter selection
  is unsupported; other fields and responses are backend-defined.
* JSON and multipart bodies are preserved byte-for-byte. Multipart requests
  carry the JSON envelope in exactly one text form field named `request`.
* The complete body limit is 32 MiB. The existing proxy handles load balancing,
  scale-from-zero, retries and request accounting.
* See [Route System One requests](../how-to/configure-systemone-models.md) for
  configuration, discovery, examples, limitations and error behavior.

### Responses

```text
POST /openai/v1/responses
```

The existing route remains unchanged.

## Anthropic Messages

```text
POST /v1/messages
```

Requires `TextGeneration` and a backend implementing the native Messages API.
KubeAI preserves the Anthropic payload and streaming response; it does not
convert Messages to OpenAI chat. See
[Use the Anthropic Messages API](../how-to/use-anthropic-messages.md).

## OpenAI Client libraries
You can use the official OpenAI client libraries by setting the
`base_url` to the KubeAI endpoint.

For example, you can use the Python client like this:
```python
from openai import OpenAI
client = OpenAI(api_key="ignored",
                base_url="http://kubeai/openai/v1")
response = client.chat.completions.create(
  model="gemma2-2b-cpu",
  messages=[
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Who won the world series in 2020?"},
    {"role": "assistant", "content": "The Los Angeles Dodgers won the World Series in 2020."},
    {"role": "user", "content": "Where was it played?"}
  ]
)
```
