package server

import (
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

func main() {
	// 创建锁管理器
	lockManager := NewLockManager()

	// 创建HTTP处理器
	handler := NewHandler(lockManager)

	// 创建路由
	router := mux.NewRouter()
	handler.RegisterRoutes(router)

	// 获取端口号（默认8080）
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// 启动HTTP服务器
	log.Printf("锁服务端启动在端口 %s", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}
