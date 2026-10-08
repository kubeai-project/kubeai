package openaiserver

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	k8sv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSystemOneModelDiscovery(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, k8sv1.AddToScheme(scheme))
	var objects []client.Object
	for i, engine := range []string{k8sv1.OLlamaEngine, k8sv1.VLLMEngine, k8sv1.LlamaCppEngine, k8sv1.SGLangEngine} {
		team := "support"
		if i == 3 {
			team = "other"
		}
		objects = append(objects, &k8sv1.Model{
			ObjectMeta: metav1.ObjectMeta{Name: "decision-" + engine, Namespace: "default", Labels: map[string]string{
				k8sv1.ModelFeatureLabelDomain + "/SystemOne": "true", "team": team,
			}},
			Spec: k8sv1.ModelSpec{Engine: engine, Features: []k8sv1.ModelFeature{k8sv1.ModelFeatureSystemOne}},
		})
	}
	objects = append(objects, &k8sv1.Model{
		ObjectMeta: metav1.ObjectMeta{Name: "chat", Namespace: "default", Labels: map[string]string{
			k8sv1.ModelFeatureLabelDomain + "/TextGeneration": "true",
		}},
		Spec: k8sv1.ModelSpec{Engine: k8sv1.VLLMEngine, Features: []k8sv1.ModelFeature{k8sv1.ModelFeatureTextGeneration}},
	})
	h := NewHandler(fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(), nil)
	for _, path := range []string{"/v1/models", "/openai/v1/models"} {
		for _, tc := range []struct {
			name, query, selector string
			want                  []string
		}{
			{"feature", "?feature=SystemOne", "", []string{"decision-OLlama", "decision-VLLM", "decision-LlamaCpp", "decision-SGLang"}},
			{"selector", "?feature=SystemOne", "team=support", []string{"decision-OLlama", "decision-VLLM", "decision-LlamaCpp"}},
			{"default", "", "", []string{"chat", "decision-OLlama", "decision-VLLM", "decision-LlamaCpp", "decision-SGLang"}},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				r := httptest.NewRequest("GET", path+tc.query, nil)
				if tc.selector != "" {
					r.Header.Set("X-Label-Selector", tc.selector)
				}
				response := httptest.NewRecorder()
				h.ServeHTTP(response, r)
				require.Equal(t, 200, response.Code)
				var result struct {
					Data []Model `json:"data"`
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				var ids []string
				for _, model := range result.Data {
					ids = append(ids, model.ID)
					if tc.query != "" {
						require.Contains(t, model.Features, k8sv1.ModelFeature(k8sv1.ModelFeatureSystemOne))
					}
				}
				require.ElementsMatch(t, tc.want, ids)
			})
		}
	}
}
