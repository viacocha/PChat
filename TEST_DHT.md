# DHT模式测试指南

## 测试步骤

### 1. 启动第一个客户端（Alice）

在终端1中运行：
```bash
./pchat -port 9001 -username Alice
```

记录输出的节点ID，例如：
```
📍 节点 ID: 12D3KooW...
📍 监听地址:
   /ip4/127.0.0.1/tcp/9001/p2p/12D3KooW...
```

### 2. 启动第二个客户端（Bob）

在终端2中运行（使用Alice的完整地址）：
```bash
./pchat -port 9002 -username Bob -peer /ip4/127.0.0.1/tcp/9001/p2p/<Alice的节点ID>
```

### 3. 测试命令

#### 测试1: 发送普通消息
在Bob的终端中输入：
```
Hello Alice, this is a test message
```

在Alice的终端中应该能看到消息。

#### 测试2: 查看在线用户列表
在Bob的终端中输入：
```
/list
```
或
```
/users
```

应该显示Alice和Bob的用户信息。

#### 测试3: 呼叫用户
在Bob的终端中输入：
```
call Alice
```
或
```
/call Alice
```

应该能够连接到Alice并交换公钥。

#### 测试4: 通过节点ID呼叫
在Bob的终端中输入：
```
call <Alice的节点ID>
```

应该能够连接到Alice。

#### 测试5: 发送文件
在Bob的终端中输入：
```
/sendfile /path/to/file.txt
```

文件应该被发送到所有已连接的peer。

#### 测试6: DHT用户发现
等待30秒后，在Bob的终端中输入：
```
/list
```

应该能看到通过DHT发现的用户（包括Alice）。

### 4. 测试DHT发现功能

#### 测试7: 启动第三个客户端（Charlie）
在终端3中运行：
```bash
./pchat -port 9003 -username Charlie -peer /ip4/127.0.0.1/tcp/9001/p2p/<Alice的节点ID>
```

等待30秒后，在Charlie的终端中输入：
```
/list
```

应该能看到Alice和Bob（通过DHT发现）。

#### 测试8: 通过DHT呼叫用户
在Charlie的终端中输入：
```
call Alice
```

应该能够通过DHT查找并连接到Alice。

### 5. 测试优雅关闭

在任意客户端终端中输入：
```
/quit
```
或
```
/exit
```

客户端应该优雅地关闭，并清理资源。

## 预期结果

1. ✅ 两个客户端能够成功连接
2. ✅ 能够发送和接收加密消息
3. ✅ `/list` 命令能够显示在线用户
4. ✅ `call` 命令能够连接到其他用户
5. ✅ `/sendfile` 命令能够发送文件
6. ✅ DHT发现功能能够发现其他用户
7. ✅ 优雅关闭功能正常工作

## 常见问题

### DHT存储失败
如果看到 "存储用户信息到DHT失败: invalid record keytype"，这是已知问题，正在修复中。不影响基本功能。

### 用户列表为空
DHT发现需要时间，请等待30秒后再试。

### 无法连接
确保：
1. 节点ID正确
2. 端口没有被占用
3. 防火墙允许连接

