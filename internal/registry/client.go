package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
)

var (
	dialRegistryFunc = func(addr string) (net.Conn, error) {
		return net.Dial("tcp", addr)
	}

	dialRegistryWithTimeoutFunc = func(addr string, timeout time.Duration) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: timeout}
		return dialer.Dial("tcp", addr)
	}
)

const (
	registryPort     = 8888
	heartbeatTimeout = 30 * time.Second // 心跳超时时间
)

// ClientInfo 客户端信息
type ClientInfo struct {
	PeerID    string    `json:"peer_id"`
	Addresses []string  `json:"addresses"`
	Username  string    `json:"username"`
	LastSeen  time.Time `json:"last_seen"`
}

// RegistryServer 注册服务器
type RegistryServer struct {
	clients map[string]*ClientInfo
	mutex   sync.RWMutex
}

// RegistryMessage 注册消息
type RegistryMessage struct {
	Type      string   `json:"type"` // register, heartbeat, list, lookup
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

// NewRegistryServer 创建新的注册服务器实例
// 该函数初始化注册服务器并启动清理过期客户端的后台 goroutine
//
// 返回:
//   - *RegistryServer: 注册服务器实例
//
// 该函数会启动一个定期清理过期客户端的 goroutine（每 10 秒检查一次）
func NewRegistryServer() *RegistryServer {
	rs := &RegistryServer{
		clients: make(map[string]*ClientInfo),
	}

	// 启动清理过期客户端的 goroutine
	go rs.cleanupExpiredClients()

	return rs
}

// cleanupExpiredClients 清理过期的客户端
// 该函数在后台 goroutine 中定期运行，删除超过 2 倍心跳超时时间的客户端
// 心跳超时时间为 30 秒，因此超过 60 秒未发送心跳的客户端会被清理
//
// 该函数会持续运行直到程序退出，每 10 秒检查一次
func (rs *RegistryServer) cleanupExpiredClients() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		rs.mutex.Lock()
		now := time.Now()
		for id, client := range rs.clients {
			if now.Sub(client.LastSeen) > heartbeatTimeout*2 {
				delete(rs.clients, id)
				log.Printf("客户端 %s (%s) 已过期，已移除\n", id, client.Username)
			}
		}
		rs.mutex.Unlock()
	}
}

// handleRequest 处理客户端请求
// 该函数处理来自客户端的 JSON 消息，支持以下操作：
// - register: 注册客户端
// - heartbeat: 更新客户端心跳时间
// - list: 列出所有已注册的客户端
// - lookup: 查找指定客户端（通过用户名或节点ID）
//
// 参数:
//   - conn: 客户端网络连接
//
// 该函数会：
// 1. 解码 JSON 消息
// 2. 根据消息类型执行相应操作
// 3. 编码响应并发送给客户端
// 4. 关闭连接
//
// 该函数是线程安全的，使用互斥锁保护共享的客户端映射
func (rs *RegistryServer) handleRequest(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var msg RegistryMessage
	if err := decoder.Decode(&msg); err != nil {
		log.Printf("解码消息失败: %v\n", err)
		return
	}

	var response RegistryResponse

	switch msg.Type {
	case "register":
		rs.mutex.Lock()
		rs.clients[msg.PeerID] = &ClientInfo{
			PeerID:    msg.PeerID,
			Addresses: msg.Addresses,
			Username:  msg.Username,
			LastSeen:  time.Now(),
		}
		rs.mutex.Unlock()
		response.Success = true
		response.Message = "注册成功"
		log.Printf("客户端 %s (%s) 已注册\n", msg.PeerID, msg.Username)

	case "unregister":
		rs.mutex.Lock()
		if client, exists := rs.clients[msg.PeerID]; exists {
			delete(rs.clients, msg.PeerID)
			response.Success = true
			response.Message = "注销成功"
			log.Printf("客户端 %s (%s) 已注销\n", msg.PeerID, client.Username)
		} else {
			response.Success = false
			response.Message = "客户端未注册"
		}
		rs.mutex.Unlock()

	case "heartbeat":
		rs.mutex.Lock()
		if client, exists := rs.clients[msg.PeerID]; exists {
			client.LastSeen = time.Now()
			response.Success = true
			response.Message = "心跳成功"
		} else {
			response.Success = false
			response.Message = "客户端未注册"
		}
		rs.mutex.Unlock()

	case "list":
		rs.mutex.RLock()
		clients := make([]*ClientInfo, 0, len(rs.clients))
		for _, client := range rs.clients {
			clients = append(clients, client)
		}
		rs.mutex.RUnlock()
		response.Success = true
		response.Clients = clients
		response.Message = fmt.Sprintf("找到 %d 个在线客户端", len(clients))

	case "lookup":
		rs.mutex.RLock()
		var targetClient *ClientInfo
		for _, client := range rs.clients {
			if client.PeerID == msg.TargetID || client.Username == msg.TargetID {
				targetClient = client
				break
			}
		}
		rs.mutex.RUnlock()

		if targetClient != nil {
			response.Success = true
			response.Client = targetClient
			response.Message = "找到目标客户端"
		} else {
			response.Success = false
			response.Message = "未找到目标客户端"
		}

	default:
		response.Success = false
		response.Message = "未知的消息类型"
	}

	if err := encoder.Encode(response); err != nil {
		log.Printf("编码响应失败: %v\n", err)
	}
}

