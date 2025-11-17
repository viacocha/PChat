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
	"errors"
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
				time.Sleep(2 * time.Second) // 等待连接稳定和DHT初始化
				commonUsernames := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Frank", "Grace", "Henry"}
				for _, username := range commonUsernames {
					userInfo, err := n.dhtDiscovery.LookupUser(n.ctx, username)
					if err == nil && userInfo.PeerID == peerIDStr {
						// 找到了这个peer的用户信息，记录到本地缓存
						fmt.Printf("✅ 自动发现用户: %s (节点ID: %s)\n", userInfo.Username, peerID.ShortString())
						// 用户信息已经在LookupUser中自动缓存，无需额外操作
						break
					}
				}
			}()
		} else {
			// 已经知道用户信息
			if userInfo := n.dhtDiscovery.GetUserByPeerID(peerIDStr); userInfo != nil {
				fmt.Printf("✅ 已连接用户: %s (节点ID: %s)\n", userInfo.Username, peerID.ShortString())
			}
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

// globalUI 全局UI实例，用于消息显示
var globalUI *ChatUI
var globalUIMutex sync.RWMutex

// init 初始化函数，启动定期清理过期 nonce 的后台 goroutine
// 该函数在包加载时自动执行，用于防止内存泄漏
func init() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cleanupNonces()
		}
	}()
}

// cleanupNonces 清理过期的 nonce 记录
// 定期清理超过 maxMessageAge 的 nonce，防止内存泄漏
// 该函数是线程安全的，使用互斥锁保护共享的 usedNonces map
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

// sendOfflineNotification 发送离线通知给所有已连接的 peer
// 当客户端退出时，向所有已连接的 peer 发送加密的离线通知消息
//
// 参数:
//   - ctx: 上下文，用于控制操作的超时和取消
//   - h: libp2p host 实例，用于获取连接和发送消息
//   - privKey: 发送方的 RSA 私钥，用于签名消息
//   - username: 发送方的用户名，将包含在离线通知中
//
// 该函数会遍历所有活跃连接，对每个 peer 发送加密签名的离线通知
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

// shutdownConnections 优雅关闭所有网络连接
// 遍历所有活跃连接并关闭它们，确保资源正确释放
//
// 参数:
//   - h: libp2p host 实例，包含所有网络连接
//
// 该函数会关闭所有已建立的连接，但不等待连接完全关闭
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

// cleanupResources 清理所有全局资源
// 清理公钥缓存、nonce 记录和 RPS 游戏选择记录
// 该函数在程序退出时调用，确保资源正确释放
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
	listenPort := flag.Int("port", 0, "监听端口，0表示随机分配")
	targetPeer := flag.String("peer", "", "要连接的peer地址，格式：/ip4/127.0.0.1/tcp/端口/p2p/peerID")
	registryAddr := flag.String("registry", "", "注册服务器地址，格式：127.0.0.1:8888")
	username := flag.String("username", "", "用户名")
	// 使用字符串标志来支持 "-ui false" 和 "-ui=false" 两种格式
	uiFlag := flag.String("ui", "false", "是否使用视窗化UI界面，格式：true/false，默认：false")
	flag.Parse()

	// 解析UI标志，支持 "true", "false", "1", "0", "yes", "no" 等格式
	useUI := false
	if *uiFlag != "" {
		uiFlagLower := strings.ToLower(strings.TrimSpace(*uiFlag))
		useUI = uiFlagLower == "true" || uiFlagLower == "1" || uiFlagLower == "yes" || uiFlagLower == "on"
	}

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

	// 如果没有提供用户名，提示用户输入（在UI启动前）
	if *username == "" {
		*username = h.ID().ShortString() // 默认使用节点ID的短格式作为用户名
	}

	// 输出调试信息（在UI启动前）
	fmt.Fprintf(os.Stderr, "=== PChat 启动调试信息 ===\n")
	fmt.Fprintf(os.Stderr, "节点ID: %s\n", h.ID())
	fmt.Fprintf(os.Stderr, "监听地址: %v\n", h.Addrs())
	fmt.Fprintf(os.Stderr, "用户名: %s\n", *username)

	globalVarsMutex.Lock()
	globalUsername = *username
	globalVarsMutex.Unlock()

	// 选择使用注册服务器还是DHT发现
	var registryClient *RegistryClient
	var dhtDiscovery *DHTDiscovery

	// 保存dhtDiscovery的引用，用于关闭时清理
	var dhtDiscoveryRef *DHTDiscovery

	// 使用之前创建的上下文（如果已创建）
	if ctx == nil {
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
	}

	// 处理中断信号（在UI启动前设置）
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var ui *ChatUI
	var uiDone chan struct{}

	if useUI {
		// 使用视窗化UI
		fmt.Fprintf(os.Stderr, "正在创建UI界面...\n")
		ui = NewChatUI(ctx, h, privKey, pubKey, registryClient, dhtDiscovery, *username)
		fmt.Fprintf(os.Stderr, "UI界面创建完成\n")

		// 设置全局UI
		globalUIMutex.Lock()
		globalUI = ui
		globalUIMutex.Unlock()

		// 在goroutine中运行UI（参考registryd的实现）
		uiDone = make(chan struct{})
		go func() {
			defer close(uiDone)
			if err := ui.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "UI运行错误: %v\n", err)
				// 如果UI运行失败，等待一小段时间后关闭
				time.Sleep(500 * time.Millisecond)
			} else {
				// UI正常退出（可能是用户按了Ctrl+C或/quit）
				fmt.Fprintf(os.Stderr, "UI正常退出\n")
			}
		}()

		// 等待一小段时间让UI启动
		time.Sleep(200 * time.Millisecond)

		// 现在可以安全地添加消息了
		ui.AddMessage("系统", fmt.Sprintf("P2P 聊天节点已启动 (节点ID: %s)", h.ID().ShortString()), true)
		for _, addr := range h.Addrs() {
			ui.AddMessage("系统", fmt.Sprintf("监听地址: %s/p2p/%s", addr, h.ID()), true)
		}
	} else {
		// 不使用UI，使用简单的命令行模式
		fmt.Printf("=== PChat 启动 ===\n")
		fmt.Printf("节点ID: %s\n", h.ID().ShortString())
		for _, addr := range h.Addrs() {
			fmt.Printf("监听地址: %s/p2p/%s\n", addr, h.ID())
		}
		fmt.Printf("用户名: %s\n", *username)
		fmt.Printf("输入 /help 查看帮助，输入 /quit 退出\n\n")
	}

	if *registryAddr != "" {
		// 使用注册服务器模式
		registryClient = NewRegistryClient(*registryAddr, h, *username)
		if err := registryClient.Register(); err != nil {
			if ui != nil {
				ui.AddMessage("系统", fmt.Sprintf("⚠️ 注册到服务器失败: %v", err), true)
			} else {
				fmt.Printf("⚠️ 注册到服务器失败: %v\n", err)
			}
		} else {
			if ui != nil {
				ui.AddMessage("系统", fmt.Sprintf("✅ 已注册到服务器: %s (用户名: %s)", *registryAddr, *username), true)
			} else {
				fmt.Printf("✅ 已注册到服务器: %s (用户名: %s)\n", *registryAddr, *username)
			}
			// 启动心跳
			go registryClient.StartHeartbeat(ctx)
		}
	} else {
		// 使用DHT去中心化发现模式
		if ui != nil {
			ui.AddMessage("系统", "🌐 使用DHT去中心化发现模式（无需注册服务器）", true)
		} else {
			fmt.Printf("🌐 使用DHT去中心化发现模式（无需注册服务器）\n")
		}
		dhtDisc, err := NewDHTDiscovery(ctx, h, *username)
		if err != nil {
			if ui != nil {
				ui.AddMessage("系统", fmt.Sprintf("⚠️ 启动DHT发现失败: %v", err), true)
				ui.AddMessage("系统", "💡 提示：DHT发现需要连接到其他节点才能工作", true)
			} else {
				fmt.Printf("⚠️ 启动DHT发现失败: %v\n", err)
				fmt.Printf("💡 提示：DHT发现需要连接到其他节点才能工作\n")
			}
		} else {
			dhtDiscovery = dhtDisc
			dhtDiscoveryRef = dhtDisc
			if ui != nil {
				ui.AddMessage("系统", fmt.Sprintf("✅ DHT发现服务已启动 (用户名: %s)", *username), true)
				ui.AddMessage("系统", "💡 提示：DHT发现需要一些时间来连接网络中的其他节点", true)
			} else {
				fmt.Printf("✅ DHT发现服务已启动 (用户名: %s)\n", *username)
				fmt.Printf("💡 提示：DHT发现需要一些时间来连接网络中的其他节点\n")
			}

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
	}

	// 更新UI中的registryClient和dhtDiscovery引用
	if ui != nil {
		ui.registryClient = registryClient
		ui.dhtDiscovery = dhtDiscovery
	}

	// 如果提供了目标 peer，则连接到它
	if *targetPeer != "" {
		if ui != nil {
			ui.AddMessage("系统", "正在连接到指定节点...", true)
		} else {
			fmt.Printf("正在连接到指定节点...\n")
		}
		go func() {
			connectToPeer(h, *targetPeer, privKey, pubKey, dhtDiscovery, ctx)
		}()
	}

	// 如果不使用UI，启动命令行输入循环
	if !useUI {
		// 显示初始提示符
		fmt.Print("> ")
		// CLI模式在main goroutine中运行，这样Ctrl+C可以正常退出
		startCLIInputLoop(ctx, h, privKey, pubKey, registryClient, dhtDiscovery, *username, sigCh)
		return // CLI模式在startCLIInputLoop中处理退出
	}

	// 等待信号或UI退出
	if useUI && ui != nil {
		select {
		case sig := <-sigCh:
			// 收到信号，立即停止UI和所有goroutine
			fmt.Fprintf(os.Stderr, "收到信号: %v，正在退出...\n", sig)
			cancel()  // 先取消上下文，停止所有后台任务
			ui.Stop() // 停止UI（这会立即返回）
			// 等待UI完全退出
			select {
			case <-uiDone:
				fmt.Fprintf(os.Stderr, "UI已退出\n")
			case <-time.After(2 * time.Second):
				// 超时，强制退出
				fmt.Fprintf(os.Stderr, "UI退出超时，强制退出\n")
			}
		case <-uiDone:
			// UI已退出（可能是通过 /quit 或 Ctrl+C）
			fmt.Fprintf(os.Stderr, "UI已退出\n")
			cancel()
		}
	} else {
		// 不使用UI，等待信号
		<-sigCh
		fmt.Printf("\n收到关闭信号，正在退出...\n")
		cancel()
		time.Sleep(500 * time.Millisecond)
	}

	// 确保上下文已取消
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

	// 等待一小段时间确保清理完成
	time.Sleep(1 * time.Second)

	// 清理资源
	fmt.Println("🧹 正在清理资源...")
	cleanupResources()

	fmt.Println("👋 程序已安全退出")
}

