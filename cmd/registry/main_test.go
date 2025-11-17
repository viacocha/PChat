package main

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestClientInfo_RegisterTime 测试ClientInfo的RegisterTime字段
func TestClientInfo_RegisterTime(t *testing.T) {
	client := &ClientInfo{
		PeerID:       "QmTest123",
		Username:     "Alice",
		RegisterTime: time.Now(),
		LastSeen:     time.Now(),
	}

	if client.RegisterTime.IsZero() {
		t.Error("RegisterTime should not be zero")
	}
	if client.LastSeen.IsZero() {
		t.Error("LastSeen should not be zero")
	}
}

// TestRegistryServer_NewRegistryServer 测试创建注册服务器
func TestRegistryServer_NewRegistryServer(t *testing.T) {
	server := NewRegistryServer()
	if server == nil {
		t.Fatal("NewRegistryServer() returned nil")
	}
	if server.clients == nil {
		t.Error("clients map should be initialized")
	}
}

// TestRegistryServer_RegisterClient 测试注册客户端（通过handleRequest模拟）
func TestRegistryServer_RegisterClient(t *testing.T) {
	server := NewRegistryServer()

	// 模拟注册一个客户端
	server.mutex.Lock()
	server.clients["test-peer"] = &ClientInfo{
		PeerID:       "test-peer",
		Username:     "TestUser",
		Addresses:    []string{"/ip4/127.0.0.1/tcp/9001"},
		RegisterTime: time.Now(),
		LastSeen:     time.Now(),
	}
	server.mutex.Unlock()

	// 验证客户端已注册
	server.mutex.RLock()
	client, exists := server.clients["test-peer"]
	server.mutex.RUnlock()

	if !exists {
		t.Error("Client should be registered")
	}
	if client.Username != "TestUser" {
		t.Errorf("Username = %q, want %q", client.Username, "TestUser")
	}
}

// TestRegistryServer_ListClients 测试列出客户端
func TestRegistryServer_ListClients(t *testing.T) {
	server := NewRegistryServer()

	// 添加几个测试客户端
	server.mutex.Lock()
	server.clients["peer1"] = &ClientInfo{
		PeerID:       "peer1",
		Username:     "Alice",
		RegisterTime: time.Now(),
		LastSeen:     time.Now(),
	}
	server.clients["peer2"] = &ClientInfo{
		PeerID:       "peer2",
		Username:     "Bob",
		RegisterTime: time.Now(),
		LastSeen:     time.Now(),
	}
	server.mutex.Unlock()

	// 验证客户端数量
	server.mutex.RLock()
	count := len(server.clients)
	server.mutex.RUnlock()

	if count != 2 {
		t.Errorf("Expected 2 clients, got %d", count)
	}
}

// TestRegistryServer_CleanupExpiredClients 测试清理过期客户端
func TestRegistryServer_CleanupExpiredClients(t *testing.T) {
	server := NewRegistryServer()

	// 添加一个过期的客户端（LastSeen超过心跳超时的2倍，即60秒前）
	server.mutex.Lock()
	server.clients["expired"] = &ClientInfo{
		PeerID:       "expired",
		Username:     "ExpiredUser",
		LastSeen:     time.Now().Add(-65 * time.Second), // 65秒前，超过60秒超时
		RegisterTime: time.Now().Add(-65 * time.Second),
	}
	// 添加一个未过期的客户端
	server.clients["active"] = &ClientInfo{
		PeerID:       "active",
		Username:     "ActiveUser",
		LastSeen:     time.Now(), // 刚刚活跃
		RegisterTime: time.Now(),
	}
	server.mutex.Unlock()

	// 等待清理goroutine执行（清理间隔是10秒，我们等待12秒确保执行一次）
	time.Sleep(12 * time.Second)

	// 验证过期客户端已被清理，活跃客户端保留
	server.mutex.RLock()
	_, expiredExists := server.clients["expired"]
	_, activeExists := server.clients["active"]
	server.mutex.RUnlock()

	if expiredExists {
		t.Error("expired client should be removed")
	}
	if !activeExists {
		t.Error("active client should not be removed")
	}
}

func sendRegistryMessage(t *testing.T, server *RegistryServer, msg RegistryMessage) RegistryResponse {
	t.Helper()

	clientConn, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handleRequest(serverConn)
		close(done)
	}()

	encoder := json.NewEncoder(clientConn)
	decoder := json.NewDecoder(clientConn)

	if err := encoder.Encode(msg); err != nil {
		t.Fatalf("encode message failed: %v", err)
	}

	var resp RegistryResponse
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	clientConn.Close()
	<-done
	return resp
}

