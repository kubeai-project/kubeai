package modelclient

import (
	"context"
	"fmt"
	"log"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *ModelClient) ScaleAtLeastOneReplica(ctx context.Context, model string) error {
	obj, err := c.LookupModel(ctx, model, "", nil)
	if err != nil {
		return fmt.Errorf("lookup model: %w", err)
	}
	if obj == nil {
		return fmt.Errorf("model %q not found", model)
	}

	if obj.Spec.AutoscalingDisabled {
		return nil
	}

	replicas := int32(0)
	if obj.Spec.Replicas != nil {
		replicas = *obj.Spec.Replicas
	}

	if replicas == 0 && !obj.Spec.AutoscalingDisabled {
		scale := &autoscalingv1.Scale{
			Spec: autoscalingv1.ScaleSpec{Replicas: 1},
		}
		if err := c.client.SubResource("scale").Update(ctx, obj, client.WithSubResourceBody(scale)); err != nil {
			return fmt.Errorf("update scale: %w", err)
		}
	}

	return nil
}

// Scale scales the model to the desired number of replicas, enforcing the min and max replica bounds.
// Model should have .Spec defined before calling Scale().
func (c *ModelClient) Scale(ctx context.Context, model *kubeaiv1.Model, replicas int32, requiredConsecutiveScaleDowns int) error {
	//obj := &kubeaiv1.Model{}
	//if err := s.client.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: model}, obj); err != nil {
	//	return fmt.Errorf("get scale: %w", err)
	//}

	replicas = enforceReplicaBounds(replicas, model)

	var existingReplicas int32 = 0
	if model.Spec.Replicas != nil {
		existingReplicas = *model.Spec.Replicas
	}

	if existingReplicas > replicas {
		// Scale down
		c.consecutiveScaleDownsMtx.RLock()
		consec := c.consecutiveScaleDowns[model.Name]
		c.consecutiveScaleDownsMtx.RUnlock()
		if consec < requiredConsecutiveScaleDowns {
			log.Printf("model %s has %d consecutive scale downs (< %d), not scaling down yet", model.Name, consec, requiredConsecutiveScaleDowns)
			c.consecutiveScaleDownsMtx.Lock()
			c.consecutiveScaleDowns[model.Name]++
			c.consecutiveScaleDownsMtx.Unlock()
			return nil
		}
	} else {
		// Scale up or constant scale.
		c.consecutiveScaleDownsMtx.Lock()
		c.consecutiveScaleDowns[model.Name] = 0
		c.consecutiveScaleDownsMtx.Unlock()
	}

	if existingReplicas != replicas {
		log.Printf("scaling model %s from %d to %d replicas", model.Name, existingReplicas, replicas)
		scale := &autoscalingv1.Scale{
			Spec: autoscalingv1.ScaleSpec{Replicas: replicas},
		}
		if err := c.client.SubResource("scale").Update(ctx, model, client.WithSubResourceBody(scale)); err != nil {
			return fmt.Errorf("update scale: %w", err)
		}
	}

	return nil
}

func enforceReplicaBounds(replicas int32, model *kubeaiv1.Model) int32 {
	max := model.Spec.MaxReplicas
	min := model.Spec.MinReplicas
	if max != nil {
		if replicas > *max {
			return *max
		}
	}
	if replicas < min {
		return min
	}
	return replicas
}
