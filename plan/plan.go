package plan

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Plan struct {
	owner       client.Object
	name        string
	namespace   string
	labels      map[string]string
	annotations map[string]string

	tasks []Task
}

type Task interface {
	Object() client.Object
	Apply(ctx context.Context, kcl client.Client) error
}

type ApplyResult struct {
	Object client.Object
	Result controllerutil.OperationResult
}

func New() *Plan {
	return &Plan{
		labels:      map[string]string{},
		annotations: map[string]string{},
	}
}

func (p *Plan) Owner(owner client.Object) *Plan {
	p.owner = owner

	if p.namespace == "" {
		p.namespace = p.owner.GetNamespace()
	}

	return p
}

func (p *Plan) Name(name string) *Plan {
	p.name = name
	return p
}

func (p *Plan) Namespace(namespace string) *Plan {
	p.namespace = namespace
	return p
}

func (p *Plan) Labels(labels map[string]string) *Plan {
	maps.Copy(p.labels, labels)
	return p
}

func (p *Plan) Label(key, value string) *Plan {
	p.labels[key] = value
	return p
}

func (p *Plan) Annotations(annotations map[string]string) *Plan {
	maps.Copy(p.annotations, annotations)
	return p
}

func (p *Plan) Annotation(key, value string) *Plan {
	p.annotations[key] = value
	return p
}

func (p *Plan) Add(builder Task) {
	p.tasks = append(p.tasks, builder)
}

func (p *Plan) Apply(ctx context.Context, kcl client.Client) ([]ApplyResult, error) {
	var acc []ApplyResult
	for _, task := range p.tasks {
		res, err := p.ApplyTask(ctx, kcl, task)
		switch {
		case err != nil:
			return acc, err
		case res != controllerutil.OperationResultNone:
			acc = append(acc, ApplyResult{
				Object: task.Object(),
				Result: res,
			})
		}
	}

	return acc, nil
}

func (p *Plan) ApplyTask(ctx context.Context, kcl client.Client, task Task) (controllerutil.OperationResult, error) {
	obj := task.Object()
	if obj == nil {
		return controllerutil.OperationResultNone, errors.New("expected object to be set")
	}

	// populate metadata.name
	name := obj.GetName()
	switch {
	case p.name == "" && name == "":
		return controllerutil.OperationResultNone, errors.New("expected name to be set")
	case p.name != "" && name == "":
		obj.SetName(p.name)
	case p.name != "" && name != "":
		obj.SetName(p.name + "-" + name)
	}

	// populate metadata.namespace
	namespaced, err := apiutil.IsObjectNamespaced(
		obj,
		kcl.Scheme(),
		kcl.RESTMapper(),
	)
	switch {
	case err != nil:
		return controllerutil.OperationResultNone, err
	case namespaced && p.namespace == "":
		return controllerutil.OperationResultNone, errors.New("expected namespace or owner to be set")
	case namespaced && p.namespace != "":
		obj.SetNamespace(p.namespace)
	}

	return controllerutil.CreateOrPatch(ctx, kcl, obj, func() error {
		if obj.GetLabels() == nil {
			obj.SetLabels(map[string]string{})
		}
		maps.Copy(obj.GetLabels(), p.labels)

		if obj.GetAnnotations() == nil {
			obj.SetAnnotations(map[string]string{})
		}
		maps.Copy(obj.GetAnnotations(), p.annotations)

		err = task.Apply(ctx, kcl)
		if err != nil {
			return err
		}

		// populate metadata.ownerReferences
		if namespaced && p.owner != nil {
			err := controllerutil.SetControllerReference(
				p.owner,
				obj,
				kcl.Scheme(),
				controllerutil.WithBlockOwnerDeletion(true),
			)
			if err != nil {
				return fmt.Errorf("set owner: %w", err)
			}
		}

		return nil
	})
}

// MarshalLog implements logr.Marshaler
func (r ApplyResult) MarshalLog() any {
	gvk := r.Object.GetObjectKind().GroupVersionKind()
	return struct {
		APIVersion string
		Kind       string
		Namespace  string
		Name       string
		UID        types.UID
		Generation int64
	}{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Namespace:  r.Object.GetNamespace(),
		Name:       r.Object.GetName(),
		UID:        r.Object.GetUID(),
		Generation: r.Object.GetGeneration(),
	}
}
