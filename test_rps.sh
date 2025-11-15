#!/bin/bash

echo "🚀 启动PChat RPS功能测试"

# 编译客户端
echo "🔨 编译客户端..."
go build -o bin/pchat ./cmd/pchat

echo "✅ 编译完成"

echo "📋 测试说明:"
echo "   1. 打开三个终端窗口"
echo "   2. 在第一个终端运行: ./bin/pchat -username Alice -port 9001"
echo "   3. 在第二个终端运行: ./bin/pchat -username Bob -port 9002"
echo "   4. 在第三个终端运行: ./bin/pchat -username Charlie -port 9003"
echo "   5. 在Bob和Charlie终端中使用/call命令连接Alice:"
echo "      /call 12D3KooWRe2XdoDJ8yQd1ZA4extUixfShD6nXNGK5q3dS2ztSqnB"
echo "   6. 在Alice终端中发起RPS游戏: /rps"
echo "   7. 观察所有终端的游戏结果"
echo ""

echo "📍 Alice的节点信息 (用于连接):"
echo "   节点ID: 12D3KooWRe2XdoDJ8yQd1ZA4extUixfShD6nXNGK5q3dS2ztSqnB"
echo "   地址: /ip4/127.0.0.1/tcp/9001/p2p/12D3KooWRe2XdoDJ8yQd1ZA4extUixfShD6nXNGK5q3dS2ztSqnB"
echo ""

echo "🎮 准备就绪，请按照上述说明进行测试"
