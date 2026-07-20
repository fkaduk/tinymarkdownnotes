# Reusable Go project tasks. Override variables on the command line, for example:
#   make build APP_NAME=api CMD_PATH=./cmd/api

SHELL := /bin/sh
.DEFAULT_GOAL := help
.DELETE_ON_ERROR:
.SUFFIXES:

-include .env

APP_NAME ?= tinymarkdownnotes
CMD_PATH ?= .
PACKAGES ?= ./...
BIN_DIR ?= bin
BINARY ?= $(BIN_DIR)/$(APP_NAME)
COVERAGE_DIR ?= coverage
COVERAGE_PROFILE ?= $(COVERAGE_DIR)/coverage.out
COVERAGE_HTML ?= $(COVERAGE_DIR)/coverage.html

GO ?= go
GOFMT ?= gofmt
GO_BUILD_FLAGS ?= -trimpath
GO_TEST_FLAGS ?=
CGO_ENABLED ?= 1
GOLANGCI_LINT ?= golangci-lint
NPM ?= npm
NPX ?= npx
COMPOSE ?= docker compose

DATA_DIR ?= data
NOTES_UID ?= 1000
NOTES_GID ?= 1000

.PHONY: help all build install run generate \
	deps deps-e2e setup setup-e2e \
	fmt fmt-check vet lint verify test test-race test-cover coverage-html bench check ci \
	prepare-data fix-data-permissions init docker-build up down restart logs ps config \
	test-e2e test-e2e-headed test-e2e-ui clean clean-cache

help: ## Show the available targets.
	@awk 'BEGIN { FS = ":.*## "; printf "Usage: make \033[36m<target>\033[0m [VARIABLE=value]\n\nTargets:\n" } /^[a-zA-Z0-9_-]+:.*## / { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

all: build ## Build the application.

build: ## Build the application binary into BIN_DIR.
	@mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GO_BUILD_FLAGS) -o "$(BINARY)" "$(CMD_PATH)"

install: ## Install the application with go install.
	CGO_ENABLED=$(CGO_ENABLED) $(GO) install "$(CMD_PATH)"

run: ## Run the application locally.
	CGO_ENABLED=$(CGO_ENABLED) $(GO) run "$(CMD_PATH)"

generate: ## Run go generate for all packages.
	$(GO) generate $(PACKAGES)

deps: ## Download Go module dependencies.
	$(GO) mod download

deps-e2e: ## Install exact JavaScript dependencies from the lockfile.
	$(NPM) ci

setup-e2e: deps-e2e ## Install the Playwright browser used by the E2E suite.
	$(NPX) playwright install chromium

setup: deps setup-e2e ## Install all development dependencies.

fmt: ## Format all Go packages.
	$(GO) fmt $(PACKAGES)

fmt-check: ## Fail if any Go source file is not gofmt-formatted.
	@files="$$(find . -type f -name '*.go' -not -path './vendor/*')"; \
	unformatted="$$($(GOFMT) -l $$files)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet: ## Run Go's static analyzer.
	$(GO) vet $(PACKAGES)

lint: ## Run golangci-lint (must already be installed).
	@command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1 || { \
		echo "$(GOLANGCI_LINT) is not installed; see https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	$(GOLANGCI_LINT) run

verify: ## Verify downloaded Go modules against go.sum.
	$(GO) mod verify

test: ## Run all Go tests.
	$(GO) test $(GO_TEST_FLAGS) $(PACKAGES)

test-race: ## Run all Go tests with the race detector.
	CGO_ENABLED=1 $(GO) test -race $(GO_TEST_FLAGS) $(PACKAGES)

test-cover: ## Run Go tests and print a coverage summary.
	@mkdir -p "$(COVERAGE_DIR)"
	$(GO) test $(GO_TEST_FLAGS) -covermode=atomic -coverprofile="$(COVERAGE_PROFILE)" $(PACKAGES)
	$(GO) tool cover -func="$(COVERAGE_PROFILE)"

coverage-html: test-cover ## Write an HTML coverage report to COVERAGE_HTML.
	$(GO) tool cover -html="$(COVERAGE_PROFILE)" -o="$(COVERAGE_HTML)"
	@echo "Coverage report: $(COVERAGE_HTML)"

bench: ## Run all Go benchmarks with allocation statistics.
	$(GO) test -run='^$$' -bench=. -benchmem $(PACKAGES)

check: fmt-check vet test ## Run the fast local quality checks.

ci: verify check test-race test-e2e ## Run the complete CI check suite.

prepare-data: ## Create the host data directory and check its permissions.
	@mkdir -p "$(DATA_DIR)"
	@if [ ! -w "$(DATA_DIR)" ]; then \
		echo "$(DATA_DIR) is not writable; run 'make fix-data-permissions' explicitly."; \
		exit 1; \
	fi

fix-data-permissions: ## Make DATA_DIR writable by the configured container user.
	@if [ -z "$(DATA_DIR)" ] || [ "$(DATA_DIR)" = "/" ] || [ "$(DATA_DIR)" = "." ]; then \
		echo "Refusing unsafe DATA_DIR: $(DATA_DIR)"; \
		exit 1; \
	fi
	@mkdir -p "$(DATA_DIR)"
	sudo chown -R "$(NOTES_UID):$(NOTES_GID)" "$(DATA_DIR)"

init: prepare-data ## Backward-compatible alias for prepare-data.

docker-build: ## Build the Compose images.
	$(COMPOSE) build

up: prepare-data ## Build and start the Compose services in the background.
	$(COMPOSE) up -d --build

down: ## Stop and remove all Compose services.
	$(COMPOSE) --profile production down

restart: ## Restart the running Compose services.
	$(COMPOSE) restart

logs: ## Follow Compose service logs.
	$(COMPOSE) logs --tail=100 -f

ps: ## Show Compose service status.
	$(COMPOSE) ps

config: ## Render and validate the Compose configuration.
	$(COMPOSE) config

test-e2e: ## Run the Playwright end-to-end tests.
	$(NPM) run test:e2e

test-e2e-headed: ## Run Playwright in a visible browser.
	$(NPM) run test:e2e:headed

test-e2e-ui: ## Open Playwright's interactive test UI.
	$(NPM) run test:e2e:ui

clean: ## Remove generated binaries and coverage reports.
	@rm -f "$(BINARY)" "$(COVERAGE_PROFILE)" "$(COVERAGE_HTML)"
	@rmdir "$(BIN_DIR)" "$(COVERAGE_DIR)" 2>/dev/null || true

clean-cache: ## Clear Go's build and test caches.
	$(GO) clean -cache -testcache
