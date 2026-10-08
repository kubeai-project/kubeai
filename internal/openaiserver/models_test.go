package openaiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	k8sv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type failingListClient struct{ client.Client }

func (c failingListClient) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return fmt.Errorf("private API details")
}

func TestModelDiscovery(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, k8sv1.AddToScheme(scheme))
	var objects []client.Object
	for _, feature := range []string{k8sv1.ModelFeatureTextGeneration, k8sv1.ModelFeatureTextEmbedding, k8sv1.ModelFeatureReranking, k8sv1.ModelFeatureSpeechToText, k8sv1.ModelFeatureSystemOne} {
		objects = append(objects, &k8sv1.Model{
			ObjectMeta: metav1.ObjectMeta{Name: feature, Namespace: "default", Labels: map[string]string{k8sv1.ModelFeatureLabelDomain + "/" + feature: "true", "team": "support"}},
			Spec:       k8sv1.ModelSpec{Features: []k8sv1.ModelFeature{k8sv1.ModelFeature(feature)}},
		})
	}
	// No feature labels: default discovery must not depend on controller labels.
	objects = append(objects, &k8sv1.Model{ObjectMeta: metav1.ObjectMeta{Name: "unlabelled", Namespace: "default"}, Spec: k8sv1.ModelSpec{Adapters: []k8sv1.Adapter{{Name: "tuned"}}}})
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	for _, path := range []string{"/v1/models", "/openai/v1/models"} {
		for _, tc := range []struct {
			name, query, selector string
			want                  []string
			status                int
			failing               bool
		}{
			{"all", "", "", []string{"TextGeneration", "TextEmbedding", "Reranking", "SpeechToText", "SystemOne", "unlabelled", "unlabelled_tuned"}, 200, false},
			{"chat", "?feature=TextGeneration", "", []string{"TextGeneration"}, 200, false},
			{"union deduplicated", "?feature=TextGeneration&feature=TextEmbedding&feature=TextGeneration", "", []string{"TextGeneration", "TextEmbedding"}, 200, false},
			{"labels without feature", "", "team=support", []string{"TextGeneration", "TextEmbedding", "Reranking", "SpeechToText", "SystemOne"}, 200, false},
			{"empty", "", "team=missing", []string{}, 200, false},
			{"unknown feature", "?feature=Unknown", "", []string{}, 200, false},
			{"bad selector", "", "team in (", nil, 400, false},
			{"API failure unfiltered", "", "", nil, 500, true},
			{"API failure filtered", "?feature=TextGeneration", "", nil, 500, true},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				var modelClient client.Client = c
				if tc.failing {
					modelClient = failingListClient{c}
				}
				h := NewHandler(modelClient, nil)
				r := httptest.NewRequest("GET", path+tc.query, nil)
				if tc.selector != "" {
					r.Header.Set("X-Label-Selector", tc.selector)
				}
				response := httptest.NewRecorder()
				h.ServeHTTP(response, r)
				require.Equal(t, tc.status, response.Code)
				require.Empty(t, response.Header().Get("Deprecation"))
				if tc.status != 200 {
					require.NotContains(t, response.Body.String(), "private API details")
					return
				}
				var result struct {
					Object string
					Data   []Model
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.Equal(t, "list", result.Object)
				require.NotNil(t, result.Data, "empty discovery must return [] rather than null")
				var ids []string
				for _, model := range result.Data {
					ids = append(ids, model.ID)
				}
				require.ElementsMatch(t, tc.want, ids)
			})
		}
	}
}