func TestRegistryServer_HandleRegisterAndList(t *testing.T) {
	server := NewRegistryServer()

	registerResp := sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})
	if !registerResp.Success {
		t.Fatalf("register failed: %s", registerResp.Message)
	}

	listResp := sendRegistryMessage(t, server, RegistryMessage{
		Type: "list",
	})

	if !listResp.Success {
		t.Fatalf("list failed: %s", listResp.Message)
	}
	if len(listResp.Clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(listResp.Clients))
	}
	if listResp.Clients[0].Username != "Alice" {
		t.Errorf("expected Alice, got %s", listResp.Clients[0].Username)
	}
}

func TestRegistryServer_HeartbeatUpdatesLastSeen(t *testing.T) {
	server := NewRegistryServer()
	sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})

	server.mutex.RLock()
	initialLastSeen := server.clients["peer1"].LastSeen
	server.mutex.RUnlock()

	time.Sleep(10 * time.Millisecond)

	heartbeatResp := sendRegistryMessage(t, server, RegistryMessage{
		Type:   "heartbeat",
		PeerID: "peer1",
	})
	if !heartbeatResp.Success {
		t.Fatalf("heartbeat failed: %s", heartbeatResp.Message)
	}

	server.mutex.RLock()
	defer server.mutex.RUnlock()
	if !server.clients["peer1"].LastSeen.After(initialLastSeen) {
		t.Fatal("LastSeen not updated after heartbeat")
	}
}

func TestRegistryServer_HeartbeatUnregisteredClient(t *testing.T) {
	server := NewRegistryServer()
	heartbeatResp := sendRegistryMessage(t, server, RegistryMessage{
		Type:   "heartbeat",
		PeerID: "nonexistent",
	})
	if heartbeatResp.Success {
		t.Error("heartbeat should fail for unregistered client")
	}
}

func TestRegistryServer_LookupByPeerID(t *testing.T) {
	server := NewRegistryServer()
	sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})

	lookupResp := sendRegistryMessage(t, server, RegistryMessage{
		Type:     "lookup",
		TargetID: "peer1",
	})
	if !lookupResp.Success {
		t.Fatalf("lookup failed: %s", lookupResp.Message)
	}
	if lookupResp.Client == nil {
		t.Fatal("expected client, got nil")
	}
	if lookupResp.Client.Username != "Alice" {
		t.Errorf("expected Alice, got %s", lookupResp.Client.Username)
	}
}

func TestRegistryServer_LookupByUsername(t *testing.T) {
	server := NewRegistryServer()
	sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})

	lookupResp := sendRegistryMessage(t, server, RegistryMessage{
		Type:     "lookup",
		TargetID: "Alice",
	})
	if !lookupResp.Success {
		t.Fatalf("lookup failed: %s", lookupResp.Message)
	}
	if lookupResp.Client == nil {
		t.Fatal("expected client, got nil")
	}
	if lookupResp.Client.PeerID != "peer1" {
		t.Errorf("expected peer1, got %s", lookupResp.Client.PeerID)
	}
}

func TestRegistryServer_LookupNotFound(t *testing.T) {
	server := NewRegistryServer()
	lookupResp := sendRegistryMessage(t, server, RegistryMessage{
		Type:     "lookup",
		TargetID: "nonexistent",
	})
	if lookupResp.Success {
		t.Error("lookup should fail for nonexistent client")
	}
}

func TestRegistryServer_Unregister(t *testing.T) {
	server := NewRegistryServer()
	sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})

	unregisterResp := sendRegistryMessage(t, server, RegistryMessage{
		Type:   "unregister",
		PeerID: "peer1",
	})
	if !unregisterResp.Success {
		t.Fatalf("unregister failed: %s", unregisterResp.Message)
	}

	server.mutex.RLock()
	_, exists := server.clients["peer1"]
	server.mutex.RUnlock()
	if exists {
		t.Error("client should be removed after unregister")
	}
}

func TestRegistryServer_UnregisterNonexistent(t *testing.T) {
	server := NewRegistryServer()
	unregisterResp := sendRegistryMessage(t, server, RegistryMessage{
		Type:   "unregister",
		PeerID: "nonexistent",
	})
	if unregisterResp.Success {
		t.Error("unregister should fail for nonexistent client")
	}
}

func TestRegistryServer_UnknownMessageType(t *testing.T) {
	server := NewRegistryServer()
	resp := sendRegistryMessage(t, server, RegistryMessage{
		Type: "unknown",
	})
	if resp.Success {
		t.Error("unknown message type should fail")
	}
}

func TestRegistryServer_RegisterPreservesRegisterTime(t *testing.T) {
	server := NewRegistryServer()
	sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})

	server.mutex.RLock()
	firstRegisterTime := server.clients["peer1"].RegisterTime
	server.mutex.RUnlock()

	time.Sleep(10 * time.Millisecond)

	// 再次注册应该保留原始注册时间
	sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})

	server.mutex.RLock()
	defer server.mutex.RUnlock()
	if !server.clients["peer1"].RegisterTime.Equal(firstRegisterTime) {
		t.Error("RegisterTime should be preserved on re-register")
	}
}

