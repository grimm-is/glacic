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


# Default target
.DEFAULT_GOAL := help

.PHONY: help all build server client ui install clean \
        test test-unit test-int integration \
        demo demo-web demo-vm \
        vm-setup vm-start vm-stop \
        lint fmt check dev

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

# Detect host architecture for cross-compilation
HOST_ARCH := $(shell uname -m)
# Fallback if uname fails
ifeq ($(HOST_ARCH),)
    HOST_ARCH := amd64
endif

ifeq ($(HOST_ARCH),arm64)
    LINUX_ARCH := arm64
else ifeq ($(HOST_ARCH),aarch64)
    LINUX_ARCH := arm64
else
    LINUX_ARCH := amd64
endif

# Configuration
BUILD_DIR := build
BINARY := $(BUILD_DIR)/$(BRAND_BINARY)
BINARY_LINUX_AMD64 := $(BUILD_DIR)/$(BRAND_BINARY)-linux-amd64
BINARY_LINUX_ARM64 := $(BUILD_DIR)/$(BRAND_BINARY)-linux-arm64
# Target Linux binary for current architecture (or host arch if linux)
BINARY_TARGET := $(if $(filter arm64,$(LINUX_ARCH)),$(BINARY_LINUX_ARM64),$(BINARY_LINUX_AMD64))
BINARY_DARWIN := $(BUILD_DIR)/$(BRAND_BINARY)-darwin-$(HOST_ARCH)
UI_DIR := ui
SCRIPTS_DIR := scripts
VM_IMAGE := $(BUILD_DIR)/rootfs.qcow2
GO_FILES := $(shell find . -type f -name '*.go' -not -path "./vendor/*" -not -path "./node_modules/*" -not -path "./ui/*")
# Test binaries only depend on internal/* sources, not toolbox/cmd
TEST_GO_FILES := $(shell find ./internal -type f -name '*.go' 2>/dev/null)

# Default target
help:
	@echo ""
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo "$(BLUE)  $(BRAND_NAME) - Build & Test System$(NC)"
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@echo "$(YELLOW)Build Commands:$(NC)"
	@echo "  make              Show help"
	@echo "  make build        Build native binary (detects OS)"
	@echo "  make server       Build Linux binary (for VM/Deploy)"
	@echo "  make ui           Build Svelte UI"
	@echo "  make install      Install native binary to /usr/local/bin"
	@echo "  make iso          Build bootable installer ISO"
	@echo ""
	@echo "$(YELLOW)Test Commands:$(NC)"
	@echo "  make test              Run all unit tests (Go tests)"
	@echo "  make test-int          Run integration tests in VM"
	@echo "  make test-int FILTER=dns  Run only tests matching 'dns'"
	@echo "  make test-int TESTS=\"t/20-dhcp/*.sh\"  Run specific test paths"
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

# Smart Build: Detects Host OS
# On macOS: Builds Client (glacic)
# On Linux: Builds Server (glacic)
build: ui $(BINARY)
	@echo "$(GREEN)✓ Build complete: $(BINARY)$(NC)"

all: build

# Alias
client: build

# UI Build
ui:
	@echo "$(BLUE)Building UI...$(NC)"
	@cd $(UI_DIR) && \
		if [ ! -d node_modules ]; then npm install --silent; fi && \
		npm run build --silent
	@echo "$(GREEN)✓ UI built$(NC)"

# Build release binary (stripped symbols)
release: $(GO_FILES)
	@echo "$(BLUE)Building $(BRAND_NAME) Release Binary (Stripped)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) .
	@echo "$(GREEN)Build complete: $(BINARY)$(NC)"

# Native Binary (Host OS)
$(BINARY): $(GO_FILES)
	@echo "$(BLUE)Building native binary ($(HOST_ARCH))...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@go build -ldflags "$(LDFLAGS)" -o $(BINARY) .
	@echo "$(GREEN)✓ Native binary built: $(BINARY)$(NC)"

# Server Build (Linux Binary - Always Cross-Compiled if needed)
# Used by VM and Deployment
server: ui $(BINARY_TARGET)

