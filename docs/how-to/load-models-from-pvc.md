# Load Models from PVC

You can store your models in a Persistent Volume Claim (PVC) and let KubeAI use them for serving.
Both vLLM and Ollama engines support loading models from PVCs.

You must ensure the model files are already present in the PVC before creating the Model resource.
Alternatively you can use KubeAI's native caching mechanism which downloads the model for you:

- [Cache Models with GCP Filestore](./cache-models-with-gcp-filestore.md)
- [Cache Models with EFS](./cache-models-with-aws-efs.md)


## vLLM

For vLLM, use the following URL format:
```yaml
url: pvc://$PVC_NAME          # Loads the model from the PVC named $PVC_NAME
url: pvc://$PVC_NAME/$PATH    # Loads from a specific path within the PVC
```

### PVC requirements

vLLM supports both ReadWriteMany and ReadOnlyMany access modes. `Many` is used in order to support more than 1 vLLM replica.


## llama.cpp

The `LlamaCpp` engine needs a single GGUF file, not a directory. Point the URL at
the directory that holds the model and name the file with the `model` query
parameter:

```yaml
apiVersion: kubeai.org/v1
kind: Model
metadata:
  name: qwen2-500m-cpu
spec:
  features: [TextGeneration]
  url: pvc://model-pvc/models?model=qwen2-0_5b-instruct-q5_k_m.gguf
  engine: LlamaCpp
  resourceProfile: cpu:1
```

For a model split across shards, name the first shard. llama.cpp derives the
paths of the remaining shards from that file name, so all shards must sit in the
same directory:

```yaml
  url: pvc://model-pvc/models?model=model-00001-of-00003.gguf
```

A URL that names the file directly (`pvc://model-pvc/models/model.gguf`) also
works, because kubelet bind-mounts a single file at `/model`. Use it only for
unsharded models: the mount does not preserve the file name, so llama.cpp cannot
find sibling shards.

### PVC requirements

The PVC must contain the GGUF file. The access mode must allow the Pod to mount
it on the node the model is scheduled to.

## Ollama

For Ollama, use the following URL formats:
```yaml
url: pvc://$PVC_NAME?model=$MODEL_NAME    # Loads the model named $MODEL_NAME that's loaded on the disk
url: pvc://$PVC_NAME/$PATH?model=$MODEL_NAME
```

### PVC Requirements
Ollama requires using ReadWriteMany access mode because the rename operation `ollama cp` needs to write to the PVC.

### Example: Loading Qwen 0.5b from PVC

1. Create a PVC with ReadWriteMany named `model-pvc`. See [example](https://github.com/kubeai-project/kubeai/blob/main/examples/ollama-pvc/pvc.yaml).
2. Create a K8s Job to load the model onto `model-pvc. See [example](https://github.com/kubeai-project/kubeai/blob/main/examples/ollama-pvc/job.yaml)

    The PVC should now have a `blobs/` and `manifests/` directory after the loader completes.


3. Create a Model to load from PVC:
   
   ```yaml
   url: pvc://model-pvc?model=qwen:0.5b
   ```
