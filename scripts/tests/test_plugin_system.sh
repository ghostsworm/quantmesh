#!/bin/bash

# 插件系统测试脚本

set -e

echo "🧪 开始测试插件系统..."

# 1. 测试插件构建
echo ""
echo "📦 步骤 1: 构建插件..."

cd ../quantmesh-premium/plugins/ai_strategy
echo "  构建 AI 策略插件..."
go build -buildmode=plugin -o ai_strategy.so

cd ../multi_strategy
echo "  构建多策略插件..."
go build -buildmode=plugin -o multi_strategy.so

cd ../advanced_risk
echo "  构建高级风控插件..."
go build -buildmode=plugin -o advanced_risk.so

echo "✅ 插件构建完成"

# 2. 复制插件到主项目
echo ""
echo "📋 步骤 2: 复制插件到主项目..."

cd ../../../../opensqt_market_maker
mkdir -p plugins

cp ../quantmesh-premium/plugins/ai_strategy/ai_strategy.so plugins/
cp ../quantmesh-premium/plugins/multi_strategy/multi_strategy.so plugins/
cp ../quantmesh-premium/plugins/advanced_risk/advanced_risk.so plugins/

echo "✅ 插件复制完成"

# 3. 测试 License 生成
echo ""
echo "🔑 步骤 3: 测试 License 生成..."

cd ../quantmesh-premium
go run plugin/tools/license_generator/main.go \
  --plugin "ai_strategy" \
  --customer "test_customer" \
  --email "test@example.com" \
  --plan "professional" \
  --expiry "2025-12-31" \
  --output "test_license.txt"

echo "✅ License 生成完成"

# 4. 测试插件加载
echo ""
echo "🔌 步骤 4: 测试插件加载..."

cd ../opensqt_market_maker

# 创建测试配置
cat > test_plugin_config.yaml <<EOF
plugins:
  enabled: true
  directory: "./plugins"
  
  licenses:
    ai_strategy: "$(cat ../quantmesh-premium/test_license.txt)"
    multi_strategy: ""
    advanced_risk: ""
  
  config:
    ai_strategy:
      gemini_api_key: "test_key"
      openai_api_key: ""
EOF

echo "✅ 测试配置创建完成"

echo ""
echo "✅ 所有测试步骤完成!"
echo ""
echo "📝 下一步:"
echo "  1. 启动主程序: ./quantmesh"
echo "  2. 检查日志确认插件加载成功"
echo "  3. 通过 API 测试插件功能"

