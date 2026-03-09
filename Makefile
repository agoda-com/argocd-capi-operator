GOLANGCILINT_VERSION := v2.5.0
GOTESTSUM_VERSION := v1.13.0

GOCOVERPKG := github.com/agoda-com/argocd-capi-operator/...

CONTROLLER_GEN_VERSION = v0.17.1
CONTROLLER_GEN_ARGS := \
	paths={./...} \
	rbac:roleName=argocd-capi-operator \
	output:rbac:dir=config/rbac

include makefiles/go.mk
include makefiles/controller.mk