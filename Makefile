.PHONY: build build/all release release/all test test/coverage lint audit quality integration/start integration/stop integration/tests integration/test integration/test/query integration/test/log integration/test/field integration/test/values integration/test/ssh integration/test/native integration/test/hl integration/test/short integration/test/all integration/logs integration/start/logs integration/stop/logs integration/deploy-simulation integration/mcp/setup integration/mcp/test install uninstall

SHA=$(shell git rev-parse --short HEAD)
# Determine latest tag (fallback to '0.0.0' when repository has no tags or git fails)
LATEST_TAG:=$(shell git describe --tags --abbrev=0 2>/dev/null || echo 0.0.0)

# Default VERSION is <LATEST_TAG>-<SHA> unless overridden on the make command line
VERSION?=$(LATEST_TAG)-$(SHA)

# Installation defaults (can be overridden on the `make` command line)
PREFIX ?= /usr/local

# Path to the generated k3s kubeconfig (created by integration/infra/k8s/configure-kubeconfig.sh)
K3S_KUBECONFIG=integration/infra/k8s/k3s.yaml

# Build targets

build:
	@echo "building (debug-friendly) version $(VERSION) for current platform"
	@go build -ldflags "-X github.com/estran-studio/logviewer/cmd.sha1ver=$(VERSION)" -o build/logviewer


build/all:
	@echo "building (debug-friendly) version $(VERSION) for all platforms"
	@GOOS=linux GOARCH=arm64 go build -ldflags "-X github.com/estran-studio/logviewer/cmd.sha1ver=$(VERSION)" -o build/logviewer-linux-arm64
	@GOOS=linux GOARCH=amd64 go build -ldflags "-X github.com/estran-studio/logviewer/cmd.sha1ver=$(VERSION)" -o build/logviewer-linux-amd64
	@GOOS=darwin GOARCH=arm64 go build -ldflags "-X github.com/estran-studio/logviewer/cmd.sha1ver=$(VERSION)" -o build/logviewer-darwin-arm64
	@GOOS=darwin GOARCH=amd64 go build -ldflags "-X github.com/estran-studio/logviewer/cmd.sha1ver=$(VERSION)" -o build/logviewer-darwin-amd64


# Optimized / stripped release build (smaller binary, no DWARF/debug, trimmed paths)
# Usage: make release [VERSION=...] [CGO_ENABLED=0] [GOOS=...] [GOARCH=...] [OUTPUT=...]
OUTPUT ?= build/logviewer
release:
	@echo "building optimized release version $(VERSION) for $(or $(GOOS),current platform)/$(or $(GOARCH),current arch)"
	@mkdir -p build
	@CGO_ENABLED=${CGO_ENABLED-0} go build -trimpath -buildvcs=false \
		-ldflags "-s -w -X github.com/estran-studio/logviewer/cmd.sha1ver=$(VERSION)" \
		-o $(OUTPUT)
	@echo "binary size: $$(wc -c < $(OUTPUT)) bytes"
	@echo "(add optional compression: upx --best $(OUTPUT))"


# Optimized multi-platform build (stripped)
release/all:
	@echo "building optimized release version $(VERSION) for all platforms"
	@$(MAKE) release GOOS=linux GOARCH=arm64 OUTPUT=build/logviewer-linux-arm64
	@$(MAKE) release GOOS=linux GOARCH=amd64 OUTPUT=build/logviewer-linux-amd64
	@$(MAKE) release GOOS=darwin GOARCH=arm64 OUTPUT=build/logviewer-darwin-arm64
	@$(MAKE) release GOOS=darwin GOARCH=amd64 OUTPUT=build/logviewer-darwin-amd64
	@$(MAKE) release GOOS=windows GOARCH=amd64 OUTPUT=build/logviewer-windows-amd64.exe
	@$(MAKE) release GOOS=windows GOARCH=arm64 OUTPUT=build/logviewer-windows-arm64.exe


# Install the built binary to a system location.
# Usage: make install [PREFIX=/usr/local] [DESTDIR=]
# Example: make install PREFIX=/usr/local
install:
	@echo "Installing logviewer with: PREFIX='$(PREFIX)'"
	@mkdir -p "$$PREFIX"
	@cp -f build/logviewer "$$PREFIX"
	@chmod 0755 "$$PREFIX/logviewer"
	@echo "Installed to $$PREFIX/logviewer"

# Remove installed binary
uninstall:
	@echo "Uninstalling logviewer from: PREFIX='$(PREFIX)'"
	@rm -f "$$PREFIX/logviewer"
	@echo "Removed $$PREFIX/logviewer"





# Unit tests

test:
	@go test ./...

test/coverage:
	@command -v gocover-cobertura >/dev/null 2>&1 || { echo "Installing gocover-cobertura"; go install github.com/boumenot/gocover-cobertura@latest; }
	# Added -race and changed covermode to atomic (required for race)
	@go test -race -coverprofile=coverage.txt -covermode atomic ./...
	@cat coverage.txt | gocover-cobertura > coverage.xml

# Quality Checks
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "Installing golangci-lint"; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin; \
	}
	@echo "Running golangci-lint..."
	@golangci-lint run

