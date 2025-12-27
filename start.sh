#!/bin/bash
# 使用 HTTP 端口（不是 SOCKS5 端口）
export https_proxy=http://127.0.0.1:7895
export http_proxy=http://127.0.0.1:7895
# QuantMesh Market Maker 启动/重启脚本
# 功能：
# 1. 检查并杀掉旧进程（重启模式）
# 2. 杀掉占用端口的进程
# 3. 自动构建前端和后端（如果需要）
# 4. 启动新服务

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 配置
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_NAME="quantmesh"
BINARY_NAME="quantmesh"
CONFIG_FILE="${1:-config.yaml}"
PID_FILE="${SCRIPT_DIR}/.${APP_NAME}.pid"
LOG_FILE="${SCRIPT_DIR}/logs/${APP_NAME}.log"

# 创建日志目录
mkdir -p "${SCRIPT_DIR}/logs"

# 日志函数
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查配置文件
if [ ! -f "${SCRIPT_DIR}/${CONFIG_FILE}" ]; then
    log_error "配置文件不存在: ${CONFIG_FILE}"
    if [ -f "${SCRIPT_DIR}/config.example.yaml" ]; then
        log_warn "发现示例配置文件: config.example.yaml"
        log_info "是否要复制为 ${CONFIG_FILE}？(y/n)"
        read -r answer
        if [ "$answer" = "y" ] || [ "$answer" = "Y" ]; then
            cp "${SCRIPT_DIR}/config.example.yaml" "${SCRIPT_DIR}/${CONFIG_FILE}"
            log_info "✅ 已复制示例配置文件为 ${CONFIG_FILE}"
            log_warn "⚠️  请先编辑 ${CONFIG_FILE} 配置交易所API密钥等信息，然后重新运行此脚本"
            exit 0
        else
            log_error "请先创建配置文件 ${CONFIG_FILE}"
            log_info "提示: cp config.example.yaml ${CONFIG_FILE}"
            exit 1
        fi
    else
        log_error "请先创建配置文件 ${CONFIG_FILE}"
        exit 1
    fi
fi

