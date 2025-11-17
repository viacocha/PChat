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

# 运行所有测试
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

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
	$(GOTEST) -coverprofile=coverage.txt ./...
	$(GO) tool cover -html=coverage.txt -o coverage.html
	@echo "Coverage report generated: coverage.html"

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