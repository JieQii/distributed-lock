package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath" 
	"syscall" 
	"os/signal" 
	"conch-content/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	contentapi "github.com/containerd/containerd/api/services/content/v1"
	contentserver "github.com/containerd/containerd/services/content/contentserver"
)

func main() {
	// Parse configuration
	configPath := flag.String("config", "config.toml", "配置文件路径")
	flag.Parse()

	if *configPath == "" {
		fmt.Println("错误: 必须指定 -config 参数")
		os.Exit(1)
	}
	cfg, err  := ParseConfig(*configPath)

	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}
	// 初始化目录+挂载mergefs
	if err := ensureDirectories(cfg); err != nil {
		fmt.Printf("初始化失败: %v\n", err)
		os.Exit(1)
	}
	// 分布式锁
	lockClient := client.NewLockClient(cfg.LockServiceURL, cfg.CurrentNode.ID)

	// 创建 socket 目录
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0755); err != nil {
		fmt.Printf("无法创建 socket 目录: %v\n", err)
		os.Exit(1)
	}

	// 清理旧 socket
	_ = os.Remove(cfg.SocketPath)

	// 监听 Unix socket（此时会创建新的 socket 文件）
	lis, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		fmt.Printf("无法监听 socket %s: %v\n", cfg.SocketPath, err)
		os.Exit(1)
	}
	defer lis.Close()

	store, err := NewStore(
		filepath.Join(cfg.CurrentNode.Root, "host"),
		filepath.Join(cfg.CurrentNode.Root, "merged"),
		cfg.CurrentNode.ID,
		lockClient,
	)

	// 创建 gRPC 服务器并注册 ContentService
	grpcServer := grpc.NewServer()
	contentService := contentserver.New(store)

	contentapi.RegisterContentServer(grpcServer, contentService)
	reflection.Register(grpcServer)

	// 启动 gRPC 服务（非阻塞）
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			fmt.Printf("gRPC 服务异常退出: %v\n", err)
			os.Exit(1)
		}
	}()

	// 等待中断信号（SIGINT / SIGTERM）
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// 优雅关闭
	fmt.Println(" 正在关闭服务...")
	grpcServer.GracefulStop()
}