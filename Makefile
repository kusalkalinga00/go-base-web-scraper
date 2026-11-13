GO ?= go
COMPOSE ?= docker compose

.PHONY: help fmt fmt-check vet test test-race build run deps-up deps-down docker-up docker-down check

help:
	@echo "Targets:"
	@echo "  make fmt        Format Go code"
	@echo "  make fmt-check  Fail if any file needs gofmt"
	@echo "  make vet        Run go vet"
	@echo "  make test       Run unit tests"
	@echo "  make test-race  Run unit tests with the race detector"
	@echo "  make build      Build all packages"
	@echo "  make run        Run the API (expects exported env vars)"
	@echo "  make deps-up    Start PostgreSQL and Redis with Docker Compose"
	@echo "  make deps-down  Stop PostgreSQL and Redis"
	@echo "  make docker-up  Build and start the full stack"
	@echo "  make docker-down Stop the full stack"
	@echo "  make check      fmt-check + vet + test-race + build"

fmt:
	gofmt -w .

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; echo "$$unformatted"; exit 1; \
	fi

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

build:
	$(GO) build ./...

run:
	$(GO) run ./cmd/api

deps-up:
	$(COMPOSE) up -d postgres redis

deps-down:
	$(COMPOSE) stop postgres redis

docker-up:
	$(COMPOSE) up --build

docker-down:
	$(COMPOSE) down

check: fmt-check vet test-race build
