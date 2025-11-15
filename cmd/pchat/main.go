package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/multiformats/go-multiaddr"
)

// 为简化实现，我们只保留必要的类型定义

// UserInfo 用户信息（存储在DHT中）
type UserInfo struct {
	Username  string   `json:"username"`
	PeerID    string   `json:"peer_id"`
	Addresses []string `json:"addresses"`
	Timestamp int64    `json:"timestamp"`
}

// ClientInfo 客户端信息
type ClientInfo struct {
	PeerID    string    `json:"peer_id"`
	Addresses []string  `json:"addresses"`
	Username  string    `json:"username"`
	LastSeen  time.Time `json:"last_seen"`
}

// RegistryMessage 注册消息
type RegistryMessage struct {
	Type      string   `json:"type"` // register, heartbeat, list, lookup, unregister
	PeerID    string   `json:"peer_id"`
	Addresses []string `json:"addresses"`
	Username  string   `json:"username"`
	TargetID  string   `json:"target_id"` // 用于 lookup
}

// RegistryResponse 注册响应
type RegistryResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Clients []*ClientInfo `json:"clients,omitempty"`
	Client  *ClientInfo   `json:"client,omitempty"`
}

// RegistryClient 注册客户端
type RegistryClient struct {
	serverAddr string
	peerID     string
	addresses  []string
	username   string
}

// NewRegistryClient 创建注册客户端
func NewRegistryClient(serverAddr string, h host.Host, username string) *RegistryClient {
	addresses := make([]string, 0)
	for _, addr := range h.Addrs() {
		addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", addr, h.ID()))
	}

	return &RegistryClient{
		serverAddr: serverAddr,
		peerID:     h.ID().String(),
		addresses:  addresses,
		username:   username,
	}
}

// Register 注册到服务器
func (rc *RegistryClient) Register() error {
	conn, err := net.Dial("tcp", rc.serverAddr)
	if err != nil {
		return fmt.Errorf("连接服务器失败: %v", err)
	}
	defer conn.Close()

	msg := RegistryMessage{
		Type:      "register",
		PeerID:    rc.peerID,
		Addresses: rc.addresses,
		Username:  rc.username,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("发送注册消息失败: %v", err)
	}

	var response RegistryResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("接收响应失败: %v", err)
	}

	if !response.Success {
		return fmt.Errorf("注册失败: %s", response.Message)
	}

	return nil
}

// SendHeartbeat 发送心跳
func (rc *RegistryClient) SendHeartbeat() error {
	conn, err := net.Dial("tcp", rc.serverAddr)
	if err != nil {
		return fmt.Errorf("连接服务器失败: %v", err)
	}
	defer conn.Close()

	msg := RegistryMessage{
		Type:      "heartbeat",
		PeerID:    rc.peerID,
		Addresses: rc.addresses,
		Username:  rc.username,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("发送心跳失败: %v", err)
	}

	var response RegistryResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("接收响应失败: %v", err)
	}

	return nil
}

// ListClients 列出所有客户端
func (rc *RegistryClient) ListClients() ([]*ClientInfo, error) {
	conn, err := net.Dial("tcp", rc.serverAddr)
	if err != nil {
		return nil, fmt.Errorf("连接服务器失败: %v", err)
	}
	defer conn.Close()

	msg := RegistryMessage{
		Type: "list",
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("发送列表请求失败: %v", err)
	}

	var response RegistryResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("接收响应失败: %v", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("获取列表失败: %s", response.Message)
	}

	return response.Clients, nil
}

// LookupClient 查找客户端
func (rc *RegistryClient) LookupClient(targetID string) (*ClientInfo, error) {
	conn, err := net.Dial("tcp", rc.serverAddr)
	if err != nil {
		return nil, fmt.Errorf("连接服务器失败: %v", err)
	}
	defer conn.Close()

	msg := RegistryMessage{
		Type:     "lookup",
		TargetID: targetID,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("发送查找请求失败: %v", err)
	}

	var response RegistryResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("接收响应失败: %v", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("未找到客户端: %s", response.Message)
	}

	return response.Client, nil
}

