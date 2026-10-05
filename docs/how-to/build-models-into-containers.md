# Build models into containers

In this guide we will preload a LLM into a custom built Ollama serving image. You can follow the same steps for other models and other serving engines.

Define some values
```bash
export MODEL_URL=ollama://qwen2:0.5b

# Customize with your own image repo.
export IMAGE=us-central1-docker.pkg.dev/substratus-dev/default/ollama-builtin-qwen2-05b:latest
```

Build and push image. Note: building (downloading base image & model) and pushing (uploading image & model) can take a while depending on the size of the model.

```bash
git clone https://github.com/kubeai-project/kubeai
cd ./kubeai/examples/ollama-builtin

docker build --build-arg MODEL_URL=$MODEL_URL -t $IMAGE .
docker push $IMAGE
```

Create a model manifest & apply into a cluster with KubeAI installed. NOTE: The only difference between an built-in model image and otherwise is the addition of the `image:` field.

```bash
kubectl apply -f - << EOF
apiVersion: kubeai.org/v1
kind: Model
metadata:
  name: builtin-model-example
spec:
  features: ["TextGeneration"]
  owner: alibaba
  image: $IMAGE # <-- The image with model built-in
  url: "$MODEL_URL?pull=false" # <-- ?pull=false to use pre-downloaded model (disable model pulling from Ollama)
  engine: OLlama
  resourceProfile: cpu:1
EOF
```
