#!/bin/bash

# WebAuthn RPID 快速修復脚本
# 使用方法: ./scripts/fix_webauthn_rpid.sh qt.facev.app

set -e

DOMAIN="$1"
CONFIG_FILE="config.yaml"

if [ -z "$DOMAIN" ]; then
    echo "❌ 使用方法: $0 <domain>"
    echo "   示例: $0 qt.facev.app"
    exit 1
fi

echo "🔧 WebAuthn RPID 修復脚本"
echo "=========================="
echo "目標域名: $DOMAIN"
echo "配置文件: $CONFIG_FILE"
echo

# 检查配置文件是否存在
if [ ! -f "$CONFIG_FILE" ]; then
    echo "❌ 配置文件 $CONFIG_FILE 不存在"
    echo "請先複製 docs/config/examples/config.example.yaml 到 config.yaml"
    exit 1
fi

echo "📝 备份原配置文件..."
cp "$CONFIG_FILE" "${CONFIG_FILE}.backup.$(date +%Y%m%d_%H%M%S)"
echo "✅ 备份完成: ${CONFIG_FILE}.backup.$(date +%Y%m%d_%H%M%S)"

echo
echo "🛠️  正在修復配置..."

# 使用 sed 添加或修改 domain 配置
if grep -q "domain:" "$CONFIG_FILE"; then
    # 如果已存在 domain 配置，則替換
    sed -i.tmp "s/domain:.*/domain: \"$DOMAIN\"/" "$CONFIG_FILE"
    echo "✅ 已更新 domain 配置"
else
    # 如果不存在，在 web 部分添加
    if grep -q "web:" "$CONFIG_FILE"; then
        # 在 web: 行後添加 domain
        sed -i.tmp "/web:/a\\
  domain: \"$DOMAIN\"" "$CONFIG_FILE"
        echo "✅ 已添加 domain 配置"
    else
        echo "❌ 未找到 web 配置部分"
        exit 1
    fi
fi

# 清理临時文件
rm -f "${CONFIG_FILE}.tmp"

echo
echo "🔍 验证配置..."
if grep "domain: \"$DOMAIN\"" "$CONFIG_FILE" > /dev/null; then
    echo "✅ 配置验证成功"
else
    echo "❌ 配置验证失败"
    exit 1
fi

echo
echo "📋 當前配置:"
echo "============"
grep -A 10 "web:" "$CONFIG_FILE" | head -15

echo
echo "🎯 下一步操作:"
echo "1. 重啟應用: ./scripts/local/stop.sh && ./scripts/local/start.sh"
echo "2. 检查日誌: tail -f logs/app.log"
echo "3. 寻找日誌中的: 'WebAuthn 管理器已初始化'"
echo "4. 应該顯示: rpID=$DOMAIN"
echo
echo "🔧 或者使用環境變數方式:"
echo "export DOMAIN=$DOMAIN"
echo "./scripts/local/start.sh"
echo
echo "✅ 修復完成！"