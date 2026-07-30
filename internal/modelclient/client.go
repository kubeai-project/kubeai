package modelclient

import (
	"context"
	"fmt"
	"sort"
	"sync"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var logger = ctrl.Log.WithName("modelclient")

type ModelClient struct {
	client client.Client
	// namespace is the operator's own namespace. It is used as a
	// disambiguation preference when multiple Models share a name across watched
	// namespaces: a match in this namespace wins over matches elsewhere.
	namespace                string
	consecutiveScaleDownsMtx sync.RWMutex
	consecutiveScaleDowns    map[string]int
}

func NewModelClient(client client.Client, namespace string) *ModelClient {
	return &ModelClient{client: client, namespace: namespace, consecutiveScaleDowns: map[string]int{}}
}

// LookupModel finds a Model by name across all watched namespaces and checks
// that it matches the given label selectors.
//
// When multiple Models share the same name across namespaces (this is possible
// because Model is namespace-scoped), a match in the operator's own namespace
// wins; otherwise the namespace that sorts first alphabetically is chosen and a
// warning is logged.
func (c *ModelClient) LookupModel(ctx context.Context, model, adapter string, labelSelectors []string) (*kubeaiv1.Model, error) {
	list := &kubeaiv1.ModelList{}
	if err := c.client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}

	var matches []kubeaiv1.Model
	for i := range list.Items {
		if list.Items[i].Name == model {
			matches = append(matches, list.Items[i])
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	m := pickModel(matches, c.namespace)

	modelLabels := m.GetLabels()
	if modelLabels == nil {
		modelLabels = map[string]string{}
	}
	for _, sel := range labelSelectors {
		parsedSel, err := labels.Parse(sel)
		if err != nil {
			return nil, fmt.Errorf("parse label selector: %w", err)
		}
		if !parsedSel.Matches(labels.Set(modelLabels)) {
			return nil, nil
		}
	}

	if adapter != "" {
		adapterFound := false
		for _, a := range m.Spec.Adapters {
			if a.Name == adapter {
				adapterFound = true
				break
			}
		}
		if !adapterFound {
			return nil, nil
		}
	}

	return m, nil
}

func (s *ModelClient) ListAllModels(ctx context.Context) ([]kubeaiv1.Model, error) {
	models := &kubeaiv1.ModelList{}
	if err := s.client.List(ctx, models); err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}

	return models.Items, nil
}

// pickModel selects a single Model from a set of same-named matches. A match in
// preferredNS wins; otherwise the alphabetically-first namespace is picked. If
// more than one match exists, a warning is emitted so operators can detect the
// collision.
func pickModel(matches []kubeaiv1.Model, preferredNS string) *kubeaiv1.Model {
	if len(matches) == 1 {
		return &matches[0]
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Namespace < matches[j].Namespace
	})
	chosen := &matches[0]
	for i := range matches {
		if matches[i].Namespace == preferredNS {
			chosen = &matches[i]
			break
		}
	}
	namespaces := make([]string, len(matches))
	for i, m := range matches {
		namespaces[i] = m.Namespace
	}
	logger.Info("model name collision across namespaces; picking one deterministically",
		"model", chosen.Name,
		"chosen_namespace", chosen.Namespace,
		"all_namespaces", namespaces,
	)
	return chosen
}
