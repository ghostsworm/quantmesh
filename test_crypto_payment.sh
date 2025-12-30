#!/bin/bash

# QuantMesh 加密货币支付系统测试脚本

set -e

echo "🧪 开始测试加密货币支付系统..."

# 配置
API_URL="${API_URL:-http://localhost:8080}"
TOKEN="${TOKEN:-demo_token}"

# 颜色输出
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 测试函数
test_api() {
    local name=$1
    local method=$2
    local endpoint=$3
    local data=$4
    
    echo -e "\n${YELLOW}测试: $name${NC}"
    echo "请求: $method $endpoint"
    
    if [ -z "$data" ]; then
        response=$(curl -s -X $method "$API_URL$endpoint" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json")
    else
        echo "数据: $data"
        response=$(curl -s -X $method "$API_URL$endpoint" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$data")
    fi
    
    echo "响应: $response"
    
    # 检查是否有错误
    if echo "$response" | grep -q '"error"'; then
        echo -e "${RED}❌ 测试失败${NC}"
        return 1
    else
        echo -e "${GREEN}✅ 测试通过${NC}"
        return 0
    fi
}

# 1. 测试获取支持的加密货币
echo -e "\n${GREEN}=== 测试 1: 获取支持的加密货币 ===${NC}"
test_api "获取支持的加密货币" "GET" "/api/payment/crypto/currencies"

# 2. 测试创建 Coinbase 支付
echo -e "\n${GREEN}=== 测试 2: 创建 Coinbase Commerce 支付 ===${NC}"
coinbase_response=$(curl -s -X POST "$API_URL/api/payment/crypto/coinbase/create" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{
        "plan": "professional",
        "email": "test@example.com"
    }')

echo "响应: $coinbase_response"

if echo "$coinbase_response" | grep -q '"payment_id"'; then
    coinbase_payment_id=$(echo "$coinbase_response" | grep -o '"payment_id":[0-9]*' | grep -o '[0-9]*')
    echo -e "${GREEN}✅ Coinbase 支付创建成功: ID=$coinbase_payment_id${NC}"
else
    echo -e "${YELLOW}⚠️ Coinbase 支付创建失败 (可能未配置 API Key)${NC}"
    coinbase_payment_id=""
fi

# 3. 测试创建直接钱包支付
echo -e "\n${GREEN}=== 测试 3: 创建直接钱包支付 ===${NC}"
direct_response=$(curl -s -X POST "$API_URL/api/payment/crypto/direct/create" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{
        "plan": "professional",
        "email": "test@example.com",
        "crypto_currency": "USDT"
    }')

echo "响应: $direct_response"

if echo "$direct_response" | grep -q '"payment_id"'; then
    direct_payment_id=$(echo "$direct_response" | grep -o '"payment_id":[0-9]*' | grep -o '[0-9]*')
    payment_address=$(echo "$direct_response" | grep -o '"payment_address":"[^"]*"' | cut -d'"' -f4)
    crypto_amount=$(echo "$direct_response" | grep -o '"crypto_amount":[0-9.]*' | grep -o '[0-9.]*')
    
    echo -e "${GREEN}✅ 直接支付创建成功:${NC}"
    echo "  - 支付 ID: $direct_payment_id"
    echo "  - 支付地址: $payment_address"
    echo "  - 支付金额: $crypto_amount USDT"
else
    echo -e "${RED}❌ 直接支付创建失败${NC}"
    direct_payment_id=""
fi

# 4. 测试查询支付状态
if [ -n "$direct_payment_id" ]; then
    echo -e "\n${GREEN}=== 测试 4: 查询支付状态 ===${NC}"
    test_api "查询支付状态" "GET" "/api/payment/crypto/$direct_payment_id"
fi

# 5. 测试提交交易哈希
if [ -n "$direct_payment_id" ]; then
    echo -e "\n${GREEN}=== 测试 5: 提交交易哈希 ===${NC}"
    test_api "提交交易哈希" "POST" "/api/payment/crypto/$direct_payment_id/submit-tx" \
        '{"transaction_hash":"0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"}'
fi

# 6. 测试查看支付历史
echo -e "\n${GREEN}=== 测试 6: 查看支付历史 ===${NC}"
test_api "查看支付历史" "GET" "/api/payment/crypto/list"

# 7. 测试不同币种
echo -e "\n${GREEN}=== 测试 7: 测试不同加密货币 ===${NC}"

for currency in "BTC" "ETH" "USDC"; do
    echo -e "\n${YELLOW}测试币种: $currency${NC}"
    response=$(curl -s -X POST "$API_URL/api/payment/crypto/direct/create" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d "{
            \"plan\": \"starter\",
            \"email\": \"test@example.com\",
            \"crypto_currency\": \"$currency\"
        }")
    
    if echo "$response" | grep -q '"payment_id"'; then
        echo -e "${GREEN}✅ $currency 支付创建成功${NC}"
    else
        echo -e "${RED}❌ $currency 支付创建失败${NC}"
    fi
done

# 8. 测试不同套餐
echo -e "\n${GREEN}=== 测试 8: 测试不同套餐 ===${NC}"

for plan in "starter" "professional" "enterprise"; do
    echo -e "\n${YELLOW}测试套餐: $plan${NC}"
    response=$(curl -s -X POST "$API_URL/api/payment/crypto/direct/create" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d "{
            \"plan\": \"$plan\",
            \"email\": \"test@example.com\",
            \"crypto_currency\": \"USDT\"
        }")
    
    if echo "$response" | grep -q '"payment_id"'; then
        amount=$(echo "$response" | grep -o '"amount_usd":[0-9.]*' | grep -o '[0-9.]*')
        echo -e "${GREEN}✅ $plan 套餐支付创建成功 (金额: \$$amount)${NC}"
    else
        echo -e "${RED}❌ $plan 套餐支付创建失败${NC}"
    fi
done

# 总结
echo -e "\n${GREEN}=== 测试完成 ===${NC}"
echo "所有测试已完成!"
echo ""
echo "📝 注意事项:"
echo "  1. Coinbase Commerce 需要配置 API Key"
echo "  2. 直接支付需要配置钱包地址"
echo "  3. 生产环境需要启用 Webhook 签名验证"
echo "  4. 建议使用 PostgreSQL 数据库"
echo ""
echo "📚 相关文档:"
echo "  - 用户指南: docs/CRYPTO_PAYMENT_GUIDE.md"
echo "  - 部署指南: docs/CRYPTO_PAYMENT_SETUP.md"
echo "  - 配置示例: .env.crypto.example"

