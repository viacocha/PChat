# PChat 开发指南

## 项目结构

```
PChat/
├── bin/                 # 编译后的二进制文件
├── cmd/                 # 主程序入口
│   ├── pchat/           # P2P聊天客户端
│   │   └── main.go      # 客户端主程序
│   └── registry/        # 注册服务器
│       └── main.go      # 注册服务器主程序
├── internal/            # 内部包
│   ├── chat/            # 聊天核心逻辑
│   ├── crypto/          # 加密解密模块
│   │   ├── crypto.go    # 加密实现
│   │   └── crypto_test.go # 加密测试
│   ├── discovery/       # 用户发现模块
│   │   └── dht.go       # DHT发现实现
│   ├── filetransfer/    # 文件传输模块
│   ├── network/         # 网络通信模块
│   ├── registry/        # 注册服务器客户端
│   │   └── client.go    # 注册客户端实现
│   ├── rps/             # 石头剪刀布游戏模块
│   └── utils/           # 工具函数
├── docs/                # 文档
│   ├── USAGE.md         # 使用说明
│   ├── TEST_DHT.md      # DHT测试指南
│   ├── DHT_TEST_SUMMARY.md # DHT测试总结
│   └── DEVELOPMENT.md   # 开发指南
├── tests/               # 测试文件
│   └── test_dht.sh      # DHT测试脚本
├── received_files/      # 接收的文件
├── go.mod              # Go模块文件
├── go.sum              # Go依赖校验和
├── Makefile            # 构建脚本
├── README.md           # 项目说明
└── LICENSE             # 许可证文件
```

## 开发环境设置

### 系统要求

- Go 1.16 或更高版本
- Git 版本控制工具
- 支持的操作系统（Linux, macOS, Windows）

### 获取源码

```bash
# 克隆仓库
git clone https://github.com/yourusername/PChat.git
cd PChat

# 安装依赖
make install
# 或者
go mod download
```

## 构建项目

### 使用Makefile构建

```bash
# 构建所有二进制文件
make build

# 构建特定二进制文件
make bin/pchat
make bin/registryd

# 清理构建产物
make clean
```

### 手动构建

```bash
# 构建客户端
go build -o bin/pchat ./cmd/pchat

# 构建注册服务器
go build -o bin/registryd ./cmd/registry
```

## 运行项目

### 运行注册服务器

```bash
# 使用Makefile
make run-registry

# 或直接运行
./bin/registryd
```

### 运行客户端

```bash
# 使用Makefile运行示例
make run-alice
make run-bob

# 或直接运行
./bin/pchat -port 9001 -username Alice
./bin/pchat -port 9002 -username Bob
```

## 测试

### 运行单元测试

```bash
# 运行所有测试
make test
# 或
go test ./...

# 运行特定包的测试
go test ./internal/crypto/...
```

### 运行集成测试

```bash
# 运行DHT测试
make test-dht
# 或
./tests/test_dht.sh
```

### 生成测试覆盖率报告

```bash
# 生成覆盖率报告
make cover
# 或
go test -coverprofile=coverage.txt ./...
go tool cover -html=coverage.txt -o coverage.html
```

## 代码质量

### 代码格式化

```bash
# 格式化代码
make fmt
# 或
go fmt ./...
```

### 代码检查

```bash
# 静态分析
make vet
# 或
go vet ./...

# 代码风格检查
make lint
# 或
golint ./...
```

## 依赖管理

### 添加新依赖

```bash
# 添加依赖
go get github.com/some/package

# 清理未使用的依赖
go mod tidy
```

### 验证依赖

```bash
# 验证依赖完整性
make verify
# 或
go mod verify
```

## 开发流程

### 1. 创建功能分支

```bash
# 创建新功能分支
git checkout -b feature/new-feature

# 或修复bug分支
git checkout -b bugfix/issue-description
```

### 2. 编写代码

- 遵循Go语言编码规范
- 添加必要的注释和文档
- 编写单元测试

### 3. 测试代码

```bash
# 运行测试
make test

# 检查代码质量
make fmt
make vet
```

### 4. 提交更改

```bash
# 添加更改
git add .

# 提交更改
git commit -m "Add new feature: description of changes"

# 推送到远程仓库
git push origin feature/new-feature
```

### 5. 创建Pull Request

在GitHub上创建Pull Request，等待代码审查。

## 模块说明

### crypto模块

提供加密和解密功能：
- AES对称加密
- RSA非对称加密
- 数字签名
- 防重放攻击

### discovery模块

提供用户发现功能：
- DHT去中心化发现
- 用户信息存储和查询

### registry模块

提供注册服务器客户端功能：
- 用户注册和注销
- 心跳保持连接
- 用户列表查询

### chat模块

提供聊天核心功能：
- 消息处理
- 连接管理
- 命令解析

### filetransfer模块

提供文件传输功能：
- 文件分块传输
- 完整性校验
- 进度显示

### rps模块

提供石头剪刀布游戏功能：
- 游戏逻辑
- 自动出拳
- 结果统计

## API文档

### 加密模块API

```go
// GenerateKeys 生成RSA密钥对
func GenerateKeys() (*rsa.PrivateKey, rsa.PublicKey, error)

// EncryptAndSignMessage 加密消息并签名
func EncryptAndSignMessage(msg string, senderPrivKey *rsa.PrivateKey, recipientPubKey *rsa.PublicKey) (string, error)

// DecryptAndVerifyMessage 解密消息并验证签名
func DecryptAndVerifyMessage(encryptedMsg string, recipientPrivKey *rsa.PrivateKey, senderID rsa.PublicKey) (string, bool, error)
```

