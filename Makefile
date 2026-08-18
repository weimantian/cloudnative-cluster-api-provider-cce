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

.PHONY: test
test: ## Run unit tests
	$(GO) test ./...

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
	$(GO) test ./test/e2e/... -timeout 60m

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
