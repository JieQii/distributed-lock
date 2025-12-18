package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	serverURL = "http://127.0.0.1:8080"
)

type LockRequest struct {
	Type       string `json:"type"`
	ResourceID string `json:"resource_id"`
	NodeID     string `json:"node_id"`
}

type LockResponse struct {
	Acquired bool   `json:"acquired"`
	Skip     bool   `json:"skip"`
	Message  string `json:"message"`
	Error    string `json:"error,omitempty"`
}

type UnlockRequest struct {
	Type       string `json:"type"`
	ResourceID string `json:"resource_id"`
	NodeID     string `json:"node_id"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

type StatusResponse struct {
	Acquired  bool `json:"acquired"`
	Completed bool `json:"completed"`
	Success   bool `json:"success"`
}

// downloadLayer 下载单个层
func downloadLayer(nodeID, layerID string, duration time.Duration) error {
	log.Printf("[%s] 🚀 开始下载层 %s (预计耗时: %v)", nodeID, layerID, duration)
	time.Sleep(duration)
	log.Printf("[%s] ✅ 层 %s 下载完成", nodeID, layerID)
	return nil
}

// requestLock 请求锁
func requestLock(nodeID, layerID string) (*LockResponse, error) {
	req := LockRequest{
		Type:       "pull",
		ResourceID: layerID,
		NodeID:     nodeID,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(serverURL+"/lock", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var lockResp LockResponse
	if err := json.Unmarshal(body, &lockResp); err != nil {
		return nil, err
	}

	return &lockResp, nil
}

// unlock 释放锁
func unlock(nodeID, layerID string, success bool) error {
	req := UnlockRequest{
		Type:       "pull",
		ResourceID: layerID,
		NodeID:     nodeID,
		Success:    success,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := http.Post(serverURL+"/unlock", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// queryStatus 查询锁状态
func queryStatus(nodeID, layerID string) (*StatusResponse, error) {
	url := fmt.Sprintf("%s/lock/status?type=pull&resource_id=%s&node_id=%s", serverURL, layerID, nodeID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var statusResp StatusResponse
	if err := json.Unmarshal(body, &statusResp); err != nil {
		return nil, err
	}

	return &statusResp, nil
}

// processLayer 处理单个层的下载
func processLayer(nodeID, layerID string, layerDuration time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Printf("[%s] 📋 请求层 %s 的锁...", nodeID, layerID)

	// 请求锁
	lockResp, err := requestLock(nodeID, layerID)
	if err != nil {
		log.Printf("[%s] ❌ 请求层 %s 的锁失败: %v", nodeID, layerID, err)
		return
	}

	log.Printf("[%s] 🔒 层 %s 锁响应: acquired=%v, skip=%v, message=%s",
		nodeID, layerID, lockResp.Acquired, lockResp.Skip, lockResp.Message)

	// 如果跳过，说明其他节点已完成
	if lockResp.Skip {
		log.Printf("[%s] ⏭️  层 %s 已由其他节点完成，跳过下载", nodeID, layerID)
		return
	}

	// 如果获得锁，直接下载
	if lockResp.Acquired {
		log.Printf("[%s] ✅ 获得层 %s 的锁，开始下载", nodeID, layerID)
		if err := downloadLayer(nodeID, layerID, layerDuration); err != nil {
			log.Printf("[%s] ❌ 层 %s 下载失败: %v", nodeID, layerID, err)
			unlock(nodeID, layerID, false)
			return
		}
		log.Printf("[%s] 🔓 释放层 %s 的锁（成功）", nodeID, layerID)
		unlock(nodeID, layerID, true)
		return
	}

	// 如果没有获得锁，进入轮询等待
	log.Printf("[%s] ⏳ 层 %s 未获得锁，进入轮询等待...", nodeID, layerID)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second)

	for {
		select {
		case <-timeout:
			log.Printf("[%s] ⏰ 层 %s 等待超时", nodeID, layerID)
			return
		case <-ticker.C:
			status, err := queryStatus(nodeID, layerID)
			if err != nil {
				log.Printf("[%s] ⚠️  查询层 %s 状态失败: %v", nodeID, layerID, err)
				continue
			}

			log.Printf("[%s] 🔍 轮询层 %s 状态: acquired=%v, completed=%v, success=%v",
				nodeID, layerID, status.Acquired, status.Completed, status.Success)

			// 如果操作已完成且成功，跳过下载
			if status.Completed && status.Success {
				log.Printf("[%s] ⏭️  层 %s 已由其他节点完成，跳过下载", nodeID, layerID)
				return
			}

			// 如果获得锁，开始下载
			if status.Acquired {
				log.Printf("[%s] ✅ 从队列中获得层 %s 的锁，开始下载", nodeID, layerID)
				if err := downloadLayer(nodeID, layerID, layerDuration); err != nil {
					log.Printf("[%s] ❌ 层 %s 下载失败: %v", nodeID, layerID, err)
					unlock(nodeID, layerID, false)
					return
				}
				log.Printf("[%s] 🔓 释放层 %s 的锁（成功）", nodeID, layerID)
				unlock(nodeID, layerID, true)
				return
			}
		}
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("==========================================")
	log.Println("测试场景：节点A和节点B同时下载四个镜像层")
	log.Println("==========================================")
	log.Println("")

	// 检查服务器是否运行
	resp, err := http.Get(serverURL + "/lock")
	if err != nil {
		log.Fatalf("❌ 服务器未运行，请先启动服务器: %v", err)
	}
	resp.Body.Close()
	log.Println("✅ 服务器运行正常")
	log.Println("")

	// 四个镜像层
	layers := []struct {
		ID       string
		Duration time.Duration
	}{
		{"sha256:layer1", 3 * time.Second},
		{"sha256:layer2", 2 * time.Second},
		{"sha256:layer3", 4 * time.Second},
		{"sha256:layer4", 2 * time.Second},
	}

	log.Println("📦 镜像层列表:")
	for i, layer := range layers {
		log.Printf("  层%d: %s (预计耗时: %v)", i+1, layer.ID, layer.Duration)
	}
	log.Println("")

	// 节点A和节点B同时开始下载
	var wg sync.WaitGroup

	// 节点A开始下载（稍微提前一点，模拟先请求）
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("节点A开始下载...")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	for _, layer := range layers {
		wg.Add(1)
		go processLayer("NODEA", layer.ID, layer.Duration, &wg)
	}

	// 等待一小段时间，让节点A先开始
	time.Sleep(200 * time.Millisecond)

	// 节点B开始下载
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("节点B开始下载...")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	for _, layer := range layers {
		wg.Add(1)
		go processLayer("NODEB", layer.ID, layer.Duration, &wg)
	}

	log.Println("")
	log.Println("⏳ 等待所有下载完成...")
	log.Println("")

	// 等待所有goroutine完成
	wg.Wait()

	log.Println("")
	log.Println("==========================================")
	log.Println("✅ 所有下载任务完成")
	log.Println("==========================================")
}

