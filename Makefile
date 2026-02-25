SHELL := /bin/bash
GOFILES := $(shell find . -name '*.go' -not -path './vendor/*')
ACTIONLINT := $(shell command -v actionlint)
ACT := $(shell command -v act)

.PHONY: fmt vet test lint-workflows validate-workflows validate tools

fmt:
	gofmt -w $(GOFILES)

vet:
	go vet ./...

test:
	go test ./...

lint-workflows:
ifndef ACTIONLINT
	$(error actionlint not found. Install with: brew install actionlint || go install github.com/rhysd/actionlint/cmd/actionlint@latest)
endif
	$(ACTIONLINT)

validate-workflows:
ifndef ACT
	$(error act not found. Install with: brew install act || go install github.com/nektos/act@latest)
endif
	$(ACT) pull_request --eventpath .github/testdata/pull_request.json --dryrun \
	  -P ubuntu-latest=ghcr.io/catthehacker/ubuntu:act-latest \
	  --container-architecture linux/amd64

validate: fmt vet test lint-workflows validate-workflows

# Helper to show tool availability

tools:
	@echo "actionlint: $(ACTIONLINT)"
	@echo "act: $(ACT)"
