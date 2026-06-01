#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

model="opt-125m-cpu"

kubectl apply -f $TEST_DIR/model.yaml
kubectl wait --timeout=5m --for=jsonpath='{.status.replicas.ready}'=1 model/$model

sleep 5

# There are 1 replicas so send 10 requests to ensure that both replicas are used.
for i in {1..5}; do
  echo "Sending request $i"
  curl http://localhost:8000/openai/v1/completions \
    --max-time 600 \
    -H "Content-Type: application/json" \
    -d '{"model": "opt-125m-cpu", "prompt": "Who was the first president of the United States?", "max_tokens": 40}'
done
