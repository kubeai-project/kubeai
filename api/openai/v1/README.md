# OpenAI API

The goal of these API structs is to define a standard API for interfacing with models using OpenAI-compatible APIs.

* JSON-to-struct-to-JSON round trip is not guaranteed. This package strives to preserve semantic meaning during JSON round trips.
* Use Go 1.27's `encoding/json/v2` and `encoding/json/jsontext` packages.
  * Preserve unknown members in a `jsontext.Value` field tagged `json:",embed"`.
  * `ChatCompletionRequest.MarshalJSON` preserves `null` for nil messages and `[]` for an explicitly empty slice.
* Extra fields at the root of requests/responses should be preserved (supports additional fields that engines like vLLM support - see [vLLM docs](https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html#extra-parameters)).

## API References

API References (useful for AI generation) were generated using:

```bash
wget https://raw.githubusercontent.com/openai/openai-openapi/refs/heads/main/openapi.yaml -O ./tmp/openaiapi.yaml

# Filter down to only the relevant components.
# This allows you to focus an AI coding assistant on
# a specific part of the API.
./hack/filter-openapi-components.py ./tmp/openaiapi.yaml /completions post -o ./api/openai/v1/reference/completions.openai.openapi.yaml
./hack/filter-openapi-components.py ./tmp/openaiapi.yaml /chat/completions post -o ./api/openai/v1/reference/chat_completions.openai.openapi.yaml
./hack/filter-openapi-components.py ./tmp/openaiapi.yaml /embeddings post -o ./api/openai/v1/reference/embeddings.openai.openapi.yaml
```

## Example Requests/Responses

This example script was generated from the OpenAI OpenAPI specs.

```bash
# To redact API keys from the output, pipe the script through sed:
redact_keys() {
  sed -E 's/Bearer [^"]*"/Bearer REDACTED"/g; s/(Bearer [^ ]*)/Bearer REDACTED/g'
}

# Ollama
OPENAI_API_KEY=placeholder OPENAI_API_BASE=http://localhost:11434 \
  COMPLETIONS_MODEL=qwen2:0.5b \
  CHAT_MODEL=qwen2:0.5b \
  EMBEDDINGS_MODEL=all-minilm \
  ./reference/example-requests.sh 2>&1 | redact_keys > ./reference/example-requests.ollama.output

# OpenAI
OPENAI_API_BASE=https://api.openai.com \
  COMPLETIONS_MODEL=gpt-3.5-turbo-instruct \
  CHAT_MODEL=gpt-4o-mini \
  EMBEDDINGS_MODEL=text-embedding-ada-002 \
  ./reference/example-requests.sh 2>&1 | redact_keys > ./reference/example-requests.openai.output

# vLLM
OPENAI_API_KEY=placeholder OPENAI_API_BASE=http://localhost:8000/openai \
  COMPLETIONS_MODEL=deepseek-r1-distill-llama-8b-l4 \
  CHAT_MODEL=deepseek-r1-distill-llama-8b-l4 \
  ./reference/example-requests.sh 2>&1 | redact_keys > ./reference/example-requests.vllm.output
```

Note: The redaction step above ensures no sensitive API keys are included in the output files.

## Concerns

When developing, pay special attention to the following:

- Zero-value types missmatching with default values in the API.
- Pointer types and optional types.
- Nullable types.

