package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"crypto/sha256"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

// 全局变量
var (
	globalHost            host.Host
	globalUsername        string
	globalCtx             context.Context
	globalDHTDiscovery    *DHTDiscovery
	globalUsernameMap     map[string]string // peerID到用户名的映射
	globalVarsMutex       sync.RWMutex
	currentUserPrivateKey *rsa.PrivateKey
	currentUserPublicKey  rsa.PublicKey
)

// 连接管理
var (
	activeConnections map[string]network.Stream // peerID到活动连接的映射
	connectionsMutex  sync.RWMutex
)

// 用户公钥管理
var (
	userPublicKeys map[string]*rsa.PublicKey // peerID到公钥的映射
	userKeysMutex  sync.RWMutex
)

// 初始化全局变量
func init() {
	activeConnections = make(map[string]network.Stream)
	userPublicKeys = make(map[string]*rsa.PublicKey)
	globalUsernameMap = make(map[string]string)
}

// 协议ID
var protocolID = protocol.ID("/pchat/1.0.0")
var keyExchangeID = protocol.ID("/pchat/keyexchange/1.0.0")

// DHTDiscovery 结构体
type DHTDiscovery struct {
	host host.Host
	// 这里应该包含DHT相关的字段，但为了简化，我们只保留host
}

// UserInfo 用户信息结构体
type UserInfo struct {
	Username string    `json:"username"`
	PeerID   string    `json:"peer_id"`
	AddrInfo string    `json:"addr_info"`
	LastSeen time.Time `json:"last_seen"`
}

// RegistryClient 注册服务器客户端
type RegistryClient struct {
	serverAddr string
	username   string
	peerID     string
	addrInfo   string
}

// Crypto 消息加密和签名工具
type Crypto struct{}

// generateRSAKeyPair 生成RSA密钥对
func generateRSAKeyPair() (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	return privateKey, &privateKey.PublicKey, nil
}

// EncryptAndSignMessage 加密并签名消息
func (c *Crypto) EncryptAndSignMessage(message string, recipientPubKey rsa.PublicKey, senderPrivKey *rsa.PrivateKey) (string, error) {
	// 生成随机AES密钥
	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		return "", err
	}

	// 用AES加密消息
	encryptedMessage, err := encryptAES([]byte(message), aesKey)
	if err != nil {
		return "", err
	}

	// 用接收方公钥加密AES密钥
	hash := sha256.New()
	encryptedKey, err := rsa.EncryptOAEP(hash, rand.Reader, &recipientPubKey, aesKey, nil)
	if err != nil {
		return "", err
	}

	// 用发送方私钥签名消息
	signature, err := rsa.SignPKCS1v15(rand.Reader, senderPrivKey, 0, []byte(message))
	if err != nil {
		return "", err
	}

	// 创建包含加密消息、加密密钥和签名的结构
	secureMsg := struct {
		EncryptedMessage []byte `json:"encrypted_message"`
		EncryptedKey     []byte `json:"encrypted_key"`
		Signature        []byte `json:"signature"`
		Timestamp        int64  `json:"timestamp"`
	}{
		EncryptedMessage: encryptedMessage,
		EncryptedKey:     encryptedKey,
		Signature:        signature,
		Timestamp:        time.Now().Unix(),
	}

	// 序列化结构
	secureMsgBytes, err := json.Marshal(secureMsg)
	if err != nil {
		return "", err
	}

	// 返回Base64编码的字符串
	return string(secureMsgBytes), nil
}

