#!/bin/bash

# QuantMesh 仅重启 API 后端脚本
# 只停止并重启 Go API 服务，不编译前端（前端由 Vite 单独服务）
#
# 使用方法：
#   ./scripts/local/restart_api.sh [config.yaml]   # 生产模式重启 API
#   ./scripts/local/restart_api.sh --dev           # 开发模式（编译 .dev/quantmesh-dev 后运行，Vite 保持运行）
#
# 停止策略与 stop.sh / restart.sh 共用 lib/process.sh：
#   - 只停止本仓库的二进制（生产 ./quantmesh、开发 .dev/quantmesh-dev），不按进程名宽泛匹配；
#   - 先 SIGINT 让程序撤单退出，超时再 SIGTERM，最后才 SIGKILL；
#   - 端口被其他进程占用时只报告并退出，不杀。

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GO_PORT=28888
APP_NAME="quantmesh"
BINARY_NAME="quantmesh"
PID_FILE="${SCRIPT_DIR}/.${APP_NAME}.pid"
PID_FILE_GO="${SCRIPT_DIR}/.dev_go.pid"
DEV_DIR="${SCRIPT_DIR}/.dev"
DEV_BIN="${DEV_DIR}/quantmesh-dev"
log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# shellcheck source=lib/process.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/process.sh"

show_help() {
    echo "用法: $0 [选项] [配置文件]"
    echo ""
    echo "选项:"
    echo "  -d, --dev    开发模式：仅重启 Go 后端，不重启 Vite"
    echo "  -h, --help   显示帮助"
    echo ""
    echo "示例:"
    echo "  $0              # 生产模式，使用 config.yaml，仅重启 API（不编译前端）"
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

# 停止开发模式后端：PID 文件中的进程，以及本仓库 .dev/quantmesh-dev 的残留进程
stop_dev_backend() {
    if [ -f "${PID_FILE_GO}" ]; then
        local pid
        pid=$(cat "${PID_FILE_GO}" 2>/dev/null || echo "")
        if [ -n "${pid}" ]; then
            graceful_stop_pid "Go 开发后端" "${pid}" || true
        fi
        rm -f "${PID_FILE_GO}"
    fi
    stop_by_pattern "Go 开发后端" "${DEV_BIN}" || true
}

stop_backend() {
    log_info "停止后端..."
    stop_dev_backend
    if ! stop_this_checkout_prod; then
        log_info "未发现运行中的本仓库生产后端"
    fi

    check_port_owner "${GO_PORT}"
    if [ -n "${PORT_OWNER_OTHERS:-}" ]; then
        log_error "端口 ${GO_PORT} 被其他进程占用 (PID: ${PORT_OWNER_OTHERS})，不是本仓库的后端，未停止。请手动处理后重试。"
        exit 1
    fi
}

log_info "=========================================="
log_info "重启 QuantMesh API 后端"
log_info "=========================================="

stop_backend

if [ "$DEV_MODE" = true ]; then
    log_info "编译开发模式后端 -> ${DEV_BIN} ..."
    mkdir -p "${DEV_DIR}"
    cd "${SCRIPT_DIR}"
    go build -o "${DEV_BIN}" .
    "${DEV_BIN}" &
    echo $! > "${PID_FILE_GO}"
    sleep 2
    if kill -0 "$(cat "${PID_FILE_GO}")" 2>/dev/null; then
        log_info "✅ 后端已启动 (PID: $(cat "${PID_FILE_GO}"))"
        log_info "   http://localhost:${GO_PORT}"
    else
        log_error "后端启动失败"
        rm -f "${PID_FILE_GO}"
        exit 1
    fi
else
    log_info "启动生产模式 API（不编译前端）..."
    exec "${SCRIPT_DIR}/scripts/local/start.sh" --api-only "${CONFIG_FILE}"
fi
