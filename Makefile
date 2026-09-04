.PHONY: run build test tidy vet fmt check

# --- Phase 0 targets -------------------------------------------------------
# up / down / load arrive in Phase 3 with docker-compose.

run: ## Run the API locally
	go run ./cmd/api

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
