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

# 获取版本号
VERSION="3.3.2"

if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
    # 尝试从 git tag 获取版本号（去掉 v 前缀）
    GIT_TAG=$(git describe --tags --exact-match 2>/dev/null || echo "")
    if [ -n "$GIT_TAG" ]; then
        VERSION=$(echo "$GIT_TAG" | sed 's/^v//')
    else
        # 如果没有 tag，使用 git describe
        GIT_DESCRIBE=$(git describe --tags --always --dirty 2>/dev/null || echo "")
        if [ -n "$GIT_DESCRIBE" ]; then
            VERSION=$(echo "$GIT_DESCRIBE" | sed 's/^v//')
        fi
    fi
fi

echo "📌 版本号: ${VERSION}"

go build -ldflags="-s -w -X main.Version=${VERSION}" -o quantmesh .

echo "✅ 构建完成！"
echo "📦 可执行文件: ./quantmesh"

