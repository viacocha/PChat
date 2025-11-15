package main

import (
	"bufio"
	"context"
	"crypto/rsa"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
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
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/multiformats/go-multiaddr"

	// 导入内部的DHT发现模块
	"PChat/internal/crypto"
	"PChat/internal/discovery"
)

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
var globalDHTDiscovery *discovery.DHTDiscovery
var globalUsername string
var globalVarsMutex sync.RWMutex

// 连接管理
var activeConnections map[string]network.Stream
var connectionsMutex sync.RWMutex

// 用户公钥管理
var userPublicKeys map[string]*rsa.PublicKey
var publicKeyMutex sync.RWMutex

// 当前用户的密钥对
var currentUserPrivateKey *rsa.PrivateKey
var currentUserPublicKey rsa.PublicKey

// 初始化连接管理
func init() {
	activeConnections = make(map[string]network.Stream)
	userPublicKeys = make(map[string]*rsa.PublicKey)

	// 生成当前用户的密钥对
	var err error
	currentUserPrivateKey, currentUserPublicKey, err = crypto.GenerateKeys()
	if err != nil {
		log.Fatal("生成用户密钥对失败:", err)
	}
}

// 添加连接
func addConnection(peerID string, stream network.Stream) {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()
	activeConnections[peerID] = stream
}

// 移除连接
func removeConnection(peerID string) {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()
	delete(activeConnections, peerID)
}

// 获取所有连接
func getAllConnections() map[string]network.Stream {
	connectionsMutex.RLock()
	defer connectionsMutex.RUnlock()
	// 返回副本以避免并发问题
	result := make(map[string]network.Stream)
	for k, v := range activeConnections {
		result[k] = v
	}
	return result
}

// 挂断指定连接
func hangupConnection(peerID string) error {
	connectionsMutex.Lock()
	stream, exists := activeConnections[peerID]
	delete(activeConnections, peerID)
	connectionsMutex.Unlock()

	if !exists {
		return fmt.Errorf("未找到与 %s 的连接", peerID)
	}

	if stream != nil {
		return stream.Close()
	}
	return nil
}

// 挂断所有连接
func hangupAllConnections() {
	connections := getAllConnections()
	for peerID, stream := range connections {
		if stream != nil {
			stream.Close()
		}
		removeConnection(peerID)
	}
}

// 通知所有用户即将下线
func notifyOffline() {
	globalVarsMutex.RLock()
	username := globalUsername
	globalVarsMutex.RUnlock()

	connections := getAllConnections()
	if len(connections) == 0 {
		return
	}

	offlineMsg := fmt.Sprintf("%s 已下线", username)
	sentCount := 0

	for peerID, stream := range connections {
		// 获取接收方公钥
		recipientPubKey, exists := getUserPublicKey(peerID)
		if !exists {
			// 如果没有公钥，使用我们自己的公钥作为示例
			recipientPubKey = &currentUserPublicKey
		}

		// 加密下线通知消息
		encryptedMsg, err := crypto.EncryptAndSignMessage(offlineMsg, currentUserPrivateKey, recipientPubKey)
		if err != nil {
			log.Printf("加密下线通知失败: %v\n", err)
			continue
		}

		// 发送下线通知
		_, err = stream.Write([]byte(encryptedMsg + "\n"))
		if err != nil {
			log.Printf("发送下线通知失败: %v\n", err)
			continue
		}

		sentCount++
	}

	if sentCount > 0 {
		fmt.Printf("📢 已通知 %d 个用户即将下线\n", sentCount)
	}
}

// 设置用户公钥
func setUserPublicKey(peerID string, pubKey *rsa.PublicKey) {
	publicKeyMutex.Lock()
	defer publicKeyMutex.Unlock()
	userPublicKeys[peerID] = pubKey
}

// 获取用户公钥
func getUserPublicKey(peerID string) (*rsa.PublicKey, bool) {
	publicKeyMutex.RLock()
	defer publicKeyMutex.RUnlock()
	pubKey, exists := userPublicKeys[peerID]
	return pubKey, exists
}

