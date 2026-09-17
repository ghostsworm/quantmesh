#!/bin/bash
# shellcheck shell=bash
#
# QuantMesh 本地脚本共用的进程识别 / 优雅停止函数。
# 由 scripts/local/{start,stop,restart}.sh 通过相对脚本目录的路径 source：
#   source "$(dirname "${BASH_SOURCE[0]}")/lib/process.sh"
#
# 调用方在 source 之前应设置：
#   SCRIPT_DIR   仓库根目录（绝对路径）
#   BINARY_NAME  生产二进制文件名（默认 quantmesh）
#   PID_FILE     生产 PID 文件（默认 ${SCRIPT_DIR}/.quantmesh.pid）
# 可选：
#   PROD_BIN              本仓库生产二进制绝对路径（默认 ${SCRIPT_DIR}/${BINARY_NAME}）
#   PROD_STOP_TIMEOUT     生产进程 SIGINT 后等待秒数（默认 30）
#   PROD_STOP_TERM_WAIT   SIGTERM 后到 SIGKILL 的等待秒数（默认 3）
#   DEV_STOP_TIMEOUT / DEV_STOP_TERM_WAIT  开发进程同上（默认 20 / 3）
#
# 原则：只停止「可执行文件就是本仓库二进制」的进程；绝不按裸进程名 pgrep -f 后直接 kill；
# 停止顺序一律 SIGINT → 等待 → SIGTERM → 等待 → SIGKILL（后端退出时要撤单/平仓/落库）。

BINARY_NAME="${BINARY_NAME:-quantmesh}"
PID_FILE="${PID_FILE:-${SCRIPT_DIR}/.${BINARY_NAME}.pid}"
PROD_BIN="${PROD_BIN:-${SCRIPT_DIR}/${BINARY_NAME}}"
PROD_STOP_TIMEOUT="${PROD_STOP_TIMEOUT:-30}"
PROD_STOP_TERM_WAIT="${PROD_STOP_TERM_WAIT:-3}"
DEV_STOP_TIMEOUT="${DEV_STOP_TIMEOUT:-20}"
DEV_STOP_TERM_WAIT="${DEV_STOP_TERM_WAIT:-3}"

# 调用方未定义日志函数时提供最简实现
if ! declare -F log_info >/dev/null 2>&1; then
    log_info() { echo -e "[INFO] $1"; }
fi
if ! declare -F log_warn >/dev/null 2>&1; then
    log_warn() { echo -e "[WARN] $1"; }
fi
if ! declare -F log_error >/dev/null 2>&1; then
    log_error() { echo -e "[ERROR] $1"; }
fi

pid_alive() {
    [ -n "$1" ] && kill -0 "$1" 2>/dev/null
}

# 输出进程的可执行文件绝对路径（Linux 读 /proc，macOS 用 lsof 的 txt 首项）；取不到时输出空
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

# 输出进程的工作目录；取不到时输出空
pid_cwd() {
    local pid=$1
    if [ -e "/proc/${pid}/cwd" ]; then
        readlink "/proc/${pid}/cwd" 2>/dev/null || true
        return
    fi
    if command -v lsof >/dev/null 2>&1; then
        lsof -a -p "${pid}" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1 || true
    fi
}

# 规范化路径（解析符号链接，如 macOS 的 /tmp -> /private/tmp）；目录或文件不存在时原样输出
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

# 判断 pid 是否是本仓库的生产二进制：
#   1) 可执行文件路径 == ${PROD_BIN}（替换过二进制时 Linux 带 " (deleted)"，已去掉）；
#   2) 取不到可执行文件路径时，退回「命令行以 ./quantmesh 或 ${PROD_BIN} 开头且工作目录是本仓库」。
is_this_checkout_binary() {
    local pid=$1
    pid_alive "${pid}" || return 1
    local want exe
    want=$(canonical_path "${PROD_BIN}")
    exe=$(pid_exe_path "${pid}")
    if [ -n "${exe}" ]; then
        [ "$(canonical_path "${exe}")" = "${want}" ]
        return
    fi
    local args cwd
    args=$(ps -o args= -p "${pid}" 2>/dev/null || true)
    case "${args}" in
        "./${BINARY_NAME}"|"./${BINARY_NAME} "*|"${PROD_BIN}"|"${PROD_BIN} "*) ;;
        *) return 1 ;;
    esac
    cwd=$(pid_cwd "${pid}")
    [ -n "${cwd}" ] && [ "$(cd "${cwd}" 2>/dev/null && pwd -P)" = "$(cd "${SCRIPT_DIR}" && pwd -P)" ]
}

