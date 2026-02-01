#!/bin/bash

# QuantMesh 开发模式启动脚本
# 同时启动 Go 后端和 Vite 前端开发服务器
#
# 端口规划：
#   - Go 后端：28888（API 和 WebSocket）
#   - Vite 前端：15173（开发服务器，代理 /api 和 /ws 到后端）

set -e

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# 端口配置
GO_PORT=28888
VITE_PORT=15173

# 脚本目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  QuantMesh 开发模式启动${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# 检查 Go 是否安装
if ! command -v go &> /dev/null; then
    log_error "未找到 Go，请先安装 Go"
    exit 1
fi

# 检查 pnpm 是否安装
if ! command -v pnpm &> /dev/null; then
    log_error "未找到 pnpm，请先安装 pnpm"
    log_warn "安装命令: npm install -g pnpm"
    exit 1
fi

# 检查 webui 目录是否存在
if [ ! -d "${SCRIPT_DIR}/webui" ]; then
    log_error "未找到 webui 目录"
    exit 1
fi

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

# 停止旧的开发进程
stop_dev_processes() {
    log_info "检查并停止旧的开发进程..."
    
    # 从 PID 文件停止
    if [ -f "${PID_FILE_GO}" ]; then
        local old_pid=$(cat "${PID_FILE_GO}" 2>/dev/null || echo "")
        if [ -n "${old_pid}" ] && kill -0 "${old_pid}" 2>/dev/null; then
            log_info "停止旧的 Go 进程 (PID: ${old_pid})"
            kill -TERM "${old_pid}" 2>/dev/null || true
            sleep 1
        fi
        rm -f "${PID_FILE_GO}"
    fi
    
    if [ -f "${PID_FILE_VITE}" ]; then
        local old_pid=$(cat "${PID_FILE_VITE}" 2>/dev/null || echo "")
        if [ -n "${old_pid}" ] && kill -0 "${old_pid}" 2>/dev/null; then
            log_info "停止旧的 Vite 进程 (PID: ${old_pid})"
            kill -TERM "${old_pid}" 2>/dev/null || true
            sleep 1
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
}

# 停止旧进程
stop_dev_processes

# 检查 webui/node_modules 是否存在，如果不存在则安装依赖
if [ ! -d "${SCRIPT_DIR}/webui/node_modules" ]; then
    log_warn "检测到 webui 目录缺少依赖，正在安装..."
    cd "${SCRIPT_DIR}/webui"
    pnpm install
    cd "${SCRIPT_DIR}"
fi

# 清理函数：当脚本退出时清理后台进程
cleanup() {
    echo ""
    log_warn "正在停止开发服务器..."
    
    # 停止 Go 进程
    if [ -n "$GO_PID" ] && kill -0 $GO_PID 2>/dev/null; then
        kill -TERM $GO_PID 2>/dev/null || true
    fi
    
    # 停止 Vite 进程
    if [ -n "$VITE_PID" ] && kill -0 $VITE_PID 2>/dev/null; then
        kill -TERM $VITE_PID 2>/dev/null || true
    fi
    
    # 等待进程退出
    wait $GO_PID $VITE_PID 2>/dev/null || true
    
    # 清理 PID 文件
    rm -f "${PID_FILE_GO}" "${PID_FILE_VITE}"
    
    log_info "开发服务器已停止"
    exit 0
}

# 注册清理函数
trap cleanup SIGINT SIGTERM EXIT

# 启动 Go 后端
log_info "启动 Go 后端服务器 (端口 ${GO_PORT})..."
cd "${SCRIPT_DIR}"
# 需要同时编译 main.go 和 symbol_manager.go
go run main.go symbol_manager.go &
GO_PID=$!
echo "${GO_PID}" > "${PID_FILE_GO}"

# 等待 Go 后端启动
sleep 2

# 检查 Go 后端是否成功启动
if ! kill -0 $GO_PID 2>/dev/null; then
    log_error "Go 后端启动失败"
    rm -f "${PID_FILE_GO}"
    exit 1
fi
log_info "Go 后端已启动 (PID: ${GO_PID})"

# 启动 Vite 前端开发服务器
log_info "启动 Vite 前端开发服务器 (端口 ${VITE_PORT})..."
cd "${SCRIPT_DIR}/webui"
pnpm dev &
VITE_PID=$!
echo "${VITE_PID}" > "${PID_FILE_VITE}"
cd "${SCRIPT_DIR}"

# 等待 Vite 启动
sleep 3

# 检查 Vite 是否成功启动
if ! kill -0 $VITE_PID 2>/dev/null; then
    log_error "Vite 前端开发服务器启动失败"
    kill $GO_PID 2>/dev/null || true
    rm -f "${PID_FILE_GO}" "${PID_FILE_VITE}"
    exit 1
fi
log_info "Vite 前端已启动 (PID: ${VITE_PID})"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  开发服务器启动成功！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}前端开发服务器:${NC} http://localhost:${VITE_PORT}"
echo -e "${BLUE}后端 API 服务器:${NC} http://localhost:${GO_PORT}"
echo ""
echo -e "${YELLOW}进程信息:${NC}"
echo -e "  Go 后端 PID: ${GO_PID}"
echo -e "  Vite 前端 PID: ${VITE_PID}"
echo ""
echo -e "${YELLOW}提示:${NC}"
echo -e "  - 前端代码修改会自动热重载 (Hot Reload)"
echo -e "  - 后端代码修改需要重启 Go 服务器"
echo -e "  - 按 Ctrl+C 停止所有服务器"
echo -e "  - 使用 ./restart.sh --dev 重启开发服务器"
echo ""

# 等待进程
wait $GO_PID $VITE_PID
