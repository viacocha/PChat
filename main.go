package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	mathrand "math/rand"
	"os"
	"os/signal"
	"strconv"
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
)

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

// SecureMessage 安全消息结构
type SecureMessage struct {
	EncryptedData []byte `json:"encrypted_data"` // 加密的消息数据
	Signature     []byte `json:"signature"`      // 数字签名
	Timestamp     int64  `json:"timestamp"`      // 时间戳
	Nonce         []byte `json:"nonce"`          // 随机数（防止重放攻击）
}

// FileTransferHeader 文件传输头部信息
type FileTransferHeader struct {
	FileName   string `json:"file_name"`   // 文件名
	FileSize   int64  `json:"file_size"`   // 文件大小
	ChunkCount int    `json:"chunk_count"` // 分块数量
	FileHash   []byte `json:"file_hash"`   // 文件 SHA256 哈希
	Signature  []byte `json:"signature"`   // 签名
	Timestamp  int64  `json:"timestamp"`   // 时间戳
	Nonce      []byte `json:"nonce"`       // 随机数
}

// FileChunk 文件分块
type FileChunk struct {
	ChunkIndex int    `json:"chunk_index"` // 分块索引
	Data       []byte `json:"data"`        // 分块数据
	Signature  []byte `json:"signature"`   // 签名
}

// 存储每个 peer 的 RSA 公钥
var peerPubKeys = make(map[peer.ID]*rsa.PublicKey)
var peerPubKeysMutex sync.RWMutex

// 存储已使用的 nonce（防止重放攻击）
var usedNonces = make(map[string]time.Time)
var usedNoncesMutex sync.RWMutex

// 石头剪刀布游戏相关
type RPSChoice struct {
	PeerID    string `json:"peer_id"`
	Choice    string `json:"choice"` // rock, paper, scissors
	Timestamp int64  `json:"timestamp"`
	Username  string `json:"username"`
}

var rpsChoices = make(map[string]*RPSChoice) // key: gameID+peerID
var rpsChoicesMutex sync.RWMutex

// 可选的石头剪刀布手势列表，避免硬编码散落在各处
var rpsOptions = []string{"rock", "paper", "scissors"}
// 当加密级随机数不可用时，使用此回退随机数生成器
var rpsFallbackRNG = mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
var rpsFallbackRNGMutex sync.Mutex

// 全局变量，用于RPS自动回复
var globalHost host.Host
var globalPrivKey *rsa.PrivateKey
var globalCtx context.Context
var globalDHTDiscovery *DHTDiscovery
// globalUsername 用于记录当前节点用户名，方便在自动回复或显示结果时使用
var globalUsername string
var globalVarsMutex sync.RWMutex

// 定期清理过期的 nonce
func init() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cleanupNonces()
		}
	}()
}

func cleanupNonces() {
	usedNoncesMutex.Lock()
	defer usedNoncesMutex.Unlock()
	now := time.Now()
	for nonce, timestamp := range usedNonces {
		if now.Sub(timestamp) > maxMessageAge {
			delete(usedNonces, nonce)
		}
	}
}

// sendOfflineNotification 发送离线通知给所有已连接的peer
func sendOfflineNotification(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, username string) {
	conns := h.Network().Conns()
	if len(conns) == 0 {
		return
	}

	fmt.Printf("📤 正在通知 %d 个已连接的peer...\n", len(conns))

	// 创建离线通知消息
	offlineMsg := fmt.Sprintf("[系统通知] %s 已离线", username)

	for _, conn := range conns {
		peerID := conn.RemotePeer()

		// 检查连接状态
		if h.Network().Connectedness(peerID) != network.Connected {
			continue
		}

		// 获取对方的公钥
		peerPubKeysMutex.RLock()
		remotePubKey, hasKey := peerPubKeys[peerID]
		peerPubKeysMutex.RUnlock()

		if !hasKey {
			continue
		}

		// 加密并签名离线通知消息
		encryptedMsg, err := encryptAndSignMessage(offlineMsg, privKey, remotePubKey)
		if err != nil {
			log.Printf("   加密离线通知失败 (%s): %v\n", peerID.ShortString(), err)
			continue
		}

		// 发送离线通知
		streamCtx, streamCancel := context.WithTimeout(ctx, 2*time.Second)
		stream, err := h.NewStream(streamCtx, peerID, protocolID)
		streamCancel()

		if err != nil {
			log.Printf("   发送离线通知失败 (%s): %v\n", peerID.ShortString(), err)
			continue
		}

		// 使用带超时的写入
		writeDone := make(chan error, 1)
		go func() {
			_, err := stream.Write([]byte(encryptedMsg + "\n"))
			writeDone <- err
		}()

		select {
		case err := <-writeDone:
			if err == nil {
				fmt.Printf("   ✅ 已通知 %s\n", peerID.ShortString())
			}
			stream.Close()
		case <-time.After(2 * time.Second):
			stream.Close()
		}
	}
}

// shutdownConnections 优雅关闭所有连接
func shutdownConnections(h host.Host) {
	conns := h.Network().Conns()
	if len(conns) == 0 {
		return
	}

	fmt.Printf("   发现 %d 个活跃连接\n", len(conns))
	for _, conn := range conns {
		peerID := conn.RemotePeer()
		fmt.Printf("   关闭与 %s 的连接...\n", peerID.ShortString())

		// 关闭连接
		if err := conn.Close(); err != nil {
			log.Printf("   关闭连接失败 (%s): %v\n", peerID.ShortString(), err)
		}
	}
}

// cleanupResources 清理所有资源
func cleanupResources() {
	// 清理 nonce 记录
	usedNoncesMutex.Lock()
	nonceCount := len(usedNonces)
	usedNonces = make(map[string]time.Time)
	usedNoncesMutex.Unlock()

	if nonceCount > 0 {
		fmt.Printf("   清理了 %d 个 nonce 记录\n", nonceCount)
	}

	// 清理公钥缓存
	peerPubKeysMutex.Lock()
	keyCount := len(peerPubKeys)
	peerPubKeys = make(map[peer.ID]*rsa.PublicKey)
	peerPubKeysMutex.Unlock()

	if keyCount > 0 {
		fmt.Printf("   清理了 %d 个公钥缓存\n", keyCount)
	}
}

func main() {
	// 解析命令行参数
	listenPort := flag.Int("port", 0, "监听端口（0表示随机）")
	targetPeer := flag.String("peer", "", "要连接的 peer 地址（格式：/ip4/127.0.0.1/tcp/端口/p2p/peerID）")
	registryAddr := flag.String("registry", "", "注册服务器地址（格式：127.0.0.1:8888）")
	username := flag.String("username", "", "用户名（用于注册）")
	flag.Parse()

	// Step 1: Generate a public/private key pair for the user
	privKey, pubKey, err := generateKeys()
	if err != nil {
		log.Fatal("生成密钥失败:", err)
	}
	_ = pubKey // 暂时不使用

	// Step 2: Initialize the P2P network
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

	// 设置全局变量，用于RPS自动回复
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	globalVarsMutex.Lock()
	globalHost = h
	globalPrivKey = privKey
	globalCtx = ctx
	globalVarsMutex.Unlock()

	// 设置流处理器来接收消息和交换公钥
	h.SetStreamHandler(protocolID, func(s network.Stream) {
		handleStream(s, privKey)
	})
	h.SetStreamHandler(keyExchangeID, func(s network.Stream) {
		handleKeyExchange(s, privKey, pubKey)
	})
	h.SetStreamHandler(fileTransferID, func(s network.Stream) {
		handleFileTransfer(s, privKey)
	})

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
		connectToPeer(h, *targetPeer, privKey, pubKey)
	}

	// 使用之前创建的上下文（如果已创建）
	if ctx == nil {
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
	}

	// 用于等待所有 goroutine 完成
	var wg sync.WaitGroup

	// 启动交互式输入
	chatDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		startChatWithPubKey(ctx, h, privKey, pubKey, registryClient, dhtDiscovery, *username, chatDone)
	}()

	// 等待中断信号或聊天结束
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		fmt.Println("\n🛑 收到关闭信号，开始优雅关闭...")
	case <-chatDone:
		fmt.Println("\n🛑 聊天已结束，开始优雅关闭...")
	}

	// 取消上下文，通知所有 goroutine 停止
	cancel()

	// 从注册服务器注销或关闭DHT（优先执行，确保及时更新）
	if registryClient != nil {
		fmt.Println("📝 正在从注册服务器注销...")
		// 使用 goroutine 确保不阻塞，但设置超时
		unregisterDone := make(chan error, 1)
		go func() {
			unregisterDone <- registryClient.Unregister()
		}()

		select {
		case err := <-unregisterDone:
			if err != nil {
				log.Printf("⚠️  注销失败: %v\n", err)
			} else {
				fmt.Println("✅ 已从注册服务器注销")
			}
		case <-time.After(2 * time.Second):
			fmt.Println("⚠️  注销超时，但注销请求已发送")
		}
	}

	// 关闭DHT发现服务
	if dhtDiscoveryRef != nil {
		// 在关闭DHT之前，先发送离线通知
		fmt.Println("📤 正在发送离线通知...")
		notifyCtx, notifyCancel := context.WithTimeout(context.Background(), 3*time.Second)
		sendOfflineNotification(notifyCtx, h, privKey, *username)
		notifyCancel()

		fmt.Println("🌐 正在关闭DHT发现服务...")
		if err := dhtDiscoveryRef.Close(); err != nil {
			log.Printf("⚠️  关闭DHT失败: %v\n", err)
		} else {
			fmt.Println("✅ DHT发现服务已关闭")
		}
	}

	// 优雅关闭所有连接
	fmt.Println("📡 正在关闭所有连接...")
	shutdownConnections(h)

	// 等待所有 goroutine 完成（最多等待 5 秒）
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("✅ 所有任务已完成")
	case <-time.After(5 * time.Second):
		fmt.Println("⚠️  等待超时，强制关闭")
	}

	// 清理资源
	fmt.Println("🧹 正在清理资源...")
	cleanupResources()

	fmt.Println("👋 程序已安全退出")
}

