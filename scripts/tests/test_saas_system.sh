#!/bin/bash

# SaaS 系统测试脚本

set -e

echo "🧪 开始测试 SaaS 系统..."

API_BASE="http://localhost:8080/api"
AUTH_TOKEN=""

# 颜色输出
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# 1. 测试健康检查
echo ""
echo "📡 步骤 1: 测试健康检查..."

response=$(curl -s -w "\n%{http_code}" http://localhost:8080/health)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | head -n-1)

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✅ 健康检查通过${NC}"
else
    echo -e "${RED}❌ 健康检查失败 (HTTP $http_code)${NC}"
    exit 1
fi

# 2. 测试认证
echo ""
echo "🔐 步骤 2: 测试认证..."

# 这里应该先登录获取 token,简化处理
AUTH_TOKEN="demo_token"

# 3. 测试创建实例
echo ""
echo "🚀 步骤 3: 测试创建实例..."

response=$(curl -s -w "\n%{http_code}" -X POST \
  "$API_BASE/saas/instances/create" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -d '{
    "plan": "professional"
  }')

http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | head -n-1)

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✅ 实例创建成功${NC}"
    instance_id=$(echo "$body" | jq -r '.instance_id')
    echo "   实例ID: $instance_id"
else
    echo -e "${RED}❌ 实例创建失败 (HTTP $http_code)${NC}"
    echo "   响应: $body"
fi

# 4. 测试获取实例列表
echo ""
echo "📋 步骤 4: 测试获取实例列表..."

response=$(curl -s -w "\n%{http_code}" \
  "$API_BASE/saas/instances" \
  -H "Authorization: Bearer $AUTH_TOKEN")

http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | head -n-1)

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✅ 获取实例列表成功${NC}"
    total=$(echo "$body" | jq -r '.total')
    echo "   实例总数: $total"
else
    echo -e "${RED}❌ 获取实例列表失败 (HTTP $http_code)${NC}"
fi

# 5. 测试获取实例指标
if [ -n "$instance_id" ]; then
    echo ""
    echo "📊 步骤 5: 测试获取实例指标..."
    
    response=$(curl -s -w "\n%{http_code}" \
      "$API_BASE/saas/instances/$instance_id/metrics" \
      -H "Authorization: Bearer $AUTH_TOKEN")
    
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)
    
    if [ "$http_code" = "200" ]; then
        echo -e "${GREEN}✅ 获取实例指标成功${NC}"
        echo "$body" | jq '.'
    else
        echo -e "${RED}❌ 获取实例指标失败 (HTTP $http_code)${NC}"
    fi
fi

# 6. 测试计费 API
echo ""
echo "💰 步骤 6: 测试计费 API..."

# 6.1 获取套餐列表
response=$(curl -s -w "\n%{http_code}" \
  "$API_BASE/billing/plans" \
  -H "Authorization: Bearer $AUTH_TOKEN")

http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | head -n-1)

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✅ 获取套餐列表成功${NC}"
    echo "$body" | jq '.plans[] | {id, name, price}'
else
    echo -e "${RED}❌ 获取套餐列表失败 (HTTP $http_code)${NC}"
fi

# 6.2 创建订阅
response=$(curl -s -w "\n%{http_code}" -X POST \
  "$API_BASE/billing/subscriptions/create" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -d '{
    "plan": "professional",
    "email": "test@example.com"
  }')

http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | head -n-1)

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✅ 创建订阅成功${NC}"
else
    echo -e "${RED}❌ 创建订阅失败 (HTTP $http_code)${NC}"
    echo "   响应: $body"
fi

echo ""
echo "✅ 所有测试完成!"

