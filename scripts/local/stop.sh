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

# ---------- 开发模式：优雅停止整棵进程树 ----------
# dev.sh 把后端编译到固定路径并以独立进程组启动；旧版 `go run .` 会派生 go-build 缓存里的子进程，
# 只杀 go run 的 PID 会留下占着 28888 的孤儿。这里按「进程组 + 直接子进程 + 端口监听者」逐层兜底。

# 优雅退出等待秒数（后端退出时会撤单/落库，给足时间）
DEV_STOP_TIMEOUT="${DEV_STOP_TIMEOUT:-20}"
# SIGTERM 之后到 SIGKILL 的等待秒数
DEV_STOP_TERM_WAIT=3
DEV_BIN="${SCRIPT_DIR}/.dev/quantmesh-dev"

pid_alive() {
    [ -n "$1" ] && kill -0 "$1" 2>/dev/null
}

# 向 pid 及其直接子进程发信号；pid 是自己进程组的组长时连同整组一起发。
# 只在 pgid==pid 时才发组信号，避免误伤调用方所在的终端进程组。
signal_tree() {
    local sig=$1
    local pid=$2
    shift 2
    local pgid
    pgid=$(ps -o pgid= -p "${pid}" 2>/dev/null | tr -d ' ' || true)
    if [ -n "${pgid}" ] && [ "${pgid}" = "${pid}" ]; then
        kill -"${sig}" -- "-${pgid}" 2>/dev/null || true
    fi
    local p
    for p in "$@" "${pid}"; do
        kill -"${sig}" "${p}" 2>/dev/null || true
    done
}

# 等待给定 pid 全部退出；超时返回 1
wait_pids_exit() {
    local timeout=$1
    shift
    local elapsed=0
    local p alive
    while [ "${elapsed}" -lt "${timeout}" ]; do
        alive=false
        for p in "$@"; do
            if pid_alive "${p}"; then
                alive=true
                break
            fi
        done
        if [ "${alive}" = false ]; then
            return 0
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

# 优雅停止：SIGINT → 等待 → SIGTERM → 等待 → SIGKILL。进程不存在时返回 1。
graceful_stop_pid() {
    local name=$1
    local pid=$2
    if ! pid_alive "${pid}"; then
        return 1
    fi
    # 先记下子进程：父进程退出后子进程会被 PID 1 收养，pgrep -P 就查不到了
    local children
    children=$(pgrep -P "${pid}" 2>/dev/null | tr '\n' ' ' || true)
    log_info "停止 ${name} (PID: ${pid}${children:+，子进程: ${children}})，发送 SIGINT，最多等待 ${DEV_STOP_TIMEOUT}s..."
    # shellcheck disable=SC2086
    signal_tree INT "${pid}" ${children}
    # shellcheck disable=SC2086
    if wait_pids_exit "${DEV_STOP_TIMEOUT}" "${pid}" ${children}; then
        return 0
    fi
    log_warn "${name} 未在 ${DEV_STOP_TIMEOUT}s 内退出，发送 SIGTERM..."
    # shellcheck disable=SC2086
    signal_tree TERM "${pid}" ${children}
    # shellcheck disable=SC2086
    if wait_pids_exit "${DEV_STOP_TERM_WAIT}" "${pid}" ${children}; then
        return 0
    fi
    log_warn "${name} 仍未退出，发送 SIGKILL"
    # shellcheck disable=SC2086
    signal_tree KILL "${pid}" ${children}
    return 0
}

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

# 按命令行模式停止遗留进程
stop_by_pattern() {
    local name=$1
    local pattern=$2
    local pids
    pids=$(pgrep -f "${pattern}" 2>/dev/null | tr '\n' ' ' || true)
    # 不要停掉自己及祖先进程（调用方 shell、dev.sh、restart.sh）
    local ancestors=" "
    local a=$$
    while [ -n "${a}" ] && [ "${a}" != "0" ] && [ "${a}" != "1" ]; do
        ancestors="${ancestors}${a} "
        a=$(ps -o ppid= -p "${a}" 2>/dev/null | tr -d ' ' || true)
    done
    local stopped=1
    local pid
    for pid in ${pids}; do
        case "${ancestors}" in
            *" ${pid} "*) continue ;;
        esac
        if graceful_stop_pid "${name}" "${pid}"; then
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

