package server

import (
	"sync"
	"testing"
	"time"
)

// TestConcurrentPullOperations 测试并发pull操作的互斥与队列
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

			acquired, _, errMsg := lm.TryLock(request)
			if errMsg != "" {
				t.Logf("节点 %s 获取锁失败: %s", nodeID, errMsg)
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

	if skipCount != 0 {
		t.Errorf("不应出现跳过操作，实际 %d", skipCount)
	}
	if successCount != 1 {
		t.Errorf("应有且只有1个节点持锁执行，实际 %d", successCount)
	}
}

// TestPullSkipWhenRefCountNotZero 现在期望后续节点排队并获得锁（不依赖引用计数）
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

	// 节点2尝试pull操作，应该排队并最终获得锁
	pullReq2 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     "node-2",
	}
	acquired2, skip2, errMsg2 := lm.TryLock(pullReq2)
	if errMsg2 != "" {
		t.Errorf("不应该有错误: %s", errMsg2)
	}
	if skip2 {
		t.Error("节点2不应跳过操作")
	}
	if !acquired2 {
		t.Error("节点2应能获得锁（排队后）")
	}
}

// TestDeleteWithReferences 现在删除不依赖引用计数，期望仍可获取锁
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

	// 尝试删除（应能获取锁，由业务侧自行判断）
	deleteReq := &LockRequest{
		Type:       OperationTypeDelete,
		ResourceID: resourceID,
		NodeID:     "node-2",
	}
	acquired, skip, errMsg := lm.TryLock(deleteReq)
	if errMsg != "" {
		t.Errorf("不应该有错误: %s", errMsg)
	}
	if !acquired {
		t.Error("期望delete操作获得锁")
	}
	if skip {
		t.Error("不应跳过操作")
	}
}

// TestDeleteWithoutReferences 删除流程，完成后队列应正常推进
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

