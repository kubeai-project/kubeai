#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

PV_HOST_PATH=/tmp/model

model="distributed-qwen-500m-cpu"
mkdir -p ${PV_HOST_PATH}

# Execute into the kind container
kind_container=$(docker ps --filter "name=kind-control-plane" --format "{{.ID}}")
docker exec -i $kind_container bash -c "
  apt update -y && apt install -y python3-pip
  pip install -U "huggingface_hub" --break-system-packages
  mkdir -p ${PV_HOST_PATH}
  hf download Qwen/Qwen2-0.5B --local-dir ${PV_HOST_PATH}"

kubectl apply -f $REPO_DIR/test/e2e/engine-vllm-pvc/pv.yaml
kubectl apply -f $REPO_DIR/test/e2e/engine-vllm-pvc/pvc.yaml

sleep 5

kubectl apply -f $TEST_DIR/model.yaml

sleep 60

# The model controller should create a LeaderWorkerSet.
# VLLM does not support ray on cpu so we check only for the number of pods created, which should be 2 (1 leader and 1 worker).

pod_count=$(kubectl get pods -l app.kubernetes.io/instance=vllm-distributed-qwen-500m-cpu --no-headers 2>/dev/null | wc -l)
if [ "$pod_count" -lt 2 ]; then
  echo "Expected at least 2 pods, found $pod_count"
  exit 1
fi