// 处理接收到的流
func handleStream(s network.Stream, privKey *rsa.PrivateKey) {
	defer s.Close()

	peerID := s.Conn().RemotePeer()
	fmt.Printf("\n📨 收到来自 %s 的消息:\n", peerID)

	// 设置读取超时
	s.SetReadDeadline(time.Now().Add(30 * time.Second))

	// 读取加密的消息
	reader := bufio.NewReader(s)
	encryptedMsg, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
			log.Printf("读取消息超时: %v\n", err)
		} else {
			log.Printf("读取消息失败: %v\n", err)
		}
		return
	}

	encryptedMsg = strings.TrimSpace(encryptedMsg)
	if encryptedMsg == "" {
		return
	}

	// 解析并验证安全消息
	decryptedMsg, verified, err := decryptAndVerifyMessage(encryptedMsg, privKey, peerID)
	if err != nil {
		fmt.Printf("🔒 加密消息: %s\n", encryptedMsg)
		fmt.Printf("⚠️  解密失败: %v\n", err)
	} else {
		// 检查是否是离线通知
		if strings.HasPrefix(decryptedMsg, "[系统通知]") {
			fmt.Printf("🔔 %s\n", decryptedMsg)
			if verified {
				fmt.Printf("✅ 离线通知已验证\n")
			}
		} else if strings.HasPrefix(decryptedMsg, "[RPS]") {
			// 处理石头剪刀布游戏消息（静默处理，不显示）
			if verified {
				// 需要获取host和context，但handleStream没有这些参数
				// 我们需要通过全局变量或其他方式传递
				handleRPSMessage(decryptedMsg, peerID)
			}
		} else {
			if verified {
				fmt.Printf("💬 消息内容: %s\n", decryptedMsg)
				fmt.Printf("✅ 消息已验证（签名有效，未检测到重放攻击）\n")
			} else {
				fmt.Printf("💬 消息内容: %s\n", decryptedMsg)
				fmt.Printf("⚠️  警告：消息验证失败（可能被篡改或重放）\n")
			}
		}
	}
	fmt.Print("\n> ")
}

// 处理公钥交换（作为服务器端，先接收对方的公钥，然后发送自己的）
func handleKeyExchange(s network.Stream, privKey *rsa.PrivateKey, pubKey rsa.PublicKey) {
	defer s.Close()

	peerID := s.Conn().RemotePeer()

	// 先接收对方的公钥
	decoder := gob.NewDecoder(s)
	var remotePubKey rsa.PublicKey
	if err := decoder.Decode(&remotePubKey); err != nil {
		log.Printf("接收公钥失败: %v\n", err)
		return
	}

	// 然后发送自己的公钥
	encoder := gob.NewEncoder(s)
	if err := encoder.Encode(pubKey); err != nil {
		log.Printf("发送公钥失败: %v\n", err)
		return
	}

	// 存储对方的公钥
	peerPubKeysMutex.Lock()
	peerPubKeys[peerID] = &remotePubKey
	peerPubKeysMutex.Unlock()

	fmt.Printf("✅ 已与 %s 交换公钥\n", peerID)
}

// 连接到指定的 peer
func connectToPeer(h host.Host, targetAddr string, privKey *rsa.PrivateKey, pubKey rsa.PublicKey) {
	maddr, err := multiaddr.NewMultiaddr(targetAddr)
	if err != nil {
		log.Fatal("解析地址失败:", err)
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		log.Fatal("解析 peer 信息失败:", err)
	}

	h.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)

	fmt.Printf("🔗 正在连接到 %s...\n", info.ID)

	ctx := context.Background()
	if err := h.Connect(ctx, *info); err != nil {
		log.Fatal("连接失败:", err)
	}

	fmt.Printf("✅ 已连接到 %s\n", info.ID)

	// 交换公钥
	stream, err := h.NewStream(ctx, info.ID, keyExchangeID)
	if err != nil {
		log.Fatal("创建密钥交换流失败:", err)
	}
	defer stream.Close()

	// 先发送自己的公钥
	encoder := gob.NewEncoder(stream)
	if err := encoder.Encode(pubKey); err != nil {
		log.Fatal("发送公钥失败:", err)
	}

	// 然后接收对方的公钥
	decoder := gob.NewDecoder(stream)
	var remotePubKey rsa.PublicKey
	if err := decoder.Decode(&remotePubKey); err != nil {
		log.Fatal("接收公钥失败:", err)
	}

	// 存储对方的公钥
	peerPubKeysMutex.Lock()
	peerPubKeys[info.ID] = &remotePubKey
	peerPubKeysMutex.Unlock()

	fmt.Printf("✅ 已与 %s 交换公钥\n\n", info.ID)
}

// 启动聊天输入循环
func startChatWithPubKey(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, pubKey rsa.PublicKey, registryClient *RegistryClient, dhtDiscovery *DHTDiscovery, myUsername string, done chan<- struct{}) {
	defer close(done)

	scanner := bufio.NewScanner(os.Stdin)

	// 使用 goroutine 监听上下文取消
	go func() {
		<-ctx.Done()
		// 当上下文取消时，尝试从 stdin 读取以退出阻塞的 Scan()
		os.Stdin.Close()
	}()

	fmt.Print("> ")

	for scanner.Scan() {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			fmt.Println("\n⚠️  正在关闭，停止接收新消息...")
			return
		default:
		}

		msg := strings.TrimSpace(scanner.Text())
		if msg == "" {
			fmt.Print("> ")
			continue
		}

		if msg == "/quit" || msg == "/exit" {
			fmt.Println("👋 正在退出...")
			return
		}

		// 处理帮助命令
		if msg == "/help" || msg == "/h" {
			showHelp(registryClient != nil, dhtDiscovery != nil)
			fmt.Print("> ")
			continue
		}

		// 处理文件发送命令
		if strings.HasPrefix(msg, "/sendfile ") || strings.HasPrefix(msg, "/file ") {
			filePath := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(msg, "/sendfile "), "/file "))
			if filePath == "" {
				fmt.Println("⚠️  用法: /sendfile <文件路径> 或 /file <文件路径>")
				fmt.Print("> ")
				continue
			}
			sendFileToPeers(ctx, h, privKey, filePath)
			fmt.Print("> ")
			continue
		}

		// 处理查询在线用户命令
		if msg == "/list" || msg == "/users" {
			if registryClient != nil {
				listOnlineUsers(registryClient)
			} else if dhtDiscovery != nil {
				// 在列出用户之前，先尝试发现网络中的用户
				// 对于每个已连接的peer，尝试通过DHT查找其用户信息
				conns := dhtDiscovery.host.Network().Conns()
				for _, conn := range conns {
					peerID := conn.RemotePeer()
					peerIDStr := peerID.String()

					// 检查是否已经知道这个peer的用户信息
					if dhtDiscovery.GetUserByPeerID(peerIDStr) == nil {
						// 尝试查找常见的用户名
						commonUsernames := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Frank", "Grace", "Henry"}
						for _, username := range commonUsernames {
							userInfo, err := dhtDiscovery.LookupUser(ctx, username)
							if err == nil && userInfo.PeerID == peerIDStr {
								// 找到了这个peer的用户信息
								break
							}
						}
					}
				}
				// 等待一小段时间让发现完成
				time.Sleep(500 * time.Millisecond)
				listDHTUsers(dhtDiscovery, ctx)
			} else {
				fmt.Println("⚠️  未启用用户发现功能")
				fmt.Println("   请使用 -registry 参数连接注册服务器，或使用DHT发现模式")
			}
			fmt.Print("> ")
			continue
		}

		// 处理call命令（支持 /call 和 call）
		if strings.HasPrefix(msg, "/call ") || strings.HasPrefix(msg, "call ") {
			var target string
			if strings.HasPrefix(msg, "/call ") {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "/call "))
			} else {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "call "))
			}
			if target == "" {
				fmt.Println("⚠️  用法: /call <用户名或节点ID> 或 call <用户名或节点ID>")
				fmt.Print("> ")
				continue
			}
			if registryClient != nil {
				callUser(ctx, h, privKey, pubKey, registryClient, target)
			} else if dhtDiscovery != nil {
				callUserViaDHT(ctx, h, privKey, pubKey, dhtDiscovery, target)
			} else {
				fmt.Println("⚠️  未启用用户发现功能")
				fmt.Println("   请使用 -registry 参数连接注册服务器，或使用DHT发现模式")
			}
			fmt.Print("> ")
			continue
		}

		// 处理挂断命令（支持 /hangup 和 /disconnect）
		if msg == "/hangup" || msg == "/disconnect" {
			// 没有参数，挂断所有连接
			hangupAllPeers(ctx, h, privKey, dhtDiscovery)
			fmt.Print("> ")
			continue
		}
		if strings.HasPrefix(msg, "/hangup ") || strings.HasPrefix(msg, "/disconnect ") {
			var target string
			if strings.HasPrefix(msg, "/hangup ") {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "/hangup "))
			} else {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "/disconnect "))
			}
			if target == "" {
				// 空参数也挂断所有连接
				hangupAllPeers(ctx, h, privKey, dhtDiscovery)
			} else {
				hangupPeer(ctx, h, privKey, target, dhtDiscovery)
			}
			fmt.Print("> ")
			continue
		}

		// 处理石头剪刀布游戏命令
		if msg == "/rps" || msg == "/rockpaperscissors" {
			playRockPaperScissors(ctx, h, privKey, myUsername, dhtDiscovery)
			fmt.Print("> ")
			continue
		}
		if strings.HasPrefix(msg, "/rps ") || strings.HasPrefix(msg, "/rockpaperscissors ") {
			fmt.Println("ℹ️  /rps 命令现在无需参数，系统会自动随机选择")
			playRockPaperScissors(ctx, h, privKey, myUsername, dhtDiscovery)
			fmt.Print("> ")
			continue
		}

		// 发送给所有已连接的 peer
		sent := false
		conns := h.Network().Conns()
		if len(conns) == 0 {
			fmt.Println("⚠️  当前没有已连接的 peer")
			fmt.Println("💡 提示：使用 /call <用户名> 或 call <用户名> 命令连接其他用户")
		}

		for _, conn := range conns {
			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return
			default:
			}

			peerID := conn.RemotePeer()

			// 检查连接状态
			if h.Network().Connectedness(peerID) != network.Connected {
				fmt.Printf("⚠️  %s 连接已断开，跳过\n", peerID.ShortString())
				continue
			}

			// 获取对方的公钥
			peerPubKeysMutex.RLock()
			remotePubKey, hasKey := peerPubKeys[peerID]
			peerPubKeysMutex.RUnlock()

			if !hasKey {
				fmt.Printf("⚠️  尚未与 %s 交换公钥，跳过\n", peerID)
				continue
			}

			// 使用对方的公钥加密消息并签名
			encryptedMsg, err := encryptAndSignMessage(msg, privKey, remotePubKey)
			if err != nil {
				log.Printf("加密失败 (%s): %v\n", peerID, err)
				continue
			}

			// 使用带超时的上下文创建流
			streamCtx, streamCancel := context.WithTimeout(ctx, 5*time.Second)
			stream, err := h.NewStream(streamCtx, peerID, protocolID)
			streamCancel()

			if err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					return
				}
				log.Printf("创建流失败 (%s): %v\n", peerID, err)
				continue
			}

			// 使用带超时的写入
			writeDone := make(chan error, 1)
			go func() {
				_, err := stream.Write([]byte(encryptedMsg + "\n"))
				writeDone <- err
			}()

			select {
			case err := <-writeDone:
				if err != nil {
					log.Printf("发送消息失败 (%s): %v\n", peerID, err)
					stream.Close()
					continue
				}
				fmt.Printf("📤 已发送加密消息给 %s\n", peerID)
				stream.Close()
				sent = true
			case <-ctx.Done():
				stream.Close()
				return
			case <-time.After(5 * time.Second):
				log.Printf("发送消息超时 (%s)\n", peerID)
				stream.Close()
				continue
			}
		}

		if !sent {
			fmt.Println("⚠️  没有已连接的 peer，无法发送消息")
		}

		fmt.Print("> ")
	}

	// 处理扫描错误
	if err := scanner.Err(); err != nil {
		if err != io.EOF {
			log.Printf("读取输入错误: %v\n", err)
		}
	}
}

