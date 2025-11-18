# PChat Makefile

# 默认目标
.PHONY: all build clean test test-dht install help

# 项目名称
PROJECT_NAME := PChat
BINARY_NAME_PCHAT := pchat
BINARY_NAME_REGISTRY := registryd

# Go相关变量
GO := go
GOBUILD := $(GO) build
GOCLEAN := $(GO) clean
GOTEST := $(GO) test
GOGET := $(GO) get

# 目录路径
CMD_DIR := cmd
BIN_DIR := bin
INTERNAL_DIR := internal

# 源码路径
PCHAT_DIR := $(CMD_DIR)/pchat
PCHAT_MAIN := $(PCHAT_DIR)/main.go
PCHAT_DHT := $(PCHAT_DIR)/dht_discovery.go
PCHAT_REGISTRY := $(PCHAT_DIR)/registry.go
REGISTRY_MAIN := $(CMD_DIR)/registry/main.go

# 二进制文件路径
PCHAT_BINARY := $(BIN_DIR)/$(BINARY_NAME_PCHAT)
REGISTRY_BINARY := $(BIN_DIR)/$(BINARY_NAME_REGISTRY)

# 默认目标：构建所有二进制文件
all: build

# 构建所有二进制文件
build: $(PCHAT_BINARY) $(REGISTRY_BINARY)

# 构建pchat客户端
PCHAT_SOURCES := $(PCHAT_MAIN) $(PCHAT_DHT) $(PCHAT_REGISTRY)
$(PCHAT_BINARY): $(PCHAT_SOURCES)
	@echo "Building pchat client..."
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) -o $@ ./$(PCHAT_DIR)

# 构建注册服务器
$(REGISTRY_BINARY): $(REGISTRY_MAIN)
	@echo "Building registry server..."
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) -o $@ ./$(CMD_DIR)/registry

# 清理构建产物
clean:
	@echo "Cleaning build artifacts..."
	$(GOCLEAN)
	rm -rf $(BIN_DIR)
	rm -f coverage.txt

# 运行所有测试（不包括集成测试，默认行为）
test:
	@echo "Running unit tests (excluding integration tests)..."
	$(GOTEST) -v -tags="!integration" -timeout=60s ./...

# 运行单元测试（快速测试，不包括集成测试和压力测试）
test-unit:
	@echo "Running unit tests only..."
	$(GOTEST) -v -tags="!integration" -timeout=60s ./...

# 运行集成测试（包括 RPS 游戏测试、端到端测试等）
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -tags=integration -timeout=5m ./...

# 运行所有测试（包括集成测试）
test-all:
	@echo "Running all tests (including integration tests)..."
	@echo "Step 1: Running unit tests..."
	@$(GOTEST) -v -tags="!integration" -timeout=60s ./...
	@echo "Step 2: Running integration tests..."
	@$(GOTEST) -v -tags=integration -timeout=5m ./...

# 运行测试并生成覆盖率报告
test-coverage:
	@echo "Running tests with coverage..."
	@$(GOTEST) -tags="!integration" -coverprofile=coverage_unit.out -timeout=60s ./...
	@$(GOTEST) -tags=integration -coverprofile=coverage_integration.out -timeout=5m ./...
	@echo "Coverage reports generated: coverage_unit.out, coverage_integration.out"

# 运行DHT测试脚本
test-dht:
	@echo "Running DHT test script..."
	./tests/test_dht.sh

# 安装依赖
install:
	@echo "Installing dependencies..."
	$(GOGET) -v ./...

# 显示帮助信息
help:
	@echo "PChat Makefile"
	@echo "=================="
	@echo "Available targets:"
	@echo "  all          - Build all binaries (default)"
	@echo "  build        - Build all binaries"
	@echo "  clean        - Clean build artifacts"
	@echo "  test         - Run all tests"
	@echo "  test-dht     - Run DHT test script"
	@echo "  install      - Install dependencies"
	@echo "  help         - Show this help message"
	@echo ""
	@echo "Testing targets:"
	@echo "  cover        - Generate coverage report"
	@echo "  cover-check  - Check coverage thresholds"
	@echo "  cover-summary - Show coverage summary"
	@echo ""
	@echo "Code quality:"
	@echo "  fmt          - Format code"
	@echo "  vet          - Check code errors"
	@echo "  lint         - Run static analysis"
	@echo "  tidy         - Tidy go.mod"
	@echo "  verify       - Verify dependencies"
	@echo ""
	@echo "Binary targets:"
	@echo "  $(PCHAT_BINARY)     - Build pchat client"
	@echo "  $(REGISTRY_BINARY)  - Build registry server"

# 运行注册服务器
run-registry: $(REGISTRY_BINARY)
	@echo "Starting registry server..."
	./$(REGISTRY_BINARY)

# 运行pchat客户端示例（Alice）
run-alice: $(PCHAT_BINARY)
	@echo "Starting pchat client (Alice)..."
	./$(PCHAT_BINARY) -port 9001 -username Alice

# 运行pchat客户端示例（Bob）
run-bob: $(PCHAT_BINARY)
	@echo "Starting pchat client (Bob)..."
	./$(PCHAT_BINARY) -port 9002 -username Bob

# 代码格式化
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# 检查代码错误
vet:
	@echo "Vetting code..."
	$(GO) vet ./...

# 生成代码覆盖率报告
cover:
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	$(GO) tool cover -func=coverage.out > coverage.txt
	@echo "Coverage report generated: coverage.html"
	@echo "Coverage summary: coverage.txt"

# 检查覆盖率阈值
cover-check:
	@echo "Checking coverage thresholds..."
	@./scripts/check_coverage.sh

# 显示覆盖率摘要
cover-summary:
	@echo "Coverage Summary:"
	@echo "=================="
	@if [ -f coverage.out ]; then \
		$(GO) tool cover -func=coverage.out | grep -E "^cmd/pchat|^internal/discovery|^cmd/registry|^internal/crypto|^internal/registry|^total:"; \
	else \
		echo "Coverage file not found. Run 'make cover' first."; \
	fi

# 运行覆盖率监控
cover-monitor:
	@echo "Running coverage monitor..."
	@chmod +x scripts/coverage_monitor.sh
	@./scripts/coverage_monitor.sh

# 静态分析
lint:
	@echo "Running static analysis..."
	@if ! command -v golint &> /dev/null; then \
		echo "Installing golint..."; \
		$(GOGET) -u golang.org/x/lint/golint; \
	fi
	golint ./...

# 检查依赖更新
tidy:
	@echo "Tidying go.mod..."
	$(GO) mod tidy

# 验证依赖
verify:
	@echo "Verifying dependencies..."
	$(GO) mod verify