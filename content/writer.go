package content

import (
	"context"
	"fmt"

	"distributed-lock/callback"
	"distributed-lock/client"
)

// Writer content插件中的Writer实现
type Writer struct {
	client     *client.LockClient
	resourceID string // 镜像层的digest
	lockType   string // 锁类型，例如 "image-layer"
	nodeID     string // 节点ID
	locked     bool   // 是否已获得锁
	skipped    bool   // 是否跳过了操作（操作已完成且成功）

	refCountManager *callback.RefCountManager
	storage         RefCountStorage
}

// NewWriter 创建新的Writer
// serverURL: 锁服务端地址
// nodeID: 当前节点ID
// resourceID: 镜像层的digest
func NewWriter(serverURL, nodeID, resourceID string) (*Writer, error) {
	lockClient := client.NewLockClient(serverURL, nodeID)

	storage := NewLocalRefCountStorage()

	return &Writer{
		client:          lockClient,
		resourceID:      resourceID,
		lockType:        "image-layer",
		nodeID:          nodeID,
		locked:          false,
		skipped:         false,
		storage:         storage,
		refCountManager: callback.NewRefCountManager(storage),
	}, nil
}

// OpenWriter 打开Writer（对应ClusterLock）
// 在调用此函数时会尝试获取分布式锁
func OpenWriter(ctx context.Context, serverURL, nodeID, resourceID string) (*Writer, error) {
	writer, err := NewWriter(serverURL, nodeID, resourceID)
	if err != nil {
		return nil, err
	}

	// 在获取锁之前，先用本地计数判断是否应执行操作
	skip, errMsg := writer.refCountManager.ShouldSkipOperation(callback.OperationTypePull, writer.resourceID)
	if skip {
		writer.skipped = true
		writer.locked = false
		return writer, nil
	}
	if errMsg != "" {
		return nil, fmt.Errorf("操作被拒绝: %s", errMsg)
	}

	// 尝试获取锁
	request := &client.Request{
		Type:       writer.lockType,
		ResourceID: writer.resourceID,
		NodeID:     writer.nodeID,
	}

	// 调用加锁接口
	result, err := client.ClusterLock(ctx, writer.client, request)
	if err != nil {
		return nil, fmt.Errorf("获取锁失败: %w", err)
	}

	// 根据结果设置状态
	if result.Acquired {
		// 获得锁，可以开始操作
		writer.locked = true
		writer.skipped = false
	} else {
		return nil, fmt.Errorf("无法获得锁")
	}

	return writer, nil
}

// Write 写入数据
func (w *Writer) Write(p []byte) (n int, err error) {
	if w.skipped {
		// 如果跳过了操作，不需要写入
		return len(p), nil // 返回成功但不实际写入
	}

	if !w.locked {
		return 0, fmt.Errorf("未获得锁，无法写入")
	}
	// 这里应该实现实际的写入逻辑
	// 例如写入到本地文件系统或对象存储
	return len(p), nil
}

// Commit 提交操作（记录操作结果）
func (w *Writer) Commit(ctx context.Context, success bool, err error) error {
	if w.skipped {
		// 如果跳过了操作，不需要提交
		return nil
	}

	if !w.locked {
		return fmt.Errorf("未获得锁，无法提交")
	}

	// 准备解锁请求
	request := &client.Request{
		Type:       w.lockType,
		ResourceID: w.resourceID,
		NodeID:     w.nodeID,
	}

	// 根据 success 和 err 设置 Error 字段
	// 服务端会根据 Error 自动推断 Success：Error == "" → Success = true
	if err != nil {
		request.Error = err.Error()
	} else {
		request.Error = "" // 空字符串表示操作成功
	}

	// 如果操作成功，先更新本地引用计数
	if success && w.refCountManager != nil {
		result := &callback.OperationResult{
			Success: true,
			NodeID:  w.nodeID,
		}
		w.refCountManager.UpdateRefCount(callback.OperationTypePull, w.resourceID, result)
	}

	// 释放锁
	if unlockErr := client.ClusterUnLock(ctx, w.client, request); unlockErr != nil {
		return fmt.Errorf("释放锁失败: %w", unlockErr)
	}

	w.locked = false
	return nil
}

// Close 关闭Writer（对应ClusterUnLock）
// defer cw.Close() 时会释放锁
func (w *Writer) Close(ctx context.Context) error {
	if w.skipped {
		// 如果跳过了操作，不需要释放锁
		return nil
	}

	if !w.locked {
		return nil // 如果没有锁，直接返回
	}

	// 默认认为操作失败（如果没有调用Commit）
	return w.Commit(ctx, false, fmt.Errorf("Writer关闭时未调用Commit"))
}

// 示例使用方式：
//
// ctx := context.Background()
// cw, err := content.OpenWriter(ctx, "http://localhost:8080", "node-1", "sha256:abc123...")
// if err != nil {
//     log.Fatal(err)
// }
// defer cw.Close(ctx)  // 确保释放锁
//
// // 执行下载操作
// // ... 下载镜像层 ...
//
// // 操作成功或失败后调用Commit
// if err := downloadLayer(); err != nil {
//     cw.Commit(ctx, false, err)  // 操作失败
// } else {
//     cw.Commit(ctx, true, nil)   // 操作成功
// }
