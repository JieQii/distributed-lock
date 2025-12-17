#!/bin/bash
# test-lock.sh - 测试分布式锁功能

RESOURCE_ID="${1:-sha256:test-$(date +%s)}"
SERVER_URL="http://127.0.0.1:8080"

echo "=========================================="
echo "测试分布式锁功能"
echo "=========================================="
echo "资源 ID: $RESOURCE_ID"
echo ""

# 测试 1: 获取锁
echo "[测试 1] 节点 A 获取锁..."
RESPONSE1=$(curl -s -X POST "$SERVER_URL/lock" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"pull\",
    \"resource_id\": \"$RESOURCE_ID\",
    \"node_id\": \"NODEA\"
  }")
echo "响应: $RESPONSE1"
echo ""

# 测试 2: 节点 B 尝试获取同一资源的锁（应该排队或跳过）
echo "[测试 2] 节点 B 尝试获取同一资源的锁..."
RESPONSE2=$(curl -s -X POST "$SERVER_URL/lock" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"pull\",
    \"resource_id\": \"$RESOURCE_ID\",
    \"node_id\": \"NODEB\"
  }")
echo "响应: $RESPONSE2"
echo ""

# 测试 3: 节点 A 释放锁（成功）
echo "[测试 3] 节点 A 释放锁（成功）..."
RESPONSE3=$(curl -s -X POST "$SERVER_URL/unlock" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"pull\",
    \"resource_id\": \"$RESOURCE_ID\",
    \"node_id\": \"NODEA\",
    \"success\": true
  }")
echo "响应: $RESPONSE3"
echo ""

# 测试 4: 节点 B 现在应该能获取锁
echo "[测试 4] 节点 B 再次尝试获取锁..."
RESPONSE4=$(curl -s -X POST "$SERVER_URL/lock" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"pull\",
    \"resource_id\": \"$RESOURCE_ID\",
    \"node_id\": \"NODEB\"
  }")
echo "响应: $RESPONSE4"
echo ""

# 测试 5: 节点 B 释放锁
echo "[测试 5] 节点 B 释放锁..."
RESPONSE5=$(curl -s -X POST "$SERVER_URL/unlock" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"pull\",
    \"resource_id\": \"$RESOURCE_ID\",
    \"node_id\": \"NODEB\",
    \"success\": true
  }")
echo "响应: $RESPONSE5"
echo ""

echo "=========================================="
echo "测试完成"
echo "=========================================="