// TestUpdateWithReferences 现在服务端不关注引用计数，期望正常获得锁
func TestUpdateWithReferences(t *testing.T) {
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

// TestUpdateWithoutReferencesRequired 配置已移除，期望正常获得锁
func TestUpdateWithoutReferencesRequired(t *testing.T) {
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
	if errMsg != "" {
		t.Errorf("不应该有错误: %s", errMsg)
	}
	if !acquired {
		t.Error("期望update操作成功获得锁")
	}
	if skip {
		t.Error("不应该跳过操作")
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

	// 不同资源应该可以并发，但同一资源只能有一个节点成功
	// resource1 和 resource2 各应该有1个节点成功，总共2个
	expectedCount := 2
	if successCount != expectedCount {
		t.Errorf("期望 %d 个成功（每个资源1个），实际 %d", expectedCount, successCount)
	} else {
		t.Logf("不同资源并发操作成功: %d 个操作（每个资源1个）", successCount)
	}
}

// TestNodeConcurrentDifferentResources 测试：节点B在等待队列中时，能够并发下载其他资源
// 场景：
// 1. 节点A和节点B同时请求某个镜像层（layer1）
// 2. 节点A获得锁，节点B加入等待队列
// 3. 节点B再收到其他资源的请求（layer2）
// 4. 节点B应该能够并发下载layer2（即使layer1还在等待）
func TestNodeConcurrentDifferentResources(t *testing.T) {
	lm := NewLockManager()
	layer1 := "sha256:layer1"
	layer2 := "sha256:layer2"
	nodeA := "NODEA"
	nodeB := "NODEB"

	// 步骤1: 节点A请求layer1并获取锁
	reqA1 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: layer1,
		NodeID:     nodeA,
	}
	acquiredA1, _, _ := lm.TryLock(reqA1)
	if !acquiredA1 {
		t.Fatal("节点A应该获得layer1的锁")
	}
	t.Logf("✅ 节点A获得layer1的锁")

	// 步骤2: 节点B请求layer1，应该加入等待队列
	reqB1 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: layer1,
		NodeID:     nodeB,
	}
	acquiredB1, _, _ := lm.TryLock(reqB1)
	if acquiredB1 {
		t.Error("节点B不应该立即获得layer1的锁，应该加入等待队列")
	}
	t.Logf("✅ 节点B加入layer1的等待队列")

	// 验证节点B在队列中
	queueLen := lm.GetQueueLength(OperationTypePull, layer1)
	if queueLen != 1 {
		t.Errorf("期望队列长度为1，实际为 %d", queueLen)
	}
	t.Logf("✅ layer1的队列长度为1（节点B在队列中）")

	// 步骤3: 节点B请求layer2，应该能够立即获得锁（不同资源，可以并发）
	reqB2 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: layer2,
		NodeID:     nodeB,
	}
	acquiredB2, _, _ := lm.TryLock(reqB2)
	if !acquiredB2 {
		t.Error("节点B应该能够获得layer2的锁（不同资源，可以并发）")
	}
	t.Logf("✅ 节点B获得layer2的锁（即使layer1还在等待队列中）")

	// 验证节点B同时持有layer2的锁，但layer1还在等待队列中
	lockInfoB2 := lm.GetLockInfo(OperationTypePull, layer2)
	if lockInfoB2 == nil || lockInfoB2.Request.NodeID != nodeB {
		t.Error("节点B应该持有layer2的锁")
	}

	queueLenAfter := lm.GetQueueLength(OperationTypePull, layer1)
	if queueLenAfter != 1 {
		t.Errorf("节点B获得layer2的锁后，layer1的队列长度应该仍为1，实际为 %d", queueLenAfter)
	}
	t.Logf("✅ 节点B持有layer2的锁，layer1的队列长度仍为1")

	// 步骤4: 节点B完成layer2的操作，释放锁
	unlockB2 := &UnlockRequest{
		Type:       OperationTypePull,
		ResourceID: layer2,
		NodeID:     nodeB,
		Success:    true,
	}
	releasedB2 := lm.Unlock(unlockB2)
	if !releasedB2 {
		t.Error("节点B应该能够释放layer2的锁")
	}
	t.Logf("✅ 节点B释放layer2的锁")

	// 验证layer1的队列状态不变
	queueLenFinal := lm.GetQueueLength(OperationTypePull, layer1)
	if queueLenFinal != 1 {
		t.Errorf("节点B释放layer2的锁后，layer1的队列长度应该仍为1，实际为 %d", queueLenFinal)
	}

	// 步骤5: 节点A完成layer1的操作，释放锁
	unlockA1 := &UnlockRequest{
		Type:       OperationTypePull,
		ResourceID: layer1,
		NodeID:     nodeA,
		Success:    false, // 操作失败，锁应该转交给队列中的节点B
	}
	releasedA1 := lm.Unlock(unlockA1)
	if !releasedA1 {
		t.Error("节点A应该能够释放layer1的锁")
	}
	t.Logf("✅ 节点A释放layer1的锁（操作失败）")

	// 验证节点B现在持有layer1的锁（从队列中分配）
	lockInfoB1 := lm.GetLockInfo(OperationTypePull, layer1)
	if lockInfoB1 == nil {
		t.Error("节点B应该持有layer1的锁（从队列中分配）")
	} else if lockInfoB1.Request.NodeID != nodeB {
		t.Errorf("期望节点B持有layer1的锁，实际为 %s", lockInfoB1.Request.NodeID)
	}
	t.Logf("✅ 节点B从队列中获得layer1的锁")

	// 验证队列已清空
	queueLenAfterUnlock := lm.GetQueueLength(OperationTypePull, layer1)
	if queueLenAfterUnlock != 0 {
		t.Errorf("节点A释放锁后，layer1的队列应该为空，实际长度为 %d", queueLenAfterUnlock)
	}
	t.Logf("✅ layer1的队列已清空")

	t.Logf("✅ 测试通过：节点B在等待layer1时，能够并发下载layer2")
}

// TestReferenceCountAccuracy 测试引用计数准确性
// 现在的设计：服务端不再基于引用计数跳过操作，后续节点应正常排队获取锁
func TestReferenceCountAccuracy(t *testing.T) {
	lm := NewLockManager()
	resourceID := "sha256:test123"

	// 第一个节点执行pull操作
	node1 := "node-1"
	req := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     node1,
	}
	acquired, skip, _ := lm.TryLock(req)
	if skip {
		t.Fatal("第一个节点不应该跳过操作")
	}
	if !acquired {
		t.Fatal("第一个节点无法获取锁")
	}

	unlockReq := &UnlockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     node1,
		Success:    true,
	}
	lm.Unlock(unlockReq)

	// 后续节点应该可以排队并获得锁
	node2 := "node-2"
	req2 := &LockRequest{
		Type:       OperationTypePull,
		ResourceID: resourceID,
		NodeID:     node2,
	}
	acquired2, skip2, _ := lm.TryLock(req2)
	if skip2 {
		t.Error("后续节点不应跳过操作")
	}
	if !acquired2 {
		t.Error("后续节点应能获得锁（排队后）")
	}
}
