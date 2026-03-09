package plan_test

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/agoda-com/argocd-capi-operator/plan"

	_ "embed"
)

var update = flag.Bool("update", false, "update golden files")

//go:embed testdata/deployment.yaml
var DeploymentData []byte

//go:embed testdata/deployment.patch.yaml
var DeploymentPatchData []byte

func TestPlan(t *testing.T) {
	if testing.Short() || os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.SkipNow()
	}

	env := &envtest.Environment{}
	kcl := setupEnvtest(t, env)

	p := plan.New().
		Namespace("default").
		Label("app.kubernetes.io/managed-by", "plan.fleet.agoda.com").
		Label("app.kubernetes.io/name", "test-plan")

	deployment := p.AppsV1().Deployment().
		Name("test-plan").
		YAML(DeploymentData).
		MergeYAML(DeploymentPatchData).
		Update(func(deployment *appsv1.Deployment) error {
			deployment.Spec.Replicas = ptr.To(int32(3))
			return nil
		})

	_, err := p.Apply(t.Context(), kcl)
	if err != nil {
		t.Fatal("apply:", err)
	}

	expectObject(t, "deployment.golden.yaml", deployment.Value())
}

func setupEnvtest(t testing.TB, env *envtest.Environment) client.Client {
	t.Helper()

	kubeconfig, err := env.Start()
	if err != nil {
		t.Fatal("start envtest:", err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Log("stop envtest:", err)
		}
	})

	kcl, err := client.New(kubeconfig, client.Options{})
	if err != nil {
		t.Fatal("create client:", err)
	}

	return kcl
}

func expectObject(t testing.TB, name string, obj client.Object) {
	t.Helper()

	dir := filepath.Join("testdata", t.Name())
	err := os.MkdirAll(dir, 0744)
	if err != nil {
		t.Fatal(err)
	}

	name = filepath.Join(dir, name)

	data, err := os.ReadFile(name)
	switch {
	case errors.Is(err, fs.ErrNotExist) && *update:
	case err != nil:
		t.Fatal("read golden:", err)
	}

	if *update {
		data, err := yaml.Marshal(obj)
		if err != nil {
			t.Fatal("marshal golden:", err)
		}

		err = os.WriteFile(name, data, 0644)
		if err != nil {
			t.Fatal("write golden:", err)
		}

		return
	}

	tpe := reflect.TypeOf(obj).Elem()
	expected := reflect.New(tpe).Interface().(client.Object)
	err = yaml.Unmarshal(data, expected)
	if err != nil {
		t.Fatal("unmarshal golden:", err)
	}

	diff := cmp.Diff(obj, expected,
		cmpopts.IgnoreTypes(metav1.Time{}, metav1.ManagedFieldsEntry{}),
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "UID", "ResourceVersion"),
	)
	if diff != "" {
		t.Error("diff (actual, expected):\n", diff)
	}
}
