package plan

import (
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AppsV1 struct {
	plan *Plan
}

func (p *Plan) AppsV1() AppsV1 {
	return AppsV1{
		plan: p,
	}
}

func (b AppsV1) Deployment() *Builder[*appsv1.Deployment] {
	builder := NewBuilder(&appsv1.Deployment{})
	b.plan.Add(builder)

	// default spec.selector to metadata.labels
	builder.After(func(deployment *appsv1.Deployment) error {
		if deployment.Spec.Template.Labels == nil && len(deployment.Labels) != 0 {
			deployment.Spec.Template.Labels = maps.Clone(deployment.Labels)
		}

		if deployment.Spec.Selector == nil && len(deployment.Spec.Template.Labels) != 0 {
			deployment.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: deployment.Spec.Template.Labels,
			}
		}

		return nil
	})

	return builder
}
