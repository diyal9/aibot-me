# ==========================================
# AI Digital Human - Build & Deploy
# Single entry point for all operations
# Usage: make [target]
# ==========================================

VERSION ?= 0.5.0
PROJECT_DIR := $(shell pwd)

.PHONY: all build deploy verify status clean help

help: ## Show this help
	@echo "AI Digital Human - Build & Deploy"
	@echo ""
	@echo "Targets:"
	@echo "  make build      Build with version injection"
	@echo "  make deploy     Build + deploy + verify"
	@echo "  make verify     Run verification gate only"
	@echo "  make status     Show current deployment status"
	@echo "  make clean      Remove build artifacts"
	@echo "  make help       Show this help"
	@echo ""
	@echo "Options:"
	@echo "  VERSION=x.y.z   Set version (default: 0.5.0)"

all: build deploy ## Build and deploy

build: ## Build: version inject + compile + generate BUILD_INFO
	@chmod +x scripts/build.sh scripts/deploy.sh scripts/verify.sh
	@./scripts/build.sh $(VERSION)

deploy: ## Deploy: stop old → start new → verify
	@chmod +x scripts/build.sh scripts/deploy.sh scripts/verify.sh
	@./scripts/deploy.sh

verify: ## Run verification gate
	@chmod +x scripts/verify.sh
	@./scripts/verify.sh

status: ## Show current deployment status
	@echo "=== Deployment Status ==="
	@echo ""
	@echo "--- BUILD_INFO ---"
	@if [ -f BUILD_INFO ]; then cat BUILD_INFO; else echo "  (none)"; fi
	@echo ""
	@echo "--- Running Services ---"
	@echo "  Backend PID: $$(pgrep -f backend_bin 2>/dev/null || echo 'not running')"
	@echo "  TTS PID:     $$(pgrep -f supertonic-server 2>/dev/null || echo 'not running')"
	@echo ""
	@echo "--- Served Version ---"
	@echo "  Backend: $$(curl -s http://127.0.0.1:8085/ 2>/dev/null | grep -oP 'v\d+\.\d+\.\d+' | head -1 || echo 'unreachable')"
	@echo "  Nginx:   $$(curl -sk https://127.0.0.1:8089/human/ 2>/dev/null | grep -oP 'v\d+\.\d+\.\d+' | head -1 || echo 'unreachable')"
	@echo ""
	@echo "--- File Consistency ---"
	@if [ -L frontend/index.html ]; then \
		echo "  index.html: symlink → $$(readlink frontend/index.html)"; \
	else \
		echo "  index.html: REGULAR FILE (⚠️ may be stale!)"; \
	fi
	@echo ""

clean: ## Remove build artifacts
	@rm -f BUILD_INFO backend/backend_bin
	@echo "Cleaned."
