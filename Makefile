.PHONY: run build test tidy vet fmt check

# --- Phase 0 targets -------------------------------------------------------
# up / down / load arrive in Phase 3 with docker-compose.

# ENV_FILE picks which env profile to load, e.g.:
#   make run                        -> .env (your local overrides, if present)
#   ENV_FILE=.env.chaos make run     -> a different profile, once one exists
# Values already exported in your shell always win over the file.
ENV_FILE ?= .env

run: ## Run the API locally, loading $(ENV_FILE) if present
	@if [ -f "$(ENV_FILE)" ]; then \
		echo "loading $(ENV_FILE)"; \
		set -a; . ./$(ENV_FILE); set +a; go run ./cmd/api; \
	else \
		go run ./cmd/api; \
	fi

build: ## Build the API binary into ./bin/api
	go build -o bin/api ./cmd/api

test: ## Run all tests
	go test ./...

tidy: ## Sync go.mod / go.sum
	go mod tidy

vet: ## Run go vet
	go vet ./...

fmt: ## Format all Go source
	gofmt -w .

check: fmt vet test ## Format, vet, and test in one shot
