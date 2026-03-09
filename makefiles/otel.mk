ifndef _include_otel_mk
_include_otel_mk := 1

include makefiles/base.mk

### Variables

MDATAGEN_FILE ?= metadata.yaml
MDATAGEN_VERSION ?= v0.85.0

### Targets

# Enable generate-mdatagen target if metadata file exists
ifneq ($(wildcard $(MDATAGEN_FILE)),)
.PHONY: generate-mdatagen

generate: generate-mdatagen
endif

### Tools

# Install otel mdatagen
MDATAGEN_ROOT := $(BINDIR)/mdatagen-$(MDATAGEN_VERSION)
MDATAGEN := $(MDATAGEN_ROOT)/mdatagen

$(MDATAGEN):
	GOBIN=$(abspath $(MDATAGEN_ROOT)) go install github.com/open-telemetry/opentelemetry-collector-contrib/cmd/mdatagen@$(MDATAGEN_VERSION)

### Implementation

ifneq ($(wildcard $(MDATAGEN_FILE)),)
generate-mdatagen: $(MDATAGEN) $(MDATAGEN_FILE)
	$(MDATAGEN) $(MDATAGEN_FILE)
endif # MDATAGEN_METADATA

endif # _include_otel_mk