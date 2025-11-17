package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// RegistryUI 注册服务器界面结构
type RegistryUI struct {
	app            *tview.Application
	header         *tview.TextView
	clientArea     *tview.TextView  // 左侧：在线用户状态
	systemArea     *tview.TextView  // 右侧：系统信息
	statusBar      *tview.TextView
	inputField     *tview.InputField // 输入框（用于查询命令）
	
	// 数据
	events         []string
	statusMessages []string
	clientCount    int
	scrollOffset   int // 滚动偏移量（用于自动滚动）
	
	// 同步
	mutex          sync.RWMutex
	
	// 上下文
	ctx            context.Context
	server         *RegistryServer
	port           int
}

// NewRegistryUI 创建新的注册服务器界面
func NewRegistryUI(ctx context.Context, server *RegistryServer, port int) *RegistryUI {
	ui := &RegistryUI{
		app:            tview.NewApplication(),
		events:         make([]string, 0),
		statusMessages: make([]string, 0),
		ctx:            ctx,
		server:         server,
		port:           port,
	}
	
	ui.initUI()
	return ui
}

// initUI 初始化UI组件
func (ui *RegistryUI) initUI() {
	// 顶部时间栏
	ui.header = tview.NewTextView()
	ui.header.SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetBorder(true).
		SetTitle(fmt.Sprintf("PChat 注册服务器 - 端口 %d", ui.port))
	
	// 左侧：在线用户状态区域
	ui.clientArea = tview.NewTextView()
	ui.clientArea.SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetBorder(true).
		SetTitle("在线用户状态")
	
	// 右侧：系统信息区域
	ui.systemArea = tview.NewTextView()
	ui.systemArea.SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetBorder(true).
		SetTitle("系统信息")
	
	// 底部状态栏
	ui.statusBar = tview.NewTextView()
	ui.statusBar.SetDynamicColors(true).
		SetBorder(true).
		SetTitle("状态信息")
	
	// 输入框（用于查询命令）
	ui.inputField = tview.NewInputField()
	ui.inputField.SetLabel("> ").
		SetFieldWidth(0).
		SetAcceptanceFunc(func(textToCheck string, lastChar rune) bool {
			return true
		}).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				text := ui.inputField.GetText()
				ui.inputField.SetText("")
				if text != "" {
					go func() {
						select {
						case <-ui.ctx.Done():
							return
						default:
							ui.handleCommand(text)
						}
					}()
				}
			} else if key == tcell.KeyEsc {
				// ESC键退出
				ui.Stop()
			}
		})
	
	// 创建左右布局（中间部分）
	leftRightFlex := tview.NewFlex().
		AddItem(ui.clientArea, 0, 1, false).
		AddItem(ui.systemArea, 0, 1, false)
	
	// 创建主布局
	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(ui.header, 3, 0, false).
		AddItem(leftRightFlex, 0, 1, false).
		AddItem(ui.statusBar, 3, 0, false).
		AddItem(ui.inputField, 1, 0, true)
	
	// 设置根节点和焦点
	ui.app.SetRoot(mainFlex, true).
		SetFocus(ui.inputField)
	
	// 设置全局键盘捕获，处理 Ctrl+C 和 Ctrl+Q
	ui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyCtrlQ {
			go func() {
				ui.AddEvent("[yellow]🛑 正在退出...[white]")
				time.Sleep(100 * time.Millisecond)
				ui.Stop()
			}()
			return nil
		}
		return event
	})
	
	// 启动时间更新
	go ui.updateTime()
	
	// 启动状态栏更新
	go ui.updateStatusBar()
	
	// 启动客户端列表更新
	go ui.updateClientList()
	
	// 启动系统信息更新
	go ui.updateSystemInfo()
}

// updateTime 更新顶部时间栏
func (ui *RegistryUI) updateTime() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ui.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			timeStr := fmt.Sprintf("[white]%s[white]", now.Format("2006-01-02 15:04:05"))
			ui.app.QueueUpdateDraw(func() {
				ui.header.SetText(timeStr)
			})
		}
	}
}