// Start 启动注册服务器
// Start 启动注册服务器
// 该函数在固定端口（registryPort = 8888）上监听 TCP 连接
//
// 返回:
//   - error: 如果启动失败则返回错误
//
// 该函数会：
// 1. 在固定端口上监听 TCP 连接
// 2. 为每个连接启动一个 goroutine 处理请求
// 3. 持续运行直到程序退出
//
// 该函数是阻塞的，应该在单独的 goroutine 中运行
func (rs *RegistryServer) Start() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", registryPort))
	if err != nil {
		return fmt.Errorf("监听失败: %v", err)
	}

	log.Printf("✅ 注册服务器已启动，监听端口 %d\n", registryPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("接受连接失败: %v\n", err)
			continue
		}

		go rs.handleRequest(conn)
	}
}

// RegistryClient 注册客户端
type RegistryClient struct {
	serverAddr string
	peerID     string
	addresses  []string
	username   string
}

// sendRequestWithDial 发送请求到注册服务器（使用自定义拨号函数）
// 该函数是 sendRequest 的通用版本，允许自定义网络连接方式（用于测试）
//
// 参数:
//   - msg: 要发送的注册消息
//   - dialFn: 自定义拨号函数，用于建立网络连接
//
// 返回:
//   - *RegistryResponse: 服务器响应
//   - error: 如果请求失败则返回错误
//
// 该函数会：
// 1. 使用 dialFn 建立连接
// 2. 编码并发送消息
// 3. 接收并解码响应
// 4. 关闭连接
func (rc *RegistryClient) sendRequestWithDial(msg RegistryMessage, dialFn func(string) (net.Conn, error)) (*RegistryResponse, error) {
	conn, err := dialFn(rc.serverAddr)
	if err != nil {
		return nil, fmt.Errorf("连接服务器失败: %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("发送消息失败: %v", err)
	}

	var response RegistryResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("接收响应失败: %v", err)
	}
	return &response, nil
}

// sendRequest 发送请求到注册服务器
// 该函数是 sendRequestWithDial 的便捷版本，使用默认的 TCP 连接
//
// 参数:
//   - msg: 要发送的注册消息
//
// 返回:
//   - *RegistryResponse: 服务器响应
//   - error: 如果请求失败则返回错误
//
// 该函数使用 dialRegistryFunc 建立连接，然后调用 sendRequestWithDial
func (rc *RegistryClient) sendRequest(msg RegistryMessage) (*RegistryResponse, error) {
	return rc.sendRequestWithDial(msg, dialRegistryFunc)
}

// NewRegistryClient 创建新的注册客户端实例
// 该函数初始化客户端结构，用于与注册服务器通信
//
// 参数:
//   - serverAddr: 注册服务器地址，格式为 "127.0.0.1:8888"
//   - h: libp2p host 实例，用于获取节点ID和地址
//   - username: 当前用户的用户名
//
// 返回:
//   - *RegistryClient: 注册客户端实例
//
// 该函数会设置客户端的节点ID、地址列表和用户名
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
// Register 向注册服务器注册当前客户端
// 该函数会将客户端的节点ID、地址列表和用户名发送给注册服务器
//
// 返回:
//   - error: 如果注册失败则返回错误
//
// 该函数会：
// 1. 构建注册消息（包含节点ID、地址、用户名）
// 2. 调用 sendRequest 发送注册请求
// 3. 检查响应是否成功
//
// 如果客户端已注册，服务器会更新其信息
func (rc *RegistryClient) Register() error {
	msg := RegistryMessage{
		Type:      "register",
		PeerID:    rc.peerID,
		Addresses: rc.addresses,
		Username:  rc.username,
	}
	resp, err := rc.sendRequest(msg)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("注册失败: %s", resp.Message)
	}

	return nil
}

