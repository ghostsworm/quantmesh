#!/bin/bash
# QuantMesh 多平台构建脚本
# 用法: ./scripts/build-release.sh [版本号]

set -e

VERSION=${1:-"dev"}
PROJECT_NAME="quantmesh"
PLUGIN_DIR="../quantmesh-premium/plugins"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}🚀 开始构建 QuantMesh v${VERSION}${NC}"

# 检查 Go 版本
GO_VERSION=$(go version | awk '{print $3}')
echo -e "${YELLOW}📌 当前 Go 版本: ${GO_VERSION}${NC}"
echo -e "${YELLOW}⚠️  所有插件将使用此版本编译${NC}"
echo ""

# 支持的平台
PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
)

# 创建发布目录
RELEASE_DIR="release/v${VERSION}"
rm -rf "${RELEASE_DIR}"
mkdir -p "${RELEASE_DIR}"

# 获取当前目录（脚本所在目录）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo -e "${GREEN}📁 项目根目录: ${PROJECT_ROOT}${NC}"
echo ""

# 构建函数
build_platform() {
    local GOOS=$1
    local GOARCH=$2
    local PLATFORM="${GOOS}-${GOARCH}"
    local OUTPUT_DIR="${RELEASE_DIR}/${PLATFORM}"
    
    echo -e "${GREEN}🔨 构建平台: ${PLATFORM}${NC}"
    mkdir -p "${OUTPUT_DIR}/plugins"
    
    # 构建主程序
    echo "  → 编译主程序..."
    cd "${PROJECT_ROOT}"
    GOOS=${GOOS} GOARCH=${GOARCH} go build \
        -ldflags "-X main.Version=${VERSION}" \
        -o "${OUTPUT_DIR}/${PROJECT_NAME}" \
        ./cmd/main.go || ./main.go 2>/dev/null || {
        echo -e "${RED}  ❌ 主程序编译失败（尝试其他入口）${NC}"
        # 如果没有 cmd/main.go，尝试根目录的 main.go
        if [ -f "main.go" ]; then
            GOOS=${GOOS} GOARCH=${GOARCH} go build \
                -ldflags "-X main.Version=${VERSION}" \
                -o "${OUTPUT_DIR}/${PROJECT_NAME}" \
                .
        else
            echo -e "${RED}  ❌ 找不到主程序入口文件${NC}"
            return 1
        fi
    }
    
    # 编译插件（如果插件目录存在）
    if [ -d "${PLUGIN_DIR}" ]; then
        echo "  → 编译插件..."
        
        # 查找所有插件目录
        find "${PLUGIN_DIR}" -mindepth 1 -maxdepth 1 -type d | while read plugin_dir; do
            plugin_name=$(basename "${plugin_dir}")
            plugin_go="${plugin_dir}/plugin.go"
            
            if [ -f "${plugin_go}" ]; then
                echo "    → 编译插件: ${plugin_name}"
                
                # 检查是否有 build.sh
                if [ -f "${plugin_dir}/build.sh" ]; then
                    cd "${plugin_dir}"
                    GOOS=${GOOS} GOARCH=${GOARCH} bash build.sh "${OUTPUT_DIR}/plugins"
                else
                    # 使用标准 Go 构建
                    cd "${plugin_dir}"
                    GOOS=${GOOS} GOARCH=${GOARCH} go build \
                        -buildmode=plugin \
                        -o "${OUTPUT_DIR}/plugins/${plugin_name}.so" \
                        . 2>&1 | grep -v "no Go files" || {
                        echo -e "${YELLOW}    ⚠️  插件 ${plugin_name} 编译跳过（无 Go 文件）${NC}"
                    }
                fi
            fi
        done
    else
        echo -e "${YELLOW}  ⚠️  插件目录不存在: ${PLUGIN_DIR}${NC}"
    fi
    
    # 复制配置文件
    if [ -f "${PROJECT_ROOT}/config.example.yaml" ]; then
        cp "${PROJECT_ROOT}/config.example.yaml" "${OUTPUT_DIR}/config.yaml.example"
        echo "  → 复制配置文件"
    fi
    
    # 创建版本信息文件
    cat > "${OUTPUT_DIR}/VERSION" << EOF
Version: ${VERSION}
Go Version: ${GO_VERSION}
Platform: ${PLATFORM}
Build Date: $(date -u +"%Y-%m-%d %H:%M:%S UTC")
EOF
    
    echo -e "${GREEN}  ✅ ${PLATFORM} 构建完成${NC}"
    echo ""
}

# 构建所有平台
for platform in "${PLATFORMS[@]}"; do
    GOOS=${platform%/*}
    GOARCH=${platform#*/}
    build_platform "${GOOS}" "${GOARCH}"
done

# 创建压缩包
echo -e "${GREEN}📦 创建压缩包...${NC}"
cd "${RELEASE_DIR}"
for platform_dir in */; do
    platform_name=${platform_dir%/}
    echo "  → 打包 ${platform_name}..."
    tar -czf "${PROJECT_NAME}-v${VERSION}-${platform_name}.tar.gz" "${platform_name}/"
    zip -q -r "${PROJECT_NAME}-v${VERSION}-${platform_name}.zip" "${platform_name}/"
done

# 创建校验和
echo -e "${GREEN}🔐 生成校验和...${NC}"
sha256sum *.tar.gz *.zip > "checksums.txt" 2>/dev/null || shasum -a 256 *.tar.gz *.zip > "checksums.txt"

# 总结
echo ""
echo -e "${GREEN}✅ 构建完成！${NC}"
echo -e "${GREEN}📁 发布文件位于: ${RELEASE_DIR}${NC}"
echo ""
echo -e "${YELLOW}📋 构建的文件:${NC}"
ls -lh "${RELEASE_DIR}"/*.tar.gz "${RELEASE_DIR}"/*.zip 2>/dev/null | awk '{print "  " $9 " (" $5 ")"}'
echo ""
echo -e "${YELLOW}⚠️  重要提示:${NC}"
echo -e "  1. 所有插件已使用 Go ${GO_VERSION} 编译"
echo -e "  2. 客户必须使用相同 Go 版本编译主程序，或使用此预编译版本"
echo -e "  3. 建议在 README 中明确说明版本要求"

