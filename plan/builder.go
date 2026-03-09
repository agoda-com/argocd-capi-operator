package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func NewBuilder[object client.Object](obj object) *Builder[object] {
	return &Builder[object]{
		obj: obj,
	}
}

type Builder[object client.Object] struct {
	obj object

	before  []UpdateFunc[object]
	updates []UpdateFunc[object]
	after   []UpdateFunc[object]
}

type UpdateFunc[object client.Object] func(object) error

func (b *Builder[object]) Value() object {
	return b.obj
}

// Object implements Task
func (b *Builder[object]) Object() client.Object {
	return b.Value()
}

func (b *Builder[object]) New() object {
	tpe := reflect.TypeFor[object]().Elem()
	obj := reflect.New(tpe).Interface()
	return obj.(object)
}

func (b *Builder[object]) Name(name string) *Builder[object] {
	b.Value().SetName(name)
	return b
}

func (b *Builder[object]) Before(hooks ...UpdateFunc[object]) *Builder[object] {
	b.before = append(b.before, hooks...)
	return b
}

func (b *Builder[object]) Update(updates ...UpdateFunc[object]) *Builder[object] {
	b.updates = append(b.updates, updates...)
	return b
}

func (b *Builder[object]) After(hooks ...UpdateFunc[object]) *Builder[object] {
	b.after = append(b.after, hooks...)
	return b
}

// JSON unmarshals json encoded data
func (b *Builder[object]) JSON(data []byte) *Builder[object] {
	return b.Before(func(obj object) error {
		return json.Unmarshal(data, obj)
	})
}

// YAML unmarshals yaml encoded data
func (b *Builder[object]) YAML(data []byte) *Builder[object] {
	return b.Before(func(obj object) error {
		return yaml.Unmarshal(data, obj)
	})
}

// MergeJSON applies provided JSON encoded strategic merge patch
func (b *Builder[object]) MergeJSON(patch []byte) *Builder[object] {
	return b.After(func(obj object) error {
		return StrategicMergePatch(obj, patch)
	})
}

// MergeYAML applies provided YAML encoded strategic merge patch
func (b *Builder[object]) MergeYAML(patch []byte) *Builder[object] {
	return b.After(func(obj object) error {
		patch, err := yaml.YAMLToJSON(patch)
		if err != nil {
			return err
		}

		return StrategicMergePatch(obj, patch)
	})
}

// Apply implements Task
func (b *Builder[object]) Apply(ctx context.Context, kcl client.Client) error {
	obj := b.Value()

	// before hooks
	for _, hook := range b.before {
		err := hook(obj)
		if err != nil {
			return fmt.Errorf("before update: %w", err)
		}
	}

	// apply updates
	for _, update := range b.updates {
		err := update(obj)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
	}

	// after hooks
	//
	// example: StrategicMergePatch
	for _, hook := range b.after {
		err := hook(obj)
		if err != nil {
			return fmt.Errorf("post update: %w", err)
		}
	}

	return nil
}

func StrategicMergePatch(obj client.Object, patch []byte) error {
	original, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal base: %w", err)
	}

	merged, err := strategicpatch.StrategicMergePatch(original, patch, obj)
	if err != nil {
		return err
	}

	err = json.Unmarshal(merged, obj)
	if err != nil {
		return fmt.Errorf("unmarshal merged: %w", err)
	}

	return nil
}
