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
	log.Printf("[Lock] 🔒 收到加锁请求: type=%s, resource_id=%s, node_id=%s",
		request.Type, request.ResourceID, request.NodeID)

	acquired, skip, errMsg := h.lockManager.TryLock(&request)

	response := map[string]interface{}{
		"acquired": acquired,
		"skip":     skip, // 兼容字段，server不再决定跳过
	}

	if errMsg != "" {
		// 有错误信息（例如delete操作时引用计数不为0）
		response["message"] = errMsg
		response["error"] = errMsg
		log.Printf("[Lock] ❌ 加锁失败: resource_id=%s, node_id=%s, error=%s",
			request.ResourceID, request.NodeID, errMsg)
		w.WriteHeader(http.StatusForbidden)
	} else if acquired {
		response["message"] = "成功获得锁"
		log.Printf("[Lock] ✅ 成功加锁: resource_id=%s, node_id=%s",
			request.ResourceID, request.NodeID)
	} else if skip {
		response["message"] = "操作已完成，跳过操作"
		log.Printf("[Lock] ⏭️  操作已完成，跳过: resource_id=%s, node_id=%s",
			request.ResourceID, request.NodeID)
	} else {
		response["message"] = "锁已被占用，已加入等待队列"
		log.Printf("[Lock] ⏳ 加入等待队列: resource_id=%s, node_id=%s",
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
	log.Printf("[Unlock] 🔓 收到解锁请求: type=%s, resource_id=%s, node_id=%s, success=%v",
		request.Type, request.ResourceID, request.NodeID, request.Success)

	released := h.lockManager.Unlock(&request)

	response := map[string]interface{}{
		"released": released,
	}

	if released {
		response["message"] = "成功释放锁"
		log.Printf("[Unlock] ✅ 成功释放锁: resource_id=%s, node_id=%s, success=%v",
			request.ResourceID, request.NodeID, request.Success)
	} else {
		response["message"] = "释放锁失败：锁不存在或不是锁的持有者"
		log.Printf("[Unlock] ❌ 释放锁失败: resource_id=%s, node_id=%s",
			request.ResourceID, request.NodeID)
		w.WriteHeader(http.StatusForbidden)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// LockStatus 查询锁状态
func (h *Handler) LockStatus(w http.ResponseWriter, r *http.Request) {
	var request LockRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "无效的请求格式", http.StatusBadRequest)
		return
	}

	// 验证请求参数
	if request.Type == "" || request.ResourceID == "" {
		http.Error(w, "缺少必要参数", http.StatusBadRequest)
		return
	}

	// 获取锁状态
	log.Printf("[LockStatus] 🔍 查询锁状态: type=%s, resource_id=%s, node_id=%s",
		request.Type, request.ResourceID, request.NodeID)

	acquired, completed, success := h.lockManager.GetLockStatus(request.Type, request.ResourceID, request.NodeID)

	response := map[string]interface{}{
		"acquired":  acquired,
		"completed": completed,
		"success":   success,
	}

	log.Printf("[LockStatus] 📊 返回状态: acquired=%v, completed=%v, success=%v",
		acquired, completed, success)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/lock", h.Lock).Methods("POST")
	router.HandleFunc("/unlock", h.Unlock).Methods("POST")
	router.HandleFunc("/lock/status", h.LockStatus).Methods("GET")
}