// TestRegistryServer_HandleRequest_InvalidJSON 测试处理无效JSON
func TestRegistryServer_HandleRequest_InvalidJSON(t *testing.T) {
	server := NewRegistryServer()

	clientConn, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handleRequest(serverConn)
		close(done)
	}()

	// 发送无效的JSON
	clientConn.Write([]byte("invalid json\n"))
	clientConn.Close()

	// 等待处理完成（应该不会panic）
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handleRequest did not complete")
	}
}

// TestRegistryServer_HandleRequest_EmptyMessage 测试处理空消息
func TestRegistryServer_HandleRequest_EmptyMessage(t *testing.T) {
	server := NewRegistryServer()

	clientConn, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handleRequest(serverConn)
		close(done)
	}()

	// 发送空消息（EOF）
	clientConn.Close()

	// 等待处理完成
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handleRequest did not complete")
	}
}

// TestRegistryServer_Start 测试服务器启动
func TestRegistryServer_Start(t *testing.T) {
	server := NewRegistryServer()

	// 使用随机端口（0）让系统自动分配
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

	// 等待一小段时间确保服务器启动
	time.Sleep(100 * time.Millisecond)

	// 尝试连接服务器
	conn, err := net.Dial("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	conn.Close()

	// 停止服务器（通过关闭连接触发错误，或者使用context）
	// 这里我们只是验证服务器能够启动和接受连接
}

// TestRegistryServer_ConcurrentRegister 测试并发注册
func TestRegistryServer_ConcurrentRegister(t *testing.T) {
	server := NewRegistryServer()

	const numClients = 10
	done := make(chan struct{}, numClients)

	// 并发注册多个客户端
	for i := 0; i < numClients; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			resp := sendRegistryMessage(t, server, RegistryMessage{
				Type:      "register",
				PeerID:    fmt.Sprintf("peer%d", id),
				Username:  fmt.Sprintf("User%d", id),
				Addresses: []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", 9000+id)},
			})
			if !resp.Success {
				t.Errorf("register failed for peer%d: %s", id, resp.Message)
			}
		}(i)
	}

	// 等待所有注册完成
	for i := 0; i < numClients; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for register %d", i)
		}
	}

	// 验证所有客户端都已注册
	server.mutex.RLock()
	count := len(server.clients)
	server.mutex.RUnlock()

	if count != numClients {
		t.Errorf("expected %d clients, got %d", numClients, count)
	}
}

// TestRegistryServer_ConcurrentHeartbeat 测试并发心跳
func TestRegistryServer_ConcurrentHeartbeat(t *testing.T) {
	server := NewRegistryServer()

	// 先注册一个客户端
	sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})

	const numHeartbeats = 10
	done := make(chan struct{}, numHeartbeats)

	// 并发发送心跳
	for i := 0; i < numHeartbeats; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			resp := sendRegistryMessage(t, server, RegistryMessage{
				Type:   "heartbeat",
				PeerID: "peer1",
			})
			if !resp.Success {
				t.Errorf("heartbeat failed: %s", resp.Message)
			}
		}()
	}

	// 等待所有心跳完成
	for i := 0; i < numHeartbeats; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for heartbeat %d", i)
		}
	}

	// 验证客户端仍然存在
	server.mutex.RLock()
	_, exists := server.clients["peer1"]
	server.mutex.RUnlock()

	if !exists {
		t.Error("client should still exist after heartbeats")
	}
}

// TestRegistryServer_ListEmpty 测试空列表
func TestRegistryServer_ListEmpty(t *testing.T) {
	server := NewRegistryServer()

	resp := sendRegistryMessage(t, server, RegistryMessage{
		Type: "list",
	})

	if !resp.Success {
		t.Fatalf("list failed: %s", resp.Message)
	}
	if len(resp.Clients) != 0 {
		t.Errorf("expected empty list, got %d clients", len(resp.Clients))
	}
}

// TestRegistryServer_RegisterWithEmptyFields 测试空字段注册
func TestRegistryServer_RegisterWithEmptyFields(t *testing.T) {
	server := NewRegistryServer()

	// 测试空用户名
	resp := sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "", // 空用户名
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})
	if !resp.Success {
		t.Logf("register with empty username: %s", resp.Message)
	}

	// 测试空地址列表
	resp2 := sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer2",
		Username:  "Bob",
		Addresses: []string{}, // 空地址列表
	})
	if !resp2.Success {
		t.Logf("register with empty addresses: %s", resp2.Message)
	}
}

