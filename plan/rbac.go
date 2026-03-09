package plan

import (
	rbacv1 "k8s.io/api/rbac/v1"
)

type RbacV1 struct {
	plan *Plan
}

func (p *Plan) RbacV1() RbacV1 {
	return RbacV1{
		plan: p,
	}
}

func (b RbacV1) ClusterRole() *Builder[*rbacv1.ClusterRole] {
	builder := NewBuilder(&rbacv1.ClusterRole{})
	b.plan.Add(builder)
	return builder
}

func (b RbacV1) ClusterRoleBinding() *Builder[*rbacv1.ClusterRoleBinding] {
	builder := NewBuilder(&rbacv1.ClusterRoleBinding{})
	b.plan.Add(builder)
	return builder
}
