#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

model=bge-embed-text-llamacpp-cpu

apply_model $model

# /v1/embeddings only answers when llama-server was started with --embedding,
# which the engine derives from the TextEmbedding feature.
response_file=$TMP_DIR/embedding.json
curl http://localhost:8000/openai/v1/embeddings \
  --max-time 600 \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Hello world",
    "model": "'$model'"
  }' > $response_file

embedding_length=$(cat $response_file | jq '.data[0].embedding | length')
if [ "$embedding_length" -ne 384 ]; then
  echo "Unexpected embedding dimension: got $embedding_length, expected 384"
  cat $response_file
  exit 1
fi

echo "Successfully generated embedding with $embedding_length dimensions"
