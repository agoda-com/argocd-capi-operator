ifndef _include_go_mk
_include_go_mk := 1

include makefiles/base.mk

### Variables

GOLANGCILINT_VERSION ?= v2.4.0
GOTESTSUM_VERSION ?= v1.12.3

GOTESTPKG ?= ./...

# Go coverage directory
# GOCOVERDIR := build/coverage
GOCOVERPKG ?= ./... # Go coverage packages

# JUnit report file
# JUNIT_FILE := build/junit.xml

# Cobertura coverage report file
# CODECOV_FILE := build/coverage.xml

# HTML coverage report file
# CODECOV_HTMLFILE := build/coverage.html

# gremlins-related variables for mutation test
GOMUTEST_VERSION ?= v0.5.0
GOMUTESTARGS ?= .

### Targets

.PHONY: fetch-coverage
.PHONY: generate-go format-go lint-go test-go integration-test-go e2e-test-go coverage-go mutation-test-go

generate: generate-go
format: format-go
lint: lint-go
test: test-go
mutation-test: mutation-test-go
integration-test: integration-test-go
e2e-test: e2e-test-go
coverage: coverage-go

### Tools

# Install golangci-lint
GOLANGCILINT_ROOT := $(BINDIR)/golangci-lint-$(GOLANGCILINT_VERSION)
GOLANGCILINT := $(GOLANGCILINT_ROOT)/golangci-lint

$(GOLANGCILINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOLANGCILINT_ROOT) $(GOLANGCILINT_VERSION)

# Install gotestsum
GOTESTSUM_ROOT := $(BINDIR)/gotestsum-$(GOTESTSUM_VERSION)
GOTESTSUM := $(GOTESTSUM_ROOT)/gotestsum

$(GOTESTSUM):
	GOBIN=$(abspath $(GOTESTSUM_ROOT)) go install gotest.tools/gotestsum@$(GOTESTSUM_VERSION)

# Install gremlins for mutation test
GOMUTEST_ROOT := $(BINDIR)/gremlins-$(GOMUTEST_VERSION)
GOMUTEST := $(GOMUTEST_ROOT)/gremlins

$(GOMUTEST):
	@mkdir -p $(GOMUTEST_ROOT)
	GOBIN=$(abspath $(GOMUTEST_ROOT)) go install github.com/go-gremlins/gremlins/cmd/gremlins@$(GOMUTEST_VERSION)

### Implementation

generate-go:
	go generate ./...

GOFMT := go fmt ./...

format-go:
	${GOFMT}

lint-go: $(GOLANGCILINT)
	$(GOLANGCILINT) run

GOTEST := go test

test-go:
	$(GOTEST) -short $(GOTESTPKG) $(GOTESTARGS)

integration-test-go:
	$(GOTEST) -v -count 1 $(GOTESTPKG) $(GOTESTARGS)

e2e-test-go:
	$(GOTEST) -v -count 1 ./e2e $(GOTESTARGS)

mutation-test-go: $(GOMUTEST)
	$(GOMUTEST) unleash $(GOMUTESTARGS)

# if JUNIT_FILE is set generate JUnit reports
ifneq ($(strip $(JUNIT_FILE)),)
test-go integration-test-go e2e-test-go: $(GOTESTSUM)
test integration-test e2e-test: $(JUNIT_FILE)

GOTESTOUT := $(TMPDIR)/test-results.json
GOTEST := $(GOTESTSUM) --format standard-verbose --jsonfile $(GOTESTOUT) --

$(JUNIT_FILE): $(GOTESTSUM)
	@mkdir -p $(dir $(JUNIT_FILE))
	$(GOTESTSUM) --junitfile $(JUNIT_FILE) --raw-command cat $(GOTESTOUT) &>/dev/null

# ensure test results are processed
.IGNORE: test-go integration-test-go e2e-test-go
endif # JUNIT_FILE

ifneq ($(filter coverage,$(MAKECMDGOALS)),)
GOCOVERDIR ?= $(TMPDIR)/coverage
GOCOVEROUT ?= $(GOCOVERDIR)/coverage.txt

# ensure coverage is processed
.IGNORE: test-go integration-test-go
endif

ifneq ($(strip $(GOCOVERDIR)),)
GOTEST += -coverpkg=$(GOCOVERPKG) -covermode=atomic
GOTESTARGS += -test.gocoverdir=$(abspath $(GOCOVERDIR))

test-go integration-test-go e2e-test-go: $(GOCOVERDIR)

coverage-go: $(GOCOVEROUT)
	go tool covdata func -i $(abspath $(GOCOVERDIR)) -pkg $(GOCOVERPKG)

$(GOCOVEROUT): $(GOCOVERDIR)
	go tool covdata textfmt -i $(abspath $(GOCOVERDIR)) -o $(GOCOVEROUT) -pkg $(GOCOVERPKG)

$(GOCOVERDIR):
	@mkdir -p $(GOCOVERDIR)

ifneq ($(strip $(FETCH_COVERAGE_ARGS)),)
fetch-coverage: $(GOCOVERDIR)
	GOCOVERDIR=$(GOCOVERDIR) \
	makefiles/scripts/fetch-coverage $(FETCH_COVERAGE_ARGS)
endif # FETCH_COVERAGE_ARGS

endif # GOCOVERDIR

endif # _include_go_mk
