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
#   ./scripts/local/restart.sh                     # 默認開發模式（Go + Vite 熱更新）
#   ./scripts/local/restart.sh --prod [config.yaml]  # 生產模式（構建後端 + webui/dist）
#   ./scripts/local/restart.sh -d                  # 等同默認，明確指定開發模式

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOCAL_SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

# ---------- 生产模式：只识别本仓库的二进制 ----------
# 旧实现用 `pgrep -f quantmesh` 并对端口占用者直接 kill -9，会误杀编辑器、其他 checkout、
# 临时目录里的试跑二进制等所有命令行含 quantmesh 的进程。现在只处理「可执行文件就是本仓库
# ${SCRIPT_DIR}/${BINARY_NAME}」的进程，并与 stop.sh --dev 一样 SIGINT → 等待 → SIGTERM → SIGKILL。

PROD_BIN="${SCRIPT_DIR}/${BINARY_NAME}"
# 优雅退出等待秒数（后端退出时会撤单/平仓/落库，给足时间）
PROD_STOP_TIMEOUT="${PROD_STOP_TIMEOUT:-30}"
# SIGTERM 之后到 SIGKILL 的等待秒数
PROD_STOP_TERM_WAIT=3

# 进程识别与优雅停止函数（pid_alive / is_this_checkout_binary / find_prod_pids /
# graceful_stop_prod_pids / check_port_owner 等）与 start.sh、stop.sh 共用
# shellcheck source=lib/process.sh
source "${LOCAL_SCRIPTS_DIR}/lib/process.sh"

# 停止开发模式进程
stop_dev_processes() {
    log_info "停止开发模式进程..."
    # 复用 stop.sh --dev：按进程组/子进程/端口监听者优雅停止，避免 go-build 子进程成为孤儿
    bash "${SCRIPT_DIR}/scripts/local/stop.sh" --dev || log_warn "停止开发模式进程时出现错误"
}

# 停止生产模式进程
stop_prod_processes() {
    log_info "停止生产模式进程..."

    local pids
    pids=$(find_prod_pids | tr '\n' ' ')
    if [ -n "${pids// /}" ]; then
        # shellcheck disable=SC2086
        graceful_stop_prod_pids ${pids}
    else
        log_info "未发现本仓库的生产进程 (${PROD_BIN})"
    fi
    # PID 文件里的进程已退出或不是本仓库二进制（PID 被复用）时同样删除
    rm -f "${PID_FILE}"

    # 端口兜底：只处理本仓库二进制
    check_port_owner "${GO_PORT}" "Go 后端"
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
    
    # 检查本仓库 dev 二进制（与 stop.sh --dev 的匹配规则一致，带仓库绝对路径）
    if pgrep -f "^${SCRIPT_DIR}/.dev/quantmesh-dev" >/dev/null 2>&1; then
        return 0
    fi

    return 1
}

# 检查是否有本仓库的生产模式进程在运行（PID 文件或进程表中可执行文件为 ${PROD_BIN}）
has_prod_processes() {
    if [ -f "${PID_FILE}" ]; then
        # 残留 PID 文件也交给 stop_prod_processes 清理
        return 0
    fi
    [ -n "$(find_prod_pids)" ]
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
    exec "${SCRIPT_DIR}/scripts/local/dev.sh"
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
    exec "${SCRIPT_DIR}/scripts/local/start.sh" "${CONFIG_FILE}"
fi

