#!/bin/bash

# QuantMesh 开发模式启动脚本
# 同时启动 Go 后端和 Vite 前端开发服务器
#
# 端口规划：
#   - Go 后端：28888（API 和 WebSocket）
#   - Vite 前端：15173（开发服务器，代理 /api 和 /ws 到后端）
#
# 后端不再用 `go run .`：go run 会从 go-build 缓存派生真正的服务进程，
# 停止时只杀 go run 的 PID 会留下占着 28888 的孤儿。改为编译到 .dev/quantmesh-dev 后直接运行，
# PID 文件记录的就是服务进程本身，并以独立进程组启动，便于 stop.sh 整组优雅停止。

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

# 仓库根目录（本脚本位于 scripts/local/）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PID_FILE_GO="${SCRIPT_DIR}/.dev_go.pid"
PID_FILE_VITE="${SCRIPT_DIR}/.dev_vite.pid"
DEV_DIR="${SCRIPT_DIR}/.dev"
DEV_BIN="${DEV_DIR}/quantmesh-dev"
STOP_SCRIPT="${SCRIPT_DIR}/scripts/local/stop.sh"

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

# 检查 yarn 是否安装（项目 package.json 指定 packageManager: yarn）
if ! command -v yarn &> /dev/null; then
    log_error "未找到 yarn，请先安装 yarn"
    log_warn "安装命令: npm install -g yarn 或使用 corepack enable"
    exit 1
fi

# 检查 webui 目录是否存在
if [ ! -d "${SCRIPT_DIR}/webui" ]; then
    log_error "未找到 webui 目录"
    exit 1
fi

# 停止旧的开发进程：统一复用 stop.sh --dev（PID 文件 → 进程组/子进程 → 端口监听者，优雅退出后再强杀）
log_info "检查并停止旧的开发进程..."
bash "${STOP_SCRIPT}" --dev || log_warn "停止旧开发进程时出现错误，继续启动"

# 检查 webui/node_modules 是否存在，如果不存在则安装依赖
if [ ! -d "${SCRIPT_DIR}/webui/node_modules" ]; then
    log_warn "检测到 webui 目录缺少依赖，正在安装..."
    cd "${SCRIPT_DIR}/webui"
    yarn install
    cd "${SCRIPT_DIR}"
fi

# 发信号给以独立进程组启动的子进程（整组），失败时退回单进程
signal_group() {
    local sig=$1
    local pid=$2
    if [ -z "${pid}" ]; then
        return
    fi
    kill -"${sig}" -- "-${pid}" 2>/dev/null || kill -"${sig}" "${pid}" 2>/dev/null || true
}

# 清理函数：当脚本退出时清理后台进程
CLEANED_UP=false
cleanup() {
    if [ "${CLEANED_UP}" = true ]; then
        return
    fi
    CLEANED_UP=true
    echo ""
    log_warn "正在停止开发服务器..."

    # 后台任务在独立进程组中，终端的 Ctrl+C 不会直接送达，这里显式转发 SIGINT 让后端走优雅退出
    if [ -n "$GO_PID" ] && kill -0 "$GO_PID" 2>/dev/null; then
        signal_group INT "$GO_PID"
    fi
    if [ -n "$VITE_PID" ] && kill -0 "$VITE_PID" 2>/dev/null; then
        signal_group TERM "$VITE_PID"
    fi

    # 等待进程退出
    wait $GO_PID $VITE_PID 2>/dev/null || true

    # 清理 PID 文件
    rm -f "${PID_FILE_GO}" "${PID_FILE_VITE}"

    log_info "开发服务器已停止"
}

# 注册清理函数
trap 'cleanup; exit 0' SIGINT SIGTERM
trap cleanup EXIT

# 编译 Go 后端到固定路径（整个包：main、symbol_manager、bot_manager 等）
log_info "编译 Go 后端 -> ${DEV_BIN} ..."
cd "${SCRIPT_DIR}"
mkdir -p "${DEV_DIR}"
go build -o "${DEV_BIN}" .

# 开启作业控制：后台任务各自成为进程组组长（PGID == PID），stop.sh 可整组停止
set -m

# 启动 Go 后端
log_info "启动 Go 后端服务器 (端口 ${GO_PORT})..."
"${DEV_BIN}" &
GO_PID=$!
echo "${GO_PID}" > "${PID_FILE_GO}"

# 等待 Go 后端进程存在
sleep 2

# 检查 Go 后端进程是否还在运行
if ! kill -0 $GO_PID 2>/dev/null; then
    log_error "Go 后端启动失败"
    rm -f "${PID_FILE_GO}"
    exit 1
fi
log_info "Go 后端进程已启动 (PID: ${GO_PID})，等待 HTTP 服务就绪..."

# 轮询等待 Go HTTP 服务真正开始监听（避免前端代理 ECONNREFUSED）
# 后端初始化链较长（配置、存储、交易所等），最多等 60 秒
wait_for_backend() {
    local port=$1
    local max_attempts=60
    local attempt=1
    while [ $attempt -le $max_attempts ]; do
        if kill -0 $GO_PID 2>/dev/null; then
            if command -v curl >/dev/null 2>&1; then
                # 必須跳過 HTTP(S)_PROXY，否則本機地址會被送到代理，/api/version 得不到 200
                if curl -s -o /dev/null -w "%{http_code}" --connect-timeout 1 --noproxy '*' "http://127.0.0.1:${port}/api/version" 2>/dev/null | grep -q '200'; then
                    return 0
                fi
            elif command -v nc >/dev/null 2>&1; then
                if nc -z 127.0.0.1 ${port} 2>/dev/null; then
                    return 0
                fi
            fi
        else
            log_error "Go 后端进程已退出"
            return 1
        fi
        if [ $((attempt % 5)) -eq 0 ]; then
            log_info "等待 Go 后端 HTTP 服务... ${attempt}s"
        fi
        sleep 1
        attempt=$((attempt + 1))
    done
    return 1
}
if ! wait_for_backend ${GO_PORT}; then
    log_error "等待 Go 后端 HTTP 服务超时（约 60 秒），请检查后端日志或 config.yaml"
    exit 1
fi
log_info "Go 后端 HTTP 服务已就绪 (端口 ${GO_PORT})"

# 启动 Vite 前端开发服务器
log_info "启动 Vite 前端开发服务器 (端口 ${VITE_PORT})..."
cd "${SCRIPT_DIR}/webui"
yarn dev &
VITE_PID=$!
echo "${VITE_PID}" > "${PID_FILE_VITE}"
cd "${SCRIPT_DIR}"

# 等待 Vite 启动
sleep 3

# 检查 Vite 是否成功启动
if ! kill -0 $VITE_PID 2>/dev/null; then
    log_error "Vite 前端开发服务器启动失败"
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
echo -e "  Go 后端 PID: ${GO_PID} (${DEV_BIN})"
echo -e "  Vite 前端 PID: ${VITE_PID}"
echo ""
echo -e "${YELLOW}提示:${NC}"
echo -e "  - 前端代码修改会自动热重载 (Hot Reload)"
echo -e "  - 后端代码修改需要重启 Go 服务器"
echo -e "  - 按 Ctrl+C 停止所有服务器，或在其他终端执行 make dev-stop"
echo -e "  - 使用 ./scripts/local/restart.sh --dev 重启开发服务器"
echo ""

# 等待进程
wait $GO_PID $VITE_PID