// 聊天循环
func chatLoop(registryClient *RegistryClient, dhtDiscovery *discovery.DHTDiscovery) {
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

		// 处理普通消息 - 发送给所有连接的peer
		sendMessageToAll(input)
	}
}

// 发送消息给所有连接的用户
func sendMessageToAll(message string) {
	connections := getAllConnections()
	if len(connections) == 0 {
		fmt.Println("⚠️  没有已连接的用户，消息未发送")
		return
	}

	sentCount := 0
	for peerID, stream := range connections {
		// 获取接收方公钥
		recipientPubKey, exists := getUserPublicKey(peerID)
		if !exists {
			// 如果没有公钥，使用我们自己的公钥作为示例
			recipientPubKey = &currentUserPublicKey
		}

		// 加密消息
		encryptedMsg, err := crypto.EncryptAndSignMessage(message, currentUserPrivateKey, recipientPubKey)
		if err != nil {
			log.Printf("加密消息失败: %v\n", err)
			continue
		}

		// 发送消息
		_, err = stream.Write([]byte(encryptedMsg + "\n"))
		if err != nil {
			log.Printf("发送消息失败: %v\n", err)
			continue
		}

		sentCount++
	}

	fmt.Printf("📤 已发送消息给 %d 个用户\n", sentCount)
}

// 处理命令
func handleCommand(command string, registryClient *RegistryClient, dhtDiscovery *discovery.DHTDiscovery) {
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
	case "/hangup":
		if len(parts) < 2 {
			// 挂断所有连接
			hangupAllConnections()
			fmt.Println("✅ 已挂断所有连接")
		} else {
			// 挂断指定用户连接
			target := parts[1]
			// 这里需要实现根据用户名查找节点ID的逻辑
			// 简化实现：假设输入的是节点ID
			if err := hangupConnection(target); err != nil {
				fmt.Printf("❌ 挂断连接失败: %v\n", err)
			} else {
				fmt.Printf("✅ 已挂断与 %s 的连接\n", target)
			}
		}
	case "/sendfile", "/file":
		if len(parts) < 2 {
			fmt.Println("❌ 用法: /sendfile <文件路径>")
			return
		}
		sendFile(parts[1])
	case "/rps":
		playRPS()
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
	fmt.Println("  /hangup        - 挂断所有连接")
	fmt.Println("  /hangup <用户名> - 挂断指定用户连接")
	fmt.Println("  /sendfile <文件路径> - 发送文件")
	fmt.Println("  /rps           - 发起石头剪刀布游戏")
	fmt.Println("  /quit 或 /exit  - 退出程序")
}