audit:
	@command -v govulncheck >/dev/null 2>&1 || { echo "Installing govulncheck"; go install golang.org/x/vuln/cmd/govulncheck@latest; }
	@echo "Running govulncheck..."
	@govulncheck ./...

quality: lint audit test/coverage
	@echo "All quality checks passed!"







# Integration Environment Management

integration/start:
	@echo "Starting all integration services..."
	@bash integration/infra/ssh/generate-keys.sh
	@cd integration/infra && docker compose up -d
	@bash integration/infra/splunk/wait-for-splunk.sh
	@bash integration/infra/splunk/create-test-indexes.sh
	@bash integration/infra/opensearch/create-test-indexes.sh
	@bash integration/infra/k8s/configure-kubeconfig.sh
	@docker ps

integration/stop:
	@echo "Stopping all integration services..."
	@cd integration/infra && docker compose down -v
	@rm -rf ./integration/infra/splunk/.hec_token

# Service-specific start/stop
integration/start/splunk:
	@echo "Starting Splunk..."
	@cd integration/infra && docker compose up -d splunk

integration/stop/splunk:
	@echo "Stopping Splunk..."
	@cd integration/infra && docker compose stop splunk && docker compose rm -fv splunk
	@rm -f ./integration/infra/splunk/.hec_token

integration/start/opensearch:
	@echo "Starting OpenSearch and Dashboards..."
	@cd integration/infra && docker compose up -d opensearch opensearch-dashboards

integration/stop/opensearch:
	@echo "Stopping OpenSearch and Dashboards..."
	@cd integration/infra && docker compose stop opensearch opensearch-dashboards && docker compose rm -fv opensearch opensearch-dashboards

integration/start/ssh:
	@echo "Starting SSH server..."
	@bash integration/infra/ssh/generate-keys.sh
	@cd integration/infra && docker compose up -d ssh-server

integration/stop/ssh:
	@echo "Stopping SSH server..."
	@cd integration/infra && docker compose stop ssh-server && docker compose rm -fv ssh-server

integration/start/k8s:
	@echo "Starting k3s server..."
	@cd integration/infra && docker compose up -d k3s-server

integration/stop/k8s:
	@echo "Stopping k3s server..."
	@cd integration/infra && docker compose stop k3s-server && docker compose rm -fv k3s-server

# Service-specific start/stop
integration/start/cloudwatch:
	@echo "Starting LocalStack for CloudWatch..."
	@cd integration/infra && docker compose up -d localstack

integration/stop/cloudwatch:
	@echo "Stopping LocalStack..."
	@cd integration/infra && docker compose stop localstack && docker compose rm -f localstack

integration/start/logs:
	@echo "Starting log-generator..."
	@export SPLUNK_HEC_TOKEN=$$(cat ./integration/infra/splunk/.hec_token 2>/dev/null || echo "") && \
		cd integration/infra && docker compose -f docker-compose-log-generator.yml up -d

integration/stop/logs:
	@echo "Stopping log-generator..."
	@cd integration/infra && docker compose -f docker-compose-log-generator.yml down -v

integration/rebuild/logs:
	@echo "Rebuilding and redeploying log-generator with latest changes..."
	@cd integration/infra && docker compose -f docker-compose-log-generator.yml down || true
	@cd integration/infra && docker compose -f docker-compose-log-generator.yml build --no-cache log-generator
	@export SPLUNK_HEC_TOKEN=$$(cat ./integration/infra/splunk/.hec_token 2>/dev/null || echo "") && \
		cd integration/infra && docker compose -f docker-compose-log-generator.yml up -d
	@echo "Waiting for log-generator to be ready..."
	@sleep 3
	@echo "Testing /health endpoint..."
	@curl -f http://localhost:8081/health 2>/dev/null && echo "\n✅ Log-generator ready with new code!" || echo "\n⚠️  /health endpoint not available"

integration/logs/tail:
	@echo "Tailing logs from all integration services..."
	@cd integration/infra && docker compose logs --tail=50 -f

integration/logs/generator-tail:
	@echo "Tailing log-generator logs..."
	@cd integration/infra && docker compose -f docker-compose-log-generator.yml logs --tail=50 -f log-generator

integration/deploy-simulation:
	@echo "Building simulator image..."
	@docker build -t log-generator:latest integration/infra/log-generator
	
	@echo "Importing image to k3s..."
	@docker save log-generator:latest | docker exec -i k3s-server ctr images import -

	@echo "Applying K8s manifests..."
	@if [ -f integration/infra/splunk/.hec_token ]; then \
		export TOKEN=$$(cat integration/infra/splunk/.hec_token) && \
		export SPLUNK_IP=$$(docker inspect splunk --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}') && \
		export OPENSEARCH_IP=$$(docker inspect opensearch --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}') && \
		export LOCALSTACK_IP=$$(docker inspect localstack --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}') && \
		sed -e "s/YOUR_HEC_TOKEN_HERE/$$TOKEN/g" \
		    -e "s|https://splunk:8088|https://$$SPLUNK_IP:8088|g" \
		    -e "s|http://opensearch:9200|http://$$OPENSEARCH_IP:9200|g" \
		    -e "s|http://localstack:4566|http://$$LOCALSTACK_IP:4566|g" \
		    integration/infra/k8s/app.yaml | \
		KUBECONFIG=$(K3S_KUBECONFIG) kubectl apply -f -; \
	else \
		echo "ERROR: Splunk HEC token not found. Run 'make integration/start' first."; \
		exit 1; \
	fi

	@echo "Simulation deployed! Logs are flowing."


