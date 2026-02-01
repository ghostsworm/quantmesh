#!/bin/bash

# QuantMesh Git 部署脚本
# 功能：
# 1. 自动提交并推送到 GitHub（如果本地有修改）
# 2. 从 GitHub 拉取最新代码到服务器
# 3. 在服务器上编译
# 4. 自动备份数据库
# 5. 使用 systemd 重启服务
#
# 使用方法：
#   ./scripts/deploy-git.sh [--skip-push]  # 跳过自动提交推送

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 配置
REMOTE_HOST="facev.app"
REMOTE_USER="root"
REMOTE_PORT="22"
REMOTE_PATH="/root/quntmesh"
GIT_REPO="git@github.com:ghostsworm/quantmesh.git"
GIT_BRANCH="main"
SERVICE_NAME="quantmesh"
SKIP_PUSH=false

# 解析参数
while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-push)
            SKIP_PUSH=true
            shift
            ;;
        *)
            log_error "未知参数: $1"
            exit 1
            ;;
    esac
done

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

# 执行远程命令
remote_exec() {
    ssh -p ${REMOTE_PORT} -o StrictHostKeyChecking=no ${REMOTE_USER}@${REMOTE_HOST} "$1"
}

# 主流程
main() {
    log_info "=========================================="
    log_info "QuantMesh Git 部署脚本"
    log_info "=========================================="
    log_info ""
    
    # 1. 验证SSH连接
    log_step "验证SSH连接..."
    if ! remote_exec "echo '连接成功'" > /dev/null 2>&1; then
        log_error "无法连接到服务器"
        exit 1
    fi
    log_info "✅ SSH连接正常"
    
    # 2. 提交并推送代码到 GitHub（如果本地有修改）
    if [ "$SKIP_PUSH" = false ]; then
        log_step "检查本地代码修改..."
        PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
        cd "${PROJECT_DIR}"
        
        if [ -n "$(git status --porcelain)" ]; then
            log_info "发现未提交的修改，自动提交..."
            git add -A
            git commit -m "自动部署: $(date +%Y%m%d_%H%M%S)" || log_warn "提交失败或没有需要提交的内容"
            git push origin ${GIT_BRANCH} || log_warn "推送失败，请手动推送"
            log_info "✅ 代码已推送到 GitHub"
        else
            log_info "本地代码无修改，跳过提交"
        fi
    else
        log_info "跳过自动提交推送（使用 --skip-push）"
    fi
    
    # 3. 检查并创建目录
    log_step "检查部署目录..."
    remote_exec "mkdir -p ${REMOTE_PATH} ${REMOTE_PATH}/data ${REMOTE_PATH}/logs ${REMOTE_PATH}/scripts ${REMOTE_PATH}/backups"
    
    # 4. 克隆或更新代码
    log_step "更新代码..."
    # 添加 Git 安全目录配置
    remote_exec "git config --global --add safe.directory ${REMOTE_PATH} 2>/dev/null || true"
    
    if remote_exec "test -d ${REMOTE_PATH}/.git" 2>/dev/null; then
        log_info "目录已存在，执行 git pull..."
        remote_exec "cd ${REMOTE_PATH} && git fetch origin && git reset --hard origin/${GIT_BRANCH} && git clean -fd"
    else
        log_info "目录不存在，执行 git clone..."
        remote_exec "cd $(dirname ${REMOTE_PATH}) && git clone ${GIT_REPO} $(basename ${REMOTE_PATH})"
        remote_exec "git config --global --add safe.directory ${REMOTE_PATH} 2>/dev/null || true"
    fi
    log_info "✅ 代码更新完成"
    
    # 5. 备份数据库（如果存在）
    log_step "备份数据库..."
    remote_exec "cd ${REMOTE_PATH} && if [ -f data/quantmesh.db ]; then mkdir -p backups && cp -r data backups/data_\$(date +%Y%m%d_%H%M%S) 2>/dev/null || true; fi"
    log_info "✅ 数据库备份完成"
    
    # 6. 在服务器上构建前端
    log_step "在服务器上构建前端..."
    remote_exec "cd ${REMOTE_PATH}/webui && if [ -f package.json ]; then if [ ! -d node_modules ]; then npm install || yarn install || pnpm install; fi && npm run build || yarn build || pnpm build; fi && cd .. && if [ -d webui/dist ]; then rm -rf web/dist && cp -r webui/dist web/dist; fi"
    log_info "✅ 前端构建完成"
    
    # 7. 在服务器上构建后端
    log_step "在服务器上构建后端..."
    remote_exec "cd ${REMOTE_PATH} && export CGO_ENABLED=1 && go mod download && go build -buildvcs=false -ldflags='-s -w' -o quantmesh ."
    log_info "✅ 后端构建完成"
    
    # 8. 设置执行权限
    log_step "设置执行权限..."
    remote_exec "chmod +x ${REMOTE_PATH}/quantmesh ${REMOTE_PATH}/scripts/*.sh 2>/dev/null || true"
    log_info "✅ 权限设置完成"
    
    # 9. 配置 systemd 服务（如果不存在）
    log_step "配置 systemd 服务..."
    if ! remote_exec "systemctl list-unit-files | grep -q ${SERVICE_NAME}.service" 2>/dev/null; then
        log_info "创建 systemd 服务文件..."
        # 使用单引号避免变量替换问题
        remote_exec "cat > /tmp/${SERVICE_NAME}.service << 'EOFSERVICE'
[Unit]
Description=QuantMesh Market Maker Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${REMOTE_PATH}
ExecStart=${REMOTE_PATH}/quantmesh config.yaml
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=quantmesh

# 资源限制
LimitNOFILE=65536
LimitNPROC=4096

[Install]
WantedBy=multi-user.target
EOFSERVICE
        "
        remote_exec "mv /tmp/${SERVICE_NAME}.service /etc/systemd/system/${SERVICE_NAME}.service"
        remote_exec "systemctl daemon-reload"
        remote_exec "systemctl enable ${SERVICE_NAME}"
        log_info "✅ systemd 服务已创建并启用"
    else
        log_info "systemd 服务已存在"
    fi
    
    # 10. 重启服务
    log_step "重启服务..."
    remote_exec "systemctl stop ${SERVICE_NAME} || true"
    sleep 2
    remote_exec "systemctl start ${SERVICE_NAME}"
    sleep 3
    
    # 检查服务状态
    local status=$(remote_exec "systemctl is-active ${SERVICE_NAME}" | tr -d '\r\n')
    if [ "$status" = "active" ]; then
        log_info "✅ systemd 服务已启动"
    else
        log_error "❌ systemd 服务启动失败"
        remote_exec "systemctl status ${SERVICE_NAME} --no-pager -l"
        return 1
    fi
    
    # 11. 健康检查
    log_step "执行健康检查..."
    sleep 2
    if remote_exec "curl -f -s http://localhost:28888/api/status > /dev/null 2>&1 && echo 'ok' || echo 'fail'" | grep -q "ok"; then
        log_info "✅ 健康检查通过"
    else
        log_warn "⚠️ 健康检查失败，请查看日志: journalctl -u ${SERVICE_NAME} -n 50"
    fi
    
    log_info ""
    log_info "=========================================="
    log_info "✅ 部署完成！"
    log_info "=========================================="
    log_info ""
    log_info "服务地址: ${REMOTE_HOST}"
    log_info "部署路径: ${REMOTE_PATH}"
    log_info "查看状态: ssh ${REMOTE_USER}@${REMOTE_HOST} 'systemctl status ${SERVICE_NAME}'"
    log_info "查看日志: ssh ${REMOTE_USER}@${REMOTE_HOST} 'journalctl -u ${SERVICE_NAME} -f'"
    log_info ""
}

main "$@"
