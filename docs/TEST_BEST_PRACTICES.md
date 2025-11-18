# PChat 测试最佳实践

## 概述

本文档描述了 PChat 项目的测试最佳实践，包括测试策略、测试组织、测试编写指南和持续改进建议。

## 目录

1. [测试策略](#测试策略)
2. [测试组织](#测试组织)
3. [测试编写指南](#测试编写指南)
4. [持续集成](#持续集成)
5. [覆盖率监控](#覆盖率监控)
6. [测试维护](#测试维护)

## 测试策略

### 测试金字塔

PChat 项目采用测试金字塔策略：

```
        /\
       /  \      E2E 测试 (5%)
      /____\
     /      \   集成测试 (15%)
    /________\
   /          \  单元测试 (80%)
  /____________\
```

- **单元测试 (80%)**: 测试单个函数或方法
- **集成测试 (15%)**: 测试多个组件之间的交互
- **E2E 测试 (5%)**: 测试完整的用户流程

### 测试优先级

1. **高优先级**: 核心功能、安全关键功能、高频使用功能
2. **中优先级**: 辅助功能、边界条件、错误处理
3. **低优先级**: 工具函数、格式化函数、UI 显示函数

## 测试组织

### 文件命名规范

- 单元测试: `*_test.go`
- 集成测试: `integration_*_test.go`
- 性能测试: `performance_*_test.go` 或 `*_bench_test.go`
- 并发测试: `concurrency_*_test.go`
- 压力测试: `stress_*_test.go`

### 测试文件结构

```
cmd/pchat/
├── main.go                    # 主代码
├── main_test.go              # 主函数测试
├── core_network_test.go      # 核心网络测试
├── ui_core_test.go           # UI 核心测试
├── helper_functions_test.go  # 辅助函数测试
├── test_helpers.go           # 测试辅助函数
└── test_mocks.go             # Mock 对象
```

### 测试函数命名

- 使用 `Test` 前缀: `TestFunctionName`
- 使用描述性名称: `TestFunctionName_Scenario`
- 使用子测试组织: `t.Run("scenario", func(t *testing.T) { ... })`

示例：
```go
func TestHandleStream_CompleteFlow(t *testing.T) { }
func TestHandleStream_InvalidMessage(t *testing.T) { }
func TestHandleStream_NilStream(t *testing.T) { }
```

## 测试编写指南

### 1. 使用表驱动测试

对于需要测试多个场景的函数，使用表驱动测试：

```go
func TestValidateUsername_TableDriven(t *testing.T) {
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

### 2. 使用测试辅助函数

使用统一的测试辅助函数库：

```go
// 设置测试环境
mn, hosts, privKey, pubKey, ctx := setupTestEnvironment(t, 2)
defer cleanupHosts(hosts)

// 使用断言
assertNoError(t, err, "操作应该成功")
assertEqual(t, got, want, "值应该相等")
assertNotNil(t, obj, "对象应该存在")
```

### 3. 测试错误场景

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

### 4. 测试边界条件

测试边界条件和极端场景：

```go
func TestFunction_BoundaryConditions(t *testing.T) {
    tests := []struct {
        name string
        input int
        expected int
    }{
        {"最小值", 0, 0},
        {"最大值", MaxValue, MaxValue},
        {"超过最大值", MaxValue + 1, MaxValue},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := myFunction(tt.input)
            if result != tt.expected {
                t.Errorf("myFunction() = %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### 5. 使用 Mock 对象

对于需要隔离依赖的测试，使用 Mock 对象：

```go
func TestFunction_WithMock(t *testing.T) {
    mockHost := NewMockHost(peerID)
    mockHost.SetFailOnNewStream(true)

    result, err := myFunction(mockHost)
    assertError(t, err, "应该返回错误")
}
```

## 持续集成

### GitHub Actions

项目使用 GitHub Actions 进行持续集成：

- **触发条件**: Push、Pull Request、定时任务
- **测试矩阵**: 多个 Go 版本（1.21, 1.22, 1.23）
- **测试步骤**: 运行测试、生成覆盖率报告、检查覆盖率阈值
- **代码质量**: 运行 golangci-lint 进行静态分析

### 本地 CI 检查

在提交代码前，运行本地检查：

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

## 覆盖率监控

### 覆盖率目标

| 模块 | 目标覆盖率 | 当前覆盖率 | 状态 |
|------|----------|----------|------|
| 总体 | >50% | ~60-65% | ✅ 已达标 |
| cmd/pchat | >50% | ~40-45% | ⚠️ 接近达标 |
| internal/discovery | >40% | ~40-42% | ✅ 已达标 |
| cmd/registry | >30% | 59.1% | ✅ 已超标 |
| internal/crypto | >70% | 81.4% | ✅ 已达标 |

### 覆盖率监控脚本

使用 `scripts/coverage_monitor.sh` 监控覆盖率：

```bash
# 运行覆盖率监控
./scripts/coverage_monitor.sh

# 查看覆盖率历史
cat coverage_history.txt
```

### 覆盖率报告

- **HTML 报告**: `coverage.html` - 可视化覆盖率
- **文本摘要**: `coverage_summary.txt` - 文本格式摘要
- **历史记录**: `coverage_history.txt` - 覆盖率历史趋势

### Makefile 命令

```bash
# 生成覆盖率报告
make cover

# 检查覆盖率阈值
make cover-check

# 显示覆盖率摘要
make cover-summary
```

## 测试维护

### 测试更新原则

1. **代码变更时更新测试**: 当修改代码时，确保相关测试也更新
2. **保持测试独立**: 每个测试应该独立运行，不依赖其他测试
3. **清理测试资源**: 使用 `defer` 和 `t.Cleanup()` 清理资源
4. **避免测试间共享状态**: 每个测试应该使用独立的状态

### 测试性能

1. **使用并行测试**: 对于独立的测试，使用 `t.Parallel()`
2. **跳过长时间测试**: 使用 `testing.Short()` 跳过压力测试
3. **优化测试设置**: 使用 `b.ResetTimer()` 排除设置时间

### 测试文档

1. **测试注释**: 为复杂的测试添加注释说明
2. **测试文档**: 在 `docs/TESTING_GUIDE.md` 中记录测试指南
3. **覆盖率报告**: 定期更新 `TEST_COVERAGE_REPORT.md`

## 测试工具

### 测试辅助函数库

位置: `cmd/pchat/test_helpers.go`

主要函数:
- `setupTestEnvironment()` - 创建完整测试环境
- `cleanupHosts()` - 清理测试 hosts
- `createTestMessage()` - 创建测试消息
- `waitForCondition()` - 等待条件满足
- 断言函数库

### Mock 对象库

位置: `cmd/pchat/test_mocks.go`

主要组件:
- `NetworkHost` 接口
- `MockHost` - 实现 NetworkHost 接口
- `MockStream` - 实现 network.Stream 接口

## 持续改进

### 定期审查

1. **每周审查**: 检查覆盖率趋势
2. **每月审查**: 审查测试质量和覆盖率目标
3. **季度审查**: 评估测试策略和最佳实践

### 改进建议

1. **识别低覆盖率函数**: 使用 `go tool cover -func` 识别
2. **添加缺失测试**: 为低覆盖率函数添加测试
3. **优化测试性能**: 识别并优化慢速测试
4. **改进测试质量**: 添加更多错误场景和边界条件测试

## 参考资源

- [Go 测试文档](https://golang.org/pkg/testing/)
- [测试覆盖率文档](https://golang.org/cmd/cover/)
- [测试最佳实践](https://golang.org/doc/effective_go#testing)
- [项目测试指南](./TESTING_GUIDE.md)
- [测试覆盖率报告](../TEST_COVERAGE_REPORT.md)

## 更新日志

- 2025-11-18: 创建测试最佳实践文档
  - 添加测试策略说明
  - 添加测试组织指南
  - 添加测试编写指南
  - 添加持续集成说明
  - 添加覆盖率监控说明
  - 添加测试维护指南
