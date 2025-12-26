#!/bin/bash

# 构建脚本：前端 + 后端 + 单文件打包

set -e

echo "🚀 开始构建 QuantMesh Market Maker..."

# 1. 构建前端
if [ -d "webui" ]; then
    echo "📦 构建前端..."
    cd webui
    if [ ! -d "node_modules" ]; then
        npm install
    fi
    npm run build
    cd ..
else
    echo "⚠️  前端目录不存在，跳过前端构建"
fi

# 2. 构建 Go 程序（会自动嵌入 dist/ 目录）
echo "🔨 构建后端..."
go build -o quantmesh .

echo "✅ 构建完成！"
echo "📦 可执行文件: ./quantmesh"