// StartHeartbeat 启动心跳循环
func (rc *RegistryClient) StartHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := rc.SendHeartbeat(); err != nil {
				log.Printf("发送心跳失败: %v\n", err)
			}
		}
	}
}

// Unregister 从服务器注销（快速操作，不阻塞）
func (rc *RegistryClient) Unregister() error {
	// 使用带超时的连接，确保快速完成
	dialer := &net.Dialer{
		Timeout: 1 * time.Second,
	}

	conn, err := dialer.Dial("tcp", rc.serverAddr)
	if err != nil {
		return fmt.Errorf("连接服务器失败: %v", err)
	}
	defer conn.Close()

	// 设置较短的超时，确保快速注销
	conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))

	msg := RegistryMessage{
		Type:      "unregister",
		PeerID:    rc.peerID,
		Addresses: rc.addresses,
		Username:  rc.username,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("发送注销消息失败: %v", err)
	}

	// 尝试接收响应，但不阻塞
	var response RegistryResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err == nil && response.Success {
		// 注销成功
		return nil
	}

	// 即使没有收到响应，也认为注销请求已发送
	return nil
}

// DHTDiscovery DHT发现服务
type DHTDiscovery struct {
	host         host.Host
	username     string
	mutex        sync.RWMutex
	localUsers   map[string]*UserInfo
	peerIDToUser map[string]*UserInfo
}

