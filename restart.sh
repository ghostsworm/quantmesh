#!/bin/bash

# QuantMesh Market Maker 啟動/重啟腳本
# QuantMesh Market Maker Start/Restart Script
#
# 功能 / Features:
# - 支持生產模式和開發模式 / Support production and development modes
# - 如果服務正在運行，先停止再啟動（重啟模式）/ Stop and restart if running
# - 自動處理端口衝突 / Auto handle port conflicts
#
# 使用方法 / Usage:
#   ./restart.sh                     # 默認開發模式（Go + Vite 熱更新）
#   ./restart.sh --prod [config.yaml]  # 生產模式（構建後端 + webui/dist）
#   ./restart.sh -d                  # 等同默認，明確指定開發模式

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 端口配置
GO_PORT=28888
VITE_PORT=15173

# PID 文件
APP_NAME="quantmesh"
PID_FILE="${SCRIPT_DIR}/.${APP_NAME}.pid"
PID_FILE_GO="${SCRIPT_DIR}/.dev_go.pid"
PID_FILE_VITE="${SCRIPT_DIR}/.dev_vite.pid"
BINARY_NAME="quantmesh"

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 显示帮助信息
show_help() {
    echo "使用方法 / Usage: $0 [选项] [配置文件（僅 --prod）]"
    echo ""
    echo "选项 / Options:"
    echo "  （默認）         開發模式：Go go run + webui Vite 熱更新"
    echo "  -d, --dev        同上，明確指定開發模式"
    echo "  -p, --prod       生產模式：編譯二進制 + webui build，讀取 config.yaml（可缺省）"
    echo "  -h, --help       顯示此幫助信息 / Show this help message"
    echo ""
    echo "示例 / Examples:"
    echo "  $0                    # 開發模式（默認）"
    echo "  $0 --prod             # 生產模式，默認 config.yaml"
    echo "  $0 -p my.yaml         # 生產模式，指定配置"
    echo "  $0 -d                 # 開發模式（明確）"
    echo ""
    echo "端口配置 / Port Configuration:"
    echo "  Go 後端 / Backend: ${GO_PORT}"
    echo "  Vite 前端（僅開發模式 / dev only）: ${VITE_PORT}"
    echo ""
    exit 0
}

# 解析参数（默認開發模式；需生產模式時顯式傳 --prod / -p）
DEV_MODE=true
CONFIG_FILE=""
PROD_EXPLICIT=false

for arg in "$@"; do
    case $arg in
        -h|--help)
            show_help
            ;;
        -d|--dev)
            DEV_MODE=true
            ;;
        -p|--prod)
            DEV_MODE=false
            PROD_EXPLICIT=true
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

# 未使用 --prod 時若帶了配置文件參數，提示並忽略（避免誤以為會影響 dev）
if [ "$DEV_MODE" = true ] && [ -n "$CONFIG_FILE" ] && [ "$PROD_EXPLICIT" = false ]; then
    log_warn "已忽略參數「${CONFIG_FILE}」：開發模式不使用該路徑；生產模式請用: $0 --prod ${CONFIG_FILE}"
    CONFIG_FILE=""
fi

# 默认配置文件
CONFIG_FILE="${CONFIG_FILE:-config.yaml}"

# 杀掉占用端口的进程
kill_port_process() {
    local port=$1
    local name=$2
    if [ -z "$port" ]; then
        return
    fi

    local pid=""
    if command -v lsof >/dev/null 2>&1; then
        pid=$(lsof -ti:${port} 2>/dev/null || echo "")
    elif command -v fuser >/dev/null 2>&1; then
        pid=$(fuser ${port}/tcp 2>/dev/null | awk '{print $1}' || echo "")
    fi

    if [ -n "${pid}" ]; then
        log_warn "发现占用端口 ${port} 的进程 (PID: ${pid})，正在停止..."
        kill -TERM ${pid} 2>/dev/null || true
        sleep 1
        if kill -0 ${pid} 2>/dev/null; then
            kill -9 ${pid} 2>/dev/null || true
        fi
        log_info "端口 ${port} (${name}) 已释放"
    fi
}

