# 测试覆盖率报告

## 当前覆盖率状态

运行 `go test ./... -coverprofile=coverage.out` 后，各模块的覆盖率如下：

### 模块覆盖率

- **cmd/pchat**: 30.7%（↑↑，从27.0%提升）→ 目标: >50%
- **cmd/registry**: 25.9%（↑↑，从19.8%提升）→ 目标: >30%
- **internal/crypto**: 81.4% ✅ (已达标)
- **internal/discovery**: 17.2%（↑，从16.6%提升）→ 目标: >40%
- **internal/registry**: 62.7%（↑）→ 目标: >30% ✅ (已达标)

- **当前总体覆盖率**: 30.7%（↑↑，从29.5%提升）
- **目标总体覆盖率**: >50%

## 已添加的测试

### 1. 命令解析测试 (`cmd/pchat/command_test.go`)
- ✅ `TestNormalizeCommandCLI` - 测试命令规范化（简写形式）
- ✅ `TestShowHelp` - 测试帮助信息显示（不panic）
- ✅ `TestCommandParsing` - 测试命令解析逻辑
- ✅ `TestNormalizeCommandCLI_EdgeCases` - 测试命令规范化的边界情况（多斜杠、特殊字符、大小写等）
- ✅ `TestIsConnectionClosedError` - 测试连接关闭错误判断（各种错误格式）
- ✅ `TestFormatRegistryList_Basic` - 测试注册列表格式化与状态显示
- ✅ `TestFormatRegistryList_DedupAndSelf` - 测试去重与自标记逻辑

### 2. DHT Discovery 测试 (`cmd/pchat/dht_discovery_test.go`, `cmd/pchat/dht_discovery_logic_test.go`, `internal/discovery/dht_logic_test.go`)
- ✅ `TestUserInfoValidator_Validate` - 测试用户信息验证器
- ✅ `TestUserInfoValidator_Select` - 测试选择最佳记录
- ✅ `TestUserInfoJSON` - 测试用户信息JSON序列化
- ✅ `TestDHTDiscovery_ListUsers` - 测试列出用户逻辑
- ✅ `TestDHTDiscovery_GetUserByPeerID` - 测试通过PeerID获取用户
- ✅ `TestRecordUserFromConnection_EdgeCases` - 测试记录用户的边界情况（空用户名、空peerID、nil discovery）
- ✅ `TestRecordUserFromConnection_UpdatesExisting` - 测试更新已存在用户
- ✅ `TestListUsers_Empty` - 测试空列表
- ✅ `TestGetUserByPeerID_NotFound` - 测试查找不存在的用户
- ✅ `TestDHTDiscovery_RecordUserFromConnection` - 测试连接记录逻辑
- ✅ `TestDHTDiscovery_ListUsersFiltersExpired` - 过期用户过滤
- ✅ `TestDHTDiscovery_CleanupExpiredUsers` - 过期清理

### 3. 加密解密测试 (`cmd/pchat/crypto_test.go`)
- ✅ `TestGenerateKeys_ValidKeys` - 测试密钥生成
- ✅ `TestAESDecrypt_ValidCiphertext` - 测试有效的AES解密
- ✅ `TestDecryptMessage_ValidData` - 测试有效的消息解密
- ✅ `TestEncryptMessageWithPubKey_NilKey` - 测试nil公钥
- ✅ `TestDecryptMessage_NilKey` - 测试nil私钥
- ✅ `TestAESEncryptDecrypt_RoundTrip` - 测试AES加密解密往返
- ✅ `TestRSAKeyGeneration` - 测试RSA密钥生成
- ✅ `TestEncryptAndSignMessage_EmptyMessage` - 测试空消息加密

