#!/bin/bash

# QuantMesh Market Maker 停止脚本

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
PID_FILE="${SCRIPT_DIR}/.${APP_NAME}.pid"

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 停止服务
stop_service() {
    if [ ! -f "${PID_FILE}" ]; then
        log_warn "PID文件不存在，尝试通过进程名查找..."
        local pids=$(pgrep -f "${BINARY_NAME}" 2>/dev/null || echo "")
        if [ -z "${pids}" ]; then
            log_info "未发现运行中的服务"
            return 0
        fi
    else
        local pid=$(cat "${PID_FILE}" 2>/dev/null || echo "")
        if [ -z "${pid}" ] || ! kill -0 "${pid}" 2>/dev/null; then
            log_warn "PID文件中的进程不存在，尝试通过进程名查找..."
            local pids=$(pgrep -f "${BINARY_NAME}" 2>/dev/null || echo "")
            if [ -z "${pids}" ]; then
                log_info "未发现运行中的服务"
                rm -f "${PID_FILE}"
                return 0
            fi
        else
            local pids="${pid}"
        fi
    fi

    log_info "正在停止服务..."
    for pid in ${pids}; do
        if kill -0 "${pid}" 2>/dev/null; then
            log_info "停止进程 PID: ${pid}"
            kill -TERM "${pid}" 2>/dev/null || true
        fi
    done

    # 等待进程退出
    sleep 3

    # 检查是否还有进程在运行
    local remaining=""
    for pid in ${pids}; do
        if kill -0 "${pid}" 2>/dev/null; then
            remaining="${remaining} ${pid}"
        fi
    done

    if [ -n "${remaining}" ]; then
        log_warn "部分进程未响应，强制停止..."
        for pid in ${remaining}; do
            kill -9 "${pid}" 2>/dev/null || true
        done
        sleep 1
    fi

    # 清理PID文件
    rm -f "${PID_FILE}"

    log_info "✅ 服务已停止"
}

log_info "=========================================="
log_info "停止 QuantMesh Market Maker"
log_info "=========================================="

stop_service