// updateStatusBar 更新状态栏
func (ui *RegistryUI) updateStatusBar() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	heartbeatCount := 0
	cleanupCount := 0
	
	for {
		select {
		case <-ui.ctx.Done():
			return
		case <-ticker.C:
			heartbeatCount++
			
			ui.mutex.RLock()
			statusMsgs := make([]string, len(ui.statusMessages))
			copy(statusMsgs, ui.statusMessages)
			clientCount := ui.clientCount
			ui.mutex.RUnlock()
			
			// 构建状态文本
			statusText := fmt.Sprintf("[green]运行时间: %d秒[white] | ", heartbeatCount)
			statusText += fmt.Sprintf("[cyan]在线客户端: %d[white] | ", clientCount)
			statusText += fmt.Sprintf("[yellow]清理次数: %d[white]", cleanupCount)
			
			// 最新状态消息（最多显示最后3条）
			if len(statusMsgs) > 0 {
				start := len(statusMsgs) - 3
				if start < 0 {
					start = 0
				}
				recentMsgs := statusMsgs[start:]
				statusText += " | [magenta]最新: " + strings.Join(recentMsgs, " | ") + "[white]"
			}
			
			ui.app.QueueUpdateDraw(func() {
				ui.statusBar.SetText(statusText)
			})
		}
	}
}

// updateClientList 更新客户端列表显示（支持自动滚动）
func (ui *RegistryUI) updateClientList() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	scrollTicker := time.NewTicker(5 * time.Second) // 每5秒滚动一次
	defer scrollTicker.Stop()
	
	for {
		select {
		case <-ui.ctx.Done():
			return
		case <-ticker.C:
			ui.refreshClientList()
		case <-scrollTicker.C:
			// 自动滚动：如果用户数量超过20个，每5秒滚动一次
			ui.mutex.Lock()
			if ui.clientCount > 20 {
				ui.scrollOffset = (ui.scrollOffset + 20) % ui.clientCount
			}
			scrollOffset := ui.scrollOffset
			ui.mutex.Unlock()
			
			// 刷新显示
			if scrollOffset > 0 {
				ui.refreshClientList()
			}
		}
	}
}

// refreshClientList 刷新客户端列表（精简显示，支持自动滚动）
func (ui *RegistryUI) refreshClientList() {
	ui.server.mutex.RLock()
	clients := make([]*ClientInfo, 0, len(ui.server.clients))
	for _, client := range ui.server.clients {
		clients = append(clients, client)
	}
	clientCount := len(clients)
	ui.server.mutex.RUnlock()
	
	// 按注册时间排序（早注册的在前）
	sort.Slice(clients, func(i, j int) bool {
		return clients[i].RegisterTime.Before(clients[j].RegisterTime)
	})
	
	ui.mutex.Lock()
	ui.clientCount = clientCount
	// 如果用户数量超过20个，启用自动滚动
	maxDisplay := 20 // 最多显示20个用户
	if clientCount > maxDisplay {
		// 自动滚动：每5秒滚动一次
		// scrollOffset 会在 updateClientList 中更新
	} else {
		ui.scrollOffset = 0
	}
	scrollOffset := ui.scrollOffset
	ui.mutex.Unlock()
	
	// 构建显示文本（精简版）
	var text strings.Builder
	text.WriteString(fmt.Sprintf("[cyan]在线客户端 (%d 个):[white]\n", clientCount))
	text.WriteString("[yellow]─────────────────────────────────────────[white]\n")
	
	if clientCount == 0 {
		text.WriteString("[gray]暂无在线客户端[white]\n")
	} else {
		// 如果用户数量超过maxDisplay，只显示一部分（滚动显示）
		startIdx := scrollOffset
		endIdx := startIdx + maxDisplay
		if endIdx > clientCount {
			endIdx = clientCount
		}
		
		if clientCount > maxDisplay {
			text.WriteString(fmt.Sprintf("[yellow]显示第 %d-%d 个用户 (共 %d 个)[white]\n\n", 
				startIdx+1, endIdx, clientCount))
		}
		
		for i := startIdx; i < endIdx; i++ {
			client := clients[i]
			timeSince := time.Since(client.LastSeen)
			timeStr := formatDuration(timeSince)
			
			// 安全地截取PeerID
			peerIDDisplay := client.PeerID
			if len(peerIDDisplay) > 12 {
				peerIDDisplay = peerIDDisplay[:12] + "..."
			}
			
			// 精简显示：只显示用户名、节点ID、最后心跳时间
			text.WriteString(fmt.Sprintf("[green]%d.[white] [cyan]%s[white] ([yellow]%s[white]) - [gray]%s前[white]\n", 
				i+1, client.Username, peerIDDisplay, timeStr))
		}
		
		if clientCount > maxDisplay {
			text.WriteString(fmt.Sprintf("\n[yellow]提示: 使用 /query <用户名> 查询详细信息[white]\n"))
		}
	}
	
	ui.app.QueueUpdateDraw(func() {
		ui.clientArea.SetText(text.String())
		ui.clientArea.ScrollToBeginning()
	})
}