// Generates a RSA public/private key pair
func generateKeys() (*rsa.PrivateKey, rsa.PublicKey, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, rsa.PublicKey{}, err
	}
	pubKey := privKey.PublicKey
	return privKey, pubKey, nil
}

// encryptAndSignMessage 加密消息并添加数字签名
func encryptAndSignMessage(msg string, senderPrivKey *rsa.PrivateKey, recipientPubKey *rsa.PublicKey) (string, error) {
	// 1. 生成随机 nonce（防止重放攻击）
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成 nonce 失败: %v", err)
	}

	// 2. 创建消息数据（包含原始消息、时间戳和 nonce）
	msgData := struct {
		Message   string `json:"message"`
		Timestamp int64  `json:"timestamp"`
		Nonce     []byte `json:"nonce"`
	}{
		Message:   msg,
		Timestamp: time.Now().Unix(),
		Nonce:     nonce,
	}

	msgJSON, err := json.Marshal(msgData)
	if err != nil {
		return "", fmt.Errorf("序列化消息失败: %v", err)
	}

	// 3. 使用发送方私钥对消息进行数字签名
	hash := sha256.Sum256(msgJSON)
	signature, err := rsa.SignPKCS1v15(rand.Reader, senderPrivKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("签名失败: %v", err)
	}

	// 4. 使用接收方公钥加密消息（AES + RSA）
	encryptedData, err := encryptMessageWithPubKey(msgJSON, recipientPubKey)
	if err != nil {
		return "", fmt.Errorf("加密失败: %v", err)
	}

	// 5. 创建安全消息结构
	secureMsg := SecureMessage{
		EncryptedData: encryptedData,
		Signature:     signature,
		Timestamp:     msgData.Timestamp,
		Nonce:         nonce,
	}

	// 6. 序列化为 JSON 并 base64 编码
	secureMsgJSON, err := json.Marshal(secureMsg)
	if err != nil {
		return "", fmt.Errorf("序列化安全消息失败: %v", err)
	}

	return base64.StdEncoding.EncodeToString(secureMsgJSON), nil
}

// Encrypts a message using AES and RSA with the recipient's public key
func encryptMessageWithPubKey(msg []byte, pubKey *rsa.PublicKey) ([]byte, error) {
	// Generate a random AES key for encryption
	aesKey := make([]byte, 32) // 256-bit key
	_, err := rand.Read(aesKey)
	if err != nil {
		return nil, err
	}

	// Encrypt the message with AES
	cipherText, err := aesEncrypt(msg, aesKey)
	if err != nil {
		return nil, err
	}

	// Encrypt the AES key with RSA using the recipient's public key
	encryptedAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, aesKey, nil)
	if err != nil {
		return nil, err
	}

	// Combine the encrypted AES key and the message ciphertext
	encryptedMessage := append(encryptedAESKey, cipherText...)
	return encryptedMessage, nil
}

// decryptAndVerifyMessage 解密消息并验证签名和重放攻击
func decryptAndVerifyMessage(encryptedMsg string, recipientPrivKey *rsa.PrivateKey, senderID peer.ID) (string, bool, error) {
	// 1. 解码 base64
	secureMsgJSON, err := base64.StdEncoding.DecodeString(encryptedMsg)
	if err != nil {
		return "", false, fmt.Errorf("解码 base64 失败: %v", err)
	}

	// 2. 解析安全消息结构
	var secureMsg SecureMessage
	if err := json.Unmarshal(secureMsgJSON, &secureMsg); err != nil {
		return "", false, fmt.Errorf("解析消息结构失败: %v", err)
	}

	// 3. 检查时间戳（防止过期消息）
	msgTime := time.Unix(secureMsg.Timestamp, 0)
	if time.Since(msgTime) > maxMessageAge {
		return "", false, fmt.Errorf("消息已过期（超过 %v）", maxMessageAge)
	}

	// 4. 检查 nonce（防止重放攻击）
	nonceKey := base64.StdEncoding.EncodeToString(secureMsg.Nonce)
	usedNoncesMutex.Lock()
	if usedTime, exists := usedNonces[nonceKey]; exists {
		usedNoncesMutex.Unlock()
		return "", false, fmt.Errorf("检测到重放攻击（nonce 已使用于 %v）", usedTime)
	}
	usedNonces[nonceKey] = msgTime
	usedNoncesMutex.Unlock()

	// 5. 解密消息数据
	decryptedData, err := decryptMessage(secureMsg.EncryptedData, recipientPrivKey)
	if err != nil {
		return "", false, fmt.Errorf("解密失败: %v", err)
	}

	// 6. 解析解密后的消息数据
	var msgData struct {
		Message   string `json:"message"`
		Timestamp int64  `json:"timestamp"`
		Nonce     []byte `json:"nonce"`
	}
	if err := json.Unmarshal(decryptedData, &msgData); err != nil {
		return "", false, fmt.Errorf("解析消息数据失败: %v", err)
	}

	// 7. 验证 nonce 匹配
	if !bytes.Equal(msgData.Nonce, secureMsg.Nonce) {
		return "", false, fmt.Errorf("nonce 不匹配")
	}

	// 8. 验证数字签名
	peerPubKeysMutex.RLock()
	senderPubKey, hasKey := peerPubKeys[senderID]
	peerPubKeysMutex.RUnlock()

	if !hasKey {
		return msgData.Message, false, fmt.Errorf("未找到发送方公钥，无法验证签名")
	}

	// 重新计算消息哈希
	msgJSON, err := json.Marshal(msgData)
	if err != nil {
		return msgData.Message, false, fmt.Errorf("序列化消息失败: %v", err)
	}

	hash := sha256.Sum256(msgJSON)
	err = rsa.VerifyPKCS1v15(senderPubKey, crypto.SHA256, hash[:], secureMsg.Signature)
	verified := err == nil

	return msgData.Message, verified, nil
}

