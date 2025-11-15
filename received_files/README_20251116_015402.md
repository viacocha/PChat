# PChat - Go-based Decentralized Secure Chat

[![Go Report Card](https://goreportcard.com/badge/github.com/yourusername/PChat)](https://goreportcard.com/report/github.com/yourusername/PChat)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## 📖 项目概述

PChat 是一个基于 Go 语言开发的去中心化安全聊天工具。它利用 P2P 网络技术和端到端加密（AES + RSA）来确保隐私和安全。没有中央服务器，用户直接相互连接，实现安全和匿名通信。

## 🌟 主要特性

- **🔒 端到端加密**: 消息使用 AES 对称加密和 RSA 非对称加密保护
- **🌐 P2P 网络**: 无中心服务器，用户直接通信
- **🕵️ 匿名性**: 用户无需注册或使用真实姓名
- **📁 安全文件传输**: 支持加密文件传输，支持大文件分块传输
- **🎮 娱乐功能**: 内置石头剪刀布游戏
- **🔄 自动发现**: 支持 DHT 去中心化发现和注册服务器两种模式
- **🛡️ 防重放攻击**: 使用 nonce 和时间戳防止消息重放攻击
- **🎯 优雅关闭**: 完整的资源清理和优雅关闭机制

## 🏗️ 技术架构

```
PChat/
├── cmd/                 # 主程序入口
│   ├── pchat/           # P2P聊天客户端
│   └── registry/        # 注册服务器
├── internal/            # 内部包
│   ├── chat/            # 聊天核心逻辑
│   ├── crypto/          # 加密解密模块
│   ├── discovery/       # 用户发现模块
│   ├── filetransfer/    # 文件传输模块
│   ├── network/         # 网络通信模块
│   ├── registry/        # 注册服务器客户端
│   ├── rps/             # 石头剪刀布游戏模块
│   └── utils/           # 工具函数
├── docs/                # 文档
├── tests/               # 测试文件
└── received_files/      # 接收的文件
```

## 🚀 快速开始

### 系统要求

- Go 1.16 或更高版本
- 支持的操作系统（Linux, macOS, Windows）

### 安装

```bash
# 克隆仓库
git clone https://github.com/yourusername/PChat.git
cd PChat

# 构建项目
go build -o bin/pchat ./cmd/pchat
go build -o bin/registryd ./cmd/registry
```

### 使用方法

#### 1. 启动注册服务器（可选）

```bash
./bin/registryd
```

#### 2. 启动客户端

```bash
# 使用注册服务器模式
./bin/pchat -port 9001 -registry 127.0.0.1:8888 -username Alice

# 使用DHT去中心化模式
./bin/pchat -port 9001 -username Alice
```

#### 3. 连接其他用户

```bash
# 查看在线用户
/list

# 呼叫用户
/call <用户名>

# 发送消息
Hello, this is a secure message!

# 发送文件
/sendfile /path/to/file.txt
```

## 📋 命令参考

### 基本命令

| 命令 | 描述 |
|------|------|
| `/help` 或 `/h` | 显示帮助信息 |
| `/quit` 或 `/exit` | 退出程序 |

### 用户管理

| 命令 | 描述 |
|------|------|
| `/list` 或 `/users` | 查看在线用户列表 |
| `/call <用户名>` | 呼叫并连接用户 |
| `/hangup` | 挂断所有连接 |
| `/hangup <用户名>` | 挂断指定用户连接 |

### 文件传输

| 命令 | 描述 |
|------|------|
| `/sendfile <文件路径>` | 发送文件给所有连接的用户 |
| `/file <文件路径>` | 发送文件（简写形式） |

### 娱乐功能

| 命令 | 描述 |
|------|------|
| `/rps` | 发起石头剪刀布游戏 |

## 🔧 配置选项

### 客户端参数

```bash
-port <端口>        # 监听端口（默认随机）
-registry <地址>    # 注册服务器地址（格式：127.0.0.1:8888）
-username <用户名>  # 用户名（可选）
-peer <地址>        # 要连接的peer地址
```

### 注册服务器参数

```bash
-port <端口>        # 监听端口（默认8888）
```

## 🛡️ 安全特性

### 加密机制

1. **消息加密**: 使用 AES-256 对称加密保护消息内容
2. **密钥交换**: 使用 RSA-2048 非对称加密保护 AES 密钥
3. **数字签名**: 每条消息都使用发送方私钥签名，接收方验证
4. **防重放攻击**: 使用 nonce 和时间戳防止消息重放

### 安全验证

- ✅ 消息签名验证
- ✅ 防重放攻击检测
- ✅ 时间戳验证（5分钟有效期）
- ✅ 文件完整性校验（SHA-256）

## 🧪 测试

### 单元测试

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./internal/crypto/...
```

### 集成测试

```bash
# 使用测试脚本
./test_dht.sh
```

## 📚 文档

- [使用说明](docs/USAGE.md) - 详细使用指南
- [DHT测试指南](docs/TEST_DHT.md) - DHT模式测试步骤
- [DHT测试总结](docs/DHT_TEST_SUMMARY.md) - DHT模式测试总结

## 🤝 贡献

欢迎贡献代码！请遵循以下步骤：

1. Fork 项目
2. 创建功能分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情

## 👥 作者

Your Name - [@yourusername](https://github.com/yourusername)

## 🙏 致谢

- [libp2p](https://github.com/libp2p/go-libp2p) - P2P网络库
- 所有贡献者