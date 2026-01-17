#!/bin/bash

# QuantMesh 构建脚本：前端 + 后端 + 单文件打包
#
# 构建流程：
#   1. 构建前端（webui/dist）
#   2. 复制前端到 web/dist（供 go:embed 使用）
#   3. 构建 Go 后端（嵌入前端静态文件）
#
# 最终产物：单个可执行文件 quantmesh

set -e

# 颜色输出
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  构建 QuantMesh Market Maker${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# 1. 构建前端
if [ -d "${SCRIPT_DIR}/webui" ]; then
    log_info "📦 构建前端..."
    cd "${SCRIPT_DIR}/webui"
    
    # 优先使用 pnpm，其次 npm
    if command -v pnpm >/dev/null 2>&1; then
        if [ ! -d "node_modules" ]; then
            log_info "安装前端依赖 (pnpm)..."
            pnpm install
        fi
        pnpm run build
    elif command -v npm >/dev/null 2>&1; then
        if [ ! -d "node_modules" ]; then
            log_info "安装前端依赖 (npm)..."
            npm install
        fi
        npm run build
    else
        log_warn "未找到 pnpm 或 npm，跳过前端构建"
    fi
    
    cd "${SCRIPT_DIR}"
    
    # 复制 webui/dist 到 web/dist 供 go:embed 使用
    if [ -d "${SCRIPT_DIR}/webui/dist" ]; then
        log_info "📋 复制前端文件到 web/dist..."
        rm -rf "${SCRIPT_DIR}/web/dist"
        mkdir -p "${SCRIPT_DIR}/web/dist"
        cp -r "${SCRIPT_DIR}/webui/dist"/* "${SCRIPT_DIR}/web/dist/"
        log_info "✅ 前端构建完成"
    fi
else
    log_warn "⚠️  前端目录不存在，跳过前端构建"
fi

# 2. 构建 Go 程序（会自动嵌入 dist/ 目录）
log_info "🔨 构建后端..."

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

log_info "📌 版本号: ${VERSION}"

cd "${SCRIPT_DIR}"
go build -ldflags="-s -w -X main.Version=${VERSION}" -o quantmesh .

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  ✅ 构建完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}可执行文件:${NC} ./quantmesh"
echo ""
echo -e "${YELLOW}使用方法:${NC}"
echo "  ./quantmesh                    # 使用默认配置 config.yaml"
echo "  ./quantmesh config.yaml        # 使用指定配置文件"
echo ""
echo -e "${YELLOW}端口配置:${NC}"
echo "  Go 后端: 28888（可在 config.yaml 中修改）"
echo ""

