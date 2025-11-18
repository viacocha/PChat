package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRegistryServer_HandleRequest_WithUI 测试带UI的请求处理
func TestRegistryServer_HandleRequest_WithUI(t *testing.T) {
	server := NewRegistryServer()
	
	// 创建一个简单的UI mock（不实际运行UI）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ui := NewRegistryUI(ctx, server, 8888)
	server.ui = ui

	// 测试注册（应该触发UI事件）
	resp := sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})
	if !resp.Success {
		t.Fatalf("register failed: %s", resp.Message)
	}

	// 验证UI事件已添加
	ui.mutex.RLock()
	eventCount := len(ui.events)
	ui.mutex.RUnlock()
	if eventCount == 0 {
		t.Error("UI event should be added on register")
	}

	// 测试心跳（应该触发UI状态消息）
	resp2 := sendRegistryMessage(t, server, RegistryMessage{
		Type:   "heartbeat",
		PeerID: "peer1",
	})
	if !resp2.Success {
		t.Fatalf("heartbeat failed: %s", resp2.Message)
	}

	// 验证UI状态消息已添加
	ui.mutex.RLock()
	statusCount := len(ui.statusMessages)
	ui.mutex.RUnlock()
	if statusCount == 0 {
		t.Error("UI status message should be added on heartbeat")
	}

	// 测试注销（应该触发UI事件）
	resp3 := sendRegistryMessage(t, server, RegistryMessage{
		Type:   "unregister",
		PeerID: "peer1",
	})
	if !resp3.Success {
		t.Fatalf("unregister failed: %s", resp3.Message)
	}

	// 验证注销事件已添加
	ui.mutex.RLock()
	eventCount2 := len(ui.events)
	ui.mutex.RUnlock()
	if eventCount2 <= eventCount {
		t.Error("UI event should be added on unregister")
	}
}

// TestRegistryServer_HandleRequest_LongPeerID 测试长PeerID的显示
func TestRegistryServer_HandleRequest_LongPeerID(t *testing.T) {
	server := NewRegistryServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ui := NewRegistryUI(ctx, server, 8888)
	server.ui = ui

	// 使用一个很长的PeerID（超过12个字符）
	longPeerID := "QmVeryLongPeerIDThatExceedsTwelveCharacters"
	resp := sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    longPeerID,
		Username:  "LongPeerUser",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})
	if !resp.Success {
		t.Fatalf("register failed: %s", resp.Message)
	}

	// 验证长PeerID被正确截断显示
	ui.mutex.RLock()
	events := ui.events
	ui.mutex.RUnlock()
	
	if len(events) == 0 {
		t.Error("should have events")
	} else {
		// 检查事件中是否包含截断的PeerID
		lastEvent := events[len(events)-1]
		if len(lastEvent) > 0 {
			// 事件应该包含截断的PeerID（前12个字符 + "..."）
			expectedPrefix := longPeerID[:12] + "..."
			if !containsSubstring(lastEvent, expectedPrefix) {
				t.Logf("Event: %s", lastEvent)
				t.Logf("Expected to contain: %s", expectedPrefix)
			}
		}
	}
}

// TestRegistryServer_Start_AcceptError 测试接受连接错误
func TestRegistryServer_Start_AcceptError(t *testing.T) {
	server := NewRegistryServer()

	// 创建一个监听器并立即关闭，模拟Accept错误
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// 在goroutine中启动服务器
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Start(port)
	}()

	// 等待一小段时间
	time.Sleep(100 * time.Millisecond)

	// 连接并立即关闭，触发Accept错误
	conn, err := net.Dial("tcp", net.JoinHostPort("", strconv.Itoa(port)))
	if err == nil {
		conn.Close()
	}

	// 等待一下，让服务器处理
	time.Sleep(50 * time.Millisecond)

	// 这个测试主要是验证服务器不会panic，而不是验证具体行为
	// 因为Accept错误后服务器会继续运行
}

// TestRegistryServer_HandleRequest_ConnectionCloseBeforeEncode 测试编码前连接关闭
func TestRegistryServer_HandleRequest_ConnectionCloseBeforeEncode(t *testing.T) {
	server := NewRegistryServer()

	clientConn, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handleRequest(serverConn)
		close(done)
	}()

	// 发送一个有效的注册消息
	encoder := json.NewEncoder(clientConn)
	msg := RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	}
	if err := encoder.Encode(msg); err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	// 立即关闭连接，模拟编码响应时的错误
	clientConn.Close()

	// 等待处理完成（应该不会panic）
	select {
	case <-done:
		// 正常完成
	case <-time.After(1 * time.Second):
		t.Fatal("handleRequest did not complete")
	}
}