# 停止开发模式进程
stop_dev_processes() {
    log_info "停止开发模式进程..."
    
    # 从 PID 文件停止 Go 进程
    if [ -f "${PID_FILE_GO}" ]; then
        local old_pid=$(cat "${PID_FILE_GO}" 2>/dev/null || echo "")
        if [ -n "${old_pid}" ] && kill -0 "${old_pid}" 2>/dev/null; then
            log_info "停止 Go 开发进程 (PID: ${old_pid})"
            kill -TERM "${old_pid}" 2>/dev/null || true
            sleep 1
            kill -9 "${old_pid}" 2>/dev/null || true
        fi
        rm -f "${PID_FILE_GO}"
    fi
    
    # 从 PID 文件停止 Vite 进程
    if [ -f "${PID_FILE_VITE}" ]; then
        local old_pid=$(cat "${PID_FILE_VITE}" 2>/dev/null || echo "")
        if [ -n "${old_pid}" ] && kill -0 "${old_pid}" 2>/dev/null; then
            log_info "停止 Vite 开发进程 (PID: ${old_pid})"
            kill -TERM "${old_pid}" 2>/dev/null || true
            sleep 1
            kill -9 "${old_pid}" 2>/dev/null || true
        fi
        rm -f "${PID_FILE_VITE}"
    fi
    
    # 杀掉占用端口的进程
    kill_port_process ${GO_PORT} "Go 后端"
    kill_port_process ${VITE_PORT} "Vite 前端"
    
    # 通过进程名杀掉可能遗留的进程
    pkill -f "go run main.go symbol_manager.go" 2>/dev/null || true
    pkill -f "go run main.go" 2>/dev/null || true
    pkill -f "vite.*${VITE_PORT}" 2>/dev/null || true
    pkill -f "pnpm.*dev" 2>/dev/null || true
    
    sleep 1
}

# 停止生产模式进程
stop_prod_processes() {
    log_info "停止生产模式进程..."
    
    # 从 PID 文件停止
    if [ -f "${PID_FILE}" ]; then
        local old_pid=$(cat "${PID_FILE}" 2>/dev/null || echo "")
        if [ -n "${old_pid}" ] && kill -0 "${old_pid}" 2>/dev/null; then
            log_info "停止生产进程 (PID: ${old_pid})"
            kill -TERM "${old_pid}" 2>/dev/null || true
            sleep 2
            kill -9 "${old_pid}" 2>/dev/null || true
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
    fi
    
    # 杀掉占用端口的进程
    kill_port_process ${GO_PORT} "Go 后端"
    
    sleep 1
}

# 检查是否有开发模式进程在运行
has_dev_processes() {
    if [ -f "${PID_FILE_GO}" ] || [ -f "${PID_FILE_VITE}" ]; then
        return 0
    fi
    
    # 检查端口
    if command -v lsof >/dev/null 2>&1; then
        if lsof -ti:${VITE_PORT} >/dev/null 2>&1; then
            return 0
        fi
    fi
    
    # 检查进程名
    if pgrep -f "go run main.go symbol_manager.go" >/dev/null 2>&1; then
        return 0
    fi
    if pgrep -f "go run main.go" >/dev/null 2>&1; then
        return 0
    fi
    if pgrep -f "vite.*${VITE_PORT}" >/dev/null 2>&1; then
        return 0
    fi
    
    return 1
}

# 检查是否有生产模式进程在运行
has_prod_processes() {
    if [ -f "${PID_FILE}" ]; then
        local pid=$(cat "${PID_FILE}" 2>/dev/null || echo "")
        if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
            return 0
        fi
    fi
    
    if pgrep -f "${BINARY_NAME}" >/dev/null 2>&1; then
        return 0
    fi
    
    return 1
}

# 主流程
log_info "=========================================="
if [ "$DEV_MODE" = true ]; then
    log_info "重启 QuantMesh（开发模式）"
else
    log_info "重启 QuantMesh（生产模式）"
fi
log_info "=========================================="
echo ""

if [ "$DEV_MODE" = true ]; then
    # 开发模式
    
    # 停止所有可能运行的进程（开发和生产）
    if has_dev_processes; then
        stop_dev_processes
    fi
    if has_prod_processes; then
        stop_prod_processes
    fi
    
    # 启动开发模式
    log_info "启动开发模式..."
    exec "${SCRIPT_DIR}/dev.sh"
else
    # 生产模式
    
    # 停止所有可能运行的进程（开发和生产）
    if has_dev_processes; then
        log_warn "检测到开发模式进程，正在停止..."
        stop_dev_processes
    fi
    if has_prod_processes; then
        log_warn "检测到生产模式进程，正在停止..."
        stop_prod_processes
    fi
    
    # 启动生产模式
    log_info "启动生产模式..."
    exec "${SCRIPT_DIR}/start.sh" "${CONFIG_FILE}"
fi

