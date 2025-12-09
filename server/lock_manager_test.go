package server

import (
	"sync"
	"testing"
	"time"
)

// TestConcurrentPullOperations 测试并发pull操作的引用计数
func TestConcurrentPullOperations(t *testing.T) {
	lm := NewLockManager()
	resourceID := "sha256:test123"
	concurrency := 10

	var wg sync.WaitGroup
	successCount := 0
	skipCount := 0
	var mu sync.Mutex

	// 并发执行pull操作
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(nodeID string) {
			defer wg.Done()

			// 获取锁
			request := &LockRequest{
				Type:       OperationTypePull,
				ResourceID: resourceID,
				NodeID:     nodeID,
			}

			acquired, skip, errMsg := lm.TryLock(request)
			if errMsg != "" {
				t.Logf("节点 %s 获取锁失败: %s", nodeID, errMsg)
				return
			}

			if skip {
				mu.Lock()
				skipCount++
				mu.Unlock()
				t.Logf("节点 %s 跳过操作（refcount != 0）", nodeID)
				return
			}

			if acquired {
				mu.Lock()
				successCount++
				mu.Unlock()

				// 模拟操作
				time.Sleep(10 * time.Millisecond)

				// 释放锁（操作成功）
				unlockReq := &UnlockRequest{
					Type:       OperationTypePull,
					ResourceID: resourceID,
					NodeID:     nodeID,
					Success:    true,
				}
				lm.Unlock(unlockReq)
			}
		}(string(rune('A' + i)))
	}

	wg.Wait()

	// 检查引用计数
	refCount := lm.GetRefCount(resourceID)
	expectedCount := successCount

	if refCount.Count != expectedCount {
		t.Errorf("引用计数不正确: 期望 %d, 实际 %d", expectedCount, refCount.Count)
	} else {
		t.Logf("引用计数正确: %d 个节点正在使用资源", refCount.Count)
		t.Logf("成功执行: %d, 跳过操作: %d", successCount, skipCount)
	}
}

// TestPullSkipWhenRefCountNotZero 测试Pull操作在refcount != 0时跳过
func TestPullSkipWhenRefCountNotZero(t *testing.T) {
	lm := NewLockManager()
	resourceID := "sha256:test123"

	// 先执行pull操作，增加引用计数
	pullReq1 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
	}
	acquired, _, _ := lm.TryLock(pullReq1)
	if !acquired {
		t.Fatal("节点1无法获取pull锁")
	}

	// 释放锁并标记成功（增加引用计数）
	unlockReq1 := &UnlockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
		Success:    true,
	}
	lm.Unlock(unlockReq1)

	// 检查引用计数
	refCount := lm.GetRefCount(resourceID)
	if refCount.Count != 1 {
		t.Errorf("期望引用计数为1，实际为 %d", refCount.Count)
	}

	// 节点2尝试pull操作，应该跳过（因为refcount != 0）
	pullReq2 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-2",
	}
	acquired2, skip2, errMsg2 := lm.TryLock(pullReq2)
	if errMsg2 != "" {
		t.Errorf("不应该有错误: %s", errMsg2)
	}
	if acquired2 {
		t.Error("节点2不应该获得锁，应该跳过操作")
	}
	if !skip2 {
		t.Error("节点2应该跳过操作（refcount != 0）")
	} else {
		t.Log("节点2正确跳过操作（refcount != 0）")
	}
}

// TestDeleteWithReferences 测试有引用时删除操作
func TestDeleteWithReferences(t *testing.T) {
	lm := NewLockManager()
	resourceID := "sha256:test123"

	// 先执行pull操作，增加引用计数
	pullReq := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
	}
	acquired, _, _ := lm.TryLock(pullReq)
	if !acquired {
		t.Fatal("无法获取pull锁")
	}

	// 释放pull锁并标记成功（增加引用计数）
	unlockReq := &UnlockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
		Success:    true,
	}
	lm.Unlock(unlockReq)

	// 检查引用计数
	refCount := lm.GetRefCount(resourceID)
	if refCount.Count != 1 {
		t.Errorf("期望引用计数为1，实际为 %d", refCount.Count)
	}

	// 尝试删除（应该失败）
	deleteReq := &LockRequest{
		Type:       OperationTypeDelete,
		ResourceID: resourceID,
		NodeID:     "node-2",
	}
	acquired, skip, errMsg := lm.TryLock(deleteReq)
	if acquired || skip {
		t.Error("期望delete操作失败，但获得了锁")
	}
	if errMsg == "" {
		t.Error("期望返回错误信息，但没有错误")
	} else {
		t.Logf("正确返回错误: %s", errMsg)
	}
}

