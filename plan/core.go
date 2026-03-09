package plan

import (
	"slices"

	corev1 "k8s.io/api/core/v1"
)

type CoreV1 struct {
	plan *Plan
}

func (p *Plan) CoreV1() CoreV1 {
	return CoreV1{
		plan: p,
	}
}

func (b CoreV1) Namespace() *Builder[*corev1.Namespace] {
	builder := NewBuilder(&corev1.Namespace{})
	b.plan.Add(builder)
	return builder
}

func (b CoreV1) ServiceAccount() *Builder[*corev1.ServiceAccount] {
	builder := NewBuilder(&corev1.ServiceAccount{})
	b.plan.Add(builder)
	return builder
}

func Volume(volumes []corev1.Volume, name string) *corev1.Volume {
	i := slices.IndexFunc(volumes, func(volume corev1.Volume) bool {
		return volume.Name == name
	})
	if i == -1 {
		return nil
	}

	return &volumes[i]
}
