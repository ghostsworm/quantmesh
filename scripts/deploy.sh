#!/bin/bash

# QuantMesh Market Maker 部署脚本
# 功能：
# 1. 在线编译（在服务器上编译，避免跨平台编译问题）
# 2. 保护数据库文件（不会覆盖现有数据库）
# 3. 支持 systemd 或 supervisor 服务管理
# 4. 自动备份数据库

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_NAME="quantmesh"
BINARY_NAME="quantmesh"
SERVICE_NAME="${APP_NAME}"
BACKUP_DIR="${SCRIPT_DIR}/backups"
DATA_DIR="${SCRIPT_DIR}/data"
LOG_DIR="${SCRIPT_DIR}/logs"
PID_FILE="${PID_FILE:-${SCRIPT_DIR}/.${BINARY_NAME}.pid}"
STOP_TIMEOUT="${STOP_TIMEOUT:-30}"     # SIGINT 后等待秒数
STOP_TERM_WAIT="${STOP_TERM_WAIT:-3}"  # SIGTERM 后到 SIGKILL 的等待秒数

# 日志函数
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# 检查命令是否存在
check_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        log_error "未找到命令: $1"
        return 1
    fi
    return 0
}

# 备份数据库
backup_database() {
    log_step "备份数据库..."
    
    # 创建备份目录
    mkdir -p "${BACKUP_DIR}"
    
    # 备份所有数据库文件
    local backup_timestamp=$(date +%Y%m%d_%H%M%S)
    local backup_path="${BACKUP_DIR}/db_backup_${backup_timestamp}"
    
    mkdir -p "${backup_path}"
    
    # 备份主数据库
    if [ -f "${DATA_DIR}/quantmesh.db" ]; then
        cp "${DATA_DIR}/quantmesh.db" "${backup_path}/quantmesh.db"
        log_info "已备份: quantmesh.db"
    fi
    
    # 备份日志数据库
    if [ -f "${DATA_DIR}/logs.db" ]; then
        cp "${DATA_DIR}/logs.db" "${backup_path}/logs.db"
        log_info "已备份: logs.db"
    fi
    
    # 备份认证数据库
    if [ -f "${DATA_DIR}/auth.db" ]; then
        cp "${DATA_DIR}/auth.db" "${backup_path}/auth.db"
        log_info "已备份: auth.db"
    fi
    
    # 备份 WebAuthn 数据库
    if [ -f "${DATA_DIR}/webauthn.db" ]; then
        cp "${DATA_DIR}/webauthn.db" "${backup_path}/webauthn.db"
        log_info "已备份: webauthn.db"
    fi
    
    # 备份配置文件
    if [ -f "${SCRIPT_DIR}/config.yaml" ]; then
        cp "${SCRIPT_DIR}/config.yaml" "${backup_path}/config.yaml"
        log_info "已备份: config.yaml"
    fi
    
    log_info "✅ 数据库备份完成: ${backup_path}"
    
    # 清理旧备份（保留最近7天）
    find "${BACKUP_DIR}" -type d -name "db_backup_*" -mtime +7 -exec rm -rf {} \; 2>/dev/null || true
}

# 停止服务（如果正在运行）
stop_service() {
    log_step "停止服务..."
    
    # 检查 systemd
    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        log_info "停止 systemd 服务..."
        sudo systemctl stop "${SERVICE_NAME}" || true
        sleep 2
    fi
    
    # 检查 supervisor
    if command -v supervisorctl >/dev/null 2>&1; then
        if supervisorctl status "${SERVICE_NAME}" >/dev/null 2>&1; then
            log_info "停止 supervisor 服务..."
            supervisorctl stop "${SERVICE_NAME}" || true
            sleep 2
        fi
    fi
    
    # 检查直接运行的进程：只停止可执行文件就是本次部署二进制（${SCRIPT_DIR}/${BINARY_NAME}
    # 或 --target 路径）的进程，绝不按裸进程名 pgrep -f 后直接 kill（会误杀同机其他实例、
    # 编辑器/日志 tail 等命令行里含 quantmesh 的进程）。
    local pids
    pids=$(find_deployed_pids "$@" | tr '\n' ' ')
    if [ -n "${pids// /}" ]; then
        # shellcheck disable=SC2086
        graceful_stop_pids ${pids}
    fi
    rm -f "${PID_FILE}"

    log_info "✅ 服务已停止"
}

# ---------- 进程识别（自包含：本脚本会被单独拷贝到服务器执行，不 source 仓库内其他文件）----------

pid_alive() {
    [ -n "$1" ] && kill -0 "$1" 2>/dev/null
}

# 输出进程可执行文件绝对路径（Linux 读 /proc，macOS 用 lsof txt 首项）；取不到输出空
pid_exe_path() {
    local pid=$1
    if [ -e "/proc/${pid}/exe" ]; then
        readlink "/proc/${pid}/exe" 2>/dev/null | sed 's/ (deleted)$//' || true
        return
    fi
    if command -v lsof >/dev/null 2>&1; then
        lsof -a -p "${pid}" -d txt -Fn 2>/dev/null | sed -n 's/^n//p' | head -1 || true
    fi
}