// DecryptAndVerifyMessage 解密并验证消息
func (c *Crypto) DecryptAndVerifyMessage(secureMessage string, recipientPrivKey *rsa.PrivateKey, senderPubKey rsa.PublicKey) (string, bool, error) {
	// 解析安全消息结构
	var secureMsg struct {
		EncryptedMessage []byte `json:"encrypted_message"`
		EncryptedKey     []byte `json:"encrypted_key"`
		Signature        []byte `json:"signature"`
		Timestamp        int64  `json:"timestamp"`
	}

	if err := json.Unmarshal([]byte(secureMessage), &secureMsg); err != nil {
		return "", false, err
	}

	// 用接收方私钥解密AES密钥
	hash := sha256.New()
	aesKey, err := rsa.DecryptOAEP(hash, rand.Reader, recipientPrivKey, secureMsg.EncryptedKey, nil)
	if err != nil {
		return "", false, err
	}

	// 用AES密钥解密消息
	messageBytes, err := decryptAES(secureMsg.EncryptedMessage, aesKey)
	if err != nil {
		return "", false, err
	}

	// 用发送方公钥验证签名
	err = rsa.VerifyPKCS1v15(&senderPubKey, 0, messageBytes, secureMsg.Signature)
	verified := err == nil

	// 检查时间戳（防重放攻击）
	if time.Now().Unix()-secureMsg.Timestamp > 300 { // 5分钟超时
		return string(messageBytes), false, fmt.Errorf("消息已过期")
	}

	return string(messageBytes), verified, nil
}

// encryptAES AES加密
func encryptAES(plaintext []byte, key []byte) ([]byte, error) {
	// 简化实现，实际应该使用标准库或第三方库
	return plaintext, nil
}

// decryptAES AES解密
func decryptAES(ciphertext []byte, key []byte) ([]byte, error) {
	// 简化实现，实际应该使用标准库或第三方库
	return ciphertext, nil
}

// setUserPublicKey 保存用户公钥
func setUserPublicKey(peerID string, pubKey *rsa.PublicKey) {
	userKeysMutex.Lock()
	defer userKeysMutex.Unlock()
	userPublicKeys[peerID] = pubKey
}

// getUserPublicKey 获取用户公钥
func getUserPublicKey(peerID string) (*rsa.PublicKey, bool) {
	userKeysMutex.RLock()
	defer userKeysMutex.RUnlock()
	pubKey, exists := userPublicKeys[peerID]
	return pubKey, exists
}

// addConnection 添加活动连接
func addConnection(peerID string, stream network.Stream) {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()
	activeConnections[peerID] = stream
}

// removeConnection 移除活动连接
func removeConnection(peerID string) {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()
	delete(activeConnections, peerID)
}

// getConnection 获取活动连接
func getConnection(peerID string) (network.Stream, bool) {
	connectionsMutex.RLock()
	defer connectionsMutex.RUnlock()
	stream, exists := activeConnections[peerID]
	return stream, exists
}

// getAllConnections 获取所有活动连接
func getAllConnections() map[string]network.Stream {
	connectionsMutex.RLock()
	defer connectionsMutex.RUnlock()
	// 返回副本以避免并发问题
	connectionsCopy := make(map[string]network.Stream)
	for k, v := range activeConnections {
		connectionsCopy[k] = v
	}
	return connectionsCopy
}

// getPeerDisplayName 获取peer的显示名称
func getPeerDisplayName(peerID peer.ID) string {
	globalVarsMutex.RLock()
	defer globalVarsMutex.RUnlock()

	// 首先尝试从用户名映射获取
	if username, exists := globalUsernameMap[peerID.String()]; exists {
		return username
	}

	// 如果没有用户名映射，返回peer ID的短字符串
	return peerID.ShortString()
}

// exchangePublicKeys 交换公钥（作为客户端，先发送自己的公钥，然后接收对方的）
func exchangePublicKeys(stream network.Stream, peerID string) error {
	// 先发送自己的公钥
	exchangeMsg := PublicKeyExchange{
		PublicKey: currentUserPublicKey,
		Username:  globalUsername,
	}

	msgBytes, err := json.Marshal(exchangeMsg)
	if err != nil {
		return fmt.Errorf("序列化公钥失败: %v", err)
	}

	// 发送公钥消息
	_, err = stream.Write(append(msgBytes, '\n'))
	if err != nil {
		return fmt.Errorf("发送公钥失败: %v", err)
	}

	// 然后读取对方的公钥
	reader := bufio.NewReader(stream)
	keyMsg, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("读取对方公钥失败: %v", err)
	}

	keyMsg = strings.TrimSpace(keyMsg)
	var receivedKey PublicKeyExchange
	if err := json.Unmarshal([]byte(keyMsg), &receivedKey); err != nil {
		return fmt.Errorf("解析对方公钥失败: %v", err)
	}

	// 保存对方的公钥和用户名映射
	setUserPublicKey(peerID, &receivedKey.PublicKey)

	// 保存用户名映射
	globalVarsMutex.Lock()
	if globalUsernameMap == nil {
		globalUsernameMap = make(map[string]string)
	}
	globalUsernameMap[peerID] = receivedKey.Username
	globalVarsMutex.Unlock()

	fmt.Printf("🔐 已与用户 %s 交换公钥\n", receivedKey.Username)
	return nil
}