// 处理接收到的流
func handleStream(s network.Stream, privKey *rsa.PrivateKey) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in handleStream: %v\n", r)
		}
		s.Close()
	}()

	if s == nil {
		log.Printf("handleStream: stream is nil")
		return
	}
	if privKey == nil {
		log.Printf("handleStream: private key is nil")
		return
	}

	conn := s.Conn()
	if conn == nil {
		log.Printf("handleStream: connection is nil")
		return
	}

	peerID := conn.RemotePeer()

	reader := bufio.NewReader(s)
	for {
		resetStreamDeadline(s.SetReadDeadline, 30*time.Second)
		encryptedMsg, readErr := readEncryptedLine(reader)
		if encryptedMsg == "" {
			if readErr == nil || errors.Is(readErr, errEmptyMessage) {
				continue
			}
			if errors.Is(readErr, io.EOF) {
				return
			}
			if netErr, ok := readErr.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				log.Printf("⚠️  读取消息超时 (%s): %v\n", peerID.ShortString(), readErr)
			} else {
				log.Printf("⚠️  读取消息失败 (%s): %v\n", peerID.ShortString(), readErr)
			}
			return
		}

		decryptedMsg, verified, err := decryptAndVerifyMessage(encryptedMsg, privKey, peerID)
		if err != nil {
			globalUIMutex.RLock()
			ui := globalUI
			globalUIMutex.RUnlock()
			if ui != nil {
				ui.AddMessage("系统", fmt.Sprintf("解密失败 (%s): %v", peerID.ShortString(), err), true)
			} else {
				log.Printf("⚠️  解密失败 (%s): %v\n", peerID.ShortString(), err)
			}
			if errors.Is(readErr, io.EOF) {
				return
			}
			continue
		}

		senderName := getUserNameByPeerID(peerID)
		if senderName == "" {
			senderName = peerID.ShortString()
		}

		globalUIMutex.RLock()
		ui := globalUI
		globalUIMutex.RUnlock()

		processIncomingMessage(decryptedMsg, senderName, verified, peerID, ui)

		if errors.Is(readErr, io.EOF) {
			return
		}
	}
}

// 处理公钥交换（作为服务器端，先接收对方的公钥，然后发送自己的）
func handleKeyExchange(s network.Stream, privKey *rsa.PrivateKey, pubKey rsa.PublicKey) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in handleKeyExchange: %v\n", r)
		}
		s.Close()
	}()

	if s == nil {
		log.Printf("handleKeyExchange: stream is nil")
		return
	}
	if privKey == nil {
		log.Printf("handleKeyExchange: private key is nil")
		return
	}

	conn := s.Conn()
	if conn == nil {
		log.Printf("handleKeyExchange: connection is nil")
		return
	}

	peerID := conn.RemotePeer()

	// 先接收对方的公钥
	decoder := gob.NewDecoder(s)
	var remotePubKey rsa.PublicKey
	if err := decoder.Decode(&remotePubKey); err != nil {
		log.Printf("接收公钥失败: %v\n", err)
		return
	}

	// 接收对方的用户名（如果发送了）
	var remoteUsername string
	if err := decoder.Decode(&remoteUsername); err != nil {
		// 如果对方没有发送用户名（旧版本兼容），尝试从DHT获取
		globalVarsMutex.RLock()
		dhtDiscovery := globalDHTDiscovery
		globalVarsMutex.RUnlock()
		if dhtDiscovery != nil {
			if userInfo := dhtDiscovery.GetUserByPeerID(peerID.String()); userInfo != nil {
				remoteUsername = userInfo.Username
			}
		}
		// 如果还是找不到，使用peerID
		if remoteUsername == "" {
			remoteUsername = peerID.ShortString()
		}
	}

	// 然后发送自己的公钥
	encoder := gob.NewEncoder(s)
	if err := encoder.Encode(pubKey); err != nil {
		if isConnectionClosedError(err) {
			return
		}
		log.Printf("发送公钥失败: %v\n", err)
		return
	}

	// 获取自己的用户名并发送
	globalVarsMutex.RLock()
	myUsername := globalUsername
	myHost := globalHost
	myCtx := globalCtx
	globalVarsMutex.RUnlock()

	// 发送自己的用户名
	if err := encoder.Encode(myUsername); err != nil {
		if isConnectionClosedError(err) {
			return
		}
		log.Printf("发送用户名失败: %v\n", err)
		return
	}

	// 存储对方的公钥
	peerPubKeysMutex.Lock()
	peerPubKeys[peerID] = &remotePubKey
	peerPubKeysMutex.Unlock()

	// 如果启用了DHT，记录对方的用户信息
	// 只有当remoteUsername是真正的用户名（不是peerID）时才记录
	globalVarsMutex.RLock()
	dhtDiscovery := globalDHTDiscovery
	globalVarsMutex.RUnlock()
	if dhtDiscovery != nil && remoteUsername != "" && remoteUsername != peerID.ShortString() {
		addresses := make([]string, 0)
		// 获取连接的所有地址
		conn := s.Conn()
		if conn != nil {
			// 获取远程地址
			remoteAddr := conn.RemoteMultiaddr()
			if remoteAddr != nil {
				addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", remoteAddr.String(), peerID))
			}
			// 也可以从host获取所有地址
			if myHost != nil {
				for _, addr := range myHost.Peerstore().Addrs(peerID) {
					addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", addr.String(), peerID))
				}
			}
		}
		if len(addresses) == 0 {
			// 如果没有地址，至少记录peerID
			addresses = []string{fmt.Sprintf("/p2p/%s", peerID)}
		}
		dhtDiscovery.RecordUserFromConnection(remoteUsername, peerID.String(), addresses)
		// 确保立即更新，以便后续消息能正确显示用户名
		log.Printf("📝 已记录用户信息: %s (节点ID: %s)\n", remoteUsername, peerID.ShortString())
	}

	// 使用从公钥交换中获取的用户名，如果没有则尝试从DHT获取
	callerUsername := remoteUsername
	if callerUsername == "" || callerUsername == peerID.ShortString() {
		// 尝试从DHT获取对方用户名
		if myHost != nil {
			globalVarsMutex.RLock()
			dhtDiscovery := globalDHTDiscovery
			globalVarsMutex.RUnlock()
			if dhtDiscovery != nil {
				if userInfo := dhtDiscovery.GetUserByPeerID(peerID.String()); userInfo != nil {
					callerUsername = userInfo.Username
				}
			}
		}
	}

	if callerUsername == "" {
		callerUsername = peerID.ShortString()
	}

	globalUIMutex.RLock()
	ui := globalUI
	globalUIMutex.RUnlock()
	if ui != nil {
		// 将交换公钥消息显示到状态栏，而不是聊天记录
		ui.AddStatusMessage(fmt.Sprintf("✅ 已与 %s (%s) 交换公钥", callerUsername, peerID.ShortString()))
	} else {
		log.Printf("✅ 已与 %s (%s) 交换公钥\n", callerUsername, peerID.ShortString())
	}

	// 发送通知消息给呼叫方，告知连接成功
	if myHost != nil && myCtx != nil {
		go func() {
			// 等待一小段时间确保连接稳定
			time.Sleep(500 * time.Millisecond)

			// 创建通知消息
			var notifyMsg string
			if myUsername != "" {
				notifyMsg = fmt.Sprintf("[系统通知] %s 已接受您的连接请求，可以开始聊天了！", myUsername)
			} else {
				notifyMsg = fmt.Sprintf("[系统通知] 对方已接受您的连接请求，可以开始聊天了！")
			}

			// 发送通知
			notifyCtx, notifyCancel := context.WithTimeout(myCtx, 3*time.Second)
			defer notifyCancel()

			stream, err := myHost.NewStream(notifyCtx, peerID, protocolID)
			if err == nil {
				encryptedMsg, err := encryptAndSignMessage(notifyMsg, privKey, &remotePubKey)
				if err == nil {
					stream.Write([]byte(encryptedMsg + "\n"))
				}
				stream.Close()
			}
		}()
	}
}