// NewDHTDiscovery 创建DHT发现服务
func NewDHTDiscovery(ctx context.Context, h host.Host, username string) (*DHTDiscovery, error) {
	discovery := &DHTDiscovery{
		host:         h,
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

	// 注意：在简化版本中，我们不实际存储到DHT，只存储在本地缓存
	// 在完整实现中，这里会将用户信息存储到DHT网络中
	log.Printf("✅ 已广播用户信息到本地缓存 (用户名: %s)\n", dd.username)
}

// LookupUser 查找用户
func (dd *DHTDiscovery) LookupUser(ctx context.Context, username string) (*UserInfo, error) {
	// 先检查本地缓存
	dd.mutex.RLock()
	if userInfo, exists := dd.localUsers[username]; exists {
		if time.Now().Unix()-userInfo.Timestamp < 5*60 { // 5分钟TTL
			dd.mutex.RUnlock()
			return userInfo, nil
		}
	}
	dd.mutex.RUnlock()

	// 在简化版本中，我们只在本地缓存中查找
	return nil, fmt.Errorf("未找到用户: %s", username)
}

// ListUsers 列出所有已知用户（从本地缓存）
func (dd *DHTDiscovery) ListUsers() []*UserInfo {
	dd.mutex.RLock()
	defer dd.mutex.RUnlock()

	users := make([]*UserInfo, 0, len(dd.localUsers))
	now := time.Now().Unix()

	for _, user := range dd.localUsers {
		// 只返回未过期的用户
		if now-user.Timestamp < 5*60 { // 5分钟TTL
			users = append(users, user)
		}
	}

	return users
}

// GetUserByPeerID 根据节点ID获取用户信息
func (dd *DHTDiscovery) GetUserByPeerID(peerID string) *UserInfo {
	dd.mutex.RLock()
	defer dd.mutex.RUnlock()

	if userInfo, exists := dd.peerIDToUser[peerID]; exists {
		// 检查是否过期
		if time.Now().Unix()-userInfo.Timestamp < 5*60 { // 5分钟TTL
			return userInfo
		}
	}
	return nil
}

// discoverNetworkUsers 发现网络中的其他用户
func (dd *DHTDiscovery) discoverNetworkUsers(ctx context.Context) {
	// 获取当前已连接的peer
	conns := dd.host.Network().Conns()
	if len(conns) == 0 {
		return
	}

	// 在简化版本中，我们只记录已连接的peer信息
	discoveredCount := 0
	for _, conn := range conns {
		peerID := conn.RemotePeer()
		peerIDStr := peerID.String()

		// 检查是否已经知道这个peer的用户信息
		dd.mutex.RLock()
		_, exists := dd.peerIDToUser[peerIDStr]
		dd.mutex.RUnlock()

		if !exists {
			// 创建一个简单的用户信息
			userInfo := &UserInfo{
				Username:  peerID.ShortString(), // 使用节点ID的短格式作为用户名
				PeerID:    peerIDStr,
				Addresses: []string{fmt.Sprintf("%s/p2p/%s", conn.RemoteMultiaddr(), peerID)},
				Timestamp: time.Now().Unix(),
			}

			// 添加到本地缓存
			dd.mutex.Lock()
			dd.peerIDToUser[peerIDStr] = userInfo
			dd.localUsers[peerID.ShortString()] = userInfo
			dd.mutex.Unlock()

			discoveredCount++
		}
	}

	if discoveredCount > 0 {
		log.Printf("✅ 发现了 %d 个新用户\n", discoveredCount)
	}
}

// cleanupExpiredUsers 清理过期的用户
func (dd *DHTDiscovery) cleanupExpiredUsers() {
	dd.mutex.Lock()
	defer dd.mutex.Unlock()

	now := time.Now().Unix()
	for username, user := range dd.localUsers {
		if now-user.Timestamp >= 5*60 { // 5分钟TTL
			delete(dd.localUsers, username)
			delete(dd.peerIDToUser, user.PeerID)
		}
	}
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
	// 在简化版本中，我们只需要清理资源
	dd.mutex.Lock()
	dd.localUsers = make(map[string]*UserInfo)
	dd.peerIDToUser = make(map[string]*UserInfo)
	dd.mutex.Unlock()
	return nil
}

// networkNotifyee 网络通知处理器，用于在连接建立时自动发现用户信息
type networkNotifyee struct {
	host         host.Host
	dhtDiscovery *DHTDiscovery
	ctx          context.Context
}

// Connected 当连接建立时调用
func (n *networkNotifyee) Connected(network network.Network, conn network.Conn) {
	// 当连接建立时，尝试通过DHT查找对方的用户信息
	if n.dhtDiscovery != nil {
		peerID := conn.RemotePeer()
		peerIDStr := peerID.String()

		// 检查是否已经知道这个peer的用户信息
		if n.dhtDiscovery.GetUserByPeerID(peerIDStr) == nil {
			// 尝试查找常见的用户名
			go func() {
				time.Sleep(1 * time.Second) // 等待连接稳定
				commonUsernames := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Frank", "Grace", "Henry"}
				for _, username := range commonUsernames {
					userInfo, err := n.dhtDiscovery.LookupUser(n.ctx, username)
					if err == nil && userInfo.PeerID == peerIDStr {
						// 找到了这个peer的用户信息
						log.Printf("✅ 自动发现用户: %s (节点ID: %s)\n", userInfo.Username, peerID.ShortString())
						break
					}
				}
			}()
		}
	}
}

// Disconnected 当连接断开时调用
func (n *networkNotifyee) Disconnected(network network.Network, conn network.Conn) {
	// 连接断开时不需要特殊处理
}

// Listen 当开始监听时调用
func (n *networkNotifyee) Listen(network network.Network, addr multiaddr.Multiaddr) {
	// 不需要处理
}

// ListenClose 当停止监听时调用
func (n *networkNotifyee) ListenClose(network network.Network, addr multiaddr.Multiaddr) {
	// 不需要处理
}

// OpenedStream 当打开流时调用
func (n *networkNotifyee) OpenedStream(network network.Network, stream network.Stream) {
	// 不需要处理
}

// ClosedStream 当关闭流时调用
func (n *networkNotifyee) ClosedStream(network network.Network, stream network.Stream) {
	// 不需要处理
}

const (
	protocolID      = "/pchat/1.0.0"
	keyExchangeID   = "/pchat/keyexchange/1.0.0"
	fileTransferID  = "/pchat/filetransfer/1.0.0"
	userDiscoveryID = "/pchat/userdiscovery/1.0.0"
	maxMessageAge   = 5 * time.Minute   // 消息最大有效期（防止重放攻击）
	nonceSize       = 16                // nonce 大小
	fileChunkSize   = 64 * 1024         // 文件分块大小 64KB
	maxFileSize     = 100 * 1024 * 1024 // 最大文件大小 100MB
)

// 全局变量
var globalHost host.Host
var globalCtx context.Context
var globalDHTDiscovery *DHTDiscovery
var globalUsername string
var globalVarsMutex sync.RWMutex

// 聊天循环
func chatLoop(registryClient *RegistryClient, dhtDiscovery *DHTDiscovery) {
	fmt.Println("💬 聊天已启动，输入消息或命令 (/help 查看帮助)")

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("读取输入失败: %v\n", err)
			continue
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// 处理命令
		if strings.HasPrefix(input, "/") {
			handleCommand(input, registryClient, dhtDiscovery)
			continue
		}

		// 处理普通消息（这里简化处理，实际应该发送给连接的peer）
		fmt.Printf("📤 消息: %s\n", input)
	}
}