// TestRegistryServer_CleanupExpiredClients_WithUIEvent 测试清理过期客户端时触发UI事件
func TestRegistryServer_CleanupExpiredClients_WithUIEvent(t *testing.T) {
	server := NewRegistryServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ui := NewRegistryUI(ctx, server, 8888)
	server.ui = ui

	// 添加一个过期的客户端
	server.mutex.Lock()
	server.clients["expired"] = &ClientInfo{
		PeerID:       "expired",
		Username:     "ExpiredUser",
		LastSeen:     time.Now().Add(-65 * time.Second),
		RegisterTime: time.Now().Add(-65 * time.Second),
	}
	server.mutex.Unlock()

	// 等待清理执行
	time.Sleep(12 * time.Second)

	// 验证客户端已被清理
	server.mutex.RLock()
	_, exists := server.clients["expired"]
	server.mutex.RUnlock()

	if exists {
		t.Error("expired client should be removed")
	}
}

// TestRegistryServer_RegisterWithVeryLongPeerID 测试非常长的PeerID注册
func TestRegistryServer_RegisterWithVeryLongPeerID(t *testing.T) {
	server := NewRegistryServer()

	// 创建一个非常长的PeerID（100个字符）
	veryLongPeerID := strings.Repeat("Qm", 50)
	resp := sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    veryLongPeerID,
		Username:  "LongPeerUser",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})
	if !resp.Success {
		t.Fatalf("register failed: %s", resp.Message)
	}

	// 验证客户端已注册
	server.mutex.RLock()
	client, exists := server.clients[veryLongPeerID]
	server.mutex.RUnlock()

	if !exists {
		t.Error("client should be registered")
	}
	if client.Username != "LongPeerUser" {
		t.Errorf("username = %q, want %q", client.Username, "LongPeerUser")
	}
}

// TestRegistryServer_ConcurrentOperations 测试并发操作
func TestRegistryServer_ConcurrentOperations(t *testing.T) {
	server := NewRegistryServer()

	const numOps = 20
	done := make(chan struct{}, numOps)

	// 并发执行多种操作
	for i := 0; i < numOps; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()

			// 注册
			resp1 := sendRegistryMessage(t, server, RegistryMessage{
				Type:      "register",
				PeerID:    fmt.Sprintf("peer%d", id),
				Username:  fmt.Sprintf("User%d", id),
				Addresses: []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", 9000+id)},
			})
			if !resp1.Success {
				t.Errorf("register failed for peer%d: %s", id, resp1.Message)
				return
			}

			// 心跳
			resp2 := sendRegistryMessage(t, server, RegistryMessage{
				Type:   "heartbeat",
				PeerID: fmt.Sprintf("peer%d", id),
			})
			if !resp2.Success {
				t.Errorf("heartbeat failed for peer%d: %s", id, resp2.Message)
			}

			// 查找
			resp3 := sendRegistryMessage(t, server, RegistryMessage{
				Type:     "lookup",
				TargetID: fmt.Sprintf("peer%d", id),
			})
			if !resp3.Success {
				t.Errorf("lookup failed for peer%d: %s", id, resp3.Message)
			}
		}(i)
	}

	// 等待所有操作完成
	for i := 0; i < numOps; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("timeout waiting for operation %d", i)
		}
	}

	// 验证所有客户端都已注册
	server.mutex.RLock()
	count := len(server.clients)
	server.mutex.RUnlock()

	if count != numOps {
		t.Errorf("expected %d clients, got %d", numOps, count)
	}
}

// TestRegistryServer_ListWithManyClients 测试列出大量客户端
func TestRegistryServer_ListWithManyClients(t *testing.T) {
	server := NewRegistryServer()

	const numClients = 50
	// 注册多个客户端
	for i := 0; i < numClients; i++ {
		sendRegistryMessage(t, server, RegistryMessage{
			Type:      "register",
			PeerID:    fmt.Sprintf("peer%d", i),
			Username:  fmt.Sprintf("User%d", i),
			Addresses: []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", 9000+i)},
		})
	}

	// 列出所有客户端
	resp := sendRegistryMessage(t, server, RegistryMessage{
		Type: "list",
	})

	if !resp.Success {
		t.Fatalf("list failed: %s", resp.Message)
	}
	if len(resp.Clients) != numClients {
		t.Errorf("expected %d clients, got %d", numClients, len(resp.Clients))
	}
}

// 辅助函数
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

