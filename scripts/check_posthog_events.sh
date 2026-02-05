#!/bin/bash
#
# 使用 PostHog API 查询事件，验证数据是否收到
#

set -e

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

# PostHog 配置
POSTHOG_PROJECT_ID="phc_kz2U334i5MD8ozz78zvCdN6aRkkx3kYyoU1RSigJOiA"
POSTHOG_API_BASE="https://us.i.posthog.com"

echo ""
echo "=============================================="
echo "  查询 PostHog 事件"
echo "=============================================="
echo ""

# 先发送一个新事件
echo -e "${BLUE}[1]${NC} 发送测试事件..."
TIMESTAMP=$(date +%s)
UNIQUE_ID="check-${TIMESTAMP}"

RESPONSE=$(curl -s -X POST "${POSTHOG_API_BASE}/capture/" \
    -H "Content-Type: application/json" \
    -d "{
        \"api_key\": \"${POSTHOG_PROJECT_ID}\",
        \"event\": \"check_event\",
        \"distinct_id\": \"check-${TIMESTAMP}\",
        \"properties\": {
            \"check_id\": \"${UNIQUE_ID}\",
            \"timestamp\": \"$(date -Iseconds)\"
        }
    }")

echo "响应: $RESPONSE"
echo ""

if [[ "$RESPONSE" == *"Ok"* ]]; then
    echo -e "${GREEN}[SUCCESS]${NC} 事件发送成功！"
else
    echo -e "${RED}[ERROR]${NC} 事件发送失败"
    exit 1
fi

echo ""
echo "等待 10 秒让 PostHog 处理..."
sleep 10

echo ""
echo "=============================================="
echo "  查看 PostHog 网页"
echo "=============================================="
echo ""
echo "📊 请在浏览器中打开以下链接查看事件："
echo ""
echo "1. Events 页面（推荐）："
echo "   https://us.posthog.com/events"
echo ""
echo "2. 直接搜索事件："
echo "   在搜索框输入: check_event"
echo ""
echo "3. Live Events（实时）："
echo "   https://us.posthog.com/events?live=true"
echo ""
echo "4. Activity 页面："
echo "   https://us.posthog.com/activity"
echo ""
echo "=============================================="
echo "  验证信息"
echo "=============================================="
echo ""
echo "查找以下信息："
echo "  - 事件名称: check_event"
echo "  - check_id: ${UNIQUE_ID}"
echo "  - distinct_id: check-${TIMESTAMP}"
echo ""
echo "💡 如果还是看不到，请检查："
echo "  1. 确认你在正确的项目（Default project）"
echo "  2. 检查项目设置中的 API Key 是否正确"
echo "  3. 尝试刷新页面（Cmd+R 或 Ctrl+R）"
echo "  4. 清除浏览器缓存后重试"
echo "  5. 等待更长时间（PostHog 免费版可能有延迟）"
echo ""