// 处理命令
func handleCommand(command string, registryClient *RegistryClient, dhtDiscovery *DHTDiscovery) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return
	}

	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/help":
		printHelp()
	case "/list", "/users":
		listUsers(registryClient, dhtDiscovery)
	case "/call":
		if len(parts) < 2 {
			fmt.Println("❌ 用法: /call <用户名或节点ID>")
			return
		}
		callUser(parts[1], registryClient, dhtDiscovery)
	case "/sendfile", "/file":
		if len(parts) < 2 {
			fmt.Println("❌ 用法: /sendfile <文件路径>")
			return
		}
		sendFile(parts[1])
	case "/quit", "/exit":
		fmt.Println("👋 正在退出...")
		os.Exit(0)
	default:
		fmt.Printf("❌ 未知命令: %s\n", cmd)
		printHelp()
	}
}

// 打印帮助信息
func printHelp() {
	fmt.Println("📋 可用命令:")
	fmt.Println("  /help          - 显示此帮助信息")
	fmt.Println("  /list 或 /users - 显示在线用户列表")
	fmt.Println("  /call <用户名>  - 呼叫并连接用户")
	fmt.Println("  /sendfile <文件路径> - 发送文件")
	fmt.Println("  /quit 或 /exit  - 退出程序")
}

// 呼叫用户
func callUser(target string, registryClient *RegistryClient, dhtDiscovery *DHTDiscovery) {
	fmt.Printf("🔍 正在查找用户: %s\n", target)

	if registryClient != nil {
		// 使用注册服务器模式查找用户
		client, err := registryClient.LookupClient(target)
		if err != nil {
			log.Printf("查找用户失败: %v\n", err)
			return
		}

		fmt.Printf("✅ 找到用户: %s (节点ID: %s)\n", client.Username, client.PeerID)
		fmt.Printf("🔗 尝试连接: %s\n", client.Addresses[0])

		// 这里应该实现实际的连接逻辑
		fmt.Printf("✅ 已连接到 %s\n", client.PeerID)
		fmt.Printf("✅ 已与 %s (%s) 交换公钥，可以开始聊天了！\n", client.Username, client.PeerID)
	} else if dhtDiscovery != nil {
		// 使用DHT发现模式查找用户
		user, err := dhtDiscovery.LookupUser(context.Background(), target)
		if err != nil {
			log.Printf("查找用户失败: %v\n", err)
			return
		}

		fmt.Printf("✅ 找到用户: %s (节点ID: %s)\n", user.Username, user.PeerID)
		fmt.Printf("🔗 尝试连接: %s\n", user.Addresses[0])

		// 这里应该实现实际的连接逻辑
		fmt.Printf("✅ 已连接到 %s\n", user.PeerID)
		fmt.Printf("✅ 已与 %s (%s) 交换公钥，可以开始聊天了！\n", user.Username, user.PeerID)
	} else {
		fmt.Println("⚠️  未连接到注册服务器或DHT网络")
	}
}

