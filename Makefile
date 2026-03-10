.PHONY: build build-local build-binary dev-release login test test-unit test-integration test-e2e test-all lint clean

REGISTRY  := ghcr.io/randybias
IMAGE     := $(REGISTRY)/tentacular-mcp
TAG       ?= latest
PLATFORMS := linux/amd64,linux/arm64
DOCKER    ?= docker

BINARY := bin/tentacular-mcp
MODULE := github.com/randybias/tentacular-mcp
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build-binary: ## Build the Go binary for the current platform
	go build $(LDFLAGS) -o $(BINARY) ./cmd/tentacular-mcp

test: test-unit ## Run unit tests (default)

test-unit: ## Unit tests (no cluster)
	go test ./pkg/... -v -count=1

test-integration: ## Integration tests (kind cluster)
	@test/integration/setup_kind.sh
	go test -tags=integration ./test/integration/... -v -timeout 300s -count=1
	@test/integration/teardown_kind.sh

test-e2e: ## E2E tests (production k0s cluster)
	@test -n "$$TENTACULAR_E2E_KUBECONFIG" || { echo "Set TENTACULAR_E2E_KUBECONFIG"; exit 1; }
	go test -tags=e2e ./test/e2e/... -v -timeout 600s -count=1

test-all: test-unit test-integration test-e2e ## Run all test tiers

lint: ## Run linters
	golangci-lint run ./...
	go vet ./...

clean: ## Clean build artifacts
	rm -rf bin/

build: ## Multi-arch build and push to GHCR (linux/amd64 + linux/arm64)
	$(DOCKER) buildx build \
		--platform $(PLATFORMS) \
		--tag $(IMAGE):$(TAG) \
		--tag $(IMAGE):$(shell git rev-parse --short HEAD) \
		--push \
		.

build-local: ## Single-arch build into local daemon (no push, for testing)
	$(DOCKER) build \
		--tag $(IMAGE):local \
		.

DEV_TAG := dev-$(shell git rev-parse --short HEAD)
dev-release: ## Multi-arch build, tag as dev-<sha>, push to GHCR
	$(DOCKER) buildx build \
		--platform $(PLATFORMS) \
		--build-arg VERSION=$(DEV_TAG) \
		--build-arg COMMIT=$(shell git rev-parse HEAD) \
		--build-arg BUILD_DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
		--tag $(IMAGE):$(DEV_TAG) \
		--push \
		.

login: ## Login to GHCR using gh CLI token
	gh auth token | $(DOCKER) login ghcr.io -u randybias --password-stdin

deploy: ## Deploy to current cluster
	kubectl apply -k deploy/manifests/

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
