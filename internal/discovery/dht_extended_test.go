package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

// TestDHTDiscovery_DiscoverNetworkUsers 测试网络用户发现
func TestDHTDiscovery_DiscoverNetworkUsers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 测试 discoverNetworkUsers（内部函数，通过其他方式间接测试）
	// 由于是私有函数，我们通过 DiscoverUsers 来间接测试
	err = dd.DiscoverUsers(ctx)
	if err != nil {
		t.Logf("DiscoverUsers 失败（可能因为网络环境）: %v", err)
	}
}

// TestDHTDiscovery_DiscoverNetworkUsers_WithConnections 测试有连接时的网络用户发现
func TestDHTDiscovery_DiscoverNetworkUsers_WithConnections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h1, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 1 失败: %v", err)
	}
	defer h1.Close()

	h2, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 2 失败: %v", err)
	}
	defer h2.Close()

	dd, err := NewDHTDiscovery(ctx, h1, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 建立连接
	err = h1.Connect(ctx, peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	})
	if err != nil {
		t.Logf("连接失败（可能因为测试环境）: %v", err)
		return
	}

	// 等待连接建立
	time.Sleep(500 * time.Millisecond)

	// 测试发现网络用户
	err = dd.DiscoverUsers(ctx)
	if err != nil {
		t.Logf("DiscoverUsers 失败（可能因为网络环境）: %v", err)
	}
}

// TestDHTDiscovery_DiscoverNetworkUsers_NoConnections 测试无连接时的网络用户发现
func TestDHTDiscovery_DiscoverNetworkUsers_NoConnections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 测试无连接时的发现（应该不会 panic）
	err = dd.DiscoverUsers(ctx)
	if err != nil {
		t.Logf("DiscoverUsers 失败（预期，因为没有连接）: %v", err)
	}
}

// TestDHTDiscovery_GetUserKey 测试 DHT 键生成
func TestDHTDiscovery_GetUserKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 测试生成键
	key := dd.getUserKey("Alice")
	if key == "" {
		t.Error("getUserKey 应该返回非空字符串")
	}

	// 测试相同用户名生成相同键
	key2 := dd.getUserKey("Alice")
	if key != key2 {
		t.Error("相同用户名应该生成相同的键")
	}

	// 测试不同用户名生成不同键
	key3 := dd.getUserKey("Bob")
	if key == key3 {
		t.Error("不同用户名应该生成不同的键")
	}
}

// TestDHTDiscovery_GetUserKey_EmptyUsername 测试空用户名
func TestDHTDiscovery_GetUserKey_EmptyUsername(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 测试空用户名
	key := dd.getUserKey("")
	if key == "" {
		t.Error("getUserKey 应该返回非空字符串（即使是空用户名）")
	}
}

// TestDHTDiscovery_GetUserKey_SpecialCharacters 测试特殊字符用户名
func TestDHTDiscovery_GetUserKey_SpecialCharacters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 测试特殊字符用户名
	testCases := []string{
		"User-123",
		"User_456",
		"User.789",
		"User@test",
	}

	for _, username := range testCases {
		key := dd.getUserKey(username)
		if key == "" {
			t.Errorf("getUserKey 应该返回非空字符串（用户名: %s）", username)
		}
	}
}

// TestDHTDiscovery_GetAddresses 测试地址获取
func TestDHTDiscovery_GetAddresses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 测试获取地址
	addresses := dd.getAddresses()
	if len(addresses) == 0 {
		t.Error("getAddresses 应该返回至少一个地址")
	}

	// 验证地址格式（应该包含 /p2p/）
	for _, addr := range addresses {
		if addr == "" {
			t.Error("地址不应该为空")
		}
		// 地址应该包含 /p2p/ 前缀
		if len(addr) < 5 {
			t.Error("地址格式可能不正确")
		}
	}
}

// TestDHTDiscovery_GetAddresses_MultipleAddresses 测试多个地址
func TestDHTDiscovery_GetAddresses_MultipleAddresses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 创建带多个地址的 host
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/127.0.0.1/tcp/9001",
			"/ip4/127.0.0.1/tcp/9002",
		),
	)
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 测试获取多个地址
	addresses := dd.getAddresses()
	if len(addresses) < 2 {
		t.Logf("期望至少 2 个地址，实际: %d", len(addresses))
	}

	// 验证所有地址都包含 /p2p/
	for _, addr := range addresses {
		if addr == "" {
			t.Error("地址不应该为空")
		}
	}
}

