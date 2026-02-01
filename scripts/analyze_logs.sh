#!/bin/bash

# 日志分析工具 - 按级别过滤和分析日志
# 使用方法: ./scripts/analyze_logs.sh [level] [date] [options]
# 
# 参数:
#   level: ERROR, WARN, INFO, DEBUG (默认: ERROR)
#   date: 日期格式 YYYY-MM-DD (默认: 今天)
#   options: --tail N (显示最后N行), --follow (实时跟踪), --stats (显示统计)
#
# 示例:
#   ./scripts/analyze_logs.sh ERROR                    # 查看今天的所有错误
#   ./scripts/analyze_logs.sh WARN 2026-01-20          # 查看指定日期的警告
#   ./scripts/analyze_logs.sh ERROR --tail 50          # 查看最后50个错误
#   ./scripts/analyze_logs.sh ERROR --follow           # 实时跟踪错误日志
#   ./scripts/analyze_logs.sh --stats                  # 显示今天的日志统计

set -e

# 颜色定义
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 默认参数
LEVEL="ERROR"
DATE=$(date +%Y-%m-%d)
TAIL_LINES=""
FOLLOW=false
SHOW_STATS=false
LOG_DIR="./logs"

# 解析参数
while [[ $# -gt 0 ]]; do
    case $1 in
        ERROR|WARN|INFO|DEBUG)
            LEVEL="$1"
            shift
            ;;
        --tail)
            TAIL_LINES="$2"
            shift 2
            ;;
        --follow|-f)
            FOLLOW=true
            shift
            ;;
        --stats|-s)
            SHOW_STATS=true
            shift
            ;;
        --help|-h)
            echo "日志分析工具 - 按级别过滤和分析日志"
            echo ""
            echo "使用方法: $0 [level] [date] [options]"
            echo ""
            echo "参数:"
            echo "  level          日志级别: ERROR, WARN, INFO, DEBUG (默认: ERROR)"
            echo "  date           日期格式 YYYY-MM-DD (默认: 今天)"
            echo ""
            echo "选项:"
            echo "  --tail N       显示最后 N 行"
            echo "  --follow, -f   实时跟踪日志"
            echo "  --stats, -s    显示日志统计信息"
            echo "  --help, -h     显示帮助信息"
            echo ""
            echo "示例:"
            echo "  $0 ERROR                    # 查看今天的所有错误"
            echo "  $0 WARN 2026-01-20          # 查看指定日期的警告"
            echo "  $0 ERROR --tail 50          # 查看最后50个错误"
            echo "  $0 ERROR --follow           # 实时跟踪错误日志"
            echo "  $0 --stats                  # 显示今天的日志统计"
            exit 0
            ;;
        [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9])
            DATE="$1"
            shift
            ;;
        *)
            echo -e "${RED}错误: 未知参数 '$1'${NC}"
            echo "使用 --help 查看帮助信息"
            exit 1
            ;;
    esac
done

# 日志文件路径
APP_LOG="${LOG_DIR}/app-quantmesh-${DATE}.log"
WEB_LOG="${LOG_DIR}/web-gin-${DATE}.log"

# 检查日志文件是否存在
if [ ! -f "${APP_LOG}" ] && [ ! -f "${WEB_LOG}" ]; then
    echo -e "${RED}错误: 未找到 ${DATE} 的日志文件${NC}"
    echo ""
    echo "可用的日志日期:"
    ls -1 ${LOG_DIR}/app-quantmesh-*.log 2>/dev/null | sed 's/.*app-quantmesh-/  /' | sed 's/.log$//' | sort -r | head -10
    exit 1
fi