# 规范化路径（解析目录符号链接）；目录不存在时原样输出
canonical_path() {
    local p=$1
    local dir base
    dir=$(dirname "${p}")
    base=$(basename "${p}")
    if [ -d "${dir}" ]; then
        echo "$(cd "${dir}" && pwd -P)/${base}"
    else
        echo "${p}"
    fi
}

# 列出可执行文件是部署二进制的 PID。参数：额外的二进制路径（如 --target）。
# 候选 = PID 文件 + 进程名恰好等于 ${BINARY_NAME} 的进程（pgrep -x，不匹配命令行），再逐个核对可执行文件路径；
# 取不到可执行文件路径的进程不停止（宁可漏停由上方 systemd/supervisor 处理，也不误杀）。
find_deployed_pids() {
    local wants="" p
    for p in "${SCRIPT_DIR}/${BINARY_NAME}" "$@"; do
        [ -n "${p}" ] && wants="${wants}|$(canonical_path "${p}")|"
    done
    local candidates=""
    if [ -f "${PID_FILE}" ]; then
        candidates="$(cat "${PID_FILE}" 2>/dev/null || true)"
    fi
    candidates="${candidates} $(pgrep -x "${BINARY_NAME}" 2>/dev/null | tr '\n' ' ' || true)"
    local seen=" " pid exe
    for pid in ${candidates}; do
        case "${pid}" in
            ''|*[!0-9]*) continue ;;
        esac
        case "${seen}" in
            *" ${pid} "*) continue ;;
        esac
        seen="${seen}${pid} "
        [ "${pid}" != "$$" ] || continue
        pid_alive "${pid}" || continue
        exe=$(pid_exe_path "${pid}")
        if [ -z "${exe}" ]; then
            log_warn "无法确认 PID ${pid} 的可执行文件路径，跳过（请手动确认）"
            continue
        fi
        case "${wants}" in
            *"|$(canonical_path "${exe}")|"*) echo "${pid}" ;;
        esac
    done
}

