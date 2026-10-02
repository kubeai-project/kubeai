#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

model="smollm2-135m-cpu"
artifact="localhost:5000/smollm2-135m:modelpack"

kubectl apply -f $TEST_DIR/llmman.yaml
kubectl rollout status deployment/llmman --timeout=5m

# Publish a ModelPack artifact to the registry beside the daemon.
kubectl exec deploy/llmman -c llmman -- \
  llmman transfer hf.co/HuggingFaceTB/SmolLM2-135M-Instruct $artifact

kubectl apply -f $TEST_DIR/model.yaml
kubectl wait --timeout=10m --for=jsonpath='{.status.replicas.ready}'=1 model/$model

pod=$(kubectl get pods -l model=$model -o jsonpath='{.items[0].metadata.name}')

# pull: the init container pulled through the daemon, resolved, and copied.
puller_logs=$(kubectl logs $pod -c model-puller)
echo "$puller_logs"
grep -q "Placed $artifact at /model" <<<"$puller_logs"

# copy: the files are where the server reads them, and not an image volume.
kubectl exec $pod -c server -- ls /model | grep -x config.json
kubectl exec $pod -c server -- ls /model | grep -x model.safetensors
kubectl get pod $pod -o json | jq -e '[.spec.volumes[] | select(.name == "model") | has("image")] == [false]'

# The model serves.
for i in {1..3}; do
  echo "Sending request $i"
  curl --fail http://localhost:8000/openai/v1/completions \
    --max-time 600 \
    -H "Content-Type: application/json" \
    -d '{"model": "smollm2-135m-cpu", "prompt": "Who was the first president of the United States?", "max_tokens": 40}'
done