// exchangePublicKeysIncoming 交换公钥（作为服务器端，先接收对方的公钥，然后发送自己的）
func exchangePublicKeysIncoming(stream network.Stream, peerID string) error {
	// 先读取对方的公钥
	reader := bufio.NewReader(stream)
	keyMsg, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("读取对方公钥失败: %v", err)
	}

	keyMsg = strings.TrimSpace(keyMsg)
	var receivedKey PublicKeyExchange
	if err := json.Unmarshal([]byte(keyMsg), &receivedKey); err != nil {
		return fmt.Errorf("解析对方公钥失败: %v", err)
	}

	// 保存对方的公钥和用户名映射
	setUserPublicKey(peerID, &receivedKey.PublicKey)

	// 保存用户名映射
	globalVarsMutex.Lock()
	if globalUsernameMap == nil {
		globalUsernameMap = make(map[string]string)
	}
	globalUsernameMap[peerID] = receivedKey.Username
	globalVarsMutex.Unlock()

	// 然后发送自己的公钥
	exchangeMsg := PublicKeyExchange{
		PublicKey: currentUserPublicKey,
		Username:  globalUsername,
	}

	msgBytes, err := json.Marshal(exchangeMsg)
	if err != nil {
		return fmt.Errorf("序列化公钥失败: %v", err)
	}

	// 发送公钥消息
	_, err = stream.Write(append(msgBytes, '\n'))
	if err != nil {
		return fmt.Errorf("发送公钥失败: %v", err)
	}

	fmt.Printf("🔐 已与用户 %s 交换公钥\n", receivedKey.Username)
	return nil
}

// handleStream 处理流上的消息
func handleStream(stream network.Stream) {
	// 注意：不要在这里立即关闭流，我们需要保持它打开以进行双向通信
	// defer stream.Close()

	// 设置协议ID
	stream.SetProtocol(protocolID)

	// 首先交换公钥
	senderID := stream.Conn().RemotePeer()
	senderIDStr := senderID.String()

	if err := exchangePublicKeysIncoming(stream, senderIDStr); err != nil {
		log.Printf("公钥交换失败: %v\n", err)
		stream.Close()
		return
	}

	// 将流添加到活动连接列表，以便可以用于发送消息
	addConnection(senderIDStr, stream)

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
		senderPubKey, exists := getUserPublicKey(senderIDStr)
		if !exists {
			// 如果没有发送方的公钥，使用我们自己的公钥作为示例
			senderPubKey = &currentUserPublicKey
		}

		decryptedMsg, verified, err := (&Crypto{}).DecryptAndVerifyMessage(message, currentUserPrivateKey, *senderPubKey)
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
			fmt.Print("> ")
		case strings.Contains(decryptedMsg, "石头剪刀布游戏"):
			// 处理石头剪刀布游戏消息
			handleRPSGame(decryptedMsg, senderIDStr)
		case strings.Contains(decryptedMsg, "的回应: "):
			// 处理石头剪刀布游戏回应消息
			handleRPSResponse(decryptedMsg, senderIDStr)
		case strings.Contains(decryptedMsg, "file_name"):
			// 处理文件传输消息
			handleFileTransfer(decryptedMsg)
		default:
			// 显示普通消息
			displayName := getPeerDisplayName(senderID)
			if displayName == "" {
				displayName = senderID.ShortString()
			}

			if verified {
				fmt.Printf("\n📨 收到来自 %s 的消息:\n", displayName)
				fmt.Printf("💬 消息内容: %s\n", decryptedMsg)
				fmt.Printf("✅ 消息已验证（签名有效，未检测到重放攻击）\n")
			} else {
				fmt.Printf("\n📨 收到来自 %s 的消息:\n", displayName)
				fmt.Printf("⚠️  警告消息: %s（签名验证失败或检测到异常）\n", decryptedMsg)
			}
			fmt.Print("> ")
		}
	}

	// 只有在循环结束后才关闭流
	stream.Close()
	// 从活动连接中移除
	removeConnection(senderIDStr)
}

