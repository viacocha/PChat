# DHT模式测试总结

## 快速测试步骤

### 1. 启动第一个客户端（Alice）

**终端1：**
```bash
cd /Users/wangli/GoProjects/PChat
./pchat -port 9001 -username Alice
```

**记录输出的节点ID**，例如：
```
📍 节点 ID: 12D3KooWFHhWqh7Nz7m8h55A1zrDgBqYSbCh3A3p3V8YQZqVJpV7
📍 监听地址:
   /ip4/127.0.0.1/tcp/9001/p2p/12D3KooWFHhWqh7Nz7m8h55A1zrDgBqYSbCh3A3p3V8YQZqVJpV7
```

### 2. 启动第二个客户端（Bob）

**终端2：**
```bash
cd /Users/wangli/GoProjects/PChat
./pchat -port 9002 -username Bob -peer /ip4/127.0.0.1/tcp/9001/p2p/12D3KooWFHhWqh7Nz7m8h55A1zrDgBqYSbCh3A3p3V8YQZqVJpV7
```

（将 `12D3KooWFHhWqh7Nz7m8h55A1zrDgBqYSbCh3A3p3V8YQZqVJpV7` 替换为Alice的实际节点ID）

### 3. 测试所有命令

#### ✅ 测试1: 发送消息
在Bob的终端输入：
```
Hello Alice, this is a test message
```
**预期结果：** Alice应该收到加密消息

#### ✅ 测试2: 查看在线用户列表
在Bob的终端输入：
```
/list
```
或
```
/users
```
**预期结果：** 显示Alice和Bob的用户信息

#### ✅ 测试3: 呼叫用户（通过用户名）
在Bob的终端输入：
```
call Alice
```
或
```
/call Alice
```
**预期结果：** 连接到Alice并交换公钥

#### ✅ 测试4: 呼叫用户（通过节点ID）
在Bob的终端输入：
```
call 12D3KooWFHhWqh7Nz7m8h55A1zrDgBqYSbCh3A3p3V8YQZqVJpV7
```
（使用Alice的实际节点ID）
**预期结果：** 连接到Alice

#### ✅ 测试5: 发送文件
在Bob的终端输入：
```
/sendfile /tmp/test.txt
```
或
```
/file /tmp/test.txt
```
**预期结果：** 文件被发送到所有已连接的peer

#### ✅ 测试6: DHT用户发现
等待30秒后，在Bob的终端输入：
```
/list
```
**预期结果：** 显示通过DHT发现的用户（包括Alice）

#### ✅ 测试7: 优雅关闭
在任意客户端输入：
```
/quit
```
或
```
/exit
```
**预期结果：** 客户端优雅关闭，清理资源

### 4. 测试DHT发现功能（可选）

#### 启动第三个客户端（Charlie）

**终端3：**
```bash
cd /Users/wangli/GoProjects/PChat
./pchat -port 9003 -username Charlie -peer /ip4/127.0.0.1/tcp/9001/p2p/<Alice的节点ID>
```

等待30秒后，在Charlie的终端输入：
```
/list
```
**预期结果：** 显示Alice和Bob（通过DHT发现）

在Charlie的终端输入：
```
call Alice
```
**预期结果：** 通过DHT查找并连接到Alice

## 测试检查清单

- [ ] 两个客户端能够成功连接
- [ ] 能够发送和接收加密消息
- [ ] `/list` 命令能够显示在线用户
- [ ] `call` 命令能够连接到其他用户（通过用户名）
- [ ] `call` 命令能够连接到其他用户（通过节点ID）
- [ ] `/sendfile` 命令能够发送文件
- [ ] DHT发现功能能够发现其他用户（需要等待30秒）
- [ ] 优雅关闭功能正常工作

## 已知问题

1. **DHT存储错误**：如果看到 "存储用户信息到DHT失败: invalid record keytype"，这是已知问题，不影响基本功能。DHT发现功能仍然可以工作，只是用户信息可能无法存储到DHT中。

2. **用户列表为空**：DHT发现需要时间，请等待30秒后再试。

3. **无法连接**：确保节点ID正确，端口没有被占用。

## 测试结果

请按照上述步骤测试，并记录：
- ✅ 成功的功能
- ❌ 失败的功能
- ⚠️ 需要注意的问题

