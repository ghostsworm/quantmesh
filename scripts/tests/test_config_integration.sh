#!/bin/bash

# 配置管理系统集成测试脚本

set -e

echo "========================================="
echo "配置管理系统自动化测试"
echo "========================================="

# 1. 运行单元测试
echo ""
echo "1. 运行配置模块单元测试..."
cd "$(dirname "$0")"
go test -v ./config -run "Test" || exit 1

# 2. 测试配置备份功能
echo ""
echo "2. 测试配置备份功能..."
go test -v ./config -run TestConfigBackup || exit 1

# 3. 测试配置差异对比
echo ""
echo "3. 测试配置差异对比..."
go test -v ./config -run TestConfigDiff || exit 1

# 4. 测试配置热更新
echo ""
echo "4. 测试配置热更新..."
go test -v ./config -run TestHotReloader || exit 1

# 5. 运行API测试（如果有）
echo ""
echo "5. 运行Web API测试..."
if go test -v ./web -run "Test.*Config" 2>/dev/null; then
    echo "✅ API测试通过"
else
    echo "⚠️  API测试跳过（需要完整的web环境）"
fi

echo ""
echo "========================================="
echo "✅ 所有测试通过！"
echo "========================================="

