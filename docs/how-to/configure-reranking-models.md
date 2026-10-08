# Configure reranking models

KubeAI supports reranking models via the Infinity and vLLM (Recommended for GPU) engine.

Use `POST /v1/rerank` for new integrations. The existing
`POST /openai/v1/rerank` remains operational during the migration, with
deprecation headers, and is planned for removal in the following release.
Both routes forward to the same `/v1/rerank` backend endpoint. See the
[API route migration](../reference/api-route-migration.md).

## Install BAAI/bge-reranker-base model using Infinity

Create a file named `kubeai-rerank-models.yaml` with the following content:

```yaml
catalog:
  bge-rerank-base-cpu:
    enabled: true
    features: ["Reranking"]
    owner: baai
    url: "hf://BAAI/bge-reranker-base"
    engine: Infinity
    #engine: VLLM
    resourceProfile: cpu:1
    # resourceProfile: nvidia-gpu-l4:1
    minReplicas: 1
```

Apply the kubeai-models helm chart:

```bash
helm install kubeai-models kubeai/models -f ./kubeai-rerank-models.yaml.yaml
```

Once the pod is ready, you can call the rerank endpoint:

```python
import requests
resp = requests.post(
    "http://localhost:8000/v1/rerank",
    json={
        "model": "bge-rerank-base-cpu",
        "query": "Which document talks about apples?",
        "documents": ["An apple a day keeps the doctor away", "Oranges are tasty"],
    },
)
print(resp.json())
```

