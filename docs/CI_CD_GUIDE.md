# PChat CI/CD 指南

## 概述

本文档描述了 PChat 项目的持续集成和持续部署（CI/CD）流程。

## 目录

1. [GitHub Actions 工作流](#github-actions-工作流)
2. [本地测试](#本地测试)
3. [覆盖率监控](#覆盖率监控)
4. [部署流程](#部署流程)

## GitHub Actions 工作流

### 工作流文件

位置: `.github/workflows/test.yml`

### 触发条件

- **Push**: 推送到 `main`、`master`、`develop` 分支
- **Pull Request**: 创建或更新 Pull Request
- **定时任务**: 每天 UTC 时间 2:00 运行（北京时间 10:00）

### 工作流任务

#### 1. 测试 (test)

- **矩阵策略**: 在 Go 1.21、1.22、1.23 上运行
- **测试命令**: `go test ./... -v -race -coverprofile=coverage.out`
- **覆盖率报告**: 生成 HTML 和文本格式的覆盖率报告
- **上传产物**: 上传覆盖率报告到 GitHub Actions

#### 2. 覆盖率报告 (coverage)

- **依赖**: 等待所有测试任务完成
- **合并报告**: 合并所有 Go 版本的覆盖率报告
- **生成摘要**: 生成覆盖率摘要并添加到 GitHub Actions 摘要

#### 3. 代码检查 (lint)

- **工具**: golangci-lint
- **超时**: 5 分钟
- **检查项**: 代码风格、潜在错误、最佳实践

#### 4. 构建 (build)

- **构建目标**: 构建所有二进制文件
- **上传产物**: 上传构建的二进制文件

### 查看工作流

1. 访问 GitHub 仓库
2. 点击 "Actions" 标签
3. 选择 "Tests" 工作流
4. 查看运行历史和详细信息

## 本地测试

### 运行测试

```bash
# 运行所有测试
make test

# 运行特定包的测试
go test ./cmd/pchat -v

# 运行特定测试函数
go test ./cmd/pchat -run TestHandleStream -v
```

### 生成覆盖率报告

```bash
# 生成覆盖率报告
make cover

# 查看覆盖率摘要
make cover-summary

# 检查覆盖率阈值
make cover-check
```

### 代码质量检查

```bash
# 格式化代码
make fmt

# 检查代码错误
make vet

# 运行静态分析
make lint

# 整理依赖
make tidy
```

## 覆盖率监控

### 覆盖率监控脚本

位置: `scripts/coverage_monitor.sh`

功能:
- 生成覆盖率报告
- 检查覆盖率阈值
- 记录覆盖率历史
- 显示各模块覆盖率状态

### 使用方法

```bash
# 运行覆盖率监控
./scripts/coverage_monitor.sh

# 查看覆盖率历史
cat coverage_history.txt

# 查看覆盖率摘要
cat coverage_summary.txt

# 打开 HTML 覆盖率报告
open coverage.html  # macOS
xdg-open coverage.html  # Linux
```

### 覆盖率阈值

| 模块 | 目标覆盖率 |
|------|----------|
| 总体 | >50% |
| cmd/pchat | >50% |
| internal/discovery | >40% |
| cmd/registry | >30% |
| internal/crypto | >70% |

### 覆盖率历史

覆盖率历史记录在 `coverage_history.txt` 文件中，格式：

```
时间戳,总体覆盖率,cmd/pchat,internal/discovery,cmd/registry,internal/crypto
2025-11-18 10:00:00,60.5,42.3,41.2,59.1,81.4
```

## 部署流程

### 构建二进制文件

```bash
# 构建所有二进制文件
make build

# 构建特定二进制文件
make bin/pchat
make bin/registryd
```

### 验证构建

```bash
# 检查二进制文件
ls -lh bin/

# 测试二进制文件
./bin/pchat --help
./bin/registryd --help
```

### 发布流程

1. **创建 Release**: 在 GitHub 上创建新的 Release
2. **上传二进制文件**: 上传构建的二进制文件
3. **更新文档**: 更新 README 和文档
4. **通知用户**: 通知用户新版本发布

## 最佳实践

### 提交前检查

在提交代码前，运行以下检查：

```bash
# 1. 格式化代码
make fmt

# 2. 检查代码错误
make vet

# 3. 运行测试
make test

# 4. 生成覆盖率报告
make cover

# 5. 检查覆盖率阈值
make cover-check
```

### Pull Request 检查清单

创建 Pull Request 前，确保：

- [ ] 所有测试通过
- [ ] 覆盖率达标或提升
- [ ] 代码格式化
- [ ] 没有代码错误
- [ ] 添加了必要的测试
- [ ] 更新了相关文档

### 持续改进

1. **定期审查**: 每周审查 CI/CD 流程
2. **优化性能**: 优化慢速测试和构建
3. **更新工具**: 定期更新 CI/CD 工具和依赖
4. **监控指标**: 监控测试执行时间和覆盖率趋势

## 故障排除

### 测试失败

1. **检查日志**: 查看 GitHub Actions 日志
2. **本地复现**: 在本地运行失败的测试
3. **检查依赖**: 确保所有依赖已安装
4. **检查环境**: 确保测试环境正确配置

### 覆盖率下降

1. **查看报告**: 查看覆盖率报告找出下降原因
2. **添加测试**: 为新代码添加测试
3. **更新阈值**: 如果合理，更新覆盖率阈值

### 构建失败

1. **检查 Go 版本**: 确保使用正确的 Go 版本
2. **检查依赖**: 运行 `go mod tidy`
3. **清理构建**: 运行 `make clean` 后重新构建

## 参考资源

- [GitHub Actions 文档](https://docs.github.com/en/actions)
- [Go 测试文档](https://golang.org/pkg/testing/)
- [测试覆盖率文档](https://golang.org/cmd/cover/)
- [项目测试指南](./TESTING_GUIDE.md)
- [测试最佳实践](./TEST_BEST_PRACTICES.md)

## 更新日志

- 2025-11-18: 创建 CI/CD 指南
  - 添加 GitHub Actions 工作流说明
  - 添加本地测试指南
  - 添加覆盖率监控说明
  - 添加部署流程说明
  - 添加最佳实践和故障排除