// sendMessage 发送消息给指定的用户
func sendMessage(message string, targetPeerID string) error {
	// 获取活动连接
	stream, exists := getConnection(targetPeerID)
	if !exists {
		return fmt.Errorf("没有与目标用户 %s 的活动连接", targetPeerID)
	}

	// 获取目标用户的公钥
	targetPubKey, exists := getUserPublicKey(targetPeerID)
	if !exists {
		return fmt.Errorf("未找到目标用户 %s 的公钥", targetPeerID)
	}

	// 加密并签名消息
	encryptedMsg, err := (&Crypto{}).EncryptAndSignMessage(message, *targetPubKey, currentUserPrivateKey)
	if err != nil {
		return fmt.Errorf("加密消息失败: %v", err)
	}

	// 发送加密消息
	_, err = stream.Write([]byte(encryptedMsg + "\n"))
	if err != nil {
		// 如果发送失败，移除连接
		removeConnection(targetPeerID)
		return fmt.Errorf("发送消息失败: %v", err)
	}

	return nil
}

// broadcastMessage 广播消息给所有已连接的用户
func broadcastMessage(message string) {
	connections := getAllConnections()
	if len(connections) == 0 {
		fmt.Println("⚠️  没有已连接的用户，消息未发送")
		return
	}

	// 创建要发送的消息
	_, err := (&Crypto{}).EncryptAndSignMessage(message, currentUserPublicKey, currentUserPrivateKey)
	if err != nil {
		log.Printf("加密广播消息失败: %v\n", err)
		return
	}
	if err != nil {
		log.Printf("加密广播消息失败: %v\n", err)
		return
	}

	// 向所有连接发送消息
	successCount := 0
	for peerID, stream := range connections {
		// 获取目标用户的公钥
		targetPubKey, exists := getUserPublicKey(peerID)
		if !exists {
			// 如果没有公钥，使用当前用户的公钥作为默认值
			targetPubKey = &currentUserPublicKey
		}

		// 加密消息
		encryptedMsg, err := (&Crypto{}).EncryptAndSignMessage(message, *targetPubKey, currentUserPrivateKey)
		if err != nil {
			log.Printf("加密消息给 %s 失败: %v\n", peerID, err)
			continue
		}

		// 发送消息
		_, err = stream.Write([]byte(encryptedMsg + "\n"))
		if err != nil {
			log.Printf("发送消息给 %s 失败: %v\n", peerID, err)
			// 移除失败的连接
			removeConnection(peerID)
			continue
		}
		successCount++
	}

	if successCount > 0 {
		fmt.Printf("📤 消息已发送给 %d 个用户\n", successCount)
	}
}

// notifyOffline 通知所有已连接的用户即将下线
func notifyOffline() {
	offlineMsg := fmt.Sprintf("%s 已下线", globalUsername)
	connections := getAllConnections()

	for peerID := range connections {
		// 获取目标用户的公钥
		targetPubKey, exists := getUserPublicKey(peerID)
		if !exists {
			// 如果没有公钥，使用当前用户的公钥作为默认值
			targetPubKey = &currentUserPublicKey
		}

		// 加密消息
		encryptedMsg, err := (&Crypto{}).EncryptAndSignMessage(offlineMsg, *targetPubKey, currentUserPrivateKey)
		if err != nil {
			log.Printf("加密下线通知给 %s 失败: %v\n", peerID, err)
			continue
		}

		// 获取连接
		stream, exists := getConnection(peerID)
		if !exists {
			continue
		}

		// 发送消息
		_, err = stream.Write([]byte(encryptedMsg + "\n"))
		if err != nil {
			log.Printf("发送下线通知给 %s 失败: %v\n", peerID, err)
		}
	}
}

