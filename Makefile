# Directory containing the Makefile.
PROJECT_ROOT = $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

export GOBIN ?= $(PROJECT_ROOT)/bin
export PATH := $(GOBIN):$(PATH)

GITHUB_USERNAME=tcuthbert
BINARY_NAME=apiserver

VERSION := $(shell git describe --dirty --tags)
COMMIT := $(shell git rev-parse --verify HEAD)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)

LDFLAGS = -ldflags "-X main.VERSION=${VERSION} -X main.COMMIT=${COMMIT} -X main.BRANCH=${BRANCH}"
GOVULNCHECK = $(GOBIN)/govulncheck
BENCH_FLAGS ?= -cpuprofile=cpu.pprof -memprofile=mem.pprof -benchmem

define semver =
		docker run --rm -v $$PWD:/tmp --workdir /tmp ghcr.io/caarlos0/svu $$@
endef

.PHONY: all
all: lint bench vulncheck build

.PHONY: lint
lint: golangci-lint tidy-lint

.PHONY: golangci-lint
golangci-lint:
	golangci-lint run ./...||true

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: tidy-lint
tidy-lint:
		go mod tidy && \
		git diff --exit-code -- go.mod go.sum||true

.PHONY: bench
BENCH ?= .
bench:
	go test -bench=. -run="^$$" $(BENCH_FLAGS)


$(GOVULNCHECK):
	cd tools && go install golang.org/x/vuln/cmd/govulncheck

.PHONY: vulncheck
vulncheck: $(GOVULNCHECK)
	$(GOVULNCHECK) ./...

.PHONY: test
test:
	@echo [test] NOT IMPLEMENTED

build:
	go build $(LDFLAGS) -o $(GOBIN)/$(BINARY_NAME)

.PHONY: release
release: build
	version="`$(semver) next --strip-prefix`" yq eval '(.images[] | select(.name == "apiserver") | .newTag) |= strenv(version)' -i kubernetes/kustomization.yaml
	git add kubernetes/kustomization.yaml && \
		git commit -S -m'bump: deployment' kubernetes/kustomization.yaml
	git tag -f `$(semver) next` && \
		git push origin --tags --force $(BRANCH)
	$(MAKE) argo-sync argo-wait

.PHONY: argo-create
argo-create:
	argocd app create $(BINARY_NAME) --repo https://github.com/tcuthbert/apiserver.git --path kubernetes --dest-server https://kubernetes.default.svc --dest-namespace default

.PHONY: argo-sync
argo-sync:
	argocd app sync $(BINARY_NAME)

.PHONY: argo-wait
argo-wait:
	argocd app wait $(BINARY_NAME)

.PHONY: clean
clean:
	go clean
	rm -fr $(PROJECT_ROOT)/bin || true