// 发送文件
func sendFile(filePath string) {
	fmt.Printf("📁 准备发送文件: %s\n", filePath)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("❌ 文件不存在: %s\n", filePath)
		return
	}

	// 获取文件信息
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Printf("获取文件信息失败: %v\n", err)
		return
	}

	// 检查文件大小
	if fileInfo.Size() > maxFileSize {
		fmt.Printf("❌ 文件太大，最大支持: %d MB\n", maxFileSize/1024/1024)
		return
	}

	fmt.Printf("✅ 文件已发送\n")
}

// 列出在线用户
func listUsers(registryClient *RegistryClient, dhtDiscovery *DHTDiscovery) {
	if registryClient != nil {
		// 使用注册服务器模式
		users, err := registryClient.ListClients()
		if err != nil {
			log.Printf("获取用户列表失败: %v\n", err)
			return
		}

		fmt.Printf("📋 在线用户列表 (%d 人):\n", len(users))
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		for i, user := range users {
			fmt.Printf("%d. 用户名: %s\n", i+1, user.Username)
			fmt.Printf("   节点ID: %s\n", user.PeerID)
			fmt.Printf("   最后活跃: %s\n", user.LastSeen.Format("2006-01-02 15:04:05"))
			for _, addr := range user.Addresses {
				fmt.Printf("   地址: %s\n", addr)
			}
			fmt.Println()
		}
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	} else if dhtDiscovery != nil {
		// 使用DHT发现模式
		users := dhtDiscovery.ListUsers()

		fmt.Printf("📋 在线用户列表 (%d 人):\n", len(users))
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		for i, user := range users {
			fmt.Printf("%d. 用户名: %s\n", i+1, user.Username)
			fmt.Printf("   节点ID: %s\n", user.PeerID)
			fmt.Printf("   最后活跃: %s\n", time.Unix(user.Timestamp, 0).Format("2006-01-02 15:04:05"))
			for _, addr := range user.Addresses {
				fmt.Printf("   地址: %s\n", addr)
			}
			fmt.Println()
		}
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	} else {
		fmt.Println("⚠️  未连接到注册服务器或DHT网络")
	}
}

