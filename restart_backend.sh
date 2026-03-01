#!/bin/bash

# QuantMesh 仅重启后端脚本
# 只停止并重启 Go 后端，不涉及前端
#
# 使用方法：
#   ./restart_backend.sh [config.yaml]   # 生产模式重启后端
#   ./restart_backend.sh --dev           # 开发模式重启后端（仅重启 go run，Vite 保持运行）

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GO_PORT=28888
APP_NAME="quantmesh"
PID_FILE="${SCRIPT_DIR}/.${APP_NAME}.pid"
PID_FILE_GO="${SCRIPT_DIR}/.dev_go.pid"
BINARY_NAME="quantmesh"
log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

show_help() {
    echo "用法: $0 [选项] [配置文件]"
    echo ""
    echo "选项:"
    echo "  -d, --dev    开发模式：仅重启 go run，不重启 Vite"
    echo "  -h, --help   显示帮助"
    echo ""
    echo "示例:"
    echo "  $0              # 生产模式，使用 config.yaml"
    echo "  $0 config.yaml  # 生产模式，指定配置"
    echo "  $0 --dev        # 开发模式"
    exit 0
}

DEV_MODE=false
CONFIG_FILE="config.yaml"
for arg in "$@"; do
    case "$arg" in
        -h|--help) show_help ;;
        -d|--dev)  DEV_MODE=true ;;
        -*)
            log_error "未知选项: $arg"
            show_help
            ;;
        *) CONFIG_FILE="$arg" ;;
    esac
done

kill_port_process() {
    local port=$1
    [ -z "$port" ] && return
    local pid=""
    if command -v lsof >/dev/null 2>&1; then
        pid=$(lsof -ti:${port} 2>/dev/null || echo "")
    elif command -v fuser >/dev/null 2>&1; then
        pid=$(fuser ${port}/tcp 2>/dev/null | awk '{print $1}' || echo "")
    fi
    if [ -n "${pid}" ]; then
        log_warn "停止占用端口 ${port} 的进程 (PID: ${pid})..."
        kill -TERM ${pid} 2>/dev/null || true
        sleep 1
        kill -9 ${pid} 2>/dev/null || true
        log_info "端口 ${port} 已释放"
    fi
}

stop_backend() {
    log_info "停止后端..."
    # PID 文件
    for pf in "${PID_FILE}" "${PID_FILE_GO}"; do
        if [ -f "$pf" ]; then
            local pid=$(cat "$pf" 2>/dev/null || echo "")
            if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
                log_info "停止进程 PID: $pid"
                kill -TERM "$pid" 2>/dev/null || true
                sleep 1
                kill -9 "$pid" 2>/dev/null || true
            fi
            rm -f "$pf"
        fi
    done
    # 进程名
    pkill -f "go run main.go symbol_manager.go" 2>/dev/null || true
    pkill -f "go run main.go" 2>/dev/null || true
    local pids=$(pgrep -f "${BINARY_NAME}" 2>/dev/null || echo "")
    if [ -n "$pids" ]; then
        echo "$pids" | xargs kill -TERM 2>/dev/null || true
        sleep 2
        echo "$pids" | xargs kill -9 2>/dev/null || true
    fi
    kill_port_process ${GO_PORT}
    sleep 1
}

log_info "=========================================="
log_info "重启 QuantMesh 后端"
log_info "=========================================="

stop_backend

if [ "$DEV_MODE" = true ]; then
    log_info "启动开发模式后端 (go run)..."
    cd "${SCRIPT_DIR}"
    go run main.go symbol_manager.go &
    echo $! > "${PID_FILE_GO}"
    sleep 2
    if kill -0 $(cat "${PID_FILE_GO}") 2>/dev/null; then
        log_info "✅ 后端已启动 (PID: $(cat ${PID_FILE_GO}))"
        log_info "   http://localhost:${GO_PORT}"
    else
        log_error "后端启动失败"
        exit 1
    fi
else
    log_info "启动生产模式后端..."
    exec "${SCRIPT_DIR}/start.sh" "${CONFIG_FILE}"
fi
