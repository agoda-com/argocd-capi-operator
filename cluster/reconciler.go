package cluster

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/clustercache"

	argocluster "github.com/argoproj/argo-cd/v2/pkg/apiclient/cluster"
	argoapp "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"

	"github.com/agoda-com/argocd-capi-operator/plan"

	_ "embed"
)

const (
	ControllerName      = "argocd-capi-operator"
	InstanceAnnotation  = "argocd.fleet.agoda.com/instance"
	RefreshAtAnnotation = "argocd.fleet.agoda.com/refresh-at"
)

var Annotations = map[string]string{
	"app.kubernetes.io/managed-by": ControllerName,
}

//go:embed resources/clusterrole.yaml
var ClusterRoleData []byte

//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=events;serviceaccounts/token,verbs=create

//+kubebuilder:rbac:groups=core,resources=namespaces;serviceaccounts,verbs=get;create;patch
//+kubebuilder:rbac:groups=rbac,resources=clusterroles;clusterrolebindings;roles;rolebindings;serviceaccounts,verbs=get;create;patch

type Reconciler struct {
	kcl      client.Client
	cache    ClusterCache
	recorder record.EventRecorder
	clusters argocluster.ClusterServiceClient
	config   Config
}

type ClusterCache interface {
	GetClient(ctx context.Context, key client.ObjectKey) (client.Client, error)
	GetRESTConfig(ctx context.Context, key client.ObjectKey) (*rest.Config, error)
}

var _ reconcile.ObjectReconciler[*clusterv1.Cluster] = &Reconciler{}

type TokenStatus struct {
	Token     string
	RefreshAt time.Time
}

func NewReconciler(kcl client.Client, cache ClusterCache, recorder record.EventRecorder, clusters argocluster.ClusterServiceClient, config Config) (*Reconciler, error) {
	if kcl == nil || recorder == nil || clusters == nil {
		return nil, errors.New("missing one of required kcl, recorder, clusters")
	}

	if config.Namespace == "" {
		config.Namespace = "argocd"
	}

	if config.ServiceAccountName == "" {
		config.ServiceAccountName = "argocd-manager"
	}

	if config.TokenTTL == 0 {
		config.TokenTTL = 24 * time.Hour
	}

	return &Reconciler{
		kcl:      kcl,
		cache:    cache,
		recorder: recorder,
		clusters: clusters,
		config:   config,
	}, nil
}

func SetupWithManager(ctx context.Context, mgr manager.Manager, cache clustercache.ClusterCache, clusters argocluster.ClusterServiceClient, config Config) error {
	reconciler, err := NewReconciler(
		mgr.GetClient(),
		cache,
		mgr.GetEventRecorderFor(ControllerName),
		clusters,
		config,
	)
	if err != nil {
		return err
	}

	source := cache.GetClusterSource(ControllerName, func(ctx context.Context, obj client.Object) []reconcile.Request {
		key := client.ObjectKeyFromObject(obj)
		log.FromContext(ctx, "cluster", key)
		return []reconcile.Request{{NamespacedName: key}}
	})

	rateLimiter := workqueue.NewTypedItemFastSlowRateLimiter[reconcile.Request](1*time.Second, 5*time.Second, 10)

	return builder.ControllerManagedBy(mgr).
		For(&clusterv1.Cluster{}).
		WatchesRawSource(source).
		WithOptions(controller.Options{
			RateLimiter: rateLimiter,
		}).
		Complete(reconcile.AsReconciler(mgr.GetClient(), reconciler))
}

func (r *Reconciler) Reconcile(ctx context.Context, cluster *clusterv1.Cluster) (reconcile.Result, error) {
	logger := log.FromContext(ctx)

	key := client.ObjectKeyFromObject(cluster)
	kcl, err := r.cache.GetClient(ctx, key)
	switch {
	case errors.Is(err, clustercache.ErrClusterNotConnected):
		logger.Info("waiting for cluster to be ready")
		return reconcile.Result{Requeue: true}, nil
	case err != nil:
		return reconcile.Result{}, fmt.Errorf("create cluster client: %w", err)
	}

	logger.Info("reconcile")

	// provision namespace and service account
	b := plan.New().
		Namespace(r.config.Namespace).
		Annotations(Annotations)

	b.CoreV1().Namespace().
		Name(r.config.Namespace)

	serviceAccount := b.CoreV1().ServiceAccount().
		Name(r.config.ServiceAccountName).
		Value()

	res, err := b.Apply(ctx, kcl)
	switch {
	case err != nil:
		return reconcile.Result{}, fmt.Errorf("apply service account: %w", err)
	case len(res) != 0:
		logger.Info("plan applied")
	}

	// provision rbac resources
	b = plan.New().
		Owner(serviceAccount).
		Name(r.config.ServiceAccountName).
		Annotations(Annotations)

	b.RbacV1().ClusterRole().YAML(ClusterRoleData)

	b.RbacV1().ClusterRoleBinding().Update(func(roleBinding *rbacv1.ClusterRoleBinding) error {
		roleBinding.Subjects = []rbacv1.Subject{{
			APIGroup:  corev1.GroupName,
			Kind:      "ServiceAccount",
			Namespace: r.config.Namespace,
			Name:      r.config.ServiceAccountName,
		}}
		roleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     r.config.ServiceAccountName,
		}
		return nil
	})

	res, err = b.Apply(ctx, kcl)
	switch {
	case err != nil:
		return reconcile.Result{}, fmt.Errorf("apply: %w", err)
	case len(res) != 0:
		logger.Info("applied", "resources", res)
		r.recorder.Event(cluster, corev1.EventTypeNormal, "ResourceSync", "applied argocd resources")
	}

	// ensure argo cluster is registered
	refreshAt, err := r.ReconcileArgoCluster(ctx, kcl, cluster, serviceAccount)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("reconcile token: %w", err)
	}

	// prune deleted clusters
	err = r.Prune(ctx)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("prune argocd clusters: %w", err)
	}

	return reconcile.Result{RequeueAfter: time.Until(refreshAt)}, nil
}

