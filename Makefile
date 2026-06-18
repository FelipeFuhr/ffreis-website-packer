SHELL := /usr/bin/env bash
.SHELLFLAGS := -e -o pipefail -c

GO ?= go
GO_IMAGE ?= golang:1.22
CONTAINER_RUNTIME ?= docker

.PHONY: help fmt-check test vet check check-container fmt-check-container test-container vet-container

help:
	@echo "Targets:"
	@echo "  make check                (fmt-check + vet + test)"
	@echo "  make check-container       (containerized fmt-check + vet + test)"
	@echo "  make test|test-container"

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [[ -n "$$unformatted" ]]; then \
		echo "Formatting required (run: gofmt -w .):"; \
		printf "%s\n" $$unformatted; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -shuffle=on ./... -count=1

check: fmt-check vet test

container-run = $(CONTAINER_RUNTIME) run --rm -t \
	-v "$(PWD):/work" -w /work \
	"$(GO_IMAGE)" \
	bash -lc

fmt-check-container:
	$(call container-run,'make fmt-check')

vet-container:
	$(call container-run,'make vet')

test-container:
	$(call container-run,'make test')

check-container:
	$(call container-run,'make check')


PLATFORM_STANDARDS_SHA ?= 3c787edb4e96ddea2e86b2add2c32139685e8db7  # v1.2.1
PLATFORM_STANDARDS_RAW ?= https://raw.githubusercontent.com/FelipeFuhr/ffreis-platform-standards

install-act: ## Download pinned act binary into .bin/
	@mkdir -p scripts
	@curl -fsSL "$(PLATFORM_STANDARDS_RAW)/$(PLATFORM_STANDARDS_SHA)/scripts/install_act.sh" \
		-o scripts/install_act.sh && chmod +x scripts/install_act.sh
	@bash ./scripts/install_act.sh

ci-local: ## Run workflows locally via act (GH Actions quota fallback). Args via ARGS=...
	@mkdir -p scripts
	@curl -fsSL "https://raw.githubusercontent.com/FelipeFuhr/ffreis-platform-ci-local/v1.0.0/scripts/run-ci-local.sh" \
		-o scripts/run-ci-local.sh && chmod +x scripts/run-ci-local.sh
	@CI_LOCAL_FINDINGS_REF=v1.0.0 PATH="$(CURDIR)/.bin:$(PATH)" bash ./scripts/run-ci-local.sh $(ARGS)
