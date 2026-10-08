package openaiserver

import (
	"encoding/json"
	"net/http"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/apiutils"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (h *Handler) getModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// List models based on the "feature" query parameter.
	// Example (default):  /v1/models
	// Example (single):   /v1/models?feature=TextEmbedding
	// Example (multiple): /v1/models?feature=TextGeneration&feature=TextEmbedding
	features := r.URL.Query()["feature"]

	var listOpts []client.ListOption
	headerSelectors := r.Header.Values("X-Label-Selector")
	for _, sel := range headerSelectors {
		parsedSel, err := labels.Parse(sel)
		if err != nil {
			sendErrorResponse(w, http.StatusBadRequest, "failed to parse label selector: %v", err)
			return
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: parsedSel})
	}

	var k8sModels []kubeaiv1.Model
	k8sModelNames := map[string]struct{}{}
	if len(features) == 0 {
		list := &kubeaiv1.ModelList{}
		if err := h.K8sClient.List(r.Context(), list, listOpts...); err != nil {
			sendErrorResponse(w, http.StatusInternalServerError, "failed to list models: %v", err)
			return
		}
		k8sModels = list.Items
	}
	for _, feature := range features {
		// NOTE: At time of writing an OR query is not supported with the
		// Kubernetes API server
		// so we just do multiple queries and merge the results.
		labelSelector := client.MatchingLabels{kubeaiv1.ModelFeatureLabelDomain + "/" + feature: "true"}
		list := &kubeaiv1.ModelList{}
		opts := append([]client.ListOption{labelSelector}, listOpts...)
		if err := h.K8sClient.List(r.Context(), list, opts...); err != nil {
			sendErrorResponse(w, http.StatusInternalServerError, "failed to list models: %v", err)
			return
		}
		for _, model := range list.Items {
			if _, ok := k8sModelNames[model.Name]; !ok {
				k8sModels = append(k8sModels, model)
				k8sModelNames[model.Name] = struct{}{}
			}
		}
	}

	models := make([]Model, 0)
	for _, k8sModel := range k8sModels {
		models = append(models, k8sModelToOpenAIModels(k8sModel)...)
	}

	// Wrapper struct to match the desired output format
	response := struct {
		Object string  `json:"object"`
		Data   []Model `json:"data"`
	}{
		Object: "list",
		Data:   models,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		sendErrorResponse(w, http.StatusInternalServerError, "failed to encode response: %v", err)
		return
	}
}

// Model is a struct that represents a model object
// from the OpenAI API.
type Model struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`

	// Adiditional (non-OpenAI) fields

	Features []kubeaiv1.ModelFeature `json:"features,omitempty"`
}

func k8sModelToOpenAIModels(k8sM kubeaiv1.Model) []Model {
	models := make([]Model, 1+len(k8sM.Spec.Adapters))
	models[0] = constructOpenAIModel(k8sM, "")
	for i, adapter := range k8sM.Spec.Adapters {
		models[i+1] = constructOpenAIModel(k8sM, adapter.Name)
	}
	return models
}

func constructOpenAIModel(k8sM kubeaiv1.Model, adapter string) Model {
	m := Model{}
	m.ID = apiutils.MergeModelAdapter(k8sM.Name, adapter)
	m.Created = k8sM.CreationTimestamp.Unix()
	m.Object = "model"
	m.OwnedBy = k8sM.Spec.Owner
	m.Features = k8sM.Spec.Features
	return m
}