func (r *Reconciler) ReconcileArgoCluster(ctx context.Context, kcl client.Client, cluster *clusterv1.Cluster, serviceAccount *corev1.ServiceAccount) (time.Time, error) {
	key := client.ObjectKeyFromObject(cluster)
	name := key.String()

	argoCluster, err := r.clusters.Get(ctx, &argocluster.ClusterQuery{
		Name: name,
	})
	if err != nil && status.Code(err) != codes.PermissionDenied {
		return time.Time{}, fmt.Errorf("get cluster: %w", err)
	}

	refreshAt := time.Time{}
	if argoCluster != nil && argoCluster.Annotations != nil {
		refreshAt, _ = time.Parse(time.RFC3339, argoCluster.Annotations[RefreshAtAnnotation])
	}

	if argoCluster != nil && maps.Equal(argoCluster.Labels, cluster.Labels) && refreshAt.After(time.Now()) {
		return refreshAt, nil
	}

	logger := log.FromContext(ctx)

	// service account has to exist at this point
	err = kcl.Get(ctx, client.ObjectKeyFromObject(serviceAccount), serviceAccount)
	if err != nil {
		return time.Time{}, err
	}

	// create token request for service account
	tokenReq := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To(int64(r.config.TokenTTL.Seconds())),
		},
	}
	err = kcl.SubResource("token").Create(ctx, serviceAccount, tokenReq)
	if err != nil {
		return time.Time{}, err
	}

	// subtract 20% from expiration timestamp to force timely refresh
	refreshAt = tokenReq.Status.ExpirationTimestamp.Add(-r.config.TokenTTL / 5)

	restConfig, err := r.cache.GetRESTConfig(ctx, key)
	if err != nil {
		return time.Time{}, err
	}

	// upsert argocd cluster
	annotations := map[string]string{
		RefreshAtAnnotation: refreshAt.Format(time.RFC3339),
	}
	if r.config.Instance != "" {
		annotations[InstanceAnnotation] = r.config.Instance
	}

	req := &argocluster.ClusterCreateRequest{
		Upsert: true,
		Cluster: &argoapp.Cluster{
			Name:             name,
			Labels:           cluster.Labels,
			Annotations:      annotations,
			Server:           restConfig.Host,
			ClusterResources: true,
			Config: argoapp.ClusterConfig{
				BearerToken: tokenReq.Status.Token,
				TLSClientConfig: argoapp.TLSClientConfig{
					CAData: restConfig.CAData,
				},
			},
		},
	}
	_, err = r.clusters.Create(ctx, req)
	if err != nil {
		return time.Time{}, fmt.Errorf("create argo cluster: %w", err)
	}

	logger.Info("registered", "refreshAt", refreshAt)

	return refreshAt, nil
}

func (r *Reconciler) Prune(ctx context.Context) error {
	if r.config.Instance == "" {
		return nil
	}

	logger := log.FromContext(ctx)

	clusters := &clusterv1.ClusterList{}
	err := r.kcl.List(ctx, clusters)
	if err != nil {
		return fmt.Errorf("list capi clusters: %w", err)
	}

	endpoints := make(map[string]struct{})
	for _, cluster := range clusters.Items {
		key := client.ObjectKeyFromObject(&cluster)
		server, err := r.cache.GetRESTConfig(ctx, key)
		if err != nil {
			return fmt.Errorf("get rest config: %w", err)
		}
		endpoints[server.Host] = struct{}{}
	}

	argoClusters, err := r.clusters.List(ctx, &argocluster.ClusterQuery{})
	if err != nil {
		return fmt.Errorf("list argocd clusters: %w", err)
	}

	for _, argoCluster := range argoClusters.Items {
		// if cluster is not managed by this instance bail
		if argoCluster.Annotations == nil || argoCluster.Annotations[InstanceAnnotation] != r.config.Instance {
			continue
		}

		// if capi cluster exists bail
		if _, found := endpoints[argoCluster.Server]; found {
			continue
		}

		_, err := r.clusters.Delete(ctx, &argocluster.ClusterQuery{
			Name:   argoCluster.Name,
			Server: argoCluster.Server,
		})
		if err != nil {
			logger.Error(err, "prune argocd cluster", "cluster", argoCluster.Name)
			return err
		}

		logger.Info("pruned argocd cluster", "cluster", argoCluster.Name)
	}

	return nil
}
