# Makefile for cloudnative-cluster-api-provider-cce
SHELL := /usr/bin/env bash
GO ?= go
GOFLAGS ?= -mod=readonly

BIN_DIR := bin
MANAGER_BIN := $(BIN_DIR)/manager
CONTROLLER_GEN ?= $(BIN_DIR)/controller-gen
CONTROLLER_TOOLS_VERSION ?= v0.21.0
IMG ?= swr.cn-north-4.myhuaweicloud.com/$(IMAGE_ORG)/cce-provider-controller:latest

.PHONY: all
all: generate manifests build

##@ Build

.PHONY: build
build: ## Build manager binary
	$(GO) build -o $(MANAGER_BIN) cmd/main.go

.PHONY: run
run: ## Run manager locally
	$(GO) run ./cmd/main.go

.PHONY: docker-build
docker-build: ## Build docker image
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push docker image
	docker push $(IMG)

##@ Generate

.PHONY: generate
generate: controller-gen ## Generate deepcopy code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: manifests
manifests: controller-gen ## Generate CRD/RBAC/webhook manifests
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=manager-role paths="./..." output:rbac:artifacts:config=config/rbac
	$(CONTROLLER_GEN) webhook paths="./..." output:webhook:artifacts:config=config/webhook

.PHONY: controller-gen
controller-gen: ## Install controller-gen
	GOBIN=$(shell pwd)/$(BIN_DIR) $(GO) install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

##@ Verify

# envtest binaries (kube-apiserver/etcd) for the controller test suite.
# The version tracks the k8s.io dependency line (go.mod -> v0.36.x).
ENVTEST_K8S_VERSION ?= 1.36.x
ENVTEST ?= $(BIN_DIR)/setup-envtest
KUBEBUILDER_ASSETS ?= $(shell $(ENVTEST) use -p path $(ENVTEST_K8S_VERSION) 2>/dev/null)

.PHONY: envtest
envtest: ## Install setup-envtest (envtest binaries provider)
	GOBIN=$(shell pwd)/$(BIN_DIR) $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: test
test: envtest ## Run unit tests (incl. envtest controller suite)
	KUBEBUILDER_ASSETS="$(KUBEBUILDER_ASSETS)" $(GO) test ./...

.PHONY: test-controllers
test-controllers: envtest ## Run only the envtest controller suite
	KUBEBUILDER_ASSETS="$(KUBEBUILDER_ASSETS)" $(GO) test ./controllers/... -v

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Run gofmt
	gofmt -w cmd/ api/ controllers/ internal/

.PHONY: lint
lint: vet ## Run linters (extend with golangci-lint in CI)

.PHONY: e2e
e2e: ## Run e2e tests (requires a management cluster and CCE credentials)
	$(GO) test -tags e2e ./test/e2e/... -timeout 60m

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
