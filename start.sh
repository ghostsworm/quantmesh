#!/bin/bash
# QuantMesh Market Maker 启动/重启脚本
# 功能：
# 1. 检查并杀掉旧进程（重启模式）
# 2. 杀掉占用端口的进程
# 3. 自动构建前端和后端（如果需要）
# 4. 启动新服务
#
# 使用方法：
#   ./start.sh [config.yaml] [-f|--force]
#   -f, --force: 强制重新编译前后端

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 显示帮助信息
show_help() {
    echo "使用方法: $0 [配置文件] [选项]"
    echo ""
    echo "选项:"
    echo "  -f, --force    强制重新编译前后端（忽略时间戳检查）"
    echo "  -h, --help     显示此帮助信息"
    echo ""
    echo "示例:"
    echo "  $0                    # 使用默认配置文件 config.yaml"
    echo "  $0 config.yaml        # 使用指定配置文件"
    echo "  $0 -f                 # 强制重新编译"
    echo "  $0 config.yaml -f     # 使用指定配置文件并强制重新编译"
    echo ""
    exit 0
}

# 解析参数
FORCE_BUILD=false
CONFIG_FILE=""

for arg in "$@"; do
    case $arg in
        -h|--help)
            show_help
            ;;
        -f|--force)
            FORCE_BUILD=true
            ;;
        -*)
            log_error "未知选项: $arg"
            show_help
            ;;
        *)
            if [ -z "$CONFIG_FILE" ]; then
                CONFIG_FILE="$arg"
            fi
            ;;
    esac
done

# 配置
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_NAME="quantmesh"
BINARY_NAME="quantmesh"
CONFIG_FILE="${CONFIG_FILE:-config.yaml}"
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
    
    # 如果强制构建，直接设置为 true
    if [ "$FORCE_BUILD" = true ]; then
        need_build=true
        log_info "强制构建模式，将重新构建前端"
    # 如果 dist 目录不存在，需要构建
    elif [ ! -d "${SCRIPT_DIR}/webui/dist" ]; then
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
        
        # 如果是强制构建，先清理旧的构建产物
        if [ "$FORCE_BUILD" = true ]; then
            log_info "清理旧的前端构建产物..."
            rm -rf dist
            rm -rf "${SCRIPT_DIR}/web/dist"
        fi
        
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
    
    # 如果强制构建，直接设置为 true
    if [ "$FORCE_BUILD" = true ]; then
        need_build=true
        log_info "强制构建模式，将重新构建项目"
    # 如果二进制文件不存在，需要构建
    elif [ ! -f "${SCRIPT_DIR}/${BINARY_NAME}" ]; then
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
        
        # 检查前端源码是否比二进制新
        if [ -d "${SCRIPT_DIR}/webui/src" ]; then
            if find "${SCRIPT_DIR}/webui/src" -type f \( -name "*.tsx" -o -name "*.ts" -o -name "*.jsx" -o -name "*.js" -o -name "*.css" \) -newer "${SCRIPT_DIR}/${BINARY_NAME}" 2>/dev/null | grep -q .; then
                frontend_newer=true
            fi
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
        # 只编译主程序文件，排除测试和回测文件
        go build -o "${BINARY_NAME}" -tags="!test" main.go symbol_manager.go 2>/dev/null || \
        go build -o "${BINARY_NAME}" $(find . -maxdepth 1 -name "*.go" ! -name "*_test.go" ! -name "test_*.go" ! -name "run_*.go" ! -name "analyze_*.go" | tr '\n' ' ')
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

# 启动服务（后台运行，启用 debug 以全量输出 Gin 日志）
nohup "./${BINARY_NAME}" -debug "${CONFIG_FILE}" >> "${LOG_FILE}" 2>&1 &
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
    log_info "   配置文件: ${CONFIG_FILE}"
    log_info ""
    
    # 查找最新的日志文件
    find_latest_log_file() {
        local log_pattern=$1
        local latest_file=""
        local latest_time=0
        
        if [ -d "${SCRIPT_DIR}/logs" ]; then
            for file in "${SCRIPT_DIR}/logs"/${log_pattern}*.log; do
                if [ -f "$file" ]; then
                    local file_time=$(stat -f "%m" "$file" 2>/dev/null || stat -c "%Y" "$file" 2>/dev/null || echo "0")
                    if [ "$file_time" -gt "$latest_time" ]; then
                        latest_time=$file_time
                        latest_file="$file"
                    fi
                fi
            done
        fi
        
        echo "$latest_file"
    }
    
    # 查找最新的应用日志文件（app-quantmesh-YYYY-MM-DD.log）
    APP_LOG_FILE=$(find_latest_log_file "app-quantmesh-")
    
    # 如果没找到应用日志文件，尝试查找旧的日志文件（quantmesh.log）
    if [ -z "$APP_LOG_FILE" ] || [ ! -f "$APP_LOG_FILE" ]; then
        if [ -f "${LOG_FILE}" ]; then
            APP_LOG_FILE="${LOG_FILE}"
        fi
    fi
    
    # 查找最新的Web日志文件（web-gin-YYYY-MM-DD.log）
    WEB_LOG_FILE=$(find_latest_log_file "web-gin-")
    
    log_info "   应用日志: ${APP_LOG_FILE:-未找到（日志级别可能不是DEBUG）}"
    if [ -n "$WEB_LOG_FILE" ] && [ -f "$WEB_LOG_FILE" ]; then
        log_info "   Web日志: ${WEB_LOG_FILE}"
    fi
    log_info ""
    log_info "停止服务: kill ${NEW_PID} 或运行 stop.sh"
    log_info "按 Ctrl+C 停止查看日志（服务会继续运行）"
    log_info ""
    log_info "=========================================="
    log_info "正在跟踪日志..."
    log_info "=========================================="
    sleep 1
    
    # 自动跟踪日志（优先跟踪应用日志，如果存在）
    if [ -n "$APP_LOG_FILE" ] && [ -f "$APP_LOG_FILE" ]; then
        tail -f "$APP_LOG_FILE"
    elif [ -n "$WEB_LOG_FILE" ] && [ -f "$WEB_LOG_FILE" ]; then
        log_warn "未找到应用日志文件，跟踪Web日志..."
        tail -f "$WEB_LOG_FILE"
    else
        log_warn "未找到日志文件，日志可能只输出到控制台"
        log_info "提示：如果日志级别不是DEBUG，日志不会写入文件"
        log_info "可以通过配置文件设置 system.log_level: DEBUG 来启用文件日志"
        # 等待一段时间让用户看到提示
        sleep 3
    fi
else
    log_error "❌ 服务启动失败！"
    log_error "请查看日志: ${LOG_FILE}"
    rm -f "${PID_FILE}"
    exit 1
fi