// Decrypts an AES-encrypted message using RSA
func decryptMessage(encryptedData []byte, privKey *rsa.PrivateKey) ([]byte, error) {
	if len(encryptedData) < 256 {
		return nil, fmt.Errorf("加密数据太短")
	}

	// Extract encrypted AES key and the message ciphertext
	encryptedAESKey := encryptedData[:256] // RSA-encrypted AES key
	cipherText := encryptedData[256:]

	// Decrypt the AES key using RSA
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, encryptedAESKey, nil)
	if err != nil {
		return nil, err
	}

	// Decrypt the message using AES
	decryptedMessage, err := aesDecrypt(cipherText, aesKey)
	if err != nil {
		return nil, err
	}

	return decryptedMessage, nil
}

// AES encryption
func aesEncrypt(msg []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	ciphertext := make([]byte, aes.BlockSize+len(msg))
	iv := ciphertext[:aes.BlockSize]
	_, err = rand.Read(iv)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], msg)
	return ciphertext, nil
}

// AES decryption
func aesDecrypt(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]
	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)
	return ciphertext, nil
}

// sendFileToPeers 发送文件给所有已连接的 peer
func sendFileToPeers(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, filePath string) {
	// 检查文件是否存在
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		fmt.Printf("❌ 文件不存在或无法访问: %v\n", err)
		return
	}

	// 检查文件大小
	if fileInfo.Size() > maxFileSize {
		fmt.Printf("❌ 文件太大（最大 %d MB）\n", maxFileSize/(1024*1024))
		return
	}

	// 读取文件
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("❌ 无法打开文件: %v\n", err)
		return
	}
	defer file.Close()

	// 读取文件内容
	fileData := make([]byte, fileInfo.Size())
	if _, err := io.ReadFull(file, fileData); err != nil {
		fmt.Printf("❌ 读取文件失败: %v\n", err)
		return
	}

	// 计算文件哈希
	fileHash := sha256.Sum256(fileData)
	fileName := fileInfo.Name()

	// 计算分块数量
	chunkCount := int((fileInfo.Size() + fileChunkSize - 1) / fileChunkSize)

	fmt.Printf("📁 准备发送文件: %s (%.2f MB, %d 块)\n", fileName, float64(fileInfo.Size())/(1024*1024), chunkCount)

	// 发送给所有已连接的 peer
	sent := false
	for _, conn := range h.Network().Conns() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		peerID := conn.RemotePeer()

		// 获取对方的公钥
		peerPubKeysMutex.RLock()
		remotePubKey, hasKey := peerPubKeys[peerID]
		peerPubKeysMutex.RUnlock()

		if !hasKey {
			fmt.Printf("⚠️  尚未与 %s 交换公钥，跳过\n", peerID.ShortString())
			continue
		}

		// 发送文件
		if err := sendFile(ctx, h, peerID, privKey, remotePubKey, fileName, fileData, fileHash[:], chunkCount); err != nil {
			fmt.Printf("❌ 发送文件失败 (%s): %v\n", peerID.ShortString(), err)
			continue
		}

		fmt.Printf("✅ 文件已发送给 %s\n", peerID.ShortString())
		sent = true
	}

	if !sent {
		fmt.Println("⚠️  没有已连接的 peer，无法发送文件")
	}
}

// sendFile 发送文件给指定的 peer
func sendFile(ctx context.Context, h host.Host, peerID peer.ID, senderPrivKey *rsa.PrivateKey, recipientPubKey *rsa.PublicKey, fileName string, fileData []byte, fileHash []byte, chunkCount int) error {
	// 创建文件传输流
	streamCtx, streamCancel := context.WithTimeout(ctx, 30*time.Second)
	defer streamCancel()

	stream, err := h.NewStream(streamCtx, peerID, fileTransferID)
	if err != nil {
		return fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	// 设置写入超时
	stream.SetWriteDeadline(time.Now().Add(60 * time.Second))

	encoder := json.NewEncoder(stream)

	// 1. 发送文件头部
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("生成 nonce 失败: %v", err)
	}

	header := FileTransferHeader{
		FileName:   fileName,
		FileSize:   int64(len(fileData)),
		ChunkCount: chunkCount,
		FileHash:   fileHash,
		Timestamp:  time.Now().Unix(),
		Nonce:      nonce,
	}

	// 签名头部
	headerJSON, _ := json.Marshal(header)
	headerHash := sha256.Sum256(headerJSON)
	header.Signature, err = rsa.SignPKCS1v15(rand.Reader, senderPrivKey, crypto.SHA256, headerHash[:])
	if err != nil {
		return fmt.Errorf("签名失败: %v", err)
	}

	if err := encoder.Encode(header); err != nil {
		return fmt.Errorf("发送头部失败: %v", err)
	}

	// 2. 分块发送文件数据
	for i := 0; i < chunkCount; i++ {
		select {
		case <-ctx.Done():
			return context.Canceled
		default:
		}

		start := i * fileChunkSize
		end := start + fileChunkSize
		if end > len(fileData) {
			end = len(fileData)
		}

		chunkData := fileData[start:end]

		// 加密分块数据
		encryptedChunk, err := encryptMessageWithPubKey(chunkData, recipientPubKey)
		if err != nil {
			return fmt.Errorf("加密分块失败: %v", err)
		}

		// 签名分块
		chunkHash := sha256.Sum256(chunkData)
		chunkSignature, err := rsa.SignPKCS1v15(rand.Reader, senderPrivKey, crypto.SHA256, chunkHash[:])
		if err != nil {
			return fmt.Errorf("签名分块失败: %v", err)
		}

		chunk := FileChunk{
			ChunkIndex: i,
			Data:       encryptedChunk,
			Signature:  chunkSignature,
		}

		if err := encoder.Encode(chunk); err != nil {
			return fmt.Errorf("发送分块失败: %v", err)
		}

		// 显示进度
		progress := float64(i+1) * 100 / float64(chunkCount)
		fmt.Printf("\r   进度: %.1f%% (%d/%d)", progress, i+1, chunkCount)
	}
	fmt.Println() // 换行

	return nil
}

// handleFileTransfer 处理接收到的文件传输
func handleFileTransfer(s network.Stream, privKey *rsa.PrivateKey) {
	defer s.Close()

	peerID := s.Conn().RemotePeer()
	fmt.Printf("\n📁 收到来自 %s 的文件传输请求\n", peerID.ShortString())

	// 设置读取超时
	s.SetReadDeadline(time.Now().Add(5 * time.Minute))

	decoder := json.NewDecoder(s)

	// 1. 接收文件头部
	var header FileTransferHeader
	if err := decoder.Decode(&header); err != nil {
		fmt.Printf("❌ 接收文件头部失败: %v\n", err)
		return
	}

	// 验证头部签名
	peerPubKeysMutex.RLock()
	senderPubKey, hasKey := peerPubKeys[peerID]
	peerPubKeysMutex.RUnlock()

	if !hasKey {
		fmt.Printf("❌ 未找到发送方公钥，无法验证签名\n")
		return
	}

	// 验证签名（签名是对不包含签名字段的头部进行签名的）
	headerCopy := header
	headerCopy.Signature = nil
	headerJSON, _ := json.Marshal(headerCopy)
	headerHash := sha256.Sum256(headerJSON)

	if err := rsa.VerifyPKCS1v15(senderPubKey, crypto.SHA256, headerHash[:], header.Signature); err != nil {
		fmt.Printf("⚠️  文件头部签名验证失败\n")
		return
	}

	fmt.Printf("   文件名: %s\n", header.FileName)
	fmt.Printf("   文件大小: %.2f MB\n", float64(header.FileSize)/(1024*1024))
	fmt.Printf("   分块数量: %d\n", header.ChunkCount)

	// 2. 接收文件分块
	fileData := make([]byte, 0, header.FileSize)
	receivedChunks := make(map[int][]byte)

	for i := 0; i < header.ChunkCount; i++ {
		var chunk FileChunk
		if err := decoder.Decode(&chunk); err != nil {
			fmt.Printf("❌ 接收分块失败: %v\n", err)
			return
		}

		// 解密分块（chunk.Data 是字节数组，不是 base64 字符串）
		decryptedChunk, err := decryptMessage(chunk.Data, privKey)
		if err != nil {
			fmt.Printf("❌ 解密分块失败: %v\n", err)
			return
		}

		// 验证分块签名
		chunkHash := sha256.Sum256(decryptedChunk)
		if err := rsa.VerifyPKCS1v15(senderPubKey, crypto.SHA256, chunkHash[:], chunk.Signature); err != nil {
			fmt.Printf("⚠️  分块 %d 签名验证失败\n", chunk.ChunkIndex)
			return
		}

		receivedChunks[chunk.ChunkIndex] = decryptedChunk
		fmt.Printf("\r   接收进度: %d/%d", i+1, header.ChunkCount)
	}
	fmt.Println()

	// 3. 重组文件
	for i := 0; i < header.ChunkCount; i++ {
		chunk, exists := receivedChunks[i]
		if !exists {
			fmt.Printf("❌ 缺少分块 %d\n", i)
			return
		}
		fileData = append(fileData, chunk...)
	}

	// 4. 验证文件哈希
	receivedHash := sha256.Sum256(fileData)
	if !bytes.Equal(receivedHash[:], header.FileHash) {
		fmt.Printf("❌ 文件哈希验证失败，文件可能已损坏\n")
		return
	}

	// 5. 保存文件
	// 创建接收目录
	receiveDir := "received_files"
	if err := os.MkdirAll(receiveDir, 0755); err != nil {
		fmt.Printf("❌ 创建接收目录失败: %v\n", err)
		return
	}

	// 生成唯一文件名（避免覆盖）
	timestamp := time.Now().Format("20060102_150405")
	savePath := fmt.Sprintf("%s/%s_%s", receiveDir, timestamp, header.FileName)

	if err := os.WriteFile(savePath, fileData, 0644); err != nil {
		fmt.Printf("❌ 保存文件失败: %v\n", err)
		return
	}

	fmt.Printf("✅ 文件已保存到: %s\n", savePath)
	fmt.Printf("✅ 文件已验证（签名和哈希都有效）\n")
	fmt.Print("\n> ")
}

