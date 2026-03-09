# argocd-capi-operator

[![codecov](https://codecov.io/gh/agoda-com/argocd-capi-operator/graph/badge.svg?token=AOPBXCCF8W)](https://codecov.io/gh/agoda-com/argocd-capi-operator)

Register [Cluster](https://doc.crds.dev/github.com/kubernetes-sigs/cluster-api/cluster.x-k8s.io/Cluster/v1beta1@v1.8.3) resources using [ArgoCD Cluster API](https://pkg.go.dev/github.com/argoproj/argo-cd/v2@v2.12.3/pkg/apiclient/cluster#ClusterServiceClient).

## How it works

Operator registers an Cluster in ArgoCD using token generated for service account `argocd-manager`.

Using cluster kubeconfig it creates/patches:

- Namespace `argocd`
- ServiceAccount `argocd-manager` in namespace `argocd`
- ClusterRole/ClusterRoleBinding `argocd-manager` letting argocd manage crds, webhooks and rbac resources

## Deployment

### Local

```bash
skaffold run
```

### Management Cluster

```bash
skaffold build --kube-context <management-cluster> --quiet | \
skaffold deploy ---kube-context <management-cluster> --build-artifacts -
```

## Operations

### Usage

| Name | Default | Usage |
| --- | --- | --- |
| context |  | Kubernetes context |
| health-probe-bind-address | :8081 | The address the probe endpoint binds to |
| instance |  | Instance to populate argocd.fleet.agoda.com/instance annotation |
| leader-election | true | Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager |
| metrics-bind-address | :8080 | The address the metric endpoint binds to |
| token-ttl | 1h0m0s | Service account bearer token TTL |
| watch-namespaces | [] | Namespaces to watch for Cluster resources |
| watch-selector |  | Selector to watch for Cluster resources |
| zap-devel | false | Development Mode defaults(encoder=consoleEncoder,logLevel=Debug,stackTraceLevel=Warn). Production Mode defaults(encoder=jsonEncoder,logLevel=Info,stackTraceLevel=Error) |
| zap-encoder |  | Zap log encoding (one of 'json' or 'console') |
| zap-log-level |  | Zap Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error', or any integer value > 0 which corresponds to custom debug levels of increasing verbosity |
| zap-stacktrace-level |  | Zap Level at and above which stacktraces are captured (one of 'info', 'error', 'panic'). |
| zap-time-encoding |  | Zap time encoding (one of 'epoch', 'millis', 'nano', 'iso8601', 'rfc3339' or 'rfc3339nano'). Defaults to 'epoch'. |

### Cluster discovery

[Cluster](https://doc.crds.dev/github.com/kubernetes-sigs/cluster-api/cluster.x-k8s.io/Cluster/v1beta1@v1.8.3) resources are only watched in namespaces specified by `--watch-namespaces`.

They can be further scoped down by `--watch-filter` which should contain value for `cluster.x-k8s.io/watch-filter` label.

Example:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argocd-capi-operator
spec:
  template:
    spec:
      containers:
        - name: operator
          args:
            - --watch-namespaces=kubernetes,tools,nosql
            - --watch-filter=argocd
```