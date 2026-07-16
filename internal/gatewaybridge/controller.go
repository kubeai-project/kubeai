package gatewaybridge

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	infextv1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
)

// Reconciler watches Model objects and maintains a singleton InferencePool that
// selects all KubeAI-managed pods. It is only active when gatewayAPI.enabled=true.
type Reconciler struct {
	client.Client
	Namespace string
	Config    config.GatewayAPI
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if err := r.reconcileInferencePool(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling InferencePool: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) reconcileInferencePool(ctx context.Context) error {
	pool := &infextv1.InferencePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.Config.InferencePoolName,
			Namespace: r.Namespace,
		},
	}

	port := infextv1.PortNumber(r.Config.EndpointPickerPort)

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, pool, func() error {
		pool.Spec = infextv1.InferencePoolSpec{
			Selector: infextv1.LabelSelector{
				MatchLabels: map[infextv1.LabelKey]infextv1.LabelValue{
					"app.kubernetes.io/managed-by": "kubeai",
				},
			},
			TargetPorts: []infextv1.Port{
				{Number: 8000},
			},
			EndpointPickerRef: infextv1.EndpointPickerRef{
				Name: infextv1.ObjectName(r.Config.EndpointPickerService),
				Port: &infextv1.Port{Number: port},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("creating or updating InferencePool %q: %w", r.Config.InferencePoolName, err)
	}

	if op != controllerutil.OperationResultNone {
		log.FromContext(ctx).Info("reconciled InferencePool", "operation", op, "name", r.Config.InferencePoolName)
	}

	return nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubeaiv1.Model{}).
		Named("gateway-bridge").
		Complete(r)
}
