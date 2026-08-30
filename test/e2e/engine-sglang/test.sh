#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

model=qwen2-500m-instruct-sglang-cpu

apply_model $model

response_file=$TMP_DIR/completion.json
curl http://localhost:8000/openai/v1/chat/completions \
  --max-time 900 \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'$model'",
    "messages": [{"role": "user", "content": "Who was the first president of the United States?"}],
    "max_tokens": 20
  }' > $response_file

content=$(cat $response_file | jq -r '.choices[0].message.content')
if [ -z "$content" ] || [ "$content" == "null" ]; then
  echo "Empty completion"
  cat $response_file
  exit 1
fi

echo "Successfully generated a completion: $content"