# Always-fresh server build (use for upgrades)
server-fresh: ui
	@rm -f $(BINARY_TARGET)
	@$(MAKE) $(BINARY_TARGET)



# ==============================================================================
# Cross-Platform Builds
# ==============================================================================

BINARY_DARWIN_AMD64 := $(BUILD_DIR)/$(BRAND_BINARY)-darwin-amd64
BINARY_DARWIN_ARM64 := $(BUILD_DIR)/$(BRAND_BINARY)-darwin-arm64

build-all: ui build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64
	@echo "$(GREEN)✓ All binaries built in $(BUILD_DIR)/$(NC)"

# Linux Targets
build-linux-amd64: $(BINARY_LINUX_AMD64)
$(BINARY_LINUX_AMD64): $(GO_FILES)
	@echo "$(BLUE)Building Linux (amd64)...$(NC)"
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_LINUX_AMD64) .

build-linux-arm64: $(BINARY_LINUX_ARM64)
$(BINARY_LINUX_ARM64): $(GO_FILES)
	@echo "$(BLUE)Building Linux (arm64)...$(NC)"
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_LINUX_ARM64) .

# Darwin (macOS) Targets
build-darwin-amd64: $(BINARY_DARWIN_AMD64)
build-macos-amd64: build-darwin-amd64
$(BINARY_DARWIN_AMD64): $(GO_FILES)
	@echo "$(BLUE)Building macOS (amd64)...$(NC)"
	@CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DARWIN_AMD64) .

build-darwin-arm64: $(BINARY_DARWIN_ARM64)
build-macos-arm64: build-darwin-arm64
$(BINARY_DARWIN_ARM64): $(GO_FILES)
	@echo "$(BLUE)Building macOS (arm64)...$(NC)"
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DARWIN_ARM64) .

# Installation
DESTDIR ?= /usr/local/bin
install: build
	@echo "$(BLUE)Installing to $(DESTDIR)...$(NC)"
	@install -m 755 $(BINARY) $(DESTDIR)/$(BRAND_BINARY)
	@echo "$(GREEN)✓ Installed $(BRAND_BINARY)$(NC)"

build-qemu-exit:
	@echo "$(BLUE)Building qemu-exit helper...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/qemu-exit-amd64 ./cmd/qemu-exit
	@GOOS=linux GOARCH=arm64 go build -o $(BUILD_DIR)/qemu-exit-arm64 ./cmd/qemu-exit
	@echo "$(GREEN)✓ qemu-exit built for amd64 and arm64$(NC)"


# Host build (for Orchestrator)
TOOLBOX_HOST := $(BUILD_DIR)/toolbox-darwin-$(HOST_ARCH)
ifeq ($(shell uname),Linux)
    TOOLBOX_HOST := $(BUILD_DIR)/toolbox-linux-$(HOST_ARCH)
endif

$(TOOLBOX_HOST): $(GO_FILES)
	@echo "$(BLUE)Building Toolbox (Host: $(HOST_ARCH))...$(NC)"
	@mkdir -p build
	@go build -ldflags "$(LDFLAGS)" -o $(TOOLBOX_HOST) ./cmd/toolbox
	@echo "$(GREEN)✓ Host toolbox built: $(TOOLBOX_HOST)$(NC)"

$(BUILD_DIR)/toolbox: $(TOOLBOX_HOST)
	@ln -sf $(notdir $(TOOLBOX_HOST)) $(BUILD_DIR)/toolbox
	@ln -sf $(notdir $(TOOLBOX_HOST)) build/glacic-orca
	@echo "$(GREEN)✓ Toolbox symlinks created$(NC)"

# Guest build (for Agent/Harness) - Static Linux
TOOLBOX_GUEST := $(BUILD_DIR)/toolbox-linux-$(LINUX_ARCH)

