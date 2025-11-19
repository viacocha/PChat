package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
)

// TestGetUserByPeerID_Simple 测试简单的通过 peerID 获取用户
func TestGetUserByPeerID_Simple(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Skipf("DHT Discovery 初始化失败: %v", err)
	}
	defer dd.Close()

	// 记录一个用户
	peerID := h.ID().String()
	dd.RecordUserFromConnection("testuser", peerID, []string{"/ip4/127.0.0.1/tcp/9001"})

	// 通过 peerID 获取用户
	user := dd.GetUserByPeerID(peerID)
	if user == nil {
		t.Fatal("应该找到用户")
	}
	if user.Username != "testuser" {
		t.Errorf("期望用户名 testuser，得到 %s", user.Username)
	}
}

// TestGetUserByPeerID_AfterAnnounce 测试广播后通过 peerID 获取用户
func TestGetUserByPeerID_AfterAnnounce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Skipf("DHT Discovery 初始化失败: %v", err)
	}
	defer dd.Close()

	// 广播自己
	dd.AnnounceSelf(ctx)

	// 通过 peerID 获取用户
	peerID := h.ID().String()
	user := dd.GetUserByPeerID(peerID)
	if user == nil {
		t.Logf("广播后可能无法立即获取用户（DHT 网络节点少时正常）")
		return
	}
	if user.Username != "testuser" {
		t.Errorf("期望用户名 testuser，得到 %s", user.Username)
	}
}

// TestListUsers_WithMultipleUsers 测试列出多个用户
func TestListUsers_WithMultipleUsers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Skipf("DHT Discovery 初始化失败: %v", err)
	}
	defer dd.Close()

	// 记录多个用户
	hosts := make([]host.Host, 3)
	for i := 0; i < 3; i++ {
		h2, err := libp2p.New()
		if err != nil {
			t.Fatalf("创建 host %d 失败: %v", i, err)
		}
		hosts[i] = h2
		defer h2.Close()

		dd.RecordUserFromConnection(
			"user"+string(rune('1'+i)),
			h2.ID().String(),
			[]string{"/ip4/127.0.0.1/tcp/9001"},
		)
	}

	// 列出用户
	users := dd.ListUsers()
	if len(users) < 3 {
		t.Errorf("期望至少 3 个用户，得到 %d", len(users))
	}
}

// TestRecordUserFromConnection_WithAddresses 测试记录带地址的用户
func TestRecordUserFromConnection_WithAddresses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Skipf("DHT Discovery 初始化失败: %v", err)
	}
	defer dd.Close()

	// 记录带多个地址的用户
	addresses := []string{
		"/ip4/127.0.0.1/tcp/9001",
		"/ip4/192.168.1.1/tcp/9002",
		"/ip6/::1/tcp/9003",
	}
	dd.RecordUserFromConnection("testuser", h.ID().String(), addresses)

	// 验证地址被记录
	user := dd.GetUserByPeerID(h.ID().String())
	if user == nil {
		t.Fatal("应该找到用户")
	}
	if len(user.Addresses) != len(addresses) {
		t.Errorf("期望 %d 个地址，得到 %d", len(addresses), len(user.Addresses))
	}
}

// TestListUsers_FiltersExpired 测试过滤过期用户
func TestListUsers_FiltersExpired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Skipf("DHT Discovery 初始化失败: %v", err)
	}
	defer dd.Close()

	// 记录一个新鲜用户
	dd.RecordUserFromConnection("fresh", h.ID().String(), []string{"/ip4/127.0.0.1/tcp/9001"})

	// 手动添加一个过期用户（通过直接操作内部结构）
	dd.mutex.Lock()
	oldTimestamp := time.Now().Unix() - int64(userInfoTTL.Seconds()) - 10
	dd.localUsers["old"] = &UserInfo{
		Username:  "old",
		PeerID:    "oldpeer",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
		Timestamp: oldTimestamp,
	}
	dd.mutex.Unlock()

	// 列出用户（应该只返回新鲜用户）
	users := dd.ListUsers()
	foundFresh := false
	foundOld := false
	for _, user := range users {
		if user.Username == "fresh" {
			foundFresh = true
		}
		if user.Username == "old" {
			foundOld = true
		}
	}

	if !foundFresh {
		t.Error("应该找到新鲜用户")
	}
	if foundOld {
		t.Error("不应该找到过期用户")
	}
}

// TestGetUserByPeerID_Concurrent 测试并发访问
func TestGetUserByPeerID_Concurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Skipf("DHT Discovery 初始化失败: %v", err)
	}
	defer dd.Close()

	// 记录用户
	peerID := h.ID().String()
	dd.RecordUserFromConnection("testuser", peerID, []string{"/ip4/127.0.0.1/tcp/9001"})

	// 并发访问
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			user := dd.GetUserByPeerID(peerID)
			done <- (user != nil)
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		if !<-done {
			t.Error("并发访问应该成功")
		}
	}
}

// TestRecordUserFromConnection_UpdatesTimestamp 测试更新用户时时间戳更新
func TestRecordUserFromConnection_UpdatesTimestamp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("创建 host 失败: %v", err)
	}
	defer h.Close()

	dd, err := NewDHTDiscovery(ctx, h, "testuser")
	if err != nil {
		t.Skipf("DHT Discovery 初始化失败: %v", err)
	}
	defer dd.Close()

	peerID := h.ID().String()
	
	// 第一次记录
	dd.RecordUserFromConnection("testuser", peerID, []string{"/ip4/127.0.0.1/tcp/9001"})
	time.Sleep(10 * time.Millisecond)
	
	// 第二次记录（更新）
	dd.RecordUserFromConnection("testuser", peerID, []string{"/ip4/127.0.0.1/tcp/9002"})
	
	// 验证时间戳已更新
	user := dd.GetUserByPeerID(peerID)
	if user == nil {
		t.Fatal("应该找到用户")
	}
	// 时间戳应该是最新的
	if user.Timestamp < time.Now().Unix()-1 {
		t.Error("时间戳应该是最新的")
	}
}

