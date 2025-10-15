# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.33.0
CRD_REF_DOCS_VERSION = v0.2.0
SKAFFOLD_VERSION = v2.13.2

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen yq
	# Generate CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd webhook paths="./..." output:crd:artifacts:config=manifests/crds/

	# Generate CustomResourceDefinition for helm chart
	echo '{{-  if .Values.crds.enabled -}}' > charts/kubeai/templates/crds/kubeai.org_models.yaml
	cat manifests/crds/kubeai.org_models.yaml >> charts/kubeai/templates/crds/kubeai.org_models.yaml
	echo '{{-  end }}' >> charts/kubeai/templates/crds/kubeai.org_models.yaml

	# Generate model manifests.
	rm -f ./manifests/models/*
	helm template ./charts/models --set all.enabled=true | \
	$(YQ) -s '"./manifests/models/" + .metadata.name + ".yaml"' --no-doc

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

# Use RUN=TestName to run specific integration tests.
.PHONY: test-unit
test-unit: fmt vet
	go test -v ./internal/... $(if $(RUN),-run $(RUN),) -coverprofile cover.unit.out

# Use RUN=TestName to run specific integration tests.
.PHONY: test-integration
test-integration: fmt vet envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test -v ./test/integration $(if $(RUN),-run $(RUN),) -coverprofile cover.integration.out

.PHONY: helm-dependency-build
helm-dependency-build:
	helm repo add open-webui https://helm.openwebui.com/
	helm dependency build ./charts/kubeai

# TEST_CASE is required for test-e2e target
.PHONY: test-e2e
test-e2e: skaffold helm-dependency-build
ifeq ($(TEST_CASE),)
	@echo "Error: TEST_CASE is required. Usage: make test-e2e TEST_CASE=<test-case>"
	@echo "Available test cases:"
	@ls -1 test/e2e/ | grep -v "\(common\|run.sh\|skaffold\|values\)" | sed 's/^/  - /'
	@exit 1
else
	$(if $(CACHE_PROFILE),CACHE_PROFILE=$(CACHE_PROFILE),) ./test/e2e/run.sh $(TEST_CASE)
endif

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name kubeai-builder
	$(CONTAINER_TOOL) buildx use kubeai-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm kubeai-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
CRD_REF_DOCS ?= $(LOCALBIN)/crd-ref-docs
SKAFFOLD ?= $(LOCALBIN)/skaffold
YQ ?= $(LOCALBIN)/yq

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.2
CONTROLLER_TOOLS_VERSION ?= v0.19.0
ENVTEST_VERSION ?= release-0.22
GOLANGCI_LINT_VERSION ?= v1.59.1
YQ_VERSION ?= v4.44.3

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: yq
yq: $(YQ) ## Download yq locally if necessary.
$(YQ): $(LOCALBIN)
	$(call go-install-tool,$(YQ),github.com/mikefarah/yq/v4,$(YQ_VERSION))

.PHONY: docs
docs: generate-kubernetes-api-reference
	cd ./docs && \
		python3 -m venv .venv && \
		source .venv/bin/activate && \
		pip install -r requirements.txt && \
		cd .. && \
		mkdocs serve

.PHONY: generate-kubernetes-api-reference
generate-kubernetes-api-reference: crd-ref-docs
	$(CRD_REF_DOCS) \
		--config=./docs/reference/.kubernetes-api/config.yaml \
		--log-level=INFO \
		--output-path=./docs/reference/kubernetes-api.md \
		--source-path=./api \
		--renderer=markdown
	# Replace the title
	perl -pi -e 's/# API Reference/# Kubernetes API/g' ./docs/reference/kubernetes-api.md

.PHONY: crd-ref-docs
crd-ref-docs: $(CRD_REF_DOCS) ## Download crd-ref-docs.
$(CRD_REF_DOCS): $(LOCALBIN)
	$(call go-install-tool,$(CRD_REF_DOCS),github.com/elastic/crd-ref-docs,$(CRD_REF_DOCS_VERSION))

SKAFFOLD_PLATFORM ?= $(shell go env GOOS)-$(shell go env GOARCH)
.PHONY: skaffold
skaffold: $(SKAFFOLD) ## Download skaffold.
$(SKAFFOLD): $(LOCALBIN)
	curl -Lo $(SKAFFOLD) https://storage.googleapis.com/skaffold/releases/$(SKAFFOLD_VERSION)/skaffold-$(SKAFFOLD_PLATFORM)
	chmod +x $(SKAFFOLD)

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
