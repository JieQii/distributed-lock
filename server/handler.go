package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

// Handler HTTP请求处理器
type Handler struct {
	lockManager *LockManager
}

// NewHandler 创建新的处理器
func NewHandler(lockManager *LockManager) *Handler {
	return &Handler{
		lockManager: lockManager,
	}
}

// Lock 加锁处理
func (h *Handler) Lock(w http.ResponseWriter, r *http.Request) {
	var request LockRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "无效的请求格式", http.StatusBadRequest)
		return
	}

	// 验证请求参数
	if request.Type == "" || request.ResourceID == "" || request.NodeID == "" {
		http.Error(w, "缺少必要参数", http.StatusBadRequest)
		return
	}

	// 尝试获取锁
	log.Printf("[Lock] 收到加锁请求: type=%s, resource_id=%s, node_id=%s",
		request.Type, request.ResourceID, request.NodeID)

	acquired, _, errMsg := h.lockManager.TryLock(&request)

	response := map[string]interface{}{
		"acquired": acquired,
		"skip":     false, // 不再使用 skip，上层已经检查过资源是否存在
	}

	if errMsg != "" {
		// 有错误信息（例如delete操作时引用计数不为0）
		response["message"] = errMsg
		response["error"] = errMsg
		log.Printf("[Lock] 加锁失败: resource_id=%s, node_id=%s, error=%s",
			request.ResourceID, request.NodeID, errMsg)
		w.WriteHeader(http.StatusForbidden)
	} else if acquired {
		response["message"] = "成功获得锁"
		log.Printf("[Lock] 成功加锁: resource_id=%s, node_id=%s",
			request.ResourceID, request.NodeID)
	} else {
		response["message"] = "锁已被占用，已加入等待队列"
		log.Printf("[Lock]加入等待队列: resource_id=%s, node_id=%s",
			request.ResourceID, request.NodeID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Unlock 解锁处理
func (h *Handler) Unlock(w http.ResponseWriter, r *http.Request) {
	var request UnlockRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "无效的请求格式", http.StatusBadRequest)
		return
	}

	// 验证请求参数
	if request.Type == "" || request.ResourceID == "" || request.NodeID == "" {
		http.Error(w, "缺少必要参数", http.StatusBadRequest)
		return
	}

	// 释放锁
	// Success 根据 Error 自动推断：没有 error 就是 success
	success := (request.Error == "")
	log.Printf("[Unlock] 收到解锁请求: type=%s, resource_id=%s, node_id=%s, success=%v, error=%s",
		request.Type, request.ResourceID, request.NodeID, success, request.Error)

	released := h.lockManager.Unlock(&request)

	response := map[string]interface{}{
		"released": released,
	}

	if released {
		response["message"] = "成功释放锁"
		log.Printf("[Unlock] 成功释放锁: resource_id=%s, node_id=%s, success=%v",
			request.ResourceID, request.NodeID, success)
	} else {
		response["message"] = "释放锁失败：锁不存在或不是锁的持有者"
		log.Printf("[Unlock] 释放锁失败: resource_id=%s, node_id=%s",
			request.ResourceID, request.NodeID)
		w.WriteHeader(http.StatusForbidden)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Subscribe 订阅资源操作完成事件（SSE）
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	// 解析查询参数
	typeParam := r.URL.Query().Get("type")
	resourceIDParam := r.URL.Query().Get("resource_id")

	if typeParam == "" || resourceIDParam == "" {
		http.Error(w, "缺少必要参数: type 和 resource_id", http.StatusBadRequest)
		return
	}

	log.Printf("[Subscribe] 收到订阅请求: type=%s, resource_id=%s", typeParam, resourceIDParam)

	// 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

	// 创建 SSE 订阅者
	subscriber := NewSSESubscriber(w, r)

	// 注册订阅者
	h.lockManager.Subscribe(typeParam, resourceIDParam, subscriber)

	// 等待连接关闭
	<-r.Context().Done()

	// 取消订阅
	h.lockManager.Unsubscribe(typeParam, resourceIDParam, subscriber)
	log.Printf("[Subscribe] 订阅者断开连接: type=%s, resource_id=%s", typeParam, resourceIDParam)
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/lock", h.Lock).Methods("POST")
	router.HandleFunc("/unlock", h.Unlock).Methods("POST")
	router.HandleFunc("/lock/subscribe", h.Subscribe).Methods("GET")
}