$(TOOLBOX_GUEST): $(GO_FILES)
	@echo "$(BLUE)Building Toolbox (Guest: Linux/$(LINUX_ARCH))...$(NC)"
	@mkdir -p build
	@CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_ARCH) go build -ldflags "$(LDFLAGS)" -o $(TOOLBOX_GUEST) ./cmd/toolbox
	@echo "$(GREEN)✓ Guest toolbox built: $(TOOLBOX_GUEST)$(NC)"

$(BUILD_DIR)/toolbox-linux: $(TOOLBOX_GUEST)
	@ln -sf $(notdir $(TOOLBOX_GUEST)) $(BUILD_DIR)/toolbox-linux
	@ln -sf $(notdir $(TOOLBOX_GUEST)) build/orca-agent
	@ln -sf $(notdir $(TOOLBOX_GUEST)) build/prove
	@echo "$(GREEN)✓ Guest toolbox symlinks created$(NC)"

build-toolbox: $(BUILD_DIR)/toolbox $(BUILD_DIR)/toolbox-linux

brand-env: internal/brand/brand.env

internal/brand/brand.env: internal/brand/brand.json
	@echo "$(BLUE)Generating brand.env from brand.json...$(NC)"
	@jq -r 'to_entries[] | "\(.key)=\(.value | @sh)"' internal/brand/brand.json | \
		while IFS='=' read -r key val; do \
			upper_key=$$(echo "BRAND_$$key" | sed 's/\([a-z]\)\([A-Z]\)/\1_\2/g' | tr '[:lower:]' '[:upper:]'); \
			echo "$$upper_key=$$val"; \
		done > internal/brand/brand.env
	@echo "$(GREEN)✓ brand.env generated$(NC)"

# Update OUI vendor database (run infrequently)
update-oui:
	@echo "$(BLUE)Downloading IEEE OUI database...$(NC)"
	@go run ./tools/oui-gen -real
	@echo "$(GREEN)✓ OUI database updated$(NC)"

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

build-tests: $(BUILD_DIR)/tests/.built

$(BUILD_DIR)/tests/.built: $(TEST_GO_FILES)
	@scripts/build/build_tests.sh linux $(LINUX_ARCH)
	@touch $(BUILD_DIR)/tests/.built

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
test-int: vm-ensure $(BINARY_TARGET) build-tests brand-env build-toolbox
	@mkdir -p build/test-artifacts
	@cp $(BINARY_TARGET) build/test-artifacts/glacic-v1
	@cp $(BINARY_TARGET) build/test-artifacts/glacic-v2
	@echo "$(BLUE)Calculating optimal parallelism...$(NC)"
	@JOBS=$$(./scripts/build/calc-jobs.sh); \
	 echo "$(BLUE)Running integration tests via parallel orca (JOBS=$$JOBS)...$(NC)"; \
	 ARGS="$(ARGS)"; \
	 if [ -n "$(FILTER)" ]; then ARGS="$$ARGS -filter $(FILTER)"; fi; \
	 if [ -n "$(TESTS)" ]; then \
	     ARGS="$$ARGS $(TESTS)"; \
	 else \
	     ARGS="$$ARGS integration_tests/linux"; \
	 fi; \
	 ./build/toolbox orca test -j $$JOBS $$ARGS

# specific linux target for clarity
test-int-linux:
	@$(MAKE) test-int TESTS="integration_tests/linux"

# Orca VM Pool Management
pool-status: build-toolbox
	@echo "$(BLUE)Checking Orca VM Pool status...$(NC)"
	@./build/toolbox orca status

pool-check: pool-status

pool-stop: build-toolbox
	@echo "$(BLUE)Stopping Orca VM Pool...$(NC)"
	@./build/toolbox orca stop

pool-clean:
	@echo "$(BLUE)Force cleaning stale VM sockets and processes...$(NC)"
	@pkill -f "qemu-system" || true
	@rm -f /tmp/glacic-vm*.sock
	@echo "$(GREEN)✓ Cleanup complete$(NC)"

test-orca: test-int



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

demo-vm: server vm-ensure
	@echo "$(BLUE)Starting VM demo with virtual network...$(NC)"
	@$(SCRIPTS_DIR)/demo/run.sh start

