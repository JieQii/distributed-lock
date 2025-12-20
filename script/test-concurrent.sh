#!/bin/bash
# test-concurrent.sh - 测试并发锁请求

RESOURCE_ID="${1:-sha256:concurrent-test-$(date +%s)}"
SERVER_URL="http://127.0.0.1:8080/lock"

echo "=========================================="
echo "测试并发锁请求"
echo "=========================================="
echo "资源 ID: $RESOURCE_ID"
echo "并发节点数: 3"
echo ""

# 并发发送锁请求
echo "并发发送锁请求..."
for node in NODEA NODEB NODEC; do
  (
    response=$(curl -s -X POST "$SERVER_URL" \
      -H "Content-Type: application/json" \
      -d "{
        \"type\": \"pull\",
        \"resource_id\": \"$RESOURCE_ID\",
        \"node_id\": \"$node\"
      }")
    echo "[$node] $response"
  ) &
done

# 等待所有后台任务完成
wait

echo ""
echo "=========================================="
echo "并发测试完成"
echo "=========================================="
echo ""
echo "注意: 应该只有一个节点获得锁 (acquired: true)"
echo "      其他节点应该排队 (acquired: false) 或跳过 (skip: true)"
echo ""