# 从配置文件读取端口
get_port_from_config() {
    # 尝试从web配置中读取端口（只提取数字，忽略注释）
    # 使用更精确的正则表达式：匹配 "port: 数字" 格式，提取第一个数字
    local port=$(grep -A 3 "^web:" "${SCRIPT_DIR}/${CONFIG_FILE}" 2>/dev/null | grep -E "^\s+port:" | sed -E 's/^[^:]*:[[:space:]]*([0-9]+).*/\1/' | head -1 || echo "")
    
    # 如果方法1失败，尝试更宽松的匹配
    if [ -z "$port" ] || ! [[ "$port" =~ ^[0-9]+$ ]]; then
        port=$(grep -A 5 "web:" "${SCRIPT_DIR}/${CONFIG_FILE}" 2>/dev/null | grep -E "port:" | sed -E 's/^[^:]*:[[:space:]]*([0-9]+).*/\1/' | head -1 || echo "")
    fi
    
    # 验证端口号是否有效（必须是纯数字，且在合理范围内）
    if [ -z "$port" ] || ! [[ "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1000 ] || [ "$port" -gt 65535 ]; then
        port="28888"  # 默认28888
    fi
    echo "$port"
}

WEB_PORT=$(get_port_from_config)
log_info "检测到Web端口: ${WEB_PORT}"

# 杀掉旧进程
kill_old_process() {
    if [ -f "${PID_FILE}" ]; then
        local old_pid=$(cat "${PID_FILE}" 2>/dev/null || echo "")
        if [ -n "${old_pid}" ] && kill -0 "${old_pid}" 2>/dev/null; then
            log_warn "发现正在运行的进程 (PID: ${old_pid})，正在停止..."
            kill -TERM "${old_pid}" 2>/dev/null || true
            sleep 2
            # 如果还在运行，强制杀掉
            if kill -0 "${old_pid}" 2>/dev/null; then
                log_warn "进程未响应，强制停止..."
                kill -9 "${old_pid}" 2>/dev/null || true
            fi
            log_info "旧进程已停止"
        fi
        rm -f "${PID_FILE}"
    fi

    # 通过进程名查找并杀掉
    local pids=$(pgrep -f "${BINARY_NAME}" 2>/dev/null || echo "")
    if [ -n "${pids}" ]; then
        log_warn "发现通过进程名匹配的进程，正在停止..."
        echo "${pids}" | xargs kill -TERM 2>/dev/null || true
        sleep 2
        echo "${pids}" | xargs kill -9 2>/dev/null || true
        log_info "已停止所有匹配的进程"
    fi
}

# 杀掉占用端口的进程
kill_port_process() {
    local port=$1
    if [ -z "$port" ]; then
        return
    fi

    # macOS使用lsof，Linux使用lsof或fuser
    local pid=""
    if command -v lsof >/dev/null 2>&1; then
        pid=$(lsof -ti:${port} 2>/dev/null || echo "")
    elif command -v fuser >/dev/null 2>&1; then
        pid=$(fuser ${port}/tcp 2>/dev/null | awk '{print $1}' || echo "")
    fi

    if [ -n "${pid}" ]; then
        log_warn "发现占用端口 ${port} 的进程 (PID: ${pid})，正在停止..."
        kill -TERM ${pid} 2>/dev/null || true
        sleep 2
        # 如果还在运行，强制杀掉
        if kill -0 ${pid} 2>/dev/null; then
            log_warn "进程未响应，强制停止..."
            kill -9 ${pid} 2>/dev/null || true
        fi
        log_info "端口 ${port} 已释放"
    else
        log_info "端口 ${port} 未被占用"
    fi
}

# 构建前端（如果需要）
build_frontend() {
    if [ ! -d "${SCRIPT_DIR}/webui" ]; then
        log_warn "前端目录不存在，跳过前端构建"
        return 0
    fi

    # 检查是否需要构建前端
    local need_build=false
    
    # 如果 dist 目录不存在，需要构建
    if [ ! -d "${SCRIPT_DIR}/webui/dist" ]; then
        need_build=true
        log_info "前端 dist 目录不存在，需要构建前端"
    else
        # 检查 package.json 是否比 dist 新（说明有更新）
        if [ "${SCRIPT_DIR}/webui/package.json" -nt "${SCRIPT_DIR}/webui/dist" ] 2>/dev/null; then
            need_build=true
            log_info "检测到前端依赖更新，需要重新构建"
        fi
        
        # 检查前端源码文件是否比 dist 新
        if find "${SCRIPT_DIR}/webui/src" -type f \( -name "*.tsx" -o -name "*.ts" -o -name "*.jsx" -o -name "*.js" -o -name "*.css" \) -newer "${SCRIPT_DIR}/webui/dist" 2>/dev/null | grep -q .; then
            need_build=true
            log_info "检测到前端源码更新，需要重新构建"
        fi
    fi

    if [ "$need_build" = true ]; then
        log_info "构建前端..."
        cd "${SCRIPT_DIR}/webui"
        
        # 检查 node_modules，如果没有则安装
        if [ ! -d "node_modules" ]; then
            log_info "安装前端依赖..."
            if command -v yarn >/dev/null 2>&1; then
                yarn install
            elif command -v npm >/dev/null 2>&1; then
                npm install
            else
                log_error "未找到 yarn 或 npm，无法构建前端"
                return 1
            fi
        fi
        
        # 构建前端
        if command -v yarn >/dev/null 2>&1; then
            yarn build
        else
            npm run build
        fi
        
        if [ $? -ne 0 ]; then
            log_error "前端构建失败"
            return 1
        fi
        
        # 将 webui/dist 复制到 web/dist（供 go:embed 使用）
        if [ -d "${SCRIPT_DIR}/webui/dist" ]; then
            log_info "复制前端文件到 web/dist..."
            rm -rf "${SCRIPT_DIR}/web/dist"
            cp -r "${SCRIPT_DIR}/webui/dist" "${SCRIPT_DIR}/web/dist"
        fi
        
        log_info "✅ 前端构建成功"
        cd "${SCRIPT_DIR}"
    else
        log_info "前端已构建，跳过"
    fi
    
    # 无论是否重新构建，都要确保 web/dist 存在（供 go:embed 使用）
    if [ -d "${SCRIPT_DIR}/webui/dist" ]; then
        # 检查是否需要更新 web/dist
        if [ ! -d "${SCRIPT_DIR}/web/dist" ] || [ "${SCRIPT_DIR}/webui/dist" -nt "${SCRIPT_DIR}/web/dist" ] 2>/dev/null; then
            log_info "同步前端文件到 web/dist..."
            rm -rf "${SCRIPT_DIR}/web/dist"
            cp -r "${SCRIPT_DIR}/webui/dist" "${SCRIPT_DIR}/web/dist"
        fi
    fi
}

# 检查并构建二进制文件
check_and_build() {
    local need_build=false
    
    # 如果二进制文件不存在，需要构建
    if [ ! -f "${SCRIPT_DIR}/${BINARY_NAME}" ]; then
        need_build=true
        log_info "二进制文件不存在，需要构建"
    else
        # 检查 Go 源码或前端是否有更新
        local go_files_newer=false
        local frontend_newer=false
        
        # 检查 Go 文件是否比二进制新
        if find "${SCRIPT_DIR}" -name "*.go" -newer "${SCRIPT_DIR}/${BINARY_NAME}" 2>/dev/null | grep -q .; then
            go_files_newer=true
        fi
        
        # 检查前端 dist 是否比二进制新（检查 webui/dist 和 web/dist）
        if [ -d "${SCRIPT_DIR}/webui/dist" ] && [ "${SCRIPT_DIR}/webui/dist" -nt "${SCRIPT_DIR}/${BINARY_NAME}" ] 2>/dev/null; then
            frontend_newer=true
        fi
        if [ -d "${SCRIPT_DIR}/web/dist" ] && [ "${SCRIPT_DIR}/web/dist" -nt "${SCRIPT_DIR}/${BINARY_NAME}" ] 2>/dev/null; then
            frontend_newer=true
        fi
        
        if [ "$go_files_newer" = true ] || [ "$frontend_newer" = true ]; then
            need_build=true
            if [ "$go_files_newer" = true ]; then
                log_info "检测到 Go 源码更新，需要重新构建"
            fi
            if [ "$frontend_newer" = true ]; then
                log_info "检测到前端更新，需要重新构建"
            fi
        fi
    fi
    
    if [ "$need_build" = true ]; then
        # 先构建前端
        build_frontend
        if [ $? -ne 0 ]; then
            log_error "前端构建失败，无法继续"
            exit 1
        fi
        
        # 再构建后端
        log_info "构建后端..."
        cd "${SCRIPT_DIR}"
        go build -o "${BINARY_NAME}" .
        if [ $? -ne 0 ]; then
            log_error "后端构建失败"
            exit 1
        fi
        log_info "✅ 后端构建成功"
    else
        log_info "二进制文件已是最新，跳过构建"
    fi
}

# 主流程
log_info "=========================================="
log_info "启动 QuantMesh Market Maker"
log_info "=========================================="

# 0. 检查并构建（如果需要）
log_info "步骤 0/4: 检查并构建项目..."
check_and_build

# 1. 杀掉旧进程
log_info "步骤 1/4: 检查并停止旧进程..."
kill_old_process

# 2. 杀掉占用端口的进程
log_info "步骤 2/4: 检查并释放端口..."
kill_port_process "${WEB_PORT}"

# 3. 启动新进程
log_info "步骤 3/4: 启动新服务..."
cd "${SCRIPT_DIR}"

# 启动服务（后台运行）
nohup "./${BINARY_NAME}" "${CONFIG_FILE}" >> "${LOG_FILE}" 2>&1 &
NEW_PID=$!

# 保存PID
echo "${NEW_PID}" > "${PID_FILE}"

# 等待一下，检查进程是否启动成功
sleep 2

if kill -0 "${NEW_PID}" 2>/dev/null; then
    log_info "步骤 4/4: 服务启动完成"
    log_info ""
    log_info "✅ 服务启动成功！"
    log_info "   PID: ${NEW_PID}"
    log_info "   端口: ${WEB_PORT}"
    log_info "   Web界面: http://localhost:${WEB_PORT}"
    log_info "   日志: ${LOG_FILE}"
    log_info "   配置文件: ${CONFIG_FILE}"
    log_info ""
    log_info "停止服务: kill ${NEW_PID} 或运行 stop.sh"
    log_info "按 Ctrl+C 停止查看日志（服务会继续运行）"
    log_info ""
    log_info "=========================================="
    log_info "正在跟踪日志..."
    log_info "=========================================="
    sleep 1
    
    # 自动跟踪日志
    tail -f "${LOG_FILE}"
else
    log_error "❌ 服务启动失败！"
    log_error "请查看日志: ${LOG_FILE}"
    rm -f "${PID_FILE}"
    exit 1
fi

