package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/multiformats/go-base32"
)

const (
	dhtNamespace = "/pchat/users/"
	userInfoTTL  = 5 * time.Minute // 用户信息TTL
)

// UserInfoValidator 用户信息验证器
type UserInfoValidator struct{}

// Validate 验证记录的有效性
func (v *UserInfoValidator) Validate(key string, value []byte) error {
	// 验证键格式 - 支持多种可能的格式
	// 键可能是 "/pchat/users/..." 或 "users/..." 或 "/users/..." 或包含 "users" 的任何格式
	validKey := false
	keyLower := strings.ToLower(key)
	
	// 检查键是否包含 "users" 关键字（更宽松的验证）
	if strings.Contains(keyLower, "users") {
		validKey = true
	}
	
	if !validKey {
		// 记录实际接收到的键格式以便调试
		log.Printf("⚠️  验证器接收到意外的键格式: %s\n", key)
		return fmt.Errorf("无效的键格式: %s", key)
	}

	// 验证值是否为有效的JSON
	var userInfo UserInfo
	if err := json.Unmarshal(value, &userInfo); err != nil {
		return fmt.Errorf("无效的用户信息格式: %v", err)
	}

	// 基本验证
	if userInfo.Username == "" {
		return fmt.Errorf("用户名不能为空")
	}
	if userInfo.PeerID == "" {
		return fmt.Errorf("节点ID不能为空")
	}

	return nil
}

// Select 在多个记录中选择最佳的一个（选择最新的）
func (v *UserInfoValidator) Select(key string, values [][]byte) (int, error) {
	if len(values) == 0 {
		return 0, fmt.Errorf("没有可选的记录")
	}

	// 选择时间戳最新的记录
	bestIndex := 0
	bestTimestamp := int64(0)

	for i, value := range values {
		var userInfo UserInfo
		if err := json.Unmarshal(value, &userInfo); err != nil {
			continue // 跳过无效记录
		}

		if userInfo.Timestamp > bestTimestamp {
			bestTimestamp = userInfo.Timestamp
			bestIndex = i
		}
	}

	return bestIndex, nil
}

// UserInfo 用户信息（存储在DHT中）
type UserInfo struct {
	Username  string   `json:"username"`
	PeerID    string   `json:"peer_id"`
	Addresses []string `json:"addresses"`
	Timestamp int64    `json:"timestamp"`
}

// DHTDiscovery DHT发现服务
type DHTDiscovery struct {
	host       host.Host
	dht        *dht.IpfsDHT
	username   string
	mutex      sync.RWMutex
	// 本地缓存的用户列表（按用户名索引）
	localUsers map[string]*UserInfo
	// 按节点ID索引的用户信息（用于快速查找）
	peerIDToUser map[string]*UserInfo
}

// NewDHTDiscovery 创建DHT发现服务
func NewDHTDiscovery(ctx context.Context, h host.Host, username string) (*DHTDiscovery, error) {
	// 创建自定义验证器
	validator := &UserInfoValidator{}

	// 创建DHT实例，使用自定义协议前缀和命名空间验证器
	// 尝试不同的配置方法：使用 "pchat" 作为命名空间
	kademliaDHT, err := dht.New(ctx, h, 
		dht.Mode(dht.ModeServer),
		dht.ProtocolPrefix("/pchat"),
		dht.NamespacedValidator("pchat", validator), // 使用 "pchat" 作为命名空间
	)
	if err != nil {
		return nil, fmt.Errorf("创建DHT失败: %v", err)
	}

	// 启动DHT
	if err := kademliaDHT.Bootstrap(ctx); err != nil {
		return nil, fmt.Errorf("启动DHT失败: %v", err)
	}

	discovery := &DHTDiscovery{
		host:         h,
		dht:          kademliaDHT,
		username:     username,
		localUsers:   make(map[string]*UserInfo),
		peerIDToUser: make(map[string]*UserInfo),
	}

	// 启动定期广播和清理
	go discovery.startPeriodicTasks(ctx)

	return discovery, nil
}

// startPeriodicTasks 启动定期任务
func (dd *DHTDiscovery) startPeriodicTasks(ctx context.Context) {
	// 定期广播自己的信息
	broadcastTicker := time.NewTicker(30 * time.Second)
	defer broadcastTicker.Stop()

	// 定期清理过期用户
	cleanupTicker := time.NewTicker(1 * time.Minute)
	defer cleanupTicker.Stop()

	// 定期发现网络中的其他用户
	discoverTicker := time.NewTicker(1 * time.Minute)
	defer discoverTicker.Stop()

	// 立即执行一次发现
	go func() {
		time.Sleep(5 * time.Second) // 等待DHT初始化
		dd.discoverNetworkUsers(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-broadcastTicker.C:
			dd.AnnounceSelf(ctx)
		case <-cleanupTicker.C:
			dd.cleanupExpiredUsers()
		case <-discoverTicker.C:
			dd.discoverNetworkUsers(ctx)
		}
	}
}

