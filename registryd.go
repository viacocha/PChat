package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

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
	if err := server.Start(); err != nil {
		log.Fatalf("服务器启动失败: %v\n", err)
	}
}

