# Configure classification models

KubeAI supports text classification models via the Infinity engine.
Requests to `POST /v1/classify` are forwarded unchanged to the `/v1/classify`
endpoint of the model server.

## Install a classification model using Infinity

Create a file named `kubeai-classification-models.yaml` with the following content:

```yaml
catalog:
  go-emotions-cpu:
    enabled: true
    features: ["TextClassification"]
    owner: samlowe
    url: "hf://SamLowe/roberta-base-go_emotions"
    engine: Infinity
    resourceProfile: cpu:1
    minReplicas: 1
```

Apply the kubeai-models helm chart:

```bash
helm upgrade --install --reuse-values kubeai-models kubeai/models -f ./kubeai-classification-models.yaml
```

Once the pod is ready, forward the KubeAI service to `localhost:8000`:

```bash
kubectl port-forward svc/kubeai 8000:80
```

Then call the classify endpoint:

```python
import requests
resp = requests.post(
    "http://localhost:8000/v1/classify",
    json={
        "model": "go-emotions-cpu",
        "input": ["I am so happy today", "This is terrible"],
    },
)
print(resp.json())
```

The response contains, for every input, a list of labels with their scores.