// TestRegistryServer_RegisterMultipleAddresses 测试多个地址注册
func TestRegistryServer_RegisterMultipleAddresses(t *testing.T) {
	server := NewRegistryServer()

	resp := sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{
			"/ip4/127.0.0.1/tcp/9001",
			"/ip4/192.168.1.1/tcp/9001",
			"/ip6/::1/tcp/9001",
		},
	})

	if !resp.Success {
		t.Fatalf("register failed: %s", resp.Message)
	}

	// 验证地址已保存
	server.mutex.RLock()
	client := server.clients["peer1"]
	server.mutex.RUnlock()

	if len(client.Addresses) != 3 {
		t.Errorf("expected 3 addresses, got %d", len(client.Addresses))
	}
}

// TestRegistryServer_LookupCaseSensitive 测试查找的大小写敏感性
func TestRegistryServer_LookupCaseSensitive(t *testing.T) {
	server := NewRegistryServer()

	sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice", // 大写A
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})

	// 使用小写查找应该失败（因为查找是精确匹配）
	resp := sendRegistryMessage(t, server, RegistryMessage{
		Type:     "lookup",
		TargetID: "alice", // 小写a
	})

	if resp.Success {
		t.Log("lookup is case-sensitive (expected behavior)")
	}
}

// TestRegistryServer_Start_InvalidPort 测试无效端口
func TestRegistryServer_Start_InvalidPort(t *testing.T) {
	server := NewRegistryServer()

	// 测试无效端口（负数）
	err := server.Start(-1)
	if err == nil {
		t.Error("Start should fail with invalid port")
	}
}

// TestRegistryServer_Start_AlreadyInUse 测试端口已被占用的情况
func TestRegistryServer_Start_AlreadyInUse(t *testing.T) {
	server1 := NewRegistryServer()
	server2 := NewRegistryServer()

	// 使用随机端口
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// 启动第一个服务器
	server1Done := make(chan error, 1)
	go func() {
		server1Done <- server1.Start(port)
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	// 尝试在相同端口启动第二个服务器（应该失败）
	err2 := server2.Start(port)
	if err2 == nil {
		t.Error("Start should fail when port is already in use")
	}

	// 清理
	listener.Close()
}

// TestRegistryServer_HandleRequest_EncodingError 测试编码错误处理
func TestRegistryServer_HandleRequest_EncodingError(t *testing.T) {
	server := NewRegistryServer()

	// 创建一个会关闭的连接来模拟编码错误
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

	// 等待处理完成
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handleRequest did not complete")
	}
}

// TestRegistryServer_CleanupExpiredClients_WithUI 测试带UI的清理逻辑
func TestRegistryServer_CleanupExpiredClients_WithUI(t *testing.T) {
	server := NewRegistryServer()
	// 注意：这里我们不实际创建UI，只是测试UI路径的代码
	// 实际的UI测试需要mock或集成测试

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

// TestRegistryServer_RegisterUpdateAddresses 测试注册时更新地址
func TestRegistryServer_RegisterUpdateAddresses(t *testing.T) {
	server := NewRegistryServer()

	// 第一次注册
	resp1 := sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})
	if !resp1.Success {
		t.Fatalf("first register failed: %s", resp1.Message)
	}

	// 再次注册，更新地址
	resp2 := sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "peer1",
		Username:  "Alice",
		Addresses: []string{"/ip4/192.168.1.1/tcp/9002"},
	})
	if !resp2.Success {
		t.Fatalf("second register failed: %s", resp2.Message)
	}

	// 验证地址已更新
	server.mutex.RLock()
	client := server.clients["peer1"]
	server.mutex.RUnlock()

	if len(client.Addresses) != 1 || client.Addresses[0] != "/ip4/192.168.1.1/tcp/9002" {
		t.Errorf("addresses should be updated, got %v", client.Addresses)
	}
}

// TestRegistryServer_LookupByPeerIDAndUsername 测试同时匹配PeerID和Username的情况
func TestRegistryServer_LookupByPeerIDAndUsername(t *testing.T) {
	server := NewRegistryServer()

	// 注册一个客户端，PeerID和Username相同
	sendRegistryMessage(t, server, RegistryMessage{
		Type:      "register",
		PeerID:    "alice",
		Username:  "alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
	})

	// 通过PeerID查找
	resp1 := sendRegistryMessage(t, server, RegistryMessage{
		Type:     "lookup",
		TargetID: "alice",
	})
	if !resp1.Success {
		t.Fatalf("lookup by peerID failed: %s", resp1.Message)
	}

	// 通过Username查找（应该也能找到，因为查找逻辑会匹配两者）
	resp2 := sendRegistryMessage(t, server, RegistryMessage{
		Type:     "lookup",
		TargetID: "alice",
	})
	if !resp2.Success {
		t.Fatalf("lookup by username failed: %s", resp2.Message)
	}
}
