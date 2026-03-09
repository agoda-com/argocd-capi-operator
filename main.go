package main

import (
	"context"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/go-logr/logr"
	"github.com/spf13/pflag"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	kscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/clustercache"
	"sigs.k8s.io/cluster-api/controllers/remote"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	argo "github.com/argoproj/argo-cd/v2/pkg/apiclient"
	argocluster "github.com/argoproj/argo-cd/v2/pkg/apiclient/cluster"

	"github.com/agoda-com/argocd-capi-operator/cluster"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()

	config := Config{}

	fs := pflag.NewFlagSet("argocd-capi-operator", pflag.ExitOnError)
	config.BindFlags(fs)
	_ = fs.Parse(os.Args[1:])

	md, err := fs.GetBool("markdown-help")
	if err != nil {
		fs.Usage()
		os.Exit(1)
	}

	if md {
		MarkdownHelp(os.Stdout, fs)
		os.Exit(2)
	}

	logger := zap.New()
	log.SetLogger(logger)

	ctx = log.IntoContext(ctx, logger)

	restConfig, err := clientconfig.GetConfigWithContext(config.Context)
	if err != nil {
		logger.Error(err, "load kubeconfig")
		os.Exit(1)
	}

	mgr, err := setupManager(logger, restConfig, config)
	if err != nil {
		logger.Error(err, "setup manager")
		os.Exit(1)
	}

	cache, err := clustercache.SetupWithManager(ctx, mgr, clustercache.Options{
		Client: clustercache.ClientOptions{
			UserAgent: remote.DefaultClusterAPIUserAgent(cluster.ControllerName),
		},
		SecretClient: mgr.GetClient(),
	}, controller.Options{})
	if err != nil {
		logger.Error(err, "setup cluster cache")
		os.Exit(1)
	}

	argoClusters, err := setupArgoClusterClient(ctx)
	if err != nil {
		logger.Error(err, "setup argocd cluster client: %w", err)
		os.Exit(1)
	}

	err = cluster.SetupWithManager(ctx, mgr, cache, argoClusters, cluster.Config{
		Instance: config.Instance,
		TokenTTL: config.TokenTTL,
	})
	if err != nil {
		logger.Error(err, "setup reconciler")
		os.Exit(1)
	}

	err = mgr.Start(ctx)
	if err != nil {
		logger.Error(err, "manager")
		os.Exit(1)
	}
}

func setupManager(logger logr.Logger, restConfig *rest.Config, config Config) (manager.Manager, error) {
	builder := runtime.NewSchemeBuilder(
		kscheme.AddToScheme,
		clusterv1.AddToScheme,
	)

	scheme := runtime.NewScheme()
	err := builder.AddToScheme(scheme)
	if err != nil {
		return nil, err
	}

	var namespaces map[string]cache.Config
	if !slices.Equal(config.WatchNamespaces, []string{"*"}) {
		namespaces = make(map[string]cache.Config, len(config.WatchNamespaces))
		for _, ns := range config.WatchNamespaces {
			namespaces[ns] = cache.Config{}
		}
	}

	// cache cluster-api secrets
	secretSelector := labels.NewSelector()
	req, _ := labels.NewRequirement(clusterv1.ClusterNameLabel, selection.Exists, nil)
	secretSelector.Add(*req)

	byObject := map[client.Object]cache.ByObject{
		&corev1.Secret{}: {
			Label: secretSelector,
		},
		&clusterv1.Cluster{}: {
			Label: config.WatchSelector.Selector(),
		},
	}

	mgr, err := manager.New(restConfig, manager.Options{
		Logger:                 logger,
		Scheme:                 scheme,
		LeaderElection:         config.LeaderElection,
		LeaderElectionID:       "argocd.fleet.agoda.com",
		HealthProbeBindAddress: config.HealthAddr,
		Metrics: metricsserver.Options{
			BindAddress: config.MetricsAddr,
		},
		Cache: cache.Options{
			DefaultNamespaces: namespaces,
			ByObject:          byObject,
		},
	})
	if err != nil {
		return nil, err
	}

	err = mgr.AddHealthzCheck("healthz", healthz.Ping)
	if err != nil {
		return nil, err
	}
	err = mgr.AddReadyzCheck("readyz", healthz.Ping)
	if err != nil {
		return nil, err
	}

	return mgr, nil
}

func setupArgoClusterClient(ctx context.Context) (argocluster.ClusterServiceClient, error) {
	acl, err := argo.NewClient(&argo.ClientOptions{})
	if err != nil {
		return nil, err
	}

	closer, clusters, err := acl.NewClusterClient()
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = closer.Close()
	}()

	return clusters, nil
}