// listOnlineUsers 列出在线用户
func listOnlineUsers(registryClient *RegistryClient) {
	clients, err := registryClient.ListClients()
	if err != nil {
		fmt.Printf("❌ 获取在线用户列表失败: %v\n", err)
		return
	}

	if len(clients) == 0 {
		fmt.Println("📋 当前没有在线用户")
		return
	}

	fmt.Printf("📋 在线用户列表 (%d 人):\n", len(clients))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	for i, client := range clients {
		lastSeen := time.Since(client.LastSeen)
		fmt.Printf("%d. 用户名: %s\n", i+1, client.Username)
		fmt.Printf("   节点ID: %s\n", client.PeerID)
		fmt.Printf("   最后活跃: %s前\n", formatDuration(lastSeen))
		if len(client.Addresses) > 0 {
			fmt.Printf("   地址: %s\n", client.Addresses[0])
		}
		fmt.Println()
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// callUser 呼叫用户并建立连接
func callUser(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, pubKey rsa.PublicKey, registryClient *RegistryClient, targetID string) {
	fmt.Printf("🔍 正在查找用户: %s\n", targetID)

	// 查找用户
	clientInfo, err := registryClient.LookupClient(targetID)
	if err != nil {
		fmt.Printf("❌ 未找到用户: %v\n", err)
		return
	}

	fmt.Printf("✅ 找到用户: %s (节点ID: %s)\n", clientInfo.Username, clientInfo.PeerID)

	// 解析节点地址
	if len(clientInfo.Addresses) == 0 {
		fmt.Println("❌ 用户没有可用地址")
		return
	}

	// 尝试连接每个地址
	var connected bool
	for _, addrStr := range clientInfo.Addresses {
		fmt.Printf("🔗 尝试连接: %s\n", addrStr)

		maddr, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			fmt.Printf("⚠️  解析地址失败: %v\n", err)
			continue
		}

		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			fmt.Printf("⚠️  解析peer信息失败: %v\n", err)
			continue
		}

		// 连接到peer
		h.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)

		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := h.Connect(connectCtx, *info); err != nil {
			cancel()
			fmt.Printf("⚠️  连接失败: %v\n", err)
			continue
		}
		cancel()

		fmt.Printf("✅ 已连接到 %s\n", info.ID)

		// 交换公钥
		streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
		stream, err := h.NewStream(streamCtx, info.ID, keyExchangeID)
		streamCancel()

		if err != nil {
			fmt.Printf("⚠️  创建密钥交换流失败: %v\n", err)
			continue
		}
		defer stream.Close()

		// 先发送自己的公钥
		encoder := gob.NewEncoder(stream)
		if err := encoder.Encode(pubKey); err != nil {
			fmt.Printf("⚠️  发送公钥失败: %v\n", err)
			continue
		}

		// 然后接收对方的公钥
		decoder := gob.NewDecoder(stream)
		var remotePubKey rsa.PublicKey
		if err := decoder.Decode(&remotePubKey); err != nil {
			fmt.Printf("⚠️  接收公钥失败: %v\n", err)
			continue
		}

		// 存储对方的公钥
		peerPubKeysMutex.Lock()
		peerPubKeys[info.ID] = &remotePubKey
		peerPubKeysMutex.Unlock()

		fmt.Printf("✅ 已与 %s (%s) 交换公钥，可以开始聊天了！\n\n", clientInfo.Username, info.ID.ShortString())
		connected = true

		// 验证连接状态
		if h.Network().Connectedness(info.ID) == network.Connected {
			fmt.Printf("✅ 连接状态确认：已连接到 %s\n", clientInfo.Username)
		} else {
			fmt.Printf("⚠️  警告：连接状态异常\n")
		}
		break
	}

	if !connected {
		fmt.Println("❌ 无法连接到目标用户")
		fmt.Println("💡 提示：请确保目标用户在线，并且网络可达")
	}
}

// hangupPeer 挂断与指定peer的连接
func hangupPeer(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, targetID string, dhtDiscovery *DHTDiscovery) {
	fmt.Printf("📞 正在挂断与 %s 的连接...\n", targetID)

	// 查找目标peer
	conns := h.Network().Conns()
	var targetPeerID peer.ID
	var targetUserInfo *UserInfo
	var found bool

	// 尝试解析为peerID
	if parsedPeerID, err := peer.Decode(targetID); err == nil {
		// 是peerID，检查是否已连接
		for _, conn := range conns {
			if conn.RemotePeer() == parsedPeerID {
				targetPeerID = parsedPeerID
				// 尝试从DHT获取用户信息
				if dhtDiscovery != nil {
					if userInfo := dhtDiscovery.GetUserByPeerID(parsedPeerID.String()); userInfo != nil {
						targetUserInfo = userInfo
					}
				}
				found = true
				break
			}
		}
	}

	// 如果不是peerID或未找到，尝试通过用户名匹配（大小写不敏感）
	if !found && dhtDiscovery != nil {
		targetLower := strings.ToLower(targetID)
		for _, conn := range conns {
			peerID := conn.RemotePeer()
			// 尝试从DHT获取用户信息
			userInfo := dhtDiscovery.GetUserByPeerID(peerID.String())
			if userInfo != nil && strings.ToLower(userInfo.Username) == targetLower {
				targetPeerID = peerID
				targetUserInfo = userInfo
				found = true
				break
			}
		}
	}

	if !found {
		fmt.Printf("❌ 未找到已连接的用户: %s\n", targetID)
		fmt.Println("💡 提示：请使用 /list 查看已连接的用户")
		return
	}

	// 显示用户信息
	if targetUserInfo != nil {
		fmt.Printf("   用户: %s (节点ID: %s)\n", targetUserInfo.Username, targetPeerID.ShortString())
	} else {
		fmt.Printf("   节点ID: %s\n", targetPeerID.ShortString())
	}

	// 可选：发送断开连接通知（如果已交换公钥）
	peerPubKeysMutex.RLock()
	_, hasKey := peerPubKeys[targetPeerID]
	peerPubKeysMutex.RUnlock()

	if hasKey {
		// 尝试发送断开连接通知
		notifyCtx, notifyCancel := context.WithTimeout(ctx, 2*time.Second)
		stream, err := h.NewStream(notifyCtx, targetPeerID, protocolID)
		notifyCancel()

		if err == nil {
			// 创建断开连接通知消息
			disconnectMsg := fmt.Sprintf("[系统通知] 连接已断开")
			peerPubKeysMutex.RLock()
			remotePubKey := peerPubKeys[targetPeerID]
			peerPubKeysMutex.RUnlock()

			if remotePubKey != nil {
				encryptedMsg, err := encryptAndSignMessage(disconnectMsg, privKey, remotePubKey)
				if err == nil {
					stream.Write([]byte(encryptedMsg + "\n"))
				}
			}
			stream.Close()
		}
	}

	// 关闭所有与该peer的连接
	closedCount := 0
	for _, conn := range conns {
		if conn.RemotePeer() == targetPeerID {
			if err := conn.Close(); err != nil {
				log.Printf("   关闭连接失败: %v\n", err)
			} else {
				closedCount++
			}
		}
	}

	// 清理公钥缓存
	peerPubKeysMutex.Lock()
	if _, exists := peerPubKeys[targetPeerID]; exists {
		delete(peerPubKeys, targetPeerID)
		fmt.Println("   ✅ 已清理公钥缓存")
	}
	peerPubKeysMutex.Unlock()

	if closedCount > 0 {
		if targetUserInfo != nil {
			fmt.Printf("✅ 已断开与 %s (%s) 的连接\n", targetUserInfo.Username, targetPeerID.ShortString())
		} else {
			fmt.Printf("✅ 已断开与 %s 的连接\n", targetPeerID.ShortString())
		}
	} else {
		fmt.Printf("⚠️  未找到与 %s 的活跃连接\n", targetPeerID.ShortString())
	}
}

// formatDuration 格式化时间间隔
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d秒", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%d分钟", int(d.Minutes()))
	} else {
		return fmt.Sprintf("%d小时", int(d.Hours()))
	}
}

