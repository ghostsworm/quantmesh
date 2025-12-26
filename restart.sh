#!/bin/bash

# OpenSQT Market Maker 启动/重启脚本
# 功能：
# - 如果服务未运行，直接启动
# - 如果服务正在运行，先停止再启动（重启模式）
# - 自动构建前端和后端（如果需要）
# - 自动处理端口冲突

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 获取配置文件参数（如果提供）
CONFIG_FILE="${1:-config.yaml}"

# 检查是否有服务在运行
APP_NAME="opensqt"
PID_FILE="${SCRIPT_DIR}/.${APP_NAME}.pid"
BINARY_NAME="opensqt"

# 检查是否有运行中的服务
has_running_service() {
    # 检查PID文件
    if [ -f "${PID_FILE}" ]; then
        local pid=$(cat "${PID_FILE}" 2>/dev/null || echo "")
        if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
            return 0  # 有运行中的服务
        fi
    fi
    
    # 检查进程名
    if pgrep -f "${BINARY_NAME}" >/dev/null 2>&1; then
        return 0  # 有运行中的服务
    fi
    
    return 1  # 没有运行中的服务
}

# 如果有运行中的服务，先停止
if has_running_service; then
    echo "🔄 检测到运行中的服务，先停止..."
    "${SCRIPT_DIR}/stop.sh"
    sleep 2
fi

# 启动服务（直接调用 start.sh）
"${SCRIPT_DIR}/start.sh" "${CONFIG_FILE}"