// AnnounceSelf 广播自己的信息到DHT
func (dd *DHTDiscovery) AnnounceSelf(ctx context.Context) {
	userInfo := UserInfo{
		Username:  dd.username,
		PeerID:    dd.host.ID().String(),
		Addresses: dd.getAddresses(),
		Timestamp: time.Now().Unix(),
	}

	// 将自己的用户信息添加到本地缓存
	dd.mutex.Lock()
	dd.localUsers[dd.username] = &userInfo
	dd.peerIDToUser[userInfo.PeerID] = &userInfo
	dd.mutex.Unlock()

	key := dd.getUserKey(dd.username)
	value, err := json.Marshal(userInfo)
	if err != nil {
		log.Printf("序列化用户信息失败: %v\n", err)
		return
	}

	// 存储到DHT
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	if err := dd.dht.PutValue(ctx, key, value); err != nil {
		// DHT存储失败可能是正常的（当网络节点少时）
		// 检查是否是"找不到节点"的错误（这是正常的，当DHT网络节点少时）
		errStr := err.Error()
		if strings.Contains(errStr, "failed to find any peer") || strings.Contains(errStr, "not enough peers") {
			// 这是正常的，当DHT网络中节点少时会出现
			// 不影响基本功能，静默处理或只记录调试信息
			log.Printf("💡 DHT网络节点较少，用户信息暂未存储到DHT（不影响P2P通信功能）\n")
		} else {
			// 其他错误，记录详细信息
			log.Printf("⚠️  DHT存储失败: %v (键: %s)\n", err, key)
			log.Printf("💡 提示：用户发现功能可能受限，但P2P通信功能正常\n")
		}
	} else {
		log.Printf("✅ 已广播用户信息到DHT网络 (键: %s)\n", key)
	}
}

// LookupUser 查找用户
func (dd *DHTDiscovery) LookupUser(ctx context.Context, username string) (*UserInfo, error) {
	// 先检查本地缓存
	dd.mutex.RLock()
	if userInfo, exists := dd.localUsers[username]; exists {
		if time.Now().Unix()-userInfo.Timestamp < int64(userInfoTTL.Seconds()) {
			dd.mutex.RUnlock()
			return userInfo, nil
		}
	}
	dd.mutex.RUnlock()

	// 从DHT查找
	key := dd.getUserKey(username)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	value, err := dd.dht.GetValue(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("DHT查找失败: %v", err)
	}

	var userInfo UserInfo
	if err := json.Unmarshal(value, &userInfo); err != nil {
		return nil, fmt.Errorf("解析用户信息失败: %v", err)
	}

	// 更新本地缓存
	dd.mutex.Lock()
	dd.localUsers[username] = &userInfo
	dd.peerIDToUser[userInfo.PeerID] = &userInfo
	dd.mutex.Unlock()

	return &userInfo, nil
}

// ListUsers 列出所有已知用户（从本地缓存）
func (dd *DHTDiscovery) ListUsers() []*UserInfo {
	dd.mutex.RLock()
	defer dd.mutex.RUnlock()

	users := make([]*UserInfo, 0, len(dd.localUsers))
	now := time.Now().Unix()
	
	for _, user := range dd.localUsers {
		// 只返回未过期的用户
		if now-user.Timestamp < int64(userInfoTTL.Seconds()) {
			users = append(users, user)
		}
	}

	return users
}

// DiscoverUsers 发现网络中的用户（通过DHT查询）
func (dd *DHTDiscovery) DiscoverUsers(ctx context.Context) error {
	// 查找一些常见的用户名前缀（简化实现）
	// 在实际应用中，可以使用更复杂的发现机制
	
	// 这里我们通过查找自己的用户名来触发DHT网络遍历
	// 实际应用中可能需要更复杂的发现协议
	_, err := dd.LookupUser(ctx, dd.username)
	return err
}