// showHelp 显示帮助信息
func showHelp(hasRegistry bool, hasDHT bool) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📖 PChat 命令帮助")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("💬 基本命令:")
	fmt.Println("  直接输入文本消息                    - 发送消息给所有已连接的peer")
	fmt.Println()
	fmt.Println("📋 用户发现命令:")
	if hasRegistry {
		fmt.Println("  /list 或 /users                    - 查看注册服务器上的在线用户列表")
	} else if hasDHT {
		fmt.Println("  /list 或 /users                    - 查看DHT发现的用户和已连接的节点")
	} else {
		fmt.Println("  /list 或 /users                    - 查看在线用户（需要启用用户发现功能）")
	}
	fmt.Println()
	fmt.Println("📞 连接命令:")
	if hasRegistry || hasDHT {
		fmt.Println("  /call <用户名> 或 call <用户名>    - 通过用户名连接用户")
		fmt.Println("  /call <节点ID> 或 call <节点ID>    - 通过节点ID连接用户")
		fmt.Println("  /hangup 或 /disconnect              - 挂断所有已连接的用户")
		fmt.Println("  /hangup <用户名或节点ID> 或 /disconnect <用户名或节点ID>")
		fmt.Println("                                    - 断开与指定用户的连接")
	} else {
		fmt.Println("  /call <用户名> 或 call <用户名>    - 连接用户（需要启用用户发现功能）")
		fmt.Println("  /hangup <用户名或节点ID> 或 /disconnect <用户名或节点ID>")
		fmt.Println("                                    - 断开与指定用户的连接")
	}
	fmt.Println()
	fmt.Println("📁 文件传输命令:")
	fmt.Println("  /sendfile <文件路径>                 - 发送文件给所有已连接的peer")
	fmt.Println("  /file <文件路径>                    - 发送文件（简写形式）")
	fmt.Println()
	fmt.Println("🎮 娱乐命令:")
	fmt.Println("  /rps                                - 发起石头剪刀布，所有人自动随机出拳")
	fmt.Println()
	fmt.Println("❓ 帮助命令:")
	fmt.Println("  /help 或 /h                         - 显示此帮助信息")
	fmt.Println()
	fmt.Println("🚪 退出命令:")
	fmt.Println("  /quit 或 /exit                      - 优雅退出程序")
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if hasDHT {
		fmt.Println("💡 提示：当前使用DHT去中心化发现模式")
		fmt.Println("   - DHT发现需要一些时间来连接网络中的其他节点")
		fmt.Println("   - 用户信息会自动发现，无需手动call")
	} else if hasRegistry {
		fmt.Println("💡 提示：当前使用注册服务器模式")
		fmt.Println("   - 用户信息由注册服务器管理")
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// listDHTUsers 列出DHT发现的用户和已连接的peer
func listDHTUsers(dhtDiscovery *DHTDiscovery, ctx context.Context) {
	users := dhtDiscovery.ListUsers()
	conns := dhtDiscovery.host.Network().Conns()
	currentPeerID := dhtDiscovery.host.ID().String()

	// 创建一个映射，将节点ID映射到用户信息
	userMap := make(map[string]*UserInfo)
	for _, user := range users {
		userMap[user.PeerID] = user
	}

	// 显示当前用户自己
	if currentUserInfo, found := userMap[currentPeerID]; found {
		fmt.Printf("📋 当前用户:\n")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("   用户名: %s\n", currentUserInfo.Username)
		fmt.Printf("   节点ID: %s\n", currentUserInfo.PeerID)
		if len(currentUserInfo.Addresses) > 0 {
			fmt.Printf("   地址: %s\n", currentUserInfo.Addresses[0])
		}
		fmt.Println()
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	}

	// 显示已连接的peer，尝试显示用户名
	if len(conns) > 0 {
		fmt.Printf("📋 已连接的节点 (%d 个):\n", len(conns))
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		for i, conn := range conns {
			peerID := conn.RemotePeer()
			peerIDStr := peerID.String()

			// 尝试从DHT发现的用户中查找用户名
			var username string
			if userInfo, found := userMap[peerIDStr]; found {
				username = userInfo.Username
			} else if dhtDiscovery != nil {
				// 尝试通过DHT查找
				if userInfo := dhtDiscovery.GetUserByPeerID(peerIDStr); userInfo != nil {
					username = userInfo.Username
				}
			}
			// 如果还是找不到，使用节点ID的短格式
			if username == "" {
				username = peerID.ShortString()
			}

			fmt.Printf("%d. 用户名: %s\n", i+1, username)
			fmt.Printf("   节点ID: %s\n", peerID)
			fmt.Printf("   地址: %s\n", conn.RemoteMultiaddr())
			fmt.Println()
		}
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	}

	// 显示DHT发现的用户（不包括已连接的和自己）
	if len(users) > 0 {
		connectedPeerIDs := make(map[string]bool)
		for _, conn := range conns {
			connectedPeerIDs[conn.RemotePeer().String()] = true
		}
		connectedPeerIDs[currentPeerID] = true // 排除自己

		discoveredUsers := make([]*UserInfo, 0)
		for _, user := range users {
			if !connectedPeerIDs[user.PeerID] {
				discoveredUsers = append(discoveredUsers, user)
			}
		}

		if len(discoveredUsers) > 0 {
			fmt.Printf("📋 DHT发现的用户 (%d 人):\n", len(discoveredUsers))
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			for i, user := range discoveredUsers {
				lastSeen := time.Since(time.Unix(user.Timestamp, 0))
				fmt.Printf("%d. 用户名: %s\n", i+1, user.Username)
				fmt.Printf("   节点ID: %s\n", user.PeerID)
				fmt.Printf("   最后更新: %s前\n", formatDuration(lastSeen))
				if len(user.Addresses) > 0 {
					fmt.Printf("   地址: %s\n", user.Addresses[0])
				}
				fmt.Println()
			}
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		}
	}

	if len(conns) == 0 && len(users) == 0 {
		fmt.Println("📋 当前没有已连接的节点或发现的用户")
		fmt.Println("💡 提示：DHT发现需要一些时间来连接网络，请稍后再试")
		fmt.Println("   或者连接到其他节点以加入DHT网络")
	}
}

// callUserViaDHT 通过DHT呼叫用户
func callUserViaDHT(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, pubKey rsa.PublicKey, dhtDiscovery *DHTDiscovery, targetID string) {
	fmt.Printf("🔍 正在查找用户: %s\n", targetID)

	// 首先检查已连接的peer
	conns := h.Network().Conns()
	var targetPeerID peer.ID
	var targetUserInfo *UserInfo
	var found bool

	// 尝试解析为peerID
	if parsedPeerID, err := peer.Decode(targetID); err == nil {
		// 是peerID，检查是否已连接
		for _, conn := range conns {
			if conn.RemotePeer() == parsedPeerID {
				targetPeerID = parsedPeerID
				// 尝试从DHT获取用户信息
				if userInfo := dhtDiscovery.GetUserByPeerID(parsedPeerID.String()); userInfo != nil {
					targetUserInfo = userInfo
				}
				found = true
				break
			}
		}
	}

	// 如果不是peerID或未找到，尝试通过用户名匹配（大小写不敏感）
	if !found {
		targetLower := strings.ToLower(targetID)
		for _, conn := range conns {
			peerID := conn.RemotePeer()
			// 尝试从DHT获取用户信息
			userInfo := dhtDiscovery.GetUserByPeerID(peerID.String())
			if userInfo != nil && strings.ToLower(userInfo.Username) == targetLower {
				targetPeerID = peerID
				targetUserInfo = userInfo
				found = true
				break
			}
		}
	}

	// 如果已连接，直接进行公钥交换
	if found && targetPeerID != "" {
		// 检查是否已经交换过公钥
		peerPubKeysMutex.RLock()
		_, hasKey := peerPubKeys[targetPeerID]
		peerPubKeysMutex.RUnlock()

		if hasKey {
			if targetUserInfo != nil {
				fmt.Printf("✅ 已与 %s (%s) 连接并交换公钥，可以开始聊天了！\n\n", targetUserInfo.Username, targetPeerID.ShortString())
			} else {
				fmt.Printf("✅ 已与 %s 连接并交换公钥，可以开始聊天了！\n\n", targetPeerID.ShortString())
			}
			return
		}

		// 进行公钥交换
		if targetUserInfo != nil {
			fmt.Printf("✅ 找到已连接的用户: %s (节点ID: %s)\n", targetUserInfo.Username, targetPeerID.ShortString())
		} else {
			fmt.Printf("✅ 找到已连接的节点: %s\n", targetPeerID.ShortString())
		}
		fmt.Println("🔑 正在交换公钥...")

		streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
		stream, err := h.NewStream(streamCtx, targetPeerID, keyExchangeID)
		streamCancel()

		if err != nil {
			fmt.Printf("⚠️  创建密钥交换流失败: %v\n", err)
			return
		}
		defer stream.Close()

		// 先发送自己的公钥
		encoder := gob.NewEncoder(stream)
		if err := encoder.Encode(pubKey); err != nil {
			fmt.Printf("⚠️  发送公钥失败: %v\n", err)
			return
		}

		// 然后接收对方的公钥
		decoder := gob.NewDecoder(stream)
		var remotePubKey rsa.PublicKey
		if err := decoder.Decode(&remotePubKey); err != nil {
			fmt.Printf("⚠️  接收公钥失败: %v\n", err)
			return
		}

		// 存储对方的公钥
		peerPubKeysMutex.Lock()
		peerPubKeys[targetPeerID] = &remotePubKey
		peerPubKeysMutex.Unlock()

		if targetUserInfo != nil {
			fmt.Printf("✅ 已与 %s (%s) 交换公钥，可以开始聊天了！\n\n", targetUserInfo.Username, targetPeerID.ShortString())
		} else {
			fmt.Printf("✅ 已与 %s 交换公钥，可以开始聊天了！\n\n", targetPeerID.ShortString())
		}
		return
	}

	// 如果未连接，通过DHT查找用户（尝试大小写不敏感）
	var userInfo *UserInfo
	var err error

	// 先尝试原始用户名
	userInfo, err = dhtDiscovery.LookupUser(ctx, targetID)
	if err != nil {
		// 如果失败，尝试首字母大写
		if len(targetID) > 0 {
			capitalized := strings.ToUpper(targetID[:1]) + strings.ToLower(targetID[1:])
			if capitalized != targetID {
				userInfo, err = dhtDiscovery.LookupUser(ctx, capitalized)
			}
		}
	}

	if err != nil {
		fmt.Printf("❌ 未找到用户: %v\n", err)
		fmt.Println("💡 提示：")
		fmt.Println("   1. 用户可能未在线或未连接到DHT网络")
		fmt.Println("   2. DHT查找可能需要一些时间，请稍后再试")
		fmt.Println("   3. 如果用户已连接，请使用 /list 查看已连接的用户")
		return
	}

	fmt.Printf("✅ 找到用户: %s (节点ID: %s)\n", userInfo.Username, userInfo.PeerID)

	// 解析节点地址
	if len(userInfo.Addresses) == 0 {
		fmt.Println("❌ 用户没有可用地址")
		return
	}

	// 尝试连接每个地址
	var connected bool
	for _, addrStr := range userInfo.Addresses {
		fmt.Printf("🔗 尝试连接: %s\n", addrStr)

		maddr, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			fmt.Printf("⚠️  解析地址失败: %v\n", err)
			continue
		}

		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			fmt.Printf("⚠️  解析peer信息失败: %v\n", err)
			continue
		}

		// 连接到peer
		h.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)

		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := h.Connect(connectCtx, *info); err != nil {
			cancel()
			fmt.Printf("⚠️  连接失败: %v\n", err)
			continue
		}
		cancel()

		fmt.Printf("✅ 已连接到 %s\n", info.ID)

		// 交换公钥
		streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
		stream, err := h.NewStream(streamCtx, info.ID, keyExchangeID)
		streamCancel()

		if err != nil {
			fmt.Printf("⚠️  创建密钥交换流失败: %v\n", err)
			continue
		}
		defer stream.Close()

		// 先发送自己的公钥
		encoder := gob.NewEncoder(stream)
		if err := encoder.Encode(pubKey); err != nil {
			fmt.Printf("⚠️  发送公钥失败: %v\n", err)
			continue
		}

		// 然后接收对方的公钥
		decoder := gob.NewDecoder(stream)
		var remotePubKey rsa.PublicKey
		if err := decoder.Decode(&remotePubKey); err != nil {
			fmt.Printf("⚠️  接收公钥失败: %v\n", err)
			continue
		}

		// 存储对方的公钥
		peerPubKeysMutex.Lock()
		peerPubKeys[info.ID] = &remotePubKey
		peerPubKeysMutex.Unlock()

		fmt.Printf("✅ 已与 %s (%s) 交换公钥，可以开始聊天了！\n\n", userInfo.Username, info.ID.ShortString())
		connected = true

		// 验证连接状态
		if h.Network().Connectedness(info.ID) == network.Connected {
			fmt.Printf("✅ 连接状态确认：已连接到 %s\n", userInfo.Username)
		} else {
			fmt.Printf("⚠️  警告：连接状态异常\n")
		}
		break
	}

	if !connected {
		fmt.Println("❌ 无法连接到目标用户")
		fmt.Println("💡 提示：请确保目标用户在线，并且网络可达")
	}
}

