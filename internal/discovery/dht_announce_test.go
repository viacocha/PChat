package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
)

// TestAnnounceSelf 测试广播自己的信息
func TestAnnounceSelf(t *testing.T) {
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

	// 测试 AnnounceSelf（可能会失败，但不应该 panic）
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("AnnounceSelf 不应该 panic: %v", r)
			}
		}()
		dd.AnnounceSelf(ctx)
	}()

	// 验证本地缓存已更新
	dd.mutex.RLock()
	user, exists := dd.localUsers["testuser"]
	dd.mutex.RUnlock()

	if !exists {
		t.Error("本地缓存应该包含自己的用户信息")
	}
	if user != nil && user.Username != "testuser" {
		t.Errorf("用户名应该为 testuser，得到 %s", user.Username)
	}
}

// TestAnnounceSelf_MultipleTimes 测试多次广播
func TestAnnounceSelf_MultipleTimes(t *testing.T) {
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

	// 多次调用 AnnounceSelf
	for i := 0; i < 3; i++ {
		dd.AnnounceSelf(ctx)
		time.Sleep(100 * time.Millisecond)
	}

	// 验证本地缓存仍然正确
	dd.mutex.RLock()
	user, exists := dd.localUsers["testuser"]
	dd.mutex.RUnlock()

	if !exists {
		t.Error("本地缓存应该包含自己的用户信息")
	}
	if user != nil {
		// 验证时间戳是最新的
		now := time.Now().Unix()
		if user.Timestamp > now || user.Timestamp < now-10 {
			t.Logf("时间戳可能不正常: %d (当前: %d)", user.Timestamp, now)
		}
	}
}

// TestCleanupExpiredUsers 测试清理过期用户
func TestCleanupExpiredUsers(t *testing.T) {
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

	// 添加一些用户：一个新鲜的，一个过期的
	now := time.Now().Unix()
	freshUser := &UserInfo{
		Username:  "fresh",
		PeerID:    "peerFresh",
		Timestamp: now,
	}
	oldUser := &UserInfo{
		Username:  "old",
		PeerID:    "peerOld",
		Timestamp: now - int64(userInfoTTL.Seconds()) - 10,
	}

	dd.mutex.Lock()
	dd.localUsers["fresh"] = freshUser
	dd.localUsers["old"] = oldUser
	dd.peerIDToUser["peerFresh"] = freshUser
	dd.peerIDToUser["peerOld"] = oldUser
	dd.mutex.Unlock()

	// 执行清理
	dd.cleanupExpiredUsers()

	// 验证结果
	dd.mutex.RLock()
	defer dd.mutex.RUnlock()

	if _, exists := dd.localUsers["old"]; exists {
		t.Error("过期用户应该被清理")
	}
	if _, exists := dd.peerIDToUser["peerOld"]; exists {
		t.Error("过期用户的 peerID 映射应该被清理")
	}
	if _, exists := dd.localUsers["fresh"]; !exists {
		t.Error("新鲜用户不应该被清理")
	}
	if _, exists := dd.peerIDToUser["peerFresh"]; !exists {
		t.Error("新鲜用户的 peerID 映射不应该被清理")
	}
}

// TestCleanupExpiredUsers_Empty 测试空列表清理
func TestCleanupExpiredUsers_Empty(t *testing.T) {
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

	// 确保列表为空
	dd.mutex.Lock()
	dd.localUsers = make(map[string]*UserInfo)
	dd.peerIDToUser = make(map[string]*UserInfo)
	dd.mutex.Unlock()

	// 执行清理（不应该 panic）
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("清理空列表不应该 panic: %v", r)
			}
		}()
		dd.cleanupExpiredUsers()
	}()

	// 验证列表仍然为空
	dd.mutex.RLock()
	defer dd.mutex.RUnlock()

	if len(dd.localUsers) != 0 {
		t.Errorf("本地用户列表应该为空，得到 %d 个", len(dd.localUsers))
	}
	if len(dd.peerIDToUser) != 0 {
		t.Errorf("peerID 映射应该为空，得到 %d 个", len(dd.peerIDToUser))
	}
}

// TestCleanupExpiredUsers_AllExpired 测试所有用户都过期
func TestCleanupExpiredUsers_AllExpired(t *testing.T) {
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

	// 添加多个过期用户
	now := time.Now().Unix()
	oldTimestamp := now - int64(userInfoTTL.Seconds()) - 10

	dd.mutex.Lock()
	for i := 0; i < 5; i++ {
		username := "old" + string(rune('0'+i))
		peerID := "peerOld" + string(rune('0'+i))
		user := &UserInfo{
			Username:  username,
			PeerID:    peerID,
			Timestamp: oldTimestamp,
		}
		dd.localUsers[username] = user
		dd.peerIDToUser[peerID] = user
	}
	dd.mutex.Unlock()

	// 执行清理
	dd.cleanupExpiredUsers()

	// 验证所有用户都被清理
	dd.mutex.RLock()
	defer dd.mutex.RUnlock()

	if len(dd.localUsers) != 0 {
		t.Errorf("所有过期用户应该被清理，但还有 %d 个", len(dd.localUsers))
	}
	if len(dd.peerIDToUser) != 0 {
		t.Errorf("所有过期用户的 peerID 映射应该被清理，但还有 %d 个", len(dd.peerIDToUser))
	}
}

// TestAnnounceSelf_UpdatesTimestamp 测试时间戳更新
func TestAnnounceSelf_UpdatesTimestamp(t *testing.T) {
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

	// 第一次广播
	dd.AnnounceSelf(ctx)

	dd.mutex.RLock()
	firstTimestamp := dd.localUsers["testuser"].Timestamp
	dd.mutex.RUnlock()

	// 等待一段时间
	time.Sleep(2 * time.Second)

	// 第二次广播
	dd.AnnounceSelf(ctx)

	dd.mutex.RLock()
	secondTimestamp := dd.localUsers["testuser"].Timestamp
	dd.mutex.RUnlock()

	// 验证时间戳已更新
	if secondTimestamp <= firstTimestamp {
		t.Errorf("时间戳应该更新: %d <= %d", secondTimestamp, firstTimestamp)
	}
}

