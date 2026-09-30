#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

models_release="kubeai-models"
model=stories15m-split-llamacpp-cpu

PV_HOST_PATH=/tmp/model

mkdir -p ${PV_HOST_PATH}

# A GGUF split across three shards. Only the first shard is named in the URL;
# llama.cpp derives the other two from that file name, which is why the whole
# directory has to be on the PVC.
kind_container=$(docker ps --filter "name=kind-control-plane" --format "{{.ID}}")
docker exec -i $kind_container bash -c "
  apt update -y && apt install -y python3-pip
  pip install -U 'huggingface_hub' --break-system-packages
  mkdir -p ${PV_HOST_PATH}
  hf download ggml-org/models-moved --local-dir ${PV_HOST_PATH} \
    --include 'tinyllamas/split/stories15M-0000*-of-00003.gguf'"

kubectl apply -f $REPO_DIR/test/e2e/engine-llamacpp-pvc/pv.yaml
kubectl apply -f $REPO_DIR/test/e2e/engine-llamacpp-pvc/pvc.yaml

apply_model $model

response_file=$TMP_DIR/completion.json
curl http://localhost:8000/openai/v1/completions \
  --max-time 600 \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'$model'",
    "prompt": "Once upon a time",
    "max_tokens": 20
  }' > $response_file

text=$(cat $response_file | jq -r '.choices[0].text')
if [ -z "$text" ] || [ "$text" == "null" ]; then
  echo "Empty completion, the sharded GGUF was probably not resolved"
  cat $response_file
  exit 1
fi

echo "Successfully served a sharded GGUF from a PVC: $text"