// discoverNetworkUsers 发现网络中的其他用户
func (dd *DHTDiscovery) discoverNetworkUsers(ctx context.Context) {
	// 获取当前已连接的peer
	conns := dd.host.Network().Conns()
	if len(conns) == 0 {
		return
	}
	
	// 对于每个已连接的peer，尝试通过DHT查找其用户信息
	// 方法：尝试通过DHT查找所有可能的用户名
	// 由于DHT的限制，我们无法直接枚举所有用户
	// 但我们可以通过以下方式改进：
	// 1. 尝试查找常见的用户名（如 "Alice", "Bob" 等）
	// 2. 通过DHT路由表中的peer来发现用户
	// 3. 实现一个用户发现协议（当peer连接时交换用户名）
	
	discoveredCount := 0
	for _, conn := range conns {
		peerID := conn.RemotePeer()
		peerIDStr := peerID.String()
		
		// 检查是否已经知道这个peer的用户信息
		dd.mutex.RLock()
		_, exists := dd.peerIDToUser[peerIDStr]
		dd.mutex.RUnlock()
		
		if exists {
			continue // 已经知道这个peer的用户信息
		}
		
		// 尝试通过DHT查找这个peer的用户信息
		// 方法：尝试查找常见的用户名，或者通过peer ID来推断
		// 由于我们不知道用户名，我们尝试一些常见的方法：
		// 1. 尝试使用peer ID的短格式作为用户名
		// 2. 尝试查找所有已知的用户名
		
		// 获取已知的用户名列表（从本地缓存）
		dd.mutex.RLock()
		knownUsernames := make([]string, 0, len(dd.localUsers))
		for username := range dd.localUsers {
			knownUsernames = append(knownUsernames, username)
		}
		dd.mutex.RUnlock()
		
		// 尝试通过已知的用户名来查找
		found := false
		for _, username := range knownUsernames {
			userInfo, err := dd.LookupUser(ctx, username)
			if err == nil && userInfo.PeerID == peerIDStr {
				// 找到了这个peer的用户信息
				discoveredCount++
				found = true
				break
			}
		}
		
		// 如果没有找到，尝试查找常见的用户名
		if !found {
			commonUsernames := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Frank", "Grace", "Henry"}
			for _, username := range commonUsernames {
				userInfo, err := dd.LookupUser(ctx, username)
				if err == nil && userInfo.PeerID == peerIDStr {
					// 找到了这个peer的用户信息
					discoveredCount++
					found = true
					break
				}
			}
		}
		
		// 如果还是没有找到，尝试通过DHT路由表来发现
		// 获取DHT路由表中的所有peer
		routingTable := dd.dht.RoutingTable()
		if routingTable != nil && !found {
			// 尝试查找路由表中的peer
			// 由于DHT的限制，我们无法直接获取用户名
			// 但我们可以通过遍历DHT网络来发现用户
			// 这里我们简化实现：通过已连接的peer来发现用户
		}
	}
	
	if discoveredCount > 0 {
		log.Printf("✅ 发现了 %d 个新用户\n", discoveredCount)
	}
}

// GetUserByPeerID 根据节点ID获取用户信息
func (dd *DHTDiscovery) GetUserByPeerID(peerID string) *UserInfo {
	dd.mutex.RLock()
	defer dd.mutex.RUnlock()
	
	if userInfo, exists := dd.peerIDToUser[peerID]; exists {
		// 检查是否过期
		if time.Now().Unix()-userInfo.Timestamp < int64(userInfoTTL.Seconds()) {
			return userInfo
		}
	}
	return nil
}

// cleanupExpiredUsers 清理过期的用户
func (dd *DHTDiscovery) cleanupExpiredUsers() {
	dd.mutex.Lock()
	defer dd.mutex.Unlock()

	now := time.Now().Unix()
	for username, user := range dd.localUsers {
		if now-user.Timestamp >= int64(userInfoTTL.Seconds()) {
			delete(dd.localUsers, username)
			delete(dd.peerIDToUser, user.PeerID)
		}
	}
}

// getUserKey 生成用户DHT键
func (dd *DHTDiscovery) getUserKey(username string) string {
	// 使用base32编码用户名作为键
	encoded := base32.StdEncoding.EncodeToString([]byte(username))
	return dhtNamespace + encoded
}

// getAddresses 获取当前节点的地址
func (dd *DHTDiscovery) getAddresses() []string {
	addresses := make([]string, 0)
	for _, addr := range dd.host.Addrs() {
		addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", addr, dd.host.ID()))
	}
	return addresses
}

// Close 关闭DHT发现服务
func (dd *DHTDiscovery) Close() error {
	return dd.dht.Close()
}