# 列出本仓库生产二进制的 PID（PID 文件 + 进程表），去重。
# 候选只取 PID 文件和「进程名恰好等于 ${BINARY_NAME}」的进程（pgrep -x，不用 -f 匹配命令行），
# 再逐个核对可执行文件路径；本仓库二进制的进程名必然是 ${BINARY_NAME}，所以不会漏。
find_prod_pids() {
    local candidates="" pid
    if [ -f "${PID_FILE}" ]; then
        candidates="$(cat "${PID_FILE}" 2>/dev/null || true)"
    fi
    candidates="${candidates} $(pgrep -x "${BINARY_NAME}" 2>/dev/null | tr '\n' ' ' || true)"
    local seen=" "
    for pid in ${candidates}; do
        case "${pid}" in
            ''|*[!0-9]*) continue ;;
        esac
        case "${seen}" in
            *" ${pid} "*) continue ;;
        esac
        seen="${seen}${pid} "
        if [ "${pid}" != "$$" ] && is_this_checkout_binary "${pid}"; then
            echo "${pid}"
        fi
    done
}

# 等待给定 pid 全部退出；超时返回 1
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
        if [ "${alive}" = false ]; then
            return 0
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

# 优雅停止本仓库生产进程：SIGINT → 等待 PROD_STOP_TIMEOUT → SIGTERM → 等待 → SIGKILL
graceful_stop_prod_pids() {
    [ "$#" -gt 0 ] || return 0
    log_info "停止生产进程 (PID: $*)，发送 SIGINT，最多等待 ${PROD_STOP_TIMEOUT}s..."
    kill -INT "$@" 2>/dev/null || true
    if wait_pids_exit "${PROD_STOP_TIMEOUT}" "$@"; then
        return 0
    fi
    log_warn "生产进程未在 ${PROD_STOP_TIMEOUT}s 内退出，发送 SIGTERM..."
    kill -TERM "$@" 2>/dev/null || true
    if wait_pids_exit "${PROD_STOP_TERM_WAIT}" "$@"; then
        return 0
    fi
    log_warn "生产进程仍未退出，发送 SIGKILL"
    kill -KILL "$@" 2>/dev/null || true
}

# 检查端口：只停止本仓库二进制；被其他进程占用时只提示，不杀。
# 始终返回 0；非本仓库占用者的 PID 写入全局 PORT_OWNER_OTHERS（空表示没有），
# 需要「被外部进程占用即失败」的调用方自行检查该变量。
PORT_OWNER_OTHERS=""
check_port_owner() {
    local port=$1
    local name=$2
    PORT_OWNER_OTHERS=""
    command -v lsof >/dev/null 2>&1 || return 0
    local pids pid ours="" others=""
    pids=$(lsof -nP -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null | sort -u | tr '\n' ' ' || true)
    for pid in ${pids}; do
        if is_this_checkout_binary "${pid}"; then
            ours="${ours} ${pid}"
        else
            others="${others} ${pid}"
        fi
    done
    if [ -n "${ours}" ]; then
        # shellcheck disable=SC2086
        graceful_stop_prod_pids ${ours}
    fi
    if [ -n "${others}" ]; then
        PORT_OWNER_OTHERS="${others# }"
        log_warn "端口 ${port} (${name}) 被非本仓库进程占用 (PID:${others})，未自动停止，请手动确认后处理"
    fi
    return 0
}

# 停止本仓库生产进程（PID 文件 + 进程表），清理 PID 文件。找到并停止过进程返回 0，否则返回 1。
stop_this_checkout_prod() {
    local pids
    pids=$(find_prod_pids | tr '\n' ' ')
    rm -f "${PID_FILE}"
    if [ -n "${pids// /}" ]; then
        # shellcheck disable=SC2086
        graceful_stop_prod_pids ${pids}
        return 0
    fi
    return 1
}

# ---------- 开发模式：优雅停止整棵进程树 ----------

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

# 输出自己及所有祖先进程 PID（空格分隔，首尾带空格）
self_and_ancestor_pids() {
    local ancestors=" "
    local a=$$
    while [ -n "${a}" ] && [ "${a}" != "0" ] && [ "${a}" != "1" ]; do
        ancestors="${ancestors}${a} "
        a=$(ps -o ppid= -p "${a}" 2>/dev/null | tr -d ' ' || true)
    done
    echo "${ancestors}"
}

# 按命令行模式停止遗留进程。模式必须足够具体（带仓库绝对路径或 go-build 路径），不得是裸进程名。
stop_by_pattern() {
    local name=$1
    local pattern=$2
    local pids
    pids=$(pgrep -f "${pattern}" 2>/dev/null | tr '\n' ' ' || true)
    # 不要停掉自己及祖先进程（调用方 shell、dev.sh、restart.sh）
    local ancestors
    ancestors=$(self_and_ancestor_pids)
    local stopped=1
    local pid
    for pid in ${pids}; do
        case " ${ancestors} " in
            *" ${pid} "*) continue ;;
        esac
        if graceful_stop_pid "${name}" "${pid}"; then
            stopped=0
        fi
    done
    return ${stopped}
}
