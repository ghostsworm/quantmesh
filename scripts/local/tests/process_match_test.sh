#!/bin/bash
# lib/process.sh 进程匹配 / 优雅停止的单元检查。
#
# 在临时目录里构造两个假 checkout（repoA、repoB）和一个「试跑」目录，把 /bin/sleep 复制成
# quantmesh 作为假二进制运行，断言只有 repoA 的二进制被识别；并验证 SIGINT → SIGTERM 升级、
# 外部进程占用端口时只报告不杀、停止函数幂等。
# 只会向本脚本自己启动的进程发信号，退出时（含失败）全部清理。
#
# 用法：bash scripts/local/tests/process_match_test.sh

set -u

LIB="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/process.sh"

FAILS=0
PASSES=0

pass() { PASSES=$((PASSES + 1)); echo "  PASS: $1"; }
fail() { FAILS=$((FAILS + 1)); echo "  FAIL: $1"; }

assert_eq() {
    local desc=$1 want=$2 got=$3
    if [ "${want}" = "${got}" ]; then
        pass "${desc}"
    else
        fail "${desc} (want='${want}' got='${got}')"
    fi
}

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/qm-process-test.XXXXXX")"
TMP_ROOT="$(cd "${TMP_ROOT}" && pwd -P)"

# 启动过的 PID 记到文件里：spawn 常在 $(...) 子 shell 中调用，变量无法回传
STARTED_FILE="${TMP_ROOT}/.started_pids"
: > "${STARTED_FILE}"

cleanup() {
    local p
    for p in $(cat "${STARTED_FILE}" 2>/dev/null); do
        kill -KILL "${p}" 2>/dev/null || true
    done
    rm -rf "${TMP_ROOT}"
}
trap cleanup EXIT

# 能否运行（子 shell 内执行并显式 exit，避免父 shell 打印 "Killed: 9"）
can_run() {
    ( "$1" 0; exit $? ) >/dev/null 2>&1
}

# 准备可运行的假二进制：复制 /bin/sleep；macOS 上复制后的平台二进制签名失效，需 ad-hoc 重签
make_fake_binary() {
    local dst=$1
    mkdir -p "$(dirname "${dst}")"
    cp /bin/sleep "${dst}"
    if ! can_run "${dst}"; then
        if command -v codesign >/dev/null 2>&1; then
            codesign -s - -f "${dst}" >/dev/null 2>&1 || true
        fi
    fi
    if ! can_run "${dst}"; then
        echo "无法运行假二进制 ${dst}，跳过测试" >&2
        exit 2
    fi
}

# 在指定目录以独立（被 init 收养的）进程启动命令，输出 PID。
# int_mode=default 时 SIGINT 为默认动作；int_mode=ignore 时忽略 SIGINT（模拟迟迟不退出的后端）。
# 用 perl 显式设置信号处置再 exec，避免非交互 shell 的后台作业默认忽略 SIGINT。
spawn() {
    local dir=$1 int_mode=$2
    shift 2
    local pidfile="${TMP_ROOT}/.spawn.$$.${RANDOM}"
    (
        cd "${dir}" || exit 1
        if [ "${int_mode}" = ignore ]; then
            perl -e '$SIG{INT}="IGNORE"; exec @ARGV or die' "$@" </dev/null >/dev/null 2>&1 &
        else
            perl -e '$SIG{INT}="DEFAULT"; exec @ARGV or die' "$@" </dev/null >/dev/null 2>&1 &
        fi
        echo $! > "${pidfile}"
    )
    local pid
    pid=$(cat "${pidfile}")
    rm -f "${pidfile}"
    echo "${pid}" >> "${STARTED_FILE}"
    echo "${pid}"
}

# 等待进程 exec 完成（args 以期望前缀开头）
wait_exec() {
    local pid=$1 prefix=$2 i
    for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
        case "$(ps -o args= -p "${pid}" 2>/dev/null)" in
            "${prefix}"*) return 0 ;;
        esac
        sleep 0.1
    done
    return 1
}

sorted() { tr ' ' '\n' | grep -v '^$' | sort -n | tr '\n' ' ' | sed 's/ $//'; }

REPO_A="${TMP_ROOT}/repoA"
REPO_B="${TMP_ROOT}/repoB"
TRIAL="${TMP_ROOT}/scratchpad/trial"
make_fake_binary "${REPO_A}/quantmesh"
make_fake_binary "${REPO_B}/quantmesh"
make_fake_binary "${TRIAL}/quantmesh"
ln -s "${REPO_A}" "${TMP_ROOT}/repoA-link"

