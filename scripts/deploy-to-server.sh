#!/bin/bash

# QuantMesh Market Maker 完整部署脚本
# 功能：
# 1. 本地编译（或使用Docker编译）
# 2. 通过SSH部署到远程服务器
# 3. 自动备份数据库
# 4. 重启服务
# 5. 健康检查
#
# 使用方法：
#   ./scripts/deploy-to-server.sh
#   或
#   ./scripts/deploy-to-server.sh --config deploy-config.sh

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 脚本目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# 默认配置
DEPLOY_CONFIG="${SCRIPT_DIR}/deploy-config.sh"
REMOTE_HOST=""
REMOTE_USER=""
REMOTE_PORT="22"
REMOTE_PATH="/opt/quantmesh"
SSH_KEY=""
USE_DOCKER_BUILD=false
SKIP_BUILD=false
SKIP_BACKUP=false
SERVICE_NAME="quantmesh"
HEALTH_CHECK_URL="http://localhost:28888/api/status"

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

log_debug() {
    echo -e "${CYAN}[DEBUG]${NC} $1"
}

# 显示帮助信息
show_help() {
    cat << EOF
QuantMesh Market Maker 完整部署脚本

用法: $0 [选项]

选项:
  --config FILE        指定部署配置文件路径（默认: scripts/deploy-config.sh）
  --host HOST          远程服务器地址
  --user USER          远程服务器用户名
  --port PORT          SSH端口（默认: 22）
  --path PATH          远程部署路径（默认: /opt/quantmesh）
  --key KEY            SSH私钥路径
  --docker             使用Docker编译（适用于Mac）
  --skip-build         跳过编译步骤
  --skip-backup        跳过数据库备份（不推荐）
  -h, --help           显示此帮助信息

示例:
  # 使用配置文件
  $0 --config deploy-config.sh

  # 直接指定参数
  $0 --host example.com --user deploy --key ~/.ssh/id_rsa

  # 使用Docker编译（Mac环境）
  $0 --host example.com --user deploy --docker

配置文件示例 (deploy-config.sh):
  #!/bin/bash
  REMOTE_HOST="your-server.com"
  REMOTE_USER="deploy"
  REMOTE_PORT="22"
  REMOTE_PATH="/opt/quantmesh"
  SSH_KEY="~/.ssh/id_rsa"
  SERVICE_NAME="quantmesh"
  HEALTH_CHECK_URL="http://localhost:28888/api/status"
EOF
    exit 0
}

# 解析参数
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --config)
                DEPLOY_CONFIG="$2"
                shift 2
                ;;
            --host)
                REMOTE_HOST="$2"
                shift 2
                ;;
            --user)
                REMOTE_USER="$2"
                shift 2
                ;;
            --port)
                REMOTE_PORT="$2"
                shift 2
                ;;
            --path)
                REMOTE_PATH="$2"
                shift 2
                ;;
            --key)
                SSH_KEY="$2"
                shift 2
                ;;
            --docker)
                USE_DOCKER_BUILD=true
                shift
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
                show_help
                ;;
            *)
                log_error "未知参数: $1"
                show_help
                ;;
        esac
    done
}

# 加载配置文件
load_config() {
    if [ -f "${DEPLOY_CONFIG}" ]; then
        log_info "加载配置文件: ${DEPLOY_CONFIG}"
        source "${DEPLOY_CONFIG}"
    elif [ -f "${SCRIPT_DIR}/deploy-config.sh" ]; then
        log_info "加载默认配置文件: ${SCRIPT_DIR}/deploy-config.sh"
        source "${SCRIPT_DIR}/deploy-config.sh"
    else
        log_warn "未找到配置文件，将使用命令行参数或提示输入"
    fi
}

