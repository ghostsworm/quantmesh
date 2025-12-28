#!/bin/bash

# QuantMesh 许可证生成工具

set -e

# 检查参数
if [ $# -lt 2 ]; then
    echo "用法: $0 <plugin_name> <customer_id> [days] [instances] [features]"
    echo ""
    echo "参数:"
    echo "  plugin_name  - 插件名称"
    echo "  customer_id  - 客户ID"
    echo "  days         - 有效天数 (默认: 365)"
    echo "  instances    - 最大实例数 (默认: 1)"
    echo "  features     - 授权功能 (默认: *)"
    echo ""
    echo "示例:"
    echo "  $0 premium_ai_strategy CUST001"
    echo "  $0 premium_ai_strategy CUST001 365 5 'ai,optimization'"
    exit 1
fi

PLUGIN_NAME=$1
CUSTOMER_ID=$2
DAYS=${3:-365}
INSTANCES=${4:-1}
FEATURES=${5:-"*"}

echo "🔐 生成许可证..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "插件名称: ${PLUGIN_NAME}"
echo "客户ID:   ${CUSTOMER_ID}"
echo "有效天数: ${DAYS}"
echo "最大实例: ${INSTANCES}"
echo "授权功能: ${FEATURES}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# 编译许可证生成器
cd "$(dirname "$0")/.."
go build -o /tmp/license_generator plugin/tools/license_generator.go

# 生成许可证
/tmp/license_generator \
    -plugin="${PLUGIN_NAME}" \
    -customer="${CUSTOMER_ID}" \
    -days="${DAYS}" \
    -instances="${INSTANCES}" \
    -features="${FEATURES}"

# 清理
rm /tmp/license_generator

echo ""
echo "💡 提示:"
echo "1. 将许可证密钥发送给客户"
echo "2. 客户将密钥添加到 config.yaml 的 plugins 配置中"
echo "3. 或通过环境变量设置: QUANTMESH_LICENSE_<PLUGIN_NAME>=<key>"