echo "== 启动假进程"
A_REL=$(spawn "${REPO_A}" default ./quantmesh 300)            # repoA，相对路径启动 → 应匹配
A_ABS=$(spawn "${TMP_ROOT}" default "${REPO_A}/quantmesh" 300) # repoA，绝对路径、cwd 不在仓库 → 应匹配
B_REL=$(spawn "${REPO_B}" default ./quantmesh 300)            # 其他 checkout → 不匹配
TRIAL_P=$(spawn "${TRIAL}" default "${TRIAL}/quantmesh" 300)   # 临时目录试跑 → 不匹配
DECOY=$(spawn "${REPO_A}" default perl -e 'sleep 300' quantmesh ./quantmesh) # 命令行含 quantmesh 的其他程序 → 不匹配
wait_exec "${A_REL}" "./quantmesh" && wait_exec "${A_ABS}" "${REPO_A}/quantmesh" \
    && wait_exec "${B_REL}" "./quantmesh" && wait_exec "${TRIAL_P}" "${TRIAL}/quantmesh" \
    && wait_exec "${DECOY}" "perl" || { echo "假进程未能启动"; exit 1; }
echo "  repoA: ${A_REL} ${A_ABS}; repoB: ${B_REL}; trial: ${TRIAL_P}; decoy: ${DECOY}"

# 每个用例在子 shell 里 source 库，隔离 SCRIPT_DIR 等全局变量
run_lib() {
    local script_dir=$1
    shift
    (
        log_info() { :; }
        log_warn() { :; }
        log_error() { :; }
        SCRIPT_DIR="${script_dir}"
        BINARY_NAME="quantmesh"
        PID_FILE="${script_dir}/.quantmesh.pid"
        PROD_STOP_TIMEOUT="${T_PROD_STOP_TIMEOUT:-30}"
        PROD_STOP_TERM_WAIT="${T_PROD_STOP_TERM_WAIT:-3}"
        # shellcheck source=../lib/process.sh
        source "${LIB}"
        "$@"
    )
}

echo "== is_this_checkout_binary（可执行文件路径匹配）"
for pid in "${A_REL}" "${A_ABS}"; do
    if run_lib "${REPO_A}" is_this_checkout_binary "${pid}"; then pass "repoA 进程 ${pid} 匹配"; else fail "repoA 进程 ${pid} 应匹配"; fi
done
for pid in "${B_REL}" "${TRIAL_P}" "${DECOY}"; do
    if run_lib "${REPO_A}" is_this_checkout_binary "${pid}"; then fail "进程 ${pid} 不应匹配 repoA"; else pass "进程 ${pid} 不匹配 repoA"; fi
done
if run_lib "${REPO_B}" is_this_checkout_binary "${A_REL}"; then fail "repoA 进程不应匹配 repoB"; else pass "repoA 进程不匹配 repoB"; fi
if run_lib "${REPO_A}" is_this_checkout_binary 99999999; then fail "不存在的 PID 不应匹配"; else pass "不存在的 PID 不匹配"; fi

echo "== find_prod_pids"
assert_eq "repoA 只找到自己的两个进程" "$(echo "${A_REL} ${A_ABS}" | sorted)" "$(run_lib "${REPO_A}" find_prod_pids | tr '\n' ' ' | sorted)"
assert_eq "经符号链接的 SCRIPT_DIR 结果相同" "$(echo "${A_REL} ${A_ABS}" | sorted)" "$(run_lib "${TMP_ROOT}/repoA-link" find_prod_pids | tr '\n' ' ' | sorted)"
assert_eq "repoB 只找到自己的进程" "${B_REL}" "$(run_lib "${REPO_B}" find_prod_pids | tr '\n' ' ' | sorted)"

# PID 文件指向其他 checkout 的进程 / 垃圾内容：不得被选中
echo "${TRIAL_P}" > "${REPO_A}/.quantmesh.pid"
assert_eq "PID 文件指向外部进程时被忽略" "$(echo "${A_REL} ${A_ABS}" | sorted)" "$(run_lib "${REPO_A}" find_prod_pids | tr '\n' ' ' | sorted)"
printf 'abc\n%s\n' "${DECOY}" > "${REPO_B}/.quantmesh.pid"
assert_eq "PID 文件含垃圾/外部 PID 时被忽略" "${B_REL}" "$(run_lib "${REPO_B}" find_prod_pids | tr '\n' ' ' | sorted)"
rm -f "${REPO_A}/.quantmesh.pid" "${REPO_B}/.quantmesh.pid"

