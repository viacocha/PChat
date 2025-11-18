# PChat 测试指南

## 概述

本文档提供了 PChat 项目的测试指南，包括如何运行测试、理解测试结构、编写新测试以及测试最佳实践。

## 目录

1. [运行测试](#运行测试)
2. [测试结构](#测试结构)
3. [测试类型](#测试类型)
4. [编写测试](#编写测试)
5. [测试最佳实践](#测试最佳实践)
6. [测试覆盖率](#测试覆盖率)
7. [常见问题](#常见问题)

## 运行测试

### 运行所有测试

```bash
# 运行所有测试
go test ./...

# 运行所有测试（详细输出）
go test ./... -v

# 运行所有测试并生成覆盖率报告
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

### 运行特定包的测试

```bash
# 运行 cmd/pchat 包的测试
go test ./cmd/pchat -v

# 运行 internal/discovery 包的测试
go test ./internal/discovery -v

# 运行 cmd/registry 包的测试
go test ./cmd/registry -v
```

### 运行特定测试函数

```bash
# 运行特定测试函数
go test ./cmd/pchat -run TestHandleStream_CompleteFlow -v

# 运行匹配模式的测试
go test ./cmd/pchat -run TestHandleStream -v
```

### 运行性能测试

```bash
# 运行性能测试（Benchmark）
go test ./cmd/pchat -bench=. -benchmem

# 运行特定性能测试
go test ./cmd/pchat -bench=BenchmarkEncryptAndSignMessage -benchmem
```

### 运行压力测试

```bash
# 运行压力测试（默认跳过，需要移除 -short 标志）
go test ./cmd/pchat -run TestStress -v

# 跳过压力测试（使用 -short 标志）
go test ./cmd/pchat -short -v
```

## 测试结构

### 测试文件组织

```
cmd/pchat/
├── main.go                    # 主代码
├── main_test.go              # 主函数测试
├── core_network_test.go      # 核心网络处理函数测试
├── ui_core_test.go           # UI 核心函数测试
├── helper_functions_test.go # 辅助函数测试
├── utility_functions_test.go # 工具函数测试
├── performance_test.go       # 性能测试
├── concurrency_test.go       # 并发安全测试
├── stress_test.go            # 压力测试
├── test_helpers.go           # 测试辅助函数
└── test_mocks.go             # Mock 对象

internal/discovery/
├── dht.go                    # DHT Discovery 实现
├── dht_logic_test.go        # DHT 逻辑测试
└── dht_extended_test.go     # DHT 扩展测试
```

### 测试文件命名规范

- 单元测试：`*_test.go`
- 性能测试：`performance_test.go`
- 并发测试：`concurrency_test.go`
- 压力测试：`stress_test.go`
- 集成测试：`integration_*_test.go`

## 测试类型

### 1. 单元测试

测试单个函数或方法的独立功能。

**示例**:
```go
func TestValidateUsername(t *testing.T) {
    tests := []struct {
        name     string
        username string
        wantErr  bool
    }{
        {"有效用户名", "Alice", false},
        {"空用户名", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateUsername(tt.username)
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidateUsername() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### 2. 集成测试

测试多个组件之间的交互。

**示例**:
```go
func TestEndToEnd_CompleteFlow(t *testing.T) {
    // 设置测试环境
    mn, hosts, privKey, pubKey, ctx := setupTestEnvironment(t, 2)
    defer cleanupHosts(hosts)

    // 测试完整流程
    // 1. 密钥交换
    // 2. 消息发送
    // 3. 文件传输
}
```

### 3. 性能测试

使用 `Benchmark` 前缀测试函数性能。

**示例**:
```go
func BenchmarkEncryptAndSignMessage(b *testing.B) {
    // 设置
    senderPrivKey, _, _ := generateKeys()
    _, recipientPubKey, _ := generateKeys()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        encryptAndSignMessage("test", senderPrivKey, &recipientPubKey)
    }
}
```

### 4. 并发安全测试

测试并发场景下的正确性和安全性。

**示例**:
```go
func TestConcurrentEncryptAndSignMessage(t *testing.T) {
    goroutines := 10
    var wg sync.WaitGroup
    wg.Add(goroutines)

    for i := 0; i < goroutines; i++ {
        go func() {
            defer wg.Done()
            // 执行并发操作
        }()
    }

    wg.Wait()
}
```

### 5. 压力测试

测试系统在高负载下的表现。

**示例**:
```go
func TestStress_EncryptManyMessages(t *testing.T) {
    if testing.Short() {
        t.Skip("跳过压力测试")
    }

    messageCount := 1000
    // 执行大量操作
}
```

## 编写测试

### 使用测试辅助函数

项目提供了统一的测试辅助函数库（`test_helpers.go`）：

```go
// 设置测试环境
mn, hosts, privKey, pubKey, ctx := setupTestEnvironment(t, 2)
defer cleanupHosts(hosts)

// 创建测试消息
message := createTestMessage("Hello, World!")

// 等待条件
waitForCondition(t, func() bool {
    return someCondition()
}, 5*time.Second, "等待条件超时")

// 使用断言
assertNoError(t, err, "操作应该成功")
assertEqual(t, got, want, "值应该相等")
```

### 使用表驱动测试

对于需要测试多个场景的函数，使用表驱动测试：

```go
func TestFunction_TableDriven(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"正常场景", "input1", "output1", false},
        {"错误场景", "input2", "", true},
        {"边界条件", "input3", "output3", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := myFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("myFunction() error = %v, wantErr %v", err, tt.wantErr)
            }
            if !tt.wantErr && got != tt.want {
                t.Errorf("myFunction() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### 使用 Mock 对象

对于需要隔离依赖的测试，使用 Mock 对象：

```go
// 创建 Mock Host
mockHost := NewMockHost(peerID)

// 设置失败场景
mockHost.SetFailOnNewStream(true)
```

### 测试错误场景

确保测试覆盖错误场景：

```go
func TestFunction_ErrorScenarios(t *testing.T) {
    tests := []struct {
        name    string
        input   interface{}
        wantErr bool
    }{
        {"nil输入", nil, true},
        {"无效输入", "invalid", true},
        {"网络错误", mockNetworkError(), true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := myFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("myFunction() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

## 测试最佳实践

### 1. 测试命名

- 使用描述性的测试名称
- 使用 `TestFunction_Scenario` 格式
- 使用子测试组织相关测试

```go
func TestHandleStream_CompleteFlow(t *testing.T) { }
func TestHandleStream_InvalidMessage(t *testing.T) { }
func TestHandleStream_NilStream(t *testing.T) { }
```

### 2. 测试组织

- 每个测试应该独立运行
- 使用 `t.Helper()` 标记辅助函数
- 使用 `defer` 确保资源清理

```go
func setupTestEnvironment(t *testing.T) {
    t.Helper()
    // 设置代码
}

func TestMyFunction(t *testing.T) {
    env := setupTestEnvironment(t)
    defer env.Cleanup()
    // 测试代码
}
```

### 3. 测试隔离

- 每个测试应该独立，不依赖其他测试
- 使用 mock 对象隔离依赖
- 避免测试之间的状态共享

### 4. 错误处理

- 测试应该验证错误情况
- 使用 `t.Fatalf()` 在关键错误时停止测试
- 使用 `t.Errorf()` 报告非致命错误

### 5. 性能考虑

- 使用 `b.ResetTimer()` 在性能测试中排除设置时间
- 使用 `testing.Short()` 跳过长时间的压力测试
- 避免在测试中创建不必要的资源

## 测试覆盖率

### 查看覆盖率

```bash
# 生成覆盖率报告
go test ./... -coverprofile=coverage.out

# 查看覆盖率摘要
go tool cover -func=coverage.out

# 生成 HTML 覆盖率报告
go tool cover -html=coverage.out -o coverage.html
```

### 覆盖率目标

- **总体覆盖率**: >50% ✅（当前 ~60-65%）
- **cmd/pchat**: >50%（当前 ~40-45%）
- **internal/discovery**: >40% ✅（当前 ~40-42%）
- **cmd/registry**: >30% ✅（当前 59.1%）
- **internal/crypto**: >70% ✅（当前 81.4%）

### 覆盖率监控

使用覆盖率监控脚本持续监控覆盖率：

```bash
# 运行覆盖率监控
./scripts/coverage_monitor.sh

# 查看覆盖率历史
cat coverage_history.txt
```

监控脚本会：
- 生成覆盖率报告
- 检查覆盖率阈值
- 记录覆盖率历史
- 显示各模块覆盖率状态

### 提高覆盖率

1. 识别低覆盖率函数
2. 为低覆盖率函数添加测试
3. 覆盖错误场景和边界条件
4. 添加集成测试

## 常见问题

### Q: 如何测试需要网络连接的函数？

A: 使用 `mocknet` 创建模拟网络环境：

```go
mn, hosts := setupMockHosts(t, 2)
defer cleanupHosts(hosts)
```

### Q: 如何测试 UI 相关函数？

A: 创建 UI 实例但不实际运行，测试逻辑部分：

```go
ui := NewChatUI(ctx, h, privKey, pubKey, nil, nil, "TestUser")
// 测试 UI 方法，不实际启动 UI
```

### Q: 如何测试需要时间的函数？

A: 使用 `waitForCondition` 辅助函数：

```go
waitForCondition(t, func() bool {
    return someCondition()
}, 5*time.Second, "等待条件超时")
```

### Q: 如何测试并发安全？

A: 使用 goroutines 和 sync.WaitGroup：

```go
var wg sync.WaitGroup
for i := 0; i < goroutines; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        // 并发操作
    }()
}
wg.Wait()
```

### Q: 如何跳过长时间运行的测试？

A: 使用 `testing.Short()`：

```go
func TestStress_LongRunning(t *testing.T) {
    if testing.Short() {
        t.Skip("跳过长时间运行的测试")
    }
    // 测试代码
}
```

## 测试示例

### 示例 1: 单元测试

```go
func TestValidateUsername(t *testing.T) {
    tests := []struct {
        name     string
        username string
        wantErr  bool
    }{
        {"有效用户名", "Alice", false},
        {"空用户名", "", true},
        {"太长用户名", strings.Repeat("a", MaxUsernameLength+1), true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateUsername(tt.username)
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidateUsername() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### 示例 2: 集成测试

```go
func TestEndToEnd_CompleteFlow(t *testing.T) {
    mn, hosts, privKey, pubKey, ctx := setupTestEnvironment(t, 2)
    defer cleanupHosts(hosts)

    // 密钥交换
    // 消息发送
    // 文件传输
}
```

### 示例 3: 性能测试

```go
func BenchmarkEncryptAndSignMessage(b *testing.B) {
    senderPrivKey, _, _ := generateKeys()
    _, recipientPubKey, _ := generateKeys()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        encryptAndSignMessage("test", senderPrivKey, &recipientPubKey)
    }
}
```

## 持续集成

### GitHub Actions

项目使用 GitHub Actions 进行持续集成测试：

- **自动触发**: Push、Pull Request、定时任务
- **多版本测试**: Go 1.21, 1.22, 1.23
- **覆盖率报告**: 自动生成并上传覆盖率报告
- **代码质量检查**: 运行 golangci-lint

查看工作流文件: `.github/workflows/test.yml`

### 本地 CI 检查

在提交代码前运行：

```bash
# 运行所有测试
make test

# 生成覆盖率报告
make cover

# 检查覆盖率阈值
make cover-check

# 运行静态分析
make lint
```

## 参考资源

- [Go 测试文档](https://golang.org/pkg/testing/)
- [测试覆盖率文档](https://golang.org/cmd/cover/)
- [测试最佳实践](https://golang.org/doc/effective_go#testing)
- [项目测试覆盖率报告](../TEST_COVERAGE_REPORT.md)
- [测试改进建议](./TEST_COVERAGE_IMPROVEMENT_V2.md)
- [测试最佳实践](./TEST_BEST_PRACTICES.md)

## 持续集成（CI）

### GitHub Actions

项目包含 GitHub Actions workflow 用于自动化测试和覆盖率报告。

**工作流文件**: `.github/workflows/test.yml`

**功能**:
- 自动运行所有测试
- 生成覆盖率报告
- 上传覆盖率到 Codecov（可选）
- 检查覆盖率阈值

**触发条件**:
- 推送到主分支
- 创建 Pull Request
- 手动触发

### 本地运行 CI 检查

```bash
# 运行所有测试（模拟 CI）
make test

# 生成覆盖率报告
make cover

# 检查代码格式
make fmt

# 检查代码错误
make vet
```

## 测试覆盖率监控

### 覆盖率报告生成

```bash
# 生成覆盖率报告
go test ./... -coverprofile=coverage.out

# 查看覆盖率摘要
go tool cover -func=coverage.out

# 生成 HTML 报告
go tool cover -html=coverage.out -o coverage.html

# 使用 Makefile
make cover
```

### 覆盖率阈值

项目设置了以下覆盖率阈值：

| 模块 | 目标覆盖率 | 当前覆盖率 | 状态 |
|------|----------|----------|------|
| cmd/pchat | >50% | ~40-45% | ⚠️ 接近 |
| internal/discovery | >40% | ~40-42% | ✅ 已达标 |
| cmd/registry | >30% | 59.1% | ✅ 已超标 |
| internal/crypto | >70% | 81.4% | ✅ 已达标 |
| 总体覆盖率 | >50% | ~60-65% | ✅ 已达标 |

### 覆盖率趋势跟踪

覆盖率报告保存在以下位置：
- `coverage.out` - 覆盖率数据文件
- `coverage.html` - HTML 可视化报告
- `TEST_COVERAGE_REPORT.md` - 覆盖率状态文档

### 自动化覆盖率检查

在 CI 流程中，如果覆盖率低于阈值，构建将失败。

## 测试最佳实践总结

### 1. 测试组织
- ✅ 使用表驱动测试覆盖多个场景
- ✅ 使用测试辅助函数减少重复代码
- ✅ 使用 Mock 对象隔离依赖
- ✅ 每个测试应该独立运行

### 2. 测试覆盖
- ✅ 覆盖正常流程
- ✅ 覆盖错误场景
- ✅ 覆盖边界条件
- ✅ 覆盖并发场景

### 3. 测试性能
- ✅ 使用性能测试识别瓶颈
- ✅ 使用压力测试验证系统稳定性
- ✅ 使用并发测试验证线程安全

### 4. 测试维护
- ✅ 保持测试代码简洁
- ✅ 使用描述性的测试名称
- ✅ 及时更新测试文档
- ✅ 定期审查测试覆盖率

## 更新日志

- 2025-11-18: 创建测试指南
  - 添加测试运行说明
  - 添加测试结构说明
  - 添加测试类型说明
  - 添加编写测试指南
  - 添加测试最佳实践
  - 添加常见问题解答
- 2025-11-18 下午: 完善测试指南
  - 添加持续集成说明
  - 添加测试覆盖率监控说明
  - 添加覆盖率阈值和趋势跟踪
  - 添加测试最佳实践总结