### 4. Registry / 集成测试
- ✅ `cmd/pchat/registry_test.go` - 客户端信息结构测试
- ✅ `cmd/registry/main_test.go` - 注册服务器逻辑测试（完整请求处理流程）
  - ✅ `TestRegistryServer_HandleRegisterAndList` - 注册和列表流程
  - ✅ `TestRegistryServer_HeartbeatUpdatesLastSeen` - 心跳更新LastSeen
  - ✅ `TestRegistryServer_HeartbeatUnregisteredClient` - 未注册客户端心跳错误处理
  - ✅ `TestRegistryServer_LookupByPeerID` - 通过PeerID查找
  - ✅ `TestRegistryServer_LookupByUsername` - 通过用户名查找
  - ✅ `TestRegistryServer_LookupNotFound` - 查找不存在客户端
  - ✅ `TestRegistryServer_Unregister` - 注销客户端
  - ✅ `TestRegistryServer_UnregisterNonexistent` - 注销不存在客户端
  - ✅ `TestRegistryServer_UnknownMessageType` - 未知消息类型处理
  - ✅ `TestRegistryServer_RegisterPreservesRegisterTime` - 重新注册保留原始注册时间
  - ✅ `TestRegistryServer_CleanupExpiredClients` - 清理过期客户端（实际清理逻辑）
  - ✅ `TestRegistryServer_HandleRequest_InvalidJSON` - 处理无效JSON
  - ✅ `TestRegistryServer_HandleRequest_EmptyMessage` - 处理空消息
  - ✅ `TestRegistryServer_Start` - 服务器启动和监听
  - ✅ `TestRegistryServer_Start_InvalidPort` - 无效端口处理
  - ✅ `TestRegistryServer_Start_AlreadyInUse` - 端口已被占用处理
  - ✅ `TestRegistryServer_ConcurrentRegister` - 并发注册（10个客户端）
  - ✅ `TestRegistryServer_ConcurrentHeartbeat` - 并发心跳（10个心跳）
  - ✅ `TestRegistryServer_ListEmpty` - 空列表处理
  - ✅ `TestRegistryServer_RegisterWithEmptyFields` - 空字段注册
  - ✅ `TestRegistryServer_RegisterMultipleAddresses` - 多个地址注册
  - ✅ `TestRegistryServer_LookupCaseSensitive` - 查找大小写敏感性
  - ✅ `TestRegistryServer_HandleRequest_EncodingError` - 编码错误处理
  - ✅ `TestRegistryServer_RegisterUpdateAddresses` - 注册时更新地址
  - ✅ `TestRegistryServer_LookupByPeerIDAndUsername` - 同时匹配PeerID和Username
- ✅ `internal/registry/client_test.go` - 注册客户端结构测试

- ✅ `TestHandleStreamWithMockHost`（mock libp2p host，多主机交互）
- ✅ `TestSendFileBetweenMockHosts`（mocknet sendFile 交互）
- ✅ `cmd/pchat/integration_test.go` - 命令解析、上下文管理
- ✅ `cmd/pchat/ui_test.go` - UI 排序与时长格式逻辑
- ✅ `cmd/pchat/file_utils_test.go` - 文件分块与范围计算
- ✅ `cmd/pchat/file_transfer_helpers_test.go` - 文件头/分块签名与加密
- ✅ `cmd/pchat/network_helpers_test.go` - 网络错误判定辅助方法
- ✅ `cmd/pchat/mock_host_test.go` - Mock libp2p host 网络交互测试
  - `TestHandleStreamWithMockHost` - 测试消息流处理（加密、签名、解密、验证）
  - `TestSendFileBetweenMockHosts` - 测试文件传输（头部、分块、加密、解密）
  - `TestMultiNodeDHTCommandFlow` - 测试多节点 DHT/CLI 命令流程
    - `ListUsers` - 测试 `/list` 命令（列出所有用户）
    - `CallUserViaDHT` - 测试 `/call` 命令（通过 DHT 查找并连接用户）
    - `SendFileWithDHT` - 测试 `sendFile` 结合 DHT Discovery（文件传输前查找用户信息）
    - `SendMessageWithDHT` - 测试消息发送结合 DHT Discovery（通过 DHT 获取发送者信息）
- ✅ `cmd/pchat/comprehensive_command_test.go` - 完整的多节点命令流测试
  - `TestComprehensiveCommandFlow_DHTMode` - DHT 模式下的完整命令流程测试
    - `ListCommand_DHT` - 测试 `/list` 命令（DHT 模式）
    - `QueryCommand_DHT` - 测试 `/query` 命令（DHT 模式）
    - `CallCommand_DHT` - 测试 `/call` 命令（DHT 模式）
    - `HangupCommand_DHT` - 测试 `/hangup` 命令（DHT 模式）
    - `SendFileCommand_DHT` - 测试 `/sendfile` 命令（DHT 模式）
    - `RPSCommand_DHT` - 测试 `/rps` 命令（DHT 模式）
  - `TestComprehensiveCommandFlow_RegistryMode` - Registry 模式下的完整命令流程测试
    - `ListCommand_Registry` - 测试 `/list` 命令（Registry 模式）
    - `QueryCommand_Registry` - 测试 `/query` 命令（Registry 模式）
    - `CallCommand_Registry` - 测试 `/call` 命令（Registry 模式）
    - `Heartbeat_Registry` - 测试心跳功能
    - `Unregister_Registry` - 测试注销功能
  - `TestAllCommandsIntegration` - 所有命令的集成流程测试
    - `CompleteCommandFlow` - 完整的命令流程（/list → /call → 发送消息 → /hangup）