// updateSystemInfo 更新系统信息（右侧：系统信息）
func (ui *RegistryUI) updateSystemInfo() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ui.ctx.Done():
			return
		case <-ticker.C:
			ui.refreshSystemInfo()
		}
	}
}

// refreshSystemInfo 刷新系统信息显示
func (ui *RegistryUI) refreshSystemInfo() {
	ui.mutex.RLock()
	events := make([]string, len(ui.events))
	copy(events, ui.events)
	ui.mutex.RUnlock()
	
	// 构建显示文本
	var text strings.Builder
	text.WriteString("[yellow]═══════════════════════════════════════════════════════[white]\n")
	text.WriteString("[cyan]系统事件日志:[white]\n")
	text.WriteString("[yellow]═══════════════════════════════════════════════════════[white]\n\n")
	
	if len(events) == 0 {
		text.WriteString("[gray]暂无系统事件[white]\n")
	} else {
		// 显示最后50条事件（从新到旧）
		start := len(events) - 50
		if start < 0 {
			start = 0
		}
		for _, event := range events[start:] {
			// 清理事件文本，移除可能导致显示混乱的字符
			cleanEvent := strings.TrimSpace(event)
			cleanEvent = strings.ReplaceAll(cleanEvent, "\r", "")
			if cleanEvent != "" {
				text.WriteString(cleanEvent)
				text.WriteString("\n")
			}
		}
	}
	
	ui.app.QueueUpdateDraw(func() {
		ui.systemArea.SetText(text.String())
		ui.systemArea.ScrollToEnd()
	})
}

// AddEvent 添加事件
func (ui *RegistryUI) AddEvent(event string) {
	ui.mutex.Lock()
	defer ui.mutex.Unlock()
	
	now := time.Now()
	timeStr := now.Format("15:04:05")
	eventStr := fmt.Sprintf("[gray][%s][white] %s", timeStr, event)
	
	ui.events = append(ui.events, eventStr)
	
	// 限制事件数量（最多保留100条）
	if len(ui.events) > 100 {
		ui.events = ui.events[len(ui.events)-100:]
	}
}

// AddStatusMessage 添加状态消息
func (ui *RegistryUI) AddStatusMessage(message string) {
	ui.mutex.Lock()
	defer ui.mutex.Unlock()
	
	ui.statusMessages = append(ui.statusMessages, message)
	
	// 限制状态消息数量（最多保留10条）
	if len(ui.statusMessages) > 10 {
		ui.statusMessages = ui.statusMessages[len(ui.statusMessages)-10:]
	}
}