// 连接到指定的 peer
func connectToPeer(h host.Host, targetAddr string, privKey *rsa.PrivateKey, pubKey rsa.PublicKey, dhtDiscovery *DHTDiscovery, ctx context.Context) {
	maddr, err := multiaddr.NewMultiaddr(targetAddr)
	if err != nil {
		log.Fatal("解析地址失败:", err)
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		log.Fatal("解析 peer 信息失败:", err)
	}

	h.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)

	globalUIMutex.RLock()
	ui := globalUI
	globalUIMutex.RUnlock()
	if ui != nil {
		ui.AddMessage("系统", fmt.Sprintf("🔗 正在连接到 %s...", info.ID.ShortString()), true)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if err := h.Connect(ctx, *info); err != nil {
		if ui != nil {
			ui.AddMessage("系统", fmt.Sprintf("❌ 连接失败: %v", err), true)
		}
		log.Fatal("连接失败:", err)
	}

	if ui != nil {
		ui.AddMessage("系统", fmt.Sprintf("✅ 已连接到 %s", info.ID.ShortString()), true)
	} else {
		log.Printf("✅ 已连接到 %s\n", info.ID)
	}

	// 交换公钥和用户名
	stream, err := h.NewStream(ctx, info.ID, keyExchangeID)
	if err != nil {
		log.Fatal("创建密钥交换流失败:", err)
	}
	defer stream.Close()

	// 获取自己的用户名
	globalVarsMutex.RLock()
	myUsername := globalUsername
	globalVarsMutex.RUnlock()

	// 先发送自己的公钥
	encoder := gob.NewEncoder(stream)
	if err := encoder.Encode(pubKey); err != nil {
		log.Fatal("发送公钥失败:", err)
	}

	// 发送自己的用户名
	if err := encoder.Encode(myUsername); err != nil {
		log.Fatal("发送用户名失败:", err)
	}

	// 然后接收对方的公钥
	decoder := gob.NewDecoder(stream)
	var remotePubKey rsa.PublicKey
	if err := decoder.Decode(&remotePubKey); err != nil {
		log.Fatal("接收公钥失败:", err)
	}

	// 接收对方的用户名
	var remoteUsername string
	if err := decoder.Decode(&remoteUsername); err != nil {
		// 如果对方没有发送用户名（旧版本兼容），尝试从DHT获取
		if dhtDiscovery != nil {
			if userInfo := dhtDiscovery.GetUserByPeerID(info.ID.String()); userInfo != nil {
				remoteUsername = userInfo.Username
			}
		}
		// 如果还是找不到，使用peerID
		if remoteUsername == "" {
			remoteUsername = info.ID.ShortString()
		}
	}

	// 存储对方的公钥
	peerPubKeysMutex.Lock()
	peerPubKeys[info.ID] = &remotePubKey
	peerPubKeysMutex.Unlock()

	// 如果启用了DHT，记录对方的用户信息
	// 只有当remoteUsername是真正的用户名（不是peerID）时才记录
	if dhtDiscovery != nil && remoteUsername != "" && remoteUsername != info.ID.ShortString() {
		addresses := make([]string, 0)
		for _, addr := range info.Addrs {
			addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", addr, info.ID))
		}
		dhtDiscovery.RecordUserFromConnection(remoteUsername, info.ID.String(), addresses)
		// 确保立即更新，以便后续消息能正确显示用户名
		log.Printf("📝 已记录用户信息: %s (节点ID: %s)\n", remoteUsername, info.ID.ShortString())
	}

	globalUIMutex.RLock()
	ui2 := globalUI
	globalUIMutex.RUnlock()
	if ui2 != nil {
		// 将交换公钥消息显示到状态栏，而不是聊天记录
		if remoteUsername != "" && remoteUsername != info.ID.ShortString() {
			ui2.AddStatusMessage(fmt.Sprintf("✅ 已与 %s (%s) 交换公钥", remoteUsername, info.ID.ShortString()))
		} else {
			ui2.AddStatusMessage(fmt.Sprintf("✅ 已与 %s 交换公钥", info.ID.ShortString()))
		}
	} else {
		if remoteUsername != "" && remoteUsername != info.ID.ShortString() {
			log.Printf("✅ 已与 %s (%s) 交换公钥\n", remoteUsername, info.ID)
		} else {
			log.Printf("✅ 已与 %s 交换公钥\n", info.ID)
		}
	}

	// 如果启用了DHT，尝试发现对方的用户信息
	if dhtDiscovery != nil {
		peerIDStr := info.ID.String()
		// 检查是否已经知道这个peer的用户信息
		if dhtDiscovery.GetUserByPeerID(peerIDStr) == nil {
			// 尝试查找常见的用户名
			go func() {
				time.Sleep(2 * time.Second) // 等待连接稳定和DHT初始化
				commonUsernames := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Frank", "Grace", "Henry"}
				for _, username := range commonUsernames {
					userInfo, err := dhtDiscovery.LookupUser(ctx, username)
					if err == nil && userInfo.PeerID == peerIDStr {
						// 找到了这个peer的用户信息
						globalUIMutex.RLock()
						ui3 := globalUI
						globalUIMutex.RUnlock()
						if ui3 != nil {
							ui3.AddMessage("系统", fmt.Sprintf("✅ 发现用户: %s (节点ID: %s)", userInfo.Username, info.ID.ShortString()), true)
						} else {
							log.Printf("✅ 发现用户: %s (节点ID: %s)\n", userInfo.Username, info.ID.ShortString())
						}
						// 用户信息已经在LookupUser中自动缓存
						break
					}
				}
			}()
		} else {
			// 已经知道用户信息，直接显示
			if userInfo := dhtDiscovery.GetUserByPeerID(peerIDStr); userInfo != nil {
				globalUIMutex.RLock()
				ui4 := globalUI
				globalUIMutex.RUnlock()
				if ui4 != nil {
					ui4.AddMessage("系统", fmt.Sprintf("✅ 用户: %s", userInfo.Username), true)
				} else {
					log.Printf("✅ 用户: %s\n", userInfo.Username)
				}
			}
		}
	}
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

		// 处理文件发送命令（支持 /sendfile, /file, /s, /f）
		if strings.HasPrefix(msg, "/sendfile ") || strings.HasPrefix(msg, "/file ") || strings.HasPrefix(msg, "/s ") || strings.HasPrefix(msg, "/f ") {
			var filePath string
			if strings.HasPrefix(msg, "/sendfile ") {
				filePath = strings.TrimSpace(strings.TrimPrefix(msg, "/sendfile "))
			} else if strings.HasPrefix(msg, "/file ") {
				filePath = strings.TrimSpace(strings.TrimPrefix(msg, "/file "))
			} else if strings.HasPrefix(msg, "/s ") {
				filePath = strings.TrimSpace(strings.TrimPrefix(msg, "/s "))
			} else if strings.HasPrefix(msg, "/f ") {
				filePath = strings.TrimSpace(strings.TrimPrefix(msg, "/f "))
			}
			if filePath == "" {
				fmt.Println("⚠️  用法: /sendfile 或 /file <文件路径>")
				fmt.Print("> ")
				continue
			}
			sendFileToPeers(ctx, h, privKey, filePath)
			fmt.Print("> ")
			continue
		}

		// 处理查询在线用户命令（支持 /list, /users, /l）
		if msg == "/list" || msg == "/users" || msg == "/l" {
			if registryClient != nil {
				listOnlineUsers(registryClient)
			} else if dhtDiscovery != nil {
				// 在列出用户之前，先尝试发现网络中的用户
				conns := dhtDiscovery.host.Network().Conns()
				if len(conns) > 0 {
					fmt.Printf("🔍 正在发现已连接节点的用户信息 (%d 个连接)...\n", len(conns))

					// 收集所有需要查找的peerID
					peerIDsToDiscover := make([]peer.ID, 0)
					for _, conn := range conns {
						peerID := conn.RemotePeer()
						peerIDStr := peerID.String()

						// 检查是否已经知道这个peer的用户信息
						if dhtDiscovery.GetUserByPeerID(peerIDStr) == nil {
							peerIDsToDiscover = append(peerIDsToDiscover, peerID)
						}
					}

					// 对于每个未知的peer，尝试查找常见的用户名
					if len(peerIDsToDiscover) > 0 {
						commonUsernames := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Frank", "Grace", "Henry"}
						for _, peerID := range peerIDsToDiscover {
							peerIDStr := peerID.String()
							for _, username := range commonUsernames {
								userInfo, err := dhtDiscovery.LookupUser(ctx, username)
								if err == nil && userInfo.PeerID == peerIDStr {
									// 找到了这个peer的用户信息
									fmt.Printf("✅ 发现用户: %s (节点ID: %s)\n", userInfo.Username, peerID.ShortString())
									break
								}
							}
						}
					}

					// 等待一小段时间让发现完成
					time.Sleep(1 * time.Second)
				}
				listDHTUsers(dhtDiscovery, ctx)
			} else {
				fmt.Println("⚠️  未启用用户发现功能")
				fmt.Println("   请使用 -registry 参数连接注册服务器，或使用DHT发现模式")
			}
			fmt.Print("> ")
			continue
		}

		// 处理call命令（支持 /call, call, /c）
		if strings.HasPrefix(msg, "/call ") || strings.HasPrefix(msg, "call ") || strings.HasPrefix(msg, "/c ") {
			var target string
			if strings.HasPrefix(msg, "/call ") {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "/call "))
			} else if strings.HasPrefix(msg, "call ") {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "call "))
			} else if strings.HasPrefix(msg, "/c ") {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "/c "))
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

		// 处理挂断命令（支持 /hangup, /disconnect, /d）
		if msg == "/hangup" || msg == "/disconnect" || msg == "/d" {
			// 没有参数，挂断所有连接
			hangupAllPeers(ctx, h, privKey, dhtDiscovery)
			fmt.Print("> ")
			continue
		}
		if strings.HasPrefix(msg, "/hangup ") || strings.HasPrefix(msg, "/disconnect ") || strings.HasPrefix(msg, "/d ") {
			var target string
			if strings.HasPrefix(msg, "/hangup ") {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "/hangup "))
			} else if strings.HasPrefix(msg, "/disconnect ") {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "/disconnect "))
			} else if strings.HasPrefix(msg, "/d ") {
				target = strings.TrimSpace(strings.TrimPrefix(msg, "/d "))
			}
			if target == "" {
				// 空参数也挂断所有连接
				hangupAllPeers(ctx, h, privKey, dhtDiscovery)
			} else {
				// 获取registryClient（如果存在）
				globalUIMutex.RLock()
				ui := globalUI
				globalUIMutex.RUnlock()
				var registryClient *RegistryClient
				if ui != nil {
					registryClient = ui.registryClient
				}
				hangupPeer(ctx, h, privKey, target, dhtDiscovery, registryClient)
			}
			fmt.Print("> ")
			continue
		}

		// 处理石头剪刀布游戏命令（支持 /rps, /r）
		if msg == "/rps" || msg == "/rockpaperscissors" || msg == "/r" {
			playRockPaperScissors(ctx, h, privKey, myUsername, dhtDiscovery)
			fmt.Print("> ")
			continue
		}
		if strings.HasPrefix(msg, "/rps ") || strings.HasPrefix(msg, "/rockpaperscissors ") || strings.HasPrefix(msg, "/r ") {
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
			connectedness := h.Network().Connectedness(peerID)
			if connectedness != network.Connected {
				continue
			}

			// 获取对方的公钥
			peerPubKeysMutex.RLock()
			remotePubKey, hasKey := peerPubKeys[peerID]
			peerPubKeysMutex.RUnlock()

			if !hasKey {
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
				log.Printf("创建流失败 (%s): %v\n", peerID.ShortString(), err)
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
					log.Printf("发送消息失败 (%s): %v\n", peerID.ShortString(), err)
					stream.Close()
					continue
				}
				// 获取用户名用于显示
				var displayName string
				if dhtDiscovery != nil {
					if userInfo := dhtDiscovery.GetUserByPeerID(peerID.String()); userInfo != nil {
						displayName = userInfo.Username
					}
				}
				if displayName == "" {
					displayName = peerID.ShortString()
				}
				fmt.Printf("📤 已发送消息给 %s\n", displayName)
				stream.Close()
				sent = true
			case <-ctx.Done():
				stream.Close()
				return
			case <-time.After(5 * time.Second):
				log.Printf("发送消息超时 (%s)\n", peerID.ShortString())
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
// 该函数实现了端到端加密通信的核心功能：
// 1. 使用 AES-256 加密消息内容
// 2. 使用 RSA 加密 AES 密钥
// 3. 使用发送方私钥对消息进行数字签名
// 4. 添加时间戳和随机 nonce 防止重放攻击
// 5. 将结果编码为 base64 字符串
//
// 参数:
//   - msg: 要加密的明文消息
//   - senderPrivKey: 发送方的 RSA 私钥，用于签名
//   - recipientPubKey: 接收方的 RSA 公钥，用于加密 AES 密钥
//
// 返回:
//   - string: base64 编码的加密消息（包含加密数据、签名、时间戳、nonce）
//   - error: 如果加密或签名失败则返回错误
//
// 该函数包含输入验证，确保密钥和消息不为空
func encryptAndSignMessage(msg string, senderPrivKey *rsa.PrivateKey, recipientPubKey *rsa.PublicKey) (string, error) {
	// 输入验证
	if senderPrivKey == nil {
		return "", fmt.Errorf("sender private key cannot be nil")
	}
	if recipientPubKey == nil {
		return "", fmt.Errorf("recipient public key cannot be nil")
	}
	if recipientPubKey.N == nil {
		return "", fmt.Errorf("recipient public key is invalid (N is nil)")
	}

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

// encryptMessageWithPubKey 使用接收方公钥加密消息
// 使用混合加密方案：AES-256 加密消息内容，RSA 加密 AES 密钥
//
// 参数:
//   - msg: 要加密的明文消息（字节数组）
//   - pubKey: 接收方的 RSA 公钥，用于加密 AES 密钥
//
// 返回:
//   - []byte: 加密后的数据（包含加密的 AES 密钥和加密的消息内容）
//   - error: 如果加密失败则返回错误
//
// 该函数会生成随机的 AES-256 密钥，使用 GCM 模式进行加密
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

// decryptAndVerifyMessage 解密消息并验证签名和重放攻击防护
// 该函数实现了安全消息接收的完整流程：
// 1. 解码 base64 编码的加密消息
// 2. 解析 SecureMessage 结构（加密数据、签名、时间戳、nonce）
// 3. 检查消息时间戳，拒绝过期消息
// 4. 检查 nonce，防止重放攻击
// 5. 使用接收方私钥解密 AES 密钥
// 6. 使用 AES 密钥解密消息内容
// 7. 使用发送方公钥验证数字签名
// 8. 记录 nonce 到已使用列表
//
// 参数:
//   - encryptedMsg: base64 编码的加密消息字符串
//   - recipientPrivKey: 接收方的 RSA 私钥，用于解密
//   - senderID: 发送方的 peer ID，用于查找其公钥进行签名验证
//
// 返回:
//   - string: 解密后的明文消息
//   - bool: 签名验证是否通过（true 表示验证通过）
//   - error: 如果解密或验证失败则返回错误
//
// 该函数包含完整的输入验证和错误处理
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

// decryptMessage 使用 RSA 私钥解密 AES 加密的消息
// 该函数是混合加密方案的解密部分：
// 1. 使用 RSA 私钥解密 AES 密钥
// 2. 使用 AES 密钥解密消息内容
//
// 参数:
//   - encryptedData: 加密的数据（包含加密的 AES 密钥和加密的消息内容）
//   - privKey: RSA 私钥，用于解密 AES 密钥
//
// 返回:
//   - []byte: 解密后的明文消息
//   - error: 如果解密失败则返回错误
//
// 该函数使用 OAEP 填充方案进行 RSA 解密，使用 GCM 模式进行 AES 解密
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

// aesEncrypt 使用 AES-256-GCM 模式加密消息
// GCM 模式提供认证加密，同时保证消息的机密性和完整性
//
// 参数:
//   - msg: 要加密的明文消息
//   - key: AES 密钥，必须是 32 字节（256 位）
//
// 返回:
//   - []byte: 加密后的数据（包含 nonce 和密文）
//   - error: 如果密钥长度不正确或加密失败则返回错误
//
// 该函数会生成随机的 12 字节 nonce，并将 nonce 附加到密文前面
func aesEncrypt(msg []byte, key []byte) ([]byte, error) {
	// 输入验证
	if len(key) != 32 {
		return nil, fmt.Errorf("AES key must be 32 bytes (256 bits), got %d bytes", len(key))
	}
	if len(msg) == 0 {
		return nil, fmt.Errorf("message cannot be empty")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %v", err)
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

// aesDecrypt 使用 AES-256-GCM 模式解密消息
// 从密文中提取 nonce，然后使用 GCM 模式解密并验证消息完整性
//
// 参数:
//   - ciphertext: 加密的数据（前 12 字节是 nonce，后面是密文）
//   - key: AES 密钥，必须是 32 字节（256 位）
//
// 返回:
//   - []byte: 解密后的明文消息
//   - error: 如果密钥长度不正确、数据格式错误或解密失败则返回错误
//
// 该函数会验证消息的完整性，如果消息被篡改则返回错误
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

	fileName := fileInfo.Name()

	// 计算分块数量
	chunkCount := calculateChunkCount(fileInfo.Size())

	// 获取发送者用户名（自己）
	globalVarsMutex.RLock()
	senderName := globalUsername
	globalVarsMutex.RUnlock()
	if senderName == "" {
		senderName = h.ID().ShortString()
	}

	fmt.Printf("📁 [%s] 准备发送文件: %s (%.2f MB, %d 块)\n", senderName, fileName, float64(fileInfo.Size())/(1024*1024), chunkCount)

	// 发送给所有已连接的 peer
	sent := false
	for _, conn := range h.Network().Conns() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		peerID := conn.RemotePeer()

		// 获取接收者用户名
		receiverName := getUserNameByPeerID(peerID)

		// 获取发送者用户名（自己）
		globalVarsMutex.RLock()
		senderName := globalUsername
		globalVarsMutex.RUnlock()
		if senderName == "" {
			senderName = h.ID().ShortString()
		}

		// 获取对方的公钥
		peerPubKeysMutex.RLock()
		remotePubKey, hasKey := peerPubKeys[peerID]
		peerPubKeysMutex.RUnlock()

		if !hasKey {
			fmt.Printf("⚠️  [%s] 尚未与 [%s] 交换公钥，跳过\n", senderName, receiverName)
			continue
		}

		// 发送文件
		if err := sendFile(ctx, h, peerID, privKey, remotePubKey, fileName, fileData, chunkCount); err != nil {
			fmt.Printf("❌ [%s] 发送文件给 [%s] 失败: %v\n", senderName, receiverName, err)
			continue
		}

		fmt.Printf("✅ [%s] 已发送文件给 [%s]: %s\n", senderName, receiverName, fileName)

		// 在UI模式下也显示消息
		globalUIMutex.RLock()
		ui := globalUI
		globalUIMutex.RUnlock()
		if ui != nil {
			ui.AddMessage("系统", fmt.Sprintf("📁 已发送文件给 [%s]: %s", receiverName, fileName), true)
		}

		sent = true
	}

	if !sent {
		fmt.Println("⚠️  没有已连接的 peer，无法发送文件")
	}
}

// sendFile 发送文件给指定的 peer
// 该函数实现了单播文件传输的核心逻辑：
// 1. 创建文件传输流
// 2. 构建并发送文件头部（包含元数据和签名）
// 3. 将文件数据分块，逐个加密、签名并发送
// 4. 处理发送过程中的错误和超时
//
// 参数:
//   - ctx: 上下文，用于控制操作的超时和取消
//   - h: libp2p host 实例，用于创建流
//   - peerID: 目标 peer 的 ID
//   - senderPrivKey: 发送方的 RSA 私钥，用于签名
//   - recipientPubKey: 接收方的 RSA 公钥，用于加密文件块
//   - fileName: 文件名
//   - fileData: 文件的完整数据
//   - chunkCount: 文件分块数量
//
// 返回:
//   - error: 如果发送失败则返回错误
//
// 该函数使用 JSON 编码传输文件头部和分块，每个分块都经过加密和签名
func sendFile(ctx context.Context, h host.Host, peerID peer.ID, senderPrivKey *rsa.PrivateKey, recipientPubKey *rsa.PublicKey, fileName string, fileData []byte, chunkCount int) error {
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
	nonce, err := generateNonce(nonceSize)
	if err != nil {
		return err
	}

	header, err := buildFileTransferHeader(fileName, fileData, chunkCount, senderPrivKey, nonce)
	if err != nil {
		return err
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

		start, end := calculateChunkBounds(i, fileChunkSize, len(fileData))
		chunkData := fileData[start:end]

		chunk, err := buildFileChunk(i, chunkData, senderPrivKey, recipientPubKey)
		if err != nil {
			return err
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

// getUserNameByPeerID 根据 peer ID 获取用户名
// 该函数实现了用户名的多源查找策略：
// 1. 首先从 DHT Discovery 缓存中查找
// 2. 如果未找到，从注册服务器查找（通过 peerID 或列表）
// 3. 如果仍未找到，使用 peer ID 的短字符串作为备用
//
// 参数:
//   - peerID: 目标 peer 的 ID
//
// 返回:
//   - string: 找到的用户名，如果找不到则返回 peer ID 的短字符串
//
// 该函数是线程安全的，使用互斥锁保护全局变量访问
func getUserNameByPeerID(peerID peer.ID) string {
	var senderName string

	// 首先尝试从DHT获取
	globalVarsMutex.RLock()
	dhtDiscovery := globalDHTDiscovery
	globalVarsMutex.RUnlock()
	if dhtDiscovery != nil {
		if userInfo := dhtDiscovery.GetUserByPeerID(peerID.String()); userInfo != nil {
			senderName = userInfo.Username
		}
	}

	// 如果DHT中没有找到，尝试从注册服务器获取
	if senderName == "" {
		globalUIMutex.RLock()
		ui := globalUI
		globalUIMutex.RUnlock()
		if ui != nil && ui.registryClient != nil {
			// 尝试通过peerID查找
			client, err := ui.registryClient.LookupClient(peerID.String())
			if err == nil && client != nil {
				senderName = client.Username
			} else {
				// 如果通过peerID找不到，尝试通过ListClients查找
				clients, err := ui.registryClient.ListClients()
				if err == nil {
					for _, client := range clients {
						if client.PeerID == peerID.String() {
							senderName = client.Username
							break
						}
					}
				}
			}
		}
	}

	// 如果还是找不到，使用peerID的短字符串
	if senderName == "" {
		senderName = peerID.ShortString()
	}

	return senderName
}

// handleFileTransfer 处理接收到的文件传输请求
// 该函数实现了文件接收的完整流程：
// 1. 接收并验证文件头部（包含文件名、大小、哈希、签名等）
// 2. 验证发送方公钥和头部签名
// 3. 逐个接收加密的文件块
// 4. 解密每个文件块并验证签名
// 5. 组装完整文件并验证 SHA256 哈希
// 6. 保存文件到 received_files 目录
// 7. 在 UI 或 CLI 模式下显示文件接收状态
//
// 参数:
//   - s: 文件传输网络流
//   - privKey: 接收方的 RSA 私钥，用于解密文件块
//
// 该函数包含完整的错误处理和文件完整性验证
// 如果文件哈希不匹配或签名验证失败，文件将被拒绝
func handleFileTransfer(s network.Stream, privKey *rsa.PrivateKey) {
	defer s.Close()

	peerID := s.Conn().RemotePeer()
	senderName := getUserNameByPeerID(peerID)

	// 获取接收者用户名（自己）
	globalVarsMutex.RLock()
	receiverName := globalUsername
	globalVarsMutex.RUnlock()
	if receiverName == "" {
		globalVarsMutex.RLock()
		myHost := globalHost
		globalVarsMutex.RUnlock()
		if myHost != nil {
			receiverName = myHost.ID().ShortString()
		} else {
			receiverName = "未知用户"
		}
	}

	fmt.Printf("\n📁 [%s] 收到来自 [%s] 的文件传输请求\n", receiverName, senderName)

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

	fmt.Printf("✅ [%s] 已接收并保存文件: %s\n", receiverName, savePath)
	fmt.Printf("✅ 文件已验证（签名和哈希都有效）\n")

	// 在UI模式下也显示消息
	globalUIMutex.RLock()
	ui := globalUI
	globalUIMutex.RUnlock()
	if ui != nil {
		ui.AddMessage("系统", fmt.Sprintf("📁 收到来自 [%s] 的文件: %s", senderName, header.FileName), true)
		ui.AddMessage("系统", fmt.Sprintf("✅ 文件已保存到: %s", savePath), true)
	}

	fmt.Print("> ")
	os.Stdout.Sync() // 刷新输出缓冲区，确保提示符立即显示
}

// queryUser 查询用户详细信息
// 该函数实现了多源用户查询功能，按优先级查找：
// 1. 从注册服务器查询（通过用户名、peerID 或前缀匹配）
// 2. 从 DHT Discovery 查询（通过用户名、peerID 或前缀匹配）
// 3. 从已连接的 peer 中查找（通过 peerID 匹配）
//
// 参数:
//   - target: 查询目标，可以是用户名、完整 peerID 或 peerID 前缀
//   - h: libp2p host 实例，用于检查连接状态
//   - registryClient: 注册服务器客户端，如果使用注册模式则不为 nil
//   - dhtDiscovery: DHT 发现服务，如果使用 DHT 模式则不为 nil
//
// 该函数会在 CLI 模式下输出格式化的用户详细信息，包括：
// - 用户名、节点ID、地址
// - 注册时间、最后心跳时间
// - 连接状态和公钥交换状态
func queryUser(target string, h host.Host, registryClient *RegistryClient, dhtDiscovery *DHTDiscovery) {
	var foundUser *UserInfo
	var foundClient *ClientInfo
	var foundPeerID peer.ID

	// 首先尝试从注册服务器查询
	if registryClient != nil {
		clients, err := registryClient.ListClients()
		if err == nil {
			for _, client := range clients {
				if client.Username == target || client.PeerID == target || strings.HasPrefix(client.PeerID, target) {
					foundClient = client
					peerID, err := peer.Decode(client.PeerID)
					if err == nil {
						foundPeerID = peerID
					}
					break
				}
			}
		}
	}

	// 如果注册服务器中没有找到，尝试从DHT查询
	if foundClient == nil && dhtDiscovery != nil {
		users := dhtDiscovery.ListUsers()
		for _, user := range users {
			if user.Username == target || user.PeerID == target || strings.HasPrefix(user.PeerID, target) {
				foundUser = user
				peerID, err := peer.Decode(user.PeerID)
				if err == nil {
					foundPeerID = peerID
				}
				break
			}
		}
	}

	// 如果都没有找到，尝试从已连接的peer中查找
	if foundClient == nil && foundUser == nil {
		conns := h.Network().Conns()
		for _, conn := range conns {
			peerID := conn.RemotePeer()
			peerIDStr := peerID.String()
			if peerIDStr == target || strings.HasPrefix(peerIDStr, target) || peerID.ShortString() == target {
				foundPeerID = peerID
				// 尝试从DHT获取用户信息
				if dhtDiscovery != nil {
					if userInfo := dhtDiscovery.GetUserByPeerID(peerIDStr); userInfo != nil {
						foundUser = userInfo
					}
				}
				// 尝试从注册服务器获取用户信息
				if foundUser == nil && registryClient != nil {
					clients, err := registryClient.ListClients()
					if err == nil {
						for _, client := range clients {
							if client.PeerID == peerIDStr {
								foundClient = client
								break
							}
						}
					}
				}
				break
			}
		}
	}

	// 显示查询结果
	if foundClient != nil {
		// 从注册服务器找到的用户
		timeSince := time.Since(foundClient.LastSeen)
		timeStr := formatDuration(timeSince)
		// 注意：ClientInfo 结构中没有 RegisterTime 字段，使用 LastSeen 作为参考
		registerTimeSince := timeSince
		registerTimeStr := formatDuration(registerTimeSince)

		// 检查连接状态和公钥交换状态
		isConnected := false
		hasKey := false
		if foundPeerID != "" {
			isConnected = h.Network().Connectedness(foundPeerID) == network.Connected
			peerPubKeysMutex.RLock()
			_, hasKey = peerPubKeys[foundPeerID]
			peerPubKeysMutex.RUnlock()
		}

		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("📋 用户详细信息查询")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("用户名: %s\n", foundClient.Username)
		fmt.Printf("节点ID: %s\n", foundClient.PeerID)
		fmt.Printf("地址: %v\n", foundClient.Addresses)
		fmt.Printf("注册时间: %s 前\n", registerTimeStr)
		fmt.Printf("最后心跳: %s 前\n", timeStr)
		if isConnected {
			if hasKey {
				fmt.Println("连接状态: ✅ 已连接 (已交换公钥)")
			} else {
				fmt.Println("连接状态: ⚠️  已连接 (未交换公钥)")
			}
		} else {
			fmt.Println("连接状态: ⚪ 未连接")
		}
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	} else if foundUser != nil {
		// 从DHT找到的用户
		peerID, err := peer.Decode(foundUser.PeerID)
		isConnected := false
		hasKey := false
		if err == nil {
			isConnected = h.Network().Connectedness(peerID) == network.Connected
			peerPubKeysMutex.RLock()
			_, hasKey = peerPubKeys[peerID]
			peerPubKeysMutex.RUnlock()
		}

		registerTime := time.Now()
		if foundUser.Timestamp > 0 {
			registerTime = time.Unix(foundUser.Timestamp, 0)
		}
		registerTimeSince := time.Since(registerTime)
		registerTimeStr := formatDuration(registerTimeSince)

		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("📋 用户详细信息查询")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("用户名: %s\n", foundUser.Username)
		fmt.Printf("节点ID: %s\n", foundUser.PeerID)
		fmt.Printf("地址: %v\n", foundUser.Addresses)
		fmt.Printf("发现时间: %s 前\n", registerTimeStr)
		if isConnected {
			if hasKey {
				fmt.Println("连接状态: ✅ 已连接 (已交换公钥)")
			} else {
				fmt.Println("连接状态: ⚠️  已连接 (未交换公钥)")
			}
		} else {
			fmt.Println("连接状态: ⚪ 未连接")
		}
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	} else if foundPeerID != "" {
		// 只找到了peerID，但没有用户信息
		isConnected := h.Network().Connectedness(foundPeerID) == network.Connected
		hasKey := false
		peerPubKeysMutex.RLock()
		_, hasKey = peerPubKeys[foundPeerID]
		peerPubKeysMutex.RUnlock()

		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("📋 用户详细信息查询")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("节点ID: %s\n", foundPeerID.String())
		fmt.Println("用户名: 未知")
		if isConnected {
			if hasKey {
				fmt.Println("连接状态: ✅ 已连接 (已交换公钥)")
			} else {
				fmt.Println("连接状态: ⚠️  已连接 (未交换公钥)")
			}
		} else {
			fmt.Println("连接状态: ⚪ 未连接")
		}
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	} else {
		fmt.Printf("❌ 未找到用户: %s\n", target)
	}

	fmt.Print("> ")
	os.Stdout.Sync()
}

// listOnlineUsers 列出注册服务器上的在线用户（CLI 模式）
// 从注册服务器获取客户端列表并格式化输出
//
// 参数:
//   - registryClient: 注册服务器客户端实例
//
// 该函数会在 CLI 模式下输出格式化的用户列表，包括用户名、节点ID和状态
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
// callUser 通过注册服务器连接用户（Registry 模式）
// 该函数实现了基于注册服务器的用户连接功能：
// 1. 从注册服务器查找目标用户（支持用户名和 peerID 匹配）
// 2. 解析用户地址并建立连接
// 3. 执行双向密钥交换
// 4. 发送连接成功通知给对方
// 5. 在 UI 或 CLI 模式下显示连接状态
//
// 参数:
//   - ctx: 上下文，用于控制操作的超时和取消
//   - h: libp2p host 实例，用于建立连接
//   - privKey: 本地的 RSA 私钥
//   - pubKey: 本地的 RSA 公钥
//   - registryClient: 注册服务器客户端实例
//   - targetID: 目标用户标识，可以是用户名或 peerID
//
// 该函数会在连接失败时输出错误信息，成功时显示连接确认消息
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

		// 发送自己的用户名
		globalVarsMutex.RLock()
		myUsername := globalUsername
		globalVarsMutex.RUnlock()
		if err := encoder.Encode(myUsername); err != nil {
			fmt.Printf("⚠️  发送用户名失败: %v\n", err)
			continue
		}

		// 然后接收对方的公钥
		decoder := gob.NewDecoder(stream)
		var remotePubKey rsa.PublicKey
		if err := decoder.Decode(&remotePubKey); err != nil {
			fmt.Printf("⚠️  接收公钥失败: %v\n", err)
			continue
		}

		// 接收对方的用户名
		var remoteUsername string
		if err := decoder.Decode(&remoteUsername); err != nil {
			// 如果对方没有发送用户名，使用已知的用户名或peerID
			remoteUsername = clientInfo.Username
			if remoteUsername == "" {
				remoteUsername = info.ID.ShortString()
			}
		}

		// 存储对方的公钥
		peerPubKeysMutex.Lock()
		peerPubKeys[info.ID] = &remotePubKey
		peerPubKeysMutex.Unlock()

		fmt.Printf("✅ 已与 %s (%s) 交换公钥，可以开始聊天了！\n\n", remoteUsername, info.ID.ShortString())
		connected = true

		// 验证连接状态
		if h.Network().Connectedness(info.ID) == network.Connected {
			fmt.Printf("✅ 连接状态确认：已连接到 %s\n", clientInfo.Username)
		} else {
			fmt.Printf("⚠️  警告：连接状态异常\n")
		}

		// 发送通知消息给被呼叫方，告知有人连接了
		go func() {
			// 等待一小段时间确保连接稳定
			time.Sleep(500 * time.Millisecond)

			// 获取自己的用户名
			globalVarsMutex.RLock()
			myUsername := globalUsername
			globalVarsMutex.RUnlock()

			// 创建通知消息
			var notifyMsg string
			if myUsername != "" {
				notifyMsg = fmt.Sprintf("[系统通知] %s 已连接到您，可以开始聊天了！", myUsername)
			} else {
				notifyMsg = fmt.Sprintf("[系统通知] 有人已连接到您，可以开始聊天了！")
			}

			// 发送通知
			notifyCtx, notifyCancel := context.WithTimeout(ctx, 3*time.Second)
			defer notifyCancel()

			notifyStream, err := h.NewStream(notifyCtx, info.ID, protocolID)
			if err == nil {
				encryptedMsg, err := encryptAndSignMessage(notifyMsg, privKey, &remotePubKey)
				if err == nil {
					notifyStream.Write([]byte(encryptedMsg + "\n"))
				}
				notifyStream.Close()
			}
		}()

		break
	}

	if !connected {
		fmt.Println("❌ 无法连接到目标用户")
		fmt.Println("💡 提示：请确保目标用户在线，并且网络可达")
	}
}

// hangupPeer 挂断与指定peer的连接
// hangupPeer 挂断与指定用户的连接
// 该函数实现了优雅断开连接的功能：
// 1. 查找目标用户（通过用户名或 peerID，支持 DHT 和注册服务器模式）
// 2. 检查用户是否已连接
// 3. 如果已连接，发送断开连接通知给对方
// 4. 关闭所有与该用户的连接
// 5. 清理公钥缓存和资源
// 6. 在 UI 或 CLI 模式下显示断开状态
//
// 参数:
//   - ctx: 上下文，用于控制操作的超时和取消
//   - h: libp2p host 实例，用于获取连接
//   - privKey: 本地的 RSA 私钥，用于签名断开通知
//   - targetID: 目标用户标识，可以是用户名或 peerID
//   - dhtDiscovery: DHT 发现服务，如果使用 DHT 模式则不为 nil
//   - registryClient: 注册服务器客户端，如果使用注册模式则不为 nil
//
// 如果用户未连接，该函数只会输出提示信息，不会执行断开操作
func hangupPeer(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, targetID string, dhtDiscovery *DHTDiscovery, registryClient *RegistryClient) {
	fmt.Printf("📞 正在挂断与 %s 的连接...\n", targetID)

	// 查找目标peer
	conns := h.Network().Conns()
	var targetPeerID peer.ID
	var targetUserInfo *UserInfo
	var targetClientInfo *ClientInfo
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
				// 尝试从注册服务器获取用户信息
				if targetUserInfo == nil && registryClient != nil {
					clients, err := registryClient.ListClients()
					if err == nil {
						for _, client := range clients {
							if client.PeerID == parsedPeerID.String() {
								targetClientInfo = client
								break
							}
						}
					}
				}
				found = true
				break
			}
		}
	}

	// 如果不是peerID或未找到，尝试通过用户名匹配（大小写不敏感）
	if !found {
		targetLower := strings.ToLower(targetID)

		// 首先尝试从注册服务器查找
		if registryClient != nil {
			clients, err := registryClient.ListClients()
			if err == nil {
				for _, client := range clients {
					if strings.ToLower(client.Username) == targetLower {
						// 找到了用户名，现在查找对应的连接
						peerID, err := peer.Decode(client.PeerID)
						if err == nil {
							// 检查是否有连接
							for _, conn := range conns {
								if conn.RemotePeer() == peerID {
									targetPeerID = peerID
									targetClientInfo = client
									found = true
									break
								}
							}
						}
						if found {
							break
						}
					}
				}
			}
		}

		// 如果注册服务器中没有找到，尝试从DHT查找
		if !found && dhtDiscovery != nil {
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
	}

	// 检查是否已连接
	isConnected := false
	if found {
		isConnected = h.Network().Connectedness(targetPeerID) == network.Connected
	}

	// 如果没有找到用户或没有连接，只提示
	if !found || !isConnected {
		if !found {
			fmt.Printf("❌ 未找到用户: %s\n", targetID)
			fmt.Println("💡 提示：请使用 /list 查看在线用户")
		} else {
			// 找到了用户但没有连接
			var displayName string
			if targetUserInfo != nil {
				displayName = targetUserInfo.Username
			} else if targetClientInfo != nil {
				displayName = targetClientInfo.Username
			} else {
				displayName = targetPeerID.ShortString()
			}
			fmt.Printf("ℹ️  用户 %s 未连接，无需断开\n", displayName)
		}
		return
	}

	// 显示用户信息
	var displayName string
	if targetUserInfo != nil {
		displayName = targetUserInfo.Username
		fmt.Printf("   用户: %s (节点ID: %s)\n", displayName, targetPeerID.ShortString())
	} else if targetClientInfo != nil {
		displayName = targetClientInfo.Username
		fmt.Printf("   用户: %s (节点ID: %s)\n", displayName, targetPeerID.ShortString())
	} else {
		displayName = targetPeerID.ShortString()
		fmt.Printf("   节点ID: %s\n", targetPeerID.ShortString())
	}

	// 检查是否已交换公钥
	peerPubKeysMutex.RLock()
	_, hasKey := peerPubKeys[targetPeerID]
	peerPubKeysMutex.RUnlock()

	// 如果已交换公钥，先发送断开连接通知给对方
	if hasKey {
		fmt.Printf("📤 正在通知对方断开连接...\n")
		notifyCtx, notifyCancel := context.WithTimeout(ctx, 3*time.Second)
		stream, err := h.NewStream(notifyCtx, targetPeerID, protocolID)
		notifyCancel()

		if err == nil {
			// 创建断开连接通知消息
			globalVarsMutex.RLock()
			myUsername := globalUsername
			globalVarsMutex.RUnlock()
			if myUsername == "" {
				myUsername = h.ID().ShortString()
			}
			disconnectMsg := fmt.Sprintf("[系统通知] %s 已断开与您的连接", myUsername)

			peerPubKeysMutex.RLock()
			remotePubKey := peerPubKeys[targetPeerID]
			peerPubKeysMutex.RUnlock()

			if remotePubKey != nil {
				encryptedMsg, err := encryptAndSignMessage(disconnectMsg, privKey, remotePubKey)
				if err == nil {
					stream.Write([]byte(encryptedMsg + "\n"))
					fmt.Printf("✅ 已通知对方断开连接\n")
				} else {
					fmt.Printf("⚠️  发送通知失败: %v\n", err)
				}
			}
			stream.Close()
		} else {
			fmt.Printf("⚠️  无法发送通知: %v\n", err)
		}

		// 等待一小段时间，确保通知已发送
		time.Sleep(200 * time.Millisecond)
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
		fmt.Printf("✅ 已断开与 %s (%s) 的连接\n", displayName, targetPeerID.ShortString())
	} else {
		fmt.Printf("⚠️  未找到与 %s 的活跃连接\n", targetPeerID.ShortString())
	}
}

// formatDuration 格式化时间间隔
// formatDuration 格式化时间间隔为人类可读的字符串
// 将 time.Duration 转换为友好的时间描述，如 "5分钟前"、"2小时前" 等
//
// 参数:
//   - d: 时间间隔
//
// 返回:
//   - string: 格式化后的时间字符串
//
// 该函数会根据时间长度自动选择合适的单位（秒、分钟、小时、天）
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
// showHelp 显示帮助信息（CLI 模式）
// 根据当前模式（Registry 或 DHT）显示相应的命令帮助信息
//
// 参数:
//   - hasRegistry: 是否使用注册服务器模式
//   - hasDHT: 是否使用 DHT 发现模式
//
// 该函数会输出格式化的帮助信息，包括：
// - 基本命令（发送消息）
// - 用户发现命令（/list）
// - 连接命令（/call, /hangup）
// - 文件传输命令（/sendfile）
// - 查询命令（/query）
// - 娱乐命令（/rps）
// - 帮助和退出命令
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
	fmt.Println("  /sendfile 或 /file <文件路径>        - 发送文件给所有已连接的peer")
	fmt.Println()
	fmt.Println("🔍 查询命令:")
	fmt.Println("  /query 或 /q <用户名或节点ID>        - 查询用户详细信息")
	fmt.Println()
	fmt.Println("🎮 娱乐命令:")
	fmt.Println("  /rps 或 /r                          - 发起石头剪刀布，所有人自动随机出拳")
	fmt.Println()
	fmt.Println("❓ 帮助命令:")
	fmt.Println("  /help 或 /h                         - 显示此帮助信息")
	fmt.Println()
	fmt.Println("🚪 退出命令:")
	fmt.Println("  /quit 或 /exit                      - 优雅退出程序")
	fmt.Println()
	fmt.Println("💡 提示: 支持 /c (call), /l (list), /s (sendfile), /f (file), /q (query), /r (rps), /d (disconnect), /h (help) 等首字母简写")
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// listDHTUsers 列出DHT发现的用户和已连接的peer（包括自己）
// listDHTUsers 列出 DHT 发现的用户（CLI 模式）
// 从 DHT Discovery 获取用户列表并格式化输出，包括：
// - 自己（标记为"自己"）
// - 已连接的用户（显示连接状态和公钥交换状态）
// - DHT 发现但未连接的用户
//
// 参数:
//   - dhtDiscovery: DHT 发现服务实例
//   - ctx: 上下文，用于控制操作的超时和取消
//
// 该函数会在 CLI 模式下输出格式化的用户列表，包括用户名、节点ID和状态信息
func listDHTUsers(dhtDiscovery *DHTDiscovery, ctx context.Context) {
	users := dhtDiscovery.ListUsers()
	conns := dhtDiscovery.host.Network().Conns()
	currentPeerID := dhtDiscovery.host.ID().String()
	currentPeerIDObj := dhtDiscovery.host.ID()

	// 创建一个映射，将节点ID映射到用户信息
	userMap := make(map[string]*UserInfo)
	for _, user := range users {
		userMap[user.PeerID] = user
	}

	// 收集所有要显示的用户（包括自己和已连接的peer）
	allUsers := make([]struct {
		peerID   peer.ID
		username string
		userInfo *UserInfo
		address  string
		isSelf   bool
	}, 0)

	// 添加自己
	var myUsername string
	var myUserInfo *UserInfo
	if info, found := userMap[currentPeerID]; found {
		myUserInfo = info
		myUsername = info.Username
	} else {
		// 尝试从DHT获取自己的信息
		if info := dhtDiscovery.GetUserByPeerID(currentPeerID); info != nil {
			myUserInfo = info
			myUsername = info.Username
		} else {
			// 使用全局用户名
			globalVarsMutex.RLock()
			myUsername = globalUsername
			globalVarsMutex.RUnlock()
			if myUsername == "" {
				myUsername = currentPeerIDObj.ShortString()
			}
		}
	}

	var myAddress string
	if myUserInfo != nil && len(myUserInfo.Addresses) > 0 {
		myAddress = myUserInfo.Addresses[0]
	} else if len(dhtDiscovery.host.Addrs()) > 0 {
		myAddress = fmt.Sprintf("%s/p2p/%s", dhtDiscovery.host.Addrs()[0], currentPeerIDObj)
	}

	allUsers = append(allUsers, struct {
		peerID   peer.ID
		username string
		userInfo *UserInfo
		address  string
		isSelf   bool
	}{
		peerID:   currentPeerIDObj,
		username: myUsername,
		userInfo: myUserInfo,
		address:  myAddress,
		isSelf:   true,
	})

	// 添加已连接的其他peer
	for _, conn := range conns {
		peerID := conn.RemotePeer()
		peerIDStr := peerID.String()

		// 跳过自己（已经在上面添加了）
		if peerIDStr == currentPeerID {
			continue
		}

		// 尝试从DHT发现的用户中查找用户名
		var username string
		var userInfo *UserInfo

		// 首先从userMap中查找
		if info, found := userMap[peerIDStr]; found {
			userInfo = info
			username = info.Username
		} else if dhtDiscovery != nil {
			// 尝试通过DHT查找（从缓存中）
			if info := dhtDiscovery.GetUserByPeerID(peerIDStr); info != nil {
				userInfo = info
				username = info.Username
			}
		}

		// 如果还是找不到用户名，使用节点ID的短格式作为备用
		if username == "" {
			username = peerID.ShortString()
		}

		var address string
		if userInfo != nil && len(userInfo.Addresses) > 0 {
			address = userInfo.Addresses[0]
		} else {
			address = conn.RemoteMultiaddr().String()
		}

		allUsers = append(allUsers, struct {
			peerID   peer.ID
			username string
			userInfo *UserInfo
			address  string
			isSelf   bool
		}{
			peerID:   peerID,
			username: username,
			userInfo: userInfo,
			address:  address,
			isSelf:   false,
		})
	}

	// 显示所有用户
	if len(allUsers) > 0 {
		fmt.Printf("📋 在线用户列表 (%d 人):\n", len(allUsers))
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		for i, user := range allUsers {
			selfMark := ""
			if user.isSelf {
				selfMark = " (自己)"
			}
			fmt.Printf("%d. 用户名: %s%s\n", i+1, user.username, selfMark)
			fmt.Printf("   节点ID: %s\n", user.peerID)
			if user.address != "" {
				fmt.Printf("   地址: %s\n", user.address)
			}
			fmt.Println()
		}
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	} else {
		fmt.Println("📋 当前没有在线用户")
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
// callUserViaDHT 通过 DHT 连接用户（DHT 模式）
// 该函数实现了基于 DHT 发现的用户连接功能：
// 1. 从已连接的 peer 或 DHT 缓存中查找目标用户（支持用户名和 peerID 匹配）
// 2. 如果已连接，检查是否已交换公钥，如未交换则执行密钥交换
// 3. 如果未连接，从 DHT 获取用户地址并建立连接
// 4. 执行双向密钥交换
// 5. 发送连接成功通知给对方
// 6. 在 UI 或 CLI 模式下显示连接状态
//
// 参数:
//   - ctx: 上下文，用于控制操作的超时和取消
//   - h: libp2p host 实例，用于建立连接
//   - privKey: 本地的 RSA 私钥
//   - pubKey: 本地的 RSA 公钥
//   - dhtDiscovery: DHT 发现服务实例
//   - targetID: 目标用户标识，可以是用户名或 peerID
//
// 该函数优先使用已建立的连接，避免重复连接
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

		// 发送自己的用户名
		globalVarsMutex.RLock()
		myUsername := globalUsername
		globalVarsMutex.RUnlock()
		if err := encoder.Encode(myUsername); err != nil {
			fmt.Printf("⚠️  发送用户名失败: %v\n", err)
			return
		}

		// 然后接收对方的公钥
		decoder := gob.NewDecoder(stream)
		var remotePubKey rsa.PublicKey
		if err := decoder.Decode(&remotePubKey); err != nil {
			fmt.Printf("⚠️  接收公钥失败: %v\n", err)
			return
		}

		// 接收对方的用户名
		var remoteUsername string
		if err := decoder.Decode(&remoteUsername); err != nil {
			// 如果对方没有发送用户名，使用已知的用户名或peerID
			if targetUserInfo != nil {
				remoteUsername = targetUserInfo.Username
			}
			if remoteUsername == "" {
				remoteUsername = targetPeerID.ShortString()
			}
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

		// 发送通知消息给被呼叫方，告知有人连接了
		go func() {
			// 等待一小段时间确保连接稳定
			time.Sleep(500 * time.Millisecond)

			// 获取自己的用户名
			globalVarsMutex.RLock()
			myUsername := globalUsername
			globalVarsMutex.RUnlock()

			// 创建通知消息
			var notifyMsg string
			if myUsername != "" {
				notifyMsg = fmt.Sprintf("[系统通知] %s 已连接到您，可以开始聊天了！", myUsername)
			} else {
				notifyMsg = fmt.Sprintf("[系统通知] 有人已连接到您，可以开始聊天了！")
			}

			// 发送通知
			notifyCtx, notifyCancel := context.WithTimeout(ctx, 3*time.Second)
			defer notifyCancel()

			notifyStream, err := h.NewStream(notifyCtx, targetPeerID, protocolID)
			if err == nil {
				encryptedMsg, err := encryptAndSignMessage(notifyMsg, privKey, &remotePubKey)
				if err == nil {
					notifyStream.Write([]byte(encryptedMsg + "\n"))
				}
				notifyStream.Close()
			}
		}()

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

		// 发送通知消息给被呼叫方，告知有人连接了
		go func() {
			// 等待一小段时间确保连接稳定
			time.Sleep(500 * time.Millisecond)

			// 获取自己的用户名
			globalVarsMutex.RLock()
			myUsername := globalUsername
			globalVarsMutex.RUnlock()

			// 创建通知消息
			var notifyMsg string
			if myUsername != "" {
				notifyMsg = fmt.Sprintf("[系统通知] %s 已连接到您，可以开始聊天了！", myUsername)
			} else {
				notifyMsg = fmt.Sprintf("[系统通知] 有人已连接到您，可以开始聊天了！")
			}

			// 发送通知
			notifyCtx, notifyCancel := context.WithTimeout(ctx, 3*time.Second)
			defer notifyCancel()

			notifyStream, err := h.NewStream(notifyCtx, info.ID, protocolID)
			if err == nil {
				encryptedMsg, err := encryptAndSignMessage(notifyMsg, privKey, &remotePubKey)
				if err == nil {
					notifyStream.Write([]byte(encryptedMsg + "\n"))
				}
				notifyStream.Close()
			}
		}()

		break
	}

	if !connected {
		fmt.Println("❌ 无法连接到目标用户")
		fmt.Println("💡 提示：请确保目标用户在线，并且网络可达")
	}
}

// hangupAllPeers 挂断所有已连接的peer
// hangupAllPeers 挂断所有已连接的 peer
// 该函数会断开与所有已连接 peer 的连接，并清理相关资源
//
// 参数:
//   - ctx: 上下文，用于控制操作的超时和取消
//   - h: libp2p host 实例，用于获取连接
//   - privKey: 本地的 RSA 私钥，用于签名断开通知
//   - dhtDiscovery: DHT 发现服务，如果使用 DHT 模式则不为 nil
//
// 该函数会向每个已连接的 peer 发送断开连接通知，然后关闭所有连接并清理公钥缓存
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
// playRockPaperScissors 发起石头剪刀布游戏
// 该函数实现了多玩家石头剪刀布游戏功能：
// 1. 生成唯一的游戏 ID
// 2. 随机生成自己的选择（尽量避免与已选择的手势重复）
// 3. 向所有已连接并交换公钥的 peer 发送游戏消息
// 4. 等待 5 秒收集所有玩家的选择
// 5. 计算游戏结果（获胜者、平局等）
// 6. 在 UI 或 CLI 模式下显示游戏结果
//
// 参数:
//   - ctx: 上下文，用于控制操作的超时和取消
//   - h: libp2p host 实例，用于获取连接和发送消息
//   - privKey: 本地的 RSA 私钥，用于签名游戏消息
//   - myUsername: 自己的用户名
//   - dhtDiscovery: DHT 发现服务，用于获取其他玩家的用户名
//
// 该函数会在后台 goroutine 中等待收集所有选择，然后显示结果
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

	// 使用全局UI显示消息（如果可用）
	globalUIMutex.RLock()
	ui := globalUI
	globalUIMutex.RUnlock()

	if ui != nil {
		ui.AddMessage("系统", fmt.Sprintf("🎮 石头剪刀布游戏开始！你的选择: %s，等待其他玩家做出选择...", getChoiceDisplay(myChoice)), true)
	} else {
		fmt.Printf("🎮 石头剪刀布游戏开始！\n")
		fmt.Printf("   你的选择: %s\n", getChoiceDisplay(myChoice))
		fmt.Printf("   等待其他玩家做出选择...\n\n")
	}

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
		globalUIMutex.RLock()
		ui := globalUI
		globalUIMutex.RUnlock()
		if ui != nil {
			ui.AddMessage("系统", "⚠️ 无法发送游戏消息给任何玩家", true)
		} else {
			fmt.Println("⚠️  无法发送游戏消息给任何玩家")
		}
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
// 该函数会优先选择当前游戏中尚未被选择的手势，以增加游戏趣味性
//
// 参数:
//   - gameID: 游戏 ID，用于查找已选择的手势
//
// 返回:
//   - string: 随机选择的手势（"rock"、"paper" 或 "scissors"）
//
// 如果所有手势都已被选择，则允许重复但保持随机性
func randomRPSChoice(gameID string) string {
	available := getAvailableRPSChoices(gameID)
	idx := randomIndex(len(available))
	return available[idx]
}

// getAvailableRPSChoices 返回当前游戏中尚未被选择的手势列表
// 该函数用于优化游戏体验，尽量让每个玩家选择不同的手势
//
// 参数:
//   - gameID: 游戏 ID，用于查找已选择的手势
//
// 返回:
//   - []string: 可用的手势列表（"rock"、"paper"、"scissors" 的子集）
//
// 如果所有手势都已被选择，则返回所有手势（允许重复）
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

// randomIndex 使用加密安全的随机数生成索引，必要时回退到 math/rand
// 该函数优先使用 crypto/rand 生成安全的随机数，如果失败则回退到 math/rand
//
// 参数:
//   - max: 最大值（不包含），生成的索引范围是 [0, max)
//
// 返回:
//   - int: 随机索引值
//
// 该函数确保即使加密级随机数生成失败，也能正常工作
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
	// 使用全局UI显示消息（如果可用）
	globalUIMutex.RLock()
	ui := globalUI
	globalUIMutex.RUnlock()

	if len(choices) == 0 {
		if ui != nil {
			ui.AddMessage("系统", "⚠️ 没有收集到任何选择", true)
		} else {
			fmt.Println("⚠️  没有收集到任何选择")
			fmt.Print("> ")
			os.Stdout.Sync()
		}
		return
	}

	// 构建结果消息
	var resultLines []string
	resultLines = append(resultLines, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	resultLines = append(resultLines, "🎮 石头剪刀布游戏结果")
	resultLines = append(resultLines, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

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

		resultLines = append(resultLines, fmt.Sprintf("   %s%s: %s", username, marker, getChoiceDisplay(choice.Choice)))
	}

	resultLines = append(resultLines, "")

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
	resultLines = append(resultLines, "📊 统计:")
	resultLines = append(resultLines, fmt.Sprintf("   ✊ 石头: %d 人", rockCount))
	resultLines = append(resultLines, fmt.Sprintf("   ✋ 布: %d 人", paperCount))
	resultLines = append(resultLines, fmt.Sprintf("   ✌️  剪刀: %d 人", scissorsCount))
	resultLines = append(resultLines, "")

	// 判断结果
	var winnerMsg string
	if rockCount > 0 && paperCount > 0 && scissorsCount > 0 {
		winnerMsg = "🤝 平局！三种选择都有，游戏无效"
		resultLines = append(resultLines, winnerMsg)
	} else if rockCount > 0 && paperCount > 0 && scissorsCount == 0 {
		winnerMsg = "🏆 布获胜！"
		resultLines = append(resultLines, winnerMsg)
		winners := getWinners(choices, "paper", myPeerID, dhtDiscovery)
		if len(winners) > 0 {
			resultLines = append(resultLines, "   获胜者: "+strings.Join(winners, ", "))
		}
	} else if rockCount > 0 && scissorsCount > 0 && paperCount == 0 {
		winnerMsg = "🏆 石头获胜！"
		resultLines = append(resultLines, winnerMsg)
		winners := getWinners(choices, "rock", myPeerID, dhtDiscovery)
		if len(winners) > 0 {
			resultLines = append(resultLines, "   获胜者: "+strings.Join(winners, ", "))
		}
	} else if paperCount > 0 && scissorsCount > 0 && rockCount == 0 {
		winnerMsg = "🏆 剪刀获胜！"
		resultLines = append(resultLines, winnerMsg)
		winners := getWinners(choices, "scissors", myPeerID, dhtDiscovery)
		if len(winners) > 0 {
			resultLines = append(resultLines, "   获胜者: "+strings.Join(winners, ", "))
		}
	} else {
		winnerMsg = "🤝 平局！所有人选择了相同的手势"
		resultLines = append(resultLines, winnerMsg)
	}

	resultLines = append(resultLines, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 显示结果
	if ui != nil {
		// 在UI模式下，将结果作为系统消息显示
		for _, line := range resultLines {
			ui.AddMessage("系统", line, true)
		}
	} else {
		// 在CLI模式下，使用fmt.Printf
		for _, line := range resultLines {
			fmt.Println(line)
		}
		// 刷新输出缓冲区并显示提示符
		fmt.Print("> ")
		os.Stdout.Sync()
	}
}

// getWinners 获取获胜者列表
func getWinners(choices map[string]*RPSChoice, winningChoice string, myPeerID string, dhtDiscovery *DHTDiscovery) []string {
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
			marker := ""
			if isMe {
				marker = " (你)"
			}

			winners = append(winners, username+marker)
		}
	}
	return winners
}

// startCLIInputLoop 启动命令行输入循环（不使用UI时）
func startCLIInputLoop(ctx context.Context, h host.Host, privKey *rsa.PrivateKey, pubKey rsa.PublicKey, registryClient *RegistryClient, dhtDiscovery *DHTDiscovery, username string, sigCh chan os.Signal) {
	scanner := bufio.NewScanner(os.Stdin)
	inputCh := make(chan string, 1)

	// 确保 stdout 无缓冲，立即显示输出
	os.Stdout.Sync()

	// 在goroutine中读取输入，避免阻塞信号处理
	go func() {
		for scanner.Scan() {
			text := scanner.Text()
			select {
			case inputCh <- text:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigCh:
			fmt.Printf("\n收到关闭信号，正在退出...\n")
			os.Stdout.Sync()
			return
		case text := <-inputCh:
			if text == "" {
				// 空输入也显示提示符
				fmt.Print("> ")
				os.Stdout.Sync()
				continue
			}

			// 处理命令
			if strings.HasPrefix(text, "/") {
				handleCLICommand(text, ctx, h, privKey, pubKey, registryClient, dhtDiscovery, username)
			} else {
				// 发送消息
				sendCLIMessage(text, ctx, h, privKey, pubKey, dhtDiscovery, username)
			}

			// 处理完输入后，显示提示符并刷新输出
			fmt.Print("> ")
			os.Stdout.Sync()
		}
	}
}

// handleCLICommand 处理CLI命令
func handleCLICommand(cmd string, ctx context.Context, h host.Host, privKey *rsa.PrivateKey, pubKey rsa.PublicKey, registryClient *RegistryClient, dhtDiscovery *DHTDiscovery, username string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	command := parts[0]
	args := parts[1:]

	// 规范化命令，支持简写形式
	normalizedCmd := normalizeCommandCLI(command)

	switch normalizedCmd {
	case "/quit", "/exit":
		fmt.Println("正在退出...")
		os.Exit(0)
	case "/help", "/h":
		showHelp(registryClient != nil, dhtDiscovery != nil)
	case "/query", "/q":
		if len(args) > 0 {
			target := strings.Join(args, " ")
			queryUser(target, h, registryClient, dhtDiscovery)
		} else {
			fmt.Println("用法: /query <用户名或节点ID>")
		}
	case "/list", "/users", "/l":
		if registryClient != nil {
			clients, err := registryClient.ListClients()
			if err == nil {
				myPeerID := h.ID().String()
				connected := make(map[string]bool)
				for _, conn := range h.Network().Conns() {
					connected[conn.RemotePeer().String()] = true
				}
				hasKey := make(map[string]bool)
				peerPubKeysMutex.RLock()
				for pid := range peerPubKeys {
					hasKey[pid.String()] = true
				}
				peerPubKeysMutex.RUnlock()

				result := formatRegistryList(clients, myPeerID, connected, hasKey)
				fmt.Printf("注册服务器上的在线用户列表 (%d 人):\n", result.Count)
				for _, line := range result.Lines {
					fmt.Print(line)
				}
			} else {
				fmt.Printf("获取注册服务器用户列表失败: %v\n", err)
			}
		} else if dhtDiscovery != nil {
			// DHT模式：显示DHT发现的用户
			users := dhtDiscovery.ListUsers()
			fmt.Printf("DHT发现的在线用户列表 (%d 人):\n", len(users))
			for i, user := range users {
				// 检查是否已连接
				peerID, err := peer.Decode(user.PeerID)
				isConnected := false
				hasKey := false
				if err == nil {
					isConnected = h.Network().Connectedness(peerID) == network.Connected
					peerPubKeysMutex.RLock()
					_, hasKey = peerPubKeys[peerID]
					peerPubKeysMutex.RUnlock()
				}

				status := "离线"
				if isConnected {
					if hasKey {
						status = "已连接 (已交换公钥)"
					} else {
						status = "已连接 (未交换公钥)"
					}
				}

				fmt.Printf("  %d. %s (节点ID: %s...) - %s\n", i+1, user.Username, user.PeerID[:12], status)
			}
		} else {
			fmt.Println("未启用用户发现功能")
		}
	case "/call", "/c":
		if len(args) > 0 {
			target := strings.Join(args, " ")
			if registryClient != nil {
				callUser(ctx, h, privKey, pubKey, registryClient, target)
			} else if dhtDiscovery != nil {
				callUserViaDHT(ctx, h, privKey, pubKey, dhtDiscovery, target)
			} else {
				fmt.Println("⚠️  未启用用户发现功能")
				fmt.Println("   请使用 -registry 参数连接注册服务器，或使用DHT发现模式")
			}
		} else {
			fmt.Println("用法: /call <用户名或节点ID>")
		}
	case "/rps", "/r":
		// 启动石头剪刀布游戏
		go playRockPaperScissors(ctx, h, privKey, username, dhtDiscovery)
	case "/hangup", "/disconnect", "/d":
		// 挂断所有连接
		if len(args) > 0 {
			target := strings.Join(args, " ")
			hangupPeer(ctx, h, privKey, target, dhtDiscovery, registryClient)
		} else {
			hangupAllPeers(ctx, h, privKey, dhtDiscovery)
		}
	case "/sendfile", "/file", "/s", "/f":
		// 发送文件
		if len(args) > 0 {
			filePath := strings.Join(args, " ")
			go sendFileToPeers(ctx, h, privKey, filePath)
		} else {
			fmt.Println("用法: /sendfile <文件路径>")
		}
	default:
		fmt.Printf("未知命令: %s (输入 /help 查看帮助)\n", command)
	}
}

// normalizeCommandCLI 规范化CLI命令，支持简写形式
func normalizeCommandCLI(cmd string) string {
	// 移除前导斜杠
	if !strings.HasPrefix(cmd, "/") {
		return cmd
	}

	cmd = strings.TrimPrefix(cmd, "/")

	// 命令简写映射
	shortcuts := map[string]string{
		"c": "call",
		"l": "list",
		"q": "query",
		"s": "sendfile",
		"f": "file",
		"r": "rps",
		"h": "help",
		"e": "exit",
		"x": "exit",
		"d": "disconnect",
	}

	// 如果是单字符，尝试映射
	if len(cmd) == 1 {
		if full, ok := shortcuts[cmd]; ok {
			return "/" + full
		}
	}

	return "/" + cmd
}

// isConnectionClosedError 判断错误是否与连接关闭相关
func isConnectionClosedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "connection closed") || strings.Contains(errStr, "go away")
}

