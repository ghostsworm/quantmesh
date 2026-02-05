#!/bin/bash
#
# 验证统计事件是否已收到
# 直接查询 PostHog API 确认事件
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
POSTHOG_ENDPOINT="https://us.i.posthog.com/capture/"

echo ""
echo "=============================================="
echo "  1. 发送测试事件"
echo "=============================================="
echo ""

# 发送一个带唯一标识的事件
TIMESTAMP=$(date +%s)
UNIQUE_ID="verify-${TIMESTAMP}"
DISTINCT_ID="test-${TIMESTAMP}"

echo -e "${BLUE}[SEND]${NC} 发送验证事件..."
RESPONSE=$(curl -s -X POST "${POSTHOG_ENDPOINT}" \
    -H "Content-Type: application/json" \
    -d "{
        \"api_key\": \"${POSTHOG_PROJECT_ID}\",
        \"event\": \"telemetry_test\",
        \"distinct_id\": \"${DISTINCT_ID}\",
        \"properties\": {
            \"test_id\": \"${UNIQUE_ID}\",
            \"timestamp\": \"$(date -Iseconds)\",
            \"message\": \"This is a verification test event\"
        }
    }")

echo "响应: $RESPONSE"
echo ""

if [[ "$RESPONSE" == *"Ok"* ]]; then
    echo -e "${GREEN}[SUCCESS]${NC} 事件发送成功！"
    echo ""
    echo "等待 5 秒让 PostHog 处理数据..."
    sleep 5
    echo ""
else
    echo -e "${RED}[ERROR]${NC} 事件发送失败"
    exit 1
fi

echo "=============================================="
echo "  2. 查看 PostHog 中的事件"
echo "=============================================="
echo ""
echo "📊 请按照以下步骤在 PostHog 中查看："
echo ""
echo "方法一：Events 页面（推荐）"
echo "  1. 访问: https://us.posthog.com/events"
echo "  2. 在搜索框输入: telemetry_test"
echo "  3. 或者筛选事件类型: telemetry_test"
echo "  4. 查找 test_id: ${UNIQUE_ID}"
echo ""
echo "方法二：Live Events"
echo "  1. 访问: https://us.posthog.com/events"
echo "  2. 点击右上角的 'Live events' 按钮"
echo "  3. 应该能看到实时事件流"
echo ""
echo "方法三：Activity 页面"
echo "  1. 访问: https://us.posthog.com/activity"
echo "  2. 查看最近的活动"
echo ""
echo "=============================================="
echo "  3. 验证信息"
echo "=============================================="
echo ""
echo "查找以下信息："
echo "  - 事件名称: telemetry_test"
echo "  - test_id: ${UNIQUE_ID}"
echo "  - distinct_id: ${DISTINCT_ID}"
echo ""
echo "如果还是看不到，可能的原因："
echo "  1. PostHog 需要更多时间处理（等待 10-30 秒）"
echo "  2. 项目设置中可能需要启用某些功能"
echo "  3. 检查是否在正确的项目（Default project）中"
echo "  4. 尝试刷新页面或清除浏览器缓存"
echo ""
echo "💡 提示："
echo "  - PostHog 免费版可能有延迟"
echo "  - 确保你在正确的项目（Project ID: ${POSTHOG_PROJECT_ID:0:20}...）"
echo "  - 可以尝试在 PostHog 中搜索: ${UNIQUE_ID}"
echo ""