// handleCommand 处理命令
func (ui *RegistryUI) handleCommand(cmd string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}
	
	command := parts[0]
	args := parts[1:]
	
	switch command {
	case "/query", "/q":
		if len(args) > 0 {
			target := strings.Join(args, " ")
			ui.queryClient(target)
		} else {
			ui.AddEvent("[yellow]⚠️ 用法: /query <用户名或节点ID>[white]")
		}
	case "/help", "/h":
		ui.showHelp()
	case "/quit", "/exit":
		go func() {
			ui.AddEvent("[yellow]🛑 正在退出...[white]")
			time.Sleep(100 * time.Millisecond)
			ui.Stop()
		}()
	default:
		ui.AddEvent(fmt.Sprintf("[yellow]⚠️ 未知命令: %s (输入 /help 查看帮助)[white]", command))
	}
}

// queryClient 查询客户端详细信息
func (ui *RegistryUI) queryClient(target string) {
	ui.server.mutex.RLock()
	var foundClient *ClientInfo
	for _, client := range ui.server.clients {
		if client.Username == target || client.PeerID == target || strings.HasPrefix(client.PeerID, target) {
			foundClient = client
			break
		}
	}
	ui.server.mutex.RUnlock()
	
	if foundClient == nil {
		ui.AddEvent(fmt.Sprintf("[red]❌ 未找到用户: %s[white]", target))
		return
	}
	
	// 显示详细信息到系统信息区域
	timeSince := time.Since(foundClient.LastSeen)
	timeStr := formatDuration(timeSince)
	registerTimeSince := time.Since(foundClient.RegisterTime)
	registerTimeStr := formatDuration(registerTimeSince)
	
	detailMsg := fmt.Sprintf("[yellow]═══════════════════════════════════════════════════════[white]\n")
	detailMsg += fmt.Sprintf("[cyan]用户详细信息查询[white]\n")
	detailMsg += fmt.Sprintf("[yellow]═══════════════════════════════════════════════════════[white]\n")
	detailMsg += fmt.Sprintf("[green]用户名[white]: [cyan]%s[white]\n", foundClient.Username)
	detailMsg += fmt.Sprintf("[green]节点ID[white]: [yellow]%s[white]\n", foundClient.PeerID)
	detailMsg += fmt.Sprintf("[green]地址[white]: [gray]%v[white]\n", foundClient.Addresses)
	detailMsg += fmt.Sprintf("[green]注册时间[white]: [yellow]%s[white] 前\n", registerTimeStr)
	detailMsg += fmt.Sprintf("[green]最后心跳[white]: [yellow]%s[white] 前\n", timeStr)
	detailMsg += fmt.Sprintf("[yellow]═══════════════════════════════════════════════════════[white]\n")
	
	ui.AddEvent(detailMsg)
}

// showHelp 显示帮助信息
func (ui *RegistryUI) showHelp() {
	helpText := []string{
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		"📖 PChat 命令帮助",
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		"",
		"🔍 查询命令:",
		"  /query 或 /q <用户名或节点ID>        - 查询用户详细信息",
		"",
		"❓ 帮助命令:",
		"  /help 或 /h                         - 显示此帮助信息",
		"",
		"🚪 退出命令:",
		"  /quit 或 /exit                      - 优雅退出程序",
		"",
		"💡 提示: 支持 /q (query) 等首字母简写，便于快速输入",
		"",
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
	}

	for _, line := range helpText {
		ui.AddEvent(line)
	}
}

// formatDuration 格式化时间间隔
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%d毫秒", d.Milliseconds())
	} else if d < time.Minute {
		return fmt.Sprintf("%.1f秒", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.1f分钟", d.Minutes())
	} else {
		return fmt.Sprintf("%.1f小时", d.Hours())
	}
}

// Run 运行UI
func (ui *RegistryUI) Run() error {
	return ui.app.Run()
}

// Stop 停止UI
func (ui *RegistryUI) Stop() {
	ui.app.Stop()
}