# 等待 pid 全部退出；超时返回 1
wait_pids_exit() {
    local timeout=$1
    shift
    local elapsed=0 p alive
    while [ "${elapsed}" -lt "${timeout}" ]; do
        alive=false
        for p in "$@"; do
            if pid_alive "${p}"; then
                alive=true
                break
            fi
        done
        [ "${alive}" = false ] && return 0
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

# 优雅停止：SIGINT → 等待 STOP_TIMEOUT（退出时要撤单/平仓/落库）→ SIGTERM → 等待 → SIGKILL
graceful_stop_pids() {
    [ "$#" -gt 0 ] || return 0
    log_info "停止进程 (PID: $*)，发送 SIGINT，最多等待 ${STOP_TIMEOUT}s..."
    kill -INT "$@" 2>/dev/null || true
    wait_pids_exit "${STOP_TIMEOUT}" "$@" && return 0
    log_warn "进程未在 ${STOP_TIMEOUT}s 内退出，发送 SIGTERM..."
    kill -TERM "$@" 2>/dev/null || true
    wait_pids_exit "${STOP_TERM_WAIT}" "$@" && return 0
    log_warn "进程仍未退出，发送 SIGKILL"
    kill -KILL "$@" 2>/dev/null || true
}

# 在线编译
build_online() {
    log_step "在线编译项目..."
    
    # 检查 Go 环境
    if ! check_command "go"; then
        log_error "未找到 Go 编译器，请先安装 Go"
        exit 1
    fi
    
    # 检查 Go 版本
    local go_version=$(go version | awk '{print $3}')
    log_info "Go 版本: ${go_version}"
    
    # 设置代理（如果需要）
    if [ -n "${https_proxy}" ]; then
        export https_proxy="${https_proxy}"
        export http_proxy="${http_proxy}"
        log_info "使用代理: ${https_proxy}"
    fi
    
    # 构建前端
    if [ -d "${SCRIPT_DIR}/webui" ]; then
        log_info "构建前端..."
        cd "${SCRIPT_DIR}/webui"
        
        # 安装依赖（如果需要）
        if [ ! -d "node_modules" ]; then
            log_info "安装前端依赖..."
            if command -v yarn >/dev/null 2>&1; then
                yarn install
            elif command -v npm >/dev/null 2>&1; then
                npm install
            else
                log_error "未找到 yarn 或 npm"
                exit 1
            fi
        fi
        
        # 构建前端
        if command -v yarn >/dev/null 2>&1; then
            yarn build
        else
            npm run build
        fi
        
        if [ $? -ne 0 ]; then
            log_error "前端构建失败"
            exit 1
        fi
        
        # 复制到 web/dist
        if [ -d "${SCRIPT_DIR}/webui/dist" ]; then
            log_info "复制前端文件到 web/dist..."
            rm -rf "${SCRIPT_DIR}/web/dist"
            cp -r "${SCRIPT_DIR}/webui/dist" "${SCRIPT_DIR}/web/dist"
        fi
        
        log_info "✅ 前端构建成功"
    else
        log_warn "前端目录不存在，跳过前端构建"
    fi
    
    # 构建后端
    log_info "构建后端..."
    cd "${SCRIPT_DIR}"
    
    # 设置 CGO（SQLite 需要）
    export CGO_ENABLED=1
    
    # 编译
    go build -o "${BINARY_NAME}" .
    
    if [ $? -ne 0 ]; then
        log_error "后端构建失败"
        exit 1
    fi
    
    log_info "✅ 后端构建成功"
    
    # 检查二进制文件
    if [ ! -f "${SCRIPT_DIR}/${BINARY_NAME}" ]; then
        log_error "二进制文件不存在: ${BINARY_NAME}"
        exit 1
    fi
    
    # 显示二进制文件信息
    log_info "二进制文件信息:"
    ls -lh "${SCRIPT_DIR}/${BINARY_NAME}"
    file "${SCRIPT_DIR}/${BINARY_NAME}"
}

# 部署到目标位置（如果需要）
deploy_binary() {
    local target_path="${1:-}"
    
    if [ -z "${target_path}" ]; then
        log_info "未指定目标路径，跳过部署"
        return 0
    fi
    
    log_step "部署二进制文件到: ${target_path}"
    
    # 确保目标目录存在
    local target_dir=$(dirname "${target_path}")
    mkdir -p "${target_dir}"
    
    # 复制二进制文件
    cp "${SCRIPT_DIR}/${BINARY_NAME}" "${target_path}"
    chmod +x "${target_path}"
    
    log_info "✅ 已部署到: ${target_path}"
}

# 重启服务
restart_service() {
    log_step "重启服务..."
    
    # 检查 systemd
    if systemctl list-unit-files | grep -q "${SERVICE_NAME}.service"; then
        log_info "使用 systemd 重启服务..."
        sudo systemctl daemon-reload
        sudo systemctl restart "${SERVICE_NAME}"
        sleep 2
        if systemctl is-active --quiet "${SERVICE_NAME}"; then
            log_info "✅ systemd 服务已启动"
            log_info "查看状态: sudo systemctl status ${SERVICE_NAME}"
            log_info "查看日志: sudo journalctl -u ${SERVICE_NAME} -f"
        else
            log_error "❌ systemd 服务启动失败"
            sudo systemctl status "${SERVICE_NAME}"
            return 1
        fi
        return 0
    fi
    
    # 检查 supervisor
    if command -v supervisorctl >/dev/null 2>&1; then
        if supervisorctl status "${SERVICE_NAME}" >/dev/null 2>&1; then
            log_info "使用 supervisor 重启服务..."
            supervisorctl reread
            supervisorctl update
            supervisorctl restart "${SERVICE_NAME}"
            sleep 2
            if supervisorctl status "${SERVICE_NAME}" | grep -q "RUNNING"; then
                log_info "✅ supervisor 服务已启动"
                log_info "查看状态: supervisorctl status ${SERVICE_NAME}"
                log_info "查看日志: supervisorctl tail -f ${SERVICE_NAME}"
            else
                log_error "❌ supervisor 服务启动失败"
                supervisorctl status "${SERVICE_NAME}"
                return 1
            fi
            return 0
        fi
    fi
    
    log_warn "未找到 systemd 或 supervisor 服务配置，请手动启动服务"
    log_info "手动启动: ${SCRIPT_DIR}/${BINARY_NAME} config.yaml"
}

# 主流程
main() {
    log_info "=========================================="
    log_info "QuantMesh Market Maker 部署脚本"
    log_info "=========================================="
    log_info ""
    
    # 解析参数
    local TARGET_PATH=""
    local SKIP_BUILD=false
    local SKIP_BACKUP=false
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            --target)
                TARGET_PATH="$2"
                shift 2
                ;;
            --skip-build)
                SKIP_BUILD=true
                shift
                ;;
            --skip-backup)
                SKIP_BACKUP=true
                shift
                ;;
            -h|--help)
                echo "用法: $0 [选项]"
                echo ""
                echo "选项:"
                echo "  --target PATH      部署二进制文件到指定路径"
                echo "  --skip-build       跳过编译步骤"
                echo "  --skip-backup      跳过数据库备份"
                echo "  -h, --help         显示帮助信息"
                exit 0
                ;;
            *)
                log_error "未知参数: $1"
                exit 1
                ;;
        esac
    done
    
    # 1. 备份数据库
    if [ "$SKIP_BACKUP" = false ]; then
        backup_database
    else
        log_warn "跳过数据库备份"
    fi
    
    # 2. 停止服务（直接运行的进程只停可执行文件为本仓库二进制或 --target 路径的）
    stop_service "${TARGET_PATH}"
    
    # 3. 在线编译
    if [ "$SKIP_BUILD" = false ]; then
        build_online
    else
        log_warn "跳过编译步骤"
    fi
    
    # 4. 部署到目标位置（如果需要）
    if [ -n "${TARGET_PATH}" ]; then
        deploy_binary "${TARGET_PATH}"
    fi
    
    # 5. 重启服务
    restart_service
    
    log_info ""
    log_info "=========================================="
    log_info "✅ 部署完成！"
    log_info "=========================================="
}

# 执行主流程
main "$@"