# 检查必需的命令
check_commands() {
    local missing_commands=()
    
    if ! command -v ssh >/dev/null 2>&1; then
        missing_commands+=("ssh")
    fi
    
    if ! command -v scp >/dev/null 2>&1; then
        missing_commands+=("scp")
    fi
    
    if [ "$USE_DOCKER_BUILD" = false ] && [ "$SKIP_BUILD" = false ]; then
        if ! command -v go >/dev/null 2>&1; then
            missing_commands+=("go")
        fi
    fi
    
    if [ ${#missing_commands[@]} -gt 0 ]; then
        log_error "缺少必需的命令: ${missing_commands[*]}"
        exit 1
    fi
}

# 提示输入配置
prompt_config() {
    if [ -z "$REMOTE_HOST" ]; then
        read -p "请输入远程服务器地址: " REMOTE_HOST
    fi
    
    if [ -z "$REMOTE_USER" ]; then
        read -p "请输入远程服务器用户名: " REMOTE_USER
    fi
    
    if [ -z "$SSH_KEY" ]; then
        read -p "请输入SSH私钥路径（留空使用默认）: " SSH_KEY
        if [ -z "$SSH_KEY" ]; then
            SSH_KEY="$HOME/.ssh/id_rsa"
        fi
    fi
    
    # 展开 ~ 路径
    SSH_KEY="${SSH_KEY/#\~/$HOME}"
    
    if [ ! -f "$SSH_KEY" ]; then
        log_error "SSH私钥文件不存在: $SSH_KEY"
        exit 1
    fi
}

# 构建SSH命令
build_ssh_cmd() {
    local cmd="ssh"
    if [ -n "$SSH_KEY" ]; then
        cmd="$cmd -i \"$SSH_KEY\""
    fi
    if [ -n "$REMOTE_PORT" ] && [ "$REMOTE_PORT" != "22" ]; then
        cmd="$cmd -p $REMOTE_PORT"
    fi
    cmd="$cmd -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
    cmd="$cmd ${REMOTE_USER}@${REMOTE_HOST}"
    echo "$cmd"
}

# 构建SCP命令
build_scp_cmd() {
    local cmd="scp"
    if [ -n "$SSH_KEY" ]; then
        cmd="$cmd -i \"$SSH_KEY\""
    fi
    if [ -n "$REMOTE_PORT" ] && [ "$REMOTE_PORT" != "22" ]; then
        cmd="$cmd -P $REMOTE_PORT"
    fi
    cmd="$cmd -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
    echo "$cmd"
}

# 执行远程命令
remote_exec() {
    local cmd="$1"
    local ssh_cmd=$(build_ssh_cmd)
    eval "$ssh_cmd \"$cmd\""
}

# 构建前端
build_frontend() {
    log_step "构建前端..."
    
    cd "${PROJECT_DIR}"
    
    if [ ! -d "webui" ]; then
        log_warn "前端目录不存在，跳过前端构建"
        return 0
    fi
    
    cd webui
    
    # 检查是否需要安装依赖
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
    log_info "构建前端..."
    if command -v yarn >/dev/null 2>&1; then
        yarn build
    else
        npm run build
    fi
    
    # 复制到 web/dist
    if [ -d "dist" ]; then
        log_info "复制前端文件到 web/dist..."
        rm -rf "${PROJECT_DIR}/web/dist"
        cp -r dist "${PROJECT_DIR}/web/dist"
    fi
    
    log_info "✅ 前端构建完成"
}

# 使用Docker构建
build_with_docker() {
    log_step "使用Docker构建..."
    
    cd "${PROJECT_DIR}"
    
    # 先构建前端
    build_frontend
    
    # 使用Docker编译后端
    log_info "使用Docker编译后端..."
    docker run --rm \
        -v "${PROJECT_DIR}":/app \
        -w /app \
        -e CGO_ENABLED=1 \
        golang:1.21 \
        bash -c "go mod download && go build -ldflags='-s -w' -o quantmesh ."
    
    if [ ! -f "${PROJECT_DIR}/quantmesh" ]; then
        log_error "Docker构建失败"
        exit 1
    fi
    
    log_info "✅ Docker构建完成"
}

# 本地构建
build_local() {
    log_step "本地构建..."
    
    cd "${PROJECT_DIR}"
    
    # 先构建前端
    build_frontend
    
    # 构建后端
    log_info "构建后端..."
    export CGO_ENABLED=1
    go build -ldflags="-s -w" -o quantmesh .
    
    if [ ! -f "${PROJECT_DIR}/quantmesh" ]; then
        log_error "构建失败"
        exit 1
    fi
    
    log_info "✅ 本地构建完成"
}

# 备份远程数据库
backup_remote_database() {
    log_step "备份远程数据库..."
    
    local backup_cmd="cd ${REMOTE_PATH} && ./scripts/backup.sh 2>/dev/null || mkdir -p backups && cp -r data backups/data_\$(date +%Y%m%d_%H%M%S) 2>/dev/null || true"
    remote_exec "$backup_cmd"
    
    log_info "✅ 远程数据库备份完成"
}

# 部署到远程服务器
deploy_to_remote() {
    log_step "部署到远程服务器..."
    
    # 确保远程目录存在
    remote_exec "mkdir -p ${REMOTE_PATH}/scripts"
    
    # 上传二进制文件
    log_info "上传二进制文件..."
    local scp_cmd=$(build_scp_cmd)
    eval "$scp_cmd \"${PROJECT_DIR}/quantmesh\" \"${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_PATH}/quantmesh\""
    
    # 设置执行权限
    remote_exec "chmod +x ${REMOTE_PATH}/quantmesh"
    
    log_info "✅ 文件上传完成"
}

# 重启远程服务
restart_remote_service() {
    log_step "重启远程服务..."
    
    # 检查是否有systemd服务
    local check_systemd="systemctl list-unit-files | grep -q ${SERVICE_NAME}.service && echo 'systemd' || echo 'none'"
    local service_type=$(remote_exec "$check_systemd" | tr -d '\r\n')
    
    if [ "$service_type" = "systemd" ]; then
        log_info "使用systemd重启服务..."
        remote_exec "sudo systemctl stop ${SERVICE_NAME} || true"
        sleep 2
        remote_exec "sudo systemctl start ${SERVICE_NAME}"
        sleep 3
        
        # 检查服务状态
        local status=$(remote_exec "sudo systemctl is-active ${SERVICE_NAME}" | tr -d '\r\n')
        if [ "$status" = "active" ]; then
            log_info "✅ systemd服务已启动"
        else
            log_error "❌ systemd服务启动失败"
            remote_exec "sudo systemctl status ${SERVICE_NAME}"
            return 1
        fi
    else
        # 尝试使用进程管理
        log_info "停止旧进程..."
        remote_exec "cd ${REMOTE_PATH} && pkill -f quantmesh || true"
        sleep 2
        
        log_info "启动新进程..."
        remote_exec "cd ${REMOTE_PATH} && nohup ./quantmesh config.yaml > logs/quantmesh.log 2>&1 &"
        sleep 3
        
        log_info "✅ 进程已启动"
    fi
}

# 健康检查
health_check() {
    log_step "执行健康检查..."
    
    local check_cmd="curl -f -s ${HEALTH_CHECK_URL} > /dev/null && echo 'ok' || echo 'fail'"
    local result=$(remote_exec "$check_cmd" | tr -d '\r\n')
    
    if [ "$result" = "ok" ]; then
        log_info "✅ 健康检查通过"
        return 0
    else
        log_error "❌ 健康检查失败"
        log_info "尝试查看服务日志..."
        remote_exec "tail -n 50 ${REMOTE_PATH}/logs/quantmesh.log 2>/dev/null || journalctl -u ${SERVICE_NAME} -n 50 --no-pager 2>/dev/null || echo '无法查看日志'"
        return 1
    fi
}

# 主流程
main() {
    log_info "=========================================="
    log_info "QuantMesh Market Maker 完整部署脚本"
    log_info "=========================================="
    log_info ""
    
    # 解析参数
    parse_args "$@"
    
    # 加载配置
    load_config
    
    # 检查命令
    check_commands
    
    # 提示输入配置（如果缺少）
    prompt_config
    
    # 验证连接
    log_step "验证SSH连接..."
    if ! remote_exec "echo 'SSH连接成功'" > /dev/null 2>&1; then
        log_error "无法连接到远程服务器"
        exit 1
    fi
    log_info "✅ SSH连接正常"
    
    # 构建
    if [ "$SKIP_BUILD" = false ]; then
        if [ "$USE_DOCKER_BUILD" = true ]; then
            build_with_docker
        else
            build_local
        fi
    else
        log_warn "跳过构建步骤"
    fi
    
    # 备份数据库
    if [ "$SKIP_BACKUP" = false ]; then
        backup_remote_database
    else
        log_warn "跳过数据库备份"
    fi
    
    # 部署
    deploy_to_remote
    
    # 重启服务
    restart_remote_service
    
    # 健康检查
    health_check
    
    log_info ""
    log_info "=========================================="
    log_info "✅ 部署完成！"
    log_info "=========================================="
    log_info ""
    log_info "服务地址: ${REMOTE_HOST}"
    log_info "部署路径: ${REMOTE_PATH}"
    log_info ""
}

# 执行主流程
main "$@"
