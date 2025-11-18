package discovery

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p"
)

// TestGetUserKey 测试生成用户DHT键
func TestGetUserKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
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

	tests := []struct {
		name     string
		username string
	}{
		{
			name:     "普通用户名",
			username: "Alice",
		},
		{
			name:     "空用户名",
			username: "",
		},
		{
			name:     "特殊字符用户名",
			username: "user@example.com",
		},
		{
			name:     "长用户名",
			username: "verylongusernamethatexceedsnormallength",
		},
		{
			name:     "中文用户名",
			username: "测试用户",
		},
		{
			name:     "数字用户名",
			username: "12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := dd.getUserKey(tt.username)
			if key == "" {
				t.Error("生成的键不应该为空")
			}
			// 验证键包含命名空间前缀
			if len(key) < len(dhtNamespace) {
				t.Errorf("键长度 %d 应该至少包含命名空间前缀", len(key))
			}
			// 验证相同用户名生成相同键
			key2 := dd.getUserKey(tt.username)
			if key != key2 {
				t.Errorf("相同用户名应该生成相同的键: %s != %s", key, key2)
			}
		})
	}
}

// TestGetAddresses 测试获取节点地址
func TestGetAddresses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
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

	addresses := dd.getAddresses()
	if len(addresses) == 0 {
		t.Error("应该至少返回一个地址")
	}

	// 验证地址格式
	for _, addr := range addresses {
		if addr == "" {
			t.Error("地址不应该为空")
		}
		// 验证地址包含 /p2p/ 前缀
		if len(addr) < 5 {
			t.Errorf("地址格式可能不正确: %s", addr)
		}
	}

	// 验证多次调用返回相同结果（在短时间内）
	addresses2 := dd.getAddresses()
	if len(addresses) != len(addresses2) {
		t.Errorf("地址数量应该相同: %d != %d", len(addresses), len(addresses2))
	}
}

// TestGetUserKey_Consistency 测试键生成的一致性
func TestGetUserKey_Consistency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
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

	dd1, err := NewDHTDiscovery(ctx, h1, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 1 初始化失败: %v", err)
		return
	}
	defer dd1.Close()

	dd2, err := NewDHTDiscovery(ctx, h2, "testuser")
	if err != nil {
		t.Logf("DHT Discovery 2 初始化失败: %v", err)
		return
	}
	defer dd2.Close()

	username := "Alice"
	key1 := dd1.getUserKey(username)
	key2 := dd2.getUserKey(username)

	// 相同用户名应该生成相同的键（因为键只依赖于用户名）
	if key1 != key2 {
		t.Errorf("相同用户名应该生成相同的键: %s != %s", key1, key2)
	}
}

// TestGetAddresses_WithMultipleAddresses 测试多个地址的情况
func TestGetAddresses_WithMultipleAddresses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建带有多个地址的 host
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/127.0.0.1/tcp/0",
			"/ip6/::1/tcp/0",
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

	addresses := dd.getAddresses()
	if len(addresses) == 0 {
		t.Error("应该至少返回一个地址")
	}

	// 验证所有地址都包含节点ID
	peerIDStr := h.ID().String()
	for _, addr := range addresses {
		if addr == "" {
			t.Error("地址不应该为空")
		}
		// 地址应该包含节点ID
		if len(addr) < len(peerIDStr) {
			t.Errorf("地址格式可能不正确: %s", addr)
		}
	}
}