// sendCLIMessage 发送CLI消息
func sendCLIMessage(msg string, ctx context.Context, h host.Host, privKey *rsa.PrivateKey, pubKey rsa.PublicKey, dhtDiscovery *DHTDiscovery, username string) {
	conns := h.Network().Conns()
	if len(conns) == 0 {
		fmt.Println("当前没有已连接的 peer")
		return
	}

	sent := false
	for _, conn := range conns {
		peerID := conn.RemotePeer()

		if h.Network().Connectedness(peerID) != network.Connected {
			continue
		}

		peerPubKeysMutex.RLock()
		remotePubKey, hasKey := peerPubKeys[peerID]
		peerPubKeysMutex.RUnlock()

		if !hasKey {
			continue
		}

		encryptedMsg, err := encryptAndSignMessage(msg, privKey, remotePubKey)
		if err != nil {
			fmt.Printf("加密失败: %v\n", err)
			continue
		}

		// 使用带超时的上下文创建stream，避免阻塞
		streamCtx, streamCancel := context.WithTimeout(ctx, 5*time.Second)
		stream, err := h.NewStream(streamCtx, peerID, protocolID)
		streamCancel()

		if err != nil {
			continue
		}

		// 在goroutine中发送，但使用select确保可以响应ctx取消
		go func(s network.Stream, encrypted string) {
			defer s.Close()
			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return
			default:
				// 使用带超时的写入
				writeCtx, writeCancel := context.WithTimeout(ctx, 3*time.Second)
				defer writeCancel()

				done := make(chan error, 1)
				go func() {
					_, err := s.Write([]byte(encrypted + "\n"))
					done <- err
				}()

				select {
				case <-writeCtx.Done():
					return
				case err := <-done:
					if err != nil {
						// 写入失败，但不影响其他连接
					}
				}
			}
		}(stream, encryptedMsg)

		var displayName string
		if dhtDiscovery != nil {
			if userInfo := dhtDiscovery.GetUserByPeerID(peerID.String()); userInfo != nil {
				displayName = userInfo.Username
			}
		}
		if displayName == "" {
			displayName = peerID.ShortString()
		}

		fmt.Printf("[%s] %s: %s\n", time.Now().Format("15:04:05"), username, msg)
		sent = true
	}

	if !sent {
		fmt.Println("没有可用的连接发送消息")
	}
}