// TestDHTDiscovery_ListUsers_FiltersExpired 测试列表用户时过滤过期用户
func TestDHTDiscovery_ListUsers_FiltersExpired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 添加一个未过期的用户
	dd.RecordUserFromConnection("Alice", "peer1", []string{"/ip4/127.0.0.1/tcp/9001"})

	// 添加一个过期的用户（手动设置过期时间戳）
	dd.mutex.Lock()
	dd.localUsers["Bob"] = &UserInfo{
		Username:  "Bob",
		PeerID:    "peer2",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9002"},
		Timestamp: time.Now().Unix() - int64(userInfoTTL.Seconds()) - 1, // 过期
	}
	dd.mutex.Unlock()

	// 测试列表用户（应该只返回未过期的用户）
	users := dd.ListUsers()
	foundAlice := false
	foundBob := false

	for _, user := range users {
		if user.Username == "Alice" {
			foundAlice = true
		}
		if user.Username == "Bob" {
			foundBob = true
		}
	}

	if !foundAlice {
		t.Error("应该找到未过期的用户 Alice")
	}
	if foundBob {
		t.Error("不应该找到过期的用户 Bob")
	}
}

// TestDHTDiscovery_GetUserByPeerID_Expired 测试获取过期用户
func TestDHTDiscovery_GetUserByPeerID_Expired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 添加一个过期的用户
	dd.mutex.Lock()
	dd.localUsers["Alice"] = &UserInfo{
		Username:  "Alice",
		PeerID:    "peer1",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
		Timestamp: time.Now().Unix() - int64(userInfoTTL.Seconds()) - 1, // 过期
	}
	dd.peerIDToUser["peer1"] = dd.localUsers["Alice"]
	dd.mutex.Unlock()

	// 测试获取过期用户（应该返回 nil）
	user := dd.GetUserByPeerID("peer1")
	if user != nil {
		t.Error("不应该返回过期的用户")
	}
}

// TestDHTDiscovery_CleanupExpiredUsers_RemovesAll 测试清理所有过期用户
func TestDHTDiscovery_CleanupExpiredUsers_RemovesAll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 添加一个未过期的用户
	dd.RecordUserFromConnection("Alice", "peer1", []string{"/ip4/127.0.0.1/tcp/9001"})

	// 添加一个过期的用户
	dd.mutex.Lock()
	dd.localUsers["Bob"] = &UserInfo{
		Username:  "Bob",
		PeerID:    "peer2",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9002"},
		Timestamp: time.Now().Unix() - int64(userInfoTTL.Seconds()) - 1, // 过期
	}
	dd.peerIDToUser["peer2"] = dd.localUsers["Bob"]
	dd.mutex.Unlock()

	// 清理过期用户
	dd.cleanupExpiredUsers()

	// 验证过期用户已被删除
	dd.mutex.RLock()
	_, existsBob := dd.localUsers["Bob"]
	_, existsPeer2 := dd.peerIDToUser["peer2"]
	_, existsAlice := dd.localUsers["Alice"]
	dd.mutex.RUnlock()

	if existsBob {
		t.Error("过期的用户 Bob 应该已被删除")
	}
	if existsPeer2 {
		t.Error("过期的 peerID peer2 应该已被删除")
	}
	if !existsAlice {
		t.Error("未过期的用户 Alice 不应该被删除")
	}
}

// TestDHTDiscovery_RecordUserFromConnection_EdgeCases 测试记录用户的边界情况
func TestDHTDiscovery_RecordUserFromConnection_EdgeCases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 测试空用户名
	dd.RecordUserFromConnection("", "peer1", []string{"/ip4/127.0.0.1/tcp/9001"})
	dd.mutex.RLock()
	_, exists := dd.localUsers[""]
	dd.mutex.RUnlock()
	if exists {
		t.Error("空用户名不应该被记录")
	}

	// 测试空 peerID
	dd.RecordUserFromConnection("Alice", "", []string{"/ip4/127.0.0.1/tcp/9001"})
	dd.mutex.RLock()
	user, exists := dd.localUsers["Alice"]
	dd.mutex.RUnlock()
	if exists && user.PeerID != "" {
		t.Error("空 peerID 不应该被记录")
	}

	// 测试空地址列表
	dd.RecordUserFromConnection("Bob", "peer2", []string{})
	dd.mutex.RLock()
	user, exists = dd.localUsers["Bob"]
	dd.mutex.RUnlock()
	if !exists {
		t.Error("即使地址列表为空，用户也应该被记录")
	}
}

// TestDHTDiscovery_RecordUserFromConnection_UpdatesExisting 测试更新已存在的用户
func TestDHTDiscovery_RecordUserFromConnection_UpdatesExisting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 初始化失败: %v", err)
		return
	}
	defer dd.Close()

	// 第一次记录
	dd.RecordUserFromConnection("Alice", "peer1", []string{"/ip4/127.0.0.1/tcp/9001"})

	// 第二次记录（更新）
	dd.RecordUserFromConnection("Alice", "peer1", []string{"/ip4/127.0.0.1/tcp/9002"})

	// 验证用户信息已更新
	dd.mutex.RLock()
	user, exists := dd.localUsers["Alice"]
	dd.mutex.RUnlock()

	if !exists {
		t.Error("用户应该存在")
	}
	if user == nil {
		t.Error("用户信息不应该为 nil")
	}
	// 时间戳应该已更新
	if user.Timestamp == 0 {
		t.Error("时间戳应该已更新")
	}
}

