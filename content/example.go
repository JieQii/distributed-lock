package content

import (
	"context"
	"fmt"
	"log"
)

// ExampleUsage 示例：如何使用content插件下载镜像层
func ExampleUsage() {
	ctx := context.Background()

	// 配置信息
	serverURL := "http://localhost:8080" // 锁服务端地址
	nodeID := "node-1"                   // 当前节点ID
	layerDigest := "sha256:abc123def456" // 镜像层的digest

	// 打开Writer，自动获取锁
	cw, err := OpenWriter(ctx, serverURL, nodeID, layerDigest)
	if err != nil {
		log.Fatalf("打开Writer失败: %v", err)
	}
	defer func() {
		// 确保释放锁（如果跳过了操作，Close不会做任何事情）
		if err := cw.Close(ctx); err != nil {
			log.Printf("关闭Writer失败: %v", err)
		}
	}()

	// 检查是否跳过了操作（操作已完成且成功）
	// 注意：Writer内部会处理跳过逻辑，这里只是示例
	// 如果跳过了操作，不需要执行下载

	// 执行下载操作
	success := false
	var downloadErr error

	// 模拟下载镜像层
	if err := downloadImageLayer(layerDigest); err != nil {
		downloadErr = err
		log.Printf("下载镜像层失败: %v", err)
	} else {
		success = true
		log.Printf("下载镜像层成功")
	}

	// 提交操作结果（成功或失败）
	// 如果跳过了操作，Commit会自动处理
	if err := cw.Commit(ctx, success, downloadErr); err != nil {
		log.Printf("提交操作结果失败: %v", err)
	}
}

// downloadImageLayer 模拟下载镜像层的函数
func downloadImageLayer(digest string) error {
	// 这里实现实际的下载逻辑
	// 例如：从镜像仓库下载层数据并保存到本地
	fmt.Printf("正在下载镜像层: %s\n", digest)

	// 模拟下载过程
	// 实际实现中，这里应该：
	// 1. 从镜像仓库获取层数据
	// 2. 验证digest
	// 3. 保存到本地存储

	return nil
}

// ExampleMultiLayer 示例：下载多层镜像
func ExampleMultiLayer() {
	ctx := context.Background()
	serverURL := "http://localhost:8080"
	nodeID := "node-1"

	// 镜像的多个层
	layers := []string{
		"sha256:layer1",
		"sha256:layer2",
		"sha256:layer3",
		"sha256:layer4",
	}

	for _, layerDigest := range layers {
		// 每个层都需要获取锁
		cw, err := OpenWriter(ctx, serverURL, nodeID, layerDigest)
		if err != nil {
			log.Printf("层 %s 获取锁失败: %v", layerDigest, err)
			continue
		}

		// 下载层
		success := false
		var downloadErr error

		if err := downloadImageLayer(layerDigest); err != nil {
			downloadErr = err
		} else {
			success = true
		}

		// 提交结果并释放锁
		if err := cw.Commit(ctx, success, downloadErr); err != nil {
			log.Printf("层 %s 提交失败: %v", layerDigest, err)
		}

		cw.Close(ctx)
	}
}

