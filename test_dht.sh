#!/bin/bash

# DHT模式测试脚本
# 测试两个客户端之间的通信

echo "🧪 开始DHT模式测试..."
echo ""

# 清理之前的进程
pkill -f "pchat" 2>/dev/null || true
sleep 1

# 创建测试文件
echo "这是测试文件内容" > /tmp/test_file.txt

# 启动第一个客户端（Alice）
echo "📱 启动第一个客户端 (Alice, 端口 9001)..."
./pchat -port 9001 -username Alice > /tmp/alice.log 2>&1 &
ALICE_PID=$!
sleep 3

# 获取Alice的节点ID
ALICE_NODE_ID=$(grep "节点 ID:" /tmp/alice.log | head -1 | awk '{print $3}')
if [ -z "$ALICE_NODE_ID" ]; then
    echo "❌ 无法获取Alice的节点ID"
    kill $ALICE_PID 2>/dev/null || true
    exit 1
fi

echo "✅ Alice已启动，节点ID: $ALICE_NODE_ID"
echo ""

# 启动第二个客户端（Bob），连接到Alice
echo "📱 启动第二个客户端 (Bob, 端口 9002)..."
ALICE_ADDR="/ip4/127.0.0.1/tcp/9001/p2p/$ALICE_NODE_ID"
./pchat -port 9002 -username Bob -peer "$ALICE_ADDR" > /tmp/bob.log 2>&1 &
BOB_PID=$!
sleep 3

echo "✅ Bob已启动并连接到Alice"
echo ""

# 等待DHT网络稳定
echo "⏳ 等待DHT网络稳定..."
sleep 5

# 测试1: 发送消息
echo "📝 测试1: 发送消息..."
echo "Hello from Bob" | timeout 2 ./pchat -port 9003 -username TestUser -peer "$ALICE_ADDR" 2>/dev/null || true
sleep 2

# 测试2: 查看在线用户列表
echo "📋 测试2: 查看在线用户列表..."
echo "/list" | timeout 2 ./pchat -port 9004 -username TestUser2 -peer "$ALICE_ADDR" 2>/dev/null || true
sleep 2

# 测试3: 呼叫用户
echo "📞 测试3: 呼叫用户..."
echo "call Alice" | timeout 2 ./pchat -port 9005 -username TestUser3 -peer "$ALICE_ADDR" 2>/dev/null || true
sleep 2

# 显示日志
echo ""
echo "📄 Alice的日志:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
tail -20 /tmp/alice.log
echo ""
echo "📄 Bob的日志:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
tail -20 /tmp/bob.log

# 清理
echo ""
echo "🧹 清理测试环境..."
kill $ALICE_PID 2>/dev/null || true
kill $BOB_PID 2>/dev/null || true
pkill -f "pchat" 2>/dev/null || true

echo ""
echo "✅ 测试完成！"
echo ""
echo "💡 提示：要手动测试，请运行："
echo "   终端1: ./pchat -port 9001 -username Alice"
echo "   终端2: ./pchat -port 9002 -username Bob -peer /ip4/127.0.0.1/tcp/9001/p2p/<Alice的节点ID>"

