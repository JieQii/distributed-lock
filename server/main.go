package server

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/mux"
)

func main() {
	// 读取多节点下载模式配置（默认开启）
	allowMultiNodeDownload := true
	if envValue := os.Getenv("ALLOW_MULTI_NODE_DOWNLOAD"); envValue != "" {
		if parsed, err := strconv.ParseBool(envValue); err == nil {
			allowMultiNodeDownload = parsed
		} else {
			log.Printf("警告: 无法解析环境变量 ALLOW_MULTI_NODE_DOWNLOAD=%s，使用默认值 true", envValue)
		}
	}

	log.Printf("多节点下载模式: %v", allowMultiNodeDownload)
	if !allowMultiNodeDownload {
		log.Printf("注意: 多节点下载模式已关闭，锁被占用时将直接返回失败，不进入等待队列")
	}

	// 创建锁管理器
	lockManager := NewLockManager(allowMultiNodeDownload)

	// 创建HTTP处理器
	handler := NewHandler(lockManager)

	// 创建路由
	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	// 获取端口号（默认8086）
	port := os.Getenv("PORT")
	if port == "" {
		port = "8086"
	}

	// 启动HTTP服务器
	log.Printf("锁服务端启动在端口 %s", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}