# 显示统计信息
show_stats() {
    echo -e "${CYAN}========================================${NC}"
    echo -e "${CYAN}日志统计 - ${DATE}${NC}"
    echo -e "${CYAN}========================================${NC}"
    echo ""
    
    for log_file in "${APP_LOG}" "${WEB_LOG}"; do
        if [ -f "${log_file}" ]; then
            log_name=$(basename "${log_file}")
            echo -e "${BLUE}📄 ${log_name}${NC}"
            echo ""
            
            # 统计各级别日志数量
            error_count=$(grep -c "\[ERROR\]" "${log_file}" 2>/dev/null || echo "0")
            warn_count=$(grep -c "\[WARN\]" "${log_file}" 2>/dev/null || echo "0")
            info_count=$(grep -c "\[INFO\]" "${log_file}" 2>/dev/null || echo "0")
            debug_count=$(grep -c "\[DEBUG\]" "${log_file}" 2>/dev/null || echo "0")
            
            echo -e "  ${RED}ERROR:${NC}  ${error_count}"
            echo -e "  ${YELLOW}WARN:${NC}   ${warn_count}"
            echo -e "  ${GREEN}INFO:${NC}   ${info_count}"
            echo -e "  ${CYAN}DEBUG:${NC}  ${debug_count}"
            echo ""
            
            # 显示文件大小
            file_size=$(du -h "${log_file}" | cut -f1)
            echo -e "  📦 文件大小: ${file_size}"
            echo ""
        fi
    done
    
    # 显示最近的错误（如果有）
    if [ -f "${APP_LOG}" ]; then
        echo -e "${RED}最近的错误 (最多10条):${NC}"
        echo ""
        grep "\[ERROR\]" "${APP_LOG}" 2>/dev/null | tail -10 | while read -r line; do
            echo -e "${RED}  • ${line}${NC}"
        done || echo "  无错误日志"
        echo ""
    fi
    
    echo -e "${CYAN}========================================${NC}"
}

# 根据级别设置颜色
get_color() {
    case $1 in
        ERROR) echo "${RED}" ;;
        WARN) echo "${YELLOW}" ;;
        INFO) echo "${GREEN}" ;;
        DEBUG) echo "${CYAN}" ;;
        *) echo "${NC}" ;;
    esac
}

# 过滤日志
filter_logs() {
    local log_file=$1
    local pattern="\[${LEVEL}\]"
    local color=$(get_color "${LEVEL}")
    
    if [ ! -f "${log_file}" ]; then
        return
    fi
    
    if [ "${FOLLOW}" = true ]; then
        # 实时跟踪模式
        echo -e "${BLUE}实时跟踪 ${LEVEL} 级别日志...${NC}"
        echo -e "${BLUE}按 Ctrl+C 退出${NC}"
        echo ""
        tail -f "${log_file}" | grep --line-buffered "${pattern}" | while read -r line; do
            echo -e "${color}${line}${NC}"
        done
    elif [ -n "${TAIL_LINES}" ]; then
        # 显示最后 N 行
        grep "${pattern}" "${log_file}" 2>/dev/null | tail -n "${TAIL_LINES}" | while read -r line; do
            echo -e "${color}${line}${NC}"
        done
    else
        # 显示所有匹配的行
        grep "${pattern}" "${log_file}" 2>/dev/null | while read -r line; do
            echo -e "${color}${line}${NC}"
        done
    fi
}

# 主逻辑
if [ "${SHOW_STATS}" = true ]; then
    show_stats
    exit 0
fi

# 显示标题
color=$(get_color "${LEVEL}")
echo -e "${CYAN}========================================${NC}"
echo -e "${color}${LEVEL} 级别日志 - ${DATE}${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""

# 处理应用日志
if [ -f "${APP_LOG}" ]; then
    echo -e "${BLUE}📄 应用日志 (app-quantmesh)${NC}"
    echo ""
    count=$(grep -c "\[${LEVEL}\]" "${APP_LOG}" 2>/dev/null || echo "0")
    echo -e "${color}找到 ${count} 条 ${LEVEL} 日志${NC}"
    echo ""
    filter_logs "${APP_LOG}"
    echo ""
fi

# 处理 Web 日志
if [ -f "${WEB_LOG}" ] && [ "${FOLLOW}" = false ]; then
    echo -e "${BLUE}📄 Web 日志 (web-gin)${NC}"
    echo ""
    count=$(grep -c "\[${LEVEL}\]" "${WEB_LOG}" 2>/dev/null || echo "0")
    echo -e "${color}找到 ${count} 条 ${LEVEL} 日志${NC}"
    echo ""
    filter_logs "${WEB_LOG}"
    echo ""
fi

echo -e "${CYAN}========================================${NC}"

# 如果是错误日志，提供一些常见问题的提示
if [ "${LEVEL}" = "ERROR" ] && [ "${FOLLOW}" = false ] && [ "${SHOW_STATS}" = false ]; then
    echo ""
    echo -e "${YELLOW}💡 提示:${NC}"
    echo "  • 使用 --follow 实时跟踪新的错误"
    echo "  • 使用 --stats 查看完整的日志统计"
    echo "  • 查看警告: $0 WARN ${DATE}"
    echo ""
fi