echo "== 回退匹配（取不到可执行文件路径时：命令行 + 工作目录）"
fallback_check() {
    pid_exe_path() { :; }
    is_this_checkout_binary "$1"
}
if run_lib "${REPO_A}" fallback_check "${A_REL}"; then pass "回退：repoA ./quantmesh 匹配"; else fail "回退：repoA ./quantmesh 应匹配"; fi
# 回退模式偏保守：绝对路径启动但工作目录不在仓库时不认（宁可漏停也不误杀）
if run_lib "${REPO_A}" fallback_check "${A_ABS}"; then fail "回退：工作目录不在仓库时不应匹配"; else pass "回退：绝对路径启动但工作目录不在仓库时不匹配（保守）"; fi
if run_lib "${REPO_A}" fallback_check "${B_REL}"; then fail "回退：repoB ./quantmesh 不应匹配 repoA"; else pass "回退：repoB ./quantmesh 不匹配 repoA"; fi
if run_lib "${REPO_A}" fallback_check "${TRIAL_P}"; then fail "回退：试跑进程不应匹配"; else pass "回退：试跑进程不匹配"; fi

echo "== check_port_owner：外部进程占用端口只报告不杀"
PORT=""
for candidate in 47311 47312 47313 47314 47315 47316 47317 47318; do
    if ! lsof -nP -tiTCP:"${candidate}" -sTCP:LISTEN >/dev/null 2>&1; then
        PORT=${candidate}
        break
    fi
done
if [ -z "${PORT}" ] || ! command -v lsof >/dev/null 2>&1; then
    echo "  SKIP: 无可用测试端口或无 lsof"
else
    LISTENER=$(spawn "${TMP_ROOT}" default perl -MIO::Socket::INET -e \
        '$s=IO::Socket::INET->new(Listen=>1,LocalAddr=>"127.0.0.1",LocalPort=>$ARGV[0],ReuseAddr=>1) or die; sleep 300' "${PORT}")
    for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
        lsof -nP -tiTCP:"${PORT}" -sTCP:LISTEN >/dev/null 2>&1 && break
        sleep 0.1
    done
    port_others() {
        check_port_owner "$1" "test"
        echo "${PORT_OWNER_OTHERS}"
    }
    assert_eq "外部监听者被报告" "${LISTENER}" "$(run_lib "${REPO_A}" port_others "${PORT}")"
    if kill -0 "${LISTENER}" 2>/dev/null; then pass "外部监听者未被停止"; else fail "外部监听者被误杀"; fi
    kill -KILL "${LISTENER}" 2>/dev/null || true
fi

echo "== 停止：SIGINT 即退出 / 忽略 SIGINT 时升级到 SIGTERM；其他进程不受影响"
A_STUBBORN=$(spawn "${REPO_A}" ignore ./quantmesh 300)
wait_exec "${A_STUBBORN}" "./quantmesh" || fail "忽略 SIGINT 的假进程未启动"
start_ts=$(date +%s)
T_PROD_STOP_TIMEOUT=2 T_PROD_STOP_TERM_WAIT=2 run_lib "${REPO_A}" stop_this_checkout_prod
rc=$?
elapsed=$(( $(date +%s) - start_ts ))
assert_eq "stop_this_checkout_prod 找到进程时返回 0" "0" "${rc}"
for pid in "${A_REL}" "${A_ABS}" "${A_STUBBORN}"; do
    if kill -0 "${pid}" 2>/dev/null; then fail "repoA 进程 ${pid} 应已停止"; else pass "repoA 进程 ${pid} 已停止"; fi
done
if [ "${elapsed}" -ge 2 ]; then pass "忽略 SIGINT 的进程等满超时后才升级 (${elapsed}s)"; else fail "未等待 SIGINT 超时 (${elapsed}s)"; fi
for pid in "${B_REL}" "${TRIAL_P}" "${DECOY}"; do
    if kill -0 "${pid}" 2>/dev/null; then pass "非 repoA 进程 ${pid} 仍在运行"; else fail "非 repoA 进程 ${pid} 被误杀"; fi
done

echo "== 幂等：再次停止无事可做"
T_PROD_STOP_TIMEOUT=2 run_lib "${REPO_A}" stop_this_checkout_prod
assert_eq "无进程时 stop_this_checkout_prod 返回 1" "1" "$?"
assert_eq "无进程时 find_prod_pids 为空" "" "$(run_lib "${REPO_A}" find_prod_pids | tr '\n' ' ' | sorted)"

echo ""
echo "结果: ${PASSES} 通过, ${FAILS} 失败"
[ "${FAILS}" -eq 0 ]
