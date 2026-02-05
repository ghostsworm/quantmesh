#!/bin/bash
#
# 测试统计功能脚本
# 模拟安装和启动统计发送，验证数据是否正确收集到 PostHog
#

set -e

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# PostHog 配置（从代码中读取）
POSTHOG_PROJECT_ID="phc_kz2U334i5MD8ozz78zvCdN6aRkkx3kYyoU1RSigJOiA"
POSTHOG_ENDPOINT="https://us.i.posthog.com/capture/"

echo ""
echo "=============================================="
echo "  QuantMesh 统计功能测试"
echo "=============================================="
echo ""

# 检查 curl 是否可用
if ! command -v curl &> /dev/null; then
    echo -e "${YELLOW}[WARN] curl 未安装，无法测试统计功能${NC}"
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

echo -e "${BLUE}[INFO]${NC} 系统信息："
echo "  操作系统: $OS_NAME"
echo "  架构: $ARCH"
echo "  时间戳: $TIMESTAMP"
echo "  版本: $VERSION"
echo ""

# 生成 distinct_id（基于系统信息）
DISTINCT_ID="${OS_NAME}-${ARCH}-${VERSION}"

# 发送安装统计
echo -e "${BLUE}[TEST]${NC} 发送安装统计（install 事件）..."
INSTALL_RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" -X POST "${POSTHOG_ENDPOINT}" \
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
            \"test\": true
        }
    }")

INSTALL_HTTP_CODE=$(echo "$INSTALL_RESPONSE" | grep "HTTP_CODE" | cut -d: -f2)
INSTALL_BODY=$(echo "$INSTALL_RESPONSE" | sed '/HTTP_CODE/d')

if [ "$INSTALL_HTTP_CODE" = "200" ]; then
    echo -e "${GREEN}[SUCCESS]${NC} 安装统计发送成功！"
    echo "  响应: $INSTALL_BODY"
else
    echo -e "${YELLOW}[WARN]${NC} 安装统计发送失败（HTTP $INSTALL_HTTP_CODE）"
    echo "  响应: $INSTALL_BODY"
fi

echo ""

# 等待一秒
sleep 1

# 发送启动统计
echo -e "${BLUE}[TEST]${NC} 发送启动统计（startup 事件）..."
TIMESTAMP=$(date -Iseconds 2>/dev/null || date +"%Y-%m-%dT%H:%M:%S%z")

STARTUP_RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" -X POST "${POSTHOG_ENDPOINT}" \
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
            \"test\": true
        }
    }")

STARTUP_HTTP_CODE=$(echo "$STARTUP_RESPONSE" | grep "HTTP_CODE" | cut -d: -f2)
STARTUP_BODY=$(echo "$STARTUP_RESPONSE" | sed '/HTTP_CODE/d')

if [ "$STARTUP_HTTP_CODE" = "200" ]; then
    echo -e "${GREEN}[SUCCESS]${NC} 启动统计发送成功！"
    echo "  响应: $STARTUP_BODY"
else
    echo -e "${YELLOW}[WARN]${NC} 启动统计发送失败（HTTP $STARTUP_HTTP_CODE）"
    echo "  响应: $STARTUP_BODY"
fi

echo ""
echo "=============================================="
echo -e "${GREEN}测试完成！${NC}"
echo "=============================================="
echo ""
echo "📊 查看统计结果："
echo "  1. 登录 PostHog: https://us.posthog.com"
echo "  2. 进入 'Events' 页面（左侧菜单）"
echo "  3. 筛选事件："
echo "     - install（安装事件）"
echo "     - startup（启动事件）"
echo "  4. 查找包含 'test: true' 的事件（测试事件）"
echo ""
echo "💡 提示："
echo "  - 事件可能需要几秒钟才会出现在 PostHog 中"
echo "  - 如果看不到事件，检查网络连接和 API Key 是否正确"
echo ""
