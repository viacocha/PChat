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

const (
	registryPort     = 8888
	heartbeatTimeout = 30 * time.Second // 心跳超时时间
)

// ClientInfo 客户端信息
type ClientInfo struct {
	PeerID    string   `json:"peer_id"`
	Addresses []string `json:"addresses"`
	Username  string   `json:"username"`
	LastSeen  time.Time `json:"last_seen"`
}

// RegistryServer 注册服务器
type RegistryServer struct {
	clients map[string]*ClientInfo
	mutex   sync.RWMutex
}

// RegistryMessage 注册消息
type RegistryMessage struct {
	Type     string   `json:"type"`      // register, heartbeat, list, lookup
	PeerID   string   `json:"peer_id"`
	Addresses []string `json:"addresses"`
	Username  string   `json:"username"`
	TargetID string   `json:"target_id"` // 用于 lookup
}

// RegistryResponse 注册响应
type RegistryResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Clients []*ClientInfo `json:"clients,omitempty"`
	Client  *ClientInfo   `json:"client,omitempty"`
}

func NewRegistryServer() *RegistryServer {
	rs := &RegistryServer{
		clients: make(map[string]*ClientInfo),
	}
	
	// 启动清理过期客户端的 goroutine
	go rs.cleanupExpiredClients()
	
	return rs
}

// cleanupExpiredClients 清理过期的客户端
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

