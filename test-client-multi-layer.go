package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"distributed-lock/client"
)

const (
	serverURL = "http://127.0.0.1:8080"
)

// downloadLayer 模拟下载单个层
func downloadLayer(nodeID, layerID string, duration time.Duration) error {
	log.Printf("[%s] 🚀 开始下载层 %s (预计耗时: %v)", nodeID, layerID, duration)
	time.Sleep(duration)
	log.Printf("[%s] ✅ 层 %s 下载完成", nodeID, layerID)
	return nil
}

// processLayer 处理单个层的下载（使用真实的client库）
func processLayer(ctx context.Context, lockClient *client.LockClient, nodeID, layerID string, layerDuration time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Printf("[%s] 📋 请求层 %s 的锁...", nodeID, layerID)

	// 使用真实的client库请求锁
	request := &client.Request{
		Type:       client.OperationTypePull,
		ResourceID: layerID,
		NodeID:     nodeID,
	}

	result, err := lockClient.Lock(ctx, request)
	if err != nil {
		log.Printf("[%s] ❌ 请求层 %s 的锁失败: %v", nodeID, layerID, err)
		return
	}

	log.Printf("[%s] 🔒 层 %s 锁结果: acquired=%v",
		nodeID, layerID, result.Acquired)

	// 检查是否有错误（包括其他节点已完成操作的情况）
	if result.Error != nil {
		log.Printf("[%s] ⚠️  层 %s 获取锁时发生错误: %v", nodeID, layerID, result.Error)
		return
	}

	// 如果获得锁，直接下载
	if result.Acquired {
		log.Printf("[%s] ✅ 获得层 %s 的锁，开始下载", nodeID, layerID)
		if err := downloadLayer(nodeID, layerID, layerDuration); err != nil {
			log.Printf("[%s] ❌ 层 %s 下载失败: %v", nodeID, layerID, err)
			request.Error = err.Error() // 设置错误信息，服务端会根据 Error 自动推断 Success = false
			if unlockErr := lockClient.Unlock(ctx, request); unlockErr != nil {
				log.Printf("[%s] ⚠️  释放层 %s 的锁失败: %v", nodeID, layerID, unlockErr)
			}
			return
		}
		log.Printf("[%s] 🔓 释放层 %s 的锁（成功）", nodeID, layerID)
		request.Error = "" // 空字符串表示操作成功，服务端会根据 Error 自动推断 Success = true
		if err := lockClient.Unlock(ctx, request); err != nil {
			log.Printf("[%s] ⚠️  释放层 %s 的锁失败: %v", nodeID, layerID, err)
		}
		return
	}

	// 如果没有获得锁且没有错误，说明锁被其他节点持有，需要等待
	// 这种情况应该通过 SSE 订阅等待，理论上不应该到达这里
	log.Printf("[%s] ⚠️  层 %s 未获得锁（异常情况，应该通过 SSE 订阅等待）", nodeID, layerID)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("==========================================")
	log.Println("测试场景：节点A和节点B同时下载四个镜像层")
	log.Println("（使用真实的client库）")
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

	// 四个镜像层（使用时间戳确保每次测试使用唯一的ID）
	timestamp := time.Now().Unix()
	layers := []struct {
		ID       string
		Duration time.Duration
	}{
		{fmt.Sprintf("sha256:layer1-%d", timestamp), 3 * time.Second},
		{fmt.Sprintf("sha256:layer2-%d", timestamp), 2 * time.Second},
		{fmt.Sprintf("sha256:layer3-%d", timestamp), 4 * time.Second},
		{fmt.Sprintf("sha256:layer4-%d", timestamp), 2 * time.Second},
	}

	log.Println("📦 镜像层列表:")
	for i, layer := range layers {
		log.Printf("  层%d: %s (预计耗时: %v)", i+1, layer.ID, layer.Duration)
	}
	log.Println("")

	// 创建context（带超时）
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 节点A和节点B同时开始下载
	var wg sync.WaitGroup

	// 创建节点A的client
	clientA := client.NewLockClient(serverURL, "NODEA")
	clientA.RequestTimeout = 5 * time.Second
	clientA.MaxRetries = 3
	clientA.RetryInterval = 100 * time.Millisecond

	// 创建节点B的client
	clientB := client.NewLockClient(serverURL, "NODEB")
	clientB.RequestTimeout = 5 * time.Second
	clientB.MaxRetries = 3
	clientB.RetryInterval = 100 * time.Millisecond

	// 节点A开始下载（稍微提前一点，模拟先请求）
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("节点A开始下载...")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	for _, layer := range layers {
		wg.Add(1)
		go processLayer(ctx, clientA, "NODEA", layer.ID, layer.Duration, &wg)
	}

	// 等待一小段时间，让节点A先开始
	time.Sleep(200 * time.Millisecond)

	// 节点B开始下载
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("节点B开始下载...")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	for _, layer := range layers {
		wg.Add(1)
		go processLayer(ctx, clientB, "NODEB", layer.ID, layer.Duration, &wg)
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