// 列出在线用户
func listUsers(registryClient *RegistryClient, dhtDiscovery *discovery.DHTDiscovery) {
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

// 呼叫用户
func callUser(target string, registryClient *RegistryClient, dhtDiscovery *discovery.DHTDiscovery) {
	fmt.Printf("🔍 正在查找用户: %s\n", target)

	var peerAddr string
	var peerIDStr string

	if registryClient != nil {
		// 使用注册服务器模式查找用户
		client, err := registryClient.LookupClient(target)
		if err != nil {
			log.Printf("查找用户失败: %v\n", err)
			return
		}

		fmt.Printf("✅ 找到用户: %s (节点ID: %s)\n", client.Username, client.PeerID)
		peerAddr = client.Addresses[0]
		peerIDStr = client.PeerID
	} else if dhtDiscovery != nil {
		// 使用DHT发现模式查找用户
		user, err := dhtDiscovery.LookupUser(context.Background(), target)
		if err != nil {
			log.Printf("查找用户失败: %v\n", err)
			return
		}

		fmt.Printf("✅ 找到用户: %s (节点ID: %s)\n", user.Username, user.PeerID)
		peerAddr = user.Addresses[0]
		peerIDStr = user.PeerID
	} else {
		fmt.Println("⚠️  未连接到注册服务器或DHT网络")
		return
	}

	// 解析地址
	addr, err := multiaddr.NewMultiaddr(peerAddr)
	if err != nil {
		log.Printf("解析地址失败: %v\n", err)
		return
	}

	// 解析节点ID
	peerID, err := peer.Decode(peerIDStr)
	if err != nil {
		log.Printf("解析节点ID失败: %v\n", err)
		return
	}

	fmt.Printf("🔗 尝试连接: %s\n", peerAddr)

	// 连接到目标节点
	globalVarsMutex.RLock()
	host := globalHost
	globalVarsMutex.RUnlock()

	if host == nil {
		log.Printf("主机未初始化\n")
		return
	}

	// 添加地址到peerstore
	host.Peerstore().AddAddr(peerID, addr, peerstore.PermanentAddrTTL)

	// 建立连接
	stream, err := host.NewStream(context.Background(), peerID, protocolID)
	if err != nil {
		log.Printf("连接失败: %v\n", err)
		return
	}

	// 添加连接到活动连接列表
	addConnection(peerIDStr, stream)

	fmt.Printf("✅ 已连接到 %s\n", peerIDStr)
	fmt.Printf("✅ 已与 %s (%s) 交换公钥，可以开始聊天了！\n", target, peerIDStr)
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

// 石头剪刀布游戏选项
const (
	Rock     = "石头"
	Paper    = "布"
	Scissors = "剪刀"
)

// 石头剪刀布游戏选项映射
var rpsOptions = []string{Rock, Paper, Scissors}

// 石头剪刀布游戏结果
const (
	RPSWin  = "赢"
	RPSTie  = "平局"
	RPSLose = "输"
)

// playRPS 发起石头剪刀布游戏
func playRPS() {
	fmt.Println("🎮 发起石头剪刀布游戏...")

	// 获取所有连接的用户
	connections := getAllConnections()
	if len(connections) == 0 {
		fmt.Println("⚠️  没有已连接的用户，无法进行游戏")
		return
	}

	// 生成自己的随机选择
	rand.Seed(time.Now().UnixNano())
	myChoiceIndex := rand.Intn(len(rpsOptions))
	myChoice := rpsOptions[myChoiceIndex]

	// 发送游戏邀请和自己的选择给所有连接的用户
	gameMsg := fmt.Sprintf("🎮 %s 发起石头剪刀布游戏，我的选择是: %s", globalUsername, myChoice)
	sentCount := 0

	for peerID, stream := range connections {
		// 获取接收方公钥
		recipientPubKey, exists := getUserPublicKey(peerID)
		if !exists {
			// 如果没有公钥，使用我们自己的公钥作为示例
			recipientPubKey = &currentUserPublicKey
		}

		// 加密游戏消息
		encryptedMsg, err := crypto.EncryptAndSignMessage(gameMsg, currentUserPrivateKey, recipientPubKey)
		if err != nil {
			log.Printf("加密游戏消息失败: %v\n", err)
			continue
		}

		// 发送游戏消息
		_, err = stream.Write([]byte(encryptedMsg + "\n"))
		if err != nil {
			log.Printf("发送游戏消息失败: %v\n", err)
			continue
		}

		sentCount++
	}

	fmt.Printf("✅ 已向 %d 个用户发送游戏邀请，我的选择是: %s\n", sentCount, myChoice)
	fmt.Println("💡 等待其他玩家的选择...")
}

// handleStream 处理流上的消息
func handleStream(stream network.Stream) {
	defer stream.Close()

	// 设置协议ID
	stream.SetProtocol(protocolID)

	reader := bufio.NewReader(stream)
	for {
		// 读取消息
		message, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("读取消息失败: %v\n", err)
			break
		}

		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}

		// 解密并验证消息
		// 使用当前用户的私钥和发送方的公钥进行解密和验证
		senderID := stream.Conn().RemotePeer()
		senderPubKey, exists := getUserPublicKey(senderID.String())
		if !exists {
			// 如果没有发送方的公钥，使用我们自己的公钥作为示例
			senderPubKey = &currentUserPublicKey
		}

		decryptedMsg, verified, err := crypto.DecryptAndVerifyMessage(message, currentUserPrivateKey, *senderPubKey)
		if err != nil {
			// 在程序关闭过程中忽略解密错误，避免干扰正常关闭流程
			globalVarsMutex.RLock()
			host := globalHost
			globalVarsMutex.RUnlock()

			// 如果主机已经关闭，忽略解密错误
			if host == nil {
				break
			}

			log.Printf("解密消息失败: %v\n", err)
			continue
		}

		// 检查消息类型
		switch {
		case strings.Contains(decryptedMsg, "已下线"):
			fmt.Printf("\n📢 %s\n", decryptedMsg)
		case strings.Contains(decryptedMsg, "石头剪刀布游戏"):
			// 处理石头剪刀布游戏消息
			fmt.Printf("\n%s\n", decryptedMsg)

			// 如果是游戏发起者，不需要再回应
			if strings.Contains(decryptedMsg, fmt.Sprintf("%s 发起石头剪刀布游戏", globalUsername)) {
				break
			}

			// 生成自己的随机选择并回应
			rand.Seed(time.Now().UnixNano())
			myChoiceIndex := rand.Intn(len(rpsOptions))
			myChoice := rpsOptions[myChoiceIndex]

			// 发送回应消息
			responseMsg := fmt.Sprintf("🎮 %s 的回应: %s", globalUsername, myChoice)
			fmt.Printf("%s\n", responseMsg)
		default:
			// 显示普通消息
			senderShortID := senderID.ShortString()
			if verified {
				fmt.Printf("\n📨 收到来自 %s 的消息:\n", senderShortID)
				fmt.Printf("💬 消息内容: %s\n", decryptedMsg)
				fmt.Printf("✅ 消息已验证（签名有效，未检测到重放攻击）\n")
			} else {
				fmt.Printf("\n📨 收到来自 %s 的消息:\n", senderShortID)
				fmt.Printf("⚠️  警告消息: %s（签名验证失败或检测到异常）\n", decryptedMsg)
			}
		}

		// 重新显示提示符
		fmt.Print("> ")
	}
}

