# 测试覆盖率改进建议

## 当前状态

- **总体覆盖率**: 31.1%
- **目标覆盖率**: > 70%

## 优先级分析

### 🔴 高优先级（覆盖率 0%，关键功能）

#### 1. main.go - 核心网络处理函数

| 函数 | 覆盖率 | 优先级 | 建议 |
|------|--------|--------|------|
| `handleStream` | 0% | 🔴 高 | 使用 mock host 测试消息接收和处理 |
| `handleKeyExchange` | 0% | 🔴 高 | 测试公钥交换流程 |
| `handleFileTransfer` | 0% | 🔴 高 | 测试文件传输的完整流程 |
| `connectToPeer` | 部分 | 🔴 高 | 测试连接建立、超时、错误处理 |
| `sendOfflineNotification` | 0% | 🟡 中 | 测试离线通知发送 |
| `shutdownConnections` | 0% | 🟡 中 | 测试优雅关闭 |
| `cleanupResources` | 0% | 🟡 中 | 测试资源清理 |

**改进建议**:
```go
// 示例：测试 handleStream
func TestHandleStream(t *testing.T) {
    mn, hosts := setupMockHosts(t, 2)
    defer cleanupHosts(hosts)
    
    // 设置密钥
    privKey, pubKey, _ := generateKeys()
    
    // 设置流处理器
    hosts[0].SetStreamHandler(protocolID, func(s network.Stream) {
        handleStream(s, privKey)
    })
    
    // 创建流并发送消息
    stream, _ := hosts[1].NewStream(ctx, hosts[0].ID(), protocolID)
    // 发送加密消息
    // 验证消息被正确处理
}
```

#### 2. ui.go - UI 核心函数

| 函数 | 覆盖率 | 优先级 | 建议 |
|------|--------|--------|------|
| `initUI` | 0% | 🔴 高 | 测试 UI 组件初始化 |
| `Run` | 0% | 🔴 高 | 测试 UI 运行和事件循环 |
| `processInput` | 0% | 🔴 高 | 测试命令解析和处理 |
| `handleCommand` | 部分 | 🔴 高 | 测试所有命令分支 |
| `sendMessage` | 部分 | 🟡 中 | 测试消息发送（已有部分测试） |
| `callUser` | 0% | 🟡 中 | 测试用户呼叫 |
| `sendFile` | 0% | 🟡 中 | 测试文件发送 |

**改进建议**:
```go
// 示例：测试 UI 命令处理
func TestChatUI_HandleCommand(t *testing.T) {
    ctx := context.Background()
    h, _ := libp2p.New()
    defer h.Close()
    
    ui := NewChatUI(ctx, h, privKey, pubKey, nil, nil, "test")
    
    // 测试各种命令
    tests := []struct {
        cmd string
        wantErr bool
    }{
        {"/help", false},
        {"/list", false},
        {"/call Alice", false},
        {"/invalid", true},
    }
    
    for _, tt := range tests {
        ui.handleCommand(tt.cmd)
        // 验证命令执行结果
    }
}
```

#### 3. dht_discovery.go - DHT 发现函数

| 函数 | 覆盖率 | 优先级 | 建议 |
|------|--------|--------|------|
| `NewDHTDiscovery` | 0% | 🔴 高 | 测试 DHT 初始化 |
| `startPeriodicTasks` | 0% | 🟡 中 | 测试定期任务启动 |
| `AnnounceSelf` | 34% | 🟡 中 | 增加成功和失败场景测试 |
| `DiscoverUsers` | 0% | 🟡 中 | 测试用户发现 |
| `Close` | 0% | 🟡 中 | 测试资源清理 |

**改进建议**:
```go
// 示例：测试 NewDHTDiscovery
func TestNewDHTDiscovery(t *testing.T) {
    ctx := context.Background()
    h, _ := libp2p.New()
    defer h.Close()
    
    dd, err := NewDHTDiscovery(ctx, h, "testuser")
    if err != nil {
        t.Fatalf("创建 DHT Discovery 失败: %v", err)
    }
    defer dd.Close()
    
    // 验证 DHT 已初始化
    if dd.dht == nil {
        t.Error("DHT 应该已初始化")
    }
}
```

### 🟡 中优先级（覆盖率 < 50%）

#### 4. main.go - 辅助函数

| 函数 | 覆盖率 | 建议 |
|------|--------|------|
| `queryUser` | 12.4% | 测试所有查询路径（Registry、DHT、已连接peer） |
| `callUser` | 70.2% | 测试错误场景和边界条件 |
| `hangupPeer` | 60.2% | 测试各种断开场景 |
| `listOnlineUsers` | 0% | 测试用户列表显示 |
| `sendFileToPeers` | 75.5% | 测试大文件、多peer场景 |
| `sendFile` | 76.7% | 测试文件分块和错误处理 |

#### 5. registry.go - 注册服务器函数

| 函数 | 覆盖率 | 建议 |
|------|--------|------|
| `Register` | 84% | 测试网络错误、超时场景 |
| `SendHeartbeat` | 84% | 测试心跳失败重试 |
| `ListClients` | 81% | 测试空列表、大量客户端 |
| `LookupClient` | 84% | 测试未找到、前缀匹配 |

### 🟢 低优先级（覆盖率 > 80%）

这些函数已经有较好的测试覆盖，可以优先处理高优先级函数。

## 具体改进策略

### 1. 使用 Mock 对象

**问题**: 很多函数需要真实的网络连接，难以测试。

**解决方案**:
- 使用 `mocknet` 创建模拟网络
- 使用接口抽象网络操作
- 使用依赖注入