### DHT发现模块API

```go
// NewDHTDiscovery 创建DHT发现服务
func NewDHTDiscovery(ctx context.Context, h host.Host, username string) (*DHTDiscovery, error)

// AnnounceSelf 广播自己的信息到DHT
func (dd *DHTDiscovery) AnnounceSelf(ctx context.Context)

// LookupUser 查找用户
func (dd *DHTDiscovery) LookupUser(ctx context.Context, username string) (*UserInfo, error)

// ListUsers 列出所有已知用户
func (dd *DHTDiscovery) ListUsers() []*UserInfo
```

### 注册客户端API

```go
// NewRegistryClient 创建注册客户端
func NewRegistryClient(serverAddr string, h host.Host, username string) *RegistryClient

// Register 注册到服务器
func (rc *RegistryClient) Register() error

// Unregister 从服务器注销
func (rc *RegistryClient) Unregister() error

// ListClients 列出所有客户端
func (rc *RegistryClient) ListClients() ([]*ClientInfo, error)

// LookupClient 查找客户端
func (rc *RegistryClient) LookupClient(targetID string) (*ClientInfo, error)
```

## 错误处理

### 常见错误类型

1. **网络连接错误**
   - 检查端口是否被占用
   - 验证节点ID是否正确
   - 确认网络连接正常

2. **加密错误**
   - 检查密钥生成是否成功
   - 验证公钥交换是否完成
   - 确认加密算法支持

3. **DHT错误**
   - 等待DHT网络稳定
   - 检查节点连接状态
   - 验证DHT路由表

### 错误处理最佳实践

- 使用错误包装提供上下文信息
- 记录详细的错误日志
- 提供用户友好的错误提示
- 实现优雅的错误恢复机制

## 性能优化

### 内存优化

- 复用缓冲区减少内存分配
- 及时释放不需要的资源
- 使用对象池管理频繁创建的对象

### 网络优化

- 使用连接池减少连接建立开销
- 实现消息批处理减少网络往返
- 优化数据序列化格式

### 并发优化

- 使用goroutine处理并发任务
- 使用channel进行goroutine间通信
- 避免竞态条件使用互斥锁

## 安全考虑

### 加密安全

- 使用强加密算法（AES-256, RSA-2048）
- 定期更新密钥
- 防止密钥泄露

### 网络安全

- 验证消息来源
- 防止重放攻击
- 检查消息完整性

### 应用安全

- 输入验证和过滤
- 防止注入攻击
- 安全的错误处理

## 调试技巧

### 日志调试

```bash
# 启用详细日志
export PCHAT_DEBUG=1
./bin/pchat -port 9001 -username Alice
```

### 性能分析

```bash
# CPU性能分析
go tool pprof cpu.prof

# 内存性能分析
go tool pprof mem.prof
```

### 网络调试

```bash
# 查看网络连接
netstat -an | grep 900

# 抓包分析
tcpdump -i lo0 port 9001
```

## 贡献指南

### 代码规范

1. 遵循Go语言编码规范
2. 添加必要的注释和文档
3. 编写单元测试
4. 保持代码简洁和可读性

### 提交规范

1. 使用有意义的提交信息
2. 每个提交只包含一个功能或修复
3. 在提交前运行测试
4. 遵循项目的代码风格

### Pull Request流程

1. 创建功能分支
2. 实现功能并添加测试
3. 运行所有测试确保通过
4. 提交Pull Request并描述更改
5. 根据审查意见进行修改

## 发布流程

### 版本管理

使用语义化版本控制：
- 主版本号：不兼容的API修改
- 次版本号：向后兼容的功能性新增
- 修订号：向后兼容的问题修正

### 发布步骤

1. 更新版本号
2. 更新CHANGELOG
3. 创建Git标签
4. 构建发布版本
5. 发布到GitHub Releases

## 常见问题

### 构建问题

**问题：依赖下载失败**
```bash
# 解决方案：使用代理或更换源
go env -w GOPROXY=https://goproxy.cn,direct
```

**问题：编译错误**
```bash
# 解决方案：清理并重新构建
make clean
make build
```

### 运行问题

**问题：端口被占用**
```bash
# 解决方案：使用不同端口
./bin/pchat -port 9003 -username Charlie
```

**问题：连接失败**
```bash
# 解决方案：检查节点ID和网络连接
# 确保防火墙允许连接
```

### 测试问题

**问题：测试超时**
```bash
# 解决方案：增加超时时间
go test -timeout 30s ./...
```

**问题：覆盖率低**
```bash
# 解决方案：编写更多测试用例
# 特别是边界条件和错误处理
```

## 未来改进

### 功能增强

1. **多媒体支持**：支持图片、音频、视频传输
2. **群聊功能**：支持多人群聊
3. **消息历史**：本地消息存储和查询
4. **联系人管理**：好友列表和黑名单

### 性能优化

1. **连接优化**：改进NAT穿透和连接稳定性
2. **存储优化**：优化DHT存储和查询性能
3. **传输优化**：实现更高效的数据传输协议

### 安全增强

1. **端到端加密**：更强的加密算法和密钥管理
2. **身份验证**：用户身份验证和授权机制
3. **隐私保护**：更好的隐私保护和匿名性

## 联系方式

如有问题或建议，请通过以下方式联系：

- 提交GitHub Issue
- 发送邮件到项目维护者
- 在项目讨论区提问