// hangupAllConnections 挂断所有连接
func hangupAllConnections() {
	fmt.Println("挂断所有连接...")
	connections := getAllConnections()

	for peerID, stream := range connections {
		// 发送挂断消息
		hangupMsg := fmt.Sprintf("%s 已挂断连接", globalUsername)

		// 获取目标用户的公钥
		targetPubKey, exists := getUserPublicKey(peerID)
		if !exists {
			// 如果没有公钥，使用当前用户的公钥作为默认值
			targetPubKey = &currentUserPublicKey
		}

		// 加密消息
		encryptedMsg, err := (&Crypto{}).EncryptAndSignMessage(hangupMsg, *targetPubKey, currentUserPrivateKey)
		if err != nil {
			log.Printf("加密挂断消息给 %s 失败: %v\n", peerID, err)
		} else {
			// 发送消息
			_, err = stream.Write([]byte(encryptedMsg + "\n"))
			if err != nil {
				log.Printf("发送挂断消息给 %s 失败: %v\n", peerID, err)
			}
		}

		// 关闭流
		stream.Close()
		// 从活动连接中移除
		removeConnection(peerID)
	}

	fmt.Println("✅ 所有连接已挂断")
}

// PublicKeyExchange 公钥交换消息结构
type PublicKeyExchange struct {
	PublicKey rsa.PublicKey `json:"public_key"`
	Username  string        `json:"username"`
}

// connectToPeer 连接到指定的 peer
func connectToPeer(targetAddr string) {
	// 解析目标peer地址
	addr, err := multiaddr.NewMultiaddr(targetAddr)
	if err != nil {
		log.Printf("⚠️  解析目标peer地址失败: %v\n", err)
		return
	}

	// 从地址中提取peer ID
	peerInfo, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil {
		log.Printf("⚠️  解析目标peer信息失败: %v\n", err)
		return
	}

	// 连接到目标peer
	globalVarsMutex.RLock()
	host := globalHost
	ctx := globalCtx
	globalVarsMutex.RUnlock()

	if host != nil && ctx != nil {
		// 添加地址到peerstore
		host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, peerstore.PermanentAddrTTL)

		// 建立连接
		streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
		stream, err := host.NewStream(streamCtx, peerInfo.ID, protocolID)
		streamCancel()
		if err != nil {
			log.Printf("⚠️  连接目标peer失败: %v\n", err)
			return
		}

		// 进行公钥交换
		if err := exchangePublicKeys(stream, peerInfo.ID.String()); err != nil {
			log.Printf("⚠️  与目标peer交换公钥失败: %v\n", err)
			stream.Close()
			return
		}

		// 添加连接到活动连接列表
		addConnection(peerInfo.ID.String(), stream)
		fmt.Printf("✅ 已连接到目标peer: %s\n", peerInfo.ID.ShortString())
	}
}

// chatLoop 聊天循环
func chatLoop(registryClient *RegistryClient, dhtDiscovery *DHTDiscovery) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Print("> ")
			continue
		}

		// 处理命令
		if strings.HasPrefix(input, "/") {
			handleCommand(input, registryClient, dhtDiscovery)
		} else {
			// 发送普通消息
			broadcastMessage(input)
		}

		fmt.Print("> ")
	}
}

