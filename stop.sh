#!/bin/bash

# QuantMesh Market Maker 停止脚本
# 支持停止生产模式和开发模式的所有进程
#
# 使用方法：
#   ./stop.sh           # 停止所有进程（生产和开发）
#   ./stop.sh --dev     # 仅停止开发模式进程
#   ./stop.sh --prod    # 仅停止生产模式进程

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

# 端口配置
GO_PORT=28888
VITE_PORT=15173

# PID 文件
PID_FILE="${SCRIPT_DIR}/.${APP_NAME}.pid"
PID_FILE_GO="${SCRIPT_DIR}/.dev_go.pid"
PID_FILE_VITE="${SCRIPT_DIR}/.dev_vite.pid"

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 解析参数
STOP_DEV=true
STOP_PROD=true

for arg in "$@"; do
    case $arg in
        --dev)
            STOP_DEV=true
            STOP_PROD=false
            ;;
        --prod)
            STOP_DEV=false
            STOP_PROD=true
            ;;
        -h|--help)
            echo "使用方法: $0 [选项]"
            echo ""
            echo "选项:"
            echo "  --dev      仅停止开发模式进程"
            echo "  --prod     仅停止生产模式进程"
            echo "  -h, --help 显示此帮助信息"
            echo ""
            echo "默认行为：停止所有进程（生产和开发）"
            exit 0
            ;;
    esac
done

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
    local found=false
    
    # 从 PID 文件停止 Go 开发进程
    if [ -f "${PID_FILE_GO}" ]; then
        local old_pid=$(cat "${PID_FILE_GO}" 2>/dev/null || echo "")
        if [ -n "${old_pid}" ] && kill -0 "${old_pid}" 2>/dev/null; then
            log_info "停止 Go 开发进程 (PID: ${old_pid})"
            kill -TERM "${old_pid}" 2>/dev/null || true
            sleep 1
            kill -9 "${old_pid}" 2>/dev/null || true
            found=true
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
            found=true
        fi
        rm -f "${PID_FILE_VITE}"
    fi
    
    # 杀掉占用 Vite 端口的进程
    kill_port_process ${VITE_PORT} "Vite 前端"
    
    # 通过进程名杀掉可能遗留的进程
    if pgrep -f "go run main.go symbol_manager.go" >/dev/null 2>&1; then
        log_info "停止 go run 进程..."
        pkill -f "go run main.go symbol_manager.go" 2>/dev/null || true
        found=true
    fi
    if pgrep -f "go run main.go" >/dev/null 2>&1; then
        log_info "停止 go run 进程..."
        pkill -f "go run main.go" 2>/dev/null || true
        found=true
    fi
    if pgrep -f "vite.*${VITE_PORT}" >/dev/null 2>&1; then
        log_info "停止 vite 进程..."
        pkill -f "vite.*${VITE_PORT}" 2>/dev/null || true
        found=true
    fi
    if pgrep -f "pnpm.*dev" >/dev/null 2>&1; then
        log_info "停止 pnpm dev 进程..."
        pkill -f "pnpm.*dev" 2>/dev/null || true
        found=true
    fi
    
    if [ "$found" = true ]; then
        log_info "✅ 开发模式进程已停止"
    else
        log_info "未发现运行中的开发模式进程"
    fi
}

# 停止生产模式进程
stop_prod_processes() {
    local found=false
    
    # 从 PID 文件停止
    if [ -f "${PID_FILE}" ]; then
        local pid=$(cat "${PID_FILE}" 2>/dev/null || echo "")
        if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
            log_info "停止生产进程 (PID: ${pid})"
            kill -TERM "${pid}" 2>/dev/null || true
            sleep 2
            if kill -0 "${pid}" 2>/dev/null; then
                kill -9 "${pid}" 2>/dev/null || true
            fi
            found=true
        fi
        rm -f "${PID_FILE}"
    fi
    
    # 通过进程名查找并杀掉
    local pids=$(pgrep -f "^\./${BINARY_NAME}" 2>/dev/null || pgrep -x "${BINARY_NAME}" 2>/dev/null || echo "")
    if [ -n "${pids}" ]; then
        log_info "停止通过进程名匹配的进程..."
        for pid in ${pids}; do
            if kill -0 "${pid}" 2>/dev/null; then
                log_info "停止进程 PID: ${pid}"
                kill -TERM "${pid}" 2>/dev/null || true
                found=true
            fi
        done
        sleep 2
        for pid in ${pids}; do
            if kill -0 "${pid}" 2>/dev/null; then
                kill -9 "${pid}" 2>/dev/null || true
            fi
        done
    fi
    
    # 杀掉占用 Go 端口的进程
    kill_port_process ${GO_PORT} "Go 后端"
    
    if [ "$found" = true ]; then
        log_info "✅ 生产模式进程已停止"
    else
        log_info "未发现运行中的生产模式进程"
    fi
}

log_info "=========================================="
log_info "停止 QuantMesh Market Maker"
log_info "=========================================="

if [ "$STOP_DEV" = true ]; then
    stop_dev_processes
fi

if [ "$STOP_PROD" = true ]; then
    stop_prod_processes
fi

log_info "=========================================="
log_info "停止完成"
log_info "=========================================="

