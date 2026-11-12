# Accountedfor developer tasks. Run `make help` for a list.

VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT       ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_NUMBER ?= 0
BUILD_DATE   ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
IMAGE        ?= ghcr.io/lobojoel2003/accountedfor
PKG          := github.com/lobojoel2003/accountedfor/internal/version
LDFLAGS      := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) \
                -X $(PKG).BuildNumber=$(BUILD_NUMBER) -X $(PKG).BuildDate=$(BUILD_DATE)

# Test database used by `make test-db` (any PostgreSQL user that may create databases).
TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable

.PHONY: help build run test test-db cover lint fmt vuln dist docker up down clean version

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

build: ## Build bin/accountedfor with version information
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/accountedfor ./cmd/accountedfor

run: build ## Run the server (needs DATABASE_URL)
	./bin/accountedfor serve

test: ## Run unit tests (database tests are skipped)
	go test -race ./...

test-db: ## Run all tests including PostgreSQL integration tests
	ACCOUNTEDFOR_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -coverprofile=coverage.out ./...

cover: test-db ## Show test coverage
	go tool cover -func=coverage.out | tail -1

lint: ## gofmt, go vet, staticcheck and go mod tidy check
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run: gofmt -w ." && exit 1)
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
	go mod tidy -diff

fmt: ## Format the code
	gofmt -w .

vuln: ## Check dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

dist: ## Cross-compile release archives into dist/
	VERSION=$(VERSION) COMMIT=$(COMMIT) BUILD_NUMBER=$(BUILD_NUMBER) BUILD_DATE=$(BUILD_DATE) scripts/build-release.sh

docker: ## Build the container image
	docker build -t $(IMAGE):$(VERSION) \
	  --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) \
	  --build-arg BUILD_NUMBER=$(BUILD_NUMBER) --build-arg BUILD_DATE=$(BUILD_DATE) .

up: ## Start the app and PostgreSQL with Docker Compose (builds from source)
	VERSION=$(VERSION) docker compose up -d --build

down: ## Stop Docker Compose
	docker compose down

version: ## Print the version that would be built
	@echo $(VERSION) build $(BUILD_NUMBER)

clean: ## Remove build output
	go clean
	find bin dist -mindepth 1 -delete 2>/dev/null || true
	find . -maxdepth 1 -name coverage.out -delete