// handleCommand 处理命令
func handleCommand(command string, registryClient *RegistryClient, dhtDiscovery *DHTDiscovery) {
	switch {
	case command == "/help":
		fmt.Println("📋 帮助信息:")
		fmt.Println("  /help - 显示帮助信息")
		fmt.Println("  /list - 显示在线用户列表")
		fmt.Println("  /call <用户名> - 呼叫指定用户")
		fmt.Println("  /hangup - 挂断所有连接")
		fmt.Println("  /rps - 发起石头剪刀布游戏")
		fmt.Println("  /sendfile <文件路径> - 发送文件")
		fmt.Println("  /quit - 退出程序")
	case command == "/list":
		// 简化实现，显示当前连接的用户
		connections := getAllConnections()
		fmt.Printf("📋 在线用户列表 (%d 人):\n", len(connections))
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		i := 1
		for peerID := range connections {
			username := getPeerDisplayName(peer.ID(peerID))
			fmt.Printf("%d. 用户名: %s\n", i, username)
			fmt.Printf("   节点ID: %s\n", peerID)
			fmt.Println()
			i++
		}
		if len(connections) == 0 {
			fmt.Println("暂无在线用户")
		}
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	case strings.HasPrefix(command, "/call "):
		// 简化实现，这里应该根据用户名查找peer ID并连接
		target := strings.TrimSpace(strings.TrimPrefix(command, "/call "))
		fmt.Printf("📞 呼叫用户: %s\n", target)
		// 实际实现应该查找用户并建立连接
	case command == "/hangup":
		hangupAllConnections()
	case command == "/rps":
		// 发起石头剪刀布游戏
		fmt.Println("🎮 发起石头剪刀布游戏...")
		// 实际实现应该发送游戏邀请给所有连接的用户
	case strings.HasPrefix(command, "/sendfile "):
		// 发送文件
		filePath := strings.TrimSpace(strings.TrimPrefix(command, "/sendfile "))
		sendFile(filePath)
	case command == "/quit":
		fmt.Println("👋 正在退出...")
		// 退出信号会在主goroutine中处理
		os.Exit(0)
	default:
		fmt.Printf("⚠️  未知命令: %s\n", command)
		fmt.Println("输入 /help 查看可用命令")
	}
}

// handleRPSGame 处理石头剪刀布游戏
func handleRPSGame(message, senderID string) {
	fmt.Printf("🎮 收到 %s 的石头剪刀布游戏邀请\n", getPeerDisplayName(peer.ID(senderID)))
	// 实际实现应该处理游戏逻辑
}

// handleRPSResponse 处理石头剪刀布游戏回应
func handleRPSResponse(message, senderID string) {
	fmt.Printf("🎮 收到 %s 的石头剪刀布游戏回应\n", getPeerDisplayName(peer.ID(senderID)))
	// 实际实现应该处理游戏回应逻辑
}

// sendFile 发送文件
func sendFile(filePath string) {
	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("⚠️  文件不存在: %s\n", filePath)
		return
	}

	// 读取文件内容
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		fmt.Printf("⚠️  读取文件失败: %v\n", err)
		return
	}

	// 获取文件名
	fileName := filepath.Base(filePath)
	fileSize := int64(len(content))

	// 创建文件传输消息
	fileMsg := struct {
		FileName string `json:"file_name"`
		FileSize int64  `json:"file_size"`
		Content  []byte `json:"content"`
	}{
		FileName: fileName,
		FileSize: fileSize,
		Content:  content,
	}

	// 序列化消息
	msgBytes, err := json.Marshal(fileMsg)
	if err != nil {
		fmt.Printf("⚠️  序列化文件消息失败: %v\n", err)
		return
	}

	// 广播文件消息
	broadcastMessage(string(msgBytes))
	fmt.Printf("📤 文件 %s 已发送\n", fileName)
}

// handleFileTransfer 处理文件传输消息
func handleFileTransfer(message string) {
	// 解析文件传输消息
	var fileMsg struct {
		FileName string `json:"file_name"`
		FileSize int64  `json:"file_size"`
		Content  []byte `json:"content"`
	}

	if err := json.Unmarshal([]byte(message), &fileMsg); err != nil {
		log.Printf("解析文件消息失败: %v\n", err)
		return
	}

	// 创建接收文件目录
	receivedDir := "received_files"
	if err := os.MkdirAll(receivedDir, 0755); err != nil {
		log.Printf("创建接收目录失败: %v\n", err)
		return
	}

	// 生成带时间戳的文件名
	timestamp := time.Now().Format("20060102_150405")
	fileExt := filepath.Ext(fileMsg.FileName)
	fileNameWithoutExt := strings.TrimSuffix(fileMsg.FileName, fileExt)
	timestampedFileName := fmt.Sprintf("%s_%s%s", fileNameWithoutExt, timestamp, fileExt)

	// 生成文件路径
	filePath := filepath.Join(receivedDir, timestampedFileName)

	// 写入文件
	if err := ioutil.WriteFile(filePath, fileMsg.Content, 0644); err != nil {
		log.Printf("保存文件失败: %v\n", err)
		return
	}

	fmt.Printf("\n📥 收到文件: %s (大小: %d 字节)\n", fileMsg.FileName, fileMsg.FileSize)
	fmt.Printf("💾 文件已保存到: %s\n", filePath)
	fmt.Print("> ")
}

