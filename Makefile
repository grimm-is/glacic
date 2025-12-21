# Glacic Firewall - Unified Build & Test System
#
# Usage:
#   make              - Show help
#   make build        - Build everything (UI + Go binary)
#   make test         - Run unit tests
#   make test-int     - Run integration tests (requires VM)
#   make demo         - Run mock demo (no VM, macOS compatible)
#   make demo-vm      - Run live VM demo with virtual network
#   make clean        - Clean build artifacts

.PHONY: help build build-ui build-go test test-unit test-int demo demo-vm clean \
        vm-setup vm-start vm-stop lint fmt check dev

# Colors
BLUE := \033[0;34m
GREEN := \033[0;32m
YELLOW := \033[1;33m
RED := \033[0;31m
NC := \033[0m

# Brand configuration (loaded from brand.json)
BRAND_NAME := $(shell jq -r '.name' internal/brand/brand.json)
BRAND_LOWER := $(shell jq -r '.lowerName' internal/brand/brand.json)
BRAND_BINARY := $(shell jq -r '.binaryName' internal/brand/brand.json)

# Configuration
BUILD_DIR := build
BINARY := $(BUILD_DIR)/$(BRAND_BINARY)
BINARY_DARWIN := $(BUILD_DIR)/$(BRAND_BINARY)-darwin
UI_DIR := ui
SCRIPTS_DIR := scripts
VM_IMAGE := $(BUILD_DIR)/rootfs.qcow2

# Default target
help:
	@echo ""
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo "$(BLUE)  $(BRAND_NAME) - Build & Test System$(NC)"
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@echo "$(YELLOW)Build Commands:$(NC)"
	@echo "  make build         Build everything (UI + Go binary for Linux)"
	@echo "  make iso           Build bootable installer ISO"
	@echo "  make build-ui      Build Svelte UI only"
	@echo "  make build-go      Build Go binary for Linux (default target)"
	@echo "  make build-darwin  Build Go binary for macOS (stub mode)"
	@echo ""
	@echo "$(YELLOW)Test Commands:$(NC)"
	@echo "  make test              Run all unit tests (Go tests)"
	@echo "  make test-int          Run integration tests in VM"
	@echo "  make test-int FILTER=dns  Run only tests matching 'dns'"
	@echo "  make test-all          Run unit + integration tests"
	@echo ""
	@echo "$(YELLOW)Demo Commands:$(NC)"
	@echo "  make demo          Run TUI demo (mock data, macOS compatible)"
	@echo "  make demo-web      Run web UI demo (mock API, macOS compatible)"
	@echo "  make demo-vm       Run full VM demo with virtual network"
	@echo ""
	@echo "$(YELLOW)Development:$(NC)"
	@echo "  make dev           Start dev environment (build + VM + hot reload)"
	@echo "  make lint          Run linters (go vet, golangci-lint)"
	@echo "  make fmt           Format code (go fmt, prettier)"
	@echo "  make check         Run all checks (lint + test)"
	@echo ""
	@echo "$(YELLOW)VM Management:$(NC)"
	@echo "  make vm-setup      Download and prepare Alpine VM image"
	@echo "  make vm-start      Start VM in background (simple)"
	@echo "  make dev-vm        Start full dev simulated VM (robust)"
	@echo "  make vm-stop       Stop running VM"
	@echo ""
	@echo "$(YELLOW)Installation:$(NC)"
	@echo "  make install-lxc   Install as Proxmox LXC (run on Proxmox host)"
	@echo ""
	@echo "$(YELLOW)Cleanup:$(NC)"
	@echo "  make clean         Remove build artifacts"
	@echo "  make clean-all     Remove build artifacts + VM images"
	@echo ""

# ==============================================================================
# Build Targets
# ==============================================================================

# Detect host architecture for cross-compilation
HOST_ARCH := $(shell uname -m)
ifeq ($(HOST_ARCH),arm64)
    LINUX_ARCH := arm64
else ifeq ($(HOST_ARCH),aarch64)
    LINUX_ARCH := arm64
else
    LINUX_ARCH := amd64
endif

# Build info for ldflags
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
GIT_MERGE_BASE := $(shell git merge-base HEAD origin/main 2>/dev/null | head -c 7 || echo "unknown")
LDFLAGS := -X 'grimm.is/glacic/internal/brand.Version=$(VERSION)' \
           -X 'grimm.is/glacic/internal/brand.BuildTime=$(BUILD_TIME)' \
           -X 'grimm.is/glacic/internal/brand.BuildArch=linux/$(LINUX_ARCH)' \
           -X 'grimm.is/glacic/internal/brand.GitCommit=$(GIT_COMMIT)' \
           -X 'grimm.is/glacic/internal/brand.GitBranch=$(GIT_BRANCH)' \
           -X 'grimm.is/glacic/internal/brand.GitMergeBase=$(GIT_MERGE_BASE)'

build: build-ui build-go
	@echo "$(GREEN)✓ Build complete$(NC)"

build-ui:
	@echo "$(BLUE)Building UI...$(NC)"
	@cd $(UI_DIR) && \
		if [ ! -d node_modules ]; then npm install --silent; fi && \
		npm run build --silent
	@echo "$(GREEN)✓ UI built$(NC)"

build-go:
	@echo "$(BLUE)Building Go binary (Linux/$(LINUX_ARCH))...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_ARCH) go build -ldflags "$(LDFLAGS)" -o $(BINARY) .
	@echo "$(GREEN)✓ Binary built: $(BINARY)$(NC)"

build-darwin:
	@echo "$(BLUE)Building Go binary (macOS/$(HOST_ARCH) - stub mode)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BINARY_DARWIN) .
	@echo "$(GREEN)✓ Binary built: $(BINARY_DARWIN)$(NC)"

build-qemu-exit:
	@echo "$(BLUE)Building qemu-exit helper...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/qemu-exit-amd64 ./cmd/qemu-exit
	@GOOS=linux GOARCH=arm64 go build -o $(BUILD_DIR)/qemu-exit-arm64 ./cmd/qemu-exit
	@echo "$(GREEN)✓ qemu-exit built for amd64 and arm64$(NC)"

build-toolbox:
	@echo "$(BLUE)Building Toolbox...$(NC)"
	@mkdir -p build
	@# Host build (for Orchestrator)
	@go build -ldflags "$(LDFLAGS)" -o build/toolbox ./cmd/toolbox
	@ln -sf toolbox build/glacic-orca
	@echo "$(GREEN)✓ Host toolbox built: build/toolbox (-> glacic-orchestrator)$(NC)"

	@# Guest build (for Agent/Harness) - Static Linux
	@CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_ARCH) go build -ldflags "$(LDFLAGS)" -o build/toolbox-linux ./cmd/toolbox
	@# Create symlinks in build dir for easy testing/inspection
	@ln -sf toolbox-linux build/agent
	@ln -sf toolbox-linux build/prove
	@echo "$(GREEN)✓ Guest toolbox built: build/toolbox-linux (-> agent, prove)$(NC)"

brand-env:
	@echo "$(BLUE)Generating brand.env from brand.json...$(NC)"
	@jq -r 'to_entries[] | "\(.key)=\(.value | @sh)"' internal/brand/brand.json | \
		while IFS='=' read -r key val; do \
			upper_key=$$(echo "BRAND_$$key" | sed 's/\([a-z]\)\([A-Z]\)/\1_\2/g' | tr '[:lower:]' '[:upper:]'); \
			echo "$$upper_key=$$val"; \
		done > internal/brand/brand.env
	@echo "$(GREEN)✓ brand.env generated$(NC)"

iso: build-iso

# ==============================================================================
# Test Targets
# ==============================================================================

test: test-unit
	@echo "$(GREEN)✓ All tests passed$(NC)"

test-unit:
	@echo "$(BLUE)Running unit tests...$(NC)"
	@go test -v ./internal/... 2>&1 | grep -E '^(ok|FAIL|---|===|PASS)' || true
	@go test ./internal/... -count=1

build-tests:
	@echo "$(BLUE)Pre-compiling Go test binaries (Linux/$(LINUX_ARCH))...$(NC)"
	@mkdir -p $(BUILD_DIR)/tests
	@for pkg in $$(go list ./internal/... 2>/dev/null); do \
		name=$$(basename $$pkg); \
		echo "  Compiling $$name..."; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_ARCH) go test -c -cover -o $(BUILD_DIR)/tests/$$name.test $$pkg 2>/dev/null || true; \
	done
	@echo "$(GREEN)✓ Test binaries built in $(BUILD_DIR)/tests/$(NC)"

test-int-legacy: vm-ensure build-go build-tests brand-env
	@echo "$(BLUE)Running integration tests in VM (Alpine)...$(NC)"
ifdef FILTER
	@echo "$(YELLOW)Filter: $(FILTER)$(NC)"
	@echo "$(FILTER)" > .test_filter
endif
	@t/run-tests.sh unit; EXIT_CODE=$$?; \
		if [ -n "$(FILTER)" ]; then rm -f .test_filter; fi; \
		exit $$EXIT_CODE

test-int-rerun: vm-ensure build-go build-tests brand-env
	@echo "$(BLUE)Rerunning failed integration tests...$(NC)"
	@t/run-tests.sh rerun

test-int-verbose: vm-ensure build-go build-tests brand-env
	@echo "$(BLUE)Running integration tests in verbose mode...$(NC)"
	@t/run-tests.sh verbose

# New Go-based parallel test orchestrator
test-int: vm-ensure build-go build-tests brand-env build-toolbox
	@mkdir -p build/test-artifacts
	@cp build/glacic build/test-artifacts/glacic-v1
	@cp build/glacic build/test-artifacts/glacic-v2
	@echo "$(BLUE)Calculating optimal parallelism...$(NC)"
	@JOBS=$$(./scripts/calc_jobs.sh); \
	 echo "$(BLUE)Running integration tests via parallel orca (JOBS=$$JOBS)...$(NC)"; \
	 ./build/toolbox orca test -j $$JOBS $(ARGS)

test-orca: test-int

test-uroot:
	@echo "$(BLUE)Running Go integration tests in VM (u-root)...$(NC)"
	@$(SCRIPTS_DIR)/uroot-test.sh ./internal/firewall

test-uroot-shell:
	@echo "$(BLUE)Running shell integration tests in VM (u-root)...$(NC)"
	@$(SCRIPTS_DIR)/uroot-test.sh --shell validation_test.sh

test-all: test-unit test-int
	@echo "$(GREEN)✓ All tests passed$(NC)"

# ==============================================================================
# Demo Targets
# ==============================================================================

demo: build-tuidemo
	@echo "$(BLUE)Starting TUI demo (mock data)...$(NC)"
	@echo "$(YELLOW)Press 'q' to quit, arrow keys to navigate$(NC)"
	@./tuidemo

demo-web: build-ui
	@echo "$(BLUE)Starting Web UI demo...$(NC)"
	@echo "$(YELLOW)Open http://localhost:5173 in your browser$(NC)"
	@echo "$(YELLOW)Press Ctrl+C to stop$(NC)"
	@cd $(UI_DIR) && npm run dev

demo-vm: build vm-ensure
	@echo "$(BLUE)Starting VM demo with virtual network...$(NC)"
	@$(SCRIPTS_DIR)/run-demo.sh start

demo-stop:
	@echo "$(BLUE)Stopping VM demo...$(NC)"
	@$(SCRIPTS_DIR)/run-demo.sh stop

build-tuidemo:
	@echo "$(BLUE)Building TUI demo...$(NC)"
	@go build -o tuidemo ./cmd/tuidemo
	@echo "$(GREEN)✓ TUI demo built$(NC)"

# ==============================================================================
# Development Targets
# ==============================================================================

dev: build vm-ensure
	@echo "$(BLUE)Starting development environment...$(NC)"
	@echo "$(YELLOW)Web UI: http://localhost:8080$(NC)"
	@$(SCRIPTS_DIR)/run-dev.sh

dev-vm: vm-ensure build-go
	@echo "$(BLUE)Starting full dev simulated virtual machine...$(NC)"
	@$(SCRIPTS_DIR)/run-demo.sh dev

# Mock API server (no VM required, simulates notifications/logs)
dev-api:
	@echo "$(BLUE)Starting mock API server...$(NC)"
	@echo "$(YELLOW)API: http://localhost:8080$(NC)"
	@echo "$(YELLOW)Auth: admin / admin123$(NC)"
	@go run ./cmd/api-dev

# Web UI with mock API (fastest iteration, runs both together)
dev-web:
	@$(SCRIPTS_DIR)/dev-web.sh

# TUI with hot-reload (requires: go install github.com/cosmtrek/air@latest)
dev-tui:
	@echo "$(BLUE)Starting TUI with hot-reload...$(NC)"
	@echo "$(YELLOW)Install air if needed: go install github.com/cosmtrek/air@latest$(NC)"
	@if command -v air >/dev/null 2>&1; then \
		air -c .air.toml; \
	else \
		echo "$(RED)Air not installed. Run: go install github.com/cosmtrek/air@latest$(NC)"; \
		echo "$(YELLOW)Falling back to manual rebuild...$(NC)"; \
		find internal cmd -name "*.go" | entr -r go run ./cmd/tuidemo 2>/dev/null || \
		(echo "$(YELLOW)Install entr or air for hot-reload$(NC)" && go run ./cmd/tuidemo); \
	fi

lint:
	@echo "$(BLUE)Running linters...$(NC)"
	@go vet ./internal/... ./cmd/...
	@echo "$(GREEN)✓ go vet passed$(NC)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./internal/... ./cmd/...; \
		echo "$(GREEN)✓ golangci-lint passed$(NC)"; \
	else \
		echo "$(YELLOW)⚠ golangci-lint not installed, skipping$(NC)"; \
	fi

fmt:
	@echo "$(BLUE)Formatting code...$(NC)"
	@go fmt ./...
	@echo "$(GREEN)✓ Go code formatted$(NC)"
	@if [ -d $(UI_DIR)/node_modules ]; then \
		cd $(UI_DIR) && npx prettier --write "src/**/*.{js,svelte,css}" 2>/dev/null || true; \
		echo "$(GREEN)✓ UI code formatted$(NC)"; \
	fi

check: lint test
	@echo "$(GREEN)✓ All checks passed$(NC)"

# ==============================================================================
# VM Management Targets
# ==============================================================================

vm-setup: build-builder
	@echo "$(BLUE)Building Alpine VM image...$(NC)"
	@$(BUILD_DIR)/glacic-builder build
	@echo "$(GREEN)✓ VM setup complete$(NC)"

build-builder:
	@echo "$(BLUE)Building glacic-builder...$(NC)"
	@go build -o $(BUILD_DIR)/glacic-builder ./cmd/glacic-builder

build-iso: build-builder build-go
	@echo "$(BLUE)Building installer ISO...$(NC)"
	@$(BUILD_DIR)/glacic-builder iso
	@echo "$(GREEN)✓ ISO build complete$(NC)"

vm-ensure:
	@if [ ! -f "$(VM_IMAGE)" ]; then \
		echo "$(YELLOW)VM image not found, running setup...$(NC)"; \
		$(MAKE) vm-setup; \
	fi

vm-start: vm-ensure build-go
	@echo "$(BLUE)Starting VM...$(NC)"
	@$(SCRIPTS_DIR)/vm-dev.sh &
	@echo "$(GREEN)✓ VM started$(NC)"

vm-stop:
	@echo "$(BLUE)Stopping VM...$(NC)"
	@if pgrep -f "qemu.*rootfs" >/dev/null 2>&1; then \
		pkill -f "qemu.*rootfs" 2>/dev/null; \
		echo "$(GREEN)✓ VM stopped$(NC)"; \
	else \
		echo "$(YELLOW)⚠ No VM is currently running$(NC)"; \
	fi

# ==============================================================================
# Installation Targets
# ==============================================================================

# Deploy to remote host
deploy:
	@if [ -z "$(HOST)" ]; then \
		echo "$(RED)Error: HOST not set. Usage: make deploy HOST=root@192.168.1.1$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Building for Linux...$(NC)"
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY)
	@echo "$(BLUE)Deploying to $(HOST)...$(NC)"
	@./scripts/deploy-remote.sh "$(HOST)" "$(BINARY)"

# Install as Proxmox LXC
install-lxc: build-go
	@echo "$(BLUE)Proxmox LXC Installation$(NC)"
	@echo "$(YELLOW)Copy this script to your Proxmox host and run it:$(NC)"
	@echo "  scp scripts/install-proxmox-lxc.sh $(BINARY) root@proxmox:~/"
	@echo "  ssh root@proxmox './install-proxmox-lxc.sh --name glacic'"
	@echo ""
	@echo "$(YELLOW)Commands:$(NC)"
	@echo "  ./install-proxmox-lxc.sh --name glacic --wan vmbr0 --lan vmbr1"
	@echo "  ./install-proxmox-lxc.sh --help"
	@echo ""
	@echo "$(YELLOW)Or run with dry-run to preview:$(NC)"
	@echo "  ./scripts/install-proxmox-lxc.sh --dry-run"

# Create Proxmox distribution bundle
dist-proxmox:
	@echo "$(BLUE)Building Proxmox Bundle...$(NC)"
	@mkdir -p $(BUILD_DIR)/proxmox-dist
	@# Build linux binary
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/proxmox-dist/glacic
	@# Copy installer as install.sh
	@cp scripts/install-proxmox-lxc.sh $(BUILD_DIR)/proxmox-dist/install.sh
	@chmod +x $(BUILD_DIR)/proxmox-dist/install.sh
	@# Create tarball
	@tar -czf $(BUILD_DIR)/glacic-proxmox.tar.gz -C $(BUILD_DIR)/proxmox-dist .
	@rm -rf $(BUILD_DIR)/proxmox-dist
	@echo "$(GREEN)Bundle created: $(BUILD_DIR)/glacic-proxmox.tar.gz$(NC)"
	@echo "To deploy: scp $(BUILD_DIR)/glacic-proxmox.tar.gz root@proxmox:~/"
	@echo ""

# Create self-extracting installer
dist-proxmox-installer: dist-proxmox
	@echo "$(BLUE)Building Self-Extracting Installer...$(NC)"
	@cat scripts/self-extract-stub.sh $(BUILD_DIR)/glacic-proxmox.tar.gz > $(BUILD_DIR)/install-glacic-proxmox.sh
	@chmod +x $(BUILD_DIR)/install-glacic-proxmox.sh
	@echo "$(GREEN)Installer created: $(BUILD_DIR)/install-glacic-proxmox.sh$(NC)"
	@echo "To deploy: scp $(BUILD_DIR)/install-glacic-proxmox.sh root@proxmox:~/"
	@echo "           ssh root@proxmox './install-glacic-proxmox.sh --name firewall'"
	@echo ""

# ==============================================================================
# Cleanup Targets
# ==============================================================================

clean:
	@echo "$(BLUE)Cleaning build artifacts...$(NC)"
	@rm -f $(BINARY) $(BINARY_DARWIN) tuidemo
	@rm -f *.test
	@find $(UI_DIR)/dist -mindepth 1 ! -name 'index.html' -delete 2>/dev/null || true
	@echo "$(GREEN)✓ Clean complete$(NC)"

clean-all: clean
	@echo "$(BLUE)Cleaning VM images...$(NC)"
	@rm -rf $(BUILD_DIR)/*.qcow2 $(BUILD_DIR)/*.iso
	@echo "$(GREEN)✓ Full clean complete$(NC)"

# ==============================================================================
# CI/CD Targets
# ==============================================================================

ci: lint test-unit
	@echo "$(GREEN)✓ CI checks passed$(NC)"

release: clean build
	@echo "$(BLUE)Creating release...$(NC)"
	@mkdir -p dist
	@cp $(BINARY) dist/
	@tar -czvf dist/$(BRAND_BINARY)-linux-amd64.tar.gz -C dist $(BRAND_BINARY)
	@echo "$(GREEN)✓ Release created: dist/$(BRAND_BINARY)-linux-amd64.tar.gz$(NC)"

# Coverage targets
# Host coverage (fast, macOS compatible packages only)
coverage:
	./scripts/test/coverage.sh $(FILTER)
.PHONY: coverage

# VM coverage (full, all packages including Linux-dependent)
coverage-vm: build-tests
	./scripts/test/coverage_vm.sh $(FILTER)
.PHONY: coverage-vm

# Merge and view coverage report
coverage-report:
	@mkdir -p build/coverage
	@echo "mode: set" > build/coverage/all.out
	@grep -h -v "^mode:" build/coverage/*.out >> build/coverage/all.out 2>/dev/null || true
	@go tool cover -func=build/coverage/all.out | tail -20
	@echo ""
	@echo "View HTML report: go tool cover -html=build/coverage/all.out"
.PHONY: coverage-report

# -----------------------------------------------------------------------------
# Stats
# -----------------------------------------------------------------------------
DIRS ?= .
READING_SPEED ?= 25# Lines per minute

.PHONY: stats
stats: ## Calculate code statistics
	@scripts/dev/stats.sh
