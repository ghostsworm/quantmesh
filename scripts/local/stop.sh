#!/bin/bash

# QuantMesh Market Maker 停止脚本
# 支持停止生产模式和开发模式的所有进程
#
# 使用方法：
#   ./scripts/local/stop.sh           # 停止所有进程（生产和开发）
#   ./scripts/local/stop.sh --dev     # 仅停止开发模式进程
#   ./scripts/local/stop.sh --prod    # 仅停止生产模式进程

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 配置
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
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

# ---------- 开发模式：优雅停止整棵进程树 ----------
# dev.sh 把后端编译到固定路径并以独立进程组启动；旧版 `go run .` 会派生 go-build 缓存里的子进程，
# 只杀 go run 的 PID 会留下占着 28888 的孤儿。这里按「进程组 + 直接子进程 + 端口监听者」逐层兜底。

# 优雅退出等待秒数（后端退出时会撤单/落库，给足时间）
DEV_STOP_TIMEOUT="${DEV_STOP_TIMEOUT:-20}"
# SIGTERM 之后到 SIGKILL 的等待秒数
DEV_STOP_TERM_WAIT=3
DEV_BIN="${SCRIPT_DIR}/.dev/quantmesh-dev"

# ---------- 生产模式：只识别本仓库的二进制 ----------
PROD_BIN="${SCRIPT_DIR}/${BINARY_NAME}"
# 优雅退出等待秒数（后端退出时会撤单/平仓/落库，给足时间）
PROD_STOP_TIMEOUT="${PROD_STOP_TIMEOUT:-30}"
# SIGTERM 之后到 SIGKILL 的等待秒数
PROD_STOP_TERM_WAIT=3

# 共用进程函数：pid_alive / signal_tree / wait_pids_exit / graceful_stop_pid / stop_by_pattern /
# is_this_checkout_binary / find_prod_pids / graceful_stop_prod_pids / check_port_owner 等
# shellcheck source=lib/process.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/process.sh"

# 从 PID 文件停止并删除 PID 文件
stop_from_pid_file() {
    local name=$1
    local file=$2
    if [ ! -f "${file}" ]; then
        return 1
    fi
    local pid
    pid=$(cat "${file}" 2>/dev/null || echo "")
    rm -f "${file}"
    graceful_stop_pid "${name}" "${pid}"
}

# 停止监听指定端口的进程（兜底：孤儿子进程、手动启动的实例）
stop_port_listeners() {
    local port=$1
    local name=$2
    local pids=""
    if command -v lsof >/dev/null 2>&1; then
        pids=$(lsof -nP -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null | sort -u | tr '\n' ' ' || true)
    elif command -v fuser >/dev/null 2>&1; then
        pids=$(fuser "${port}"/tcp 2>/dev/null || true)
    fi
    local stopped=1
    local pid
    for pid in ${pids}; do
        if graceful_stop_pid "${name} (端口 ${port})" "${pid}"; then
            stopped=0
        fi
    done
    return ${stopped}
}

# 停止开发模式进程（幂等：没有进程时只输出提示）
stop_dev_processes() {
    local found=false

    if stop_from_pid_file "Go 开发进程" "${PID_FILE_GO}"; then found=true; fi
    if stop_from_pid_file "Vite 开发进程" "${PID_FILE_VITE}"; then found=true; fi

    # 遗留进程：dev.sh 编译产物、旧版 go run 及其 go-build 缓存子进程、vite
    # 模式必须足够具体（带仓库路径/二进制名），宽泛的 "vite"/"go run" 会误伤调用方 shell 或其他项目
    if stop_by_pattern "Go 开发二进制" "^${DEV_BIN}"; then found=true; fi
    if stop_by_pattern "go-build 缓存进程" "go-build[^ ]*/exe/quantmesh( |$)"; then found=true; fi
    if stop_by_pattern "vite 进程" "${SCRIPT_DIR}/webui/node_modules/.*vite"; then found=true; fi

    # 端口兜底
    if stop_port_listeners "${GO_PORT}" "Go 后端"; then found=true; fi
    if stop_port_listeners "${VITE_PORT}" "Vite 前端"; then found=true; fi

    if [ "$found" = true ]; then
        log_info "✅ 开发模式进程已停止"
    else
        log_info "未发现运行中的开发模式进程"
    fi
}

# 停止生产模式进程（幂等）。只停止可执行文件为 ${PROD_BIN} 的进程（PID 文件经可执行文件路径核验），
# SIGINT → 等待 PROD_STOP_TIMEOUT → SIGTERM → 等待 → SIGKILL。
# Go 端口被非本仓库进程占用时只报告、不杀，并返回 1。
stop_prod_processes() {
    local found=false

    if stop_this_checkout_prod; then
        found=true
    fi

    # 端口兜底：只处理本仓库二进制
    check_port_owner "${GO_PORT}" "Go 后端"
    if [ -n "${PORT_OWNER_OTHERS}" ]; then
        log_error "端口 ${GO_PORT} 仍被非本仓库进程占用 (PID: ${PORT_OWNER_OTHERS})，未停止；请确认是否为其他 checkout / 试跑实例后手动处理"
        return 1
    fi

    if [ "$found" = true ]; then
        log_info "✅ 生产模式进程已停止"
    else
        log_info "未发现运行中的生产模式进程 (${PROD_BIN})"
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

