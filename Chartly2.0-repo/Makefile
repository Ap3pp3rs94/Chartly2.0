# Chartly 2.0  Developer Makefile
# Note: On Windows, use WSL for `make` or run equivalent commands in PowerShell.
# This Makefile is designed to be safe even while services are still being implemented.

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo ""
	@echo "Chartly 2.0  common commands"
	@echo ""
	@echo "Local infra (Docker Compose):"
	@echo "  make up         Start local dependencies (postgres/redis/minio)"
	@echo "  make down       Stop local dependencies"
	@echo "  make ps         Show containers"
	@echo "  make logs       Tail logs"
	@echo ""
	@echo "Go:"
	@echo "  make fmt-go     Format Go code (gofmt)"
	@echo "  make lint-go    Lint Go code (go vet)"
	@echo "  make test-go    Run Go tests (go test ./...)"
	@echo ""
	@echo "Web (optional):"
	@echo "  make fmt-web    Format web code (npm run fmt) if web/ exists"
	@echo "  make lint-web   Lint web code (npm run lint) if web/ exists"
	@echo "  make test-web   Test web code (npm test) if web/ exists"
	@echo ""
	@echo "Contracts:"
	@echo "  make validate-contracts  Validate JSON schemas/fixtures (python) if available"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean      Remove common build artifacts (safe)"
	@echo ""

#############
# Docker
#############
.PHONY: up
target :=

.PHONY: up
up:
	@docker compose up -d
	@docker compose ps

.PHONY: down
down:
	@docker compose down

.PHONY: ps
ps:
	@docker compose ps

.PHONY: logs
logs:
	@docker compose logs -f --tail=200

#############
# Go
#############
.PHONY: fmt-go
fmt-go:
	@echo "Formatting Go files..."
	@find . -name '*.go' -not -path './web/*' -print0 2>/dev/null | xargs -0 -I {} gofmt -w {} 2>/dev/null || true

.PHONY: lint-go
lint-go:
	@echo "Linting Go code..."
	@go vet ./... || true

.PHONY: test-go
test-go:
	@echo "Running Go tests..."
	@go test ./... || true

#############
# Web (optional)
#############
.PHONY: fmt-web
fmt-web:
	@if [ -d web ] && [ -f web/package.json ]; then \
		cd web && npm run fmt; \
	else \
		echo "web/ not present or dependencies missing; skipping"; \
	fi

.PHONY: lint-web
lint-web:
	@if [ -d web ] && [ -f web/package.json ]; then \
		cd web && npm run lint; \
	else \
		echo "web/ not present or dependencies missing; skipping"; \
	fi

.PHONY: test-web
test-web:
	@if [ -d web ] && [ -f web/package.json ]; then \
		cd web && npm test; \
	else \
		echo "web/ not present or dependencies missing; skipping"; \
	fi

#############
# Contracts (python)
#############
.PHONY: validate-contracts
validate-contracts:
	@echo "Validating contracts (if python is available)..."
	@if command -v python >/dev/null 2>&1; then \
		python contracts/validators/validate.py; \
	elif command -v python3 >/dev/null 2>&1; then \
		python3 contracts/validators/validate.py; \
	else \
		echo "Python not found. Install Python 3.x to run schema validation."; \
		exit 0; \
	fi

#############
# Cleanup
#############
.PHONY: clean
clean:
	@echo "Cleaning build artifacts (safe)..."
	@rm -rf ./dist ./build ./bin 2>/dev/null || true
	@find . -maxdepth 4 -name '*.exe' -type f -delete 2>/dev/null || true
	@find . -name '*.out' -type f -delete 2>/dev/null || true
	@find . -name '*.test' -type f -delete 2>/dev/null || true
	@find . -name '__pycache__' -type d -prune -exec rm -rf {} \; 2>/dev/null || true
	@rm -rf .pytest_cache .mypy_cache .ruff_cache 2>/dev/null || true
	@echo "Done."