# Log Generation and Uploading
integration/logs: integration/logs/generator integration/logs/ssh integration/logs/cloudwatch

integration/logs/cloudwatch:
	@echo "Sending logs to CloudWatch..."
	@cd integration/infra/cloudwatch && ./send-logs.sh

integration/logs/generator: integration/start/logs
	@echo "Deploying sample logs to Splunk and OpenSearch via log-generator..."
	@for i in $$(seq 1 30); do \
		if curl -s http://localhost:8081 >/dev/null; then break; fi; \
		echo "Waiting for log-generator..."; \
		sleep 1; \
	done
	@curl -s -o /dev/null -G --data-urlencode "message=User 'alice' logged in successfully" http://localhost:8081/log/info
	@curl -s -o /dev/null -G --data-urlencode "message=Payment failed for order #12345: Insufficient funds" http://localhost:8081/log/error
	@curl -s -o /dev/null -H "X-Request-ID: xyz-987-abc" -G --data-urlencode "message=API key is approaching expiration date" http://localhost:8081/log/warn

integration/logs/ssh:
	@echo "Uploading logs to SSH server..."
	@cd integration/infra/ssh && ./upload-log.sh

integration/tests: build
	@echo "Running legacy shell integration tests..."
	@bash integration/legacy/test-all.sh

# Go-based integration tests (new approach)
integration/test: build
	@echo "Running Go-based integration tests..."
	@echo "Ensure Docker services are running: make integration/start"
	@LOGVIEWER_TLS_INSECURE=true go test -v -tags=integration ./integration/tests/e2e/... -timeout 30m

integration/test/query: build
	@echo "Running query-related tests..."
	@LOGVIEWER_TLS_INSECURE=true TEST_FIXTURES="error-logs,payment-logs,order-logs" go test -v -tags=integration ./integration/tests/e2e/... -run TestQuery -timeout 10m

integration/test/log: build
	@echo "Running log query tests..."
	@LOGVIEWER_TLS_INSECURE=true TEST_FIXTURES="error-logs,payment-logs,order-logs" go test -v -tags=integration ./integration/tests/e2e/... -run TestQueryLog -timeout 10m

integration/test/field: build
	@echo "Running field query tests..."
	@LOGVIEWER_TLS_INSECURE=true TEST_FIXTURES="payment-logs,order-logs" go test -v -tags=integration ./integration/tests/e2e/... -run TestQueryField -timeout 10m

integration/test/values: build
	@echo "Running values query tests..."
	@LOGVIEWER_TLS_INSECURE=true TEST_FIXTURES="payment-logs,order-logs,mixed-levels" go test -v -tags=integration ./integration/tests/e2e/... -run TestQueryValues -timeout 10m

integration/test/ssh: build
	@echo "Running SSH backend tests..."
	@# SSH tests don't use standard fixtures
	@LOGVIEWER_TLS_INSECURE=true TEST_FIXTURES="" go test -v -tags=integration ./integration/tests/e2e/... -run TestSSH -timeout 10m

integration/test/native: build
	@echo "Running native query tests..."
	@LOGVIEWER_TLS_INSECURE=true TEST_FIXTURES="error-logs,payment-logs,order-logs" go test -v -tags=integration ./integration/tests/e2e/... -run TestNative -timeout 10m

integration/test/hl: build
	@echo "Running HL query syntax tests..."
	@LOGVIEWER_TLS_INSECURE=true TEST_FIXTURES="error-logs,payment-logs,order-logs" go test -v -tags=integration ./integration/tests/e2e/... -run TestHL -timeout 10m

integration/test/short: build
	@echo "Running short integration tests (quick smoke test)..."
	@LOGVIEWER_TLS_INSECURE=true go test -v -tags=integration -short ./integration/tests/e2e/... -timeout 5m

# Run all tests (legacy + new Go tests)
integration/test/all: integration/tests integration/test


# MCP Agent Integration Tests (requires Ollama)

integration/mcp/setup:
	@echo "Setting up MCP integration tests..."
	@command -v ollama >/dev/null 2>&1 || { echo "ERROR: Ollama not installed. Visit https://ollama.ai/download"; exit 1; }
	@echo "Pulling llama3.1 model (this may take a while)..."
	@ollama pull llama3.1
	@echo "MCP integration setup complete."

integration/mcp/test:
	@echo "Running MCP agent integration tests..."
	@command -v ollama >/dev/null 2>&1 || { echo "ERROR: Ollama not installed. Run 'make integration/mcp/setup' first."; exit 1; }
	@OLLAMA_MODEL=llama3.1 go test ./cmd/... -run TestMCPAgent -v