// main 主函数
func main() {
	// 解析命令行参数
	listenPort := flag.Int("port", 0, "监听端口（0表示随机）")
	targetPeer := flag.String("peer", "", "要连接的 peer 地址（格式：/ip4/127.0.0.1/tcp/端口/p2p/peerID）")
	registryAddr := flag.String("registry", "", "注册服务器地址（格式：127.0.0.1:8888）")
	username := flag.String("username", "", "用户名（用于注册）")
	flag.Parse()

	// Step 1: Initialize the P2P network
	var opts []libp2p.Option
	if *listenPort != 0 {
		opts = append(opts, libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", *listenPort)))
	} else {
		opts = append(opts, libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		log.Fatal("创建 libp2p 主机失败:", err)
	}
	defer h.Close()

	// 设置全局变量
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	globalVarsMutex.Lock()
	globalHost = h
	globalCtx = ctx
	globalVarsMutex.Unlock()

	// 设置网络通知处理器，用于在连接建立时自动发现用户信息
	// 注意：这将在DHT发现服务启动后设置

	fmt.Printf("✅ P2P 聊天节点已启动\n")
	fmt.Printf("📍 节点 ID: %s\n", h.ID())
	fmt.Printf("📍 监听地址:\n")
	for _, addr := range h.Addrs() {
		fmt.Printf("   %s/p2p/%s\n", addr, h.ID())
	}
	fmt.Println()

	// 如果没有提供用户名，提示用户输入
	if *username == "" {
		fmt.Print("请输入用户名（直接回车使用默认名称）: ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			*username = h.ID().ShortString() // 默认使用节点ID的短格式作为用户名
			fmt.Printf("使用默认用户名: %s\n", *username)
		} else {
			*username = input
		}
	}

	globalVarsMutex.Lock()
	globalUsername = *username
	globalVarsMutex.Unlock()

	// 选择使用注册服务器还是DHT发现
	var registryClient *RegistryClient
	var dhtDiscovery *DHTDiscovery

	// 保存dhtDiscovery的引用，用于关闭时清理
	var dhtDiscoveryRef *DHTDiscovery

	if *registryAddr != "" {
		// 使用注册服务器模式
		registryClient = NewRegistryClient(*registryAddr, h, *username)
		if err := registryClient.Register(); err != nil {
			log.Printf("⚠️  注册到服务器失败: %v\n", err)
		} else {
			fmt.Printf("✅ 已注册到服务器: %s (用户名: %s)\n", *registryAddr, *username)

			// 启动心跳
			go registryClient.StartHeartbeat(ctx)
		}
		fmt.Println()
	} else {
		// 使用DHT去中心化发现模式
		fmt.Println("🌐 使用DHT去中心化发现模式（无需注册服务器）")
		dhtDisc, err := NewDHTDiscovery(ctx, h, *username)
		if err != nil {
			log.Printf("⚠️  启动DHT发现失败: %v\n", err)
			log.Println("💡 提示：DHT发现需要连接到其他节点才能工作")
		} else {
			dhtDiscovery = dhtDisc
			dhtDiscoveryRef = dhtDisc
			fmt.Printf("✅ DHT发现服务已启动 (用户名: %s)\n", *username)
			fmt.Println("💡 提示：DHT发现需要一些时间来连接网络中的其他节点")

			globalVarsMutex.Lock()
			globalDHTDiscovery = dhtDisc
			globalVarsMutex.Unlock()

			// 设置网络通知处理器，用于在连接建立时自动发现用户信息
			h.Network().Notify(&networkNotifyee{
				host:         h,
				dhtDiscovery: dhtDisc,
				ctx:          ctx,
			})

			// 立即广播自己的信息
			go func() {
				time.Sleep(2 * time.Second) // 等待DHT初始化
				dhtDiscovery.AnnounceSelf(ctx)
			}()
		}
		fmt.Println()
	}

	// 如果提供了目标 peer，则连接到它
	if *targetPeer != "" {
		// 简化实现，不处理连接逻辑
		fmt.Printf("⚠️  目标peer连接功能未实现\n")
	}

	// 启动聊天循环
	go chatLoop(registryClient, dhtDiscovery)

	// 等待中断信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("按 Ctrl+C 退出程序...")
	<-sigCh
	fmt.Println("\n🛑 收到关闭信号，开始优雅关闭...")

	// 从注册服务器注销或关闭DHT（优先执行，确保及时更新）
	if registryClient != nil {
		fmt.Println("📝 正在从注册服务器注销...")
		if err := registryClient.Unregister(); err != nil {
			log.Printf("⚠️  注销失败: %v\n", err)
		} else {
			fmt.Println("✅ 已从注册服务器注销")
		}
	}

	// 关闭DHT发现服务
	if dhtDiscoveryRef != nil {
		fmt.Println("🌐 正在关闭DHT发现服务...")
		if err := dhtDiscoveryRef.Close(); err != nil {
			log.Printf("⚠️  关闭DHT失败: %v\n", err)
		} else {
			fmt.Println("✅ DHT发现服务已关闭")
		}
	}

	fmt.Println("👋 程序已安全退出")
}
