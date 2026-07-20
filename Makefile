SHELL := /bin/sh
.DEFAULT_GOAL := all

GO ?= go
GOFMT ?= gofmt
NPM ?= npm
BINARY ?= bin/tinymarkdownnotes

.PHONY: all audit test build run

all: audit test build

audit:
	@files="$$(find . -type f -name '*.go' -not -path './vendor/*')"; \
	unformatted="$$($(GOFMT) -l $$files)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	$(GO) mod tidy -diff
	$(GO) vet ./...

test:
	CGO_ENABLED=1 $(GO) test -count=1 -shuffle=on ./...
	$(NPM) run test:e2e

build:
	@mkdir -p "$$(dirname "$(BINARY)")"
	CGO_ENABLED=1 $(GO) build -trimpath -o "$(BINARY)" .

run:
	CGO_ENABLED=1 $(GO) run .
