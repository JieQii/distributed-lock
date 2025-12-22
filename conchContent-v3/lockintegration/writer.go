package lockintegration

import (
	"context"
	"fmt"

	"conchContent-v3/lockcallback"
	"conchContent-v3/lockclient"
)

// Writer 提供给 content 插件使用的封装
// 封装了：本地引用计数决策 + 分布式锁获取/释放
type Writer struct {
	client     *lockclient.LockClient
	resourceID string // 镜像层的digest
	lockType   string // 锁类型，例如 "image-layer"
	nodeID     string // 节点ID
	locked     bool   // 是否已获得锁
	skipped    bool   // 是否跳过了操作（操作已完成且成功）

	refCountManager *lockcallback.RefCountManager
	storage         RefCountStorage
}

// NewWriter 创建新的 Writer
// serverURL: 锁服务端地址
// nodeID: 当前节点ID
// resourceID: 镜像层的digest
func NewWriter(serverURL, nodeID, resourceID string) (*Writer, error) {
	lockClient := lockclient.NewLockClient(serverURL, nodeID)

	storage := NewLocalRefCountStorage()

	return &Writer{
		client:          lockClient,
		resourceID:      resourceID,
		lockType:        "image-layer",
		nodeID:          nodeID,
		locked:          false,
		skipped:         false,
		storage:         storage,
		refCountManager: lockcallback.NewRefCountManager(storage),
	}, nil
}

// OpenWriter 打开 Writer（对应 ClusterLock）
// 在调用此函数时会：
// 1. 先根据本地引用计数判断是否需要执行操作（ShouldSkipOperation）
// 2. 如需要执行，再向分布式锁 server 请求锁
func OpenWriter(ctx context.Context, serverURL, nodeID, resourceID string) (*Writer, error) {
	writer, err := NewWriter(serverURL, nodeID, resourceID)
	if err != nil {
		return nil, err
	}

	// 在获取锁之前，先用本地计数判断是否应执行操作
	skip, errMsg := writer.refCountManager.ShouldSkipOperation(lockcallback.OperationTypePull, writer.resourceID)
	if skip {
		writer.skipped = true
		writer.locked = false
		return writer, nil
	}
	if errMsg != "" {
		return nil, fmt.Errorf("操作被拒绝: %s", errMsg)
	}

	// 尝试获取锁
	request := &lockclient.Request{
		Type:       writer.lockType,
		ResourceID: writer.resourceID,
		NodeID:     writer.nodeID,
	}

	// 调用加锁接口
	result, err := lockclient.ClusterLock(ctx, writer.client, request)
	if err != nil {
		return nil, fmt.Errorf("获取锁失败: %w", err)
	}

	// 根据结果设置状态
	if result.Skipped {
		// 操作已完成且成功，跳过操作（其他节点已经完成）
		writer.skipped = true
		writer.locked = false
		// 更新本地引用计数：其他节点已完成操作，当前节点也应该增加引用计数
		// 注意：这里使用当前节点ID，表示当前节点"使用"了这个资源
		if writer.refCountManager != nil {
			operationResult := &lockcallback.OperationResult{
				Success: true,
				NodeID:  writer.nodeID,
			}
			writer.refCountManager.UpdateRefCount(lockcallback.OperationTypePull, writer.resourceID, operationResult)
		}
		return writer, nil
	}

	if result.Acquired {
		// 获得锁，可以开始操作
		writer.locked = true
		writer.skipped = false
	} else {
		// 没有获得锁，也没有跳过（可能是错误情况）
		if result.Error != nil {
			return nil, fmt.Errorf("获取锁失败: %w", result.Error)
		}
		return nil, fmt.Errorf("无法获得锁")
	}

	return writer, nil
}

// Skipped 是否跳过了真正的业务操作
func (w *Writer) Skipped() bool {
	return w.skipped
}

// Locked 是否已获得锁
func (w *Writer) Locked() bool {
	return w.locked
}

// Write 写入数据（示例，占位用，真实逻辑由调用方实现）
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
// success: 业务操作是否成功
// err:     业务错误（如有）
func (w *Writer) Commit(ctx context.Context, success bool, err error) error {
	if w.skipped {
		// 如果跳过了操作，不需要提交
		return nil
	}

	if !w.locked {
		return fmt.Errorf("未获得锁，无法提交")
	}

	// 准备解锁请求
	request := &lockclient.Request{
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
		result := &lockcallback.OperationResult{
			Success: true,
			NodeID:  w.nodeID,
		}
		w.refCountManager.UpdateRefCount(lockcallback.OperationTypePull, w.resourceID, result)
	}

	// 释放锁
	if unlockErr := lockclient.ClusterUnLock(ctx, w.client, request); unlockErr != nil {
		return fmt.Errorf("释放锁失败: %w", unlockErr)
	}

	w.locked = false
	return nil
}

// Close 关闭 Writer（对应 ClusterUnLock）
// 建议使用：defer w.Close(ctx)
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