**示例**:
```go
// 创建网络接口
type NetworkHost interface {
    NewStream(ctx context.Context, peerID peer.ID, protocolID protocol.ID) (network.Stream, error)
    Network() network.Network
    ID() peer.ID
}

// 在测试中使用 mock
type mockHost struct {
    // 实现 NetworkHost 接口
}
```

### 2. 测试工具函数

**建议**: 创建测试辅助函数

```go
// test_helpers.go
func setupTestEnvironment(t *testing.T) (*mocknet.Mocknet, []host.Host, *rsa.PrivateKey, rsa.PublicKey) {
    t.Helper()
    mn := mocknet.New()
    hosts := make([]host.Host, 2)
    // ... 初始化
    return mn, hosts, privKey, pubKey
}

func createTestMessage(t *testing.T, content string) string {
    // 创建测试消息
}
```

### 3. 表驱动测试

**建议**: 对所有验证函数使用表驱动测试

```go
func TestFunction_TableDriven(t *testing.T) {
    tests := []struct {
        name    string
        input   interface{}
        want    interface{}
        wantErr bool
    }{
        // 测试用例
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 执行测试
        })
    }
}
```

### 4. 集成测试

**建议**: 添加端到端集成测试

```go
func TestEndToEnd_MessageFlow(t *testing.T) {
    // 1. 启动两个节点
    // 2. 建立连接
    // 3. 交换公钥
    // 4. 发送消息
    // 5. 验证消息接收
}
```

### 5. 错误场景测试

**建议**: 专门测试错误处理

```go
func TestFunction_ErrorScenarios(t *testing.T) {
    // 测试 nil 输入
    // 测试无效输入
    // 测试网络错误
    // 测试超时
    // 测试资源不足
}
```

## 测试文件组织建议

### 当前结构
```
cmd/pchat/
├── main_test.go
├── validation_test.go
├── dht_discovery_test.go
├── registry_test.go
└── ...
```

### 建议结构
```
cmd/pchat/
├── main_test.go              # 主函数测试
├── validation_test.go        # 验证函数测试 ✅
├── network_test.go           # 网络操作测试（新建）
├── message_test.go           # 消息处理测试（新建）
├── file_transfer_test.go     # 文件传输测试（新建）
├── ui_test.go                # UI 测试（扩展）
├── dht_discovery_test.go     # DHT 测试（扩展）
├── registry_test.go          # 注册服务器测试（扩展）
└── test_helpers.go           # 测试辅助函数（新建）
```

## 覆盖率目标分解

### 短期目标（1-2周）
- 核心网络函数: 0% → 60%
- UI 核心函数: 0% → 50%
- 总体覆盖率: 31% → 45%

### 中期目标（1个月）
- 核心网络函数: 60% → 80%
- UI 核心函数: 50% → 70%
- 总体覆盖率: 45% → 60%

### 长期目标（2-3个月）
- 所有关键函数: > 80%
- 总体覆盖率: > 70%

## 实施步骤

### 第一步：建立测试基础设施
1. ✅ 创建 `test_helpers.go`（测试辅助函数）
2. ✅ 创建 `network_test.go`（网络测试框架）
3. ✅ 设置 mock 环境

### 第二步：测试核心功能
1. `handleStream` - 消息接收和处理
2. `handleKeyExchange` - 公钥交换
3. `handleFileTransfer` - 文件传输
4. `connectToPeer` - 连接建立

### 第三步：测试 UI 功能
1. `initUI` - UI 初始化
2. `processInput` - 输入处理
3. `handleCommand` - 命令处理
4. `sendMessage` - 消息发送

### 第四步：测试辅助功能
1. `queryUser` - 用户查询
2. `listOnlineUsers` - 用户列表
3. `sendOfflineNotification` - 离线通知
4. `cleanupResources` - 资源清理

### 第五步：测试边界和错误场景
1. 网络错误处理
2. 超时处理
3. 资源不足
4. 并发安全

## 测试最佳实践

### 1. 测试命名
```go
// 好的命名
func TestFunctionName_Scenario_ExpectedResult(t *testing.T)

// 示例
func TestHandleStream_ValidMessage_ProcessesCorrectly(t *testing.T)
func TestHandleStream_InvalidMessage_ReturnsError(t *testing.T)
```

### 2. 测试组织
```go
func TestFunction(t *testing.T) {
    t.Run("正常场景", func(t *testing.T) {
        // 测试正常流程
    })
    
    t.Run("错误场景", func(t *testing.T) {
        // 测试错误处理
    })
    
    t.Run("边界条件", func(t *testing.T) {
        // 测试边界值
    })
}
```

### 3. 测试清理
```go
func TestFunction(t *testing.T) {
    // 设置
    resource := setupResource(t)
    defer cleanupResource(t, resource) // 确保清理
    
    // 测试
}
```

### 4. 并行测试
```go
func TestFunction(t *testing.T) {
    t.Parallel() // 允许并行执行
    
    // 测试代码
}
```

## 工具和资源

### 推荐工具
1. **go test** - 基础测试工具
2. **go tool cover** - 覆盖率分析
3. **testify** - 测试断言库（可选）
4. **gomock** - Mock 生成工具（可选）

### 参考资源
1. [Go Testing Best Practices](https://golang.org/doc/effective_go#testing)
2. [Table-Driven Tests](https://github.com/golang/go/wiki/TableDrivenTests)
3. [Mocking in Go](https://github.com/golang/mock)

## 总结

提高测试覆盖率的关键是：
1. **优先级排序** - 先测试关键功能
2. **使用 Mock** - 隔离依赖，提高测试速度
3. **表驱动测试** - 覆盖更多场景
4. **持续改进** - 每次代码变更都更新测试

通过系统化的测试改进，可以将覆盖率从当前的 31.1% 提升到 70% 以上。

