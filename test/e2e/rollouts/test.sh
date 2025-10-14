#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

models_release="kubeai-models"

helm install $models_release $REPO_DIR/charts/models -f - <<EOF
catalog:
  deepseek-r1-1.5b-cpu:
    enabled: true
    features: [TextGeneration]
    url: 'ollama://deepseek-r1:1.5b'
    engine: OLlama
    minReplicas: 1
    resourceProfile: 'cpu:1'
  qwen2-500m-cpu:
    enabled: true
  nomic-embed-text-cpu:
    enabled: true
EOF

# Use a timeout with curl to ensure that the test fails and all
# debugging information is printed if the request takes too long.
curl http://localhost:8000/openai/v1/completions \
  --max-time 900 \
  -H "Content-Type: application/json" \
  -d '{"model": "deepseek-r1-1.5b-cpu", "prompt": "Who was the first president of the United States?", "max_tokens": 40}'


# Verify that the Model URL can be updated without requests failing.
DEEPSEEK_POD=$(kubectl get pod -l model=deepseek-r1-1.5b-cpu -o jsonpath='{.items[0].metadata.name}')
OLD_MODEL_URL="ollama://deepseek-r1:1.5b"
NEW_MODEL_URL="ollama://qwen2.5:0.5b"
OLD_MODEL_NAME=${OLD_MODEL_URL#ollama://}
NEW_MODEL_NAME=${NEW_MODEL_URL#ollama://}

kubectl patch model deepseek-r1-1.5b-cpu --type=merge -p "{\"spec\": {\"url\": \"$NEW_MODEL_URL\"}}"

check_pod_gone() {
  ! kubectl get pod $DEEPSEEK_POD | grep -q -E "Running|Terminating|Completed"
}

# Make a request to the model
make_request() {
  curl http://localhost:8000/openai/v1/completions \
    --max-time 900 \
    -H "Content-Type: application/json" \
    -d '{"model": "deepseek-r1-1.5b-cpu", "prompt": "Who was the first president of the United States?", "max_tokens": 40}'

  # Check if the old pod is gone
  check_pod_gone
}

retry 120 make_request

# Verify that the rollout was successful
echo "Verifying successful rollout..."

# List the new pods for the model
echo "Current pods for the model:"
kubectl get pods -l model=deepseek-r1-1.5b-cpu

# For Ollama models, the model URL is in the startup probe command, not in container args
NEW_POD=$(kubectl get pod -l model=deepseek-r1-1.5b-cpu -o jsonpath='{.items[0].metadata.name}')
STARTUP_PROBE_CMD=$(kubectl get pod $NEW_POD -o jsonpath='{.spec.containers[0].startupProbe.exec.command[2]}')
echo "Startup probe command for the new pod:"
echo "$STARTUP_PROBE_CMD"

# Verify that the new model URL is in the startup probe command
if ! echo "$STARTUP_PROBE_CMD" | grep -q "$NEW_MODEL_NAME"; then
  echo "❌ Rollout verification failed: New model name '$NEW_MODEL_NAME' not found in startup probe command"
  exit 1
fi

# Check that the old URL is no longer in use
if echo "$STARTUP_PROBE_CMD" | grep -q "$OLD_MODEL_NAME"; then
  echo "❌ Rollout verification failed: Old model name '$OLD_MODEL_NAME' still found in startup probe command"
  exit 1
fi

# Also check that the model is actually available by making a request
echo "Making a request to verify the model is available..."
curl http://localhost:8000/openai/v1/completions \
  --max-time 900 \
  -H "Content-Type: application/json" \
  -d '{"model": "deepseek-r1-1.5b-cpu", "prompt": "Who was the first president of the United States?", "max_tokens": 40}'