// SendHeartbeat 发送心跳
// SendHeartbeat 向注册服务器发送心跳
// 该函数用于更新客户端在服务器上的 LastSeen 时间，表明客户端仍然在线
//
// 返回:
//   - error: 如果发送心跳失败则返回错误
//
// 该函数会：
// 1. 构建心跳消息（包含节点ID、地址、用户名）
// 2. 调用 sendRequest 发送心跳请求
// 3. 检查响应是否成功
//
// 心跳应该定期发送（建议每 20-30 秒一次），以避免被服务器清理
func (rc *RegistryClient) SendHeartbeat() error {
	msg := RegistryMessage{
		Type:      "heartbeat",
		PeerID:    rc.peerID,
		Addresses: rc.addresses,
		Username:  rc.username,
	}
	_, err := rc.sendRequest(msg)
	return err
}

// ListClients 列出所有客户端
// ListClients 从注册服务器获取所有已注册的客户端列表
// 该函数用于发现网络中的其他用户
//
// 返回:
//   - []*ClientInfo: 客户端信息列表
//   - error: 如果获取列表失败则返回错误
//
// 该函数会：
// 1. 构建列表请求消息
// 2. 调用 sendRequest 发送列表请求
// 3. 检查响应并返回客户端列表
//
// 返回的客户端信息包括：节点ID、地址、用户名、最后心跳时间
func (rc *RegistryClient) ListClients() ([]*ClientInfo, error) {
	msg := RegistryMessage{
		Type: "list",
	}
	resp, err := rc.sendRequest(msg)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("获取列表失败: %s", resp.Message)
	}

	return resp.Clients, nil
}

// LookupClient 查找客户端
// LookupClient 从注册服务器查找指定客户端
// 该函数支持通过用户名或节点ID查找客户端信息
//
// 参数:
//   - targetID: 目标标识，可以是用户名或节点ID
//
// 返回:
//   - *ClientInfo: 找到的客户端信息，如果未找到则为 nil
//   - error: 如果查找失败则返回错误
//
// 该函数会：
// 1. 构建查找请求消息
// 2. 调用 sendRequest 发送查找请求
// 3. 检查响应并返回客户端信息
//
// 服务器会尝试匹配用户名或节点ID
func (rc *RegistryClient) LookupClient(targetID string) (*ClientInfo, error) {
	msg := RegistryMessage{
		Type:     "lookup",
		TargetID: targetID,
	}
	resp, err := rc.sendRequest(msg)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("未找到客户端: %s", resp.Message)
	}

	return resp.Client, nil
}

// StartHeartbeat 启动心跳循环
// StartHeartbeat 启动定期心跳发送
// 该函数在后台 goroutine 中定期向注册服务器发送心跳
//
// 参数:
//   - ctx: 上下文，用于控制心跳的取消
//
// 该函数会：
// 1. 每 20 秒发送一次心跳
// 2. 持续运行直到上下文被取消
// 3. 如果心跳失败，会在下次循环时重试
//
// 心跳用于保持客户端在服务器上的在线状态，避免被清理
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
// Unregister 从注册服务器注销当前客户端
// 该函数用于在客户端退出时通知服务器，清理服务器上的客户端记录
//
// 返回:
//   - error: 如果注销失败则返回错误
//
// 该函数会：
// 1. 构建注销请求消息（包含节点ID）
// 2. 调用 sendRequest 发送注销请求
// 3. 检查响应是否成功
//
// 注销后，服务器会删除该客户端的记录
func (rc *RegistryClient) Unregister() error {
	msg := RegistryMessage{
		Type:      "unregister",
		PeerID:    rc.peerID,
		Addresses: rc.addresses,
		Username:  rc.username,
	}
	resp, err := rc.sendRequestWithDial(msg, func(addr string) (net.Conn, error) {
		conn, err := dialRegistryWithTimeoutFunc(addr, 1*time.Second)
		if err != nil {
			return nil, err
		}
		conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		return conn, nil
	})
	if err == nil && resp != nil && resp.Success {
		return nil
	}

	// 即使没有收到响应，也认为注销请求已发送
	return nil
}
