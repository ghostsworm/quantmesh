#!/bin/bash
#
# 详细测试统计功能脚本（带调试信息）
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
echo "  QuantMesh 统计功能详细测试"
echo "=============================================="
echo ""

# 检查 curl
if ! command -v curl &> /dev/null; then
    echo -e "${RED}[ERROR]${NC} curl 未安装"
    exit 1
fi

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

echo -e "${BLUE}[INFO]${NC} 配置信息："
echo "  Endpoint: $POSTHOG_ENDPOINT"
echo "  Project ID: ${POSTHOG_PROJECT_ID:0:20}..."
echo "  Distinct ID: $DISTINCT_ID"
echo ""

# 构建 JSON 数据
INSTALL_JSON=$(cat <<EOF
{
    "api_key": "${POSTHOG_PROJECT_ID}",
    "event": "install",
    "distinct_id": "${DISTINCT_ID}",
    "properties": {
        "timestamp": "${TIMESTAMP}",
        "version": "${VERSION}",
        "os": "${OS_NAME}",
        "arch": "${ARCH}",
        "test": true,
        "test_run": "$(date +%s)"
    }
}
EOF
)

echo -e "${BLUE}[DEBUG]${NC} 发送的 JSON 数据："
echo "$INSTALL_JSON" | python3 -m json.tool 2>/dev/null || echo "$INSTALL_JSON"
echo ""

# 发送安装统计
echo -e "${BLUE}[TEST]${NC} 发送安装统计..."
echo "  URL: $POSTHOG_ENDPOINT"
echo "  Method: POST"
echo ""

FULL_RESPONSE=$(curl -v -X POST "${POSTHOG_ENDPOINT}" \
    -H "Content-Type: application/json" \
    -H "User-Agent: QuantMesh-TestScript/${VERSION}" \
    -d "$INSTALL_JSON" 2>&1)

HTTP_CODE=$(echo "$FULL_RESPONSE" | grep -oP '< HTTP/\d+\.\d+ \K\d+' | tail -1)
RESPONSE_BODY=$(echo "$FULL_RESPONSE" | grep -A 100 "^{" | head -5)

echo -e "${BLUE}[DEBUG]${NC} 完整响应："
echo "$FULL_RESPONSE" | tail -20
echo ""

if [ "$HTTP_CODE" = "200" ]; then
    echo -e "${GREEN}[SUCCESS]${NC} HTTP 状态码: $HTTP_CODE"
    echo -e "${GREEN}[SUCCESS]${NC} 响应体: $RESPONSE_BODY"
else
    echo -e "${RED}[ERROR]${NC} HTTP 状态码: $HTTP_CODE"
    echo -e "${RED}[ERROR]${NC} 响应体: $RESPONSE_BODY"
fi

echo ""
echo "等待 2 秒..."
sleep 2

# 发送启动统计
TIMESTAMP=$(date -Iseconds 2>/dev/null || date +"%Y-%m-%dT%H:%M:%S%z")

STARTUP_JSON=$(cat <<EOF
{
    "api_key": "${POSTHOG_PROJECT_ID}",
    "event": "startup",
    "distinct_id": "${DISTINCT_ID}",
    "properties": {
        "timestamp": "${TIMESTAMP}",
        "version": "${VERSION}",
        "os": "${OS_NAME}",
        "arch": "${ARCH}",
        "test": true,
        "test_run": "$(date +%s)"
    }
}
EOF
)

echo -e "${BLUE}[TEST]${NC} 发送启动统计..."
FULL_RESPONSE=$(curl -v -X POST "${POSTHOG_ENDPOINT}" \
    -H "Content-Type: application/json" \
    -H "User-Agent: QuantMesh-TestScript/${VERSION}" \
    -d "$STARTUP_JSON" 2>&1)

HTTP_CODE=$(echo "$FULL_RESPONSE" | grep -oP '< HTTP/\d+\.\d+ \K\d+' | tail -1)
RESPONSE_BODY=$(echo "$FULL_RESPONSE" | grep -A 100 "^{" | head -5)

echo -e "${BLUE}[DEBUG]${NC} 完整响应："
echo "$FULL_RESPONSE" | tail -20
echo ""

if [ "$HTTP_CODE" = "200" ]; then
    echo -e "${GREEN}[SUCCESS]${NC} HTTP 状态码: $HTTP_CODE"
    echo -e "${GREEN}[SUCCESS]${NC} 响应体: $RESPONSE_BODY"
else
    echo -e "${RED}[ERROR]${NC} HTTP 状态码: $HTTP_CODE"
    echo -e "${RED}[ERROR]${NC} 响应体: $RESPONSE_BODY"
fi

echo ""
echo "=============================================="
echo -e "${GREEN}测试完成！${NC}"
echo "=============================================="
echo ""
echo "📊 查看统计结果："
echo "  1. 登录 PostHog: https://us.posthog.com"
echo "  2. 进入 'Events' 页面"
echo "  3. 点击右上角的 'Live events' 或刷新页面"
echo "  4. 筛选事件类型：install 或 startup"
echo "  5. 查找包含 'test: true' 的事件"
echo ""
echo "💡 如果还是看不到："
echo "  - 检查 PostHog 项目设置中的 API Key 是否正确"
echo "  - 确认你在正确的项目（Default project）中查看"
echo "  - 尝试在 PostHog 中搜索事件：test_run"
echo ""