- ✅ `cmd/pchat/command_flow_test.go` - 命令处理流程测试（新增）
  - `TestHandleCLICommand_Help` - 测试帮助命令在不同模式下的行为
  - `TestHandleCLICommand_List` - 测试列表命令（无模式/DHT模式）
  - `TestHandleCLICommand_Call` - 测试呼叫命令（无参数/有参数/简写）
  - `TestHandleCLICommand_Query` - 测试查询命令（无参数/有参数/简写）
  - `TestHandleCLICommand_RPS` - 测试石头剪刀布命令
  - `TestHandleCLICommand_Hangup` - 测试挂断命令（所有/指定用户/简写）
  - `TestHandleCLICommand_SendFile` - 测试发送文件命令（无参数/有参数/简写）
  - `TestHandleCLICommand_Unknown` - 测试未知命令处理
  - `TestHandleCLICommand_EmptyCommand` - 测试空命令和边界情况
  - `TestHandleCLICommand_RegistryMode` - 测试注册模式下的命令行为
  - `TestHandleCLICommand_CommandNormalization` - 测试命令规范化在命令处理中的应用
  - `TestHandleCLICommand_ComplexArgs` - 测试复杂参数的命令处理
  - `TestSendCLIMessage_NoConnections` - 测试无连接时发送消息
  - `TestSendCLIMessage_WithConnection` - 测试有连接时发送消息

## 测试策略

### 已完成
1. ✅ 为纯函数添加单元测试（命令解析、验证器）
2. ✅ 为数据结构添加测试（JSON序列化、字段验证）
3. ✅ 为加密解密核心逻辑添加测试
4. ✅ 添加边界条件测试（nil值、空值、无效输入）

### 待完成
1. ⏳ 为网络相关函数添加更全面的mock测试（当前已覆盖DHT缓存逻辑，仍需覆盖真实libp2p Host交互）
2. ⏳ 为UI相关函数添加更深入的交互测试（当前已覆盖排序/格式化等纯逻辑）
3. ⏳ 添加端到端集成测试（需要启动实际服务）
4. ⏳ 为文件传输添加真实网络流测试（当前覆盖分块逻辑）
5. ⏳ 为流处理添加更多超时/错误场景验证（当前覆盖连接关闭判定）

## 提升覆盖率的建议

### 短期目标（1-2周）
1. **命令处理逻辑**: 当前 72.5%（handleCLICommand），78.3%（sendCLIMessage），目标 60%+ ✅ **已达标**
   - ✅ 已完成基础测试（`command_test.go`）
   - ✅ 已添加边界条件测试（命令规范化、错误判断等）
   - ✅ 已添加更多命令处理流程测试（`command_flow_test.go`）
     - 测试了所有主要命令：/help, /list, /call, /query, /rps, /hangup, /sendfile
     - 测试了命令简写形式（/c, /l, /q, /r, /h, /d, /s, /f）
     - 测试了不同模式（Registry/DHT/无模式）下的命令行为
     - 测试了空命令、未知命令、复杂参数等边界情况
     - 测试了 `sendCLIMessage` 函数（无连接和有连接场景）

2. **DHT Discovery**: 当前 17.2%，目标 40%+
   - ✅ 已完成验证器测试
   - ✅ 已添加逻辑测试（记录、查找、清理等）
   - ✅ 已添加 mock libp2p host 网络交互测试（`TestHandleStreamWithMockHost`, `TestSendFileBetweenMockHosts`, `TestMultiNodeDHTCommandFlow`）

3. **Registry**: 当前 19.8%（服务器）+ 62.7%（客户端），目标 30%+
   - ✅ 已完成结构测试
   - ✅ 已添加完整服务器逻辑测试（注册、心跳、查找、注销等）
   - ✅ 客户端测试已达标（62.7%）
   - ⏳ 服务器测试接近目标（19.8% → 30%）

### 中期目标（1个月）
1. **总体覆盖率**: 目标 50%+
2. **核心功能覆盖率**: 目标 70%+
3. **关键路径覆盖率**: 目标 80%+

### 长期目标（3个月）
1. **总体覆盖率**: 目标 70%+
2. **所有模块覆盖率**: 目标 >50%
3. **关键安全功能**: 目标 90%+

## 测试工具和命令

### 运行所有测试
```bash
go test ./... -v
```

### 生成覆盖率报告
```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

### 运行特定包的测试
```bash
go test ./cmd/pchat -v
go test ./internal/crypto -v
go test ./cmd/registry -v
./scripts/run_mock_tests.sh   # 运行 mock libp2p host 相关测试
```

## 注意事项

1. **Mock需求**: 许多函数需要libp2p host对象，需要使用mock或测试辅助函数
2. **网络测试**: 网络相关测试可能需要实际网络环境或mock
3. **UI测试**: UI相关测试需要mock tview或使用headless模式
4. **集成测试**: 端到端测试需要启动实际服务，可能需要测试脚本

## 下一步行动

1. 继续为可测试的函数添加单元测试
2. 创建mock对象用于网络和UI测试
3. 添加更多边界条件和错误处理测试
4. 逐步提高覆盖率到目标水平

