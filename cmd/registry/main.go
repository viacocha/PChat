package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"encoding/json"
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

// RegistryServer 注册服务器
type RegistryServer struct {
	clients map[string]*ClientInfo
	mutex   sync.RWMutex
}

// NewRegistryServer 创建注册服务器
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
		response.Message = "获取客户端列表成功"

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
func (rs *RegistryServer) Start(port int) error {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	defer listener.Close()

	log.Printf("✅ 注册服务器已启动，监听端口 %d\n", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("接受连接失败: %v\n", err)
			continue
		}

		go rs.handleRequest(conn)
	}
}

func main() {
	port := flag.Int("port", registryPort, "注册服务器端口")
	flag.Parse()

	server := NewRegistryServer()

	// 处理中断信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("\n🛑 正在关闭注册服务器...")
		os.Exit(0)
	}()

	log.Printf("🚀 启动注册服务器，端口: %d\n", *port)
	if err := server.Start(*port); err != nil {
		log.Fatalf("服务器启动失败: %v\n", err)
	}
}
