#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

model=qwen2-500m-instruct-llamacpp-cpu

apply_model $model

# The GGUF is downloaded by llama-server itself from the "-hf" reference, so
# this also covers the LLAMA_CACHE volume.
response_file=$TMP_DIR/completion.json
curl http://localhost:8000/openai/v1/chat/completions \
  --max-time 600 \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'$model'",
    "messages": [{"role": "user", "content": "Who was the first president of the United States?"}],
    "max_tokens": 40
  }' > $response_file

content=$(cat $response_file | jq -r '.choices[0].message.content')
if [ -z "$content" ] || [ "$content" == "null" ]; then
  echo "Empty completion"
  cat $response_file
  exit 1
fi

echo "Successfully generated a completion: $content"

# The engine adds --metrics, so the endpoint has to serve llama.cpp metrics.
pod=$(kubectl get pod -l model=$model -o jsonpath='{.items[0].metadata.name}')
if ! kubectl exec $pod -c server -- curl -sS http://localhost:8000/metrics | grep -q '^llamacpp:'; then
  echo "No llamacpp: metrics exposed"
  exit 1
fi

echo "Successfully scraped llamacpp: metrics"