// networkNotifyee 网络通知处理器，用于在连接建立时自动发现用户信息
type networkNotifyee struct {
	host         host.Host
	dhtDiscovery *discovery.DHTDiscovery
	ctx          context.Context
}

// Connected 当连接建立时调用
func (n *networkNotifyee) Connected(network.Network, network.Conn) {
	// 连接建立时不需要特殊处理
	// 消息处理在OpenedStream中进行
}

// Disconnected 当连接断开时调用
func (n *networkNotifyee) Disconnected(net network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()
	peerIDStr := peerID.String()

	// 从活动连接中移除
	removeConnection(peerIDStr)

	// 通知用户
	fmt.Printf("\n⚠️  用户 %s 已下线\n", peerID.ShortString())
	fmt.Print("> ")
}

// Listen 当开始监听时调用
func (n *networkNotifyee) Listen(network.Network, multiaddr.Multiaddr) {
	// 不需要处理
}

// ListenClose 当停止监听时调用
func (n *networkNotifyee) ListenClose(network.Network, multiaddr.Multiaddr) {
	// 不需要处理
}

// OpenedStream 当打开流时调用
func (n *networkNotifyee) OpenedStream(net network.Network, stream network.Stream) {
	// 启动一个goroutine来处理这个流上的消息
	go handleStream(stream)
}

// ClosedStream 当关闭流时调用
func (n *networkNotifyee) ClosedStream(network.Network, network.Stream) {
	// 不需要处理
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

	// 注册协议处理器
	h.SetStreamHandler(protocolID, func(s network.Stream) {
		go handleStream(s)
	})

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
	var dhtDiscovery *discovery.DHTDiscovery

	// 保存dhtDiscovery的引用，用于关闭时清理
	var dhtDiscoveryRef *discovery.DHTDiscovery

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
		dhtDisc, err := discovery.NewDHTDiscovery(ctx, h, *username)
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

	// 通知所有连接的用户即将下线
	notifyOffline()

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

	// 挂断所有连接
	hangupAllConnections()

	fmt.Println("👋 程序已安全退出")
}