// hangupAllPeers 挂断所有已连接的peer
func hangupAllPeers(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, dhtDiscovery *DHTDiscovery) {
	conns := h.Network().Conns()
	if len(conns) == 0 {
		fmt.Println("📞 当前没有已连接的用户")
		return
	}

	fmt.Printf("📞 正在挂断所有已连接的用户 (%d 个)...\n", len(conns))

	// 收集所有需要挂断的peerID
	peerIDs := make(map[peer.ID]*UserInfo)
	for _, conn := range conns {
		peerID := conn.RemotePeer()
		var userInfo *UserInfo
		if dhtDiscovery != nil {
			userInfo = dhtDiscovery.GetUserByPeerID(peerID.String())
		}
		peerIDs[peerID] = userInfo
	}

	// 逐个挂断
	successCount := 0
	for peerID, userInfo := range peerIDs {
		// 可选：发送断开连接通知（如果已交换公钥）
		peerPubKeysMutex.RLock()
		_, hasKey := peerPubKeys[peerID]
		peerPubKeysMutex.RUnlock()

		if hasKey {
			// 尝试发送断开连接通知
			notifyCtx, notifyCancel := context.WithTimeout(ctx, 1*time.Second)
			stream, err := h.NewStream(notifyCtx, peerID, protocolID)
			notifyCancel()

			if err == nil {
				disconnectMsg := fmt.Sprintf("[系统通知] 连接已断开")
				peerPubKeysMutex.RLock()
				remotePubKey := peerPubKeys[peerID]
				peerPubKeysMutex.RUnlock()

				if remotePubKey != nil {
					encryptedMsg, err := encryptAndSignMessage(disconnectMsg, privKey, remotePubKey)
					if err == nil {
						stream.Write([]byte(encryptedMsg + "\n"))
					}
				}
				stream.Close()
			}
		}

		// 关闭所有与该peer的连接
		closed := false
		for _, conn := range conns {
			if conn.RemotePeer() == peerID {
				if err := conn.Close(); err == nil {
					closed = true
				}
			}
		}

		// 清理公钥缓存
		peerPubKeysMutex.Lock()
		if _, exists := peerPubKeys[peerID]; exists {
			delete(peerPubKeys, peerID)
		}
		peerPubKeysMutex.Unlock()

		if closed {
			successCount++
			if userInfo != nil {
				fmt.Printf("   ✅ 已断开与 %s (%s) 的连接\n", userInfo.Username, peerID.ShortString())
			} else {
				fmt.Printf("   ✅ 已断开与 %s 的连接\n", peerID.ShortString())
			}
		}
	}

	fmt.Printf("\n✅ 已挂断 %d/%d 个连接\n", successCount, len(peerIDs))
}

// playRockPaperScissors 玩石头剪刀布游戏
func playRockPaperScissors(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, myUsername string, dhtDiscovery *DHTDiscovery) {
	fmt.Println("🎲 随机生成选择中...")

	conns := h.Network().Conns()
	if len(conns) == 0 {
		fmt.Println("⚠️  当前没有已连接的用户，无法进行游戏")
		return
	}

	// 生成游戏ID（基于时间戳）
	gameID := fmt.Sprintf("%d", time.Now().UnixNano())
	myPeerID := h.ID().String()
	playerName := sanitizeRPSUsername(myUsername)
	if playerName == "" {
		playerName = h.ID().ShortString()
	}
	myChoice := randomRPSChoice(gameID)

	fmt.Printf("🎮 石头剪刀布游戏开始！\n")
	fmt.Printf("   你的选择: %s\n", getChoiceDisplay(myChoice))
	fmt.Printf("   等待其他玩家做出选择...\n\n")

	// 存储自己的选择
	rpsChoicesMutex.Lock()
	rpsChoices[gameID+"_"+myPeerID] = &RPSChoice{
		PeerID:    myPeerID,
		Choice:    myChoice,
		Timestamp: time.Now().Unix(),
		Username:  playerName,
	}
	rpsChoicesMutex.Unlock()

	// 向所有连接的peer发送选择
	rpsMsg := fmt.Sprintf("[RPS]%s|%s|%d|%s", gameID, myChoice, time.Now().Unix(), playerName)

	sentCount := 0
	for _, conn := range conns {
		peerID := conn.RemotePeer()

		// 检查连接状态
		if h.Network().Connectedness(peerID) != network.Connected {
			continue
		}

		// 获取对方的公钥
		peerPubKeysMutex.RLock()
		remotePubKey, hasKey := peerPubKeys[peerID]
		peerPubKeysMutex.RUnlock()

		if !hasKey {
			continue
		}

		// 加密并签名消息
		encryptedMsg, err := encryptAndSignMessage(rpsMsg, privKey, remotePubKey)
		if err != nil {
			log.Printf("加密RPS消息失败 (%s): %v\n", peerID.ShortString(), err)
			continue
		}

		// 发送消息
		streamCtx, streamCancel := context.WithTimeout(ctx, 3*time.Second)
		stream, err := h.NewStream(streamCtx, peerID, protocolID)
		streamCancel()

		if err != nil {
			log.Printf("发送RPS消息失败 (%s): %v\n", peerID.ShortString(), err)
			continue
		}

		writeDone := make(chan error, 1)
		go func() {
			_, err := stream.Write([]byte(encryptedMsg + "\n"))
			writeDone <- err
		}()

		select {
		case err := <-writeDone:
			if err == nil {
				sentCount++
			}
			stream.Close()
		case <-time.After(3 * time.Second):
			stream.Close()
		}
	}

	if sentCount == 0 {
		fmt.Println("⚠️  无法发送游戏消息给任何玩家")
		return
	}

	// 等待收集所有玩家的选择（最多5秒）
	time.Sleep(5 * time.Second)

	// 收集所有选择并比较结果
	rpsChoicesMutex.RLock()
	allChoices := make(map[string]*RPSChoice)
	for key, choice := range rpsChoices {
		if strings.HasPrefix(key, gameID+"_") {
			allChoices[choice.PeerID] = choice
		}
	}
	rpsChoicesMutex.RUnlock()

	// 清理本次游戏的选择
	rpsChoicesMutex.Lock()
	for key := range allChoices {
		delete(rpsChoices, gameID+"_"+key)
	}
	rpsChoicesMutex.Unlock()

	// 显示结果
	displayRPSResults(allChoices, myPeerID, dhtDiscovery)
}

// randomRPSChoice 生成随机的石头剪刀布选择，尽量避免重复
func randomRPSChoice(gameID string) string {
	available := getAvailableRPSChoices(gameID)
	idx := randomIndex(len(available))
	return available[idx]
}

