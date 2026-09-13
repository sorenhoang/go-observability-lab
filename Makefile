.PHONY: run build test tidy vet fmt check check-config up down load spike dash dash-infra logs traces

# --- Phase 0-3 targets -----------------------------------------------------

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

check-config: ## Validate Prometheus rules, Alertmanager, and Loki config
	docker run --rm --entrypoint promtool -v $(PWD)/prometheus:/p prom/prometheus:v3.7.3 check rules /p/rules/recording.yml /p/rules/alerts.yml
	docker run --rm --entrypoint amtool -v $(PWD)/alertmanager:/a prom/alertmanager:v0.28.1 check-config /a/alertmanager.yml
	docker run --rm -v $(PWD)/loki:/etc/loki grafana/loki:3.7.7 -config.file=/etc/loki/loki-config.yml -verify-config
	docker run --rm -v $(PWD)/tempo:/etc/tempo grafana/tempo:2.10.8 -config.file=/etc/tempo/tempo-config.yml -config.verify=true

up: ## Start app + prometheus + alertmanager + grafana
	docker compose up --build -d

down: ## Stop the Docker Compose stack
	docker compose down

load: ## Start the k6 load generator against the running stack
	docker compose --profile load run --rm --no-deps k6

spike: ## Run the k6 traffic-spike scenario
	docker compose --profile load run --rm --no-deps k6 run /scripts/spike.js

dash: ## Open the Grafana RED dashboard
	open http://localhost:3000/d/red

dash-infra: ## Open the Grafana infrastructure dashboard
	open http://localhost:3000/d/infra

logs: ## Open Grafana Explore against Loki
	open "http://localhost:3000/explore?left=%7B%22datasource%22:%22loki%22%7D"

traces: ## Open Grafana Explore against Tempo
	open "http://localhost:3000/explore?left=%7B%22datasource%22:%22tempo%22%7D"