// TestDeleteWithoutReferences 测试无引用时删除操作
func TestDeleteWithoutReferences(t *testing.T) {
	lm := NewLockManager()
	resourceID := "sha256:test123"

	// 尝试删除（应该成功，因为没有引用）
	deleteReq := &LockRequest{
		Type:       OperationTypeDelete,
		ResourceID: resourceID,
		NodeID:     "node-1",
	}
	acquired, skip, errMsg := lm.TryLock(deleteReq)
	if errMsg != "" {
		t.Errorf("不应该有错误: %s", errMsg)
	}
	if !acquired {
		t.Error("期望delete操作成功，但没有获得锁")
	}
	if skip {
		t.Error("不应该跳过操作")
	}

	// 释放锁并标记成功
	unlockReq := &UnlockRequest{
		Type:       OperationTypeDelete,
		ResourceID: resourceID,
		NodeID:     "node-1",
		Success:    true,
	}
	lm.Unlock(unlockReq)

	// 检查引用计数应该被清理
	refCount := lm.GetRefCount(resourceID)
	if refCount.Count != 0 {
		t.Errorf("期望引用计数为0，实际为 %d", refCount.Count)
	}
}

// TestDeleteSkipWhenRefCountZero 测试Delete操作在refcount == 0时的情况
// 注意：当前实现中，如果refcount == 0且没有锁，仍然允许获取锁执行delete
// 这是为了处理资源不存在的情况
func TestDeleteWhenRefCountZero(t *testing.T) {
	lm := NewLockManager()
	resourceID := "sha256:test123"

	// refcount == 0，尝试删除（应该可以获取锁，因为资源可能不存在）
	deleteReq := &LockRequest{
		Type:       OperationTypeDelete,
		ResourceID: resourceID,
		NodeID:     "node-1",
	}
	acquired, skip, errMsg := lm.TryLock(deleteReq)
	if errMsg != "" {
		t.Errorf("不应该有错误: %s", errMsg)
	}
	if !acquired {
		t.Error("期望delete操作可以获取锁（refcount == 0）")
	}
	if skip {
		t.Error("不应该跳过操作")
	}

	// 释放锁并标记成功
	unlockReq := &UnlockRequest{
		Type:       OperationTypeDelete,
		ResourceID: resourceID,
		NodeID:     "node-1",
		Success:    true,
	}
	lm.Unlock(unlockReq)
}

// TestUpdateWithReferences 测试有引用时update操作（默认允许）
func TestUpdateWithReferences(t *testing.T) {
	lm := NewLockManager()
	lm.UpdateRequiresNoRef = false // 允许热更新
	resourceID := "sha256:test123"

	// 先执行pull操作，增加引用计数
	pullReq := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
	}
	acquired, _, _ := lm.TryLock(pullReq)
	if !acquired {
		t.Fatal("无法获取pull锁")
	}

	unlockReq := &UnlockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
		Success:    true,
	}
	lm.Unlock(unlockReq)

	// 尝试update（应该成功，因为允许热更新）
	updateReq := &LockRequest{
		Type:       OperationTypeUpdate,
		ResourceID: resourceID,
		NodeID:     "node-2",
	}
	acquired, skip, errMsg := lm.TryLock(updateReq)
	if errMsg != "" {
		t.Errorf("不应该有错误: %s", errMsg)
	}
	if !acquired {
		t.Error("期望update操作成功，但没有获得锁")
	}
	if skip {
		t.Error("不应该跳过操作")
	}
}

// TestUpdateWithoutReferencesRequired 测试配置要求无引用时update操作
func TestUpdateWithoutReferencesRequired(t *testing.T) {
	lm := NewLockManager()
	lm.UpdateRequiresNoRef = true // 不允许热更新
	resourceID := "sha256:test123"

	// 先执行pull操作，增加引用计数
	pullReq := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
	}
	acquired, _, _ := lm.TryLock(pullReq)
	if !acquired {
		t.Fatal("无法获取pull锁")
	}

	unlockReq := &UnlockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
		Success:    true,
	}
	lm.Unlock(unlockReq)

	// 尝试update（应该失败，因为配置要求无引用）
	updateReq := &LockRequest{
		Type:       OperationTypeUpdate,
		ResourceID: resourceID,
		NodeID:     "node-2",
	}
	acquired, skip, errMsg := lm.TryLock(updateReq)
	if acquired || skip {
		t.Error("期望update操作失败，但获得了锁")
	}
	if errMsg == "" {
		t.Error("期望返回错误信息，但没有错误")
	} else {
		t.Logf("正确返回错误: %s", errMsg)
	}
}

