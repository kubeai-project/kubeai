#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

models_release="kubeai-models"

PV_HOST_PATH=/tmp/model

mkdir -p ${PV_HOST_PATH}

# Execute into the kind container
kind_container=$(docker ps --filter "name=kind-control-plane" --format "{{.ID}}")
docker exec -i $kind_container bash -c "
  apt update -y && apt install -y python3-pip
  pip install -U "huggingface_hub" --break-system-packages
  mkdir -p ${PV_HOST_PATH}
  hf download facebook/opt-125m --local-dir ${PV_HOST_PATH} \
    --exclude 'tf_model.h5' --exclude 'flax_model.msgpack'"

kubectl apply -f $REPO_DIR/test/e2e/engine-vllm-pvc/pv.yaml
kubectl apply -f $REPO_DIR/test/e2e/engine-vllm-pvc/pvc.yaml

helm install $models_release $REPO_DIR/charts/models -f - <<EOF
catalog:
  opt-125m-cpu:
    enabled: true
    url: pvc://model-pvc
    minReplicas: 2
EOF

sleep 5

# There are 2 replicas so send 10 requests to ensure that both replicas are used.
for i in {1..10}; do
  echo "Sending request $i"
  curl http://localhost:8000/openai/v1/completions \
    --max-time 600 \
    -H "Content-Type: application/json" \
    -d '{"model": "opt-125m-cpu", "prompt": "Who was the first president of the United States?", "max_tokens": 40}'
done
