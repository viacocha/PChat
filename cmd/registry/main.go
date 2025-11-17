package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	registryPort     = 8888
	heartbeatTimeout = 30 * time.Second // 心跳超时时间
)

// ClientInfo 客户端信息
type ClientInfo struct {
	PeerID       string    `json:"peer_id"`
	Addresses    []string  `json:"addresses"`
	Username     string    `json:"username"`
	LastSeen     time.Time `json:"last_seen"`
	RegisterTime time.Time `json:"register_time"` // 注册时间
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
	ui      *RegistryUI // UI引用，可选
}

// NewRegistryServer 创建新的注册服务器实例
// 该函数初始化注册服务器并启动清理过期客户端的后台 goroutine
//
// 返回:
//   - *RegistryServer: 注册服务器实例
//
// 该函数会启动一个定期清理过期客户端的 goroutine（每 10 秒检查一次）
// 超过 2 倍心跳超时时间（60 秒）未发送心跳的客户端会被自动清理
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
// 清理操作会通过 UI 显示事件信息（如果 UI 可用）
func (rs *RegistryServer) cleanupExpiredClients() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		rs.mutex.Lock()
		now := time.Now()
		for id, client := range rs.clients {
			if now.Sub(client.LastSeen) > heartbeatTimeout*2 {
				delete(rs.clients, id)
				peerIDDisplay := id
				if len(peerIDDisplay) > 12 {
					peerIDDisplay = peerIDDisplay[:12] + "..."
				}
				eventMsg := fmt.Sprintf("[yellow]⏰ 客户端过期[white]: [cyan]%s[white] (节点ID: [yellow]%s[white])", client.Username, peerIDDisplay)
				if rs.ui != nil {
					rs.ui.AddEvent(eventMsg)
				} else {
					log.Printf("客户端 %s (%s) 已过期，已移除\n", id, client.Username)
				}
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
		now := time.Now()
		// 如果是新注册，记录注册时间；如果是重新注册，保持原有注册时间
		var registerTime time.Time
		if existingClient, exists := rs.clients[msg.PeerID]; exists {
			registerTime = existingClient.RegisterTime // 保持原有注册时间
		} else {
			registerTime = now // 新注册，使用当前时间
		}
		rs.clients[msg.PeerID] = &ClientInfo{
			PeerID:       msg.PeerID,
			Addresses:    msg.Addresses,
			Username:     msg.Username,
			LastSeen:     now,
			RegisterTime: registerTime,
		}
		rs.mutex.Unlock()
		response.Success = true
		response.Message = "注册成功"
		peerIDDisplay := msg.PeerID
		if len(peerIDDisplay) > 12 {
			peerIDDisplay = peerIDDisplay[:12] + "..."
		}
		eventMsg := fmt.Sprintf("[green]✅ 客户端注册[white]: [cyan]%s[white] (节点ID: [yellow]%s[white])", msg.Username, peerIDDisplay)
		if rs.ui != nil {
			rs.ui.AddEvent(eventMsg)
		} else {
			log.Printf("客户端 %s (%s) 已注册\n", msg.PeerID, msg.Username)
		}

	case "unregister":
		rs.mutex.Lock()
		if client, exists := rs.clients[msg.PeerID]; exists {
			delete(rs.clients, msg.PeerID)
			response.Success = true
			response.Message = "注销成功"
			peerIDDisplay := msg.PeerID
			if len(peerIDDisplay) > 12 {
				peerIDDisplay = peerIDDisplay[:12] + "..."
			}
			eventMsg := fmt.Sprintf("[red]❌ 客户端注销[white]: [cyan]%s[white] (节点ID: [yellow]%s[white])", client.Username, peerIDDisplay)
			if rs.ui != nil {
				rs.ui.AddEvent(eventMsg)
			} else {
				log.Printf("客户端 %s (%s) 已注销\n", msg.PeerID, client.Username)
			}
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
			if rs.ui != nil {
				rs.ui.AddStatusMessage(fmt.Sprintf("收到心跳: %s", client.Username))
			}
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
	port := flag.Int("port", registryPort, "注册服务器端口，格式：数字，默认：8888")
	uiFlag := flag.String("ui", "true", "是否使用视窗化UI界面，格式：true/false，默认：true")
	flag.Parse()

	// 解析UI标志
	useUI := true
	if *uiFlag != "" {
		uiFlagLower := strings.ToLower(strings.TrimSpace(*uiFlag))
		useUI = uiFlagLower == "true" || uiFlagLower == "1" || uiFlagLower == "yes" || uiFlagLower == "on"
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := NewRegistryServer()

	var ui *RegistryUI
	if useUI {
		// 创建并启动UI
		ui = NewRegistryUI(ctx, server, *port)
		server.ui = ui // 设置UI引用

		// 显示启动信息
		ui.AddEvent(fmt.Sprintf("[green]🚀 注册服务器已启动[white]，监听端口 [cyan]%d[white]", *port))

		// 在goroutine中运行UI
		uiDone := make(chan struct{})
		go func() {
			defer close(uiDone)
			if err := ui.Run(); err != nil {
				log.Printf("UI运行错误: %v\n", err)
			}
		}()

		// 处理中断信号（UI模式）
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		// 在goroutine中运行服务器
		serverDone := make(chan error, 1)
		go func() {
			serverDone <- server.Start(*port)
		}()

		select {
		case <-sigCh:
			ui.AddEvent("[yellow]🛑 收到关闭信号，正在退出...[white]")
			time.Sleep(500 * time.Millisecond)
			cancel()
			ui.Stop()
		case <-uiDone:
			// UI已退出
			cancel()
		case err := <-serverDone:
			if err != nil {
				ui.AddEvent(fmt.Sprintf("[red]❌ 服务器错误: %v[white]", err))
				time.Sleep(2 * time.Second)
			}
			cancel()
			ui.Stop()
		}
	} else {
		// 非UI模式：直接运行服务器
		log.Printf("🚀 注册服务器已启动，监听端口 %d\n", *port)
		log.Printf("💡 提示：使用 -ui true 启用视窗化界面\n")

		// 处理中断信号（非UI模式）
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		// 在goroutine中运行服务器
		serverDone := make(chan error, 1)
		go func() {
			serverDone <- server.Start(*port)
		}()

		select {
		case <-sigCh:
			log.Printf("\n🛑 收到关闭信号，正在退出...\n")
			cancel()
		case err := <-serverDone:
			if err != nil {
				log.Printf("❌ 服务器错误: %v\n", err)
			}
			cancel()
		}
	}
}