demo-stop:
	@echo "$(BLUE)Stopping VM demo...$(NC)"
	@$(SCRIPTS_DIR)/demo/run.sh stop

build-tuidemo:
	@echo "$(BLUE)Building TUI demo...$(NC)"
	@go build -o tuidemo ./cmd/tuidemo
	@echo "$(GREEN)✓ TUI demo built$(NC)"

# ==============================================================================
# Development Targets
# ==============================================================================

dev: server vm-ensure
	@echo "$(BLUE)Starting development environment...$(NC)"
	@echo "$(YELLOW)Web UI: http://localhost:8080$(NC)"
	@$(SCRIPTS_DIR)/dev/run.sh

dev-vm: vm-ensure build-go
	@echo "$(BLUE)Starting full dev simulated virtual machine...$(NC)"
	@$(SCRIPTS_DIR)/demo/run.sh dev

# Mock API server (no VM required, simulates notifications/logs)
dev-api:
	@echo "$(BLUE)Starting mock API server...$(NC)"
	@echo "$(YELLOW)API: http://localhost:8080$(NC)"
	@echo "$(YELLOW)Auth: admin / admin123$(NC)"
	@go run ./cmd/api-dev

# Web UI with mock API (fastest iteration, runs both together)
dev-web:
	@$(SCRIPTS_DIR)/dev/web.sh

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

vm-start: vm-ensure server
	@echo "$(BLUE)Starting VM...$(NC)"
	@$(SCRIPTS_DIR)/vm/dev.sh &
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
	@./scripts/deploy/remote.sh "$(HOST)" "$(BINARY)"

# Install as Proxmox LXC
install-lxc: server
	@echo "$(BLUE)Proxmox LXC Installation$(NC)"
	@echo "$(YELLOW)Copy this script to your Proxmox host and run it:$(NC)"
	@echo "  scp scripts/deploy/install-proxmox.sh $(BINARY) root@proxmox:~/"
	@echo "  ssh root@proxmox './install-proxmox.sh --name glacic'"
	@echo ""
	@echo "$(YELLOW)Commands:$(NC)"
	@echo "  ./install-proxmox.sh --name glacic --wan vmbr0 --lan vmbr1"
	@echo "  ./install-proxmox.sh --help"
	@echo ""
	@echo "$(YELLOW)Or run with dry-run to preview:$(NC)"
	@echo "  ./scripts/deploy/install-proxmox.sh --dry-run"

# Create Proxmox distribution bundle
dist-proxmox:
	@echo "$(BLUE)Building Proxmox Bundle...$(NC)"
	@mkdir -p $(BUILD_DIR)/proxmox-dist
	@# Build linux binary
	@$(MAKE) server
	@cp $(BINARY_TARGET) $(BUILD_DIR)/proxmox-dist/glacic
	@# Copy installer as install.sh
	@cp scripts/deploy/install-proxmox.sh $(BUILD_DIR)/proxmox-dist/install.sh
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
	@cat scripts/build/self-extract.sh $(BUILD_DIR)/glacic-proxmox.tar.gz > $(BUILD_DIR)/install-glacic-proxmox.sh
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
	@rm -f $(BINARY) $(BINARY_LINUX_AMD64) $(BINARY_LINUX_ARM64) $(BINARY_DARWIN) tuidemo
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

package: clean release
	@echo "$(BLUE)Creating release package...$(NC)"
	@mkdir -p dist
	@cp $(BINARY) dist/
	@tar -czvf dist/$(BRAND_BINARY)-linux-amd64.tar.gz -C dist $(BRAND_BINARY)
	@echo "$(GREEN)✓ Package created: dist/$(BRAND_BINARY)-linux-amd64.tar.gz$(NC)"

# Coverage targets (Disabled: scripts missing)
# coverage:
# 	./scripts/test/coverage.sh $(FILTER)
# .PHONY: coverage
#
# coverage-vm: build-tests
# 	./scripts/test/coverage_vm.sh $(FILTER)
# .PHONY: coverage-vm

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