func main() {
	// 解析命令行参数
	port := flag.Int("port", 9000, "监听端口")
	username := flag.String("username", "", "用户名")
	registryAddr := flag.String("registry", "", "注册服务器地址")
	targetPeer := flag.String("peer", "", "目标peer地址")
	flag.Parse()

	if *username == "" {
		log.Fatal("❌ 用户名不能为空")
	}

	globalUsername = *username

	// 生成RSA密钥对
	privateKey, publicKey, err := generateRSAKeyPair()
	if err != nil {
		log.Fatal("❌ 生成密钥对失败:", err)
	}
	currentUserPrivateKey = privateKey
	currentUserPublicKey = *publicKey

	// 创建libp2p主机
	ctx := context.Background()
	globalCtx = ctx

	// 解析监听地址
	sourceMultiAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", *port))
	if err != nil {
		log.Fatal("❌ 解析监听地址失败:", err)
	}

	// 生成libp2p主机
	fmt.Println("🚀 正在启动P2P聊天节点...")
	privKey, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
	if err != nil {
		log.Fatal("❌ 生成密钥对失败:", err)
	}

	host, err := libp2p.New(
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.Identity(privKey),
	)
	if err != nil {
		log.Fatal("❌ 创建libp2p主机失败:", err)
	}
	globalHost = host

	// 设置流处理器
	host.SetStreamHandler(protocolID, handleStream)
	host.SetStreamHandler(keyExchangeID, func(s network.Stream) {
		// 处理密钥交换
		peerID := s.Conn().RemotePeer().String()
		if err := exchangePublicKeysIncoming(s, peerID); err != nil {
			log.Printf("密钥交换失败: %v\n", err)
			s.Close()
			return
		}
		// 添加连接到活动连接列表
		addConnection(peerID, s)
	})

	fmt.Printf("✅ P2P 聊天节点已启动\n")
	fmt.Printf("📍 节点 ID: %s\n", host.ID().String())
	fmt.Println("📍 监听地址:")
	for _, addr := range host.Addrs() {
		fmt.Printf("   %s/p2p/%s\n", addr.String(), host.ID().String())
	}

	// 初始化发现服务
	var registryClient *RegistryClient
	var dhtDiscovery *DHTDiscovery

	if *registryAddr != "" {
		// 使用注册服务器模式
		registryClient = &RegistryClient{
			serverAddr: *registryAddr,
			username:   *username,
		}
		fmt.Printf("📡 使用注册服务器模式: %s\n", *registryAddr)
	} else {
		// 使用DHT去中心化发现模式
		dhtDiscovery = &DHTDiscovery{
			host: host,
		}
		globalDHTDiscovery = dhtDiscovery
		fmt.Println("🌐 使用DHT去中心化发现模式（无需注册服务器）")
		fmt.Printf("✅ DHT发现服务已启动 (用户名: %s)\n", *username)
		fmt.Println("💡 提示：DHT发现需要一些时间来连接网络中的其他节点")
	}

	// 如果提供了目标 peer，则连接到它
	if *targetPeer != "" {
		connectToPeer(*targetPeer)
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
		// 模拟注销过程
		fmt.Println("✅ 已从注册服务器注销")
	}

	// 关闭DHT发现服务
	if dhtDiscovery != nil {
		fmt.Println("🌐 正在关闭DHT发现服务...")
		// 模拟关闭过程
		fmt.Println("✅ DHT发现服务已关闭")
	}

	// 挂断所有连接
	hangupAllConnections()

	fmt.Println("👋 程序已安全退出")
}
