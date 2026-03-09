package cluster

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	argocluster "github.com/argoproj/argo-cd/v2/pkg/apiclient/cluster"
	argoapp "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
)

func TestReconciler(t *testing.T) {
	scheme := setupScheme(t)
	kubeconfig := setupEnvtest(t, scheme)
	kcl, err := client.New(kubeconfig, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal("client:", err)
	}

	cache := setupClusterCache(kcl)
	recorder := record.NewFakeRecorder(100)
	clusters := setupArgo(t)
	reconciler, err := NewReconciler(
		kcl,
		cache,
		recorder,
		clusters,
		Config{
			Instance: "test-instance",
		},
	)
	if err != nil {
		t.Fatal("setup reconciler:", err)
	}

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-cluster",
		},
		Spec: clusterv1.ClusterSpec{
			ControlPlaneEndpoint: clusterv1.APIEndpoint{
				Host: "test-cluster.default.svc",
				Port: 6443,
			},
		},
	}
	err = kcl.Create(t.Context(), cluster)
	if err != nil {
		t.Fatal("create cluster:", err)
	}

	_, err = reconciler.Reconcile(t.Context(), cluster)
	if err != nil {
		t.Fatal("reconcile:", err)
	}

	// cluster should be registered in ArgoCD
	clusterList, err := clusters.List(t.Context(), &argocluster.ClusterQuery{})
	if err != nil {
		t.Fatal("get argo cluster:", err)
	}
	if len(clusterList.Items) != 1 {
		t.Fatalf("expected 1 argo cluster, got %d", len(clusterList.Items))
	}
	if clusterList.Items[0].Server != "https://test-cluster.default.svc:6443" {
		t.Fatalf("expected initial cluster to have the current endpoint, got %s", clusterList.Items[0].Server)
	}

	// second reconcile should be no-op as refresh-at is set
	_, err = reconciler.Reconcile(t.Context(), cluster)
	if err != nil {
		t.Fatal("reconcile:", err)
	}

	// Update cluster with new endpoint (simulating cluster recreation with same name but different endpoint)
	cluster.Spec.ControlPlaneEndpoint.Host = "new-ep.test-cluster.default.svc"
	err = kcl.Update(t.Context(), cluster)
	if err != nil {
		t.Fatal("update cluster:", err)
	}

	// third reconcile should register the new cluster & prune the old one
	_, err = reconciler.Reconcile(t.Context(), cluster)
	if err != nil {
		t.Fatal("reconcile:", err)
	}

	clusterList, err = clusters.List(t.Context(), &argocluster.ClusterQuery{})
	if err != nil {
		t.Fatal("list argo clusters:", err)
	}
	if len(clusterList.Items) > 1 {
		t.Fatalf("expected 1 argo cluster, got %d", len(clusterList.Items))
	}
	if clusterList.Items[0].Server != "https://new-ep.test-cluster.default.svc:6443" {
		t.Fatalf("expected new cluster to have the new endpoint, got %s", clusterList.Items[0].Server)
	}
}

func setupScheme(t testing.TB) *runtime.Scheme {
	scheme := runtime.NewScheme()
	builder := runtime.NewSchemeBuilder(
		kscheme.AddToScheme,
		clusterv1.AddToScheme,
	)
	err := builder.AddToScheme(scheme)
	if err != nil {
		t.Fatal("setup scheme:", err)
	}

	return scheme
}

func setupEnvtest(t testing.TB, scheme *runtime.Scheme) *rest.Config {
	t.Helper()

	if testing.Short() || os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.SkipNow()
	}

	env := &envtest.Environment{
		Scheme: scheme,
		CRDDirectoryPaths: []string{
			filepath.Join("testdata", "crd"),
		},
	}
	kubeconfig, err := env.Start()
	if err != nil {
		t.Fatal("start envtest: ", err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Log("stop envtest: ", err)
		}
	})

	return kubeconfig
}

func setupClusterCache(kcl client.Client) *fakeClusterCache {
	return &fakeClusterCache{
		client: kcl,
	}
}

func setupArgo(t testing.TB) argocluster.ClusterServiceClient {
	t.Helper()

	server := grpc.NewServer()
	t.Cleanup(func() {
		server.GracefulStop()
	})

	clusterServer := &fakeClusterServer{}
	argocluster.RegisterClusterServiceServer(server, clusterServer)

	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal("listen:", err)
	}
	go func() {
		_ = server.Serve(lis)
	}()

	_, port, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	client, err := grpc.NewClient(net.JoinHostPort("localhost", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal("client:", err)
	}

	return argocluster.NewClusterServiceClient(client)
}

var (
	_ ClusterCache                     = &fakeClusterCache{}
	_ argocluster.ClusterServiceServer = &fakeClusterServer{}
)

type fakeClusterCache struct {
	client client.Client
}

type fakeClusterServer struct {
	argocluster.UnimplementedClusterServiceServer

	mtx      sync.Mutex
	clusters []*argoapp.Cluster
}

func (s *fakeClusterCache) GetRESTConfig(ctx context.Context, key client.ObjectKey) (*rest.Config, error) {
	cluster := &clusterv1.Cluster{}
	err := s.client.Get(ctx, key, cluster)
	if err != nil {
		return nil, err
	}

	// Build the server URL from ControlPlaneEndpoint
	serverURL := fmt.Sprintf("https://%s:%d", cluster.Spec.ControlPlaneEndpoint.Host, cluster.Spec.ControlPlaneEndpoint.Port)

	return &rest.Config{
		Host: serverURL,
	}, nil
}

func (s *fakeClusterCache) GetClient(ctx context.Context, key client.ObjectKey) (client.Client, error) {
	return s.client, nil
}

func (s *fakeClusterServer) Get(ctx context.Context, req *argocluster.ClusterQuery) (*argoapp.Cluster, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	i := s.index(req)
	if i == -1 {
		return nil, status.Error(codes.PermissionDenied, "not found")
	}

	return s.clusters[i], nil
}

func (s *fakeClusterServer) Create(ctx context.Context, req *argocluster.ClusterCreateRequest) (*argoapp.Cluster, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	query := &argocluster.ClusterQuery{
		Server: req.Cluster.Server,
		Name:   req.Cluster.Name,
	}
	i := s.index(query)
	switch {
	case i != -1 && !req.Upsert:
		return nil, status.Error(codes.AlreadyExists, "already exists")
	case i != -1:
		s.clusters[i] = req.Cluster
	default:
		s.clusters = append(s.clusters, req.Cluster)
	}

	return req.Cluster.DeepCopy(), nil
}

func (s *fakeClusterServer) List(ctx context.Context, req *argocluster.ClusterQuery) (*argoapp.ClusterList, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	var items []argoapp.Cluster
	for _, cluster := range s.clusters {
		items = append(items, *cluster.DeepCopy())
	}

	return &argoapp.ClusterList{
		Items: items,
	}, nil
}

func (s *fakeClusterServer) Delete(ctx context.Context, req *argocluster.ClusterQuery) (*argocluster.ClusterResponse, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	i := s.index(req)
	if i == -1 {
		return nil, status.Error(codes.PermissionDenied, "not found")
	}

	s.clusters = slices.Delete(s.clusters, i, 1)

	return &argocluster.ClusterResponse{}, nil
}

func (s *fakeClusterServer) index(req *argocluster.ClusterQuery) int {
	return slices.IndexFunc(s.clusters, func(cluster *argoapp.Cluster) bool {
		return cluster.Server == req.Server
	})
}