// TestFIFOQueue 测试FIFO队列
func TestFIFOQueue(t *testing.T) {
	lm := NewLockManager()
	resourceID := "sha256:test123"

	// 第一个请求获取锁
	req1 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
	}
	acquired1, _, _ := lm.TryLock(req1)
	if !acquired1 {
		t.Fatal("第一个请求应该获得锁")
	}

	// 第二个请求应该进入队列
	req2 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-2",
	}
	acquired2, _, _ := lm.TryLock(req2)
	if acquired2 {
		t.Error("第二个请求不应该立即获得锁")
	}

	// 第三个请求也应该进入队列
	req3 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-3",
	}
	acquired3, _, _ := lm.TryLock(req3)
	if acquired3 {
		t.Error("第三个请求不应该立即获得锁")
	}

	// 释放第一个请求的锁
	unlockReq := &UnlockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-1",
		Success:    false, // 操作失败，锁应该转交给队列中的下一个
	}
	lm.Unlock(unlockReq)

	// 检查队列长度
	queueLen := lm.GetQueueLength(OperationTypePull, resourceID)
	if queueLen != 1 {
		t.Errorf("期望队列长度为1，实际为 %d", queueLen)
	}

	// 检查锁信息（应该是node-2持有锁，因为FIFO）
	lockInfo := lm.GetLockInfo(OperationTypePull, resourceID)
	if lockInfo == nil {
		t.Error("锁信息不应该为nil")
	} else if lockInfo.Request.NodeID != "node-2" {
		t.Errorf("期望node-2持有锁，实际为 %s", lockInfo.Request.NodeID)
	}
}

// TestConcurrentDifferentResources 测试不同资源的并发操作
func TestConcurrentDifferentResources(t *testing.T) {
	lm := NewLockManager()
	resource1 := "sha256:resource1"
	resource2 := "sha256:resource2"

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// 并发操作不同资源
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(resourceID, nodeID string) {
			defer wg.Done()
			req := &LockRequest{
				Type:       OperationTypePull,
				ResourceID: resourceID,
				NodeID:     nodeID,
			}
			acquired, _, _ := lm.TryLock(req)
			if acquired {
				mu.Lock()
				successCount++
				mu.Unlock()

				time.Sleep(10 * time.Millisecond)

				unlockReq := &UnlockRequest{
					Type:       OperationTypePull,
					ResourceID: resourceID,
					NodeID:     nodeID,
					Success:    true,
				}
				lm.Unlock(unlockReq)
			}
		}(resource1, string(rune('A'+i)))
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(resourceID, nodeID string) {
			defer wg.Done()
			req := &LockRequest{
				Type:       OperationTypePull,
				ResourceID: resourceID,
				NodeID:     nodeID,
			}
			acquired, _, _ := lm.TryLock(req)
			if acquired {
				mu.Lock()
				successCount++
				mu.Unlock()

				time.Sleep(10 * time.Millisecond)

				unlockReq := &UnlockRequest{
					Type:       OperationTypePull,
					ResourceID: resourceID,
					NodeID:     nodeID,
					Success:    true,
				}
				lm.Unlock(unlockReq)
			}
		}(resource2, string(rune('F'+i)))
	}

	wg.Wait()

	// 不同资源应该可以并发，所以期望所有请求都成功
	expectedCount := 10
	if successCount != expectedCount {
		t.Errorf("期望 %d 个成功，实际 %d", expectedCount, successCount)
	} else {
		t.Logf("不同资源并发操作成功: %d 个操作", successCount)
	}
}

// TestReferenceCountAccuracy 测试引用计数准确性
func TestReferenceCountAccuracy(t *testing.T) {
	lm := NewLockManager()
	resourceID := "sha256:test123"

	// 执行多个pull操作
	nodes := []string{"node-1", "node-2", "node-3", "node-4", "node-5"}
	for _, nodeID := range nodes {
		req := &LockRequest{
			Type:       OperationTypePull,
			ResourceID: resourceID,
			NodeID:     nodeID,
		}
		acquired, _, _ := lm.TryLock(req)
		if !acquired {
			t.Fatalf("节点 %s 无法获取锁", nodeID)
		}

		unlockReq := &UnlockRequest{
			Type:       OperationTypePull,
			ResourceID: resourceID,
			NodeID:     nodeID,
			Success:    true,
		}
		lm.Unlock(unlockReq)
	}

	// 检查引用计数
	refCount := lm.GetRefCount(resourceID)
	if refCount.Count != len(nodes) {
		t.Errorf("期望引用计数为 %d，实际为 %d", len(nodes), refCount.Count)
	}

	// 检查节点集合
	for _, nodeID := range nodes {
		if !refCount.Nodes[nodeID] {
			t.Errorf("期望节点 %s 在引用集合中，但不在", nodeID)
		}
	}

	t.Logf("引用计数准确: %d 个节点，节点集合: %v", refCount.Count, refCount.Nodes)
}

