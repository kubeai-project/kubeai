#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

model_name="qwen2.5-500m-cpu"
adapter_name="wiki"

kubectl apply -f $REPO_DIR/test/e2e/lora-adapter/model.yaml
kubectl wait --timeout=5m --for=jsonpath='{.status.replicas.ready}'=1 model/${model_name}
echo "Testing base model..."
# Use a timeout with curl to ensure that the test fails and all
# debugging information is printed if the request takes too long.
curl http://localhost:8000/openai/v1/completions \
  --max-time 900 \
  -H "Content-Type: application/json" \
  -d '{"model": "qwen2.5-500m-cpu", "prompt": "Who was the first president of the United States?", "max_tokens": 40}'

check_adapter() {
  kubectl get pod -l "adapter.kubeai.org/${adapter_name}" | grep -q "${model_name}"
}

retry 120 check_adapter

# Verify that the rollout was successful
echo "Testing adapter"

# Also check that the adapter is actually available by making a request
echo "Making a request to verify the model is available..."
curl http://localhost:8000/openai/v1/completions \
  --max-time 900 \
  -H "Content-Type: application/json" \
  -d '{"model": "qwen2.5-500m-cpu_wiki", "prompt": "Who was the first president of the United States?", "max_tokens": 40}'
