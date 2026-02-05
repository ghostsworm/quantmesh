#!/bin/bash
#
# 使用正确的 API Key 发送测试事件
#

set -e

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

# PostHog 配置（正确的 API Key）
POSTHOG_PROJECT_ID="phc_kz2U334i5MD8ozz78zvCdN6aRkkx3kYyoU1RSigJOiA"
POSTHOG_ENDPOINT="https://us.i.posthog.com/capture/"

echo ""
echo "=============================================="
echo "  使用正确的 API Key 发送测试事件"
echo "=============================================="
echo ""
echo "API Key: ${POSTHOG_PROJECT_ID:0:30}..."
echo "Endpoint: $POSTHOG_ENDPOINT"
echo ""

# 获取系统信息
ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) ARCH="unknown" ;;
esac

OS_NAME=$(uname -s | tr '[:upper:]' '[:lower:]')
TIMESTAMP=$(date -Iseconds 2>/dev/null || date +"%Y-%m-%dT%H:%M:%S%z")
VERSION="test-$(date +%Y%m%d-%H%M%S)"
DISTINCT_ID="${OS_NAME}-${ARCH}-${VERSION}"

echo -e "${BLUE}[1]${NC} 发送安装统计（install 事件）..."
INSTALL_RESPONSE=$(curl -s -X POST "${POSTHOG_ENDPOINT}" \
    -H "Content-Type: application/json" \
    -H "User-Agent: QuantMesh-TestScript/${VERSION}" \
    -d "{
        \"api_key\": \"${POSTHOG_PROJECT_ID}\",
        \"event\": \"install\",
        \"distinct_id\": \"${DISTINCT_ID}\",
        \"properties\": {
            \"timestamp\": \"${TIMESTAMP}\",
            \"version\": \"${VERSION}\",
            \"os\": \"${OS_NAME}\",
            \"arch\": \"${ARCH}\",
            \"test\": true,
            \"api_key_verified\": true
        }
    }")

echo "响应: $INSTALL_RESPONSE"
if [[ "$INSTALL_RESPONSE" == *"Ok"* ]]; then
    echo -e "${GREEN}[SUCCESS]${NC} 安装统计发送成功！"
else
    echo -e "${RED}[ERROR]${NC} 安装统计发送失败"
fi

echo ""
sleep 1

echo -e "${BLUE}[2]${NC} 发送启动统计（startup 事件）..."
TIMESTAMP=$(date -Iseconds 2>/dev/null || date +"%Y-%m-%dT%H:%M:%S%z")

STARTUP_RESPONSE=$(curl -s -X POST "${POSTHOG_ENDPOINT}" \
    -H "Content-Type: application/json" \
    -H "User-Agent: QuantMesh-TestScript/${VERSION}" \
    -d "{
        \"api_key\": \"${POSTHOG_PROJECT_ID}\",
        \"event\": \"startup\",
        \"distinct_id\": \"${DISTINCT_ID}\",
        \"properties\": {
            \"timestamp\": \"${TIMESTAMP}\",
            \"version\": \"${VERSION}\",
            \"os\": \"${OS_NAME}\",
            \"arch\": \"${ARCH}\",
            \"test\": true,
            \"api_key_verified\": true
        }
    }")

echo "响应: $STARTUP_RESPONSE"
if [[ "$STARTUP_RESPONSE" == *"Ok"* ]]; then
    echo -e "${GREEN}[SUCCESS]${NC} 启动统计发送成功！"
else
    echo -e "${RED}[ERROR]${NC} 启动统计发送失败"
fi

echo ""
echo "=============================================="
echo -e "${GREEN}测试完成！${NC}"
echo "=============================================="
echo ""
echo "📊 查看统计结果："
echo "  1. 访问: https://us.posthog.com/events"
echo "  2. 等待 10-30 秒让 PostHog 处理数据"
echo "  3. 搜索事件: install 或 startup"
echo "  4. 查找包含 'api_key_verified: true' 的事件"
echo ""
echo "💡 提示："
echo "  - 使用正确的 API Key: ${POSTHOG_PROJECT_ID:0:30}..."
echo "  - 事件可能需要几秒钟才会出现在 PostHog 中"
echo "  - 如果看不到，尝试刷新页面或等待更长时间"
echo ""
