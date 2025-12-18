package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"conchContent-v3/lockintegration"
)

// DownloadImageLayers 并发下载镜像的多个层
// 演示：当某个层被其他节点占用时，可以并发下载其他层，同时轮询等待的层
func DownloadImageLayers(ctx context.Context, serverURL, nodeID string, layers []string) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(map[string]error)

	// 并发下载所有层
	for _, layerID := range layers {
		wg.Add(1)
		go func(layer string) {
			defer wg.Done()

			// 尝试获取锁并下载层
			err := downloadLayer(ctx, serverURL, nodeID, layer)
			mu.Lock()
			results[layer] = err
			mu.Unlock()

			if err != nil {
				log.Printf("[%s] 层 %s 下载失败: %v", nodeID, layer, err)
			} else {
				log.Printf("[%s] 层 %s 下载成功", nodeID, layer)
			}
		}(layerID)
	}

	wg.Wait()

	// 检查结果
	for layer, err := range results {
		if err != nil {
			return fmt.Errorf("层 %s 下载失败: %w", layer, err)
		}
	}

	return nil
}

// downloadLayer 下载单个层
// 如果层被其他节点占用，会进入轮询等待，如果其他节点完成，会跳过下载
func downloadLayer(ctx context.Context, serverURL, nodeID, layerID string) error {
	// 打开Writer（会尝试获取锁）
	writer, err := lockintegration.OpenWriter(ctx, serverURL, nodeID, layerID)
	if err != nil {
		return fmt.Errorf("打开Writer失败: %w", err)
	}
	defer writer.Close(ctx)

	// 检查是否跳过了操作（其他节点已完成）
	if writer.Skipped() {
		log.Printf("[%s] 层 %s 已由其他节点完成，跳过下载", nodeID, layerID)
		return nil // 跳过下载，直接返回成功
	}

	// 检查是否获得锁
	if !writer.Locked() {
		return fmt.Errorf("未获得锁，无法下载层 %s", layerID)
	}

	log.Printf("[%s] 开始下载层 %s", nodeID, layerID)

	// 模拟下载过程
	// 在实际使用中，这里应该是真正的下载逻辑
	if err := simulateDownload(layerID); err != nil {
		// 下载失败，提交失败结果
		if commitErr := writer.Commit(ctx, false, err); commitErr != nil {
			return fmt.Errorf("提交失败结果失败: %w", commitErr)
		}
		return err
	}

	// 下载成功，提交成功结果
	if err := writer.Commit(ctx, true, nil); err != nil {
		return fmt.Errorf("提交成功结果失败: %w", err)
	}

	log.Printf("[%s] 层 %s 下载完成", nodeID, layerID)
	return nil
}

// simulateDownload 模拟下载过程
func simulateDownload(layerID string) error {
	// 模拟下载时间
	time.Sleep(2 * time.Second)
	return nil
}

// ExampleConcurrentDownload 示例：节点A和节点B同时下载镜像
func ExampleConcurrentDownload() {
	ctx := context.Background()
	serverURL := "http://localhost:8080"

	// 镜像的多个层
	layers := []string{
		"sha256:layer1",
		"sha256:layer2",
		"sha256:layer3",
		"sha256:layer4",
	}

	// 节点A和节点B同时开始下载
	var wg sync.WaitGroup

	// 节点A开始下载
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("节点A开始下载镜像...")
		if err := DownloadImageLayers(ctx, serverURL, "NODEA", layers); err != nil {
			log.Printf("节点A下载失败: %v", err)
		} else {
			log.Println("节点A下载完成")
		}
	}()

	// 等待一小段时间，让节点A先开始
	time.Sleep(100 * time.Millisecond)

	// 节点B开始下载（稍后开始，模拟同时请求的场景）
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("节点B开始下载镜像...")
		if err := DownloadImageLayers(ctx, serverURL, "NODEB", layers); err != nil {
			log.Printf("节点B下载失败: %v", err)
		} else {
			log.Println("节点B下载完成")
		}
	}()

	wg.Wait()
	log.Println("所有节点下载完成")
}

// 注意：需要在 lockintegration.Writer 中添加 Locked() 方法
// 如果还没有，需要添加：
// func (w *Writer) Locked() bool {
//     return w.locked
// }