// getAvailableRPSChoices 返回当前游戏中尚未被选择的手势列表
func getAvailableRPSChoices(gameID string) []string {
	usedChoices := make(map[string]struct{})

	if gameID != "" {
		rpsChoicesMutex.RLock()
		for key, choice := range rpsChoices {
			if strings.HasPrefix(key, gameID+"_") {
				usedChoices[choice.Choice] = struct{}{}
			}
		}
		rpsChoicesMutex.RUnlock()
	}

	available := make([]string, 0, len(rpsOptions))
	for _, opt := range rpsOptions {
		if _, exists := usedChoices[opt]; !exists {
			available = append(available, opt)
		}
	}

	// 如果所有选项都被使用，允许重复但保持随机
	if len(available) == 0 {
		available = append(available, rpsOptions...)
	}
	return available
}

// randomIndex 使用加密安全的随机数生成索引，必要时回退到math/rand
func randomIndex(max int) int {
	if max <= 1 {
		return 0
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err == nil {
		return int(n.Int64())
	}

	rpsFallbackRNGMutex.Lock()
	defer rpsFallbackRNGMutex.Unlock()
	return rpsFallbackRNG.Intn(max)
}

// sanitizeRPSUsername 对用户名做简单清理，避免特殊分隔符
func sanitizeRPSUsername(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.ReplaceAll(name, "|", "/")
}

// getChoiceDisplay 获取选择的显示文本
func getChoiceDisplay(choice string) string {
	switch choice {
	case "rock":
		return "✊ 石头"
	case "paper":
		return "✋ 布"
	case "scissors":
		return "✌️  剪刀"
	default:
		return choice
	}
}

// handleRPSMessage 处理收到的RPS消息
func handleRPSMessage(msg string, senderPeerID peer.ID) {
	// 解析消息格式: [RPS]gameID|choice|timestamp[|username]
	parts := strings.Split(strings.TrimPrefix(msg, "[RPS]"), "|")
	if len(parts) < 3 {
		return
	}

	gameID := parts[0]
	choice := parts[1]
	timestamp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return
	}
	senderUsername := ""
	if len(parts) >= 4 {
		senderUsername = sanitizeRPSUsername(parts[3])
	}

	// 检查时间戳（防止过期消息，允许10秒内的消息）
	if time.Now().Unix()-timestamp > 10 {
		return
	}

	// 获取全局变量
	globalVarsMutex.RLock()
	myHost := globalHost
	myPrivKey := globalPrivKey
	myCtx := globalCtx
	dhtDiscovery := globalDHTDiscovery
	myUsername := globalUsername
	globalVarsMutex.RUnlock()

	if myHost == nil || myPrivKey == nil {
		// 如果全局变量未设置，只存储选择
		rpsChoicesMutex.Lock()
		rpsChoices[gameID+"_"+senderPeerID.String()] = &RPSChoice{
			PeerID:    senderPeerID.String(),
			Choice:    choice,
			Timestamp: timestamp,
			Username:  senderUsername,
		}
		rpsChoicesMutex.Unlock()
		return
	}

	myPeerID := myHost.ID().String()
	gameKey := gameID + "_" + senderPeerID.String()

	// 存储发送方的选择
	rpsChoicesMutex.Lock()
	rpsChoices[gameKey] = &RPSChoice{
		PeerID:    senderPeerID.String(),
		Choice:    choice,
		Timestamp: timestamp,
		Username:  senderUsername,
	}

	// 检查自己是否已经参与了这个游戏
	myGameKey := gameID + "_" + myPeerID
	_, alreadyParticipated := rpsChoices[myGameKey]
	rpsChoicesMutex.Unlock()

	// 如果自己还没有参与这个游戏，自动随机出并回复
	if !alreadyParticipated {
		myChoice := randomRPSChoice(gameID)
		sanitizedName := sanitizeRPSUsername(myUsername)
		if sanitizedName == "" {
			sanitizedName = myHost.ID().ShortString()
		}

		// 存储自己的选择
		rpsChoicesMutex.Lock()
		rpsChoices[myGameKey] = &RPSChoice{
			PeerID:    myPeerID,
			Choice:    myChoice,
			Timestamp: time.Now().Unix(),
			Username:  sanitizedName,
		}
		rpsChoicesMutex.Unlock()

		// 向所有连接的peer发送自己的选择
		conns := myHost.Network().Conns()
		rpsMsg := fmt.Sprintf("[RPS]%s|%s|%d|%s", gameID, myChoice, time.Now().Unix(), sanitizedName)

		for _, conn := range conns {
			peerID := conn.RemotePeer()

			// 检查连接状态
			if myHost.Network().Connectedness(peerID) != network.Connected {
				continue
			}

			// 获取对方的公钥
			peerPubKeysMutex.RLock()
			remotePubKey, hasKey := peerPubKeys[peerID]
			peerPubKeysMutex.RUnlock()

			if !hasKey {
				continue
			}

			// 加密并签名消息
			encryptedMsg, err := encryptAndSignMessage(rpsMsg, myPrivKey, remotePubKey)
			if err != nil {
				continue
			}

			// 发送消息（异步，不阻塞）
			go func(pID peer.ID, encMsg string) {
				streamCtx, streamCancel := context.WithTimeout(myCtx, 2*time.Second)
				stream, err := myHost.NewStream(streamCtx, pID, protocolID)
				streamCancel()

				if err == nil {
					stream.Write([]byte(encMsg + "\n"))
					stream.Close()
				}
			}(peerID, encryptedMsg)
		}

		// 等待一段时间后显示结果
		go func() {
			time.Sleep(5 * time.Second)

			// 收集所有选择并比较结果
			rpsChoicesMutex.RLock()
			allChoices := make(map[string]*RPSChoice)
			for key, choice := range rpsChoices {
				if strings.HasPrefix(key, gameID+"_") {
					allChoices[choice.PeerID] = choice
				}
			}
			rpsChoicesMutex.RUnlock()

			// 清理本次游戏的选择
			rpsChoicesMutex.Lock()
			for key := range allChoices {
				delete(rpsChoices, gameID+"_"+key)
			}
			rpsChoicesMutex.Unlock()

			// 显示结果
			displayRPSResults(allChoices, myPeerID, dhtDiscovery)
		}()
	}
}

// displayRPSResults 显示游戏结果
func displayRPSResults(choices map[string]*RPSChoice, myPeerID string, dhtDiscovery *DHTDiscovery) {
	if len(choices) == 0 {
		fmt.Println("⚠️  没有收集到任何选择")
		return
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🎮 石头剪刀布游戏结果")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 显示所有玩家的选择
	for peerIDStr, choice := range choices {
		peerID, err := peer.Decode(peerIDStr)
		if err != nil {
			continue
		}

		username := strings.TrimSpace(choice.Username)
		if username == "" && dhtDiscovery != nil {
			if userInfo := dhtDiscovery.GetUserByPeerID(peerIDStr); userInfo != nil {
				username = userInfo.Username
			}
		}
		if username == "" {
			username = peerID.ShortString()
		}

		isMe := peerIDStr == myPeerID
		marker := ""
		if isMe {
			marker = " (你)"
		}

		fmt.Printf("   %s%s: %s\n", username, marker, getChoiceDisplay(choice.Choice))
	}

	fmt.Println()

	// 统计每种选择的数量
	rockCount := 0
	paperCount := 0
	scissorsCount := 0

	for _, choice := range choices {
		switch choice.Choice {
		case "rock":
			rockCount++
		case "paper":
			paperCount++
		case "scissors":
			scissorsCount++
		}
	}

	// 判断胜负
	fmt.Println("📊 统计:")
	fmt.Printf("   ✊ 石头: %d 人\n", rockCount)
	fmt.Printf("   ✋ 布: %d 人\n", paperCount)
	fmt.Printf("   ✌️  剪刀: %d 人\n", scissorsCount)
	fmt.Println()

	// 判断结果
	if rockCount > 0 && paperCount > 0 && scissorsCount > 0 {
		fmt.Println("🤝 平局！三种选择都有，游戏无效")
	} else if rockCount > 0 && paperCount > 0 && scissorsCount == 0 {
		fmt.Println("🏆 布获胜！")
		showWinners(choices, "paper", myPeerID, dhtDiscovery)
	} else if rockCount > 0 && scissorsCount > 0 && paperCount == 0 {
		fmt.Println("🏆 石头获胜！")
		showWinners(choices, "rock", myPeerID, dhtDiscovery)
	} else if paperCount > 0 && scissorsCount > 0 && rockCount == 0 {
		fmt.Println("🏆 剪刀获胜！")
		showWinners(choices, "scissors", myPeerID, dhtDiscovery)
	} else {
		fmt.Println("🤝 平局！所有人选择了相同的手势")
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// showWinners 显示获胜者
func showWinners(choices map[string]*RPSChoice, winningChoice string, myPeerID string, dhtDiscovery *DHTDiscovery) {
	winners := []string{}
	for peerIDStr, choice := range choices {
		if choice.Choice == winningChoice {
			peerID, err := peer.Decode(peerIDStr)
			if err != nil {
				continue
			}

			username := strings.TrimSpace(choice.Username)
			if username == "" && dhtDiscovery != nil {
				if userInfo := dhtDiscovery.GetUserByPeerID(peerIDStr); userInfo != nil {
					username = userInfo.Username
				}
			}
			if username == "" {
				username = peerID.ShortString()
			}

			isMe := peerIDStr == myPeerID
			if isMe {
				username += " (你)"
			}

			winners = append(winners, username)
		}
	}

	if len(winners) > 0 {
		fmt.Printf("   🎉 获胜者: %s\n", strings.Join(winners, ", "))
	}
}
